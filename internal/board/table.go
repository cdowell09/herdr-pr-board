package board

import (
	"fmt"
	"strings"

	gh "github.com/cdowell09/herdr-pr-board/internal/github"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// tableLayout picks column sizes for the current terminal width. A width of
// zero hides the column. The title absorbs all remaining width.
//
// Keep CI and review symbols at every width. Drop author and updated before
// the posted source and repository so the title remains readable.
type tableLayout struct {
	repo    int
	title   int
	author  int
	updated bool
	posted  bool
}

func (m Model) tableLayout() tableLayout {
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
	layout.title = max(0, m.width-layout.fixedWidth())
	return layout
}

// fixedWidth returns the cell width of every column except the title.
func (l tableLayout) fixedWidth() int {
	width := 6 + 2 + 3 + 2 + 3 + 2 // PR, CI, REV, and separators
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
		width += 8 + 1 // UPDATED column
	}
	return width
}

func (m Model) renderHeader(layout tableLayout) string {
	var b strings.Builder
	if layout.repo > 0 {
		b.WriteString(padCells("REPOSITORY", layout.repo))
		b.WriteString("  ")
	}
	b.WriteString("PR    ")
	b.WriteString("  ")
	b.WriteString("CI ")
	b.WriteString("  ")
	b.WriteString("REV  ")
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
		b.WriteString(padCells("UPDATED", 8))
	}
	return headerStyle.Width(m.width).Render(truncate(b.String(), m.width))
}

func (m Model) renderPRRow(pr gh.PullRequest, layout tableLayout) string {
	var b strings.Builder
	if layout.repo > 0 {
		b.WriteString(padCells(truncate(pr.Repository, layout.repo), layout.repo))
		b.WriteString("  ")
	}
	b.WriteString(fmt.Sprintf("#%-5d", pr.Number))
	b.WriteString("  ")
	b.WriteString(renderCI(pr.CI))
	b.WriteString("  ")
	summary := m.rowReviewSummary(pr)
	b.WriteString(renderReviewState(summary.state))
	b.WriteString("  ")
	if layout.posted {
		b.WriteString(padCells(truncate(summary.posted, 8), 8))
		b.WriteString("  ")
	}
	title := pr.Title
	if pr.Draft {
		title = "[draft] " + title
	}
	b.WriteString(padCells(truncate(title, layout.title), layout.title))
	if layout.author > 0 {
		b.WriteString("  ")
		b.WriteString(padCells(truncate(pr.Author, layout.author), layout.author))
	}
	if layout.updated {
		b.WriteString(" ")
		b.WriteString(padCells(relativeTime(pr.UpdatedAt), 8))
	}
	return truncate(b.String(), m.width)
}

func renderReviewState(state string) string {
	switch state {
	case "running", "waiting", "queued", "ready":
		return renderCI(gh.CIPending)
	case "completed":
		return renderCI(gh.CISuccess)
	case "failed", "blocked", "abandoned":
		return renderCI(gh.CIFailure)
	case "none":
		return renderCI(gh.CINone)
	default:
		return renderCI(gh.CIUnknown)
	}
}

func (m Model) selectedReviewLines(pr gh.PullRequest) []string {
	summary := m.rowReviewSummary(pr)
	detail := "Review: " + summary.detail + " · Posted: " + summary.postedDetail
	return strings.Split(ansi.Wrap(reviewText(detail), max(1, m.width), ""), "\n")
}

func (m Model) renderSelected() string {
	pr, ok := m.selectedPR()
	if !ok {
		return dimStyle.Render("No PR selected")
	}
	return urlStyle.Render(truncate(pr.URL, m.width)) + "\n" + reviewSecondaryStyle.Render(strings.Join(m.selectedReviewLines(pr), "\n"))
}

func renderCI(state gh.CIState) string {
	var icon string
	switch state {
	case gh.CISuccess:
		icon = lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Render("✓")
	case gh.CIPending:
		icon = lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Render("●")
	case gh.CIFailure, gh.CIError:
		icon = lipgloss.NewStyle().Foreground(lipgloss.Color("210")).Render("✗")
	case gh.CINone:
		icon = dimStyle.Foreground(lipgloss.Color("248")).Render("–")
	default:
		icon = dimStyle.Foreground(lipgloss.Color("248")).Render("?")
	}
	return " " + icon + " "
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
