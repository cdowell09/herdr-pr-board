package board

import (
	"fmt"
	"slices"
	"strings"
	"time"

	gh "github.com/cdowell09/herdr-pr-board/internal/github"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// The detail pane shows the selected PR under the table: a rounded frame
// titled with the PR, then its URL, title, and facts, and then the review
// section. Zoom shows the same pane at full height. The URL comes first so
// that it stays on screen in the shortest pane.
const (
	// paneURLOffset is the URL row under the top edge.
	paneURLOffset = 1
)

// panePR is the PR that the pane shows: the PR that the review section is
// bound to, or the selected PR without local reviews.
func (m Model) panePR() (gh.PullRequest, bool) {
	if m.region != nil {
		return m.region.pr, true
	}
	return m.selectedPR()
}

// zoomed reports that the pane fills the screen.
func (m Model) zoomed() bool {
	_, ok := m.panePR()
	return m.zoom && ok
}

// paneWidth is the content width inside the pane's sides and their padding.
func (m Model) paneWidth() int { return max(1, m.width-4) }

// renderPane returns the pane in at most height lines. The review section
// takes the rows that the other parts leave.
func (m Model) renderPane(height int) []string {
	pr, _ := m.panePR()
	inner := m.paneWidth()
	side := frameStyle.Render("│")
	row := func(line string) string {
		return side + " " + padCells(ansi.Truncate(line, inner, "…"), inner) + " " + side
	}
	facts, title, bottom, review := m.paneRows(height)
	lines := []string{m.paneTop(pr), row(urlStyle.Render(reviewText(pr.URL)))}
	if title {
		lines = append(lines, row(keyStyle.Render(reviewText(pr.Title))))
	}
	for _, fact := range facts {
		lines = append(lines, row(fact))
	}
	if title {
		lines = append(lines, row(rule("Review", "", inner, dimStyle)))
	}
	// The review section is the only part that needs local reviews.
	section := []string{dimStyle.Render(reviewsUnavailable)}
	if m.region != nil {
		section = m.regionViewport(m.regionLines(m.zoom), review)
	}
	for _, line := range section[:min(len(section), review)] {
		lines = append(lines, row(line))
	}
	if bottom {
		lines = append(lines, frameStyle.Render("╰"+strings.Repeat("─", max(0, m.width-2))+"╯"))
	}
	lines = lines[:min(len(lines), max(0, height))]
	// A pane narrower than its frame and padding cuts each row at the screen edge.
	for i, line := range lines {
		lines[i] = ansi.Truncate(line, m.width, "")
	}
	return lines
}

// paneRows returns the optional parts that fit a pane of height rows and the
// review rows that they leave. The top edge and the URL always show. The
// bottom edge comes next, then the title and the review rule, then the
// facts, while one review row stays.
func (m Model) paneRows(height int) (facts []string, title, bottom bool, review int) {
	review = height - 2
	if review-1 >= 1 {
		bottom, review = true, review-1
	}
	if bottom && review-2 >= 1 {
		title, review = true, review-2
		pr, _ := m.panePR()
		if f := m.paneFacts(pr); review-len(f) >= 1 {
			facts, review = f, review-len(f)
		}
	}
	return facts, title, bottom, max(0, review)
}

// paneReviewRows is the height of the review section in a pane of height rows.
func (m Model) paneReviewRows(height int) int {
	_, _, _, review := m.paneRows(height)
	return review
}

// paneTop is the top edge: the repository and number, and the PR's
// position in the listed rows, for example "1 of 4".
func (m Model) paneTop(pr gh.PullRequest) string {
	position := ""
	rows := m.filteredPRs()
	if i := slices.IndexFunc(rows, func(row gh.PullRequest) bool { return row.URL == pr.URL }); i >= 0 {
		position = fmt.Sprintf("%d of %d", i+1, len(rows))
	}
	return frameEdge(m.width-2, reviewText(fmt.Sprintf("%s #%d", pr.Repository, pr.Number)), position)
}

// paneFacts lists the PR's facts as dim labels with bright values. The lines
// wrap between facts, so a label always stays next to its value.
func (m Model) paneFacts(pr gh.PullRequest) []string {
	type fact struct {
		label string
		value span
	}
	value := func(text string) span { return span{text: reviewText(text), style: regionBodyStyle} }
	var facts []fact
	if pr.Author != "" {
		facts = append(facts, fact{"Author", value(pr.Author)})
	}
	state := strings.ToLower(string(pr.State))
	// A closed or merged draft shows its final state.
	if pr.Draft && (pr.State == gh.PROpen || pr.State == "") {
		state = "draft"
	}
	if state != "" {
		facts = append(facts, fact{"State", value(state)})
	}
	mark := ciMark(pr.CI)
	facts = append(facts, fact{"CI", span{text: mark.text + " " + ciWord(pr.CI), style: mark.style}})
	if pr.HeadOID != "" {
		facts = append(facts, fact{"Revision", value(shortRevision(pr.HeadOID) + " → " + pr.BaseRefName)})
	}
	if !pr.UpdatedAt.IsZero() {
		age := relativeTime(pr.UpdatedAt, time.Now())
		if age != "now" {
			age += " ago"
		}
		facts = append(facts, fact{"Updated", value(age + " · " + pr.UpdatedAt.Local().Format("02 Jan 15:04"))})
	}
	const gap = "   "
	var lines []string
	var line strings.Builder
	used := 0
	for _, f := range facts {
		width := lipgloss.Width(f.label + " " + f.value.text)
		if used > 0 && used+len(gap)+width > m.paneWidth() {
			lines = append(lines, line.String())
			line.Reset()
			used = 0
		}
		if used > 0 {
			line.WriteString(gap)
			used += len(gap)
		}
		line.WriteString(dimStyle.Render(f.label+" ") + f.value.style.Render(f.value.text))
		used += width
	}
	return append(lines, line.String())
}

// ciWord names a CI state next to its mark.
func ciWord(state gh.CIState) string {
	switch state {
	case gh.CISuccess:
		return "passing"
	case gh.CIPending:
		return "pending"
	case gh.CIFailure:
		return "failing"
	case gh.CIError:
		return "error"
	case gh.CINone:
		return "no checks"
	default:
		return "unknown"
	}
}

// frameEdge returns a rounded top edge with width cells inside it: the
// title at the left and an optional dim note at the right. A narrow edge
// drops the note before it cuts the title.
func frameEdge(width int, title, note string) string {
	if width < 4 {
		return frameStyle.Render("╭" + strings.Repeat("─", max(0, width)) + "╮")
	}
	// "─ " before and " " after the title keep it clear of the corners.
	title = truncate(title, width-3)
	fill := width - 3 - lipgloss.Width(title)
	tail := ""
	// " note ─" needs its own cells and at least one rule cell before it.
	if note != "" && fill >= lipgloss.Width(note)+4 {
		fill -= lipgloss.Width(note) + 3
		tail = " " + dimStyle.Render(note) + frameStyle.Render(" ─")
	}
	return frameStyle.Render("╭─ ") + keyStyle.Render(title) + frameStyle.Render(" "+strings.Repeat("─", fill)) + tail + frameStyle.Render("╮")
}
