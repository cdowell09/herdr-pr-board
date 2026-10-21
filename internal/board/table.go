package board

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	gh "github.com/cdowell09/herdr-pr-board/internal/github"
	"github.com/cdowell09/herdr-pr-board/internal/reviewmemory"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// renderTable draws the table area in a rounded frame. The top edge takes the
// blank row under the tabs and names the view. The bottom edge closes the
// table above the URL row. The frame adds no rows, so the board geometry and
// the mouse rows stay the same.
func (m Model) renderTable(lay boardLayout) string {
	header, lines := m.tableBody(lay)
	width := m.tableWidth()
	side := frameStyle.Render("│")
	var output strings.Builder
	output.WriteString(m.frameTop() + "\n")
	if header != "" {
		output.WriteString(side + padCells(header, width) + side + "\n")
		output.WriteString(frameStyle.Render("├"+strings.Repeat("─", width)+"┤") + "\n")
	}
	for _, line := range lines {
		// Every line fits inside the frame, so no message pushes the right
		// side off the row.
		output.WriteString(side + padCells(ansi.Truncate(line, width, "…"), width) + side + "\n")
	}
	output.WriteString(frameStyle.Render("╰"+strings.Repeat("─", width)+"╯") + "\n")
	return output.String()
}

// tableWidth is the width inside the table frame.
func (m Model) tableWidth() int { return max(1, m.width-2) }

// frameTop returns the top frame edge with the view title and its count, for
// example "Opened by me (4)". An active filter shows its matches, for example
// "Opened by me (2 of 4)".
func (m Model) frameTop() string {
	view := m.currentView()
	count := strconv.Itoa(len(view.PRs))
	if m.filter != "" {
		count = fmt.Sprintf("%d of %d", len(m.filteredPRs()), len(view.PRs))
	}
	return frameEdge(m.tableWidth(), fmt.Sprintf("%s (%s)", view.View.Title, count), "")
}

// tableBody returns the column header and the lines under it. A view without
// rows has no header, and its message takes the header rows.
func (m Model) tableBody(lay boardLayout) (header string, lines []string) {
	view := m.currentView()
	width := m.tableWidth()
	if view.Err != nil && len(view.PRs) == 0 {
		return "", []string{errorStyle.Render(truncate("GitHub query failed: "+view.Err.Error(), width))}
	}
	rows := m.filteredPRs()
	if len(rows) == 0 {
		if m.loading {
			return "", []string{dimStyle.Render("Loading pull requests…")}
		}
		if m.filter != "" {
			return "", []string{dimStyle.Render("No pull requests match the filter.")}
		}
		return "", m.renderEmptyView(lay)
	}

	cols := m.tableLayout(lay.repository != "")
	if lay.repository != "" {
		group := fmt.Sprintf("%s%s (%d)", strings.Repeat(" ", gutterWidth), lay.repository, len(rows))
		lines = append(lines, groupStyle.Render(truncate(group, width)))
	}
	end := min(m.offset+lay.visibleRows, len(rows))
	// One clock reading per render keeps every row's age consistent.
	now := time.Now()
	for i := m.offset; i < end; i++ {
		lines = append(lines, m.renderPRRow(rows[i], cols, i == m.cursor, now))
	}
	return m.renderHeader(cols), lines
}

// tableLayout picks column sizes for the current terminal width. A width of
// zero hides the column. The title absorbs all remaining width.
//
// Keep CI and review symbols at every width. Drop author and updated before
// the posted source and repository so the title remains readable. A table
// whose rows share one repository hides the repository column.
type tableLayout struct {
	repo    int
	title   int
	author  int
	updated bool
	posted  bool
}

func (m Model) tableLayout(sharedRepository bool) tableLayout {
	var layout tableLayout
	switch {
	case m.width >= 120:
		layout.repo, layout.author, layout.updated, layout.posted = 24, 14, true, true
	case m.width >= tierWide:
		layout.repo, layout.author, layout.updated, layout.posted = 18, 10, true, true
	case m.width >= tierMedium:
		layout.repo, layout.updated, layout.posted = 16, true, true
	case m.width >= tierNarrow:
		layout.repo, layout.posted = 12, true
	}
	if sharedRepository {
		layout.repo = 0
	}
	layout.title = max(0, m.tableWidth()-layout.fixedWidth())
	return layout
}

// fixedWidth returns the cell width of every column except the title.
func (l tableLayout) fixedWidth() int {
	width := gutterWidth + 6 + 2 + 3 + 2 + reviewColumnWidth + 2 // gutter, PR, CI, REVIEW, and separators
	if l.posted {
		width += 8 + 2
	}
	if l.repo > 0 {
		width += l.repo + 2
	}
	if l.author > 0 {
		width += l.author + 2
	}
	if l.updated {
		width += 1 + updatedColumnWidth
	}
	return width
}

// gutterWidth is the left margin that carries the selection accent edge.
const gutterWidth = 3

// updatedColumnWidth fits the UPDATED header; ages align to its right edge.
const updatedColumnWidth = 7

// span is one styled piece of a table row. A row is built from spans so the
// selection background can cover every cell, including the colored symbols.
type span struct {
	text  string
	style lipgloss.Style
}

func plainSpan(text string) span { return span{text: text, style: lipgloss.NewStyle()} }

func (m Model) renderHeader(layout tableLayout) string {
	var b strings.Builder
	b.WriteString(strings.Repeat(" ", gutterWidth))
	if layout.repo > 0 {
		b.WriteString(padCells("REPOSITORY", layout.repo))
		b.WriteString("  ")
	}
	b.WriteString("PR    ")
	b.WriteString("  ")
	b.WriteString("CI ")
	b.WriteString("  ")
	b.WriteString(padCells("REVIEW", reviewColumnWidth))
	b.WriteString("  ")
	if layout.posted {
		b.WriteString("POSTED    ")
	}
	b.WriteString(padCells("TITLE", layout.title))
	if layout.author > 0 {
		b.WriteString("  ")
		b.WriteString(padCells("AUTHOR", layout.author))
	}
	if layout.updated {
		b.WriteString(" ")
		b.WriteString(padCells("UPDATED", updatedColumnWidth))
	}
	return headerStyle.Width(m.tableWidth()).Render(truncate(b.String(), m.tableWidth()))
}

// renderPRRow renders one table row across the width inside the frame. A selected row
// shows the accent edge in the gutter and the selection background in every
// cell.
func (m Model) renderPRRow(pr gh.PullRequest, layout tableLayout, selected bool, now time.Time) string {
	base := lipgloss.NewStyle()
	spans := []span{plainSpan(strings.Repeat(" ", gutterWidth))}
	if selected {
		base = selectedStyle
		spans[0] = span{text: " ▌ ", style: selectionEdgeStyle}
	}
	cell := func(text string, width int) {
		spans = append(spans, plainSpan(padCells(truncate(text, width), width)))
	}
	gap := func(n int) { spans = append(spans, plainSpan(strings.Repeat(" ", n))) }
	if layout.repo > 0 {
		cell(pr.Repository, layout.repo)
		gap(2)
	}
	cell(fmt.Sprintf("#%d", pr.Number), 6)
	gap(3)
	spans = append(spans, ciMark(pr.CI))
	gap(3)
	summary := m.rowReviewSummary(pr)
	spans = append(spans, reviewCell(summary)...)
	gap(2)
	if layout.posted {
		if summary.posted == noneMark.text {
			// The selected detail names why no source is known.
			spans = append(spans, span{text: padCells(noneMark.text, 8), style: noneMark.style})
		} else {
			cell(summary.posted, 8)
		}
		gap(2)
	}
	title := pr.Title
	if pr.Draft {
		title = "[draft] " + title
	}
	cell(title, layout.title)
	if layout.author > 0 {
		gap(2)
		cell(pr.Author, layout.author)
	}
	if layout.updated {
		gap(1)
		age := relativeTime(pr.UpdatedAt, now)
		spans = append(spans, plainSpan(strings.Repeat(" ", max(0, updatedColumnWidth-lipgloss.Width(age)))+age))
	}
	var b strings.Builder
	for _, s := range spans {
		b.WriteString(s.style.Inherit(base).Render(s.text))
	}
	line := ansi.Truncate(b.String(), m.tableWidth(), "…")
	if padding := m.tableWidth() - lipgloss.Width(line); padding > 0 {
		line += base.Render(strings.Repeat(" ", padding))
	}
	return line
}

// reviewColumnWidth fits the worst severity and its count: "● P0 12".
const reviewColumnWidth = 8

// severityStyles color a completed review by its worst severity.
var severityStyles = [len(reviewmemory.Severities)]lipgloss.Style{
	errorStyle,
	warningStyle,
	lipgloss.NewStyle().Foreground(cautionColor),
	lipgloss.NewStyle().Foreground(subtleColor),
}

// reviewCell shows the worst severity and its count for a completed review,
// "✓ clean" for a clean review, and the review state symbol otherwise. The
// spans fill the REVIEW column width.
func reviewCell(summary reviewRowSummary) []span {
	label := func(text string, style lipgloss.Style) span {
		return span{text: padCells(truncate(text, reviewColumnWidth-1), reviewColumnWidth-1), style: style}
	}
	if summary.state != "completed" {
		return []span{reviewMark(summary.state), label("", lipgloss.NewStyle())}
	}
	worst := worstSeverity(summary.findings)
	if worst < 0 {
		return []span{ciMark(gh.CISuccess), label(" clean", dimStyle)}
	}
	style := severityStyles[worst]
	return []span{{text: "●", style: style}, label(" "+reviewmemory.Severities[worst]+" "+strconv.Itoa(summary.findings[worst]), style.Bold(true))}
}

// worstSeverity returns the index of the most severe nonzero count, or -1
// for a clean review.
func worstSeverity(counts reviewmemory.SeverityCounts) int {
	for i, count := range counts {
		if count > 0 {
			return i
		}
	}
	return -1
}

// reviewCellText returns the visible REVIEW cell text, which the filter matches.
func reviewCellText(summary reviewRowSummary) string {
	var b strings.Builder
	for _, s := range reviewCell(summary) {
		b.WriteString(s.text)
	}
	return b.String()
}

// noneMark is the dim dot for an absent or unknown state in any column.
var noneMark = span{text: "·", style: dimStyle}

func reviewMark(state string) span {
	switch state {
	case "running":
		// A running review has its own shape, so it does not read as pending CI.
		return span{text: "◉", style: ciMark(gh.CIPending).style}
	case "waiting", "ready":
		return ciMark(gh.CIPending)
	case "completed":
		return ciMark(gh.CISuccess)
	case "failed", "blocked", "abandoned":
		return ciMark(gh.CIFailure)
	default:
		return noneMark
	}
}

func (m Model) selectedReviewLines(pr gh.PullRequest) []string {
	summary := m.rowReviewSummary(pr)
	detail := "Review: " + summary.detail + " · Posted: " + summary.postedDetail
	return strings.Split(ansi.Wrap(reviewText(detail), max(1, m.width), ""), "\n")
}

// renderSelected shows the selected URL and the one-line review summary
// when the detail pane does not fit.
func (m Model) renderSelected() string {
	pr, ok := m.selectedPR()
	if !ok {
		// The table area already explains why no PR is listed.
		return ""
	}
	url := urlStyle.Render(truncate(pr.URL, m.width))
	return url + "\n" + reviewSecondaryStyle.Render(strings.Join(m.selectedReviewLines(pr), "\n"))
}

func ciMark(state gh.CIState) span {
	switch state {
	case gh.CISuccess:
		return span{text: "✓", style: okStyle}
	case gh.CIPending:
		return span{text: "●", style: warningStyle}
	case gh.CIFailure, gh.CIError:
		return span{text: "✗", style: errorStyle}
	default:
		// No checks and an unknown status share the dim dot. The filter
		// still tells them apart by state name.
		return noneMark
	}
}

// truncate shortens value to at most width terminal cells, adding an
// ellipsis when it cuts. It measures display width, so emoji, combining
// characters, and wide glyphs stay aligned.
func truncate(value string, width int) string {
	if lipgloss.Width(value) <= width {
		return value
	}
	if width <= 1 {
		return "…"
	}
	budget := width - 1
	used := 0
	var keep strings.Builder
	for _, r := range value {
		cellWidth := lipgloss.Width(string(r))
		if used+cellWidth > budget {
			break
		}
		keep.WriteRune(r)
		used += cellWidth
	}
	return keep.String() + "…"
}

// padCells right-pads value with spaces to exactly width terminal cells.
func padCells(value string, width int) string {
	padding := width - lipgloss.Width(value)
	if padding <= 0 {
		return value
	}
	return value + strings.Repeat(" ", padding)
}
