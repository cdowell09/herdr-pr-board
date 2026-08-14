// Package sidebar reports pull-request counts into Herdr sidebar tokens.
//
// The board computes the counts from its own refresh snapshot and sends
// them through `herdr workspace report-metadata`. Herdr renders tokens
// named $prs_open, $prs_review, and $prs_ci when the user adds them to
// [ui.sidebar.spaces] rows in the Herdr configuration.
package sidebar

import (
	"fmt"

	"github.com/cdowell09/herdr-pr-board/internal/github"
)

// Source identifies this plugin as the reporter of sidebar metadata tokens.
const Source = "cdowell09.pr-board"

// Token names reported into Herdr workspace metadata.
const (
	TokenOpen   = "prs_open"
	TokenReview = "prs_review"
	TokenCI     = "prs_ci"
)

// View carries one board view's refresh result for token computation.
type View struct {
	ID  string
	PRs []github.PullRequest
	Err error
}

// Tokens builds the sidebar token values from a full refresh snapshot.
//
// It returns nil when any view failed. Partial data would overwrite
// fresher tokens with stale counts, so the board keeps the previous
// tokens until their TTL expires.
//
// The prs_ci token is omitted when no PR has a failed check. The
// prs_review token is omitted when no view has the configured ID.
func Tokens(reviewView string, views []View) map[string]string {
	distinct := make(map[string]github.PullRequest)
	reviewCount := -1
	for _, view := range views {
		if view.Err != nil {
			return nil
		}
		for _, pr := range view.PRs {
			distinct[pr.URL] = pr
		}
		if view.ID == reviewView {
			reviewCount = len(view.PRs)
		}
	}

	tokens := map[string]string{
		TokenOpen: fmt.Sprintf("%d open", len(distinct)),
	}
	if reviewCount >= 0 {
		tokens[TokenReview] = fmt.Sprintf("%d review", reviewCount)
	}
	failures := 0
	for _, pr := range distinct {
		if pr.CI == github.CIFailure || pr.CI == github.CIError {
			failures++
		}
	}
	if failures > 0 {
		tokens[TokenCI] = fmt.Sprintf("%d fail", failures)
	}
	return tokens
}
