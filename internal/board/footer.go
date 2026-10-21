package board

import (
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/cdowell09/herdr-pr-board/internal/config"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
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
	{"s", "settings"},
	{"v", "zoom"},
	{"?", "help"},
}

// zoomKeyHelp is the footer control list of the zoomed detail pane.
var zoomKeyHelp = []keyHelpEntry{
	{"v", "board"},
	{"j/k", "scroll"},
	{"g/G", "ends"},
	{"t", "stop"},
	{"c", "comment"},
	{"a", "approve"},
	{"x", "changes"},
	{"s", "settings"},
	{"o", "open"},
	{"?", "help"},
}

// documentedKeys lists every key literal the board and review guides must document.
// The documentation drift test fails when one is missing. Add a new
// single-character binding to config.ReservedKeys and other bindings here, then
// to helpSections and the corresponding guide.
var documentedKeys = append([]string{
	"1", "9", "Tab", "Shift+Tab", "←", "→", "↑", "↓", "Home", "End", "PgUp", "PgDn",
	"Enter", "Ctrl+U", "Esc", "Backspace", "Ctrl+R", "Ctrl+C",
}, config.ReservedKeys...)

// footerHelpLines renders the board control rows. The filter shows on the
// meta line, so starting a filter does not move any row.
func (m Model) footerHelpLines() []string {
	return renderHints(m.boardHints())
}

// boardHints wraps the board control reference at pair boundaries so each
// keybinding stays next to its action on narrow terminals. The rows left
// after the title, tabs, stale notice, table header, one PR row, the URL,
// the collapsed summary, and the meta line bound the controls. The split
// keeps the smallest detail pane in place of the URL and the summary.
func (m Model) boardHints() [][]keyHint {
	budget := m.height - 9
	if m.regionSplit() {
		budget = m.height - 8 - paneMinRows
	} else if pr, ok := m.selectedPR(); ok {
		budget -= len(m.selectedReviewLines(pr))
	}
	if stale(m.currentView()) {
		budget--
	}
	return m.hintRows(slices.Concat(keyHelp, m.shortcutHelp()), budget)
}

// zoomHints are the controls of the zoomed detail pane footer. They leave
// the pane's two edges, its URL, one review row, and the meta line. A
// shorter screen drops the bottom edge first, so one review row stays.
func (m Model) zoomHints() [][]keyHint {
	return m.hintRows(slices.Concat(zoomKeyHelp, m.shortcutHelp()), m.height-5)
}

// keyHint is one rendered footer control and the key literal it names.
type keyHint struct {
	keys string
	text string
}

var hintSeparator = dimStyle.Render(" · ")

// hintGap is the width of the separator between two controls on one row.
var hintGap = lipgloss.Width(hintSeparator)

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
			return hint.keys, true
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
// board or the zoomed region. View() ends with the help lines and the meta
// line, and the control rows open the help lines.
func (m Model) footerKey(message tea.MouseMsg) (tea.KeyMsg, bool) {
	if message.Button != tea.MouseButtonLeft || message.Action != tea.MouseActionPress {
		return tea.KeyMsg{}, false
	}
	rows := m.zoomHints()
	help := len(rows)
	if !m.zoomed() {
		rows, help = m.boardHints(), len(m.footerHelpLines())
	}
	top := strings.Count(m.View(), "\n") - help
	keys, ok := hintAt(rows, message.X, message.Y-top)
	if !ok {
		return tea.KeyMsg{}, false
	}
	return hintKey(keys)
}

// renderFooter renders the control lines and the meta line under them.
// Alerts use the warning color. Healthy status, such as the refresh age and
// the rate limits, is dim.
func (m Model) renderFooter(help []string) string {
	// Collect the meta parts, then join them. Prefixing a separator to each
	// part leaves a leading separator when an earlier part is absent.
	var parts []string
	add := func(text string, alert bool) {
		style := dimStyle
		if alert {
			style = warningStyle
		}
		// The meta line owns exactly one row, so a multiline message flattens.
		parts = append(parts, style.Render(strings.Join(strings.Fields(text), " ")))
	}
	// A narrow terminal truncates the tail. An armed confirmation comes
	// first, so no filter can hide what Enter is about to publish. Every
	// alert comes before the routine refresh age and rate limits.
	if m.confirm != nil {
		add(m.confirm.prompt(), true)
	}
	if filter := m.renderFilter(); filter != "" {
		parts = append(parts, filter)
	}
	if m.notice != "" {
		add(m.notice, true)
	}
	if m.monitorError != "" {
		add(reviewText(m.monitorError), true)
	}
	// A collapsed region cannot show its status line, so the footer does.
	if m.region != nil && m.region.message != "" && !m.zoom && !m.regionSplit() {
		add(reviewText(m.region.message), true)
	}
	if m.warning != "" {
		add(m.warning, true)
	}
	if stale(m.currentView()) {
		add("stale", true)
	}
	freshness := m.currentView().UpdatedAt
	if !freshness.IsZero() {
		add("updated "+relativeTime(freshness, time.Now()), false)
	}
	// A rate is low when it cannot pay for the next full refresh or the
	// next GraphQL batch. The refresh keys use the same Search check.
	if m.rates.Search.Limit > 0 {
		add(fmt.Sprintf("Search %d/%d", m.rates.Search.Remaining, m.rates.Search.Limit), !m.rates.Search.HasCapacity(m.cfg.SearchRequestCount()))
	}
	if m.rates.GraphQL.Limit > 0 {
		add(fmt.Sprintf("GraphQL %d/%d", m.rates.GraphQL.Remaining, m.rates.GraphQL.Limit), !m.rates.GraphQL.HasCapacity(m.rates.GraphQL.CostPerQuery()))
	}
	meta := ansi.Truncate(strings.Join(parts, hintSeparator), max(1, m.width), "…")
	return strings.Join(append(help, meta), "\n")
}

// renderFilter shows the filter and its match count, for example
// "/ rev · 2 of 4". The meta line shows it, so starting a filter does not
// move the table or the footer controls. The filter text takes at most half
// the width, so a long filter cannot push notices and warnings off the line.
func (m Model) renderFilter() string {
	if !m.editing && m.filter == "" {
		return ""
	}
	count := hintSeparator + dimStyle.Render(fmt.Sprintf("%d of %d", len(m.filteredPRs()), len(m.currentView().PRs)))
	cursor := ""
	if m.editing {
		cursor = "▌"
	}
	room := m.width/2 - lipgloss.Width("/ "+cursor+count)
	return keyStyle.Render("/") + " " + truncate(m.filter, max(1, room)) + cursor + count
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
