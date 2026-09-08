package publication

import (
	"context"
	"errors"
	gh "github.com/cdowell09/herdr-pr-board/internal/github"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cdowell09/herdr-pr-board/internal/config"
)

func TestAutomaticSelectorRevocationPreventsPostWithoutRevokingManualPermission(t *testing.T) {
	service, github, run := publicationFixture(t, `"comment"`)
	data, err := os.ReadFile(service.configPath)
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, []byte("auto_publish = \"comment\"\n")...)
	if err := os.WriteFile(service.configPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	github.onCapture = func() {
		if err := os.WriteFile(service.configPath, []byte(strings.Replace(string(data), `auto_publish = "comment"`, `auto_publish = ""`, 1)), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := service.PublishConfigured(context.Background(), testPR, run.ID); err == nil || github.posts != 0 {
		t.Fatalf("revoked automatic publication: error=%v posts=%d", err, github.posts)
	}
	github.onCapture = nil
	if _, err := service.Publish(context.Background(), testPR, run.ID, config.PublishComment); err != nil || github.posts != 1 {
		t.Fatalf("manual permission changed: error=%v posts=%d", err, github.posts)
	}
}

func setConfiguredAction(t *testing.T, s *Service, action string) {
	t.Helper()
	data, err := os.ReadFile(s.configPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.configPath, append(data, []byte("auto_publish = "+strconv.Quote(action)+"\n")...), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestConfiguredPublicationDoesNotRequireAutomaticLaunch(t *testing.T) {
	service, github, run := publicationFixture(t, `"comment"`)
	setConfiguredAction(t, service, "comment")
	cfg, err := config.LoadExisting(service.configPath)
	if err != nil {
		t.Fatal(err)
	}
	repo, _ := cfg.RepositoryFor(run.Identity.Repository)
	if repo.AutoLaunch || len(cfg.Review.AutoViews) != 0 {
		t.Fatal("fixture accidentally enabled automatic review launches")
	}
	for i := 0; i < 2; i++ {
		attempt, err := service.PublishConfigured(context.Background(), testPR, run.ID)
		if err != nil || attempt.Status != Published {
			t.Fatalf("%+v %v", attempt, err)
		}
	}
	if github.posts != 1 {
		t.Fatalf("duplicate completed-run POSTs: %d", github.posts)
	}
}

func TestConfiguredLocalOnlyAndHistoryReadsDoNotPublish(t *testing.T) {
	service, github, run := publicationFixture(t, `"comment"`)
	if attempt, err := service.PublishConfigured(context.Background(), testPR, run.ID); err != nil || attempt.Status != "" || github.posts != 0 {
		t.Fatalf("local-only posted: %+v %v", attempt, err)
	}
	setConfiguredAction(t, service, "comment")
	if _, err := service.History(testPR); err != nil {
		t.Fatal(err)
	}
	if github.posts != 0 {
		t.Fatal("configuration or history published historical run")
	}
}

func TestConfiguredUncertainResponseNeverRepeatsPost(t *testing.T) {
	service, github, run := publicationFixture(t, `"comment"`)
	setConfiguredAction(t, service, "comment")
	github.postErr = errors.New("connection lost")
	for i := 0; i < 2; i++ {
		if _, err := service.PublishConfigured(context.Background(), testPR, run.ID); !errors.Is(err, ErrUncertain) {
			t.Fatalf("lost uncertainty: %v", err)
		}
	}
	if github.posts != 1 {
		t.Fatal("uncertain response repeated POST")
	}
}

type cancelledPost struct {
	GitHub
	entered, release chan struct{}
}

func (g cancelledPost) PublishReview(ctx context.Context, _, _, _, _ string) (gh.PublishedReview, error) {
	close(g.entered)
	<-ctx.Done()
	<-g.release
	return gh.PublishedReview{}, ctx.Err()
}

func TestWaitDrainsCancelledPublicationAndPreservesUncertainRecord(t *testing.T) {
	service, _, run := publicationFixture(t, `"comment"`)
	setConfiguredAction(t, service, "comment")
	transport := cancelledPost{GitHub: service.github, entered: make(chan struct{}), release: make(chan struct{})}
	service.github = transport
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	posted := make(chan error, 1)
	go func() { _, err := service.PublishConfigured(ctx, testPR, run.ID); posted <- err }()
	<-transport.entered
	waited := make(chan struct{})
	go func() { service.Wait(); close(waited) }()
	cancel()
	select {
	case <-waited:
		t.Fatal("shutdown skipped active publication")
	case <-time.After(20 * time.Millisecond):
	}
	close(transport.release)
	<-waited
	if err := <-posted; !errors.Is(err, ErrUncertain) {
		t.Fatalf("cancelled POST lost uncertainty: %v", err)
	}
	history, err := service.History(testPR)
	if err != nil || len(history) != 1 || history[0].Status != Uncertain {
		t.Fatalf("history=%+v %v", history, err)
	}
	if _, err := service.PublishConfigured(context.Background(), testPR, run.ID); err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("late publication accepted: %v", err)
	}
}
