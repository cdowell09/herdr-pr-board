package monitor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/cdowell09/herdr-pr-board/internal/config"
	"github.com/cdowell09/herdr-pr-board/internal/discovery"
	gh "github.com/cdowell09/herdr-pr-board/internal/github"
	"github.com/cdowell09/herdr-pr-board/internal/localstate"
)

type fakeLoader struct {
	calls    int
	snapshot discovery.Snapshot
}

func (f *fakeLoader) RefreshAll(context.Context) discovery.Snapshot { f.calls++; return f.snapshot }
func (f *fakeLoader) RefreshOne(context.Context, config.View) discovery.ViewSnapshot {
	f.calls++
	return discovery.ViewSnapshot{}
}
func (f *fakeLoader) Reconfigured(config.Config) discovery.Loader { return f }

func testSource(t *testing.T) (*Source, *fakeLoader) {
	t.Helper()
	cfg, err := config.Load(filepath.Join(t.TempDir(), "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	f := &fakeLoader{snapshot: discovery.Snapshot{StartedAt: now, FinishedAt: now.Add(time.Second)}}
	for _, v := range cfg.Views {
		f.snapshot.Views = append(f.snapshot.Views, discovery.ViewData{View: v, UpdatedAt: now, ObservedAt: now})
	}
	return New(t.TempDir(), cfg, f), f
}

func TestObservationOwnershipFallbackAndFreshScans(t *testing.T) {
	s, f := testSource(t)
	owner, err := localstate.TryLock(filepath.Join(s.dir, "monitor.lock"))
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	if err := s.write(f.snapshot); err != nil {
		t.Fatal(err)
	}
	if result := s.Observe(context.Background()); result == nil || f.calls != 0 {
		t.Fatalf("result=%+v calls=%d", result, f.calls)
	}
	if result := s.Observe(context.Background()); result != nil {
		t.Fatal("replayed unchanged observation")
	}
	s.RefreshAll(context.Background())
	s.RefreshOne(context.Background(), s.cfg.Views[0])
	if f.calls != 2 {
		t.Fatal("explicit scans reused storage")
	}
	owner.Close()
	if s.Observe(context.Background()) == nil || f.calls != 3 {
		t.Fatal("no fallback scan after owner stopped")
	}
	if s.Observe(context.Background()) != nil || f.calls != 3 {
		t.Fatal("fallback ignored configured interval")
	}
}

func TestSnapshotRoundTripPreservesFailureAndRateEvidence(t *testing.T) {
	s, f := testSource(t)
	f.snapshot.Rates.Search = gh.RateResource{Limit: 30, Remaining: 4, Reset: time.Now().UTC(), Cost: 3}
	f.snapshot.Rates.GraphQL = gh.RateResource{Limit: 5000, Remaining: 4, Reset: time.Now().UTC(), Cost: 9}
	f.snapshot.Views[0].Err = errors.New("partial search")
	f.snapshot.Views[0].UpdatedAt = time.Time{}
	f.snapshot.Views[0].PRs = []gh.PullRequest{{URL: "https://github.com/a/b/pull/1", HeadOID: "abc", MetadataObservedAt: time.Now().UTC()}}
	f.snapshot.CapacityErr = errors.New("capacity")
	f.snapshot.Errors = []discovery.RetrievalError{{Stage: "search", ViewID: s.cfg.Views[0].ID, Err: errors.New("partial search")}}
	if err := s.write(f.snapshot); err != nil {
		t.Fatal(err)
	}
	got, err := s.read()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, f.snapshot) {
		t.Fatalf("roundtrip changed evidence:\n%+v\n%+v", got, f.snapshot)
	}
	s.cfg.UI.Title = "Different title"
	if _, err := s.read(); err != nil {
		t.Fatalf("presentation invalidated discovery: %v", err)
	}
	s.cfg.GitHub.Scopes = []string{"org:other"}
	if _, err := s.read(); err == nil {
		t.Fatal("accepted incompatible configuration")
	}
}

func TestMissingOrInvalidSnapshotNeverScansUnderOwner(t *testing.T) {
	s, f := testSource(t)
	owner, err := localstate.TryLock(filepath.Join(s.dir, "monitor.lock"))
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	for _, data := range []string{"", "{", "{}"} {
		if data != "" {
			if err := os.WriteFile(filepath.Join(s.dir, "monitor-snapshot.json"), []byte(data), 0600); err != nil {
				t.Fatal(err)
			}
		}
		got := s.Observe(context.Background())
		if got == nil || len(got.Errors) == 0 || f.calls != 0 {
			t.Fatalf("got=%+v calls=%d", got, f.calls)
		}
	}
}

func TestScanLockCancellationReturnsStructuredFailure(t *testing.T) {
	s, f := testSource(t)
	lock, err := localstate.TryLock(filepath.Join(s.dir, "scan.lock"))
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got := s.RefreshAll(ctx)
	if len(got.Errors) != 1 || !errors.Is(got.Errors[0].Err, context.Canceled) || f.calls != 0 {
		t.Fatalf("got=%+v calls=%d", got, f.calls)
	}
}

func TestConcurrentReadersSeeCompleteSnapshots(t *testing.T) {
	s, f := testSource(t)
	if err := s.write(f.snapshot); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		for range 40 {
			if err := s.write(f.snapshot); err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()
	for {
		if got, err := s.read(); err != nil || !got.FinishedAt.Equal(f.snapshot.FinishedAt) {
			t.Fatalf("torn snapshot: %+v %v", got, err)
		}
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
			return
		default:
		}
	}
}
