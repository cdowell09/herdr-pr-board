// Package publication owns permission checks and idempotent GitHub review publication.
package publication

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cdowell09/herdr-pr-board/internal/config"
	gh "github.com/cdowell09/herdr-pr-board/internal/github"
	"github.com/cdowell09/herdr-pr-board/internal/localstate"
	"github.com/cdowell09/herdr-pr-board/internal/reviewmemory"
)

var ErrUncertain = errors.New("publication outcome remains uncertain; retry only reconciles existing GitHub reviews")

type GitHub interface {
	CaptureRevision(context.Context, string) (gh.PullRequest, error)
	PublicationActor(context.Context) (string, error)
	ListPublishedReviews(context.Context, string) ([]gh.PublishedReview, error)
	PublishReview(context.Context, string, string, string, string) (gh.PublishedReview, error)
}

type Service struct {
	dir, configPath string
	store           *reviewmemory.Store
	github          GitHub
	timeout         time.Duration
}

func New(stateDir, configPath string, client GitHub) (*Service, error) {
	store, err := reviewmemory.Open(stateDir)
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(stateDir, "publications")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	return &Service{dir: dir, configPath: configPath, store: store, github: client, timeout: 90 * time.Second}, nil
}

func (s *Service) completedRun(prURL, runID string) (reviewmemory.Run, error) {
	repo, number, err := gh.ParsePRURL(prURL)
	if err != nil {
		return reviewmemory.Run{}, err
	}
	runs, err := s.store.PRHistory(repo, number)
	if err != nil {
		return reviewmemory.Run{}, err
	}
	for _, run := range runs {
		if run.ID == runID && run.Status == reviewmemory.Completed {
			return run, nil
		}
	}
	return reviewmemory.Run{}, errors.New("select a completed local review run for this PR")
}

func (s *Service) permission(repository string, action config.PublicationAction, automatic bool) error {
	cfg, err := config.LoadExisting(s.configPath)
	if err != nil {
		return err
	}
	if automatic {
		repo, _ := cfg.RepositoryFor(repository)
		if repo.AutoPublish != action {
			return errors.New("automatic publication selection changed before publication")
		}
	}
	return cfg.AllowPublication(repository, action)
}

// Publish sends at most one request for a completed run and publication action.
// A lost response never authorizes another POST, including after process restart.
func (s *Service) Publish(ctx context.Context, prURL, runID string, action config.PublicationAction) (Attempt, error) {
	return s.publish(ctx, prURL, runID, action, false)
}

// PublishAutomatic additionally requires the explicit automatic action selector.
func (s *Service) PublishAutomatic(ctx context.Context, prURL, runID string, action config.PublicationAction) (Attempt, error) {
	return s.publish(ctx, prURL, runID, action, true)
}

func (s *Service) publish(ctx context.Context, prURL, runID string, action config.PublicationAction, automatic bool) (Attempt, error) {
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	if !action.Valid() {
		return Attempt{}, errors.New("invalid publication action")
	}
	run, err := s.completedRun(prURL, runID)
	if err != nil {
		return Attempt{}, err
	}
	// ponytail: publications serialize globally; use per-run/action locks if throughput requires it.
	lock, err := localstate.Lock(ctx, filepath.Join(s.dir, "publication.lock"))
	if err != nil {
		return Attempt{}, err
	}
	defer lock.Close()
	previous, err := s.read(run.ID, action)
	if err == nil {
		if previous.Identity != run.Identity {
			return Attempt{}, errors.New("publication record identity differs from the completed run")
		}
		if previous.Status == Published {
			return previous, nil
		}
		if previous.Status == Uncertain {
			return s.reconcile(ctx, prURL, previous)
		}
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return Attempt{}, err
	}
	if err := s.permission(run.Identity.Repository, action, automatic); err != nil {
		return Attempt{}, err
	}
	actor, err := s.github.PublicationActor(ctx)
	if err != nil {
		return Attempt{}, err
	}
	current, err := s.github.CaptureRevision(ctx, prURL)
	if err != nil {
		return Attempt{}, err
	}
	if current.HeadOID != run.Identity.HeadOID || current.BaseRefName != run.Identity.BaseRefName || !strings.EqualFold(current.Repository, run.Identity.Repository) || current.Number != run.Identity.Number {
		return Attempt{}, errors.New("review findings refer to an older revision; review the current head and target before publication")
	}
	var token [16]byte
	if _, err := rand.Read(token[:]); err != nil {
		return Attempt{}, err
	}
	a := Attempt{Version: 1, ID: hex.EncodeToString(token[:]), RunID: run.ID, Identity: run.Identity, Action: action, Actor: actor, Status: Uncertain, StartedAt: time.Now().UTC()}
	body := reviewBody(run, a.marker())
	if len(body) > 60000 {
		return Attempt{}, errors.New("review findings exceed the 60000-byte publication limit")
	}
	// Reload after network preflight. Revocation does not depend on the board's cache.
	if err := s.permission(run.Identity.Repository, action, automatic); err != nil {
		return Attempt{}, err
	}
	if err := s.write(a); err != nil {
		return Attempt{}, err
	}
	result, postErr := s.github.PublishReview(ctx, prURL, run.Identity.HeadOID, strings.ToUpper(string(action)), body)
	if postErr == nil && a.matches(result) {
		a.Status, a.GitHubID, a.URL = Published, result.ID, result.URL
	} else {
		if postErr == nil {
			postErr = errors.New("GitHub returned a different review identity or action")
		}
		a.Message = postErr.Error()
		var rejected *gh.PublicationRejected
		if errors.As(postErr, &rejected) {
			a.Status = Failed
		}
	}
	if err := s.write(a); err != nil {
		return a, fmt.Errorf("record publication result: %w", err)
	}
	if a.Status == Failed {
		return a, postErr
	}
	if a.Status == Uncertain {
		return a, fmt.Errorf("%w: %s", ErrUncertain, a.Message)
	}
	return a, nil
}

func (s *Service) reconcile(ctx context.Context, prURL string, a Attempt) (Attempt, error) {
	actor, err := s.github.PublicationActor(ctx)
	if err != nil {
		return a, err
	}
	if !strings.EqualFold(actor, a.Actor) {
		return a, fmt.Errorf("%w; authenticate as %s to reconcile", ErrUncertain, a.Actor)
	}
	reviews, err := s.github.ListPublishedReviews(ctx, prURL)
	if err != nil {
		return a, fmt.Errorf("%w: %v", ErrUncertain, err)
	}
	var match *gh.PublishedReview
	for i := range reviews {
		if a.matches(reviews[i]) {
			if match != nil {
				return a, fmt.Errorf("%w; multiple matching reviews require inspection", ErrUncertain)
			}
			match = &reviews[i]
		}
	}
	if match == nil {
		return a, ErrUncertain
	}
	a.Status, a.GitHubID, a.URL, a.Message = Published, match.ID, match.URL, ""
	if err := s.write(a); err != nil {
		return a, err
	}
	return a, nil
}

func reviewBody(run reviewmemory.Run, marker string) string {
	var body strings.Builder
	fmt.Fprintf(&body, "%s\n\nReviewed commit `%s` targeting `%s`.\n\n%s\n", marker, run.Identity.HeadOID, run.Identity.BaseRefName, run.Message)
	for _, f := range run.Findings {
		fmt.Fprintf(&body, "\n### [%s] %s\n\n", f.Severity, f.Title)
		if f.Path != "" {
			fmt.Fprintf(&body, "`%s`", f.Path)
			if f.Line > 0 {
				fmt.Fprintf(&body, ":%d", f.Line)
			}
			body.WriteString("\n\n")
		}
		body.WriteString(f.Body + "\n")
	}
	return body.String()
}
