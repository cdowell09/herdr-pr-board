package board

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"time"

	"github.com/cdowell09/herdr-pr-board/internal/config"
	"github.com/cdowell09/herdr-pr-board/internal/reviewinstructions"
	tea "github.com/charmbracelet/bubbletea"
)

// Essential rows keep fixed indexes and come first. One global view row follows
// for each configured view. The Advanced file rows come last.
const (
	repositoryReviewerRow = iota
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
	installed map[string]bool
	editing   *instructionEditor
	row       int
	saving    bool
	views     []config.View
	automatic config.AutomaticViewsEdit
	offset    int
}

// missing reports that the agent program of a reviewer is absent from PATH.
// A reviewer that names its own program reports no probe and stays available.
func (s *repositorySetup) missing(reviewer config.Reviewer) bool {
	executable := reviewer.Executable()
	return executable != "" && !s.installed[executable]
}

// available reports that setup can select a reviewer as the default. A custom
// command names its own program, so it always counts as available. A built-in
// reviewer needs its agent program on PATH. A built-in reviewer that setup
// cannot probe stays unavailable, because PATH cannot prove that it runs.
func (s *repositorySetup) available(reviewer config.Reviewer) bool {
	if reviewer.Builtin() == "" {
		return true
	}
	executable := reviewer.Executable()
	return executable != "" && s.installed[executable]
}

// noAgentInstalled reports that PATH holds no built-in agent program while a
// built-in reviewer is selected. A custom command needs no built-in program.
func (s *repositorySetup) noAgentInstalled() bool {
	return len(s.installed) == 0 && s.selectedReviewer().Builtin() != ""
}

// Monitor state matters only when this repository or the global views ask for
// automatic launches. Manual reviews need no monitor.
func (s *repositorySetup) automationSelected() bool {
	return s.repo.AutoLaunch || len(s.automatic.Selected) > 0
}

// The Advanced rows follow the last global view row.
func (s *repositorySetup) promptRow() int { return repositoryViewsRow + len(s.views) }

func (s *repositorySetup) skillRow() int { return s.promptRow() + 1 }

// Report the global view a row selects. All other rows select no view.
func (s *repositorySetup) globalView(row int) (config.View, bool) {
	if row < repositoryViewsRow || row >= s.promptRow() {
		return config.View{}, false
	}
	return s.views[row-repositoryViewsRow], true
}

type repositorySettingsMsg struct {
	url string
	cfg config.Config
	// installed holds each built-in agent program found on PATH.
	installed map[string]bool
	force     bool
	err       error
}
type repositorySavedMsg struct {
	url string
	cfg config.Config
	err error
}

// installedAgents reports each built-in agent program that PATH holds. The
// probe reads no configuration and sends no GitHub request.
func installedAgents(lookPath func(string) (string, error)) map[string]bool {
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	installed := map[string]bool{}
	for _, executable := range config.DetectableExecutables() {
		if _, err := lookPath(executable); err == nil {
			installed[executable] = true
		}
	}
	return installed
}

// The PATH probe runs in this command, never on the update or render path.
func (m Model) repositorySettingsCmd(force bool) tea.Cmd {
	path, url, lookPath := m.configPath, m.reviewPanel.pr.URL, m.lookPath
	return func() tea.Msg {
		cfg, err := config.LoadExisting(path)
		return repositorySettingsMsg{url: url, cfg: cfg, installed: installedAgents(lookPath), force: force, err: err}
	}
}

func newRepositorySetup(cfg config.Config, name string, installed map[string]bool) (*repositorySetup, error) {
	repo, exists := cfg.RepositoryFor(name)
	s := &repositorySetup{repo: repo, original: slices.Clone(cfg.Reviewers), installed: installed, views: cfg.Views, automatic: config.AutomaticViewsEdit{Selected: slices.Clone(cfg.Review.AutoViews), Expected: cfg}}
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
	// A saved selection stays, even when its program is absent. Only a repository
	// with no saved reviewer takes the first available reviewer, in configuration
	// order. The first reviewer stays the default when none is available.
	if s.repo.Reviewer == "" {
		s.repo.Reviewer = s.reviewers[0].ID
		for _, reviewer := range s.reviewers {
			if s.available(reviewer) {
				s.repo.Reviewer = reviewer.ID
				break
			}
		}
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
			setup, err := newRepositorySetup(msg.cfg, m.reviewPanel.pr.Repository, msg.installed)
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
	case "left":
		s.toggle(-1)
	case "right", " ":
		s.toggle(1)
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

// toggle changes the selected row. Only the reviewer row has an order, so step
// selects the next reviewer for 1 and the previous reviewer for -1. Every other
// row ignores step and keeps one action for the left, right, and Space keys.
func (s *repositorySetup) toggle(step int) {
	if view, ok := s.globalView(s.row); ok {
		if i := slices.Index(s.automatic.Selected, view.ID); i >= 0 {
			s.automatic.Selected = slices.Delete(s.automatic.Selected, i, i+1)
		} else {
			s.automatic.Selected = append(s.automatic.Selected, view.ID)
		}
		return
	}
	switch s.row {
	case repositoryReviewerRow:
		count := len(s.reviewers)
		if i := s.reviewerIndex(); i >= 0 {
			s.repo.Reviewer = s.reviewers[((i+step)%count+count)%count].ID
		}
	case repositoryAutomaticRow:
		s.repo.AutoLaunch = !s.repo.AutoLaunch
	case repositoryPermissionsRow, repositoryApprovalRow, repositoryChangesRow:
		action := config.PublicationActions()[s.row-repositoryPermissionsRow]
		if index := slices.Index(s.repo.PublishActions, action); index >= 0 {
			s.repo.SetPublishActions(slices.Delete(s.repo.PublishActions, index, index+1))
		} else {
			s.repo.PublishActions = append(s.repo.PublishActions, action)
		}
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
	default: // The Advanced prompt and skill rows.
		if s.selectedReviewer().Builtin() != "" {
			prompt, skill := s.instructionFiles()
			value := prompt
			if s.row == s.skillRow() {
				value = skill
			}
			s.editing = &instructionEditor{value: []rune(value), cursor: len([]rune(value))}
		}
	}
}

func (s *repositorySetup) rows() []string {
	rows := []string{s.reviewerLabel(), repositoryToggleLabel("Automatic launches", s.repo.AutoLaunch)}
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
	return append(rows, "Prompt file: "+prompt, "Skill file: "+skill)
}

// reviewerIndex reports the position of the selected reviewer in the cycle.
func (s *repositorySetup) reviewerIndex() int {
	return slices.IndexFunc(s.reviewers, func(r config.Reviewer) bool { return r.ID == s.repo.Reviewer })
}

// The reviewer row names the selection, its position in the cycle, and its
// install status. The position tells the user how many reviewers remain.
func (s *repositorySetup) reviewerLabel() string {
	label := fmt.Sprintf("Reviewer: %s (%d/%d)", s.repo.Reviewer, s.reviewerIndex()+1, len(s.reviewers))
	if s.missing(*s.selectedReviewer()) {
		label += " · not installed"
	}
	return label
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
		if s.row == s.promptRow() {
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
