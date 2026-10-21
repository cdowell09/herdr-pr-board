package board

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cdowell09/herdr-pr-board/internal/config"
	gh "github.com/cdowell09/herdr-pr-board/internal/github"
	"github.com/cdowell09/herdr-pr-board/internal/publication"
	"github.com/cdowell09/herdr-pr-board/internal/reviewmemory"
	tea "github.com/charmbracelet/bubbletea"
)

// unframe returns a pane row without its sides, their padding, and the
// padding after the content.
func unframe(line string) string {
	return strings.TrimRight(strings.TrimSuffix(strings.TrimPrefix(line, "│ "), " │"), " ")
}

// reviewSection returns the split pane's rendered review rows without the frame.
func reviewSection(t *testing.T, m Model) []string {
	t.Helper()
	lines := strings.Split(stripANSI(m.View()), "\n")
	// The URL row is followed by the title, the facts, and the review rule.
	top := m.boardLayout().selectedURLRow + 2 + len(m.paneFacts(m.region.pr)) + 1
	var rows []string
	for _, line := range lines[top : top+m.regionSize()] {
		rows = append(rows, unframe(line))
	}
	return rows
}

func TestDetailPaneShowsThePRAboveItsReviews(t *testing.T) {
	m := splitModel(t, &reviewFake{})
	m.views[0].PRs[0].Author, m.views[0].PRs[0].State, m.views[0].PRs[0].CI = "lin", gh.PROpen, gh.CISuccess
	m.views[0].PRs[1].Draft, m.views[0].PRs[1].CI = true, gh.CIFailure
	m.setRegionRuns(completedRun(1))
	// The pane follows the latest observation on the next update.
	next, _ := m.Update(tea.WindowSizeMsg{Width: m.width, Height: m.height})
	m = next.(Model)
	for _, c := range []struct {
		down  bool
		facts []string
		edge  string
	}{
		{false, []string{"Author lin", "State open", "CI ✓ passing", "Revision aaaaaaaa → main", "Updated 1h ago · "}, "1 of 2"},
		{true, []string{"State draft", "CI ✗ failing"}, "2 of 2"},
	} {
		if c.down {
			m = press(m, tea.KeyMsg{Type: tea.KeyDown})
		}
		lay := m.boardLayout()
		lines := strings.Split(stripANSI(m.View()), "\n")
		top, title := lines[lay.selectedURLRow-paneURLOffset], unframe(lines[lay.selectedURLRow+1])
		if !strings.HasPrefix(top, "╭─ acme/repo #"+strconv.Itoa(m.region.pr.Number)+" ─") || !strings.HasSuffix(top, " "+c.edge+" ─╮") {
			t.Fatalf("top edge = %q", top)
		}
		if title != m.region.pr.Title {
			t.Fatalf("title row = %q", title)
		}
		facts := strings.Join(lines[lay.selectedURLRow+2:lay.selectedURLRow+2+len(m.paneFacts(m.region.pr))], "\n")
		for _, want := range c.facts {
			if !strings.Contains(facts, want) {
				t.Fatalf("facts lack %q:\n%s", want, facts)
			}
		}
		if rule := unframe(lines[lay.selectedURLRow+2+len(m.paneFacts(m.region.pr))]); !strings.HasPrefix(rule, "── Review ──") {
			t.Fatalf("review rule = %q", rule)
		}
	}
}

func TestDetailPaneMouseMatchesRenderedRows(t *testing.T) {
	for _, width := range []int{50, 90, 140} {
		m := splitModel(t, &reviewFake{})
		next, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: 30})
		m = next.(Model)
		m.setRegionRuns(completedRun(12))
		opened := ""
		m.openBrowser = func(url string) tea.Cmd { opened = url; return nil }
		lines := strings.Split(stripANSI(m.View()), "\n")
		edge, url := -1, -1
		for y, line := range lines {
			if edge < 0 && strings.HasPrefix(line, "╭─ acme/repo #1") {
				edge = y
			}
			if url < 0 && strings.Contains(line, "https://github.com/acme/repo/pull/1") {
				url = y
			}
		}
		if edge < 0 || url != edge+paneURLOffset || url != m.boardLayout().selectedURLRow {
			t.Fatalf("width %d: edge %d, URL %d, layout URL %d", width, edge, url, m.boardLayout().selectedURLRow)
		}
		m.Update(tea.MouseMsg{X: width / 2, Y: url, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
		if opened != "https://github.com/acme/repo/pull/1" {
			t.Fatalf("width %d: URL click opened %q", width, opened)
		}
		// The wheel over any pane row scrolls the reviews and keeps the selection.
		for _, y := range []int{edge, url + 1, m.height - 3} {
			// The region is shared between model copies, so each wheel starts at the top.
			m.region.offset = 0
			next, _ := m.Update(tea.MouseMsg{X: 2, Y: y, Button: tea.MouseButtonWheelDown})
			if scrolled := next.(Model); scrolled.cursor != 0 || scrolled.region.offset != mouseStep {
				t.Fatalf("width %d: wheel at row %d: cursor=%d offset=%d", width, y, scrolled.cursor, scrolled.region.offset)
			}
		}
		next, _ = m.Update(tea.MouseMsg{X: 2, Y: edge - 2, Button: tea.MouseButtonWheelDown})
		if next.(Model).cursor != 1 {
			t.Fatalf("width %d: wheel over the table did not move the selection", width)
		}
	}
}

func TestZoomedPaneDropsNoise(t *testing.T) {
	m := splitModel(t, &reviewFake{})
	run := completedRun(1)
	m.setRegionData(func(data *regionData) {
		data.runs = []reviewmemory.Run{run}
		data.publications = []publication.Attempt{{RunID: run.ID, Action: config.PublishComment, Status: publication.Published}}
	})
	m.overview.monitor.ObservedAt = time.Now()
	m = press(m, runes("v"))
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 80})
	m = next.(Model)
	zoomed := stripANSI(m.View())
	if !strings.Contains(zoomed, "P0 1") || strings.Contains(zoomed, "P1:0") {
		t.Fatalf("zoom shows zero severity counts:\n%s", zoomed)
	}
	if strings.Count(zoomed, "Run "+run.ID) != 1 {
		t.Fatalf("zoom names the run %d times:\n%s", strings.Count(zoomed, "Run "+run.ID), zoomed)
	}
	if iso := regexp.MustCompile(`\d{4}-\d\d-\d\dT`); iso.MatchString(zoomed) {
		t.Fatalf("zoom shows an ISO time:\n%s", zoomed)
	}
	if lines := strings.Split(zoomed, "\n"); !strings.HasPrefix(lines[0], "╭─ acme/repo #1") || unframe(lines[paneURLOffset]) != m.region.pr.URL {
		t.Fatalf("zoom is not the pane:\n%s", zoomed)
	}
}

func TestFrameEdgeDropsTheNoteBeforeTheTitle(t *testing.T) {
	if edge := stripANSI(frameEdge(20, "acme/repo #1", "1 of 2")); edge != "╭─ acme/repo #1 ─────╮" {
		t.Fatalf("narrow edge = %q", edge)
	}
	if edge := stripANSI(frameEdge(25, "acme/repo #1", "1 of 2")); edge != "╭─ acme/repo #1 ─ 1 of 2 ─╮" {
		t.Fatalf("edge = %q", edge)
	}
}

func TestShortZoomKeepsAReviewRow(t *testing.T) {
	m := splitModel(t, &reviewFake{})
	m.views[0].PRs[0].Author = "lin"
	m.setRegionRuns(completedRun(3))
	m = press(m, runes("v"))
	for _, size := range [][2]int{{80, 10}, {50, 12}, {30, 10}, {50, 8}, {50, 7}, {30, 8}, {40, 5}, {30, 6}, {12, 8}} {
		next, _ := m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		m = next.(Model)
		if _, rows := m.zoomViewport(); rows < 1 {
			t.Fatalf("%v: zoom has %d review rows:\n%s", size, rows, stripANSI(m.View()))
		}
		m = press(m, runes("G"))
		section := m.regionLines(true)
		if last, view := stripANSI(section[len(section)-1]), stripANSI(m.View()); !strings.Contains(view, last) {
			t.Fatalf("%v: G did not reach the details:\n%s", size, view)
		}
		m = press(m, runes("g"))
	}
}

func TestClosedDraftShowsItsFinalState(t *testing.T) {
	m := splitModel(t, &reviewFake{})
	pr := splitPR(1, 0)
	pr.Draft = true
	for state, want := range map[gh.PRState]string{gh.PROpen: "State draft", "": "State draft", gh.PRClosed: "State closed", gh.PRMerged: "State merged"} {
		pr.State = state
		if facts := stripANSI(strings.Join(m.paneFacts(pr), "\n")); !strings.Contains(facts, want) {
			t.Fatalf("state %q: facts = %q, want %q", state, facts, want)
		}
	}
}
