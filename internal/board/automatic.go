package board

import (
	"github.com/cdowell09/herdr-pr-board/internal/config"
	"github.com/cdowell09/herdr-pr-board/internal/discovery"
	"github.com/cdowell09/herdr-pr-board/internal/dispatch"
)

// automaticDecision runs with review-history retrieval, never inside View().
func (m Model) automaticDecision(url string) dispatch.Decision {
	cfg := m.cfg
	if m.configPath != "" {
		latest, err := config.LoadExisting(m.configPath)
		if err != nil {
			return dispatch.Decision{URL: url, Reason: err.Error()}
		}
		cfg = latest
	}
	for _, candidate := range m.autoCandidates {
		if candidate.PR.URL != url {
			continue
		}
		if !config.SameDiscovery(m.cfg, cfg) {
			candidate.Observed = false
		}
		return dispatch.Decisions([]dispatch.Candidate{candidate}, cfg, m.reviews)[0]
	}
	return dispatch.Decision{URL: url, Reason: "waiting for a full observation"}
}

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
