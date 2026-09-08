package board

import (
	"errors"
	"strings"
	"testing"
	"time"

	gh "github.com/cdowell09/herdr-pr-board/internal/github"
	"github.com/cdowell09/herdr-pr-board/internal/publication"
	"github.com/cdowell09/herdr-pr-board/internal/reviewmemory"
)

func TestPostedReviewOriginsAndRevisionEvidence(t *testing.T) {
	now := time.Now()
	head, old := strings.Repeat("a", 40), strings.Repeat("b", 40)
	posted := func(id int64, commit string) gh.SubmittedReview {
		return gh.SubmittedReview{ID: id, HeadOID: commit, State: "COMMENTED", SubmittedAt: now.Add(-time.Minute)}
	}
	boardPost := publication.Attempt{GitHubID: 2, Actor: "Ada", Status: publication.Published, Identity: reviewmemory.Identity{HeadOID: old}, StartedAt: now.Add(-time.Minute)}
	for _, tc := range []struct {
		name          string
		reviews       []gh.SubmittedReview
		attempts      []publication.Attempt
		complete      bool
		unknown       bool
		localErr      error
		label, detail string
	}{
		{name: "none", reviews: []gh.SubmittedReview{}, complete: true, label: "–", detail: "No submitted reviews"},
		{name: "GitHub", reviews: []gh.SubmittedReview{posted(1, head)}, complete: true, label: "GitHub", detail: "GitHub · current revision"},
		{name: "board older", reviews: []gh.SubmittedReview{posted(2, old)}, attempts: []publication.Attempt{boardPost}, complete: true, label: "PR Board", detail: "PR Board · older revision"},
		{name: "board unknown revision", reviews: []gh.SubmittedReview{posted(2, "")}, attempts: []publication.Attempt{boardPost}, complete: true, label: "PR Board", detail: "PR Board · revision unknown"},
		{name: "both", reviews: []gh.SubmittedReview{posted(1, head), posted(2, old)}, attempts: []publication.Attempt{boardPost}, complete: true, label: "Both", detail: "GitHub · current revision; PR Board · older revision"},
		{name: "other account record", reviews: []gh.SubmittedReview{posted(2, old)}, attempts: []publication.Attempt{{GitHubID: 2, Actor: "someone-else", Status: publication.Published}}, complete: true, label: "GitHub", detail: "GitHub · older revision"},
		{name: "draft not submitted", reviews: []gh.SubmittedReview{{ID: 1, HeadOID: head, State: "PENDING"}}, complete: true, label: "–", detail: "No submitted reviews"},
		{name: "partial", reviews: []gh.SubmittedReview{posted(1, head)}, label: "?", detail: "GitHub · current revision; GitHub review history incomplete"},
		{name: "unavailable", unknown: true, label: "?", detail: "GitHub review status unavailable"},
		{name: "local state failure", reviews: []gh.SubmittedReview{posted(1, head)}, complete: true, localErr: errors.New("corrupt"), label: "?", detail: "Review publication source unavailable"},
		{name: "deleted board post", reviews: []gh.SubmittedReview{}, attempts: []publication.Attempt{boardPost}, complete: true, label: "–", detail: "No submitted reviews"},
		{name: "uncertain own post", reviews: []gh.SubmittedReview{}, attempts: []publication.Attempt{{Actor: "ada", Status: publication.Uncertain}}, complete: true, label: "?", detail: "PR Board publication outcome uncertain"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pr := gh.PullRequest{HeadOID: head, MetadataObservedAt: now, ViewerReviews: &gh.ReviewObservation{Actor: "ada", ObservedAt: now, Complete: tc.complete, Reviews: tc.reviews}}
			if tc.unknown {
				pr.ViewerReviews = nil
			}
			label, detail := postedReviewSummary(pr, tc.attempts, tc.localErr)
			if label != tc.label || detail != tc.detail {
				t.Fatalf("got %q %q; want %q %q", label, detail, tc.label, tc.detail)
			}
		})
	}
	pr := gh.PullRequest{HeadOID: head, MetadataObservedAt: now, ViewerReviews: &gh.ReviewObservation{Actor: "ada", ObservedAt: now, Complete: true}}
	boardPost.StartedAt = now.Add(time.Second)
	boardPost.Identity.HeadOID = head
	if label, detail := postedReviewSummary(pr, []publication.Attempt{boardPost}, nil); label != "PR Board" || detail != "PR Board · current revision" {
		t.Fatalf("fresh local confirmation: %s %s", label, detail)
	}
	pr.HeadOID = ""
	pr.ViewerReviews.Reviews = []gh.SubmittedReview{posted(1, head)}
	if _, detail := postedReviewSummary(pr, nil, nil); detail != "GitHub · revision unknown" {
		t.Fatalf("unknown revision became older: %s", detail)
	}
}
