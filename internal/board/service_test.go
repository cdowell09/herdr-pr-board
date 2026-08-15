package board

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cdowell09/herdr-pr-board/internal/config"
	gh "github.com/cdowell09/herdr-pr-board/internal/github"
)

// fakeGitHub is a focused adapter for the GitHub seam used by board policy tests.
type fakeGitHub struct {
	rateRemaining   []int
	graphRemainings []int
	searchResult    []gh.PullRequest
	searchErr       error
	enrichRate      gh.RateResource
	enrichStates    []gh.CIState
	enrichWarnings  []string
	enrichErr       error
	rateCalls       int
	searchCalls     atomic.Int64
	enrichCalls     int

	blockSearches   chan struct{}
	searchInFlight  atomic.Int64
	searchMaxFlight atomic.Int64
}

func (f *fakeGitHub) RateLimits(_ context.Context) (gh.RateLimits, error) {
	index := min(f.rateCalls, len(f.rateRemaining)-1)
	graphRemaining := 5000
	if len(f.graphRemainings) > 0 {
		graphRemaining = f.graphRemainings[min(index, len(f.graphRemainings)-1)]
	}
	f.rateCalls++
	reset := time.Now().Add(time.Hour)
	return gh.RateLimits{
		Search:  gh.RateResource{Limit: 30, Remaining: f.rateRemaining[index], Reset: reset},
		GraphQL: gh.RateResource{Limit: 5000, Remaining: graphRemaining, Reset: reset},
	}, nil
}

func (f *fakeGitHub) SearchView(_ context.Context, _ config.View) ([]gh.PullRequest, error) {
	f.searchCalls.Add(1)
	if f.blockSearches != nil {
		now := f.searchInFlight.Add(1)
		updateMaxAtomic(&f.searchMaxFlight, now)
		<-f.blockSearches
		f.searchInFlight.Add(-1)
	}
	return f.searchResult, f.searchErr
}

// EnrichCI scripts one adapter outcome: it applies the result's CI states to
// the input PRs, then returns the scripted rate, warnings, and error.
func (f *fakeGitHub) EnrichCI(_ context.Context, prs []gh.PullRequest, _ gh.RateResource) (gh.RateResource, []string, error) {
	f.enrichCalls++
	for i := range prs {
		if i < len(f.enrichStates) {
			prs[i].CI = f.enrichStates[i]
		}
	}
	return f.enrichRate, f.enrichWarnings, f.enrichErr
}

func TestRefreshAllUpdatesSearchRateAfterRequests(t *testing.T) {
	cfg := serviceTestConfig(config.View{ID: "mine", Title: "Mine", Query: "is:open author:@me", Scope: config.ScopeGlobal})
	fake := &fakeGitHub{rateRemaining: []int{2, 1}}
	service := NewService(cfg, fake)

	snapshot := service.RefreshAll(context.Background())
	if fake.rateCalls != 2 {
		t.Fatalf("rate-limit calls = %d, want 2", fake.rateCalls)
	}
	if fake.searchCalls.Load() != 1 {
		t.Fatalf("search calls = %d, want 1", fake.searchCalls.Load())
	}
	if snapshot.Rates.Search.Remaining != 1 {
		t.Fatalf("displayed Search remaining = %d, want 1", snapshot.Rates.Search.Remaining)
	}
}

func TestRefreshOneUpdatesSearchRateAfterRequest(t *testing.T) {
	view := config.View{ID: "mine", Title: "Mine", Query: "is:open author:@me", Scope: config.ScopeGlobal}
	cfg := serviceTestConfig(view)
	fake := &fakeGitHub{rateRemaining: []int{2, 1}}
	service := NewService(cfg, fake)

	refresh := service.RefreshOne(context.Background(), view)
	if fake.rateCalls != 2 {
		t.Fatalf("rate-limit calls = %d, want 2", fake.rateCalls)
	}
	if fake.searchCalls.Load() != 1 {
		t.Fatalf("search calls = %d, want 1", fake.searchCalls.Load())
	}
	if refresh.Rates.Search.Remaining != 1 {
		t.Fatalf("displayed Search remaining = %d, want 1", refresh.Rates.Search.Remaining)
	}
}

func TestRefreshAllDoesNotExceedSearchBudget(t *testing.T) {
	cfg := serviceTestConfig(config.View{ID: "all", Title: "All", Query: "is:open", Scope: config.ScopeConfigured})
	cfg.GitHub.Scopes = []string{"repo:acme/one", "repo:acme/two", "repo:acme/three", "repo:acme/four"}
	fake := &fakeGitHub{rateRemaining: []int{3}}
	service := NewService(cfg, fake)

	snapshot := service.RefreshAll(context.Background())
	if fake.searchCalls.Load() != 0 {
		t.Fatalf("search calls = %d, want 0", fake.searchCalls.Load())
	}
	if len(snapshot.Views) != 1 || snapshot.Views[0].Err == nil {
		t.Fatalf("budget error missing: %#v", snapshot.Views)
	}
	if !strings.Contains(snapshot.Views[0].Err.Error(), "requires 4") {
		t.Fatalf("budget error = %q", snapshot.Views[0].Err)
	}
}

func TestRefreshAllBudgetsSearchPagination(t *testing.T) {
	cfg := serviceTestConfig(config.View{ID: "mine", Title: "Mine", Query: "is:open", Scope: config.ScopeGlobal})
	cfg.GitHub.LimitPerScope = 250
	fake := &fakeGitHub{rateRemaining: []int{2}}
	service := NewService(cfg, fake)

	snapshot := service.RefreshAll(context.Background())
	if fake.searchCalls.Load() != 0 {
		t.Fatalf("search calls = %d, want 0", fake.searchCalls.Load())
	}
	if !strings.Contains(snapshot.Views[0].Err.Error(), "requires 3") {
		t.Fatalf("budget error = %q", snapshot.Views[0].Err)
	}
}

func TestRefreshAllPropagatesGraphQLCapacityError(t *testing.T) {
	cfg := serviceTestConfig(config.View{ID: "mine", Title: "Mine", Query: "is:open", Scope: config.ScopeGlobal})
	fake := &fakeGitHub{
		rateRemaining: []int{30, 29},
		searchResult: []gh.PullRequest{
			{Number: 1, Title: "One", URL: "https://github.com/acme/api/pull/1", Repository: "acme/api", CI: gh.CIUnknown},
			{Number: 2, Title: "Two", URL: "https://github.com/acme/api/pull/2", Repository: "acme/api", CI: gh.CIUnknown},
		},
		enrichErr: errors.New("GraphQL rate limit has 1 points remaining but CI refresh needs at least 2; CI status is stale"),
	}
	service := NewService(cfg, fake)

	snapshot := service.RefreshAll(context.Background())
	if !strings.Contains(snapshot.Warning, "needs at least 2") {
		t.Fatalf("warning = %q", snapshot.Warning)
	}
}

func TestRefreshAllUpdatesRatesAfterGraphQLError(t *testing.T) {
	cfg := serviceTestConfig(config.View{ID: "mine", Title: "Mine", Query: "is:open", Scope: config.ScopeGlobal})
	fake := &fakeGitHub{
		rateRemaining:   []int{30, 29, 29},
		graphRemainings: []int{5, 5, 4},
		searchResult: []gh.PullRequest{
			{Number: 1, Title: "One", URL: "https://github.com/acme/api/pull/1", Repository: "acme/api", CI: gh.CIUnknown},
		},
		enrichErr: errors.New("unexpected GraphQL request"),
	}
	service := NewService(cfg, fake)

	snapshot := service.RefreshAll(context.Background())
	if fake.enrichCalls != 1 {
		t.Fatalf("EnrichCI calls = %d, want 1", fake.enrichCalls)
	}
	if fake.rateCalls != 3 {
		t.Fatalf("rate-limit calls = %d, want 3", fake.rateCalls)
	}
	if snapshot.Rates.GraphQL.Remaining != 4 {
		t.Fatalf("displayed GraphQL remaining = %d, want 4", snapshot.Rates.GraphQL.Remaining)
	}
	if !strings.Contains(snapshot.Warning, "unexpected GraphQL request") {
		t.Fatalf("warning = %q", snapshot.Warning)
	}
	if len(snapshot.Views) != 1 || len(snapshot.Views[0].PRs) != 1 || snapshot.Views[0].PRs[0].CI != gh.CIUnknown {
		t.Fatalf("PR CI = %#v, want UNKNOWN after enrichment error", snapshot.Views[0].PRs)
	}
}

func TestRefreshAllKeepsCompletedCIOnEnrichmentError(t *testing.T) {
	cfg := serviceTestConfig(config.View{ID: "mine", Title: "Mine", Query: "is:open", Scope: config.ScopeGlobal})
	fake := &fakeGitHub{
		rateRemaining: []int{30},
		searchResult: []gh.PullRequest{
			{Number: 1, Title: "One", URL: "https://github.com/acme/api/pull/1", Repository: "acme/api", CI: gh.CIUnknown},
			{Number: 2, Title: "Two", URL: "https://github.com/acme/api/pull/2", Repository: "acme/api", CI: gh.CIUnknown},
		},
		enrichStates:   []gh.CIState{gh.CISuccess},
		enrichWarnings: []string{"CI refresh incomplete"},
		enrichErr:      errors.New("load CI checks: boom"),
	}
	service := NewService(cfg, fake)

	snapshot := service.RefreshAll(context.Background())
	if fake.enrichCalls != 1 {
		t.Fatalf("EnrichCI calls = %d, want 1", fake.enrichCalls)
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
	if !strings.Contains(snapshot.Warning, "CI refresh incomplete") {
		t.Fatalf("warning = %q, want scripted warning propagated", snapshot.Warning)
	}
	if !strings.Contains(snapshot.Warning, "load CI checks: boom") {
		t.Fatalf("warning = %q, want enrichment error propagated", snapshot.Warning)
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
	fake := &fakeGitHub{rateRemaining: []int{3}}
	service := NewService(cfg, fake)

	refresh := service.RefreshOne(context.Background(), view)
	if fake.searchCalls.Load() != 0 {
		t.Fatalf("search calls = %d, want 0", fake.searchCalls.Load())
	}
	if refresh.Data.Err == nil || !strings.Contains(refresh.Data.Err.Error(), "requires 4") {
		t.Fatalf("budget error = %q", refresh.Data.Err)
	}
}

func TestRefreshAllSearchConcurrencyAppliesAcrossViews(t *testing.T) {
	cfg := serviceTestConfig(config.View{ID: "first", Title: "First", Query: "is:open", Scope: config.ScopeGlobal})
	cfg.Views = []config.View{
		{ID: "one", Title: "One", Query: "is:open", Scope: config.ScopeGlobal},
		{ID: "two", Title: "Two", Query: "is:open", Scope: config.ScopeGlobal},
		{ID: "three", Title: "Three", Query: "is:open", Scope: config.ScopeGlobal},
		{ID: "four", Title: "Four", Query: "is:open", Scope: config.ScopeGlobal},
	}
	cfg.GitHub.MaxConcurrency = 4
	fake := &fakeGitHub{

		rateRemaining: []int{30},
		blockSearches: make(chan struct{}),
	}
	service := NewService(cfg, fake)
	done := make(chan Snapshot, 1)
	go func() { done <- service.RefreshAll(context.Background()) }()

	deadline := time.Now().Add(2 * time.Second)
	for fake.searchInFlight.Load() < 4 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	close(fake.blockSearches)
	snapshot := <-done

	if got := fake.searchMaxFlight.Load(); got != 4 {
		t.Fatalf("concurrent searches = %d, want 4 across views", got)
	}
	if fake.searchCalls.Load() != 4 {
		t.Fatalf("search calls = %d, want 4", fake.searchCalls.Load())
	}
	if len(snapshot.Views) != 4 {
		t.Fatalf("views = %d, want 4", len(snapshot.Views))
	}
	for _, view := range snapshot.Views {
		if view.Err != nil {
			t.Fatalf("view error = %v", view.Err)
		}
	}
}

func TestRefreshAllSearchConcurrencyCappedAcrossViews(t *testing.T) {
	cfg := serviceTestConfig(config.View{ID: "first", Title: "First", Query: "is:open", Scope: config.ScopeGlobal})
	cfg.Views = []config.View{
		{ID: "one", Title: "One", Query: "is:open", Scope: config.ScopeGlobal},
		{ID: "two", Title: "Two", Query: "is:open", Scope: config.ScopeGlobal},
		{ID: "three", Title: "Three", Query: "is:open", Scope: config.ScopeGlobal},
		{ID: "four", Title: "Four", Query: "is:open", Scope: config.ScopeGlobal},
	}
	cfg.GitHub.MaxConcurrency = 2
	fake := &fakeGitHub{

		rateRemaining: []int{30},
		blockSearches: make(chan struct{}),
	}
	service := NewService(cfg, fake)
	done := make(chan Snapshot, 1)
	go func() { done <- service.RefreshAll(context.Background()) }()

	deadline := time.Now().Add(2 * time.Second)
	for fake.searchInFlight.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	close(fake.blockSearches)
	snapshot := <-done

	if got := fake.searchMaxFlight.Load(); got > 2 {
		t.Fatalf("concurrent searches = %d, want at most 2 across views", got)
	}
	if got := fake.searchMaxFlight.Load(); got < 2 {
		t.Fatalf("concurrent searches = %d, want 2 to prove overlap", got)
	}
	if fake.searchCalls.Load() != 4 {
		t.Fatalf("search calls = %d, want 4", fake.searchCalls.Load())
	}
	if len(snapshot.Views) != 4 {
		t.Fatalf("views = %d, want 4", len(snapshot.Views))
	}
	for _, view := range snapshot.Views {
		if view.Err != nil {
			t.Fatalf("view error = %v", view.Err)
		}
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
