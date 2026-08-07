package github

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/cdowell09/herdr-pr-board/internal/config"
)

type runnerFunc func(context.Context, ...string) ([]byte, error)

func (f runnerFunc) Run(ctx context.Context, args ...string) ([]byte, error) {
	return f(ctx, args...)
}

func TestSearchViewResolvesScopesDeduplicatesAndSorts(t *testing.T) {
	var queries []string
	runner := runnerFunc(func(_ context.Context, args ...string) ([]byte, error) {
		if len(args) >= 2 && args[0] == "api" && args[1] == "user" {
			return []byte("cdowell09\n"), nil
		}
		if len(args) >= 3 && args[0] == "search" && args[1] == "prs" {
			queries = append(queries, args[2])
			switch {
			case strings.Contains(args[2], "user:cdowell09"):
				return []byte(`[{"number":2,"title":"Cookies","url":"https://github.com/cdowell09/cookies/pull/2","isDraft":false,"updatedAt":"2026-08-07T10:00:00Z","author":{"login":"cdowell09"},"repository":{"nameWithOwner":"cdowell09/cookies"}}]`), nil
			case strings.Contains(args[2], "repo:acme/api"):
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
	if len(queries) != 2 || queries[0] != "is:open user:cdowell09" {
		t.Fatalf("queries = %#v", queries)
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
	rate, err := client.EnrichCI(context.Background(), prs)
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
	if _, err := client.EnrichCI(context.Background(), prs); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("cached enrichment made another request; calls = %d", calls)
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
