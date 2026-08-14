package github

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cdowell09/herdr-pr-board/internal/config"
)

type runnerFunc func(context.Context, ...string) ([]byte, error)

func (f runnerFunc) Run(ctx context.Context, args ...string) ([]byte, error) {
	return f(ctx, args...)
}

func TestSearchSeparatesFlagsFromNegativeQueryTerms(t *testing.T) {
	var got []string
	runner := runnerFunc(func(_ context.Context, args ...string) ([]byte, error) {
		got = append([]string(nil), args...)
		return []byte("[]"), nil
	})
	client := NewClient(runner, config.GitHubConfig{LimitPerScope: 1})
	if _, err := client.search(context.Background(), "is:open -is:draft"); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"search", "prs",
		"--limit", "1",
		"--sort", "updated", "--order", "desc",
		"--json", "number,title,url,author,isDraft,updatedAt,repository",
		"--", "is:open", "-is:draft",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("arguments = %#v, want %#v", got, want)
	}
}

func TestSearchViewResolvesScopesDeduplicatesAndSorts(t *testing.T) {
	var queryMu sync.Mutex
	var queries []string
	recordQuery := func(query string) {
		queryMu.Lock()
		defer queryMu.Unlock()
		queries = append(queries, query)
	}
	runner := runnerFunc(func(_ context.Context, args ...string) ([]byte, error) {
		if len(args) >= 2 && args[0] == "api" && args[1] == "user" {
			return []byte("cdowell09\n"), nil
		}
		if len(args) >= 3 && args[0] == "search" && args[1] == "prs" {
			separator := slices.Index(args, "--")
			if separator == -1 {
				return nil, fmt.Errorf("missing query separator: %v", args)
			}
			query := strings.Join(args[separator+1:], " ")
			recordQuery(query)
			switch {
			case strings.Contains(query, "user:cdowell09"):
				return []byte(`[{"number":2,"title":"Cookies","url":"https://github.com/cdowell09/cookies/pull/2","isDraft":false,"updatedAt":"2026-08-07T10:00:00Z","author":{"login":"cdowell09"},"repository":{"nameWithOwner":"cdowell09/cookies"}}]`), nil
			case strings.Contains(query, "repo:acme/api"):
				return []byte(`[{"number":9,"title":"API","url":"https://github.com/acme/api/pull/9","isDraft":true,"updatedAt":"2026-08-07T12:00:00Z","author":{"login":"bot"},"repository":{"nameWithOwner":"acme/api"}},{"number":2,"title":"Cookies","url":"https://github.com/cdowell09/cookies/pull/2","isDraft":false,"updatedAt":"2026-08-07T10:00:00Z","author":{"login":"cdowell09"},"repository":{"nameWithOwner":"cdowell09/cookies"}}]`), nil
			}
		}
		return nil, fmt.Errorf("unexpected command: %v", args)
	})
	client := NewClient(runner, config.GitHubConfig{
		LimitPerScope: 100,
		CIBatchSize:   25,
		Scopes:        []string{"user:@me", "repo:acme/api"},
	})
	prs, err := client.SearchView(context.Background(), config.View{Title: "All", Query: "is:open", Scope: "configured"})
	if err != nil {
		t.Fatal(err)
	}
	queryMu.Lock()
	gotQueries := slices.Sorted(slices.Values(queries))
	queryMu.Unlock()
	wantQueries := []string{"is:open repo:acme/api", "is:open user:cdowell09"}
	if !slices.Equal(gotQueries, wantQueries) {
		t.Fatalf("queries = %#v, want %#v", gotQueries, wantQueries)
	}
	if len(prs) != 2 {
		t.Fatalf("PR count = %d, want 2", len(prs))
	}
	if prs[0].Repository != "acme/api" || prs[0].Number != 9 || !prs[0].Draft {
		t.Fatalf("unexpected first PR: %#v", prs[0])
	}
	if prs[1].CI != CIUnknown {
		t.Fatalf("initial CI = %q", prs[1].CI)
	}
}

func TestEnrichCIBatchesAndMapsRollups(t *testing.T) {
	calls := 0
	runner := runnerFunc(func(_ context.Context, args ...string) ([]byte, error) {
		calls++
		if !strings.Contains(args[len(args)-1], "p0: repository") {
			return nil, fmt.Errorf("missing GraphQL alias: %v", args)
		}
		if calls == 1 {
			return []byte(`{"data":{"rateLimit":{"limit":5000,"remaining":4990,"resetAt":"2026-08-07T13:00:00Z","cost":10},"p0":{"pullRequest":{"commits":{"nodes":[{"commit":{"statusCheckRollup":{"state":"SUCCESS"}}}]}}},"p1":{"pullRequest":{"commits":{"nodes":[{"commit":{"statusCheckRollup":{"state":"FAILURE"}}}]}}}}}`), nil
		}
		return []byte(`{"data":{"rateLimit":{"limit":5000,"remaining":4988,"resetAt":"2026-08-07T13:00:00Z","cost":2},"p0":{"pullRequest":{"commits":{"nodes":[{"commit":{"statusCheckRollup":null}}]}}}}}`), nil
	})
	client := NewClient(runner, config.GitHubConfig{CIBatchSize: 2})
	prs := []PullRequest{
		{Repository: "acme/one", Number: 1, URL: "https://github.com/acme/one/pull/1"},
		{Repository: "acme/two", Number: 2, URL: "https://github.com/acme/two/pull/2"},
		{Repository: "acme/three", Number: 3, URL: "https://github.com/acme/three/pull/3"},
	}
	rate, _, err := client.EnrichCI(context.Background(), prs, RateResource{})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
	if prs[0].CI != CISuccess || prs[1].CI != CIFailure || prs[2].CI != CINone {
		t.Fatalf("CI states = %q, %q, %q", prs[0].CI, prs[1].CI, prs[2].CI)
	}
	if rate.Remaining != 4988 || !rate.Reset.Equal(time.Date(2026, 8, 7, 13, 0, 0, 0, time.UTC)) {
		t.Fatalf("rate = %#v", rate)
	}
	if _, _, err := client.EnrichCI(context.Background(), prs, rate); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("cached enrichment made another request; calls = %d", calls)
	}
}

func TestEnrichCIPreservesCompletedBatchesOnFailure(t *testing.T) {
	calls := 0
	runner := runnerFunc(func(_ context.Context, args ...string) ([]byte, error) {
		calls++
		switch calls {
		case 1:
			return []byte(`{"data":{"rateLimit":{"limit":5000,"remaining":4999,"resetAt":"2026-08-07T13:00:00Z","cost":1},"p0":{"pullRequest":{"commits":{"nodes":[{"commit":{"statusCheckRollup":{"state":"SUCCESS"}}}]}}}}}`), nil
		case 2:
			return []byte(`{"data":{"rateLimit":{"limit":5000,"remaining":4998,"resetAt":"2026-08-07T13:00:00Z","cost":1},"p0":{"pullRequest":{"commits":{"nodes":[{"commit":{"statusCheckRollup":{"state":"FAILURE"}}}]}}}}}`), nil
		}
		return nil, errors.New("connection reset")
	})
	client := NewClient(runner, config.GitHubConfig{CIBatchSize: 1})
	prs := []PullRequest{
		{Repository: "acme/one", Number: 1, URL: "https://github.com/acme/one/pull/1", CI: CIUnknown},
		{Repository: "acme/two", Number: 2, URL: "https://github.com/acme/two/pull/2", CI: CIUnknown},
		{Repository: "acme/three", Number: 3, URL: "https://github.com/acme/three/pull/3", CI: CIUnknown},
	}

	rate, warnings, err := client.EnrichCI(context.Background(), prs, RateResource{Limit: 5000, Remaining: 5000, Reset: time.Now().Add(time.Hour)})
	if err == nil || !strings.Contains(err.Error(), "connection reset") {
		t.Fatalf("error = %v", err)
	}
	if calls != 3 {
		t.Fatalf("GraphQL calls = %d, want 3", calls)
	}
	if prs[0].CI != CISuccess || prs[1].CI != CIFailure || prs[2].CI != CIUnknown {
		t.Fatalf("CI states = %q, %q, %q", prs[0].CI, prs[1].CI, prs[2].CI)
	}
	if rate.Limit != 5000 || rate.Remaining != 4998 {
		t.Fatalf("rate = %#v, want latest known from successful batches", rate)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %#v", warnings)
	}

	calls = 0
	cached := []PullRequest{{Repository: "acme/one", Number: 1, URL: "https://github.com/acme/one/pull/1"}}
	if _, _, err := client.EnrichCI(context.Background(), cached, rate); err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatalf("completed batch was not cached; calls = %d", calls)
	}
	if cached[0].CI != CISuccess {
		t.Fatalf("cached CI = %q, want SUCCESS", cached[0].CI)
	}
}

func TestEnrichCISurfacesWarningsWhenResponseHasDataAndErrors(t *testing.T) {
	runner := runnerFunc(func(_ context.Context, _ ...string) ([]byte, error) {
		return []byte(`{"data":{"rateLimit":{"limit":5000,"remaining":4999,"resetAt":"2026-08-07T13:00:00Z","cost":1},"p0":{"pullRequest":{"commits":{"nodes":[{"commit":{"statusCheckRollup":{"state":"SUCCESS"}}}]}}}},"errors":[{"message":"p1 resolves to a deleted repository"}]}`), nil
	})
	client := NewClient(runner, config.GitHubConfig{CIBatchSize: 2})
	prs := []PullRequest{
		{Repository: "acme/one", Number: 1, URL: "https://github.com/acme/one/pull/1"},
		{Repository: "acme/gone", Number: 2, URL: "https://github.com/acme/gone/pull/2"},
	}

	rate, warnings, err := client.EnrichCI(context.Background(), prs, RateResource{})
	if err != nil {
		t.Fatal(err)
	}
	if prs[0].CI != CISuccess || prs[1].CI != CIUnknown {
		t.Fatalf("CI states = %q, %q", prs[0].CI, prs[1].CI)
	}
	if rate.Remaining != 4999 {
		t.Fatalf("rate = %#v", rate)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "deleted repository") {
		t.Fatalf("warnings = %#v", warnings)
	}
}

func TestEnrichCIKeepsWarningsWithCapacityError(t *testing.T) {
	calls := 0
	runner := runnerFunc(func(_ context.Context, _ ...string) ([]byte, error) {
		calls++
		return []byte(`{"data":{"rateLimit":{"limit":5000,"remaining":1,"resetAt":"2027-08-07T13:00:00Z","cost":1},"p0":{"pullRequest":{"commits":{"nodes":[{"commit":{"statusCheckRollup":{"state":"SUCCESS"}}}]}}}},"errors":[{"message":"rate limiting may interfere"}]}`), nil
	})
	client := NewClient(runner, config.GitHubConfig{CIBatchSize: 1})
	prs := []PullRequest{
		{Repository: "acme/one", Number: 1, URL: "https://github.com/acme/one/pull/1", CI: CIUnknown},
		{Repository: "acme/two", Number: 2, URL: "https://github.com/acme/two/pull/2", CI: CIUnknown},
		{Repository: "acme/three", Number: 3, URL: "https://github.com/acme/three/pull/3", CI: CIUnknown},
	}
	budget := RateResource{Limit: 5000, Remaining: 3, Reset: time.Now().Add(time.Hour)}

	rate, warnings, err := client.EnrichCI(context.Background(), prs, budget)
	if err == nil || !strings.Contains(err.Error(), "needs at least 2") {
		t.Fatalf("error = %v", err)
	}
	if calls != 1 {
		t.Fatalf("GraphQL calls = %d, want 1", calls)
	}
	if prs[0].CI != CISuccess || prs[1].CI != CIUnknown || prs[2].CI != CIUnknown {
		t.Fatalf("CI states = %q, %q, %q", prs[0].CI, prs[1].CI, prs[2].CI)
	}
	if rate.Limit != 5000 || rate.Remaining != 1 {
		t.Fatalf("rate = %#v, want latest from completed batch", rate)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "rate limiting") {
		t.Fatalf("warnings = %#v", warnings)
	}
}

func TestEnrichCICacheConcurrentAccess(t *testing.T) {
	var calls atomic.Int64
	runner := runnerFunc(func(_ context.Context, _ ...string) ([]byte, error) {
		calls.Add(1)
		return []byte(`{"data":{"rateLimit":{"limit":5000,"remaining":4999,"resetAt":"2026-08-07T13:00:00Z","cost":1},"p0":{"pullRequest":{"commits":{"nodes":[{"commit":{"statusCheckRollup":{"state":"SUCCESS"}}}]}}}}}`), nil
	})
	client := NewClient(runner, config.GitHubConfig{CIBatchSize: 1})
	results := make([]CIState, 8)
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			prs := []PullRequest{{Repository: "acme/one", Number: 1, URL: "https://github.com/acme/one/pull/1", CI: CIUnknown}}
			if _, _, err := client.EnrichCI(context.Background(), prs, RateResource{}); err != nil {
				t.Error(err)
				return
			}
			results[i] = prs[0].CI
		}()
	}
	wg.Wait()
	for i, state := range results {
		if state != CISuccess {
			t.Fatalf("call %d CI = %q, want SUCCESS", i, state)
		}
	}
	if total := calls.Load(); total < 1 || total > 8 {
		t.Fatalf("runner calls = %d, want between 1 and 8", total)
	}
}

func TestEnrichCIStopsBeforeUnbudgetedBatch(t *testing.T) {
	calls := 0
	runner := runnerFunc(func(_ context.Context, _ ...string) ([]byte, error) {
		calls++
		if calls > 1 {
			return nil, errors.New("unexpected second GraphQL request")
		}
		return []byte(`{"data":{"rateLimit":{"limit":5000,"remaining":0,"resetAt":"2027-08-07T13:00:00Z","cost":1},"p0":{"pullRequest":{"commits":{"nodes":[{"commit":{"statusCheckRollup":{"state":"SUCCESS"}}}]}}}}}`), nil
	})
	client := NewClient(runner, config.GitHubConfig{CIBatchSize: 1})
	prs := []PullRequest{
		{Repository: "acme/one", Number: 1, URL: "https://github.com/acme/one/pull/1"},
		{Repository: "acme/two", Number: 2, URL: "https://github.com/acme/two/pull/2"},
	}

	budget := RateResource{Limit: 5000, Remaining: 2, Reset: time.Now().Add(time.Hour)}
	rate, _, err := client.EnrichCI(context.Background(), prs, budget)
	if err == nil || !strings.Contains(err.Error(), "needs at least 1") {
		t.Fatalf("error = %v", err)
	}
	if calls != 1 {
		t.Fatalf("GraphQL calls = %d, want 1", calls)
	}
	if rate.Limit != 5000 || rate.Remaining != 0 {
		t.Fatalf("rate = %#v", rate)
	}
}

func TestEnrichCIRechecksExpiredCacheBeforeFirstBatch(t *testing.T) {
	calls := 0
	runner := runnerFunc(func(_ context.Context, _ ...string) ([]byte, error) {
		calls++
		return nil, errors.New("unexpected GraphQL request")
	})
	client := NewClient(runner, config.GitHubConfig{CIBatchSize: 1})
	pr := PullRequest{Repository: "acme/one", Number: 1, URL: "https://github.com/acme/one/pull/1"}
	client.ciCache[pr.URL] = ciCacheEntry{state: CISuccess, expiresAt: time.Now().Add(-time.Second)}
	budget := RateResource{Limit: 5000, Remaining: 0, Reset: time.Now().Add(time.Hour)}

	rate, _, err := client.EnrichCI(context.Background(), []PullRequest{pr}, budget)
	if err == nil || !strings.Contains(err.Error(), "needs at least 1") {
		t.Fatalf("error = %v", err)
	}
	if calls != 0 {
		t.Fatalf("GraphQL calls = %d, want 0", calls)
	}
	if rate.Limit != 5000 || rate.Remaining != 0 {
		t.Fatalf("rate = %#v", rate)
	}
}

func TestRateLimits(t *testing.T) {
	runner := runnerFunc(func(_ context.Context, _ ...string) ([]byte, error) {
		return []byte(`{"resources":{"search":{"limit":30,"remaining":27,"reset":1786107600},"graphql":{"limit":5000,"remaining":4800,"reset":1786111200}}}`), nil
	})
	client := NewClient(runner, config.GitHubConfig{})
	rates, err := client.RateLimits(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if rates.Search.Remaining != 27 || rates.GraphQL.Limit != 5000 {
		t.Fatalf("rates = %#v", rates)
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

func TestSearchViewRunsScopeSearchesConcurrently(t *testing.T) {
	var started atomic.Int64
	var inFlight atomic.Int64
	var maxInFlight atomic.Int64
	release := make(chan struct{})
	runner := runnerFunc(func(_ context.Context, args ...string) ([]byte, error) {
		if len(args) >= 3 && args[0] == "search" && args[1] == "prs" {
			now := inFlight.Add(1)
			updateMaxAtomic(&maxInFlight, now)
			started.Add(1)
			<-release
			inFlight.Add(-1)
			separator := slices.Index(args, "--")
			query := strings.Join(args[separator+1:], " ")
			if strings.Contains(query, "repo:acme/one") {
				return []byte(`[{"number":1,"title":"Overlap","url":"https://github.com/acme/one/pull/1","isDraft":false,"updatedAt":"2026-08-07T11:00:00Z","author":{"login":"alice"},"repository":{"nameWithOwner":"acme/one"}}]`), nil
			}
			return []byte(`[{"number":1,"title":"Overlap","url":"https://github.com/acme/one/pull/1","isDraft":false,"updatedAt":"2026-08-07T10:00:00Z","author":{"login":"alice"},"repository":{"nameWithOwner":"acme/one"}}]`), nil
		}
		return nil, fmt.Errorf("unexpected command: %v", args)
	})
	client := NewClient(runner, config.GitHubConfig{
		LimitPerScope:  100,
		MaxConcurrency: 2,
		Scopes:         []string{"repo:acme/one", "repo:acme/two"},
	})
	type outcome struct {
		prs []PullRequest
		err error
	}
	done := make(chan outcome, 1)
	go func() {
		prs, err := client.SearchView(context.Background(), config.View{Title: "All", Query: "is:open", Scope: "configured"})
		done <- outcome{prs: prs, err: err}
	}()

	deadline := time.Now().Add(2 * time.Second)
	for started.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if started.Load() < 2 {
		t.Fatal("scope searches did not run concurrently")
	}
	if got := maxInFlight.Load(); got > 2 {
		t.Fatalf("concurrent searches = %d, want at most 2", got)
	}
	close(release)

	result := <-done
	if result.err != nil {
		t.Fatal(result.err)
	}
	if len(result.prs) != 1 {
		t.Fatalf("PR count = %d, want 1 after deduplication", len(result.prs))
	}
	if result.prs[0].URL != "https://github.com/acme/one/pull/1" || !result.prs[0].UpdatedAt.Equal(time.Date(2026, 8, 7, 11, 0, 0, 0, time.UTC)) {
		t.Fatalf("PR = %#v, want the most recently updated row", result.prs[0])
	}
}

func TestSearchViewConcurrencyDoesNotExceedMaxConcurrency(t *testing.T) {
	var inFlight atomic.Int64
	var maxInFlight atomic.Int64
	var total atomic.Int64
	release := make(chan struct{})
	runner := runnerFunc(func(_ context.Context, args ...string) ([]byte, error) {
		if len(args) >= 3 && args[0] == "search" && args[1] == "prs" {
			now := inFlight.Add(1)
			updateMaxAtomic(&maxInFlight, now)
			total.Add(1)
			<-release
			inFlight.Add(-1)
			return []byte("[]"), nil
		}
		return nil, fmt.Errorf("unexpected command: %v", args)
	})
	client := NewClient(runner, config.GitHubConfig{
		LimitPerScope:  100,
		MaxConcurrency: 2,
		Scopes: []string{
			"repo:acme/one", "repo:acme/two", "repo:acme/three",
			"repo:acme/four", "repo:acme/five", "repo:acme/six",
		},
	})
	type outcome struct {
		prs []PullRequest
		err error
	}
	done := make(chan outcome, 1)
	go func() {
		prs, err := client.SearchView(context.Background(), config.View{Title: "All", Query: "is:open", Scope: "configured"})
		done <- outcome{prs: prs, err: err}
	}()

	deadline := time.Now().Add(2 * time.Second)
	for inFlight.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	close(release)
	result := <-done
	if result.err != nil {
		t.Fatal(result.err)
	}
	if got := total.Load(); got != 6 {
		t.Fatalf("search calls = %d, want 6", got)
	}
	if got := maxInFlight.Load(); got != 2 {
		t.Fatalf("concurrent searches = %d, want exactly 2", got)
	}
}

func TestSearchViewStopsRemainingSearchesOnError(t *testing.T) {
	otherScopeWaiting := make(chan struct{})
	var secondCanceled atomic.Bool
	runner := runnerFunc(func(ctx context.Context, args ...string) ([]byte, error) {
		if len(args) >= 3 && args[0] == "search" && args[1] == "prs" {
			separator := slices.Index(args, "--")
			query := strings.Join(args[separator+1:], " ")
			if strings.Contains(query, "repo:acme/one") {
				select {
				case <-otherScopeWaiting:
					return nil, errors.New("scope one failed")
				case <-time.After(2 * time.Second):
					return nil, errors.New("second scope search never started")
				}
			}
			close(otherScopeWaiting)
			select {
			case <-ctx.Done():
				secondCanceled.Store(true)
				return nil, ctx.Err()
			case <-time.After(2 * time.Second):
				return nil, errors.New("search kept running after another scope failed")
			}
		}
		return nil, fmt.Errorf("unexpected command: %v", args)
	})
	client := NewClient(runner, config.GitHubConfig{
		LimitPerScope:  100,
		MaxConcurrency: 2,
		Scopes:         []string{"repo:acme/one", "repo:acme/two"},
	})
	_, err := client.SearchView(context.Background(), config.View{Title: "All", Query: "is:open", Scope: "configured"})
	if err == nil || !strings.Contains(err.Error(), `search "All"`) || !strings.Contains(err.Error(), "scope one failed") {
		t.Fatalf("error = %v, want the first failure wrapped with the view title", err)
	}
	if !secondCanceled.Load() {
		t.Fatal("in-flight scope search kept running after another scope failed")
	}
}
