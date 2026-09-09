package board

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/cdowell09/herdr-pr-board/internal/config"
	"github.com/cdowell09/herdr-pr-board/internal/publication"
	"github.com/cdowell09/herdr-pr-board/internal/reviewmemory"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func TestFirstReviewSetupControlsAndSavedConfiguration(t *testing.T) {
	m := panelModel(t)
	m.configPath = filepath.Join(t.TempDir(), "selected.toml")
	m = m.WithPublications(t.TempDir(), nil)
	cfg, err := config.Load(m.configPath)
	if err != nil {
		t.Fatal(err)
	}
	next, _, _ := m.updateRepository(repositorySettingsMsg{url: m.reviewPanel.pr.URL, cfg: cfg})
	m = next
	if m.reviewPanel.setup == nil || m.reviewPanel.setup.selectedBuiltin() == nil {
		t.Fatal("first review did not offer Pi setup")
	}
	for _, width := range []int{30, 100} {
		m.width = width
		lines := strings.Split(stripANSI(m.View()), "\n")
		renderedRepositoryLine(t, m, "Reviewer:")
		renderedRepositoryLine(t, m, "[ ] Comments")
		for _, line := range lines {
			if lipgloss.Width(line) > width {
				t.Fatalf("setup overflows width %d: %q", width, line)
			}
		}
	}
	m.width = 100
	updated, _ := m.Update(tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, X: 1, Y: renderedRepositoryLine(t, m, "[ ] Comments")})
	m = updated.(Model)
	settings := m.reviewPanel.setup.repo
	if len(settings.PublishActions) != 1 || settings.PublishActions[0] != config.PublishComment || settings.AutoLaunch {
		t.Fatalf("unsafe publication defaults: %+v", settings)
	}
	updated, save := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if save == nil {
		t.Fatal("Enter did not save")
	}
	updated, _ = m.Update(save())
	m = updated.(Model)
	if m.reviewPanel.setup != nil || !strings.Contains(m.reviewPanel.message, "saved") {
		t.Fatalf("setup did not finish: %+v", m.reviewPanel)
	}
	data, err := os.ReadFile(m.configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), config.DefaultFile) {
		t.Fatal("setup changed unrelated configuration")
	}
	loaded, err := config.LoadExisting(m.configPath)
	if err != nil {
		t.Fatal(err)
	}
	updated, _, _ = m.updateRepository(repositorySettingsMsg{url: m.reviewPanel.pr.URL, cfg: loaded})
	if updated.(Model).reviewPanel.setup != nil {
		t.Fatal("saved repository prompted again")
	}
}

type publicationFake struct {
	action config.PublicationAction
	runID  string
}

func (*publicationFake) HistoryForRuns([]reviewmemory.Run) ([]publication.Attempt, error) {
	return nil, nil
}

func (f *publicationFake) Publish(_ context.Context, _ string, runID string, action config.PublicationAction) (publication.Attempt, error) {
	f.runID, f.action = runID, action
	return publication.Attempt{Action: action, Status: publication.Published}, nil
}
func (f *publicationFake) PublishConfigured(ctx context.Context, url, runID string) (publication.Attempt, error) {
	return f.Publish(ctx, url, runID, config.PublishComment)
}
func (*publicationFake) History(string) ([]publication.Attempt, error) { return nil, nil }

func TestPublicationControlsTargetLatestCompletion(t *testing.T) {
	m := panelModel(t)
	backend := &publicationFake{}
	m = m.WithPublications(t.TempDir(), backend)
	m.reviewPanel.runs = []reviewmemory.Run{{ID: "completed", Outcome: reviewmemory.Outcome{Status: reviewmemory.Completed}}, {ID: "failed", Outcome: reviewmemory.Outcome{Status: reviewmemory.Failed}}}
	for key, action := range map[string]config.PublicationAction{"c": config.PublishComment, "a": config.PublishApprove, "x": config.PublishRequestChanges} {
		updated, command := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
		m = updated.(Model)
		if command == nil {
			t.Fatalf("%s did not publish", key)
		}
		updated, _ = m.Update(command())
		m = updated.(Model)
		if backend.action != action || backend.runID != "completed" {
			t.Fatalf("published wrong action/run: %+v", backend)
		}
	}
}

func TestNarrowSetupKeepsEveryPermissionIndicatorVisible(t *testing.T) {
	m := panelModel(t)
	m.width = 30
	setup, err := newRepositorySetup(m.cfg, "acme/repo")
	if err != nil {
		t.Fatal(err)
	}
	m.reviewPanel.setup = setup
	for _, enabled := range []bool{false, true} {
		setup.repo.AutoLaunch = enabled
		setup.repo.PublishActions = nil
		want := "[ ]"
		if enabled {
			setup.repo.PublishActions = config.PublicationActions()
			want = "[x]"
		}
		for row := repositoryAutomaticRow; row <= repositoryChangesRow; row++ {
			setup.row = row
			m.revealRepositoryRow()
			header, content, start, size := m.repositoryViewport()
			lines := strings.Split(stripANSI(m.View()), "\n")
			found := false
			for i := start; i < min(len(content), start+size); i++ {
				if content[i].row == row && strings.Contains(lines[len(header)+i-start], want) {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("permission state hidden at width 30: row %d: %v", row, lines)
			}
		}
	}
}

// Find mouse coordinates in actual terminal output, including wrapped sections.
func renderedRepositoryLine(t *testing.T, m Model, text string) int {
	t.Helper()
	for y, line := range strings.Split(stripANSI(m.View()), "\n") {
		if strings.Contains(line, text) {
			return y
		}
	}
	t.Fatalf("missing %q in panel:\n%s", text, stripANSI(m.View()))
	return -1
}

func TestRepositorySetupOffersMissingAdaptersAndPreservesCustomCommands(t *testing.T) {
	for _, builtin := range config.BuiltinReviewers("") {
		selected := builtin.ID
		if selected == "pi" {
			continue // This fixture already has a custom Pi command.
		}
		t.Run(selected, func(t *testing.T) {
			m := panelModel(t)
			m.configPath = filepath.Join(t.TempDir(), "config.toml")
			m = m.WithPublications(t.TempDir(), nil)
			data := config.DefaultFile + "\n# Custom Pi must remain unchanged.\n[[reviewers]]\nid = \"pi\"\ncommand = [\"custom-pi\", \"custom argument\"]\n"
			if err := os.WriteFile(m.configPath, []byte(data), 0600); err != nil {
				t.Fatal(err)
			}
			cfg, err := config.LoadExisting(m.configPath)
			if err != nil {
				t.Fatal(err)
			}
			m, _, _ = m.updateRepository(repositorySettingsMsg{url: m.reviewPanel.pr.URL, cfg: cfg})
			setup := m.reviewPanel.setup
			if setup == nil || len(setup.reviewers) != len(config.BuiltinReviewers("")) || setup.selectedBuiltin() != nil {
				t.Fatalf("setup=%+v", setup)
			}
			for i := 0; i < len(setup.reviewers) && setup.repo.Reviewer != selected; i++ {
				next, _ := m.Update(tea.KeyMsg{Type: tea.KeySpace})
				m = next.(Model)
			}
			if setup.selectedBuiltin() == nil || setup.selectedBuiltin().ID != selected {
				t.Fatalf("selected builtin=%+v", setup.selectedBuiltin())
			}
			next, save := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
			m = next.(Model)
			if save == nil {
				t.Fatal("missing save")
			}
			next, _ = m.Update(save())
			m = next.(Model)
			if m.reviewPanel.setup != nil {
				t.Fatalf("save failed: %s", m.reviewPanel.message)
			}
			cfg, err = config.LoadExisting(m.configPath)
			if err != nil {
				t.Fatal(err)
			}
			repo, _ := cfg.RepositoryFor(m.reviewPanel.pr.Repository)
			if repo.Reviewer != selected || len(cfg.Reviewers) != 2 || cfg.Reviewers[0].Command[0] != "custom-pi" || cfg.Reviewers[1].ID != selected {
				t.Fatalf("unexpected saved selection: %+v %+v", cfg.Reviewers, repo)
			}
			saved, err := os.ReadFile(m.configPath)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.HasPrefix(saved, []byte(data)) {
				t.Fatal("changed existing configuration")
			}
		})
	}
}

func TestInstructionFilesRoundTripThroughBoardAndTOML(t *testing.T) {
	for _, builtin := range config.BuiltinReviewers("board") {
		t.Run(builtin.ID, func(t *testing.T) {
			m := panelModel(t)
			m.configPath = filepath.Join(t.TempDir(), "config.toml")
			m.stateDir = t.TempDir()
			builtin.Command = append(builtin.Command, "--"+builtin.ID+"-skill", "old-skill.md")
			builtin.ID = "security"
			command, _ := json.Marshal(builtin.Command)
			before := config.DefaultFile + "\n# Preserve this profile and its command.\n[[reviewers]]\nid = \"security\"\ncommand = " + string(command) + "\nprompt_file = \"initial.md\"\n\n[[repositories]]\nname = \"acme/repo\"\nreviewer = \"security\"\n[[repositories]]\nname = \"acme/other\"\nreviewer = \"security\"\n"
			if err := os.WriteFile(m.configPath, []byte(before), 0600); err != nil {
				t.Fatal(err)
			}
			prompt := "q jk security 日本語.md"
			for _, name := range []string{"initial.md", "old-skill.md", prompt, "direct.md"} {
				if err := os.WriteFile(filepath.Join(filepath.Dir(m.configPath), name), []byte("Review security issues."), 0600); err != nil {
					t.Fatal(err)
				}
			}
			load := func() config.Config {
				cfg, err := config.LoadExisting(m.configPath)
				if err != nil {
					t.Fatal(err)
				}
				return cfg
			}
			m, _, _ = m.updateRepository(repositorySettingsMsg{url: m.reviewPanel.pr.URL, cfg: load(), force: true})
			s := m.reviewPanel.setup
			if rows := strings.Join(s.rows(), "\n"); !strings.Contains(rows, "initial.md") || !strings.Contains(rows, "old-skill.md") {
				t.Fatalf("TOML and legacy selections missing: %s", rows)
			}
			if !strings.Contains(stripANSI(m.View()), "All repositories using security share these files.") {
				t.Fatal("shared profile impact is hidden")
			}
			key := func(kind tea.KeyType, value string) {
				next, _ := m.Update(tea.KeyMsg{Type: kind, Runes: []rune(value)})
				m = next.(Model)
			}
			s.row = repositoryPromptRow
			key(tea.KeySpace, "")
			key(tea.KeyCtrlU, "")
			key(tea.KeyRunes, prompt)
			key(tea.KeyEnter, "")
			if s.editing != nil || s.selectedReviewer().PromptFile == nil || *s.selectedReviewer().PromptFile != prompt {
				t.Fatal("path entry changed characters or failed to update the draft")
			}
			if data, err := os.ReadFile(m.configPath); err != nil || string(data) != before {
				t.Fatal("accepting a path saved settings before Enter save")
			}
			s.row = repositorySkillRow
			m.revealRepositoryRow()
			next, _ := m.Update(tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, X: 1, Y: renderedRepositoryLine(t, m, "Skill file:")})
			m = next.(Model)
			if s.editing == nil {
				t.Fatal("click did not edit the rendered skill row")
			}
			key(tea.KeyCtrlU, "")
			key(tea.KeyEnter, "")
			next, save := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
			m = next.(Model)
			next, _ = m.Update(save())
			m = next.(Model)
			if m.reviewPanel.setup != nil {
				t.Fatalf("save failed: %s", m.reviewPanel.message)
			}
			cfg := load()
			profile := cfg.Reviewers[0]
			if profile.PromptFile == nil || *profile.PromptFile != prompt || profile.SkillFile == nil || *profile.SkillFile != "" || !reflect.DeepEqual(profile.Command, builtin.Command) {
				t.Fatalf("saved profile differs: %+v", profile)
			}
			if len(cfg.Repositories) != 2 || cfg.Repositories[1].Reviewer != "security" {
				t.Fatal("changed the other repository")
			}
			data, err := os.ReadFile(m.configPath)
			if err != nil || !strings.Contains(string(data), "# Preserve this profile and its command.") || !strings.Contains(string(data), "command = "+string(command)) {
				t.Fatalf("changed unrelated TOML: %s %v", data, err)
			}
			if err := os.WriteFile(m.configPath, []byte(strings.Replace(string(data), prompt, "direct.md", 1)), 0600); err != nil {
				t.Fatal(err)
			}
			m, _, _ = m.updateRepository(repositorySettingsMsg{url: m.reviewPanel.pr.URL, cfg: load(), force: true})
			if rows := strings.Join(m.reviewPanel.setup.rows(), "\n"); !strings.Contains(rows, "Prompt file: direct.md") || !strings.Contains(rows, "Skill file: None") {
				t.Fatalf("direct TOML edits not reflected: %s", rows)
			}
		})
	}
}

func TestInstructionEditorKeepsCursorVisibleAndCancelsDraft(t *testing.T) {
	m := onboardingModel(t, 30, 10, 3)
	s := m.reviewPanel.setup
	s.repo.Reviewer = "pi"
	s.row = repositoryPromptRow
	s.toggle()
	path := strings.Repeat("日本語/", 30) + "left"
	for _, key := range []tea.KeyMsg{{Type: tea.KeyRunes, Runes: []rune(path)}, {Type: tea.KeyRunes, Runes: []rune("q")}, {Type: tea.KeySpace}} {
		next, _ := m.Update(key)
		m = next.(Model)
	}
	if string(s.editing.value) != path+"q " {
		t.Fatal("path text triggered a command")
	}
	view := stripANSI(m.View())
	if !strings.Contains(view, "leftq ▏") || len(strings.Split(view, "\n")) > m.height {
		t.Fatalf("cursor hidden or height exceeded: %s", view)
	}
	for _, line := range strings.Split(view, "\n") {
		if lipgloss.Width(line) > m.width {
			t.Fatalf("wide path overflow: %q", line)
		}
	}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(Model)
	if s.editing != nil || s.selectedReviewer().PromptFile != nil || m.reviewPanel.setup == nil {
		t.Fatal("Escape did not discard only the path draft")
	}
}

func TestMissingInstructionFileKeepsSetupOpenForRepair(t *testing.T) {
	m := panelModel(t)
	m.configPath = filepath.Join(t.TempDir(), "config.toml")
	m.stateDir = t.TempDir()
	cfg, err := config.Load(m.configPath)
	if err != nil {
		t.Fatal(err)
	}
	m, _, _ = m.updateRepository(repositorySettingsMsg{url: m.reviewPanel.pr.URL, cfg: cfg, force: true})
	missing := "missing-security.md"
	m.reviewPanel.setup.selectedReviewer().PromptFile = &missing
	next, save := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	next, _ = m.Update(save())
	m = next.(Model)
	if m.reviewPanel.setup == nil || m.reviewPanel.setup.saving || !strings.Contains(m.reviewPanel.message, missing) {
		t.Fatalf("missing file not repairable: %+v", m.reviewPanel)
	}
	data, err := os.ReadFile(m.configPath)
	if err != nil || string(data) != config.DefaultFile {
		t.Fatal("invalid path changed saved configuration")
	}
}
