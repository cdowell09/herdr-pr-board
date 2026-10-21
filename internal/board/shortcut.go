package board

import (
	"context"
	"fmt"

	"github.com/cdowell09/herdr-pr-board/internal/config"
	gh "github.com/cdowell09/herdr-pr-board/internal/github"
	tea "github.com/charmbracelet/bubbletea"
)

type shortcutMsg struct {
	shortcut config.Shortcut
	pr       gh.PullRequest
	err      error
}

// WithShortcutLauncher enables command shortcuts. launch runs a command
// shortcut for a PR in a new Herdr pane. The board does not track the pane.
func (m Model) WithShortcutLauncher(launch func(context.Context, config.Shortcut, gh.PullRequest) error) Model {
	m.launch = launch
	return m
}

// shortcut returns the command shortcut bound to key.
func (m Model) shortcut(key string) (config.Shortcut, bool) {
	for _, shortcut := range m.cfg.Shortcuts {
		if shortcut.Key == key {
			return shortcut, true
		}
	}
	return config.Shortcut{}, false
}

// shortcutHelp lists the command shortcuts as footer and help controls.
func (m Model) shortcutHelp() []keyHelpEntry {
	entries := make([]keyHelpEntry, len(m.cfg.Shortcuts))
	for i, shortcut := range m.cfg.Shortcuts {
		entries[i] = keyHelpEntry{shortcut.Key, shortcut.Name}
	}
	return entries
}

func (m Model) launchShortcut(shortcut config.Shortcut, pr gh.PullRequest) tea.Cmd {
	launch := m.launch
	if launch == nil {
		return nil
	}
	return func() tea.Msg {
		return shortcutMsg{shortcut: shortcut, pr: pr, err: launch(context.Background(), shortcut, pr)}
	}
}

func (m Model) updateShortcut(message tea.Msg) (Model, tea.Cmd, bool) {
	msg, ok := message.(shortcutMsg)
	if !ok {
		return m, nil, false
	}
	m.notice = ""
	if msg.err != nil {
		m.warning = appendWarning(m.warning, msg.shortcut.Name+" failed: "+msg.err.Error())
		return m, nil, true
	}
	m.notice = fmt.Sprintf("%s → %s#%d", msg.shortcut.Name, msg.pr.Repository, msg.pr.Number)
	return m, nil, true
}
