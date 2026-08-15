package board

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cdowell09/herdr-pr-board/internal/config"
	gh "github.com/cdowell09/herdr-pr-board/internal/github"
)

type serviceRunner struct {
	rateRemaining   []int
	graphRemainings []int
	searchOutput    string
	graphqlOutputs  []string
	rateCalls       int
	searchCalls     atomic.Int64
	graphqlCalls    int

	blockSearches   chan struct{}
	searchStarted   atomic.Int64
	searchInFlight  atomic.Int64
	searchMaxFlight atomic.Int64
}

func (r *serviceRunner) Run(_ context.Context, args ...string) ([]byte, error) {
	if len(args) >= 2 && args[0] == "api" && args[1] == "rate_limit" {
		index := min(r.rateCalls, len(r.rateRemaining)-1)
		remaining := r.rateRemaining[index]
		graphRemaining := 5000
		if len(r.graphRemainings) > 0 {
			graphRemaining = r.graphRemainings[min(index, len(r.graphRemainings)-1)]
		}
		r.rateCalls++
		reset := time.Now().Add(time.Hour).Unix()
		return fmt.Appendf(nil, `{"resources":{"search":{"limit":30,"remaining":%d,"reset":%d},"graphql":{"limit":5000,"remaining":%d,"reset":%d}}}`, remaining, reset, graphRemaining, reset), nil
	}
	if len(args) >= 2 && args[0] == "search" && args[1] == "prs" {
		r.searchCalls.Add(1)
		if r.blockSearches != nil {
			now := r.searchInFlight.Add(1)
			updateMaxAtomic(&r.searchMaxFlight, now)
			r.searchStarted.Add(1)
			<-r.blockSearches
			r.searchInFlight.Add(-1)
		}
		if r.searchOutput != "" {
			return []byte(r.searchOutput), nil
		}
		return []byte("[]"), nil
	}
	if len(args) >= 2 && args[0] == "api" && args[1] == "graphql" {
		index := r.graphqlCalls
		r.graphqlCalls++
		if index < len(r.graphqlOutputs) {
			return []byte(r.graphqlOutputs[index]), nil
		}
		return nil, errors.New("unexpected GraphQL request")
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
	if runner.searchCalls.Load() != 1 {
		t.Fatalf("search calls = %d, want 1", runner.searchCalls.Load())
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
	if runner.searchCalls.Load() != 1 {
		t.Fatalf("search calls = %d, want 1", runner.searchCalls.Load())
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
	if runner.searchCalls.Load() != 0 {
		t.Fatalf("search calls = %d, want 0", runner.searchCalls.Load())
	}
	if len(snapshot.Views) != 1 || snapshot.Views[0].Err == nil {
		t.Fatalf("budget error missing: %#v", snapshot.Views)
	}
	if !strings.Contains(snapshot.Views[0].Err.Error(), "requires 4") {
		t.Fatalf("budget error = %q", snapshot.Views[0].Err)
	}
	if snapshot.capacityErr == nil {
		t.Fatal("capacity error was not marked on the snapshot")
	}
}

func TestRefreshAllBudgetsSearchPagination(t *testing.T) {
	cfg := serviceTestConfig(config.View{ID: "mine", Title: "Mine", Query: "is:open", Scope: config.ScopeGlobal})
	cfg.GitHub.LimitPerScope = 250
	runner := &serviceRunner{rateRemaining: []int{2}}
	service := NewService(cfg, gh.NewClient(runner, cfg.GitHub))

	snapshot := service.RefreshAll(context.Background())
	if runner.searchCalls.Load() != 0 {
		t.Fatalf("search calls = %d, want 0", runner.searchCalls.Load())
	}
	if !strings.Contains(snapshot.Views[0].Err.Error(), "requires 3") {
		t.Fatalf("budget error = %q", snapshot.Views[0].Err)
	}
}

func TestRefreshAllDoesNotExceedGraphQLBudget(t *testing.T) {
	cfg := serviceTestConfig(config.View{ID: "mine", Title: "Mine", Query: "is:open", Scope: config.ScopeGlobal})
	cfg.GitHub.CIBatchSize = 1
	runner := &serviceRunner{
		rateRemaining:   []int{30, 29},
		graphRemainings: []int{1, 1},
		searchOutput:    `[{"number":1,"title":"One","url":"https://github.com/acme/api/pull/1","repository":{"nameWithOwner":"acme/api"}},{"number":2,"title":"Two","url":"https://github.com/acme/api/pull/2","repository":{"nameWithOwner":"acme/api"}}]`,
	}
	service := NewService(cfg, gh.NewClient(runner, cfg.GitHub))

	snapshot := service.RefreshAll(context.Background())
	if runner.graphqlCalls != 0 {
		t.Fatalf("GraphQL calls = %d, want 0", runner.graphqlCalls)
	}
	if !strings.Contains(snapshot.Warning, "needs at least 2") {
		t.Fatalf("warning = %q", snapshot.Warning)
	}
}

func TestRefreshAllUpdatesRatesAfterGraphQLError(t *testing.T) {
	cfg := serviceTestConfig(config.View{ID: "mine", Title: "Mine", Query: "is:open", Scope: config.ScopeGlobal})
	runner := &serviceRunner{
		rateRemaining:   []int{30, 29, 29},
		graphRemainings: []int{5, 5, 4},
		searchOutput:    `[{"number":1,"title":"One","url":"https://github.com/acme/api/pull/1","repository":{"nameWithOwner":"acme/api"}}]`,
	}
	service := NewService(cfg, gh.NewClient(runner, cfg.GitHub))

	snapshot := service.RefreshAll(context.Background())
	if runner.graphqlCalls != 1 {
		t.Fatalf("GraphQL calls = %d, want 1", runner.graphqlCalls)
	}
	if runner.rateCalls != 3 {
		t.Fatalf("rate-limit calls = %d, want 3", runner.rateCalls)
	}
	if snapshot.Rates.GraphQL.Remaining != 4 {
		t.Fatalf("displayed GraphQL remaining = %d, want 4", snapshot.Rates.GraphQL.Remaining)
	}
	if !strings.Contains(snapshot.Warning, "unexpected GraphQL request") {
		t.Fatalf("warning = %q", snapshot.Warning)
	}
	if len(snapshot.Views) != 1 || len(snapshot.Views[0].PRs) != 1 || snapshot.Views[0].PRs[0].CI != gh.CIUnknown {
		t.Fatalf("PR CI = %#v, want UNKNOWN after failed batch", snapshot.Views[0].PRs)
	}
}

func TestRefreshAllKeepsCompletedCIBatchesOnGraphQLFailure(t *testing.T) {
	cfg := serviceTestConfig(config.View{ID: "mine", Title: "Mine", Query: "is:open", Scope: config.ScopeGlobal})
	cfg.GitHub.CIBatchSize = 1
	runner := &serviceRunner{
		rateRemaining:   []int{30, 29, 29},
		graphRemainings: []int{5, 5, 4},
		searchOutput:    `[{"number":1,"title":"One","url":"https://github.com/acme/api/pull/1","repository":{"nameWithOwner":"acme/api"}},{"number":2,"title":"Two","url":"https://github.com/acme/api/pull/2","repository":{"nameWithOwner":"acme/api"}}]`,
		graphqlOutputs: []string{
			`{"data":{"rateLimit":{"limit":5000,"remaining":4,"resetAt":"2026-08-07T13:00:00Z","cost":1},"p0":{"pullRequest":{"commits":{"nodes":[{"commit":{"statusCheckRollup":{"state":"SUCCESS"}}}]}}}}}`,
		},
	}
	service := NewService(cfg, gh.NewClient(runner, cfg.GitHub))

	snapshot := service.RefreshAll(context.Background())
	if runner.graphqlCalls != 2 {
		t.Fatalf("GraphQL calls = %d, want 2", runner.graphqlCalls)
	}
	if len(snapshot.Views) != 1 || len(snapshot.Views[0].PRs) != 2 {
		t.Fatalf("views = %#v", snapshot.Views)
	}
	successes, unknowns := 0, 0
	for _, pr := range snapshot.Views[0].PRs {
		switch pr.CI {
		case gh.CISuccess:
			successes++
		case gh.CIUnknown:
			unknowns++
		}
	}
	if successes != 1 || unknowns != 1 {
		t.Fatalf("CI states = %#v, want one SUCCESS and one UNKNOWN", snapshot.Views[0].PRs)
	}
	if !strings.Contains(snapshot.Warning, "unexpected GraphQL request") {
		t.Fatalf("warning = %q", snapshot.Warning)
	}
}

func updateMaxAtomic(value *atomic.Int64, candidate int64) {
	for {
		current := value.Load()
		if candidate <= current || value.CompareAndSwap(current, candidate) {
			return
		}
	}
}

func TestRefreshOneDoesNotExceedSearchBudget(t *testing.T) {
	view := config.View{ID: "all", Title: "All", Query: "is:open", Scope: config.ScopeConfigured}
	cfg := serviceTestConfig(view)
	cfg.GitHub.Scopes = []string{"repo:acme/one", "repo:acme/two", "repo:acme/three", "repo:acme/four"}
	runner := &serviceRunner{rateRemaining: []int{3}}
	service := NewService(cfg, gh.NewClient(runner, cfg.GitHub))

	refresh := service.RefreshOne(context.Background(), view)
	if runner.searchCalls.Load() != 0 {
		t.Fatalf("search calls = %d, want 0", runner.searchCalls.Load())
	}
	if refresh.Data.Err == nil || !strings.Contains(refresh.Data.Err.Error(), "requires 4") {
		t.Fatalf("budget error = %q", refresh.Data.Err)
	}
}

func TestRefreshAllSearchConcurrencyAppliesAcrossScopes(t *testing.T) {
	cfg := serviceTestConfig(config.View{ID: "all", Title: "All", Query: "is:open", Scope: config.ScopeConfigured})
	cfg.GitHub.Scopes = []string{"repo:acme/one", "repo:acme/two", "repo:acme/three", "repo:acme/four"}
	cfg.GitHub.MaxConcurrency = 4
	runner := &serviceRunner{
		rateRemaining: []int{30},
		blockSearches: make(chan struct{}),
	}
	service := NewService(cfg, gh.NewClient(runner, cfg.GitHub))
	done := make(chan Snapshot, 1)
	go func() { done <- service.RefreshAll(context.Background()) }()

	deadline := time.Now().Add(2 * time.Second)
	for runner.searchInFlight.Load() < 4 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	close(runner.blockSearches)
	snapshot := <-done

	if got := runner.searchMaxFlight.Load(); got != 4 {
		t.Fatalf("concurrent searches = %d, want 4 across scopes", got)
	}
	if runner.searchCalls.Load() != 4 {
		t.Fatalf("search calls = %d, want 4", runner.searchCalls.Load())
	}
	if len(snapshot.Views) != 1 || snapshot.Views[0].Err != nil {
		t.Fatalf("views = %#v", snapshot.Views)
	}
}

func TestRefreshAllSearchConcurrencyCappedAcrossViewsAndScopes(t *testing.T) {
	cfg := serviceTestConfig(config.View{ID: "first", Title: "First", Query: "is:open", Scope: config.ScopeConfigured})
	cfg.Views = append(cfg.Views, config.View{ID: "second", Title: "Second", Query: "is:open", Scope: config.ScopeConfigured})
	cfg.GitHub.Scopes = []string{"repo:acme/one", "repo:acme/two"}
	cfg.GitHub.MaxConcurrency = 2
	runner := &serviceRunner{
		rateRemaining: []int{30},
		blockSearches: make(chan struct{}),
	}
	service := NewService(cfg, gh.NewClient(runner, cfg.GitHub))
	done := make(chan Snapshot, 1)
	go func() { done <- service.RefreshAll(context.Background()) }()

	deadline := time.Now().Add(2 * time.Second)
	for runner.searchInFlight.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	close(runner.blockSearches)
	snapshot := <-done

	if got := runner.searchMaxFlight.Load(); got > 2 {
		t.Fatalf("concurrent searches = %d, want at most 2 across views and scopes", got)
	}
	if got := runner.searchMaxFlight.Load(); got < 2 {
		t.Fatalf("concurrent searches = %d, want 2 to prove overlap", got)
	}
	if runner.searchCalls.Load() != 4 {
		t.Fatalf("search calls = %d, want 4", runner.searchCalls.Load())
	}
	if len(snapshot.Views) != 2 || snapshot.Views[0].Err != nil || snapshot.Views[1].Err != nil {
		t.Fatalf("views = %#v", snapshot.Views)
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
