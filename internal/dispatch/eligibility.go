// Package dispatch owns automatic review eligibility and monitor dispatch.
package dispatch

import (
	"strings"
	"time"

	"github.com/cdowell09/herdr-pr-board/internal/config"
	"github.com/cdowell09/herdr-pr-board/internal/discovery"
	gh "github.com/cdowell09/herdr-pr-board/internal/github"
	"github.com/cdowell09/herdr-pr-board/internal/reviewmemory"
)

const (
	Ready               = "eligible"
	ViewNotSelected     = "view is not selected for automatic reviews"
	ObservationFailed   = "latest observation failed or is stale"
	ConflictingRevision = "views contain conflicting PR observations"
	RevisionUnavailable = "current revision evidence is unavailable"
	NotOpen             = "PR is not open"
	Draft               = "PR is a draft"
	PermissionDenied    = "repository does not allow automatic reviews"
)

type Candidate struct {
	PR         gh.PullRequest
	Views      []config.View
	Selected   bool
	Observed   bool
	ObservedAt time.Time
	Conflict   bool
}

type Decision struct {
	Identity reviewmemory.Identity `json:"identity"`
	URL      string                `json:"url"`
	Views    []string              `json:"views"`
	Eligible bool                  `json:"eligible"`
	Reason   string                `json:"reason"`
}

func Identity(pr gh.PullRequest) reviewmemory.Identity {
	return reviewmemory.Identity{Repository: strings.ToLower(pr.Repository), Number: pr.Number, HeadOID: pr.HeadOID, BaseRefName: pr.BaseRefName}
}

// Candidates deduplicates PRs and retains conflicting observations as a hold.
// A failed snapshot never supplies dispatchable retained rows.
func Candidates(snapshot discovery.Snapshot, configuredViews []config.View, selectedViews []string) []Candidate {
	configured := map[string]config.View{}
	for _, view := range configuredViews {
		configured[view.ID] = view
	}
	selected := map[string]bool{}
	for _, id := range selectedViews {
		selected[id] = true
	}
	type key struct {
		repository string
		number     int
	}
	byPR := map[key]int{}
	result := []Candidate{}
	successful := len(snapshot.Errors) == 0 && snapshot.CapacityErr == nil && !snapshot.StartedAt.IsZero() && !snapshot.FinishedAt.Before(snapshot.StartedAt)
	for _, view := range snapshot.Views {
		observed := successful && configured[view.View.ID] == view.View && view.Err == nil && !view.UpdatedAt.Before(snapshot.StartedAt) && !view.ObservedAt.Before(snapshot.StartedAt)
		for _, pr := range view.PRs {
			id := key{strings.ToLower(pr.Repository), pr.Number}
			current := Candidate{PR: pr, Views: []config.View{view.View}, Selected: selected[view.View.ID], Observed: observed, ObservedAt: snapshot.StartedAt}
			if index, exists := byPR[id]; exists {
				previous := &result[index]
				previous.Views = append(previous.Views, view.View)
				previous.Selected = previous.Selected || current.Selected
				previous.Observed = previous.Observed && current.Observed
				previous.Conflict = previous.Conflict || Identity(previous.PR) != Identity(pr) || previous.PR.BaseOID != pr.BaseOID || previous.PR.Draft != pr.Draft || previous.PR.State != pr.State
			} else {
				byPR[id] = len(result)
				result = append(result, current)
			}
		}
	}
	return result
}

// Evaluate is shared by status displays and dispatch. CI does not gate reviews.
func Evaluate(candidate Candidate, autoLaunch bool, historyErr error) Decision {
	pr := candidate.PR
	result := Decision{Identity: Identity(pr), URL: pr.URL, Views: []string{}}
	for _, view := range candidate.Views {
		result.Views = append(result.Views, view.ID)
	}
	switch {
	case !candidate.Selected:
		result.Reason = ViewNotSelected
	case !candidate.Observed:
		result.Reason = ObservationFailed
	case candidate.Conflict:
		result.Reason = ConflictingRevision
	case pr.MetadataObservedAt.IsZero() || pr.MetadataObservedAt.Before(candidate.ObservedAt) || reviewmemory.ValidateRevision(result.Identity, pr.BaseOID) != nil:
		result.Reason = RevisionUnavailable
	case pr.State != gh.PROpen:
		result.Reason = NotOpen
	case pr.Draft:
		result.Reason = Draft
	case !autoLaunch:
		result.Reason = PermissionDenied
	default:
		if historyErr != nil {
			result.Reason = historyErr.Error()
		} else {
			result.Eligible = true
			result.Reason = Ready
		}
	}
	return result
}
