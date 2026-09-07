package board

import (
	"context"
	"os"
	"slices"
	"time"

	"github.com/cdowell09/herdr-pr-board/internal/config"
	tea "github.com/charmbracelet/bubbletea"
)

type repositorySetup struct {
	repo      config.Repository
	expected  *config.Repository
	reviewers []config.Reviewer
	builtin   *config.Reviewer
	row       int
	saving    bool
	views     []config.View
	automatic config.AutomaticViewsEdit
	offset    int
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
	s := &repositorySetup{repo: repo, reviewers: cfg.Reviewers, views: cfg.Views, automatic: config.AutomaticViewsEdit{Selected: slices.Clone(cfg.Review.AutoViews), Expected: cfg}}
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
		m.cfg.Review.AutoViews = slices.Clone(msg.cfg.Review.AutoViews)
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
			m.cfg.Review.AutoViews = slices.Clone(msg.cfg.Review.AutoViews)
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
		return m, m.monitorStatusCmd(), true
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
		return m, nil
	case "up", "k":
		s.row = max(0, s.row-1)
	case "down", "j":
		s.row = min(len(s.rows())-1, s.row+1)
	case "pgup":
		s.offset = max(0, s.offset-max(1, m.height-5))
		m.clampRepositoryOffset()
		return m, nil
	case "pgdown":
		s.offset += max(1, m.height-5)
		m.clampRepositoryOffset()
		return m, nil
	case "home", "g":
		s.row, s.offset = 0, 0
	case "end", "G":
		s.offset = len(m.repositoryContent())
		m.clampRepositoryOffset()
		return m, nil
	case "left", "right", " ":
		s.toggle()
	case "enter":
		s.saving = true
		automatic := s.automatic
		automatic.Selected = slices.Clone(automatic.Selected)
		path, state, url, repo, builtin, expected := m.configPath, m.stateDir, m.reviewPanel.pr.URL, s.repo, s.builtin, s.expected
		return m, func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			cfg, err := config.SaveRepository(ctx, path, state, repo, builtin, expected, &automatic)
			return repositorySavedMsg{url, cfg, err}
		}
	}
	m.revealRepositoryRow()
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
		if s.row >= 6 {
			id := s.views[s.row-6].ID
			if i := slices.Index(s.automatic.Selected, id); i >= 0 {
				s.automatic.Selected = slices.Delete(s.automatic.Selected, i, i+1)
			} else {
				s.automatic.Selected = append(s.automatic.Selected, id)
			}
			return
		}
		action := config.PublicationActions()[s.row-2]
		if index := slices.Index(s.repo.PublishActions, action); index >= 0 {
			s.repo.SetPublishActions(slices.Delete(s.repo.PublishActions, index, index+1))
		} else {
			s.repo.PublishActions = append(s.repo.PublishActions, action)
		}
	}
}

func (s *repositorySetup) rows() []string {
	rows := []string{"Reviewer: " + s.repo.Reviewer, repositoryToggleLabel("Automatic launches", s.repo.AutoLaunch)}
	for _, action := range config.PublicationActions() {
		label := ""
		switch action {
		case config.PublishComment:
			label = "Comments"
		case config.PublishApprove:
			label = "Approval"
		case config.PublishRequestChanges:
			label = "Change requests"
		}
		rows = append(rows, repositoryToggleLabel(label, slices.Contains(s.repo.PublishActions, action)))
	}
	publication := "Keep local"
	switch s.repo.AutoPublish {
	case config.PublishComment:
		publication = "Post comment"
	case config.PublishApprove:
		publication = "Approve PR"
	case config.PublishRequestChanges:
		publication = "Request changes"
	}
	rows = append(rows, "After review: "+publication)
	for _, view := range s.views {
		rows = append(rows, repositoryToggleLabel(view.ID+" · "+view.Title, slices.Contains(s.automatic.Selected, view.ID)))
	}
	return rows
}

func repositoryToggleLabel(label string, enabled bool) string {
	indicator := "[ ]"
	if enabled {
		indicator = "[x]"
	}
	return indicator + " " + label
}
