package board

import (
	"fmt"
	"strings"

	"github.com/cdowell09/herdr-pr-board/internal/config"
	"github.com/charmbracelet/x/ansi"
)

// emptyViewSteps names the next action for a view that has no pull requests.
// It repeats the key literals that the footer and the ? overlay use.
var emptyViewSteps = []keyHelpEntry{
	{"Tab", "next view"},
	{"E", "edit config"},
	{"r", "refresh"},
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

// renderEmptyView explains the empty view and names the next keys. The text
// wraps at the terminal width, so no width removes a key. The whole state
// keeps only the rows that the table area holds, so neither a long query nor a
// short pane can push the tabs off the screen and invalidate the mouse rows.
func (m Model) renderEmptyView(lay boardLayout) string {
	width, budget := max(1, m.width), max(1, lay.visibleRows)
	steps := emptyViewSteps
	if len(m.views) < 2 {
		// One view has no next view, so Tab does nothing.
		steps = steps[1:]
	}

	// One row always explains the view. The keys take the rows that remain.
	// A pane that is too short for the actions keeps the keys alone.
	stepLines := packLines(keyPairs(steps), width)
	if len(stepLines) > budget-1 {
		stepLines = packLines(keyLabels(steps), width)
	}
	if len(stepLines) > budget-1 {
		stepLines = stepLines[:max(0, budget-1)]
	}

	message := strings.Split(ansi.Wrap(emptyViewMessage(m.currentView().View), width, ""), "\n")
	if room := budget - len(stepLines); len(message) > room {
		// Compress the rest of the message into the last row it keeps.
		message = append(message[:room-1], truncate(strings.Join(message[room-1:], " "), width))
	}

	lines := make([]string, 0, len(message)+len(stepLines))
	for _, line := range message {
		lines = append(lines, dimStyle.Render(line))
	}
	return strings.Join(append(lines, stepLines...), "\n") + "\n"
}
