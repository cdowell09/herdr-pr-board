package board

import (
	"github.com/cdowell09/herdr-pr-board/internal/reviewmemory"
	tea "github.com/charmbracelet/bubbletea"
)

type reviewStoppedMsg struct {
	url, runID string
	err        error
}

// The stop action targets the newest running attempt recorded for this PR.
func (m Model) stopTarget() (reviewmemory.Run, bool) {
	runs := m.regionState().runs
	for i := len(runs) - 1; i >= 0; i-- {
		if runs[i].Status == reviewmemory.Running {
			return runs[i], true
		}
	}
	return reviewmemory.Run{}, false
}

func (m Model) stopReviewCmd() (tea.Model, tea.Cmd) {
	run, ok := m.stopTarget()
	url := m.region.pr.URL
	if !ok || m.reviews == nil || m.stopping[url] != "" {
		return m, nil
	}
	m.stopping[url] = run.ID
	m.region.message = "Requesting stop for run " + shortRevision(run.ID)
	m.clampRegionOffset()
	backend := m.reviews
	return m, func() tea.Msg {
		return reviewStoppedMsg{url: url, runID: run.ID, err: backend.Stop(run.ID)}
	}
}

// updateReviewStopped records the owner's answer for the pending stop of
// this PR. The message reaches the region only while it shows that PR.
func (m Model) updateReviewStopped(msg reviewStoppedMsg) (Model, tea.Cmd, bool) {
	if m.stopping[msg.url] != msg.runID {
		return m, nil, true
	}
	message := "Stop requested for run " + shortRevision(msg.runID) + "; waiting for reviewer cleanup"
	if msg.err != nil {
		delete(m.stopping, msg.url)
		message = "Cannot stop run " + shortRevision(msg.runID) + ": " + msg.err.Error()
	}
	if m.region != nil && m.region.pr.URL == msg.url {
		m.region.message = message
		m.clampRegionOffset()
	}
	cmd := m.requestOverview()
	return m, cmd, true
}

// settleStops clears every pending stop whose run the latest read no longer
// records as running, whether or not a view lists its PR, and reports the
// outcome in the region that shows it. A failed read settles nothing.
func (m *Model) settleStops() {
	if m.overview.err != nil {
		return
	}
	for url, id := range m.stopping {
		run, recorded := m.overview.runs[id]
		if recorded && run.Status == reviewmemory.Running {
			continue
		}
		delete(m.stopping, url)
		if recorded && m.region != nil && m.region.pr.URL == url {
			m.region.message = "Run " + shortRevision(run.ID) + ": " + string(run.Status) + " · " + run.Message
		}
	}
}
