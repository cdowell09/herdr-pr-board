package review

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	gh "github.com/cdowell09/herdr-pr-board/internal/github"
	"github.com/cdowell09/herdr-pr-board/internal/reviewercontract"
	"github.com/cdowell09/herdr-pr-board/internal/reviewmemory"
	"golang.org/x/sys/unix"
)

func TestReviewerProcess(t *testing.T) {
	mode := os.Getenv("REVIEW_TEST_MODE")
	if mode == "" {
		return
	}
	var in reviewercontract.Input
	if err := json.NewDecoder(os.Stdin).Decode(&in); err != nil {
		t.Fatal(err)
	}
	if os.Args[len(os.Args)-1] != "argument with spaces; $(no shell)" {
		t.Fatal("reviewer arguments changed")
	}
	if os.Getenv("HERDR_REVIEW_CLAIM_FD") != "3" {
		t.Fatal("claim descriptor is missing")
	}
	if _, err := os.NewFile(3, "claim").Stat(); err != nil {
		t.Fatal(err)
	}
	if mode == "timeout" {
		time.Sleep(10 * time.Second)
		return
	}
	if mode == "fifo" {
		if err := unix.Mkfifo(in.ResultPath, 0600); err != nil {
			t.Fatal(err)
		}
		return
	}
	if mode == "missing" {
		return
	}
	result := reviewercontract.Result{Version: 1, Identity: in.Identity, BaseOID: in.BaseOID, Outcome: reviewmemory.Outcome{Status: reviewmemory.Completed, Message: "Reviewed captured code", Findings: []reviewmemory.Finding{}}}
	switch mode {
	case "mismatch":
		result.Identity.HeadOID = strings.Repeat("c", 40)
	case "comparison":
		result.BaseOID = strings.Repeat("c", 40)
	case "omitted":
		result.Version = 0
	case "findings":
		result.Outcome.Findings = nil
	case "blocked":
		result.Outcome = reviewmemory.Outcome{Status: reviewmemory.Blocked, Message: "Missing specification context"}
	case "failed":
		result.Outcome = reviewmemory.Outcome{Status: reviewmemory.Failed, Message: "Agent could not review"}
	}
	data, _ := json.Marshal(result)
	if mode == "malformed" {
		data = []byte("{broken")
	}
	if mode == "empty-object" {
		data = []byte("{}")
	}
	if mode == "null" {
		data = []byte("null")
	}
	if err := os.WriteFile(in.ResultPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	if mode == "nonzero" {
		os.Exit(9)
	}
}

type revisionStub struct {
	calls   atomic.Int32
	changed atomic.Bool
}

func (s *revisionStub) CaptureRevision(_ context.Context, url string) (gh.PullRequest, error) {
	s.calls.Add(1)
	repo, number, err := gh.ParsePRURL(url)
	head := strings.Repeat("a", 40)
	if s.changed.Load() {
		head = strings.Repeat("c", 40)
	}
	return gh.PullRequest{Repository: repo, Number: number, URL: url, Title: "PR $(untrusted)", HeadOID: head, BaseOID: strings.Repeat("b", 40), BaseRefName: "main"}, err
}

const testPRURL = "https://github.com/acme/repo/pull/1"

func testService(t *testing.T, mode string) (*Service, *revisionStub) {
	t.Helper()
	t.Setenv("REVIEW_TEST_MODE", mode)
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	command, _ := json.Marshal([]string{os.Args[0], "-test.run=^TestReviewerProcess$", "--", "argument with spaces; $(no shell)"})
	data := fmt.Sprintf(`[github]
scopes = ["user:@me"]
[[views]]
id = "all"
title = "All"
query = "is:open"
scope = "global"
[review]
timeout = "2s"
[[reviewers]]
id = "fake"
command = %s
[[repositories]]
name = "acme/repo"
reviewer = "fake"
`, command)
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	source := &revisionStub{}
	service, err := New(dir, path, source)
	if err != nil {
		t.Fatal(err)
	}
	return service, source
}

func TestReviewValidatesResultsAndRecordsFailures(t *testing.T) {
	for _, mode := range []string{"valid", "blocked", "failed", "malformed", "empty-object", "null", "omitted", "findings", "mismatch", "comparison", "missing", "nonzero", "fifo", "timeout"} {
		t.Run(mode, func(t *testing.T) {
			service, _ := testService(t, mode)
			started := time.Now()
			run, err := service.Review(context.Background(), Request{URL: testPRURL}, nil)
			want := reviewmemory.Failed
			if mode == "valid" {
				want = reviewmemory.Completed
			}
			if mode == "blocked" {
				want = reviewmemory.Blocked
			}
			if run.Status != want || run.ID == "" {
				t.Fatalf("run=%+v error=%v", run, err)
			}
			if mode != "valid" && mode != "blocked" && mode != "failed" && err == nil {
				t.Fatal("invalid result returned no error")
			}
			if time.Since(started) > 6*time.Second {
				t.Fatal("review did not stop promptly")
			}
			history, historyErr := service.History(testPRURL)
			if historyErr != nil || len(history) != 1 || history[0].Status != want {
				t.Fatalf("history=%+v error=%v", history, historyErr)
			}
			if _, err := service.Review(context.Background(), Request{URL: testPRURL}, nil); err == nil {
				t.Fatal("review repeated without explicit retry")
			}
		})
	}
}

func TestQueueWaitsLocallyAndCancellationDoesNotLaunch(t *testing.T) {
	s, source := testService(t, "valid")
	pr, _ := source.CaptureRevision(context.Background(), testPRURL)
	owner, err := s.store.Claim(reviewmemory.Request{Identity: reviewmemory.Identity{Repository: pr.Repository, Number: 2, HeadOID: pr.HeadOID, BaseRefName: pr.BaseRefName}, BaseOID: pr.BaseOID, Reviewer: "fake"})
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	source.calls.Store(0)
	queued := false
	ctx, cancel := context.WithTimeout(context.Background(), 1300*time.Millisecond)
	defer cancel()
	run, err := s.Review(ctx, Request{URL: testPRURL}, func(status string) { queued = status == "queued" })
	if !errors.Is(err, context.DeadlineExceeded) || run.ID != "" || !queued {
		t.Fatalf("run=%+v queued=%v err=%v", run, queued, err)
	}
	if source.calls.Load() != 1 {
		t.Fatalf("queue repeated GitHub capture %d times", source.calls.Load())
	}
	history, err := s.History(testPRURL)
	if err != nil || len(history) != 0 {
		t.Fatalf("queue started a review: %+v %v", history, err)
	}
}

func TestDuplicateClaimAndExplicitRerun(t *testing.T) {
	s, _ := testService(t, "valid")
	pr, _ := s.source.CaptureRevision(context.Background(), testPRURL)
	claim, err := s.store.Claim(reviewmemory.Request{Identity: reviewmemory.Identity{Repository: pr.Repository, Number: pr.Number, HeadOID: pr.HeadOID, BaseRefName: pr.BaseRefName}, BaseOID: pr.BaseOID, Reviewer: "fake"})
	if err != nil {
		t.Fatal(err)
	}
	defer claim.Close()
	if _, err := s.Review(context.Background(), Request{URL: testPRURL, Rerun: true}, nil); !errors.Is(err, reviewmemory.ErrActive) {
		t.Fatalf("duplicate claim: %v", err)
	}
	claim.Close()
	run, err := s.Review(context.Background(), Request{URL: testPRURL, Rerun: true}, nil)
	if err != nil || run.Status != reviewmemory.Completed {
		t.Fatalf("explicit retry: %+v %v", run, err)
	}
}

func TestQueueRecapturesRevisionBeforeLaunching(t *testing.T) {
	s, source := testService(t, "valid")
	data, err := os.ReadFile(s.configPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.configPath, []byte(strings.Replace(string(data), `timeout = "2s"`, `timeout = "10s"`, 1)), 0600); err != nil {
		t.Fatal(err)
	}
	owner, err := s.store.Claim(reviewmemory.Request{Identity: reviewmemory.Identity{Repository: "acme/repo", Number: 2, HeadOID: strings.Repeat("a", 40), BaseRefName: "main"}, BaseOID: strings.Repeat("b", 40), Reviewer: "fake"})
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	run, err := s.Review(context.Background(), Request{URL: testPRURL}, func(status string) {
		if status == "queued" {
			source.changed.Store(true)
			owner.Close()
		}
	})
	if err != nil || run.Status != reviewmemory.Completed || run.Identity.HeadOID != strings.Repeat("c", 40) {
		t.Fatalf("queued revision: %+v %v", run, err)
	}
	if source.calls.Load() != 2 {
		t.Fatalf("capture calls=%d", source.calls.Load())
	}
	s.Wait()
	if _, err := s.Review(context.Background(), Request{URL: testPRURL, Rerun: true}, nil); err == nil {
		t.Fatal("closed service accepted work")
	}
}
