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
	layout.title = max(0, m.width-layout.fixedWidth())
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
	return headerStyle.Width(m.width).Render(truncate(b.String(), m.width))
}

// renderPRRow renders one table row across the full width. A selected row
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
		cell(summary.posted, 8)
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
	line := ansi.Truncate(b.String(), m.width, "…")
	return line + base.Render(strings.Repeat(" ", max(0, m.width-lipgloss.Width(line))))
}

// reviewColumnWidth fits the worst severity and its count: "● P0 12".
const reviewColumnWidth = 8

// severityStyles color a completed review by its worst severity.
var severityStyles = [len(reviewmemory.Severities)]lipgloss.Style{
	lipgloss.NewStyle().Foreground(lipgloss.Color("210")),
	lipgloss.NewStyle().Foreground(lipgloss.Color("214")),
	lipgloss.NewStyle().Foreground(lipgloss.Color("179")),
	lipgloss.NewStyle().Foreground(lipgloss.Color("248")),
}

// reviewCell shows the worst severity and its count for a completed review,
// "✓ clean" for a clean review, and the review state symbol otherwise. The
// spans fill the REVIEW column width.
func reviewCell(summary reviewRowSummary) []span {
	mark, label := reviewMark(summary.state), ""
	if summary.state == "completed" {
		label = "clean"
		for i, count := range summary.findings {
			if count > 0 {
				mark = span{text: "●", style: severityStyles[i]}
				label = reviewmemory.Severities[i] + " " + strconv.Itoa(count)
				break
			}
		}
	}
	spans := []span{mark}
	if label != "" {
		label = " " + label
	}
	style := dimStyle
	if mark.text == "●" {
		style = mark.style.Bold(true)
	}
	return append(spans, span{text: padCells(truncate(label, reviewColumnWidth-1), reviewColumnWidth-1), style: style})
}

// reviewText returns the visible REVIEW cell text, which the filter matches.
func reviewCellText(summary reviewRowSummary) string {
	var b strings.Builder
	for _, s := range reviewCell(summary) {
		b.WriteString(s.text)
	}
	return b.String()
}

// findingsDetail lists the exact count at every severity for the selected detail line.
func findingsDetail(counts reviewmemory.SeverityCounts) string {
	if counts == (reviewmemory.SeverityCounts{}) {
		return "no findings"
	}
	return counts.String()
}

func reviewMark(state string) span {
	switch state {
	case "running", "waiting", "ready":
		return ciMark(gh.CIPending)
	case "completed":
		return ciMark(gh.CISuccess)
	case "failed", "blocked", "abandoned":
		return ciMark(gh.CIFailure)
	case "none":
		return ciMark(gh.CINone)
	default:
		return ciMark(gh.CIUnknown)
	}
}

func (m Model) selectedReviewLines(pr gh.PullRequest) []string {
	summary := m.rowReviewSummary(pr)
	detail := "Review: " + summary.detail + " · Posted: " + summary.postedDetail
	return strings.Split(ansi.Wrap(reviewText(detail), max(1, m.width), ""), "\n")
}

// renderSelected shows the selected URL. The one-line summary follows it
// only when the review region is collapsed.
func (m Model) renderSelected(lay boardLayout) string {
	pr, ok := m.selectedPR()
	if !ok {
		return dimStyle.Render("No PR selected")
	}
	url := urlStyle.Render(truncate(pr.URL, m.width))
	if lay.regionRows > 0 {
		return url
	}
	return url + "\n" + reviewSecondaryStyle.Render(strings.Join(m.selectedReviewLines(pr), "\n"))
}

func ciMark(state gh.CIState) span {
	switch state {
	case gh.CISuccess:
		return span{text: "✓", style: okStyle}
	case gh.CIPending:
		return span{text: "●", style: lipgloss.NewStyle().Foreground(lipgloss.Color("214"))}
	case gh.CIFailure, gh.CIError:
		return span{text: "✗", style: lipgloss.NewStyle().Foreground(lipgloss.Color("210"))}
	case gh.CINone:
		return span{text: "–", style: dimStyle.Foreground(lipgloss.Color("248"))}
	default:
		return span{text: "?", style: dimStyle.Foreground(lipgloss.Color("248"))}
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
