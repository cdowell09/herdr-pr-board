// Package reviewflow coordinates review completion, review notification, and
// configured publication.
package reviewflow

import (
	"context"
	"errors"
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

// Notifier shows a review notification for a finished run.
type Notifier interface {
	Notify(context.Context, reviewmemory.Run) error
}

// Result is the recorded run of one coordinated review. Notification holds a
// failed review notification, which changes neither the run nor the error.
type Result struct {
	Run          reviewmemory.Run
	Notification error
}

// Run notifies on the recorded run outcome and publishes a newly returned
// successful completion. The two run concurrently, so a slow notification
// never delays publication, and Run returns after both finish. Publication
// errors leave the recorded review completed; callers report that separate
// failure.
func Run(ctx context.Context, reviewer Reviewer, publisher Publisher, notifier Notifier, request review.Request, progress func(string)) (Result, error) {
	run, err := reviewer.Review(ctx, request, progress)
	notified := make(chan error, 1)
	go func() { notified <- notify(ctx, notifier, run, err) }()
	publishErr := publish(ctx, publisher, request.URL, run, err)
	return Result{Run: run, Notification: <-notified}, publishErr
}

// notify skips a stopped run and a caller that is shutting down. The notifier
// decides everything else from the recorded run.
func notify(ctx context.Context, notifier Notifier, run reviewmemory.Run, err error) error {
	if notifier == nil || ctx.Err() != nil || errors.Is(err, reviewmemory.ErrStopped) {
		return nil
	}
	return notifier.Notify(ctx, run)
}

func publish(ctx context.Context, publisher Publisher, url string, run reviewmemory.Run, err error) error {
	if err != nil || run.Status != reviewmemory.Completed || publisher == nil {
		return err
	}
	if _, err := publisher.PublishConfigured(ctx, url, run.ID); err != nil {
		return fmt.Errorf("review completed; publication failed: %w", err)
	}
	return nil
}
