package board

import (
	"context"
	"os"
	"path/filepath"
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
	if m.reviewPanel.setup == nil || m.reviewPanel.setup.builtin == nil {
		t.Fatal("first review did not offer Pi setup")
	}
	for _, width := range []int{30, 100} {
		m.width = width
		lines := strings.Split(stripANSI(m.View()), "\n")
		if !strings.Contains(lines[3], "Reviewer:") || !strings.Contains(lines[5], "Allow comment") {
			t.Fatalf("setup row coordinates changed: %v", lines)
		}
		for _, line := range lines {
			if lipgloss.Width(line) > width {
				t.Fatalf("setup overflows width %d: %q", width, line)
			}
		}
	}
	m.width = 100
	updated, _ := m.Update(tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, X: 1, Y: 5})
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

func (f *publicationFake) Publish(_ context.Context, _ string, runID string, action config.PublicationAction) (publication.Attempt, error) {
	f.runID, f.action = runID, action
	return publication.Attempt{Action: action, Status: publication.Published}, nil
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
		lines := strings.Split(stripANSI(m.View()), "\n")
		for row := 4; row <= 7; row++ {
			if !strings.HasPrefix(strings.TrimSpace(lines[row]), want) {
				t.Fatalf("permission state hidden at width 30: row %d = %q", row, lines[row])
			}
		}
	}
}
