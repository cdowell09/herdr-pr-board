package board

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// helpSection is one group of controls in the ? overlay.
type helpSection struct {
	title   string
	entries []keyHelpEntry
}

// helpSections is the single source of truth for the ? overlay. It names every
// board control and every review panel control. The documentation drift test
// checks it against documentedKeys.
var helpSections = []helpSection{
	{"Board", []keyHelpEntry{
		{"1–9 Tab Shift+Tab h/l ←/→", "select a view"},
		{"j/k ↑/↓", "select a PR"},
		{"g/G Home/End", "select the first or last PR"},
		{"Enter o", "open the PR in a browser"},
		{"v", "open local reviews"},
		{"/", "start the filter"},
		{"Enter", "finish the filter"},
		{"Backspace", "remove one filter character"},
		{"Ctrl+U Esc", "clear the filter"},
		{"r R", "refresh one view or all views"},
		{"E", "edit the configuration"},
		{"wheel click", "select and open with the mouse"},
		{"?", "open or close this help"},
		{"q Ctrl+C", "close the board"},
	}},
	{"Review panel", []keyHelpEntry{
		{"n", "start a review"},
		{"N", "repeat a review"},
		{"t", "stop the newest active review"},
		{"s", "edit repository settings"},
		{"c", "publish a comment"},
		{"a", "publish an approval"},
		{"x", "publish a change request"},
		{"j/k ↑/↓ g/G Home/End wheel", "scroll the history"},
		{"o", "open the PR in a browser"},
		{"Esc v", "return to the board"},
		{"?", "open or close this help"},
		{"q Ctrl+C", "close the board"},
	}},
}

// helpOverlayLines renders every control as one styled line. A pair that does
// not fit the terminal width breaks after the keys, so narrow terminals keep
// the full key list.
func (m Model) helpOverlayLines() []string {
	width := max(1, m.width)
	var lines []string
	for i, section := range helpSections {
		if i > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, keyStyle.Render(truncate(section.title, width)))
		for _, entry := range section.entries {
			if lipgloss.Width(entry.keys+"  "+entry.action) <= width {
				lines = append(lines, keyStyle.Render(entry.keys)+"  "+dimStyle.Render(entry.action))
				continue
			}
			lines = append(lines, keyStyle.Render(truncate(entry.keys, width)))
			lines = append(lines, "  "+dimStyle.Render(truncate(entry.action, max(1, width-2))))
		}
	}
	return lines
}

// helpVisibleRows is the number of control lines between the title and the
// close hint.
func (m Model) helpVisibleRows() int {
	return max(1, m.height-2)
}

func (m *Model) clampHelpOffset() {
	m.helpOffset = max(0, min(m.helpOffset, max(0, len(m.helpOverlayLines())-m.helpVisibleRows())))
}

func (m Model) renderHelpOverlay() string {
	lines := m.helpOverlayLines()
	visible := m.helpVisibleRows()
	offset := max(0, min(m.helpOffset, max(0, len(lines)-visible)))
	body := []string{titleStyle.Render(truncate("Keyboard controls", m.width))}
	body = append(body, lines[offset:min(len(lines), offset+visible)]...)
	body = append(body, dimStyle.Render(truncate("? Esc close · j/k ↑/↓ scroll", m.width)))
	return strings.Join(body[:min(len(body), max(1, m.height))], "\n")
}

func (m Model) updateHelpKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "?", "esc":
		m.helpOverlay, m.helpOffset = false, 0
		return m, nil
	case "j", "down":
		m.helpOffset++
	case "k", "up":
		m.helpOffset--
	case "g", "home":
		m.helpOffset = 0
	case "G", "end":
		m.helpOffset = len(m.helpOverlayLines())
	}
	m.clampHelpOffset()
	return m, nil
}

func (m Model) updateHelpMouse(message tea.MouseMsg) (tea.Model, tea.Cmd) {
	switch tea.MouseEvent(message).Button {
	case tea.MouseButtonWheelUp:
		m.helpOffset -= mouseStep
	case tea.MouseButtonWheelDown:
		m.helpOffset += mouseStep
	}
	m.clampHelpOffset()
	return m, nil
}
