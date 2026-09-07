package board

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/cdowell09/herdr-pr-board/internal/config"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

type repositorySetup struct {
	repo      config.Repository
	expected  *config.Repository
	reviewers []config.Reviewer
	builtin   *config.Reviewer
	row       int
	saving    bool
}

type repositorySettingsMsg struct {
	url   string
	cfg   config.Config
	force bool
	err   error
}
type repositorySavedMsg struct {
	url string
	cfg config.Config
	err error
}

func (m Model) repositorySettingsCmd(force bool) tea.Cmd {
	path, url := m.configPath, m.reviewPanel.pr.URL
	return func() tea.Msg {
		cfg, err := config.LoadExisting(path)
		return repositorySettingsMsg{url, cfg, force, err}
	}
}

func newRepositorySetup(cfg config.Config, name string) (*repositorySetup, error) {
	repo, exists := cfg.RepositoryFor(name)
	s := &repositorySetup{repo: repo, reviewers: cfg.Reviewers}
	if exists {
		s.expected = &repo
	}
	s.repo.PublishActions = append([]config.PublicationAction(nil), repo.PublishActions...)
	if len(s.reviewers) == 0 {
		binary, err := os.Executable()
		if err != nil {
			return nil, err
		}
		s.builtin = &config.Reviewer{ID: "pi", Command: []string{binary, "--pi-reviewer"}}
		s.reviewers = []config.Reviewer{*s.builtin}
	}
	if s.repo.Reviewer == "" {
		s.repo.Reviewer = s.reviewers[0].ID
	}
	return s, nil
}

func (m Model) updateRepository(message tea.Msg) (Model, tea.Cmd, bool) {
	switch msg := message.(type) {
	case repositorySettingsMsg:
		if m.reviewPanel == nil || m.reviewPanel.pr.URL != msg.url {
			return m, nil, true
		}
		m.reviewPanel.settingsLoading = false
		if msg.err != nil {
			m.reviewPanel.message = msg.err.Error()
			return m, nil, true
		}
		m.cfg.Reviewers, m.cfg.Repositories = msg.cfg.Reviewers, msg.cfg.Repositories
		_, exists := msg.cfg.RepositoryFor(m.reviewPanel.pr.Repository)
		if msg.force || !exists {
			setup, err := newRepositorySetup(msg.cfg, m.reviewPanel.pr.Repository)
			if err != nil {
				m.reviewPanel.message = err.Error()
			} else {
				m.reviewPanel.setup = setup
				m.reviewPanel.message = ""
			}
		}
		return m, nil, true
	case repositorySavedMsg:
		if msg.err == nil {
			m.cfg.Reviewers, m.cfg.Repositories = msg.cfg.Reviewers, msg.cfg.Repositories
		}
		if m.reviewPanel == nil || m.reviewPanel.pr.URL != msg.url {
			return m, nil, true
		}
		if m.reviewPanel.setup != nil {
			m.reviewPanel.setup.saving = false
		}
		if msg.err != nil {
			m.reviewPanel.message = msg.err.Error()
		} else {
			m.reviewPanel.setup = nil
			m.reviewPanel.message = "Repository settings saved. Press n to run a review."
		}
		return m, nil, true
	}
	return m, nil, false
}

func (m Model) updateRepositoryKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	s := m.reviewPanel.setup
	if key.String() == "q" || key.String() == "ctrl+c" {
		return m, tea.Quit
	}
	if s.saving {
		return m, nil
	}
	switch key.String() {
	case "esc":
		m.reviewPanel.setup = nil
	case "up", "k":
		s.row = max(0, s.row-1)
	case "down", "j":
		s.row = min(5, s.row+1)
	case "left", "right", " ":
		s.toggle()
	case "enter":
		s.saving = true
		path, state, url, repo, builtin, expected := m.configPath, m.stateDir, m.reviewPanel.pr.URL, s.repo, s.builtin, s.expected
		return m, func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			cfg, err := config.SaveRepository(ctx, path, state, repo, builtin, expected)
			return repositorySavedMsg{url, cfg, err}
		}
	}
	return m, nil
}

func (s *repositorySetup) toggle() {
	switch s.row {
	case 0:
		for i, r := range s.reviewers {
			if r.ID == s.repo.Reviewer {
				s.repo.Reviewer = s.reviewers[(i+1)%len(s.reviewers)].ID
				break
			}
		}
	case 1:
		s.repo.AutoLaunch = !s.repo.AutoLaunch
	case 5:
		choices := []config.PublicationAction{"", config.PublishComment}
		for _, action := range config.PublicationActions() {
			if action != config.PublishComment && slices.Contains(s.repo.PublishActions, action) {
				choices = append(choices, action)
			}
		}
		index := slices.Index(choices, s.repo.AutoPublish)
		s.repo.AutoPublish = choices[(index+1)%len(choices)]
		if s.repo.AutoPublish == config.PublishComment && !slices.Contains(s.repo.PublishActions, config.PublishComment) {
			s.repo.PublishActions = append(s.repo.PublishActions, config.PublishComment)
		}
	default:
		action := config.PublicationActions()[s.row-2]
		if index := slices.Index(s.repo.PublishActions, action); index >= 0 {
			s.repo.SetPublishActions(slices.Delete(s.repo.PublishActions, index, index+1))
		} else {
			s.repo.PublishActions = append(s.repo.PublishActions, action)
		}
	}
}

func (m Model) repositoryRows() []string {
	s := m.reviewPanel.setup
	rows := []string{"Reviewer: " + s.repo.Reviewer, repositoryToggleLabel("Automatic launches", s.repo.AutoLaunch)}
	for _, action := range config.PublicationActions() {
		rows = append(rows, repositoryToggleLabel(fmt.Sprintf("Allow %s publication", action), slices.Contains(s.repo.PublishActions, action)))
	}
	publication := string(s.repo.AutoPublish)
	if publication == "" {
		publication = "local only"
	}
	rows = append(rows, publication+" · Automatic publication")
	for i := range rows {
		if i == s.row {
			rows[i] = "› " + rows[i]
		} else {
			rows[i] = "  " + rows[i]
		}
		rows[i] = truncate(rows[i], m.width)
	}
	return rows
}

func (m Model) renderRepositoryPanel() string {
	s := m.reviewPanel.setup
	lines := []string{titleStyle.Render("Repository settings"), urlStyle.Render(truncate(m.reviewPanel.pr.URL, m.width)), ""}
	lines = append(lines, m.repositoryRows()...)
	if s.builtin != nil {
		lines = append(lines, "Pi setup adds a reusable reviewer. Pi and its review skill must be installed.")
	}
	if m.reviewPanel.message != "" {
		lines = append(lines, reviewText(m.reviewPanel.message))
	}
	if s.saving {
		lines = append(lines, "Saving…")
	}
	lines = append(lines, "↑/↓ select · Space/←/→ change · Enter save · Esc cancel · q quit")
	for i, line := range lines {
		if i >= 9 {
			lines[i] = ansi.Wrap(line, max(1, m.width), "")
		}
	}
	return strings.Join(lines, "\n")
}

func (m Model) updateRepositoryMouse(message tea.MouseMsg) (tea.Model, tea.Cmd) {
	event := tea.MouseEvent(message)
	if event.Button == tea.MouseButtonLeft && event.Action == tea.MouseActionPress && !m.reviewPanel.setup.saving {
		if event.Y == 1 {
			return m, m.openBrowser(m.reviewPanel.pr.URL)
		}
		row := event.Y - 3
		if row >= 0 && row < len(m.repositoryRows()) {
			m.reviewPanel.setup.row = row
			m.reviewPanel.setup.toggle()
		}
	}
	return m, nil
}

func repositoryToggleLabel(label string, enabled bool) string {
	indicator := "[ ]"
	if enabled {
		indicator = "[x]"
	}
	return indicator + " " + label
}
