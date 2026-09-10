package board

import (
	"github.com/cdowell09/herdr-pr-board/internal/discovery"
	"github.com/cdowell09/herdr-pr-board/internal/dispatch"
)

func (m *Model) invalidateAutomatic(snapshot discovery.ViewSnapshot) {
	for i := range m.autoCandidates {
		candidate := &m.autoCandidates[i]
		affected := false
		for _, view := range candidate.Views {
			if view.ID == snapshot.Data.View.ID {
				affected = true
			}
		}
		if !affected {
			continue
		}
		matches := false
		for _, pr := range snapshot.Data.PRs {
			if pr.URL == candidate.PR.URL && dispatch.Identity(pr) == dispatch.Identity(candidate.PR) && pr.BaseOID == candidate.PR.BaseOID && pr.State == candidate.PR.State && pr.Draft == candidate.PR.Draft {
				matches = true
			}
		}
		if snapshot.Data.Err != nil || len(snapshot.Errors) > 0 || !matches {
			candidate.Observed = false
		}
	}
}
