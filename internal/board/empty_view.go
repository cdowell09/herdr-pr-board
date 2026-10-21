package board

import (
	"fmt"
	"strings"

	"github.com/cdowell09/herdr-pr-board/internal/config"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// emptyViewSteps names the next action for a view that has no pull requests.
// It repeats the key literals that the footer and the ? overlay use.
var emptyViewSteps = []keyHelpEntry{
	{"Tab", "next view"},
	{"E", "edit config"},
	{"Ctrl+R", "refresh"},
}

// emptyViewMessage explains why a view has no pull requests. A default view
// that keeps its default query and scope gets tailored text. Every other view
// names the query that found no pull requests. An edited default view is not
// the default view any more, so it also names its query.
func emptyViewMessage(view config.View) string {
	if base, ok := config.DefaultView(view.ID); ok && base.Query == view.Query && base.Scope == view.Scope {
		switch view.ID {
		case config.ViewAuthored:
			return "You have no open pull requests."
		case config.ViewReview:
			return "No open pull requests wait for your review."
		case config.ViewAll:
			return "No open pull requests are in the configured scopes."
		}
	}
	return fmt.Sprintf("No pull requests match %q.", view.Query)
}

// renderEmptyView explains the empty view and names the next keys, centered
// in the table frame. It returns exactly the rows of the table area. The text
// wraps at the frame width, so no width removes a key. A long query or a short
// pane cannot push the tabs off the screen and invalidate the mouse rows.
func (m Model) renderEmptyView(lay boardLayout) []string {
	width, budget := m.tableWidth(), max(1, lay.emptyRows)
	steps := emptyViewSteps
	if len(m.views) < 2 {
		// One view has no next view, so Tab does nothing.
		steps = steps[1:]
	}

	// One row always explains the view. The keys take the rows that remain.
	// A pane that is too short for the actions keeps the keys alone.
	pack := func(texts []string) []string { return renderHints(packHints(hints(steps, texts), width)) }
	// The actions use the normal text color: they are the content here.
	pairs := make([]string, len(steps))
	for i, step := range steps {
		pairs[i] = keyStyle.Render(step.keys) + " " + step.action
	}
	stepLines := pack(pairs)
	if len(stepLines) > budget-1 {
		stepLines = pack(keyLabels(steps))
	}
	stepLines = stepLines[:min(len(stepLines), budget-1)]

	message := strings.Split(ansi.Wrap(emptyViewMessage(m.currentView().View), width, ""), "\n")
	if room := budget - len(stepLines); len(message) > room {
		// Compress the rest of the message into the last row it keeps.
		message = append(message[:room-1], truncate(strings.Join(message[room-1:], " "), width))
	}
	lines := append(message, stepLines...)
	for i, line := range lines {
		lines[i] = strings.Repeat(" ", max(0, width-lipgloss.Width(line))/2) + line
	}
	// Blank rows above and below center the text and fill the frame.
	top := make([]string, max(0, budget-len(lines))/2)
	lines = append(top, lines...)
	return append(lines, make([]string, max(0, budget-len(lines)))...)
}
