package board

import (
	"context"
	"fmt"
	"strings"

	"github.com/cdowell09/herdr-pr-board/internal/config"
	gh "github.com/cdowell09/herdr-pr-board/internal/github"
	"github.com/cdowell09/herdr-pr-board/internal/publication"
	"github.com/cdowell09/herdr-pr-board/internal/reviewmemory"
	tea "github.com/charmbracelet/bubbletea"
)

type PublicationBackend interface {
	HistoryForRuns([]reviewmemory.Run) ([]publication.Attempt, error)
	PublishConfigured(context.Context, string, string) (publication.Attempt, error)
	Publish(context.Context, string, string, config.PublicationAction) (publication.Attempt, error)
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

func (m Model) updatePublication(message tea.Msg) (Model, tea.Cmd, bool) {
	switch msg := message.(type) {
	case publicationDoneMsg:
		message := string(msg.attempt.Status)
		if msg.err != nil {
			message = msg.err.Error()
		}
		m.warning = "publication: " + message
		delete(m.publishing, msg.url)
		if m.region != nil && m.region.pr.URL == msg.url {
			m.region.message = message
		}
		cmd := m.requestOverview()
		return m, cmd, true
	}
	return m, nil, false
}

// pendingPublication is a publication action that waits for confirmation.
type pendingPublication struct {
	pr     gh.PullRequest
	action config.PublicationAction
}

func (p pendingPublication) prompt() string {
	return fmt.Sprintf("%s %s#%d? enter/y", strings.ReplaceAll(string(p.action), "_", " "), p.pr.Repository, p.pr.Number)
}

// updateConfirm settles an armed publication. Enter or y publishes it while
// the region still shows its PR. Every other key cancels it.
func (m Model) updateConfirm(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	pending := *m.confirm
	m.confirm = nil
	if (key.String() != "enter" && key.String() != "y") || m.region == nil || m.region.pr.URL != pending.pr.URL {
		return m, nil
	}
	return m.publishCmd(pending.action)
}

// armPublication asks for confirmation of the action on the region PR.
func (m Model) armPublication(action config.PublicationAction) (tea.Model, tea.Cmd) {
	if _, ok := m.checkPublicationRun(); ok {
		m.confirm = &pendingPublication{m.region.pr, action}
	}
	return m, nil
}

// checkPublicationRun returns the run the region PR can publish now. When
// no completed run exists, it tells the user in the region message. Arming
// and publishing both check it, so a confirmation never publishes what the
// board can no longer publish.
func (m *Model) checkPublicationRun() (reviewmemory.Run, bool) {
	if m.publications == nil || m.publishing[m.region.pr.URL] {
		return reviewmemory.Run{}, false
	}
	run, ok := latestCompleted(m.regionState().runs)
	if !ok {
		m.region.message = "No completed local review is available for publication."
	}
	return run, ok
}

func (m Model) publishCmd(action config.PublicationAction) (tea.Model, tea.Cmd) {
	run, ok := m.checkPublicationRun()
	if !ok {
		return m, nil
	}
	url := m.region.pr.URL
	m.publishing[url] = true
	m.region.message = "Publishing " + string(action) + "…"
	backend, ctx := m.publications, m.reviewContext
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
