package board

import (
	"strings"
	"testing"
	"time"

	gh "github.com/cdowell09/herdr-pr-board/internal/github"
	"github.com/cdowell09/herdr-pr-board/internal/reviewmemory"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

func TestReviewStateUsesCISymbols(t *testing.T) {
	for _, tc := range []struct {
		state string
		ci    gh.CIState
	}{
		{"running", gh.CIPending}, {"waiting", gh.CIPending}, {"ready", gh.CIPending},
		{"completed", gh.CISuccess}, {"failed", gh.CIFailure}, {"blocked", gh.CIFailure}, {"abandoned", gh.CIFailure},
		{"none", gh.CINone}, {"unknown", gh.CIUnknown},
	} {
		if got, want := reviewMark(tc.state), ciMark(tc.ci); got.text != want.text || got.style.Render(got.text) != want.style.Render(want.text) {
			t.Fatalf("%s marker = %q, want CI marker %q", tc.state, got.text, want.text)
		}
	}
}

func TestReviewCellShowsTheWorstSeverity(t *testing.T) {
	completed := func(counts reviewmemory.SeverityCounts) reviewRowSummary {
		return reviewRowSummary{state: "completed", findings: counts}
	}
	for _, tc := range []struct {
		name    string
		summary reviewRowSummary
		want    string
	}{
		{"worst severity wins", completed(reviewmemory.SeverityCounts{1, 2, 0, 3}), "● P0 1"},
		{"lower severities show when no worse one exists", completed(reviewmemory.SeverityCounts{0, 0, 1, 0}), "● P2 1"},
		{"exact counts above nine", completed(reviewmemory.SeverityCounts{0, 12, 0, 9}), "● P1 12"},
		{"clean review", completed(reviewmemory.SeverityCounts{}), "✓ clean"},
		{"counts never leak into other states", reviewRowSummary{state: "running", findings: reviewmemory.SeverityCounts{1, 0, 0, 0}}, "●"},
		{"failed rerun hides older counts", reviewRowSummary{state: "failed", findings: reviewmemory.SeverityCounts{1, 0, 0, 0}}, "✗"},
	} {
		cell := reviewCellText(tc.summary)
		if got := strings.TrimSpace(cell); got != tc.want {
			t.Fatalf("%s: cell = %q, want %q", tc.name, got, tc.want)
		}
		if width := lipgloss.Width(cell); width != reviewColumnWidth {
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
		layout := model.tableLayout(false)
		header := stripANSI(model.renderHeader(layout))
		row := stripANSI(model.renderPRRow(pr, layout, false, time.Now()))
		markerX := strings.Index(header, "REVIEW")
		cellsBeforeMarker := lipgloss.Width(header[:markerX])
		if got, want := ansi.Cut(row, cellsBeforeMarker, cellsBeforeMarker+reviewColumnWidth), reviewCellText(summary); got != want {
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

// colorProfile renders styles as ANSI 256 colors for one test.
func colorProfile(t *testing.T) {
	t.Helper()
	previous := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	t.Cleanup(func() { lipgloss.SetColorProfile(previous) })
}

func TestSelectedRowHighlightSpansTheFullRow(t *testing.T) {
	colorProfile(t)
	for _, width := range []int{50, 80, 120} {
		m := layoutModel(t, width)
		m.cursor = 1
		lines := strings.Split(m.View(), "\n")
		lay := m.boardLayout()
		selected := lines[lay.firstPRRow+1]
		if plain := stripANSI(selected); !strings.HasPrefix(plain, " ▌ ") || lipgloss.Width(plain) != width {
			t.Fatalf("width %d: selected row = %q", width, plain)
		}
		for _, segment := range strings.Split(selected, "\x1b[0m") {
			if stripANSI(segment) != "" && !strings.Contains(segment, "48;5;237") {
				t.Fatalf("width %d: segment %q loses the selection background in %q", width, segment, selected)
			}
		}
		if plain := stripANSI(lines[lay.firstPRRow]); !strings.HasPrefix(plain, "   ") {
			t.Fatalf("width %d: unselected row = %q", width, plain)
		}
	}
}

func TestRelativeTimeShowsCompactAges(t *testing.T) {
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		age  time.Duration
		want string
	}{
		{30 * time.Second, "now"}, {5 * time.Minute, "5m"}, {3 * time.Hour, "3h"},
		{2 * 24 * time.Hour, "2d"}, {6 * 24 * time.Hour, "6d"}, {7 * 24 * time.Hour, "1w"},
		{41 * 24 * time.Hour, "5w"}, {42 * 24 * time.Hour, "6w"}, {-time.Hour, "now"},
	} {
		if got := relativeTime(now.Add(-tc.age), now); got != tc.want {
			t.Fatalf("age %v = %q, want %q", tc.age, got, tc.want)
		}
	}
	if got := relativeTime(time.Time{}, now); got != "–" {
		t.Fatalf("zero time = %q", got)
	}
}

func TestUpdatedAgesAlignToTheRightEdge(t *testing.T) {
	m := layoutModel(t, 120)
	m.views[0].PRs[0].UpdatedAt = time.Now().Add(-42 * 24 * time.Hour)
	lines := strings.Split(stripANSI(m.View()), "\n")
	lay := m.boardLayout()
	for i, want := range []string{"30m", "1h", "6w"} {
		if row := lines[lay.firstPRRow+i]; !strings.HasSuffix(row, " "+want) || lipgloss.Width(row) != m.width {
			t.Fatalf("row %d = %q, want age %q at the right edge", i, row, want)
		}
	}
}
