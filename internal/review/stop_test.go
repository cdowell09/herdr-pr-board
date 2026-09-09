package review

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/cdowell09/herdr-pr-board/internal/cli"
	"github.com/cdowell09/herdr-pr-board/internal/config"
	"github.com/cdowell09/herdr-pr-board/internal/localstate"
	"github.com/cdowell09/herdr-pr-board/internal/reviewercontract"
	"github.com/cdowell09/herdr-pr-board/internal/reviewmemory"
	"github.com/pelletier/go-toml/v2"
)

func TestStoppableReviewerProcess(t *testing.T) {
	if os.Getenv("PR_BOARD_STOP_REVIEWER") != "1" {
		return
	}
	var input reviewercontract.Input
	if err := json.NewDecoder(os.Stdin).Decode(&input); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM)
	defer cancel()
	claim, err := cli.InheritedFile("HERDR_REVIEW_CLAIM_FD")
	if err != nil || claim == nil {
		t.Fatalf("missing claim: %v", err)
	}
	defer claim.Close()
	child := exec.Command(os.Args[0], "-test.run=^TestStoppableReviewerChild$", "--", filepath.Join(filepath.Dir(input.ResultPath), "child.pid"))
	if err := cli.PassFile(child, claim, "HERDR_REVIEW_CLAIM_FD"); err != nil {
		t.Fatal(err)
	}
	_ = cli.RunProcess(ctx, child, 100*time.Millisecond)
}

func TestStoppableReviewerChild(t *testing.T) {
	if os.Getenv("PR_BOARD_STOP_REVIEWER") != "1" {
		return
	}
	signal.Ignore(syscall.SIGTERM)
	// The parent treats this path as readiness, so publish the complete PID.
	if err := localstate.AtomicWrite(os.Args[len(os.Args)-1], []byte(strconv.Itoa(os.Getpid()))); err != nil {
		t.Fatal(err)
	}
	time.Sleep(30 * time.Second)
}

func TestStopFromAnotherBoardCancelsOnlySelectedReviewerAndChildren(t *testing.T) {
	t.Setenv("PR_BOARD_STOP_REVIEWER", "1")
	s, _ := testService(t, "valid")
	cfg, err := config.LoadExisting(s.configPath)
	if err != nil {
		t.Fatal(err)
	}
	command := cfg.Reviewers[0].Command
	cfg.Reviewers[0].Command = []string{os.Args[0], "-test.run=^TestStoppableReviewerProcess$"}
	cfg.Review.MaxConcurrency, cfg.Review.Timeout = 2, "15s"
	save := func() {
		t.Helper()
		data, err := toml.Marshal(cfg)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(s.configPath, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	save()
	controller, err := New(s.stateDir, s.configPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer func() { cancel(); s.Wait() }()
	type result struct {
		run reviewmemory.Run
		err error
	}
	urls := []string{testPRURL, "https://github.com/acme/repo/pull/2"}
	done := []chan result{make(chan result, 1), make(chan result, 1)}
	for i, url := range urls {
		go func() {
			run, err := s.Review(ctx, Request{URL: url}, nil)
			done[i] <- result{run, err}
		}()
	}
	var runs [2]reviewmemory.Run
	var childPIDs [2]int
	for i, url := range urls {
		for childPIDs[i] == 0 {
			if ctx.Err() != nil {
				t.Fatal("reviewer did not start")
			}
			history, err := controller.History(url)
			if err != nil {
				t.Fatal(err)
			}
			if len(history) > 0 {
				runs[i] = history[0]
				data, err := os.ReadFile(filepath.Join(s.RunDirectory(runs[i].ID), "child.pid"))
				if err == nil {
					childPIDs[i], err = strconv.Atoi(strings.TrimSpace(string(data)))
					if err != nil {
						t.Fatal(err)
					}
				}
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	alive := [2]func() bool{reviewChildAlive(t, childPIDs[0]), reviewChildAlive(t, childPIDs[1])}
	for i := range runs {
		if err := controller.Stop(runs[i].ID); err != nil {
			t.Fatal(err)
		}
		select {
		case got := <-done[i]:
			if got.run.Status != reviewmemory.Failed || got.run.Message != reviewmemory.ErrStopped.Error() || !errors.Is(got.err, reviewmemory.ErrStopped) {
				t.Fatalf("stop outcome: %+v %v", got.run, got.err)
			}
		case <-ctx.Done():
			t.Fatal("stop did not finish")
		}
		if alive[i]() {
			t.Fatalf("reviewer child %d survived cancellation", childPIDs[i])
		}
		if i == 0 {
			select {
			case got := <-done[1]:
				t.Fatalf("stop affected another review: %+v", got)
			default:
			}
			if !alive[1]() {
				t.Fatal("other reviewer child stopped")
			}
		}
		if err := controller.ReviewCapacity(); err != nil {
			t.Fatalf("stop did not release slot: %v", err)
		}
	}
	if _, err := s.Review(ctx, Request{URL: testPRURL}, nil); !errors.Is(err, reviewmemory.ErrRetryRequired) {
		t.Fatalf("stopped review restarted without explicit retry: %v", err)
	}
	cfg.Reviewers[0].Command = command
	save()
	run, err := s.Review(ctx, Request{URL: testPRURL, Rerun: true}, nil)
	if err != nil || run.Status != reviewmemory.Completed || run.ID == runs[0].ID {
		t.Fatalf("explicit retry failed: %+v %v", run, err)
	}
}
