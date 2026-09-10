package board

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestTitleBarShowsTheVersionAtTheWideAndMediumTiers(t *testing.T) {
	cases := []struct {
		width int
		want  bool
	}{
		{width: tierWide, want: true},
		{width: tierMedium, want: true},
		{width: tierNarrow, want: false},
	}
	for _, tc := range cases {
		model := layoutModel(t, tc.width).WithVersion("0.6.0")
		lines := strings.Split(model.View(), "\n")
		title := stripANSI(lines[0])
		if got := strings.Contains(title, "v0.6.0"); got != tc.want {
			t.Fatalf("width %d: title %q shows the version = %v, want %v", tc.width, title, got, tc.want)
		}
		if !strings.HasPrefix(title, model.cfg.UI.Title) {
			t.Fatalf("width %d: title %q lost the configured title", tc.width, title)
		}
		if got := lipgloss.Width(title); got > tc.width {
			t.Fatalf("width %d: title is %d cells wide: %q", tc.width, got, title)
		}
		if got := len(lines); got != len(strings.Split(layoutModel(t, tc.width).View(), "\n")) {
			t.Fatalf("width %d: the version changed the rendered line count to %d", tc.width, got)
		}
	}
}

// TestTitleBarKeepsTheVersionWhenTheConfiguredTitleIsLong renders a title that
// fills every cell the refresh status leaves. The version must stay visible.
func TestTitleBarKeepsTheVersionWhenTheConfiguredTitleIsLong(t *testing.T) {
	for _, width := range []int{tierMedium, tierWide, 160} {
		model := layoutModel(t, width).WithVersion("0.6.0")
		model.cfg.UI.Title = strings.Repeat("T", width-lipgloss.Width(refreshStatus))
		model.loading = true
		line := stripANSI(strings.Split(model.View(), "\n")[0])
		if !strings.Contains(line, "v0.6.0") {
			t.Fatalf("width %d: the long title hid the version: %q", width, line)
		}
		if !strings.Contains(line, "refresh") {
			t.Fatalf("width %d: the long title hid the refresh status: %q", width, line)
		}
		if got := lipgloss.Width(line); got > width {
			t.Fatalf("width %d: title line is %d cells wide: %q", width, got, line)
		}
	}
}

func TestTitleBarKeepsTheRefreshStatusInsideTheTerminal(t *testing.T) {
	for _, width := range []int{12, 20, tierNarrow, tierMedium, tierWide, 160} {
		for _, title := range []string{"Board", strings.Repeat("T", 67), strings.Repeat("T", 400)} {
			model := layoutModel(t, width).WithVersion("0.6.0")
			model.cfg.UI.Title = title
			model.loading = true
			line := stripANSI(strings.Split(model.View(), "\n")[0])
			if got := lipgloss.Width(line); got > width {
				t.Fatalf("width %d: title line is %d cells wide: %q", width, got, line)
			}
			if !strings.Contains(line, "refresh") {
				t.Fatalf("width %d: title line lost the refresh status: %q", width, line)
			}
		}
	}
}
