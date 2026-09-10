package dispatch

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cdowell09/herdr-pr-board/internal/config"
	"github.com/cdowell09/herdr-pr-board/internal/discovery"
	gh "github.com/cdowell09/herdr-pr-board/internal/github"
	"github.com/cdowell09/herdr-pr-board/internal/reviewmemory"
)

func observedSnapshot() discovery.Snapshot {
	now := time.Now()
	pr := gh.PullRequest{Repository: "owner/repo", Number: 42, URL: "https://github.com/owner/repo/pull/42", State: "OPEN", HeadOID: strings.Repeat("a", 40), BaseRefName: "main", BaseOID: strings.Repeat("b", 40), MetadataObservedAt: now, CI: gh.CIFailure}
	return discovery.Snapshot{StartedAt: now, FinishedAt: now, Views: []discovery.ViewData{{View: config.View{ID: "review"}, PRs: []gh.PullRequest{pr}, ObservedAt: now, UpdatedAt: now}}}
}

func TestEligibilityUsesOneReasonForEachHold(t *testing.T) {
	for _, test := range []struct {
		name, reason string
		change       func(*Candidate)
		allowed      bool
		status       reviewmemory.Status
	}{
		{name: "failed CI eligible", reason: Ready, allowed: true},
		{name: "unselected", reason: ViewNotSelected, allowed: true, change: func(c *Candidate) { c.Selected = false }},
		{name: "stale", reason: ObservationFailed, allowed: true, change: func(c *Candidate) { c.Observed = false }},
		{name: "conflict", reason: ConflictingRevision, allowed: true, change: func(c *Candidate) { c.Conflict = true }},
		{name: "missing head", reason: RevisionUnavailable, allowed: true, change: func(c *Candidate) { c.PR.HeadOID = "" }},
		{name: "old metadata", reason: RevisionUnavailable, allowed: true, change: func(c *Candidate) { c.PR.MetadataObservedAt = c.ObservedAt.Add(-time.Second) }},
		{name: "closed", reason: NotOpen, allowed: true, change: func(c *Candidate) { c.PR.State = "CLOSED" }},
		{name: "draft", reason: Draft, allowed: true, change: func(c *Candidate) { c.PR.Draft = true }},
		{name: "permission", reason: PermissionDenied},
		{name: "running", reason: reviewmemory.ErrActive.Error(), allowed: true, status: reviewmemory.Running},
		{name: "completed", reason: reviewmemory.ErrReviewed.Error(), allowed: true, status: reviewmemory.Completed},
		{name: "failed", reason: reviewmemory.ErrRetryRequired.Error(), allowed: true, status: reviewmemory.Failed},
		{name: "blocked", reason: reviewmemory.ErrRetryRequired.Error(), allowed: true, status: reviewmemory.Blocked},
		{name: "abandoned", reason: reviewmemory.ErrRetryRequired.Error(), allowed: true, status: reviewmemory.Abandoned},
	} {
		t.Run(test.name, func(t *testing.T) {
			candidate := Candidates(observedSnapshot(), []config.View{{ID: "review"}})[0]
			candidate.Selected = true
			if test.change != nil {
				test.change(&candidate)
			}
			var historyErr error
			if test.status != "" {
				historyErr = errors.New(test.reason)
			}
			got := Evaluate(candidate, test.allowed, historyErr)
			if got.Reason != test.reason || got.Eligible != (test.reason == Ready) {
				t.Fatalf("decision=%+v", got)
			}
		})
	}
}

func TestCandidatesDeduplicateAndRejectFailedObservations(t *testing.T) {
	snapshot := observedSnapshot()
	duplicate := snapshot.Views[0]
	duplicate.View.ID = "all"
	snapshot.Views = append(snapshot.Views, duplicate)
	candidates := Candidates(snapshot, []config.View{{ID: "review"}, {ID: "all"}})
	if len(candidates) != 1 || len(candidates[0].Views) != 2 || !candidates[0].Observed || candidates[0].Conflict {
		t.Fatalf("candidates=%+v", candidates)
	}
	snapshot.Errors = []discovery.RetrievalError{{Stage: "enrichment", Err: errors.New("failed")}}
	if got := Candidates(snapshot, []config.View{{ID: "review"}, {ID: "all"}})[0]; got.Observed {
		t.Fatalf("decision=%+v", got)
	}
	snapshot.Errors = nil
	snapshot.Views[1].PRs = append([]gh.PullRequest(nil), snapshot.Views[1].PRs...)
	snapshot.Views[1].PRs[0].HeadOID = strings.Repeat("c", 40)
	if got := Candidates(snapshot, []config.View{{ID: "review"}, {ID: "all"}})[0]; !got.Conflict {
		t.Fatalf("decision=%+v", got)
	}
}

func TestChangedViewDefinitionDoesNotReuseOldObservation(t *testing.T) {
	snapshot := observedSnapshot()
	candidates := Candidates(snapshot, []config.View{{ID: "review", Query: "new query"}})
	if got := candidates[0]; got.Observed {
		t.Fatalf("decision=%+v", got)
	}
}

func TestDecisionsUseCurrentViewSelection(t *testing.T) {
	_, cfg := dispatchConfig(t, t.TempDir(), []string{"unused"}, false)
	candidates := Candidates(snapshotWithPRs(cfg, 1), cfg.Views)
	for _, selected := range [][]string{nil, {"review"}, nil} {
		cfg.Review.AutoViews = selected
		got := Decisions(candidates, cfg, &fakeReviews{})[0]
		want := ViewNotSelected
		if len(selected) > 0 {
			want = Ready
		}
		if got.Reason != want || got.Eligible != (want == Ready) {
			t.Fatalf("selected=%v decision=%+v", selected, got)
		}
	}
}
