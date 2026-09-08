package board

import (
	"strings"

	gh "github.com/cdowell09/herdr-pr-board/internal/github"
	"github.com/cdowell09/herdr-pr-board/internal/publication"
)

// GitHub IDs establish origin. Review bodies and display names do not.
func postedReviewSummary(pr gh.PullRequest, attempts []publication.Attempt, localErr error) (string, string) {
	observation := pr.ViewerReviews
	if observation == nil || observation.Actor == "" {
		return "?", "GitHub review status unavailable"
	}
	if localErr != nil {
		return "?", "Review publication source unavailable"
	}
	owned := map[int64]publication.Attempt{}
	uncertain := false
	for _, attempt := range attempts {
		if strings.EqualFold(attempt.Actor, observation.Actor) && attempt.Status == publication.Uncertain {
			uncertain = true
		}
		if attempt.Status == publication.Published && strings.EqualFold(attempt.Actor, observation.Actor) {
			owned[attempt.GitHubID] = attempt
		}
	}
	type evidence struct{ current, older, unknown bool }
	sources := map[string]evidence{}
	add := func(source, head string) {
		value := sources[source]
		if pr.HeadOID == "" || pr.MetadataObservedAt.IsZero() || head == "" {
			value.unknown = true
		} else if head == pr.HeadOID {
			value.current = true
		} else {
			value.older = true
		}
		sources[source] = value
	}
	seen := map[int64]bool{}
	for _, posted := range observation.Reviews {
		if posted.State == "PENDING" || posted.SubmittedAt.IsZero() {
			continue
		}
		source := "GitHub"
		if attempt, ok := owned[posted.ID]; ok && (posted.HeadOID == "" || attempt.Identity.HeadOID == posted.HeadOID) {
			source = "PR Board"
		}
		add(source, posted.HeadOID)
		seen[posted.ID] = true
	}
	// A confirmed local POST can be newer than the cached GitHub observation.
	// Older missing records must not resurrect reviews deleted on GitHub.
	for id, attempt := range owned {
		if !seen[id] && attempt.StartedAt.After(observation.ObservedAt) {
			add("PR Board", attempt.Identity.HeadOID)
		}
	}
	var names, details []string
	for _, source := range []string{"GitHub", "PR Board"} {
		value, exists := sources[source]
		if !exists {
			continue
		}
		names = append(names, source)
		revision := "revision unknown"
		if value.current {
			revision = "current revision"
		} else if value.older && !value.unknown {
			revision = "older revision"
		}
		details = append(details, source+" · "+revision)
	}
	label := "–"
	if len(names) == 1 {
		label = names[0]
	} else if len(names) == 2 {
		label = "Both"
	}
	detail := strings.Join(details, "; ")
	if uncertain {
		if detail != "" {
			detail += "; "
		}
		return "?", detail + "PR Board publication outcome uncertain"
	}
	if !observation.Complete {
		if detail != "" {
			detail += "; "
		}
		return "?", detail + "GitHub review history incomplete"
	}
	if detail == "" {
		detail = "No submitted reviews"
	}
	return label, detail
}
