package dispatch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/cdowell09/herdr-pr-board/internal/config"
	"github.com/cdowell09/herdr-pr-board/internal/discovery"
	"github.com/cdowell09/herdr-pr-board/internal/localstate"
	"github.com/cdowell09/herdr-pr-board/internal/monitor"
	"github.com/cdowell09/herdr-pr-board/internal/review"
	"github.com/cdowell09/herdr-pr-board/internal/reviewercontract"
	"github.com/cdowell09/herdr-pr-board/internal/reviewmemory"
)

type stopMonitorLoader struct {
	discovery.Loader
	cfg config.Config
}

func (l stopMonitorLoader) RefreshAll(context.Context) discovery.Snapshot {
	return snapshotWithPRs(l.cfg, 1)
}

func TestStopMonitorReviewerProcess(t *testing.T) {
	if os.Getenv("PR_BOARD_STOP_MONITOR") == "" {
		return
	}
	var input reviewercontract.Input
	if err := json.NewDecoder(os.Stdin).Decode(&input); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(input.ResultPath+".ready", nil, 0600); err != nil {
		t.Fatal(err)
	}
	time.Sleep(30 * time.Second)
}

func TestStoppedMonitorProcess(t *testing.T) {
	dir := os.Getenv("PR_BOARD_STOP_MONITOR")
	if dir == "" {
		return
	}
	path := filepath.Join(dir, "config.toml")
	cfg, err := config.LoadExisting(path)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := snapshotWithPRs(cfg, 1)
	reviews, err := review.New(dir, path, capturedPR{snapshot.Views[0].PRs[0]})
	if err != nil {
		t.Fatal(err)
	}
	defer reviews.Wait()
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM)
	defer cancel()
	source := monitor.New(dir, cfg, stopMonitorLoader{cfg: cfg})
	publisher := &fakePublisher{}
	err = New(path, reviews, publisher).Run(ctx, cfg, func(ctx context.Context, report func(discovery.Snapshot)) error {
		return source.Run(ctx, report, nil)
	}, func(event Event) {
		if event.Run == nil {
			return
		}
		if event.Run.Status != reviewmemory.Failed || event.Run.Message != reviewmemory.ErrStopped.Error() || len(publisher.runs) != 0 {
			t.Errorf("stopped monitor review published or succeeded: %+v", event)
		}
		fresh := snapshotWithPRs(cfg, 1)
		decision := Decisions(Candidates(fresh, cfg.Views, cfg.Review.AutoViews), cfg, reviews)[0]
		if decision.Eligible || decision.Reason != reviewmemory.ErrRetryRequired.Error() {
			t.Errorf("stopped revision can restart automatically: %+v", decision)
		}
		if err := localstate.AtomicWrite(filepath.Join(dir, "stopped"), []byte(event.Run.ID)); err != nil {
			t.Error(err)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestBoardStopsReviewOwnedBySeparateMonitor(t *testing.T) {
	dir := t.TempDir()
	path, cfg := dispatchConfig(t, dir, []string{os.Args[0], "-test.run=^TestStopMonitorReviewerProcess$"}, true)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, bytes.Replace(data, []byte(`timeout = "5s"`), []byte(`timeout = "30s"`), 1), 0600); err != nil {
		t.Fatal(err)
	}
	controller, err := review.New(dir, path, nil)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestStoppedMonitorProcess$")
	cmd.Env = append(os.Environ(), "PR_BOARD_STOP_MONITOR="+dir)
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	defer func() {
		_ = cmd.Process.Signal(syscall.SIGTERM)
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("monitor failed: %v\n%s", err, output.String())
			}
		case <-time.After(5 * time.Second):
			_ = cmd.Process.Kill()
			<-done
			t.Error("monitor did not stop during test cleanup")
		}
	}()
	deadline := time.Now().Add(12 * time.Second)
	url := "https://github.com/owner/repo/pull/42"
	var target reviewmemory.Run
	for {
		history, err := controller.History(url)
		if err != nil {
			t.Fatal(err)
		}
		if len(history) == 1 {
			target = history[0]
			if _, err := os.Stat(filepath.Join(controller.RunDirectory(target.ID), "result.json.ready")); err == nil {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("monitor reviewer did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := controller.Stop(target.ID); err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "stopped")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("monitor did not finish stopped review")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if status := monitor.Inspect(dir, cfg); status.State != monitor.Running {
		t.Fatalf("stopping one review stopped the monitor: %+v", status)
	}
	if err := controller.ReviewCapacity(); err != nil {
		t.Fatalf("monitor's slot remained occupied: %v", err)
	}
	if err := controller.ReviewStatus(target.Identity); !errors.Is(err, reviewmemory.ErrRetryRequired) {
		t.Fatalf("stopped monitor review did not require retry: %v", err)
	}
}
