package board

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	gh "github.com/cdowell09/herdr-pr-board/internal/github"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var layoutWidths = []int{40, 50, 60, 70, 80, 100, 120, 160}

func layoutModel(t *testing.T, width int) Model {
	t.Helper()
	cfg := testConfig()
	now := time.Now()
	model, err := NewModel(cfg, fakeLoader{})
	if err != nil {
		t.Fatal(err)
	}
	model.views = []ViewData{
		{View: cfg.Views[0], PRs: []gh.PullRequest{
			{Repository: "cdowell09/herdr-pr-board", Number: 1234, Title: "Add responsive layouts and cell-width truncation", URL: "https://github.com/cdowell09/herdr-pr-board/pull/1234", Author: "cdowell09", UpdatedAt: now.Add(-2 * time.Hour), CI: gh.CISuccess},
			{Repository: "acme/web-ui", Number: 42, Title: "🎉 Ship the onboarding revamp", URL: "https://github.com/acme/web-ui/pull/42", Author: "ada", UpdatedAt: now.Add(-time.Hour), CI: gh.CIPending},
			{Repository: "acme/api-gateway", Number: 7, Title: "日本語のタイトルで広いグリフ", URL: "https://github.com/acme/api-gateway/pull/7", Author: "grace", UpdatedAt: now.Add(-30 * time.Minute), CI: gh.CIFailure, Draft: true},
		}},
		{View: cfg.Views[1]},
	}
	model.loading = false
	model.width = width
	model.height = 30
	return model
}

func TestModelLayoutsFitTerminalWidth(t *testing.T) {
	for _, width := range layoutWidths {
		model := layoutModel(t, width)
		output := model.View()
		for _, line := range strings.Split(output, "\n") {
			plain := stripANSI(line)
			got := lipgloss.Width(plain)
			if got > width {
				t.Fatalf("width %d: rendered line is %d cells wide:\n%q", width, got, line)
			}
		}
	}
}

func TestModelLayoutsDropColumnsIntentionally(t *testing.T) {
	cases := []struct {
		width   int
		present []string
		absent  []string
	}{
		{width: 40, present: []string{"TITLE", "PR", "CI"}, absent: []string{"REPOSITORY", "AUTHOR", "UPDATED"}},
		{width: 60, present: []string{"REPOSITORY", "TITLE", "UPDATED"}, absent: []string{"AUTHOR"}},
		{width: 80, present: []string{"REPOSITORY", "TITLE", "AUTHOR", "UPDATED"}},
		{width: 120, present: []string{"REPOSITORY", "TITLE", "AUTHOR", "UPDATED"}},
	}
	for _, tc := range cases {
		model := layoutModel(t, tc.width)
		header := ""
		for _, line := range strings.Split(model.View(), "\n") {
			plain := stripANSI(line)
			if strings.Contains(plain, "REPOSITORY") || strings.Contains(plain, "TITLE") {
				header = plain
				break
			}
		}
		if header == "" {
			t.Fatalf("width %d: no header rendered:\n%s", tc.width, model.View())
		}
		for _, want := range tc.present {
			if !strings.Contains(header, want) {
				t.Fatalf("width %d: header missing %q: %q", tc.width, want, header)
			}
		}
		for _, absent := range tc.absent {
			if strings.Contains(header, absent) {
				t.Fatalf("width %d: header unexpectedly keeps %q: %q", tc.width, absent, header)
			}
		}
	}
}

func TestModelURLRemainsVisibleInEveryLayout(t *testing.T) {
	for _, width := range layoutWidths {
		model := layoutModel(t, width)
		output := model.View()
		if !strings.Contains(output, "https://github.com/") {
			t.Fatalf("width %d: URL not visible:\n%s", width, output)
		}
		urlY := -1
		for y, line := range strings.Split(output, "\n") {
			if strings.Contains(line, "https://github.com/") {
				urlY = y
				break
			}
		}
		if urlY != model.selectedURLY() {
			t.Fatalf("width %d: rendered URL Y = %d, mouse Y = %d", width, urlY, model.selectedURLY())
		}
		if urlY >= model.height {
			t.Fatalf("width %d: rendered URL Y = %d exceeds height %d", width, urlY, model.height)
		}
	}
}

func TestModelMouseCoordinatesMatchRenderedOutputInEachLayout(t *testing.T) {
	for _, width := range []int{50, 80, 120} {
		model := layoutModel(t, width)
		lines := strings.Split(model.View(), "\n")

		tabLine := stripANSI(lines[tabRowY])
		for i, view := range model.views {
			label := model.tabLabel(i, view)
			idx := strings.Index(tabLine, label)
			if idx < 0 {
				t.Fatalf("width %d: tab %d label %q not rendered: %q", width, i, label, tabLine)
			}
			col := lipgloss.Width(tabLine[:idx])
			if got, ok := model.tabAtX(col); !ok || got != i {
				t.Fatalf("width %d: tabAtX(%d) = %d, %v, want %d", width, col, got, ok, i)
			}
			if i > 0 {
				if got, ok := model.tabAtX(col - 2); ok && got == i {
					t.Fatalf("width %d: tabAtX(%d) still selects tab %d", width, col-2, i)
				}
			}
		}

		firstRowY, urlY := -1, -1
		for y, line := range lines {
			plain := stripANSI(line)
			switch {
			case strings.Contains(plain, "#7"):
				firstRowY = y
			case strings.Contains(plain, "https://github.com/"):
				urlY = y
			}
		}
		if firstRowY != model.firstPRRow() {
			t.Fatalf("width %d: rendered first row Y = %d, mouse Y = %d", width, firstRowY, model.firstPRRow())
		}
		if urlY != model.selectedURLY() {
			t.Fatalf("width %d: rendered URL Y = %d, mouse Y = %d", width, urlY, model.selectedURLY())
		}

		updated, command := model.Update(tea.MouseMsg(tea.MouseEvent{
			X: 2, Y: firstRowY, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
		}))
		if command != nil {
			t.Fatalf("width %d: row click returned a command", width)
		}
		model = updated.(Model)
		if model.cursor != 0 {
			t.Fatalf("width %d: row click selected cursor %d, want 0", width, model.cursor)
		}

		updated, command = model.Update(tea.MouseMsg(tea.MouseEvent{
			X: 2, Y: urlY, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
		}))
		if command == nil {
			t.Fatalf("width %d: URL click did not return a browser command", width)
		}
		_ = updated
	}
}

func TestTruncateUsesTerminalCellWidth(t *testing.T) {
	cases := []struct {
		value string
		width int
		want  string
	}{
		{value: "abc", width: 3, want: "abc"},
		{value: "abcd", width: 3, want: "ab…"},
		{value: "🎉 party time", width: 6, want: "🎉 pa…"},
		{value: "日本語のタイトル", width: 4, want: "日…"},
		{value: "e\u0301x", width: 2, want: "e\u0301x"},
		{value: "e\u0301xtra", width: 2, want: "e\u0301…"},
		{value: "abc", width: 1, want: "…"},
		{value: "abc", width: 0, want: "…"},
	}
	for _, tc := range cases {
		got := truncate(tc.value, tc.width)
		if got != tc.want {
			t.Fatalf("truncate(%q, %d) = %q, want %q", tc.value, tc.width, got, tc.want)
		}
		if lipgloss.Width(got) > max(1, tc.width) {
			t.Fatalf("truncate(%q, %d) = %q is %d cells wide", tc.value, tc.width, got, lipgloss.Width(got))
		}
	}
}

var implementedKeys = documentedKeys

func TestREADMEDocumentsEveryImplementedKey(t *testing.T) {
	readme, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range implementedKeys {
		literal := "`" + key + "`"
		if !strings.Contains(string(readme), literal) {
			t.Fatalf("README.md does not document the %q control (missing %q)", key, literal)
		}
	}
}

func TestFooterListsEveryImplementedControl(t *testing.T) {
	model := layoutModel(t, 200)
	output := stripANSI(model.View())
	for _, key := range []string{"h/l", "g/G", "Home/End", "Ctrl+U", "/", "E", "Enter", "wheel", "quit"} {
		if !strings.Contains(output, key) {
			t.Fatalf("footer missing %q:\n%s", key, output)
		}
	}
}

func TestFooterNamesShortcutContexts(t *testing.T) {
	model := layoutModel(t, 200)
	footer := stripANSI(model.renderFooter())
	for _, want := range []string{
		"h/l ←/→ view", "j/k ↑/↓ select", "g/G Home/End first/last",
		"/ Enter filter", "Ctrl+U Esc clear", "Backspace edit", "E edit config",
		"r R refresh", "Enter o open", "wheel/click mouse", "q Ctrl+C quit",
	} {
		if !strings.Contains(footer, want) {
			t.Fatalf("footer missing %q:\n%s", want, footer)
		}
	}
}

func TestFooterWrapsWithinWidthAndHeight(t *testing.T) {
	for _, width := range []int{40, 60, 80, 120, 200} {
		model := layoutModel(t, width)
		for _, line := range model.footerHelpLines() {
			if got := lipgloss.Width(stripANSI(line)); got > width {
				t.Fatalf("width %d: footer line is %d cells wide:\n%q", width, got, line)
			}
		}
		if lines := len(strings.Split(model.View(), "\n")); lines > model.height {
			t.Fatalf("width %d: rendered %d lines in a %d-line terminal", width, lines, model.height)
		}
	}
}

func TestModelNarrowLayoutsFitStaleAndErrorLines(t *testing.T) {
	for _, width := range []int{40, 50, 60} {
		cfg := testConfig()
		model, err := NewModel(cfg, fakeLoader{})
		if err != nil {
			t.Fatal(err)
		}
		model.views = []ViewData{
			{View: cfg.Views[0], PRs: []gh.PullRequest{{Repository: "acme/api", Number: 1, Title: "Keep me"}}, Err: errors.New("GitHub search failed: timeout")},
			{View: cfg.Views[1], Err: errors.New("GitHub search failed: timeout")},
		}
		model.loading = false
		model.width, model.height = width, 30

		for active, want := range map[int]string{0: "stale", 1: "GitHub query failed"} {
			model.active = active
			output := model.View()
			if !strings.Contains(output, want) {
				t.Fatalf("width %d active %d: %q missing:\n%s", width, active, want, output)
			}
			for _, line := range strings.Split(output, "\n") {
				if lipgloss.Width(stripANSI(line)) > width {
					t.Fatalf("width %d active %d: rendered line is too wide:\n%q", width, active, line)
				}
			}
		}
	}
}
