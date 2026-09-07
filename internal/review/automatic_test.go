package review

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/cdowell09/herdr-pr-board/internal/config"
	gh "github.com/cdowell09/herdr-pr-board/internal/github"
	"github.com/cdowell09/herdr-pr-board/internal/reviewmemory"
)

type captureHook func(context.Context, string) (gh.PullRequest, error)

func (f captureHook) CaptureRevision(ctx context.Context, url string) (gh.PullRequest, error) {
	return f(ctx, url)
}

func automaticRequest(t *testing.T, s *Service) Request {
	t.Helper()
	data, err := os.ReadFile(s.configPath)
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Replace(string(data), "[review]", "[review]\nauto_views = [\"all\"]", 1) + "auto_launch = true\n"
	if err := os.WriteFile(s.configPath, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadExisting(s.configPath)
	if err != nil {
		t.Fatal(err)
	}
	id := reviewmemory.Identity{Repository: "acme/repo", Number: 1, HeadOID: strings.Repeat("a", 40), BaseRefName: "main"}
	return Request{URL: testPRURL, Automatic: true, ExpectedRevision: &id, ObservedViews: cfg.Views, ObservedConfig: cfg}
}

func TestAutomaticReviewRechecksObservationAndPermissionsAfterCapture(t *testing.T) {
	for _, mode := range []string{"head", "target", "draft", "closed", "launch revoked", "view revoked", "scopes changed", "query changed"} {
		t.Run(mode, func(t *testing.T) {
			service, source := testService(t, "valid")
			request := automaticRequest(t, service)
			service.source = captureHook(func(ctx context.Context, url string) (gh.PullRequest, error) {
				pr, err := source.CaptureRevision(ctx, url)
				pr.State = "OPEN"
				switch mode {
				case "head":
					pr.HeadOID = strings.Repeat("c", 40)
				case "target":
					pr.BaseRefName = "release"
				case "draft":
					pr.Draft = true
				case "closed":
					pr.State = "CLOSED"
				default:
					data, readErr := os.ReadFile(service.configPath)
					if readErr != nil {
						return pr, readErr
					}
					old, new := "auto_launch = true", "auto_launch = false"
					if mode == "scopes changed" {
						old, new = `scopes = ["user:@me"]`, `scopes = ["org:acme"]`
					}
					if mode == "query changed" {
						old, new = `query = "is:open"`, `query = "is:open label:changed"`
					}
					if mode == "view revoked" {
						old, new = "auto_views = [\"all\"]", "auto_views = []"
					}
					if err := os.WriteFile(service.configPath, []byte(strings.Replace(string(data), old, new, 1)), 0600); err != nil {
						return pr, err
					}
				}
				return pr, err
			})
			run, err := service.Review(context.Background(), request, nil)
			if err == nil || run.ID != "" {
				t.Fatalf("review launched after %s: %+v %v", mode, run, err)
			}
			history, err := service.History(testPRURL)
			if err != nil || len(history) != 0 {
				t.Fatalf("unexpected claim %+v %v", history, err)
			}
		})
	}
}

func TestAutomaticReviewDoesNotQueueForCapacity(t *testing.T) {
	service, source := testService(t, "valid")
	request := automaticRequest(t, service)
	service.source = captureHook(func(ctx context.Context, url string) (gh.PullRequest, error) {
		pr, err := source.CaptureRevision(ctx, url)
		pr.State = "OPEN"
		return pr, err
	})
	claim, err := service.store.Claim(reviewmemory.Request{Identity: reviewmemory.Identity{Repository: "acme/repo", Number: 2, HeadOID: request.ExpectedRevision.HeadOID, BaseRefName: "main"}, BaseOID: strings.Repeat("b", 40), Reviewer: "fake"})
	if err != nil {
		t.Fatal(err)
	}
	defer claim.Close()
	start := time.Now()
	_, err = service.Review(context.Background(), request, nil)
	if !errors.Is(err, reviewmemory.ErrCapacity) || time.Since(start) > time.Second {
		t.Fatalf("automatic request queued: %v", err)
	}
}
