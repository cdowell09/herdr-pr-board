package dispatch

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cdowell09/herdr-pr-board/internal/discovery"
	"github.com/cdowell09/herdr-pr-board/internal/review"
	"github.com/cdowell09/herdr-pr-board/internal/reviewmemory"
)

type fakeNotifier struct {
	mu   sync.Mutex
	runs []reviewmemory.Run
	err  error
}

func (f *fakeNotifier) Notify(_ context.Context, run reviewmemory.Run) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.runs = append(f.runs, run)
	return f.err
}

func completedReviews() *fakeReviews {
	return &fakeReviews{statuses: map[reviewmemory.Identity]error{}, run: func(_ context.Context, request review.Request) (reviewmemory.Run, error) {
		return reviewmemory.Run{ID: "run", Identity: *request.ExpectedRevision, Outcome: reviewmemory.Outcome{Status: reviewmemory.Completed}}, nil
	}}
}

func TestLaunchReportsNotificationFailureWithoutRelabelingRun(t *testing.T) {
	path, cfg := dispatchConfig(t, t.TempDir(), []string{"unused"}, false)
	snapshot := snapshotWithPRs(cfg, 1)
	candidate := Candidates(snapshot, cfg.Views)[0]
	reviews := completedReviews()
	eligible := decision(candidate, cfg, reviews)
	notifier := &fakeNotifier{err: errors.New("no herdr server")}
	event := New(path, reviews, nil, notifier).launch(context.Background(), candidate, eligible, cfg)
	if event.Run == nil || event.Run.Status != reviewmemory.Completed || !event.Decision.Eligible || event.Error != "" || !strings.Contains(event.Notification, "no herdr server") {
		t.Fatalf("notification relabeled review: %+v", event)
	}
	if len(notifier.runs) != 1 || notifier.runs[0].ID != "run" {
		t.Fatalf("notified runs = %+v", notifier.runs)
	}
}

func TestRunNotifiesEachLaunchedReview(t *testing.T) {
	path, cfg := dispatchConfig(t, t.TempDir(), []string{"unused"}, false)
	snapshot := snapshotWithPRs(cfg, 2)
	notifier := &fakeNotifier{}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	finished := 0
	err := New(path, completedReviews(), nil, notifier).Run(ctx, cfg, func(ctx context.Context, report func(discovery.Snapshot)) error {
		report(snapshot)
		<-ctx.Done()
		return nil
	}, func(event Event) {
		if event.Run != nil {
			finished++
			if finished == 2 {
				cancel()
			}
		}
		if event.Notification != "" {
			t.Errorf("unexpected notification problem: %+v", event)
		}
	})
	if err != nil || finished != 2 || len(notifier.runs) != 2 {
		t.Fatalf("finished=%d notified=%d error=%v", finished, len(notifier.runs), err)
	}
}
