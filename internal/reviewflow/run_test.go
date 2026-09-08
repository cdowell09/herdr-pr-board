package reviewflow

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cdowell09/herdr-pr-board/internal/publication"
	"github.com/cdowell09/herdr-pr-board/internal/review"
	"github.com/cdowell09/herdr-pr-board/internal/reviewmemory"
)

type reviewFunc func(context.Context, review.Request, func(string)) (reviewmemory.Run, error)

func (f reviewFunc) Review(ctx context.Context, r review.Request, n func(string)) (reviewmemory.Run, error) {
	return f(ctx, r, n)
}

type publishFunc func(context.Context, string, string) (publication.Attempt, error)

func (f publishFunc) PublishConfigured(ctx context.Context, url, id string) (publication.Attempt, error) {
	return f(ctx, url, id)
}

func TestEveryNewCompletionUsesSamePostingPath(t *testing.T) {
	for _, request := range []review.Request{{URL: "manual"}, {URL: "rerun", Rerun: true}, {URL: "monitor", Automatic: true}} {
		t.Run(request.URL, func(t *testing.T) {
			calls := 0
			reviewer := reviewFunc(func(_ context.Context, got review.Request, notify func(string)) (reviewmemory.Run, error) {
				if got.URL != request.URL || got.Rerun != request.Rerun || got.Automatic != request.Automatic {
					t.Fatalf("launch semantics changed: %+v", got)
				}
				notify("running")
				return reviewmemory.Run{ID: "new-run", Outcome: reviewmemory.Outcome{Status: reviewmemory.Completed}}, nil
			})
			publisher := publishFunc(func(_ context.Context, url, id string) (publication.Attempt, error) {
				calls++
				if url != request.URL || id != "new-run" {
					t.Fatal("wrong completion published")
				}
				return publication.Attempt{}, nil
			})
			progress := ""
			run, err := Run(context.Background(), reviewer, publisher, request, func(state string) { progress = state })
			if err != nil || run.Status != reviewmemory.Completed || calls != 1 || progress != "running" {
				t.Fatalf("%+v %v calls%d", run, err, calls)
			}
		})
	}
}

func TestNoncompletionsAndReviewErrorsNeverPublish(t *testing.T) {
	for _, status := range []reviewmemory.Status{reviewmemory.Failed, reviewmemory.Blocked, reviewmemory.Running, reviewmemory.Abandoned, reviewmemory.Completed} {
		t.Run(string(status), func(t *testing.T) {
			reviewErr := error(nil)
			if status == reviewmemory.Completed {
				reviewErr = errors.New("completion recording failed")
			}
			reviewer := reviewFunc(func(context.Context, review.Request, func(string)) (reviewmemory.Run, error) {
				return reviewmemory.Run{Outcome: reviewmemory.Outcome{Status: status}}, reviewErr
			})
			publisher := publishFunc(func(context.Context, string, string) (publication.Attempt, error) {
				t.Fatal("published incomplete review")
				return publication.Attempt{}, nil
			})
			_, err := Run(context.Background(), reviewer, publisher, review.Request{}, nil)
			if !errors.Is(err, reviewErr) {
				t.Fatalf("review error lost: %v", err)
			}
		})
	}
}

func TestPublicationFailurePreservesCompletedRunAndCause(t *testing.T) {
	reviewer := reviewFunc(func(context.Context, review.Request, func(string)) (reviewmemory.Run, error) {
		return reviewmemory.Run{ID: "completed", Outcome: reviewmemory.Outcome{Status: reviewmemory.Completed}}, nil
	})
	publisher := publishFunc(func(context.Context, string, string) (publication.Attempt, error) {
		return publication.Attempt{}, publication.ErrUncertain
	})
	run, err := Run(context.Background(), reviewer, publisher, review.Request{}, nil)
	if run.Status != reviewmemory.Completed || run.ID != "completed" || !errors.Is(err, publication.ErrUncertain) || !strings.Contains(err.Error(), "review completed; publication failed") {
		t.Fatalf("%+v %v", run, err)
	}
}
