package board

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/cdowell09/herdr-pr-board/internal/config"
	gh "github.com/cdowell09/herdr-pr-board/internal/github"
)

type serviceRunner struct {
	rateRemaining []int
	rateCalls     int
	searchCalls   int
}

func (r *serviceRunner) Run(_ context.Context, args ...string) ([]byte, error) {
	if len(args) >= 2 && args[0] == "api" && args[1] == "rate_limit" {
		remaining := r.rateRemaining[min(r.rateCalls, len(r.rateRemaining)-1)]
		r.rateCalls++
		reset := time.Now().Add(time.Hour).Unix()
		return fmt.Appendf(nil, `{"resources":{"search":{"limit":30,"remaining":%d,"reset":%d},"graphql":{"limit":5000,"remaining":5000,"reset":%d}}}`, remaining, reset, reset), nil
	}
	if len(args) >= 2 && args[0] == "search" && args[1] == "prs" {
		r.searchCalls++
		return []byte("[]"), nil
	}
	return nil, fmt.Errorf("unexpected command: %v", args)
}

func TestRefreshAllUpdatesSearchRateAfterRequests(t *testing.T) {
	cfg := serviceTestConfig(config.View{ID: "mine", Title: "Mine", Query: "is:open author:@me", Scope: config.ScopeGlobal})
	runner := &serviceRunner{rateRemaining: []int{2, 1}}
	service := NewService(cfg, gh.NewClient(runner, cfg.GitHub))

	snapshot := service.RefreshAll(context.Background())
	if runner.rateCalls != 2 {
		t.Fatalf("rate-limit calls = %d, want 2", runner.rateCalls)
	}
	if runner.searchCalls != 1 {
		t.Fatalf("search calls = %d, want 1", runner.searchCalls)
	}
	if snapshot.Rates.Search.Remaining != 1 {
		t.Fatalf("displayed Search remaining = %d, want 1", snapshot.Rates.Search.Remaining)
	}
}

func TestRefreshOneUpdatesSearchRateAfterRequest(t *testing.T) {
	view := config.View{ID: "mine", Title: "Mine", Query: "is:open author:@me", Scope: config.ScopeGlobal}
	cfg := serviceTestConfig(view)
	runner := &serviceRunner{rateRemaining: []int{2, 1}}
	service := NewService(cfg, gh.NewClient(runner, cfg.GitHub))

	refresh := service.RefreshOne(context.Background(), view)
	if runner.rateCalls != 2 {
		t.Fatalf("rate-limit calls = %d, want 2", runner.rateCalls)
	}
	if runner.searchCalls != 1 {
		t.Fatalf("search calls = %d, want 1", runner.searchCalls)
	}
	if refresh.Rates.Search.Remaining != 1 {
		t.Fatalf("displayed Search remaining = %d, want 1", refresh.Rates.Search.Remaining)
	}
}

func TestRefreshAllDoesNotExceedSearchBudget(t *testing.T) {
	cfg := serviceTestConfig(config.View{ID: "all", Title: "All", Query: "is:open", Scope: config.ScopeConfigured})
	cfg.GitHub.Scopes = []string{"repo:acme/one", "repo:acme/two", "repo:acme/three", "repo:acme/four"}
	runner := &serviceRunner{rateRemaining: []int{3}}
	service := NewService(cfg, gh.NewClient(runner, cfg.GitHub))

	snapshot := service.RefreshAll(context.Background())
	if runner.searchCalls != 0 {
		t.Fatalf("search calls = %d, want 0", runner.searchCalls)
	}
	if len(snapshot.Views) != 1 || snapshot.Views[0].Err == nil {
		t.Fatalf("budget error missing: %#v", snapshot.Views)
	}
	if !strings.Contains(snapshot.Views[0].Err.Error(), "requires 4") {
		t.Fatalf("budget error = %q", snapshot.Views[0].Err)
	}
}

func serviceTestConfig(view config.View) config.Config {
	return config.Config{
		GitHub: config.GitHubConfig{
			RefreshInterval: "5m",
			LimitPerScope:   100,
			MaxConcurrency:  1,
			CIBatchSize:     25,
		},
		Views: []config.View{view},
	}
}
