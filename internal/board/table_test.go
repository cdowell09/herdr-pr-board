package board

import (
	"strings"
	"testing"

	gh "github.com/cdowell09/herdr-pr-board/internal/github"
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
		if got, want := renderReviewState(tc.state), renderCI(tc.ci); got != want {
			t.Fatalf("%s marker = %q, want CI marker %q", tc.state, got, want)
		}
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
		markerX := strings.Index(header, "REV")
		cellsBeforeMarker := lipgloss.Width(header[:markerX])
		if got := ansi.Cut(row, cellsBeforeMarker, cellsBeforeMarker+3); !strings.HasPrefix(got, stripANSI(renderReviewState(summary.state))) {
			t.Fatalf("width %d: review marker does not align: %q / %q", width, header, row)
		}
		if layout.title < 16 {
			t.Fatalf("width %d: title has only %d cells", width, layout.title)
		}
		selected := stripANSI(model.renderSelected())
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
