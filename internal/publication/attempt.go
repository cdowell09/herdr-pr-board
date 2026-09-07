package publication

import (
	"encoding/hex"
	"encoding/json"
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

type Status string

const (
	Uncertain Status = "uncertain"
	Failed    Status = "failed"
	Published Status = "published"
)

type Attempt struct {
	Version   int                      `json:"version"`
	ID        string                   `json:"id"`
	RunID     string                   `json:"run_id"`
	Identity  reviewmemory.Identity    `json:"identity"`
	Action    config.PublicationAction `json:"action"`
	Actor     string                   `json:"actor"`
	Status    Status                   `json:"status"`
	StartedAt time.Time                `json:"started_at"`
	GitHubID  int64                    `json:"github_id,omitempty"`
	URL       string                   `json:"url,omitempty"`
	Message   string                   `json:"message,omitempty"`
}

func (a Attempt) marker() string { return "<!-- herdr-pr-board publication:" + a.ID + " -->" }

func (a Attempt) matches(review gh.PublishedReview) bool {
	state := map[config.PublicationAction]string{config.PublishComment: "COMMENTED", config.PublishApprove: "APPROVED", config.PublishRequestChanges: "CHANGES_REQUESTED"}[a.Action]
	return review.ID > 0 && strings.EqualFold(review.URL, reviewURL(a.Identity, review.ID)) && review.CommitID == a.Identity.HeadOID && strings.EqualFold(review.User.Login, a.Actor) && review.State == state && strings.Contains(review.Body, a.marker())
}

func reviewURL(id reviewmemory.Identity, reviewID int64) string {
	return fmt.Sprintf("https://github.com/%s/pull/%d#pullrequestreview-%d", id.Repository, id.Number, reviewID)
}

func validToken(value string) bool {
	b, err := hex.DecodeString(value)
	return err == nil && len(b) == 16 && value == strings.ToLower(value)
}

func (a Attempt) validate() error {
	if a.Version != 1 || !validToken(a.ID) || !validToken(a.RunID) || !a.Action.Valid() || reviewmemory.ValidateIdentity(a.Identity) != nil || a.Actor == "" || a.StartedAt.IsZero() {
		return errors.New("invalid publication record")
	}
	if a.Status != Uncertain && a.Status != Published && a.Status != Failed {
		return errors.New("invalid publication status")
	}
	if a.Status != Published && (a.GitHubID != 0 || a.URL != "") {
		return errors.New("unresolved publication contains a published identifier")
	}
	if a.Status == Failed && strings.TrimSpace(a.Message) == "" {
		return errors.New("failed publication has no failure detail")
	}
	if a.Status == Published && (a.GitHubID <= 0 || !strings.EqualFold(a.URL, reviewURL(a.Identity, a.GitHubID))) {
		return errors.New("invalid published review identifier")
	}
	return nil
}

func (s *Service) path(runID string, action config.PublicationAction) string {
	return filepath.Join(s.dir, runID+"-"+string(action)+".json")
}

type record struct {
	Version  int       `json:"version"`
	Attempts []Attempt `json:"attempts"`
}

func (s *Service) readAll(runID string, action config.PublicationAction) ([]Attempt, error) {
	var r record
	data, err := os.ReadFile(s.path(runID, action))
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("invalid publication record: %w", err)
	}
	if r.Version != 1 || len(r.Attempts) == 0 {
		return nil, errors.New("invalid publication history")
	}
	seen := map[string]bool{}
	for i, a := range r.Attempts {
		if err := a.validate(); err != nil {
			return nil, err
		}
		if a.RunID != runID || a.Action != action || seen[a.ID] || a.Identity != r.Attempts[0].Identity {
			return nil, errors.New("publication history does not match its identity")
		}
		if i < len(r.Attempts)-1 && a.Status != Failed {
			return nil, errors.New("publication history retried an unresolved attempt")
		}
		seen[a.ID] = true
	}
	return r.Attempts, nil
}

func (s *Service) read(runID string, action config.PublicationAction) (Attempt, error) {
	attempts, err := s.readAll(runID, action)
	if err != nil {
		return Attempt{}, err
	}
	return attempts[len(attempts)-1], nil
}

func (s *Service) write(a Attempt) error {
	if err := a.validate(); err != nil {
		return err
	}
	attempts, err := s.readAll(a.RunID, a.Action)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if len(attempts) > 0 && attempts[len(attempts)-1].ID == a.ID {
		attempts[len(attempts)-1] = a
	} else {
		attempts = append(attempts, a)
	}
	data, err := json.Marshal(record{Version: 1, Attempts: attempts})
	if err != nil {
		return err
	}
	return localstate.AtomicWrite(s.path(a.RunID, a.Action), data)
}

// History returns publication records independently of local review completions.
func (s *Service) History(prURL string) ([]Attempt, error) {
	repo, number, err := gh.ParsePRURL(prURL)
	if err != nil {
		return nil, err
	}
	runs, err := s.store.PRHistory(repo, number)
	if err != nil {
		return nil, err
	}
	attempts := []Attempt{}
	for _, run := range runs {
		for _, action := range config.PublicationActions() {
			records, err := s.readAll(run.ID, action)
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				return nil, err
			}
			for _, a := range records {
				if a.Identity != run.Identity {
					return nil, errors.New("publication identity differs from review history")
				}
				attempts = append(attempts, a)
			}
		}
	}
	return attempts, nil
}
