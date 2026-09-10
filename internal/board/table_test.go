package board

import (
	"strings"
	"testing"

	gh "github.com/cdowell09/herdr-pr-board/internal/github"
	"github.com/cdowell09/herdr-pr-board/internal/reviewmemory"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func TestReviewStateUsesCISymbols(t *testing.T) {
	for _, tc := range []struct {
		state string
		ci    gh.CIState
	}{
		{"running", gh.CIPending}, {"waiting", gh.CIPending}, {"queued", gh.CIPending}, {"ready", gh.CIPending},
		{"completed", gh.CISuccess}, {"failed", gh.CIFailure}, {"blocked", gh.CIFailure}, {"abandoned", gh.CIFailure},
		{"none", gh.CINone}, {"unknown", gh.CIUnknown},
	} {
		if got, want := renderReviewState(tc.state), ciSymbol(tc.ci); got != want {
			t.Fatalf("%s marker = %q, want CI marker %q", tc.state, got, want)
		}
	}
}

func TestReviewCellShowsSeverityCounts(t *testing.T) {
	completed := func(counts reviewmemory.SeverityCounts) reviewRowSummary {
		return reviewRowSummary{state: "completed", findings: counts}
	}
	for _, tc := range []struct {
		name    string
		summary reviewRowSummary
		want    string
	}{
		{"counts in severity order", completed(reviewmemory.SeverityCounts{1, 2, 0, 3}), "P0:1 P1:2 P2:0 P3:3"},
		{"counts above nine are capped", completed(reviewmemory.SeverityCounts{12, 0, 10, 9}), "P0:+ P1:0 P2:+ P3:9"},
		{"clean review keeps the symbol", completed(reviewmemory.SeverityCounts{}), "✓"},
		{"counts never leak into other states", reviewRowSummary{state: "running", findings: reviewmemory.SeverityCounts{1, 0, 0, 0}}, "●"},
		{"failed rerun hides older counts", reviewRowSummary{state: "failed", findings: reviewmemory.SeverityCounts{1, 0, 0, 0}}, "✗"},
	} {
		cell := renderReviewCell(tc.summary)
		if got := strings.TrimSpace(stripANSI(cell)); got != tc.want {
			t.Fatalf("%s: cell = %q, want %q", tc.name, got, tc.want)
		}
		if width := lipgloss.Width(stripANSI(cell)); width != reviewColumnWidth {
			t.Fatalf("%s: cell is %d cells, want %d", tc.name, width, reviewColumnWidth)
		}
	}
}

func TestFindingsDetailListsExactCounts(t *testing.T) {
	if got := findingsDetail(reviewmemory.SeverityCounts{12, 1, 0, 3}); got != "P0:12 P1:1 P2:0 P3:3" {
		t.Fatalf("findingsDetail = %q", got)
	}
	if got := findingsDetail(reviewmemory.SeverityCounts{}); got != "no findings" {
		t.Fatalf("findingsDetail of no findings = %q", got)
	}
}

func TestReviewOverviewLayouts(t *testing.T) {
	for _, width := range []int{40, 60, 80, 120} {
		model := layoutModel(t, width)
		pr, _ := model.selectedPR()
		summary := model.rowReviewSummary(pr)
		layout := model.tableLayout()
		header := stripANSI(model.renderHeader(layout))
		row := stripANSI(model.renderPRRow(pr, layout))
		markerX := strings.Index(header, "REVIEW")
		cellsBeforeMarker := lipgloss.Width(header[:markerX])
		if got, want := ansi.Cut(row, cellsBeforeMarker, cellsBeforeMarker+reviewColumnWidth), stripANSI(renderReviewCell(summary)); got != want {
			t.Fatalf("width %d: review cell does not align: %q / %q", width, header, row)
		}
		if layout.fixedWidth() > width {
			t.Fatalf("width %d: fixed columns need %d cells", width, layout.fixedWidth())
		}
		if layout.title < 1 || (width >= tierWide && layout.title < 15) {
			t.Fatalf("width %d: title has only %d cells", width, layout.title)
		}
		selected := stripANSI(model.renderSelected(model.boardLayout()))
		detail := strings.Join(strings.Fields(strings.Join(strings.Split(selected, "\n")[1:], " ")), " ")
		want := strings.Join(strings.Fields("Review: "+summary.detail+" · Posted: "+summary.postedDetail), " ")
		if detail != want {
			t.Fatalf("width %d: selected detail = %q, want %q", width, detail, want)
		}
		t.Logf("width %d:\n%s", width, stripANSI(model.View()))
	}
}

func TestReviewDetailReservesRowsWithFullTable(t *testing.T) {
	for _, width := range []int{40, 60, 120} {
		model := layoutModel(t, width)
		model.height = 20
		pr := model.views[0].PRs[0]
		for range 30 {
			model.views[0].PRs = append(model.views[0].PRs, pr)
		}
		model.cursor = len(model.views[0].PRs) - 1
		model.clampCursor()
		lines := strings.Split(model.View(), "\n")
		if len(lines) > model.height {
			t.Fatalf("width %d: %d lines exceed height %d", width, len(lines), model.height)
		}
		layout := model.boardLayout()
		if !strings.Contains(lines[layout.selectedURLRow], "https://github.com/") {
			t.Fatalf("width %d: URL hitbox no longer matches rendered URL", width)
		}
		if !strings.Contains(lines[layout.selectedURLRow+1], "Review:") {
			t.Fatalf("width %d: review detail missing below URL", width)
		}
	}
}
