package dispatch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cdowell09/herdr-pr-board/internal/config"
	"github.com/cdowell09/herdr-pr-board/internal/discovery"
	gh "github.com/cdowell09/herdr-pr-board/internal/github"
	"github.com/cdowell09/herdr-pr-board/internal/publication"
	"github.com/cdowell09/herdr-pr-board/internal/review"
	"github.com/cdowell09/herdr-pr-board/internal/reviewercontract"
	"github.com/cdowell09/herdr-pr-board/internal/reviewmemory"
)

type fakeReviews struct {
	mu       sync.Mutex
	statuses map[reviewmemory.Identity]error
	calls    []review.Request
	run      func(context.Context, review.Request) (reviewmemory.Run, error)
}

func (f *fakeReviews) ReviewStatus(id reviewmemory.Identity) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.statuses[id]
}
func (f *fakeReviews) Review(ctx context.Context, request review.Request, _ func(string)) (reviewmemory.Run, error) {
	f.mu.Lock()
	f.calls = append(f.calls, request)
	f.statuses[*request.ExpectedRevision] = reviewmemory.ErrActive
	f.mu.Unlock()
	run, err := f.run(ctx, request)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.statuses[*request.ExpectedRevision] = reviewmemory.ErrRetryRequired
	if run.Status == reviewmemory.Completed {
		f.statuses[*request.ExpectedRevision] = reviewmemory.ErrReviewed
	}
	return run, err
}

type fakePublisher struct {
	mu      sync.Mutex
	actions []config.PublicationAction
}

func (f *fakePublisher) PublishAutomatic(_ context.Context, _ string, _ string, action config.PublicationAction) (publication.Attempt, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.actions = append(f.actions, action)
	return publication.Attempt{}, nil
}

func dispatchConfig(t *testing.T, dir string, command []string, autoPublish bool) (string, config.Config) {
	t.Helper()
	args, _ := json.Marshal(command)
	publish := ""
	if autoPublish {
		publish = "publish_actions = [\"comment\"]\nauto_publish = \"comment\"\n"
	}
	data := fmt.Sprintf(`[github]
scopes = ["user:@me"]
[[views]]
id = "review"
title = "Review"
query = "is:open"
scope = "global"
[review]
auto_views = ["review"]
max_concurrency = 1
timeout = "5s"
[[reviewers]]
id = "fake"
command = %s
[[repositories]]
name = "owner/repo"
reviewer = "fake"
auto_launch = true
%s`, args, publish)
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadExisting(path)
	if err != nil {
		t.Fatal(err)
	}
	return path, cfg
}

func snapshotWithPRs(cfg config.Config, count int) discovery.Snapshot {
	snapshot := observedSnapshot()
	snapshot.Views[0].View = cfg.Views[0]
	original := snapshot.Views[0].PRs[0]
	snapshot.Views[0].PRs = nil
	for i := range count {
		pr := original
		pr.Number += i
		pr.URL = fmt.Sprintf("https://github.com/owner/repo/pull/%d", pr.Number)
		snapshot.Views[0].PRs = append(snapshot.Views[0].PRs, pr)
	}
	return snapshot
}

func TestDispatchContinuesAfterFailureAndPublishesOnlyExplicitAction(t *testing.T) {
	for _, autoPublish := range []bool{false, true} {
		t.Run(fmt.Sprint(autoPublish), func(t *testing.T) {
			path, cfg := dispatchConfig(t, t.TempDir(), []string{"unused"}, autoPublish)
			snapshot := snapshotWithPRs(cfg, 2)
			snapshot.Views = append(snapshot.Views, snapshot.Views[0])
			reviews := &fakeReviews{statuses: map[reviewmemory.Identity]error{}}
			reviews.run = func(_ context.Context, request review.Request) (reviewmemory.Run, error) {
				status := reviewmemory.Completed
				if request.ExpectedRevision.Number == 42 {
					status = reviewmemory.Failed
				}
				return reviewmemory.Run{ID: fmt.Sprint(request.ExpectedRevision.Number), Identity: *request.ExpectedRevision, Outcome: reviewmemory.Outcome{Status: status}}, nil
			}
			publisher := &fakePublisher{}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			count := 0
			err := New(path, reviews, publisher).Run(ctx, cfg, func(ctx context.Context, report func(discovery.Snapshot)) error {
				report(snapshot)
				<-ctx.Done()
				return nil
			}, func(event Event) {
				if event.Run != nil {
					count++
					if count == 2 {
						cancel()
					}
				}
			})
			if err != nil || count != 2 || len(reviews.calls) != 2 {
				t.Fatalf("count=%d calls=%v error=%v", count, reviews.calls, err)
			}
			want := 0
			if autoPublish {
				want = 1
			}
			if len(publisher.actions) != want {
				t.Fatalf("publication actions=%v", publisher.actions)
			}
			for _, request := range reviews.calls {
				if !request.Automatic || request.ExpectedRevision == nil || len(request.ObservedViews) == 0 || request.Rerun {
					t.Fatalf("request=%+v", request)
				}
			}
		})
	}
}

func TestNewFailedObservationDropsPendingWork(t *testing.T) {
	path, cfg := dispatchConfig(t, t.TempDir(), []string{"unused"}, false)
	snapshot := snapshotWithPRs(cfg, 2)
	started := make(chan struct{})
	release := make(chan struct{})
	reviews := &fakeReviews{statuses: map[reviewmemory.Identity]error{}, run: func(ctx context.Context, request review.Request) (reviewmemory.Run, error) {
		if request.ExpectedRevision.Number != 42 {
			return reviewmemory.Run{}, errors.New("stale pending PR launched")
		}
		close(started)
		select {
		case <-release:
		case <-ctx.Done():
		}
		return reviewmemory.Run{ID: "42", Identity: *request.ExpectedRevision, Outcome: reviewmemory.Outcome{Status: reviewmemory.Completed}}, nil
	}}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	completed := false
	err := New(path, reviews, nil).Run(ctx, cfg, func(ctx context.Context, report func(discovery.Snapshot)) error {
		report(snapshot)
		select {
		case <-started:
		case <-ctx.Done():
			return ctx.Err()
		}
		failed := snapshot
		failed.Errors = []discovery.RetrievalError{{Stage: "search", Err: errors.New("failed")}}
		report(failed)
		close(release)
		<-ctx.Done()
		return nil
	}, func(event Event) {
		if event.Run != nil {
			completed = true
		}
		if completed && event.Decision.Reason == ObservationFailed {
			cancel()
		}
	})
	if err != nil || len(reviews.calls) != 1 || !completed {
		t.Fatalf("calls=%v completed=%v error=%v", reviews.calls, completed, err)
	}
}

func TestDispatchReviewerProcess(t *testing.T) {
	if os.Getenv("PR_BOARD_DISPATCH_REVIEWER") != "1" {
		return
	}
	var input reviewercontract.Input
	if err := json.NewDecoder(os.Stdin).Decode(&input); err != nil {
		t.Fatal(err)
	}
	result := reviewercontract.Result{Version: 1, Identity: input.Identity, BaseOID: input.BaseOID, Outcome: reviewmemory.Outcome{Status: reviewmemory.Completed, Message: "Both review axes complete", Findings: []reviewmemory.Finding{}}}
	data, _ := json.Marshal(result)
	if err := os.WriteFile(input.ResultPath, data, 0600); err != nil {
		t.Fatal(err)
	}
}

type capturedPR struct{ pr gh.PullRequest }

func (source capturedPR) CaptureRevision(context.Context, string) (gh.PullRequest, error) {
	return source.pr, nil
}

func TestSnapshotToRealReviewExecution(t *testing.T) {
	t.Setenv("PR_BOARD_DISPATCH_REVIEWER", "1")
	dir := t.TempDir()
	path, cfg := dispatchConfig(t, dir, []string{os.Args[0], "-test.run=^TestDispatchReviewerProcess$"}, false)
	snapshot := snapshotWithPRs(cfg, 1)
	reviews, err := review.New(dir, path, capturedPR{snapshot.Views[0].PRs[0]})
	if err != nil {
		t.Fatal(err)
	}
	defer reviews.Wait()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var outcome *reviewmemory.Run
	err = New(path, reviews, nil).Run(ctx, cfg, func(ctx context.Context, report func(discovery.Snapshot)) error {
		report(snapshot)
		<-ctx.Done()
		return nil
	}, func(event Event) {
		if event.Run != nil {
			outcome = event.Run
			cancel()
		}
	})
	if err != nil || outcome == nil || outcome.Status != reviewmemory.Completed {
		t.Fatalf("outcome=%+v error=%v", outcome, err)
	}
	if err := reviews.ReviewStatus(Identity(snapshot.Views[0].PRs[0])); !errors.Is(err, reviewmemory.ErrReviewed) {
		t.Fatalf("history status=%v", err)
	}
}

func TestDispatchBoundsConcurrencyAndKeepsMonitorResponsive(t *testing.T) {
	path, cfg := dispatchConfig(t, t.TempDir(), []string{"unused"}, false)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(string(data), "max_concurrency = 1", "max_concurrency = 2", 1)), 0600); err != nil {
		t.Fatal(err)
	}
	snapshot := snapshotWithPRs(cfg, 3)
	started := make(chan struct{}, 3)
	release := make(chan struct{})
	var active, peak atomic.Int32
	reviews := &fakeReviews{statuses: map[reviewmemory.Identity]error{}, run: func(ctx context.Context, request review.Request) (reviewmemory.Run, error) {
		count := active.Add(1)
		defer active.Add(-1)
		for previous := peak.Load(); count > previous && !peak.CompareAndSwap(previous, count); previous = peak.Load() {
		}
		started <- struct{}{}
		select {
		case <-release:
		case <-ctx.Done():
		}
		return reviewmemory.Run{ID: fmt.Sprint(request.ExpectedRevision.Number), Identity: *request.ExpectedRevision, Outcome: reviewmemory.Outcome{Status: reviewmemory.Completed}}, nil
	}}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	count := 0
	err = New(path, reviews, nil).Run(ctx, cfg, func(ctx context.Context, report func(discovery.Snapshot)) error {
		report(snapshot)
		for range 2 {
			select {
			case <-started:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		report(snapshot) // The monitor can supply another observation while both reviewers run.
		close(release)
		<-ctx.Done()
		return nil
	}, func(event Event) {
		if event.Run != nil {
			count++
			if count == 3 {
				cancel()
			}
		}
	})
	if err != nil || count != 3 || len(reviews.calls) != 3 || peak.Load() != 2 {
		t.Fatalf("count=%d calls=%d peak=%d error=%v", count, len(reviews.calls), peak.Load(), err)
	}
}

func TestCancellationWaitsForMaximumActiveReviewers(t *testing.T) {
	path, cfg := dispatchConfig(t, t.TempDir(), []string{"unused"}, false)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(string(data), "max_concurrency = 1", "max_concurrency = 8", 1)), 0600); err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{}, 8)
	var active atomic.Int32
	reviews := &fakeReviews{statuses: map[reviewmemory.Identity]error{}, run: func(ctx context.Context, _ review.Request) (reviewmemory.Run, error) {
		active.Add(1)
		defer active.Add(-1)
		started <- struct{}{}
		<-ctx.Done()
		return reviewmemory.Run{}, ctx.Err()
	}}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err = New(path, reviews, nil).Run(ctx, cfg, func(ctx context.Context, report func(discovery.Snapshot)) error {
		report(snapshotWithPRs(cfg, 9))
		for range 8 {
			select {
			case <-started:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		cancel()
		return nil
	}, nil)
	if err != nil || len(reviews.calls) != 8 || active.Load() != 0 {
		t.Fatalf("calls=%d active=%d error=%v", len(reviews.calls), active.Load(), err)
	}
}
