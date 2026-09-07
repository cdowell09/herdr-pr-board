package board

import tea "github.com/charmbracelet/bubbletea"

type monitorStartedMsg struct{ err error }

// WithMonitorStarter enables startup only for the interactive executable.
// The starter reads saved opt-ins and bounds startup independently of board exit.
func (m Model) WithMonitorStarter(start func() error) Model {
	m.monitorStart = start
	return m
}

func (m Model) startMonitorCmd() tea.Cmd {
	start := m.monitorStart
	if start == nil {
		return nil
	}
	return func() tea.Msg { return monitorStartedMsg{err: start()} }
}

func (m Model) afterMonitorStart(next tea.Cmd) tea.Cmd {
	if start := m.startMonitorCmd(); start != nil {
		return tea.Sequence(start, next)
	}
	return next
}

func (m Model) updateMonitor(message tea.Msg) (Model, tea.Cmd, bool) {
	msg, ok := message.(monitorStartedMsg)
	if !ok {
		return m, nil, false
	}
	m.monitorError = ""
	if msg.err != nil {
		m.monitorError = "Monitor startup failed: " + msg.err.Error()
	}
	if m.reviewPanel != nil {
		m.clampReviewOffset()
		return m, m.monitorStatusCmd(), true
	}
	return m, nil, true
}
