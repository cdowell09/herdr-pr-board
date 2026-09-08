package board

import (
	"github.com/cdowell09/herdr-pr-board/internal/reviewmemory"
	tea "github.com/charmbracelet/bubbletea"
)

type reviewStoppedMsg struct {
	url, runID string
	generation uint64
	err        error
}

// The stop action targets the newest running attempt shown for this PR.
func (p *reviewPanel) stopTarget() (reviewmemory.Run, bool) {
	for i := len(p.runs) - 1; i >= 0; i-- {
		if p.runs[i].Status == reviewmemory.Running {
			return p.runs[i], true
		}
	}
	return reviewmemory.Run{}, false
}

func (m Model) stopReviewCmd() (tea.Model, tea.Cmd) {
	run, ok := m.reviewPanel.stopTarget()
	if !ok || m.reviews == nil || m.reviewPanel.stopping != "" {
		return m, nil
	}
	m.reviewPanel.stopping = run.ID
	m.reviewPanel.message = "Requesting stop for run " + shortRevision(run.ID)
	m.clampReviewOffset()
	backend, url, generation := m.reviews, m.reviewPanel.pr.URL, m.reviewGeneration
	return m, func() tea.Msg {
		return reviewStoppedMsg{url: url, runID: run.ID, generation: generation, err: backend.Stop(run.ID)}
	}
}

func (m Model) updateReviewStopped(msg reviewStoppedMsg) (Model, tea.Cmd, bool) {
	p := m.reviewPanel
	if p == nil || p.pr.URL != msg.url || m.reviewGeneration != msg.generation || p.stopping != msg.runID {
		return m, nil, true
	}
	p.message = "Stop requested for run " + shortRevision(msg.runID) + "; waiting for reviewer cleanup"
	if msg.err != nil {
		p.stopping = ""
		p.message = "Cannot stop run " + shortRevision(msg.runID) + ": " + msg.err.Error()
	}
	m.clampReviewOffset()
	return m, m.reviewHistoryCmd(msg.url), true
}
