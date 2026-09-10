package board

import (
	"fmt"
	"strings"
	"testing"

	"github.com/cdowell09/herdr-pr-board/internal/config"
	tea "github.com/charmbracelet/bubbletea"
)

// The reviewer row reports the position and the total, so the user knows how
// many reviewers remain. Left cycles backward. Right and Space cycle forward.
func TestReviewerRowShowsPositionAndCyclesBothWays(t *testing.T) {
	m := panelModel(t)
	setup := setupWith(t, installedAgents(fakeLookPath("claude")))
	m.reviewPanel.setup = setup
	setup.row = repositoryReviewerRow
	total := len(config.BuiltinReviewers(""))
	if len(setup.reviewers) != total {
		t.Fatalf("setup offers %d reviewers, want %d", len(setup.reviewers), total)
	}
	press := func(key tea.KeyMsg) {
		t.Helper()
		next, _ := m.Update(key)
		m = next.(Model)
	}
	for _, step := range []struct {
		key      tea.KeyMsg
		reviewer string
		position int
		missing  bool
	}{
		{tea.KeyMsg{Type: tea.KeyRight}, "qwen", 4, true},
		{tea.KeyMsg{Type: tea.KeySpace}, "omp", 5, true},
		{tea.KeyMsg{Type: tea.KeyLeft}, "qwen", 4, true},
		{tea.KeyMsg{Type: tea.KeyLeft}, "claude", 3, false},
		{tea.KeyMsg{Type: tea.KeyLeft}, "codex", 2, true},
		{tea.KeyMsg{Type: tea.KeyLeft}, "pi", 1, true},
		{tea.KeyMsg{Type: tea.KeyLeft}, "grok", total, true},
		{tea.KeyMsg{Type: tea.KeyRight}, "pi", 1, true},
	} {
		press(step.key)
		want := fmt.Sprintf("Reviewer: %s (%d/%d)", step.reviewer, step.position, total)
		if step.missing {
			want += " · not installed"
		}
		if got := setup.rows()[repositoryReviewerRow]; got != want {
			t.Fatalf("after %s the row reads %q, want %q", step.key, got, want)
		}
		if !strings.Contains(stripANSI(m.View()), want) {
			t.Fatalf("panel omits %q:\n%s", want, stripANSI(m.View()))
		}
	}
}

// Only the reviewer row has an order. Every other row keeps one action for the
// left, right, and Space keys.
func TestOtherSetupRowsKeepOneActionForEveryKey(t *testing.T) {
	for _, key := range []tea.KeyMsg{{Type: tea.KeyLeft}, {Type: tea.KeyRight}, {Type: tea.KeySpace}} {
		m := panelModel(t)
		setup := setupWith(t, installedAgents(fakeLookPath("claude")))
		m.reviewPanel.setup = setup
		setup.row = repositoryAutomaticRow
		for _, want := range []bool{true, false} {
			next, _ := m.Update(key)
			m = next.(Model)
			if setup.repo.AutoLaunch != want {
				t.Fatalf("%s set automatic launches to %v, want %v", key, setup.repo.AutoLaunch, want)
			}
		}
	}
}
