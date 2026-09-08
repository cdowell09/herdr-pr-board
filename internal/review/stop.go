package review

import (
	"context"
	"fmt"
	"time"

	"github.com/cdowell09/herdr-pr-board/internal/config"
	gh "github.com/cdowell09/herdr-pr-board/internal/github"
	"github.com/cdowell09/herdr-pr-board/internal/reviewmemory"
)

// Stop requests cancellation from the run's owner, including another board or monitor.
func (s *Service) Stop(runID string) error { return s.store.RequestStop(runID) }

func (s *Service) executeStoppable(ctx context.Context, claim *reviewmemory.Claim, pr gh.PullRequest, reviewer config.Reviewer, request Request) (reviewmemory.Outcome, error) {
	if err := claim.EnableStop(); err != nil {
		return reviewmemory.Outcome{Status: reviewmemory.Failed, Message: err.Error()}, err
	}
	ctx, cancel := context.WithCancelCause(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		for {
			stopped, err := claim.StopRequested()
			if err != nil {
				cancel(fmt.Errorf("read review stop request: %w", err))
				return
			}
			if stopped {
				cancel(reviewmemory.ErrStopped)
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	outcome, err := s.execute(ctx, claim, pr, reviewer, request)
	if cause := context.Cause(ctx); cause != nil {
		outcome, err = reviewmemory.Outcome{Status: reviewmemory.Failed, Message: cause.Error()}, cause
	}
	cancel(nil)
	<-done
	return outcome, err
}
