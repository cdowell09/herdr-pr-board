// Package review owns manual reviewer execution and outcome recording.
package review

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"github.com/cdowell09/herdr-pr-board/internal/config"
	gh "github.com/cdowell09/herdr-pr-board/internal/github"
	"github.com/cdowell09/herdr-pr-board/internal/reviewmemory"
)

type RevisionSource interface {
	CaptureRevision(context.Context, string) (gh.PullRequest, error)
}

type Request struct {
	URL              string
	Reviewer         string
	Rerun            bool
	Automatic        bool
	ExpectedRevision *reviewmemory.Identity
	ObservedViews    []config.View
	ObservedConfig   config.Config
}

type Service struct {
	mu         sync.Mutex
	closed     bool
	wg         sync.WaitGroup
	store      *reviewmemory.Store
	stateDir   string
	configPath string
	source     RevisionSource
}

func New(stateDir, configPath string, source RevisionSource) (*Service, error) {
	store, err := reviewmemory.Open(stateDir)
	if err != nil {
		return nil, err
	}
	return &Service{store: store, stateDir: stateDir, configPath: configPath, source: source}, nil
}

func (s *Service) History(prURL string) ([]reviewmemory.Run, error) {
	repo, number, err := gh.ParsePRURL(prURL)
	if err != nil {
		return nil, err
	}
	return s.store.PRHistory(repo, number)
}

func (s *Service) Wait() { s.mu.Lock(); s.closed = true; s.mu.Unlock(); s.wg.Wait() }

func (s *Service) Snapshot() (reviewmemory.Snapshot, error) { return s.store.Snapshot() }

func (s *Service) ReviewStatus(id reviewmemory.Identity) error { return s.store.ReviewStatus(id) }

// ReviewCapacity reports current installation-wide availability without reserving a slot.
func (s *Service) ReviewCapacity() error {
	cfg, err := config.LoadExisting(s.configPath)
	if err != nil {
		return err
	}
	available, err := s.store.HasCapacity(cfg.Review.MaxConcurrency)
	if err != nil {
		return err
	}
	if !available {
		return reviewmemory.ErrCapacity
	}
	return nil
}

func (s *Service) RunDirectory(id string) string { return filepath.Join(s.stateDir, "reviews", id) }

// Review queues only for capacity. Each launch rechecks configuration and revision.
// notify receives queued/running transitions on the calling goroutine.
func (s *Service) Review(ctx context.Context, request Request, notify func(string)) (reviewmemory.Run, error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return reviewmemory.Run{}, errors.New("review service is closed")
	}
	s.wg.Add(1)
	s.mu.Unlock()
	defer s.wg.Done()
	if request.Automatic && (request.Rerun || request.ExpectedRevision == nil) {
		return reviewmemory.Run{}, errors.New("automatic review requires an observed revision and cannot rerun")
	}
	initial, err := config.LoadExisting(s.configPath)
	if err != nil {
		return reviewmemory.Run{}, err
	}
	timeout, err := initial.Review.TimeoutDuration()
	if err != nil {
		return reviewmemory.Run{}, err
	}
	ctx, cancelReview := context.WithTimeout(ctx, timeout)
	defer cancelReview()
	if _, _, err := gh.ParsePRURL(request.URL); err != nil {
		return reviewmemory.Run{}, err
	}
	queued := false
	for {
		if err := ctx.Err(); err != nil {
			return reviewmemory.Run{}, err
		}
		cfg, err := config.LoadExisting(s.configPath)
		if err != nil {
			return reviewmemory.Run{}, err
		}
		if queued {
			available, err := s.store.HasCapacity(cfg.Review.MaxConcurrency)
			if err != nil {
				return reviewmemory.Run{}, err
			}
			if !available {
				if err := waitForSlot(ctx); err != nil {
					return reviewmemory.Run{}, err
				}
				continue
			}
		}
		captureCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		pr, err := s.source.CaptureRevision(captureCtx, request.URL)
		cancel()
		if err != nil {
			return reviewmemory.Run{}, fmt.Errorf("capture current PR revision: %w", err)
		}
		cfg, err = config.LoadExisting(s.configPath)
		if err != nil {
			return reviewmemory.Run{}, err
		}
		identity := reviewmemory.Identity{Repository: pr.Repository, Number: pr.Number, HeadOID: pr.HeadOID, BaseRefName: pr.BaseRefName}
		if err := request.validateAutomatic(pr, cfg); err != nil {
			return reviewmemory.Run{}, err
		}
		reviewer, err := cfg.ResolveLaunch(pr.Repository, request.Reviewer, request.Automatic)
		if err != nil {
			return reviewmemory.Run{}, err
		}
		claim, err := s.store.Claim(reviewmemory.Request{Identity: identity, BaseOID: pr.BaseOID, Reviewer: reviewer.ID, Rerun: request.Rerun, MaxConcurrent: cfg.Review.MaxConcurrency})
		if errors.Is(err, reviewmemory.ErrCapacity) {
			if request.Automatic {
				return reviewmemory.Run{}, err
			}
			if !queued && notify != nil {
				notify("queued")
			}
			queued = true
			if err := waitForSlot(ctx); err != nil {
				return reviewmemory.Run{}, err
			}
			continue
		}
		if err != nil {
			return reviewmemory.Run{}, err
		}
		defer claim.Close()
		if notify != nil {
			notify("running")
		}
		timeout, _ := cfg.Review.TimeoutDuration()
		runCtx, cancel := context.WithTimeout(ctx, timeout)
		outcome, executionErr := s.executeStoppable(runCtx, claim, pr, reviewer, request)
		cancel()
		if err := claim.Finish(outcome); errors.Is(err, reviewmemory.ErrStopped) {
			executionErr = err
		} else if err != nil {
			return reviewmemory.Run{}, fmt.Errorf("record review outcome: %w", err)
		}
		history, err := s.store.History(identity)
		if err != nil {
			return reviewmemory.Run{}, err
		}
		for _, run := range history {
			if run.ID == claim.ID() {
				return run, executionErr
			}
		}
		return reviewmemory.Run{}, errors.New("recorded review run is unavailable")
	}
}

func waitForSlot(ctx context.Context) error {
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (request Request) validateAutomatic(pr gh.PullRequest, cfg config.Config) error {
	if !request.Automatic {
		return nil
	}
	if request.Rerun || request.ExpectedRevision == nil {
		return errors.New("automatic review requires an observed revision and cannot rerun")
	}
	if !config.SameDiscovery(request.ObservedConfig, cfg) {
		return errors.New("discovery configuration changed since the selected observation")
	}
	id := reviewmemory.Identity{Repository: pr.Repository, Number: pr.Number, HeadOID: pr.HeadOID, BaseRefName: pr.BaseRefName}
	if id != *request.ExpectedRevision {
		return errors.New("PR revision changed since the selected observation")
	}
	if pr.Draft || pr.State != gh.PROpen {
		return errors.New("automatic review requires an open, non-draft PR")
	}
	if !cfg.SelectsAutomaticView(request.ObservedViews) {
		return errors.New("observed view is no longer selected for automatic reviews")
	}
	return nil
}
