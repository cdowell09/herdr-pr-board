package board

import (
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// helpSection is one group of controls in the ? overlay.
type helpSection struct {
	title   string
	entries []keyHelpEntry
}

// helpSections is the single source of truth for the ? overlay. It names every
// board control and every review region control. The documentation drift test
// checks it against documentedKeys. The controls that work everywhere come
// last, once.
var helpSections = []helpSection{
	{"Board", []keyHelpEntry{
		{"1–9 Tab Shift+Tab h/l ←/→", "select a view"},
		{"↑/↓ Home/End", "select a PR"},
		{"j/k g/G PgUp/PgDn", "scroll the review region"},
		{"wheel", "select over the table, scroll over the review region"},
		{"Enter o", "open the PR in a browser"},
		{"v", "zoom the review region"},
		{"/", "start the filter"},
		{"↑/↓", "select a PR during the filter"},
		{"Enter", "keep the filter and finish it"},
		{"Backspace", "remove one filter character, or finish an empty filter"},
		{"Ctrl+U Esc", "clear the filter"},
		{"Ctrl+R R", "refresh one view or all views"},
		{"E", "edit the configuration"},
		{"click", "select a PR, or open the selected PR"},
		{"click a footer key", "press that key"},
	}},
	{"Review region", []keyHelpEntry{
		{"t", "stop the newest active review"},
		{"s", "edit repository settings"},
		{"c", "publish a comment"},
		{"a", "publish an approval"},
		{"x", "publish a change request"},
		{"Enter y", "confirm the publication; another key or a click cancels it"},
		{"j/k ↑/↓ g/G Home/End PgUp/PgDn wheel", "scroll the zoomed region"},
		{"o", "open the PR in a browser"},
		{"Esc v", "leave zoom"},
	}},
	{"General", []keyHelpEntry{
		{"?", "open or close this help"},
		{"q Ctrl+C", "close the board"},
	}},
}

const (
	helpTitle = "Keyboard controls"
	helpHint  = "? Esc close · j/k ↑/↓ scroll"
	helpClose = "[×]"
	// helpKeyColumn is the widest key column. A wider key takes its own row.
	helpKeyColumn = 25
)

var (
	helpKeyStyle    = lipgloss.NewStyle().Bold(true).Foreground(accentColor)
	helpBorderStyle = lipgloss.NewStyle().Foreground(subtleColor)
	// backdropStyle dims the screen behind the help.
	backdropStyle = lipgloss.NewStyle().Foreground(borderColor)
)

// helpLayout is the place of the help modal on the screen, borders
// included, and its content rows. One calculation serves the rendering, the
// scrolling, and the close control.
type helpLayout struct {
	x, y, width, height int
	lines               []string
}

// visibleRows is the number of content rows between the top edge and the
// close hint.
func (l helpLayout) visibleRows() int { return max(1, l.height-3) }

// offset is the first visible content row for a requested scroll offset.
func (l helpLayout) offset(requested int) int {
	return max(0, min(requested, len(l.lines)-l.visibleRows()))
}

func (m Model) helpSections() []helpSection {
	if shortcuts := m.shortcutHelp(); len(shortcuts) > 0 {
		// Shortcuts come before the controls that work everywhere.
		last := len(helpSections) - 1
		return append(slices.Clip(helpSections[:last]), helpSection{"Command shortcuts", shortcuts}, helpSections[last])
	}
	return helpSections
}

// helpLayout centers the modal. It is as wide as its longest control, and
// it keeps a margin to the screen edges.
func (m Model) helpLayout() helpLayout {
	keys, actions := 0, 0
	for _, section := range m.helpSections() {
		for _, entry := range section.entries {
			keys = max(keys, lipgloss.Width(entry.keys))
			actions = max(actions, lipgloss.Width(entry.action))
		}
	}
	marginX, marginY := 2, 1
	if m.width < tierNarrow {
		marginX = 1
	}
	if m.height < 12 {
		marginY = 0
	}
	// Each content row has one cell of padding on both sides, and two cells
	// separate the key column from the actions.
	natural := 2 + min(keys, helpKeyColumn) + 2 + actions + 2
	layout := helpLayout{width: max(1, min(natural, m.width-2*marginX))}
	layout.lines = m.helpLinesFor(layout.width)
	layout.height = max(1, min(len(layout.lines)+3, m.height-2*marginY))
	layout.x, layout.y = (m.width-layout.width)/2, (m.height-layout.height)/2
	return layout
}

// helpLinesFor renders the controls for a modal that is width cells wide.
// Keys sit right-aligned in one column and actions wrap in the next. A key
// wider than its column takes its own row. No width removes text, so
// scrolling always reaches every complete control.
func (m Model) helpLinesFor(width int) []string {
	content := max(1, width-4)
	sections := m.helpSections()
	keyColumn := 0
	for _, section := range sections {
		for _, entry := range section.entries {
			keyColumn = max(keyColumn, lipgloss.Width(entry.keys))
		}
	}
	keyColumn = max(1, min(keyColumn, helpKeyColumn, (content-2)/3))
	actionWidth := content - keyColumn - 2
	indent := strings.Repeat(" ", keyColumn+2)
	if actionWidth < 1 {
		// Two columns do not fit. Each key takes its own row, and its action
		// follows without an indent.
		keyColumn, actionWidth, indent = 0, content, ""
	}
	wrap := func(value string, width int) []string {
		return strings.Split(ansi.Wrap(value, width, ""), "\n")
	}
	var lines []string
	for i, section := range sections {
		if i > 0 {
			lines = append(lines, "")
		}
		for _, line := range wrap(section.title, content) {
			lines = append(lines, keyStyle.Render(line))
		}
		for _, entry := range section.entries {
			actions := wrap(entry.action, actionWidth)
			if keyWidth := lipgloss.Width(entry.keys); keyWidth <= keyColumn {
				lead := strings.Repeat(" ", keyColumn-keyWidth) + helpKeyStyle.Render(entry.keys) + "  "
				lines = append(lines, lead+dimStyle.Render(actions[0]))
				actions = actions[1:]
			} else {
				for _, line := range wrap(entry.keys, content) {
					lines = append(lines, helpKeyStyle.Render(line))
				}
			}
			for _, line := range actions {
				lines = append(lines, indent+dimStyle.Render(line))
			}
		}
	}
	return lines
}

func (m *Model) clampHelpOffset() {
	m.helpOffset = m.helpLayout().offset(m.helpOffset)
}

// helpCloseSpan returns the cells of the close control in the top edge of a
// modal that is width cells wide. ok is false when the modal is too narrow
// for it. The top edge renders the control from the same span.
func helpCloseSpan(width int) (start, end int, ok bool) {
	if width < 8 {
		return 0, 0, false
	}
	return width - 6, width - 3, true
}

// helpTopEdge is the top border with the title and the close control:
// "╭─ Keyboard controls ──── [×] ─╮".
func helpTopEdge(width int) string {
	start, _, ok := helpCloseSpan(width)
	if !ok {
		return helpBorderStyle.Render(truncate("╭"+strings.Repeat("─", max(0, width-2))+"╮", width))
	}
	// The cells before the space in front of the close control.
	lead := start - 1
	left := helpBorderStyle.Render("╭" + strings.Repeat("─", lead-1))
	// A title needs "╭─ " before it and a space after it. Below four cells
	// a cut title says nothing, so the edge leaves it out.
	if budget := lead - 4; budget >= 4 {
		title := truncate(helpTitle, budget)
		fill := budget - lipgloss.Width(title)
		left = helpBorderStyle.Render("╭─ ") + titleStyle.Render(title) + helpBorderStyle.Render(" "+strings.Repeat("─", fill))
	}
	return left + " " + errorStyle.Render(helpClose) + helpBorderStyle.Render(" ─╮")
}

func (m Model) renderHelpModal(layout helpLayout) []string {
	inner := max(0, layout.width-2)
	side := helpBorderStyle.Render("│")
	row := func(value string) string { return side + padCells(" "+value, inner) + side }
	offset := layout.offset(m.helpOffset)
	rows := []string{helpTopEdge(layout.width)}
	for _, line := range layout.lines[offset:min(len(layout.lines), offset+layout.visibleRows())] {
		rows = append(rows, row(line))
	}
	rows = append(rows, row(dimStyle.Render(truncate(helpHint, max(0, inner-2)))))
	rows = append(rows, helpBorderStyle.Render("╰"+strings.Repeat("─", inner)+"╯"))
	return rows[:min(len(rows), layout.height)]
}

// renderHelpOverlay draws the help modal over a dimmed copy of the screen
// behind it.
func (m Model) renderHelpOverlay() string {
	if m.width == 0 || m.height == 0 {
		return ""
	}
	behind := m
	behind.helpOverlay = false
	backdrop := strings.Split(ansi.Strip(behind.View()), "\n")
	layout := m.helpLayout()
	modal := m.renderHelpModal(layout)
	output := make([]string, m.height)
	for y := range output {
		line := ""
		if y < len(backdrop) {
			line = ansi.Truncate(backdrop[y], m.width, "")
		}
		i := y - layout.y
		if i < 0 || i >= len(modal) {
			output[y] = backdropStyle.Render(line)
			continue
		}
		left := padCells(ansi.Truncate(line, layout.x, ""), layout.x)
		right := ansi.TruncateLeft(line, layout.x+layout.width, "")
		output[y] = backdropStyle.Render(left) + modal[i] + backdropStyle.Render(right)
	}
	return strings.Join(output, "\n")
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
		m.helpOffset = len(m.helpLayout().lines)
	}
	m.clampHelpOffset()
	return m, nil
}

// updateHelpMouse keeps every mouse event inside the help. The wheel scrolls
// it, and a click on the close control closes it.
func (m Model) updateHelpMouse(message tea.MouseMsg) (tea.Model, tea.Cmd) {
	event := tea.MouseEvent(message)
	switch event.Button {
	case tea.MouseButtonWheelUp:
		m.helpOffset -= mouseStep
	case tea.MouseButtonWheelDown:
		m.helpOffset += mouseStep
	case tea.MouseButtonLeft:
		layout := m.helpLayout()
		start, end, ok := helpCloseSpan(layout.width)
		if ok && event.Action == tea.MouseActionPress && event.Y == layout.y && event.X >= layout.x+start && event.X < layout.x+end {
			m.helpOverlay, m.helpOffset = false, 0
			return m, nil
		}
	}
	m.clampHelpOffset()
	return m, nil
}
