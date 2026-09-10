package board

import (
	"fmt"
	"slices"
	"strings"

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
	"n", "N", "s", "t", "c", "a", "x", "r", "R", "o", "q", "Ctrl+C",
}

// footerHelpLines wraps the board control reference at pair boundaries so each
// keybinding stays next to its action on narrow terminals. The rows left
// after the title, tabs, stale notice, table header, one PR row, the URL,
// the collapsed summary, the filter row, and the meta line bound the controls.
func (m Model) footerHelpLines() []string {
	width, budget := max(1, m.width), m.height-9
	if stale(m.currentView()) {
		budget--
	}
	if pr, ok := m.selectedPR(); ok && !m.regionSplit() {
		budget -= len(m.selectedReviewLines(pr))
	}
	if m.editing || m.filter != "" {
		budget-- // the filter row follows the controls
	}
	lines := m.helpLines(keyHelp, budget)
	if m.editing {
		lines = append(lines, dimStyle.Render("filter: ")+truncate(m.filter+"▌", max(1, width-8)))
	} else if m.filter != "" {
		lines = append(lines, dimStyle.Render("filter: ")+truncate(m.filter, max(1, width-8)))
	}
	return lines
}

// helpLines renders a control list within budget rows. A short pane drops
// the actions, then the trailing lines. When a review can be stopped, the
// stop control names its run first and replaces the generic stop entry, so
// the target stays visible even when its run scrolls out of the region.
func (m Model) helpLines(entries []keyHelpEntry, budget int) []string {
	width, budget := max(1, m.width), max(1, budget)
	var stop []string
	if m.region != nil {
		if target, ok := m.stopTarget(); ok {
			stop = []string{truncate(keyPairs([]keyHelpEntry{{"t", "stop " + shortRevision(target.ID)}})[0], width)}
			entries = slices.DeleteFunc(slices.Clone(entries), func(entry keyHelpEntry) bool { return entry.keys == "t" })
		}
	}
	lines := packLines(append(stop, keyPairs(entries)...), width)
	if len(lines) > budget {
		lines = packLines(append(stop, keyLabels(entries)...), width)
	}
	return lines[:min(len(lines), budget)]
}

// renderFooter renders the control lines and the meta line under them.
func (m Model) renderFooter(help []string) string {
	// Collect the meta parts, then join them. Prefixing a separator to each
	// part leaves a leading separator when an earlier part is absent.
	var parts []string
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

// packLines puts as many parts on each line as the width holds. A part never
// breaks, so no width separates a key from its action.
func packLines(parts []string, width int) []string {
	separator := dimStyle.Render(" · ")
	var lines []string
	current := ""
	for _, part := range parts {
		candidate := part
		if current != "" {
			candidate = current + separator + part
		}
		if lipgloss.Width(candidate) <= width {
			current = candidate
			continue
		}
		if current != "" {
			lines = append(lines, current)
		}
		current = part
	}
	if current != "" {
		lines = append(lines, current)
	}
	return lines
}
