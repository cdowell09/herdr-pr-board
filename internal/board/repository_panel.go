package board

import (
	"context"
	"os"
	"slices"
	"time"

	"github.com/cdowell09/herdr-pr-board/internal/config"
	"github.com/cdowell09/herdr-pr-board/internal/reviewinstructions"
	tea "github.com/charmbracelet/bubbletea"
)

const (
	repositoryReviewerRow = iota
	repositoryPromptRow
	repositorySkillRow
	repositoryAutomaticRow
	repositoryPermissionsRow
	repositoryApprovalRow
	repositoryChangesRow
	repositoryPostingRow
	repositoryViewsRow
)

type instructionEditor struct {
	value  []rune
	cursor int
}

type repositorySetup struct {
	repo      config.Repository
	expected  *config.Repository
	reviewers []config.Reviewer
	original  []config.Reviewer
	files     map[string]reviewinstructions.Files
	editing   *instructionEditor
	row       int
	saving    bool
	views     []config.View
	automatic config.AutomaticViewsEdit
	offset    int
}

// Monitor state matters only when this repository or the global views ask for
// automatic launches. Manual reviews need no monitor.
func (s *repositorySetup) automationSelected() bool {
	return s.repo.AutoLaunch || len(s.automatic.Selected) > 0
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
	s := &repositorySetup{repo: repo, original: slices.Clone(cfg.Reviewers), views: cfg.Views, automatic: config.AutomaticViewsEdit{Selected: slices.Clone(cfg.Review.AutoViews), Expected: cfg}}
	if exists {
		s.expected = &repo
	}
	s.repo.PublishActions = append([]config.PublicationAction(nil), repo.PublishActions...)
	binary, err := os.Executable()
	if err != nil {
		return nil, err
	}
	s.reviewers = slices.Clone(cfg.Reviewers)
	for _, builtin := range config.BuiltinReviewers(binary) {
		if !slices.ContainsFunc(cfg.Reviewers, func(r config.Reviewer) bool { return r.ID == builtin.ID }) {
			s.reviewers = append(s.reviewers, builtin)
		}
	}
	s.files = make(map[string]reviewinstructions.Files, len(s.reviewers))
	for _, reviewer := range s.reviewers {
		prompt, skill, err := reviewer.InstructionFiles()
		if err != nil {
			return nil, err
		}
		s.files[reviewer.ID] = reviewinstructions.Files{Prompt: prompt, Skill: skill}
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
		var start tea.Cmd
		if msg.err == nil {
			start = m.startMonitorCmd()
			m.cfg.Reviewers, m.cfg.Repositories = msg.cfg.Reviewers, msg.cfg.Repositories
			m.cfg.Review.AutoViews = slices.Clone(msg.cfg.Review.AutoViews)
		}
		if m.reviewPanel == nil || m.reviewPanel.pr.URL != msg.url {
			return m, start, true
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
		return m, tea.Batch(start, m.monitorStatusCmd()), true
	}
	return m, nil, false
}

func (m Model) updateRepositoryKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	s := m.reviewPanel.setup
	if key.String() == "ctrl+c" || s.editing == nil && key.String() == "q" {
		return m, tea.Quit
	}
	if s.saving {
		return m, nil
	}
	if s.editing != nil {
		s.editInstruction(key)
		m.revealRepositoryRow()
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
		path, state, url, repo, reviewer, expected := m.configPath, m.stateDir, m.reviewPanel.pr.URL, s.repo, s.reviewerEdit(), s.expected
		return m, func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			cfg, err := config.SaveRepository(ctx, path, state, repo, reviewer, expected, &automatic)
			return repositorySavedMsg{url, cfg, err}
		}
	}
	m.revealRepositoryRow()
	return m, nil
}

func (s *repositorySetup) toggle() {
	switch s.row {
	case repositoryReviewerRow:
		for i, r := range s.reviewers {
			if r.ID == s.repo.Reviewer {
				s.repo.Reviewer = s.reviewers[(i+1)%len(s.reviewers)].ID
				break
			}
		}
	case repositoryPromptRow, repositorySkillRow:
		reviewer := s.selectedReviewer()
		if reviewer.Builtin() != "" {
			prompt, skill := s.instructionFiles()
			value := prompt
			if s.row == repositorySkillRow {
				value = skill
			}
			s.editing = &instructionEditor{value: []rune(value), cursor: len([]rune(value))}
		}
	case repositoryAutomaticRow:
		s.repo.AutoLaunch = !s.repo.AutoLaunch
	case repositoryPostingRow:
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
		if s.row >= repositoryViewsRow {
			id := s.views[s.row-repositoryViewsRow].ID
			if i := slices.Index(s.automatic.Selected, id); i >= 0 {
				s.automatic.Selected = slices.Delete(s.automatic.Selected, i, i+1)
			} else {
				s.automatic.Selected = append(s.automatic.Selected, id)
			}
			return
		}
		action := config.PublicationActions()[s.row-repositoryPermissionsRow]
		if index := slices.Index(s.repo.PublishActions, action); index >= 0 {
			s.repo.SetPublishActions(slices.Delete(s.repo.PublishActions, index, index+1))
		} else {
			s.repo.PublishActions = append(s.repo.PublishActions, action)
		}
	}
}

func (s *repositorySetup) rows() []string {
	prompt, skill := s.instructionFiles()
	if prompt == "" {
		prompt = "Default review"
	}
	if skill == "" {
		skill = "None"
	}
	if s.selectedReviewer().Builtin() == "" {
		prompt, skill = "Custom command", "Custom command"
	}
	rows := []string{"Reviewer: " + s.repo.Reviewer, "Prompt file: " + prompt, "Skill file: " + skill, repositoryToggleLabel("Automatic launches", s.repo.AutoLaunch)}
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

func (s *repositorySetup) selectedBuiltin() *config.Reviewer {
	if slices.ContainsFunc(s.original, func(r config.Reviewer) bool { return r.ID == s.repo.Reviewer }) {
		return nil
	}
	return s.selectedReviewer()
}

func (s *repositorySetup) selectedReviewer() *config.Reviewer {
	for i := range s.reviewers {
		if s.reviewers[i].ID == s.repo.Reviewer {
			return &s.reviewers[i]
		}
	}
	panic("repository setup has no selected reviewer")
}

// Render only captured paths. Resolve legacy command paths when setup opens.
func (s *repositorySetup) instructionFiles() (prompt, skill string) {
	reviewer := s.selectedReviewer()
	files := s.files[reviewer.ID]
	if reviewer.PromptFile != nil {
		files.Prompt = *reviewer.PromptFile
	}
	if reviewer.SkillFile != nil {
		files.Skill = *reviewer.SkillFile
	}
	return files.Prompt, files.Skill
}

func (s *repositorySetup) reviewerEdit() *config.ReviewerEdit {
	selected := *s.selectedReviewer()
	for _, original := range s.original {
		if original.ID == selected.ID {
			if original.Equal(selected) {
				return nil
			}
			return &config.ReviewerEdit{Value: selected, Expected: &original}
		}
	}
	return &config.ReviewerEdit{Value: selected}
}

func (s *repositorySetup) editInstruction(key tea.KeyMsg) {
	e := s.editing
	switch key.Type {
	case tea.KeyEsc:
		s.editing = nil
	case tea.KeyEnter:
		value := string(e.value)
		if s.row == repositoryPromptRow {
			s.selectedReviewer().PromptFile = &value
		} else {
			s.selectedReviewer().SkillFile = &value
		}
		s.editing = nil
	case tea.KeyCtrlU:
		e.value, e.cursor = nil, 0
	case tea.KeyLeft:
		e.cursor = max(0, e.cursor-1)
	case tea.KeyRight:
		e.cursor = min(len(e.value), e.cursor+1)
	case tea.KeyHome, tea.KeyCtrlA:
		e.cursor = 0
	case tea.KeyEnd, tea.KeyCtrlE:
		e.cursor = len(e.value)
	case tea.KeyBackspace, tea.KeyCtrlH:
		if e.cursor > 0 {
			e.value = slices.Delete(e.value, e.cursor-1, e.cursor)
			e.cursor--
		}
	case tea.KeyDelete:
		if e.cursor < len(e.value) {
			e.value = slices.Delete(e.value, e.cursor, e.cursor+1)
		}
	default:
		if key.Type == tea.KeyRunes || key.Type == tea.KeySpace {
			value := key.Runes
			if key.Type == tea.KeySpace {
				value = []rune{' '}
			}
			e.value = slices.Insert(e.value, e.cursor, value...)
			e.cursor += len(value)
		}
	}
}
