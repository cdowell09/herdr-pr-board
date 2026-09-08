package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/cdowell09/herdr-pr-board/internal/config"
)

func reviewResponse(nodes, page string, remaining, cost int) []byte {
	return []byte(fmt.Sprintf(`{"data":{"rateLimit":{"limit":5000,"remaining":%d,"cost":%d,"resetAt":"2099-01-01T00:00:00Z"},"p0":{"pullRequest":{"headRefOid":"head","baseRefName":"main","baseRefOid":"base","commits":{"nodes":[{"commit":{"statusCheckRollup":null}}]},"reviews":{"nodes":%s,"pageInfo":%s}}}}}`, remaining, cost, nodes, page))
}

func TestViewerReviewsPaginationAndCache(t *testing.T) {
	calls, identities := 0, 0
	client := NewClient(func(_ context.Context, args ...string) ([]byte, error) {
		if args[1] == "user" {
			identities++
			return []byte("alice"), nil
		}
		calls++
		query := args[len(args)-1]
		if !strings.Contains(query, `author: "alice"`) || !strings.Contains(query, "fullDatabaseId") {
			t.Fatalf("query = %s", query)
		}
		if calls == 1 {
			return reviewResponse(`[{"fullDatabaseId":"9007199254740993","state":"APPROVED","submittedAt":"2026-01-01T00:00:00Z","commit":{"oid":"head"}},{"fullDatabaseId":2,"state":"PENDING","submittedAt":null}]`, `{"hasNextPage":true,"endCursor":"next"}`, 4997, 3), nil
		}
		if !strings.Contains(query, `after: "next"`) {
			t.Fatalf("missing cursor: %s", query)
		}
		return reviewResponse(`[{"fullDatabaseId":3,"state":"DISMISSED","submittedAt":"2026-01-02T00:00:00Z","commit":{"oid":"older"}}]`, `{"hasNextPage":false}`, 4994, 3), nil
	}, config.GitHubConfig{CIBatchSize: 1})
	prs := []PullRequest{{Repository: "acme/repo", Number: 1, URL: "one"}}
	rate, warnings, err := client.EnrichCI(context.Background(), prs, RateResource{})
	observation := prs[0].ViewerReviews
	if err != nil || len(warnings) != 0 || calls != 2 || identities != 1 || rate.Remaining != 4994 || observation == nil || !observation.Complete || observation.Actor != "alice" || len(observation.Reviews) != 2 || observation.ObservedAt.IsZero() {
		t.Fatalf("result: %#v, %v, %v, calls %d identity %d", observation, warnings, err, calls, identities)
	}
	if observation.Reviews[0].ID != 9007199254740993 || observation.Reviews[1].HeadOID != "older" {
		t.Fatalf("reviews %#v", observation.Reviews)
	}
	observation.Reviews[0].HeadOID = "mutated"
	if _, _, err := client.EnrichCI(context.Background(), prs, rate); err != nil || calls != 2 || identities != 1 || prs[0].ViewerReviews.Reviews[0].HeadOID != "head" {
		t.Fatalf("cache alias or request: %#v, %v", prs[0], err)
	}
}

func TestViewerReviewsPageFailuresPreserveMetadata(t *testing.T) {
	for _, failure := range []string{"capacity", "transport", "partial", "cursor"} {
		t.Run(failure, func(t *testing.T) {
			calls := 0
			client := newEnrichmentTestClient(func(context.Context, ...string) ([]byte, error) {
				calls++
				if calls == 1 {
					remaining := 4998
					if failure == "capacity" {
						remaining = 1
					}
					return reviewResponse(`[{"fullDatabaseId":1,"state":"COMMENTED","submittedAt":"2026-01-01T00:00:00Z","commit":{"oid":"head"}}]`, `{"hasNextPage":true,"endCursor":"next"}`, remaining, 2), nil
				}
				if failure == "transport" {
					return nil, errors.New("offline")
				}
				if failure == "cursor" {
					return reviewResponse(`[]`, `{"hasNextPage":true,"endCursor":"next"}`, 4996, 2), nil
				}
				return []byte(`{"data":{"rateLimit":{"limit":5000,"remaining":4995,"cost":3},"p0":{"pullRequest":{"reviews":null}}},"errors":[{"message":"reviews failed","path":["p0","pullRequest","reviews"]}]}`), errors.New("partial response")
			}, config.GitHubConfig{CIBatchSize: 1})
			prs := []PullRequest{{Repository: "acme/repo", Number: 1, URL: "one"}}
			rate, warnings, err := client.EnrichCI(context.Background(), prs, RateResource{})
			observation := prs[0].ViewerReviews
			if observation == nil || observation.Complete || len(observation.Reviews) != 1 || prs[0].CI != CINone || prs[0].HeadOID != "head" || prs[0].BaseOID != "base" {
				t.Fatalf("partial data lost: %#v", prs[0])
			}
			if failure == "capacity" {
				if err == nil || calls != 1 || rate.Remaining != 1 {
					t.Fatalf("budget: %#v %v calls %d", rate, err, calls)
				}
			} else if err != nil || len(warnings) == 0 {
				t.Fatalf("failure: %v %v", warnings, err)
			}
			if failure == "partial" && (rate.Remaining != 4995 || rate.Cost != 3) {
				t.Fatalf("lost failure rates: %#v", rate)
			}
		})
	}
}

func TestViewerReviewsIdentityFailurePreservesCI(t *testing.T) {
	client := NewClient(func(_ context.Context, args ...string) ([]byte, error) {
		if args[1] == "user" {
			return nil, errors.New("identity unavailable")
		}
		if strings.Contains(args[len(args)-1], "reviews(") {
			t.Fatal("reviews requested without identity")
		}
		return reviewResponse(`[]`, `{"hasNextPage":false}`, 4999, 1), nil
	}, config.GitHubConfig{CIBatchSize: 1})
	prs := []PullRequest{{Repository: "acme/repo", Number: 1, URL: "one"}}
	_, warnings, err := client.EnrichCI(context.Background(), prs, RateResource{})
	if err != nil || len(warnings) != 1 || prs[0].CI != CINone || prs[0].ViewerReviews != nil {
		t.Fatalf("result %#v %v %v", prs[0], warnings, err)
	}
}

func TestDecodeViewerReviewsUnknownAndEmpty(t *testing.T) {
	for _, connection := range []string{`null`, `{}`, `{"nodes":[],"pageInfo":null}`, `{"nodes":[],"pageInfo":{}}`, `{"nodes":[],"pageInfo":{"hasNextPage":true}}`, `{"nodes":[null],"pageInfo":{"hasNextPage":false}}`, `{"nodes":[{"state":"APPROVED","submittedAt":null}],"pageInfo":{"hasNextPage":false}}`} {
		_, _, err := decodeReviewPage(json.RawMessage(`{"pullRequest":{"reviews":` + connection + `}}`))
		if err == nil {
			t.Fatalf("unknown accepted: %s", connection)
		}
	}
	reviews, next, err := decodeReviewPage(json.RawMessage(`{"pullRequest":{"reviews":{"nodes":[],"pageInfo":{"hasNextPage":false}}}}`))
	if err != nil || len(reviews) != 0 || next != "" {
		t.Fatalf("empty page: %v %q %v", reviews, next, err)
	}
}

func TestCopyViewerReviewsIndependent(t *testing.T) {
	source := PullRequest{ViewerReviews: &ReviewObservation{Actor: "alice", Reviews: []SubmittedReview{{ID: 1, SubmittedAt: time.Now()}}}}
	var target PullRequest
	target.CopyEnrichment(source)
	target.ViewerReviews.Actor = "bob"
	target.ViewerReviews.Reviews[0].ID = 2
	if source.ViewerReviews.Actor != "alice" || source.ViewerReviews.Reviews[0].ID != 1 {
		t.Fatal("copied review observation aliases source")
	}
	target.CopyEnrichment(PullRequest{})
	if target.ViewerReviews != nil {
		t.Fatal("unknown observation retained old reviews")
	}
}

func TestViewerReviewsPaginationOnlyQueriesUnfinishedConnections(t *testing.T) {
	calls := 0
	client := newEnrichmentTestClient(func(_ context.Context, args ...string) ([]byte, error) {
		calls++
		if calls == 1 {
			var response map[string]any
			if err := json.Unmarshal(reviewResponse(`[]`, `{"hasNextPage":false}`, 4999, 1), &response); err != nil {
				t.Fatal(err)
			}
			data := response["data"].(map[string]any)
			data["p1"] = map[string]any{"pullRequest": map[string]any{"headRefOid": "second", "baseRefName": "main", "baseRefOid": "base", "commits": map[string]any{"nodes": []any{map[string]any{"commit": map[string]any{"statusCheckRollup": nil}}}}, "reviews": map[string]any{"nodes": []any{}, "pageInfo": map[string]any{"hasNextPage": true, "endCursor": "next"}}}}
			return json.Marshal(response)
		}
		query := args[len(args)-1]
		if strings.Contains(query, "p0:") || !strings.Contains(query, "p1:") {
			t.Fatalf("incorrect page aliases: %s", query)
		}
		return []byte(`{"data":{"p1":{"pullRequest":{"reviews":{"nodes":[],"pageInfo":{"hasNextPage":false}}}}}}`), nil
	}, config.GitHubConfig{CIBatchSize: 2})
	prs := []PullRequest{{Repository: "acme/repo", Number: 1, URL: "one"}, {Repository: "acme/repo", Number: 2, URL: "two"}}
	_, warnings, err := client.EnrichCI(context.Background(), prs, RateResource{})
	if err != nil || len(warnings) != 0 || calls != 2 {
		t.Fatalf("calls %d warnings %v err %v", calls, warnings, err)
	}
	for _, pr := range prs {
		if pr.ViewerReviews == nil || !pr.ViewerReviews.Complete || pr.CI != CINone {
			t.Fatalf("incomplete PR %#v", pr)
		}
	}
}

func TestViewerReviewsRetryIncompleteObservations(t *testing.T) {
	for _, first := range []string{"unknown", "partial"} {
		t.Run(first, func(t *testing.T) {
			calls := 0
			client := newEnrichmentTestClient(func(context.Context, ...string) ([]byte, error) {
				calls++
				if calls == 1 {
					if first == "unknown" {
						return []byte(strings.Replace(string(reviewResponse(`[]`, `{"hasNextPage":false}`, 4998, 2)), `"reviews":{"nodes":[],"pageInfo":{"hasNextPage":false}}`, `"reviews":null`, 1)), nil
					}
					return reviewResponse(`[]`, `{"hasNextPage":true,"endCursor":"next"}`, 4998, 2), nil
				}
				if first == "partial" && calls == 2 {
					return nil, errors.New("page unavailable")
				}
				return reviewResponse(`[]`, `{"hasNextPage":false}`, 4996, 2), nil
			}, config.GitHubConfig{CIBatchSize: 1})
			prs := []PullRequest{{Repository: "acme/repo", Number: 1, URL: "one"}}
			rate, warnings, err := client.EnrichCI(context.Background(), prs, RateResource{})
			if err != nil || len(warnings) == 0 || prs[0].CI != CINone || (prs[0].ViewerReviews != nil && prs[0].ViewerReviews.Complete) {
				t.Fatalf("failed retrieval: %#v %v %v", prs[0], warnings, err)
			}
			failedCalls := calls
			rate, warnings, err = client.EnrichCI(context.Background(), prs, rate)
			if err != nil || len(warnings) != 0 || calls != failedCalls+1 || prs[0].ViewerReviews == nil || !prs[0].ViewerReviews.Complete {
				t.Fatalf("retry: %#v %v %v calls %d", prs[0], warnings, err, calls)
			}
			_, warnings, err = client.EnrichCI(context.Background(), prs, rate)
			if err != nil || len(warnings) != 0 || calls != failedCalls+1 {
				t.Fatalf("complete cache: %v %v calls %d", warnings, err, calls)
			}
		})
	}
}
