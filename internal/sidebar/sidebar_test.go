package sidebar

import (
	"errors"
	"testing"

	gh "github.com/cdowell09/herdr-pr-board/internal/github"
)

func pr(url string, ci gh.CIState) gh.PullRequest {
	return gh.PullRequest{Repository: "acme/api", Number: 1, Title: "T", URL: url, CI: ci}
}

func TestTokensCountsDistinctPRsAcrossViews(t *testing.T) {
	views := []View{
		{ID: "authored", PRs: []gh.PullRequest{pr("https://github.com/acme/api/pull/1", gh.CISuccess)}},
		{ID: "all", PRs: []gh.PullRequest{
			pr("https://github.com/acme/api/pull/1", gh.CISuccess),
			pr("https://github.com/acme/api/pull/2", gh.CIPending),
			pr("https://github.com/acme/api/pull/3", gh.CIFailure),
		}},
	}
	tokens := Tokens("review", views)
	if tokens[TokenOpen] != "3 open" {
		t.Fatalf("prs_open = %q, want %q", tokens[TokenOpen], "3 open")
	}
	if _, exists := tokens[TokenReview]; exists {
		t.Fatalf("prs_review reported without a review view: %#v", tokens)
	}
	if tokens[TokenCI] != "1 fail" {
		t.Fatalf("prs_ci = %q, want %q", tokens[TokenCI], "1 fail")
	}
}

func TestTokensCountsReviewViewAndCIFailures(t *testing.T) {
	views := []View{
		{ID: "review", PRs: []gh.PullRequest{
			pr("https://github.com/acme/api/pull/1", gh.CIFailure),
			pr("https://github.com/acme/api/pull/2", gh.CIError),
		}},
		{ID: "all", PRs: []gh.PullRequest{
			pr("https://github.com/acme/api/pull/1", gh.CIFailure),
			pr("https://github.com/acme/api/pull/2", gh.CIError),
			pr("https://github.com/acme/api/pull/3", gh.CIPending),
			pr("https://github.com/acme/api/pull/4", gh.CISuccess),
			pr("https://github.com/acme/api/pull/5", gh.CINone),
			pr("https://github.com/acme/api/pull/6", gh.CIUnknown),
		}},
	}
	tokens := Tokens("review", views)
	if tokens[TokenOpen] != "6 open" {
		t.Fatalf("prs_open = %q, want %q", tokens[TokenOpen], "6 open")
	}
	if tokens[TokenReview] != "2 review" {
		t.Fatalf("prs_review = %q, want %q", tokens[TokenReview], "2 review")
	}
	if tokens[TokenCI] != "2 fail" {
		t.Fatalf("prs_ci = %q, want %q", tokens[TokenCI], "2 fail")
	}
}

func TestTokensOmitsCITokenWhenNothingFails(t *testing.T) {
	views := []View{
		{ID: "review", PRs: []gh.PullRequest{
			pr("https://github.com/acme/api/pull/1", gh.CISuccess),
			pr("https://github.com/acme/api/pull/2", gh.CIPending),
		}},
	}
	tokens := Tokens("review", views)
	if _, exists := tokens[TokenCI]; exists {
		t.Fatalf("prs_ci reported with no failures: %#v", tokens)
	}
	if tokens[TokenOpen] != "2 open" || tokens[TokenReview] != "2 review" {
		t.Fatalf("tokens = %#v", tokens)
	}
}

func TestTokensReportsZeroCountsOnEmptyBoard(t *testing.T) {
	views := []View{{ID: "review"}, {ID: "all"}}
	tokens := Tokens("review", views)
	if tokens[TokenOpen] != "0 open" {
		t.Fatalf("prs_open = %q, want %q", tokens[TokenOpen], "0 open")
	}
	if tokens[TokenReview] != "0 review" {
		t.Fatalf("prs_review = %q, want %q", tokens[TokenReview], "0 review")
	}
	if _, exists := tokens[TokenCI]; exists {
		t.Fatalf("prs_ci reported on an empty board: %#v", tokens)
	}
}

func TestTokensNilWhenAnyViewFailed(t *testing.T) {
	views := []View{
		{ID: "authored", PRs: []gh.PullRequest{pr("https://github.com/acme/api/pull/1", gh.CISuccess)}},
		{ID: "all", Err: errors.New("rate limited")},
	}
	if tokens := Tokens("review", views); tokens != nil {
		t.Fatalf("tokens = %#v, want nil", tokens)
	}
}

func TestTokensOmitReviewTokenForUnknownReviewView(t *testing.T) {
	views := []View{{ID: "mine", PRs: []gh.PullRequest{pr("https://github.com/acme/api/pull/1", gh.CISuccess)}}}
	tokens := Tokens("renamed", views)
	if _, exists := tokens[TokenReview]; exists {
		t.Fatalf("prs_review reported for missing view: %#v", tokens)
	}
	if tokens[TokenOpen] != "1 open" {
		t.Fatalf("prs_open = %q", tokens[TokenOpen])
	}
}
