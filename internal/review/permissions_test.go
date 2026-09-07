package review

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	gh "github.com/cdowell09/herdr-pr-board/internal/github"
	"github.com/cdowell09/herdr-pr-board/internal/reviewmemory"
)

type revokeOnCapture struct {
	source RevisionSource
	revoke func()
}

func (s revokeOnCapture) CaptureRevision(ctx context.Context, url string) (gh.PullRequest, error) {
	pr, err := s.source.CaptureRevision(ctx, url)
	s.revoke()
	return pr, err
}

func TestReviewerRevokedDuringCaptureDoesNotClaimOrLaunch(t *testing.T) {
	s, source := testService(t, "valid")
	s.source = revokeOnCapture{source: source, revoke: func() {
		data, err := os.ReadFile(s.configPath)
		if err != nil {
			t.Fatal(err)
		}
		text, _, _ := strings.Cut(string(data), "[[repositories]]")
		if err := os.WriteFile(s.configPath, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}}
	run, err := s.Review(context.Background(), Request{URL: testPRURL}, nil)
	if err == nil || run.ID != "" {
		t.Fatalf("run=%+v err=%v", run, err)
	}
	history, err := s.History(testPRURL)
	if err != nil || len(history) != 0 {
		t.Fatalf("revocation created a claim: %+v %v", history, err)
	}
}

func TestPermissionRevocationAtExecutionBoundaryPreventsLaunch(t *testing.T) {
	s, source := testService(t, "valid")
	request := automaticRequest(t, s)
	s.source = captureHook(func(ctx context.Context, url string) (gh.PullRequest, error) {
		pr, err := source.CaptureRevision(ctx, url)
		pr.State = gh.PROpen
		return pr, err
	})
	data, err := os.ReadFile(s.configPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.configPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	run, err := s.Review(context.Background(), request, func(state string) {
		if state == "running" {
			if err := os.WriteFile(s.configPath, []byte(strings.Replace(string(data), "auto_launch = true", "auto_launch = false", 1)), 0600); err != nil {
				t.Fatal(err)
			}
		}
	})
	if err == nil || run.Status != reviewmemory.Failed || !strings.Contains(err.Error(), "automatic") {
		t.Fatalf("run=%+v err=%v", run, err)
	}
	if _, err := os.Stat(filepath.Join(s.RunDirectory(run.ID), "result.json")); !os.IsNotExist(err) {
		t.Fatal("revoked reviewer executed")
	}
}
