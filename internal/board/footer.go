package board

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type keyHelpEntry struct {
	keys   string
	action string
}

// keyHelp is the single source of truth for the board footer control list.
// The footer shows the most useful keys. The ? overlay shows all of them.
var keyHelp = []keyHelpEntry{
	{"Tab", "view"},
	{"↑↓", "select"},
	{"Enter", "open"},
	{"/", "filter"},
	{"j/k", "scroll"},
	{"n", "run"},
	{"s", "settings"},
	{"v", "zoom"},
	{"?", "help"},
}

// zoomKeyHelp is the footer control list of the zoomed review region.
var zoomKeyHelp = []keyHelpEntry{
	{"v", "board"},
	{"j/k", "scroll"},
	{"g/G", "ends"},
	{"n", "run"},
	{"N", "rerun"},
	{"t", "stop"},
	{"c", "comment"},
	{"a", "approve"},
	{"x", "changes"},
	{"s", "settings"},
	{"o", "open"},
	{"?", "help"},
}

// documentedKeys lists every key literal the board and review guides must document.
// The documentation drift test fails when one is missing. Add new bindings from
// updateKey, updateFilter, updateZoomKey, or updateReviewAction here, to
// helpSections, and to the corresponding guide.
var documentedKeys = []string{
	"1", "9", "Tab", "Shift+Tab", "h", "l", "←", "→",
	"j", "k", "↑", "↓", "g", "G", "Home", "End", "PgUp", "PgDn",
	"/", "?", "Enter", "Ctrl+U", "Esc", "Backspace", "E", "v",
	"n", "N", "s", "t", "c", "a", "x", "y", "r", "R", "o", "q", "Ctrl+C",
}

// footerHelpLines renders the board controls and, while a filter is set,
// the filter row under them.
func (m Model) footerHelpLines() []string {
	width := max(1, m.width)
	lines := renderHints(m.boardHints())
	if m.editing {
		lines = append(lines, dimStyle.Render("filter: ")+truncate(m.filter+"▌", max(1, width-8)))
	} else if m.filter != "" {
		lines = append(lines, dimStyle.Render("filter: ")+truncate(m.filter, max(1, width-8)))
	}
	return lines
}

// boardHints wraps the board control reference at pair boundaries so each
// keybinding stays next to its action on narrow terminals. The rows left
// after the title, tabs, stale notice, table header, one PR row, the URL,
// the collapsed summary, the filter row, and the meta line bound the controls.
func (m Model) boardHints() [][]keyHint {
	budget := m.height - 9
	if stale(m.currentView()) {
		budget--
	}
	if pr, ok := m.selectedPR(); ok && !m.regionSplit() {
		budget -= len(m.selectedReviewLines(pr))
	}
	if m.editing || m.filter != "" {
		budget-- // the filter row follows the controls
	}
	return m.hintRows(keyHelp, budget)
}

// zoomHints are the controls of the zoomed review region footer.
func (m Model) zoomHints() [][]keyHint {
	return m.hintRows(zoomKeyHelp, m.height-4)
}

// keyHint is one rendered footer control and the key literal it names.
type keyHint struct {
	keys string
	text string
}

// hintGap is the width of the separator between two controls on one row.
var hintGap = lipgloss.Width(hintSeparator)

var hintSeparator = dimStyle.Render(" · ")

// hintRows lays out a control list within budget rows. Rendering and mouse
// hitboxes both come from these rows, so a click covers exactly a rendered
// control. A short pane drops the actions, then the trailing rows. When a
// review can be stopped, the stop control names its run first and replaces
// the generic stop entry, so the target stays visible even when its run
// scrolls out of the region.
func (m Model) hintRows(entries []keyHelpEntry, budget int) [][]keyHint {
	width, budget := max(1, m.width), max(1, budget)
	var stop []keyHint
	if m.region != nil {
		if target, ok := m.stopTarget(); ok {
			stop = []keyHint{{"t", truncate(keyPairs([]keyHelpEntry{{"t", "stop " + shortRevision(target.ID)}})[0], width)}}
			entries = slices.DeleteFunc(slices.Clone(entries), func(entry keyHelpEntry) bool { return entry.keys == "t" })
		}
	}
	rows := packHints(slices.Concat(stop, hints(entries, keyPairs(entries))), width)
	if len(rows) > budget {
		rows = packHints(slices.Concat(stop, hints(entries, keyLabels(entries))), width)
	}
	return rows[:min(len(rows), budget)]
}

func hints(entries []keyHelpEntry, texts []string) []keyHint {
	out := make([]keyHint, len(entries))
	for i, entry := range entries {
		out[i] = keyHint{entry.keys, texts[i]}
	}
	return out
}

// renderHints joins each row of controls with the separator.
func renderHints(rows [][]keyHint) []string {
	lines := make([]string, len(rows))
	for i, row := range rows {
		texts := make([]string, len(row))
		for j, hint := range row {
			texts[j] = hint.text
		}
		lines[i] = strings.Join(texts, hintSeparator)
	}
	return lines
}

// hintAt returns the key literal of the control at column x of a row.
func hintAt(rows [][]keyHint, x, row int) (string, bool) {
	if row < 0 || row >= len(rows) {
		return "", false
	}
	left := 0
	for _, hint := range rows[row] {
		width := lipgloss.Width(hint.text)
		if x >= left && x < left+width {
			return hint.keys, hint.keys != ""
		}
		left += width + hintGap
	}
	return "", false
}

// hintKey is the key press a footer control replays. A control that names
// more than one key, such as j/k, replays none.
func hintKey(keys string) (tea.KeyMsg, bool) {
	switch keys {
	case "Tab":
		return tea.KeyMsg{Type: tea.KeyTab}, true
	case "Enter":
		return tea.KeyMsg{Type: tea.KeyEnter}, true
	}
	if utf8.RuneCountInString(keys) == 1 {
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(keys)}, true
	}
	return tea.KeyMsg{}, false
}

// footerKey returns the key of the footer control under a left click on the
// board or the zoomed region. The footer is the last block of View(): the
// control rows, the filter row on the board, and the meta line.
func (m Model) footerKey(message tea.MouseMsg) (tea.KeyMsg, bool) {
	if message.Button != tea.MouseButtonLeft || message.Action != tea.MouseActionPress {
		return tea.KeyMsg{}, false
	}
	rows, below := m.zoomHints(), 0
	if !m.zoom || m.region == nil {
		rows = m.boardHints()
		below = len(m.footerHelpLines()) - len(rows) // the filter row
	}
	top := strings.Count(m.View(), "\n") - below - len(rows)
	keys, ok := hintAt(rows, message.X, message.Y-top)
	if !ok {
		return tea.KeyMsg{}, false
	}
	return hintKey(keys)
}

// renderFooter renders the control lines and the meta line under them.
func (m Model) renderFooter(help []string) string {
	// Collect the meta parts, then join them. Prefixing a separator to each
	// part leaves a leading separator when an earlier part is absent.
	var parts []string
	if m.confirm != nil {
		parts = append(parts, m.confirm.prompt())
	}
	if m.editorNotice != "" {
		// Keep the notice first. A narrow terminal truncates the tail.
		parts = append(parts, m.editorNotice)
	}
	if m.monitorError != "" {
		parts = append(parts, reviewText(m.monitorError))
	}
	if len(m.reviewJobs) > 0 {
		parts = append(parts, fmt.Sprintf("%d review requests", len(m.reviewJobs)))
	}
	// A collapsed region cannot show its status line, so the footer does.
	if m.region != nil && m.region.message != "" && !m.zoom && !m.regionSplit() {
		parts = append(parts, reviewText(m.region.message))
	}
	freshness := m.currentView().UpdatedAt
	if !freshness.IsZero() {
		parts = append(parts, "updated "+relativeTime(freshness))
	}
	if stale(m.currentView()) {
		parts = append(parts, "stale")
	}
	if m.rates.Search.Limit > 0 {
		parts = append(parts, fmt.Sprintf("Search %d/%d", m.rates.Search.Remaining, m.rates.Search.Limit))
	}
	if m.rates.GraphQL.Limit > 0 {
		parts = append(parts, fmt.Sprintf("GraphQL %d/%d", m.rates.GraphQL.Remaining, m.rates.GraphQL.Limit))
	}
	if m.warning != "" {
		parts = append(parts, m.warning)
	}
	// The meta line owns exactly one row, so a multiline message flattens.
	meta := strings.Join(strings.Fields(strings.Join(parts, " · ")), " ")
	return strings.Join(append(help, warningStyle.Render(truncate(meta, m.width))), "\n")
}

// keyPairs renders each control as a bright key and a dim action.
func keyPairs(entries []keyHelpEntry) []string {
	parts := make([]string, 0, len(entries))
	for _, entry := range entries {
		parts = append(parts, keyStyle.Render(entry.keys)+" "+dimStyle.Render(entry.action))
	}
	return parts
}

// keyLabels renders only the key literals. A pane that is too short for the
// actions keeps every key this way.
func keyLabels(entries []keyHelpEntry) []string {
	parts := make([]string, 0, len(entries))
	for _, entry := range entries {
		parts = append(parts, keyStyle.Render(entry.keys))
	}
	return parts
}

// packLines puts as many parts on each line as the width holds.
func packLines(parts []string, width int) []string {
	return renderHints(packHints(hints(make([]keyHelpEntry, len(parts)), parts), width))
}

// packHints puts as many controls on each row as the width holds. A control
// never breaks, so no width separates a key from its action.
func packHints(controls []keyHint, width int) [][]keyHint {
	var rows [][]keyHint
	used := 0
	for _, hint := range controls {
		w := lipgloss.Width(hint.text)
		if last := len(rows) - 1; last >= 0 && used+hintGap+w <= width {
			rows[last] = append(rows[last], hint)
			used += hintGap + w
			continue
		}
		rows = append(rows, []keyHint{hint})
		used = w
	}
	return rows
}
