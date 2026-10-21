package board

import (
	"fmt"
	"slices"
	"strings"

	"github.com/cdowell09/herdr-pr-board/internal/reviewmemory"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// regionLabelWidth is the left gutter that holds the split region's labels.
const regionLabelWidth = 14

// splitRegionLines renders the selected PR's review state as labeled rows:
// REVIEW, FINDINGS, and REVISION for the latest run, then POSTED and
// AUTOMATION, then each finding under its severity. The run ID and the
// diagnostics path stay in zoom.
func (m Model) splitRegionLines() []string {
	s := m.regionState()
	gutter := min(regionLabelWidth, m.width/3)
	var lines []string
	row := func(label span, right string, spans ...span) {
		lines = append(lines, labeledRow(m.width, gutter, label, right, spans...)...)
	}
	labeled := func(text string) span { return span{text: text, style: dimStyle} }
	blank := span{}
	body := keyStyle.Bold(false)

	for _, warning := range m.regionWarnings() {
		for _, line := range strings.Split(ansi.Wrap(reviewText(warning), max(1, m.width), ""), "\n") {
			lines = append(lines, warningStyle.Render(line))
		}
	}
	current := m.reviewObservation()
	target, hasTarget := latestCompleted(s.runs)
	var findings []reviewmemory.Finding
	for i := len(s.runs) - 1; i >= 0; i-- {
		run := s.runs[i]
		// The split shows the newest run and, when a newer run did not
		// complete, the completion that publication would target.
		newest := i == len(s.runs)-1
		if !newest && !(hasTarget && run.ID == target.ID) {
			continue
		}
		status := runStatus(run)
		if run.Reviewer != "" {
			status = append(status, span{text: " · " + reviewText(run.Reviewer), style: reviewSecondaryStyle})
		}
		status = append(status, span{text: " · " + revisionComparison(run, current), style: reviewSecondaryStyle})
		row(labeled("REVIEW"), fmt.Sprintf("run %d of %d", len(s.runs)-i, len(s.runs)), status...)
		if stop, ok := m.stopLine(run); ok {
			row(blank, "", stop)
		}
		label, summarized := labeled("FINDINGS"), false
		for _, line := range strings.Split(reviewText(strings.TrimSpace(run.Message)), "\n") {
			if line = strings.TrimSpace(line); line == "" {
				continue
			}
			style := body
			if strings.HasPrefix(strings.ToLower(line), "not run") {
				style = reviewSecondaryStyle
			}
			row(label, "", span{text: line, style: style})
			label, summarized = blank, true
		}
		if !summarized && run.Status == reviewmemory.Completed && len(run.Findings) == 0 {
			row(label, "", span{text: "no findings", style: body})
		}
		revision := []span{
			{text: shortRevision(run.Identity.HeadOID) + " → " + reviewText(run.Identity.BaseRefName), style: body},
			{text: " · " + run.StartedAt.Local().Format("02 Jan 15:04"), style: reviewSecondaryStyle},
		}
		if !newest {
			revision = append(revision, span{text: " · publication target", style: reviewSecondaryStyle})
		}
		row(labeled("REVISION"), "", revision...)
		for _, result := range m.publicationResults(run.ID) {
			row(blank, "", span{text: reviewText(result.text), style: result.style})
		}
		findings = append(findings, run.Findings...)
	}
	if len(s.runs) == 0 {
		row(labeled("REVIEW"), "", span{text: m.noRunsText(), style: body})
	}
	row(labeled("POSTED"), "", span{text: reviewText(m.rowReviewSummary(m.region.pr).postedDetail), style: body})

	auto := m.automationState()
	setup := "monitor " + auto.monitor + " · launches " + auto.launches + " · after review: " + auto.after
	if auto.waiting != "" {
		setup += " · " + auto.waiting
	}
	row(labeled("AUTOMATION"), "", span{text: reviewText(setup), style: reviewSecondaryStyle})
	if s.automatic.Reason != "" {
		row(blank, "", span{text: reviewText("Latest full observation: " + s.automatic.Reason), style: reviewSecondaryStyle})
	}
	if s.monitor.Message != "" {
		row(blank, "", span{text: reviewText(s.monitor.Message), style: warningStyle})
	}
	for _, f := range findings {
		row(span{text: reviewText(f.Severity), style: severityStyle(f.Severity).Bold(true)}, "", span{text: reviewText(f.Title), style: keyStyle})
		if f.Path != "" {
			row(blank, "", span{text: reviewText(fmt.Sprintf("%s:%d", f.Path, f.Line)), style: reviewSecondaryStyle})
		}
		row(blank, "", span{text: reviewText(f.Body), style: body})
	}
	return lines
}

// runStatus is a run's outcome for the REVIEW row. A completed run shows
// its exact counts colored by the worst severity, or "✓ Clean review".
func runStatus(run reviewmemory.Run) []span {
	if run.Status != reviewmemory.Completed {
		mark := reviewMark(string(run.Status))
		return []span{mark, {text: " " + string(run.Status), style: mark.style}}
	}
	counts := reviewmemory.CountSeverities(run.Findings)
	worst := worstSeverity(counts)
	if worst < 0 {
		return []span{{text: "✓ Clean review", style: okStyle}}
	}
	var parts []string
	for i, count := range counts {
		if count > 0 {
			parts = append(parts, fmt.Sprintf("%s %d", reviewmemory.Severities[i], count))
		}
	}
	return []span{{text: "● " + strings.Join(parts, ", "), style: severityStyles[worst].Bold(true)}}
}

// severityStyle colors a severity label; an unknown severity keeps the body color.
func severityStyle(severity string) lipgloss.Style {
	if i := slices.Index(reviewmemory.Severities[:], severity); i >= 0 {
		return severityStyles[i]
	}
	return keyStyle
}

// labeledRow renders one region row: the label in a gutter of gutter cells,
// the value wrapped under a blank gutter, and right at the right edge of the
// first line when it fits. A value on one line keeps each span's style; a
// wrapped value takes the last span's style.
// ponytail: wrapped rows lose per-span color; wrap styled spans if long rows need it.
func labeledRow(width, gutter int, label span, right string, spans ...span) []string {
	var text, styled strings.Builder
	for _, sp := range spans {
		text.WriteString(sp.text)
		styled.WriteString(sp.style.Render(sp.text))
	}
	wrapped := strings.Split(ansi.Wrap(text.String(), max(1, width-gutter), ""), "\n")
	lines := make([]string, len(wrapped))
	for i, value := range wrapped {
		head := strings.Repeat(" ", gutter)
		if i == 0 && label.text != "" {
			head = label.style.Render(padCells(truncate("  "+label.text, max(1, gutter-1)), gutter))
		}
		if len(wrapped) == 1 {
			value = styled.String()
		} else {
			value = spans[len(spans)-1].style.Render(value)
		}
		if i == 0 && right != "" {
			if room := width - gutter - lipgloss.Width(value) - lipgloss.Width(right); room > 0 {
				value += strings.Repeat(" ", room) + reviewSecondaryStyle.Render(right)
			}
		}
		lines[i] = head + value
	}
	return lines
}
