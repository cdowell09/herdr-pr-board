package reviewflow

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cdowell09/herdr-pr-board/internal/publication"
	"github.com/cdowell09/herdr-pr-board/internal/review"
	"github.com/cdowell09/herdr-pr-board/internal/reviewmemory"
)

type reviewFunc func(context.Context, review.Request, func(string)) (reviewmemory.Run, error)

func (f reviewFunc) Review(ctx context.Context, r review.Request, progress func(string)) (reviewmemory.Run, error) {
	return f(ctx, r, progress)
}

type publishFunc func(context.Context, string, string) (publication.Attempt, error)

func (f publishFunc) PublishConfigured(ctx context.Context, url, id string) (publication.Attempt, error) {
	return f(ctx, url, id)
}

type notifyFunc func(context.Context, reviewmemory.Run) error

func (f notifyFunc) Notify(ctx context.Context, run reviewmemory.Run) error { return f(ctx, run) }

func TestEveryNewCompletionUsesSamePostingPath(t *testing.T) {
	for _, request := range []review.Request{{URL: "manual"}, {URL: "rerun", Rerun: true}, {URL: "monitor", Automatic: true}} {
		t.Run(request.URL, func(t *testing.T) {
			calls := 0
			reviewer := reviewFunc(func(_ context.Context, got review.Request, progress func(string)) (reviewmemory.Run, error) {
				if got.URL != request.URL || got.Rerun != request.Rerun || got.Automatic != request.Automatic {
					t.Fatalf("launch semantics changed: %+v", got)
				}
				progress("running")
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
			result, err := Run(context.Background(), reviewer, publisher, nil, request, func(state string) { progress = state })
			if err != nil || result.Run.Status != reviewmemory.Completed || result.Notification != nil || calls != 1 || progress != "running" {
				t.Fatalf("%+v %v calls%d", result, err, calls)
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
			_, err := Run(context.Background(), reviewer, publisher, nil, review.Request{}, nil)
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
	result, err := Run(context.Background(), reviewer, publisher, nil, review.Request{}, nil)
	if result.Run.Status != reviewmemory.Completed || result.Run.ID != "completed" || !errors.Is(err, publication.ErrUncertain) || !strings.Contains(err.Error(), "review completed; publication failed") {
		t.Fatalf("%+v %v", result, err)
	}
}

func TestEveryRecordedOutcomeNotifiesAlongsidePublication(t *testing.T) {
	for _, status := range []reviewmemory.Status{reviewmemory.Completed, reviewmemory.Blocked, reviewmemory.Failed} {
		t.Run(string(status), func(t *testing.T) {
			reviewer := reviewFunc(func(context.Context, review.Request, func(string)) (reviewmemory.Run, error) {
				return reviewmemory.Run{ID: "run", Outcome: reviewmemory.Outcome{Status: status}}, nil
			})
			published := 0
			publisher := publishFunc(func(context.Context, string, string) (publication.Attempt, error) {
				published++
				return publication.Attempt{}, publication.ErrUncertain
			})
			var notified []reviewmemory.Status
			notifier := notifyFunc(func(_ context.Context, run reviewmemory.Run) error {
				notified = append(notified, run.Status)
				return nil
			})
			result, err := Run(context.Background(), reviewer, publisher, notifier, review.Request{}, nil)
			wantPublished := 0
			if status == reviewmemory.Completed {
				wantPublished = 1
				if !errors.Is(err, publication.ErrUncertain) {
					t.Fatalf("publication failure lost: %v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if len(notified) != 1 || notified[0] != status || published != wantPublished || result.Notification != nil || result.Run.ID != "run" {
				t.Fatalf("notified = %v, published = %d, result = %+v", notified, published, result)
			}
		})
	}
}

func TestBlockedNotifierDoesNotDelayPublication(t *testing.T) {
	reviewer := reviewFunc(func(context.Context, review.Request, func(string)) (reviewmemory.Run, error) {
		return reviewmemory.Run{ID: "run", Outcome: reviewmemory.Outcome{Status: reviewmemory.Completed}}, nil
	})
	published := make(chan struct{})
	publisher := publishFunc(func(context.Context, string, string) (publication.Attempt, error) {
		close(published)
		return publication.Attempt{}, nil
	})
	notifyErr := errors.New("late failure")
	notifier := notifyFunc(func(context.Context, reviewmemory.Run) error {
		<-published
		return notifyErr
	})
	type outcome struct {
		result Result
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, err := Run(context.Background(), reviewer, publisher, notifier, review.Request{}, nil)
		done <- outcome{result, err}
	}()
	select {
	case got := <-done:
		if got.err != nil || !errors.Is(got.result.Notification, notifyErr) || got.result.Run.ID != "run" {
			t.Fatalf("%+v %v", got.result, got.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("publication waited for the notifier")
	}
}

func TestNotificationFailureNeverChangesRunOrError(t *testing.T) {
	recordErr := errors.New("completion recording failed")
	reviewer := reviewFunc(func(context.Context, review.Request, func(string)) (reviewmemory.Run, error) {
		return reviewmemory.Run{ID: "run", Outcome: reviewmemory.Outcome{Status: reviewmemory.Failed}}, recordErr
	})
	notifyErr := errors.New("no herdr server")
	notifier := notifyFunc(func(context.Context, reviewmemory.Run) error { return notifyErr })
	result, err := Run(context.Background(), reviewer, nil, notifier, review.Request{}, nil)
	if !errors.Is(err, recordErr) || !errors.Is(result.Notification, notifyErr) || result.Run.ID != "run" {
		t.Fatalf("%+v %v", result, err)
	}
}

func TestStoppedAndCancelledRunsNeverNotify(t *testing.T) {
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	cases := []struct {
		name string
		ctx  context.Context
		err  error
	}{
		{"stopped", context.Background(), reviewmemory.ErrStopped},
		{"wrapped stop", context.Background(), errors.Join(errors.New("record"), reviewmemory.ErrStopped)},
		{"shutdown", cancelled, context.Canceled},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reviewer := reviewFunc(func(context.Context, review.Request, func(string)) (reviewmemory.Run, error) {
				return reviewmemory.Run{ID: "run", Outcome: reviewmemory.Outcome{Status: reviewmemory.Failed, Message: tc.err.Error()}}, tc.err
			})
			notified := 0
			notifier := notifyFunc(func(context.Context, reviewmemory.Run) error {
				notified++
				return nil
			})
			result, err := Run(tc.ctx, reviewer, nil, notifier, review.Request{}, nil)
			if notified != 0 || !errors.Is(err, tc.err) || result.Notification != nil || result.Run.ID != "run" {
				t.Fatalf("notified=%d %+v %v", notified, result, err)
			}
		})
	}
}
