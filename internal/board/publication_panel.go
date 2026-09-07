package board

import (
	"context"
	"fmt"

	"github.com/cdowell09/herdr-pr-board/internal/config"
	"github.com/cdowell09/herdr-pr-board/internal/publication"
	"github.com/cdowell09/herdr-pr-board/internal/reviewmemory"
	tea "github.com/charmbracelet/bubbletea"
)

type PublicationBackend interface {
	Publish(context.Context, string, string, config.PublicationAction) (publication.Attempt, error)
	History(string) ([]publication.Attempt, error)
}

type publicationHistoryMsg struct {
	url      string
	attempts []publication.Attempt
	err      error
}
type publicationDoneMsg struct {
	url     string
	attempt publication.Attempt
	err     error
}

func (m Model) WithPublications(stateDir string, backend PublicationBackend) Model {
	m.stateDir, m.publications = stateDir, backend
	return m
}

func (m Model) publicationHistoryCmd(url string) tea.Cmd {
	if m.publications == nil {
		return nil
	}
	backend := m.publications
	return func() tea.Msg { history, err := backend.History(url); return publicationHistoryMsg{url, history, err} }
}

func (m Model) updatePublication(message tea.Msg) (Model, tea.Cmd, bool) {
	switch msg := message.(type) {
	case publicationHistoryMsg:
		if m.reviewPanel != nil && m.reviewPanel.pr.URL == msg.url {
			m.reviewPanel.publications = msg.attempts
			if msg.err != nil {
				m.reviewPanel.message = msg.err.Error()
			}
		}
		return m, nil, true
	case publicationDoneMsg:
		message := string(msg.attempt.Status)
		if msg.err != nil {
			message = msg.err.Error()
		}
		m.warning = "publication: " + message
		if m.reviewPanel != nil && m.reviewPanel.pr.URL == msg.url {
			m.reviewPanel.publishing = false
			m.reviewPanel.message = message
		}
		return m, m.publicationHistoryCmd(msg.url), true
	}
	return m, nil, false
}

func (m Model) publishCmd(action config.PublicationAction) (tea.Model, tea.Cmd) {
	if m.publications == nil || m.reviewPanel.publishing {
		return m, nil
	}
	run, ok := latestCompleted(m.reviewPanel.runs)
	if !ok {
		m.reviewPanel.message = "No completed local review is available for publication."
		return m, nil
	}
	m.reviewPanel.publishing = true
	m.reviewPanel.message = "Publishing " + string(action) + "…"
	backend, ctx, url := m.publications, m.reviewContext, m.reviewPanel.pr.URL
	return m, func() tea.Msg {
		attempt, err := backend.Publish(ctx, url, run.ID, action)
		return publicationDoneMsg{url, attempt, err}
	}
}

func latestCompleted(runs []reviewmemory.Run) (reviewmemory.Run, bool) {
	for i := len(runs) - 1; i >= 0; i-- {
		if runs[i].Status == reviewmemory.Completed {
			return runs[i], true
		}
	}
	return reviewmemory.Run{}, false
}

func (m Model) publicationLines() []string {
	lines := []string{}
	if run, ok := latestCompleted(m.reviewPanel.runs); ok {
		lines = append(lines, "Publication target: latest completed run "+run.ID)
	}
	for _, a := range m.reviewPanel.publications {
		line := fmt.Sprintf("Publication %s: %s · run %s", a.Action, a.Status, a.RunID)
		if a.URL != "" {
			line += " · " + a.URL
		}
		lines = append(lines, line)
		if a.Message != "" {
			lines = append(lines, a.Message)
		}
	}
	return lines
}
