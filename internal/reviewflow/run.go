// Package reviewflow coordinates review completion and configured publication.
package reviewflow

import (
	"context"
	"fmt"

	"github.com/cdowell09/herdr-pr-board/internal/publication"
	"github.com/cdowell09/herdr-pr-board/internal/review"
	"github.com/cdowell09/herdr-pr-board/internal/reviewmemory"
)

type Reviewer interface {
	Review(context.Context, review.Request, func(string)) (reviewmemory.Run, error)
}
type Publisher interface {
	PublishConfigured(context.Context, string, string) (publication.Attempt, error)
}

// Run publishes only a newly returned successful completion. Publication errors
// leave the recorded review completed; callers report that separate failure.
func Run(ctx context.Context, reviewer Reviewer, publisher Publisher, request review.Request, notify func(string)) (reviewmemory.Run, error) {
	run, err := reviewer.Review(ctx, request, notify)
	if err != nil || run.Status != reviewmemory.Completed || publisher == nil {
		return run, err
	}
	if _, err := publisher.PublishConfigured(ctx, request.URL, run.ID); err != nil {
		return run, fmt.Errorf("review completed; publication failed: %w", err)
	}
	return run, nil
}
