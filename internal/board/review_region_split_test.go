package board

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cdowell09/herdr-pr-board/internal/config"
	"github.com/cdowell09/herdr-pr-board/internal/discovery"
	"github.com/cdowell09/herdr-pr-board/internal/dispatch"
	gh "github.com/cdowell09/herdr-pr-board/internal/github"
	"github.com/cdowell09/herdr-pr-board/internal/publication"
	"github.com/cdowell09/herdr-pr-board/internal/reviewmemory"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func splitPR(number int, age time.Duration) gh.PullRequest {
	return gh.PullRequest{Repository: "acme/repo", Number: number, Title: fmt.Sprintf("Change %d", number), URL: fmt.Sprintf("https://github.com/acme/repo/pull/%d", number), HeadOID: strings.Repeat("a", 40), BaseRefName: "main", MetadataObservedAt: time.Now(), UpdatedAt: time.Now().Add(-age)}
}

// splitModel builds a board with two PRs and local reviews, sized for the
// split, with the region bound to the first row.
func splitModel(t *testing.T, backend ReviewBackend) Model {
	t.Helper()
	m, err := NewModel(testConfig(), fakeLoader{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	m = m.WithReviews(context.Background(), backend)
	m.cfg.Reviewers = []config.Reviewer{{ID: "agent", Command: []string{"fake-reviewer"}}}
	m.cfg.Repositories = []config.Repository{{Name: "acme/repo", Reviewer: "agent"}}
	m.loading = false
	m.views[0].PRs = []gh.PullRequest{splitPR(1, time.Hour), splitPR(2, 2*time.Hour)}
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 35})
	return next.(Model)
}

func completedRun(findings int) reviewmemory.Run {
	run := reviewmemory.Run{ID: "run-1", Reviewer: "agent", StartedAt: time.Now(), Identity: reviewmemory.Identity{Repository: "acme/repo", Number: 1, HeadOID: strings.Repeat("a", 40), BaseRefName: "main"}, Outcome: reviewmemory.Outcome{Status: reviewmemory.Completed, Message: "Summary"}}
	for i := 0; i < findings; i++ {
		severity := reviewmemory.Severities[i%len(reviewmemory.Severities)]
		run.Findings = append(run.Findings, reviewmemory.Finding{Severity: severity, Title: fmt.Sprintf("Finding %d", i), Body: fmt.Sprintf("Body text %d", i), Path: "main.go", Line: i + 1})
	}
	return run
}

func TestRegionFollowsTheSelectionFromTheLatestRead(t *testing.T) {
	m := splitModel(t, &reviewFake{})
	if m.region == nil || m.region.pr.Number != 1 {
		t.Fatalf("region not bound to the selected PR: %+v", m.region)
	}
	second := completedRun(1)
	second.Identity.Number = 2
	read := reviewOverviewMsg{epoch: m.epoch, read: overviewRead{regions: map[string]regionData{"https://github.com/acme/repo/pull/2": {runs: []reviewmemory.Run{second}}}}}
	next, _ := m.Update(read)
	m = next.(Model)
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = next.(Model)
	if cmd != nil {
		t.Fatal("selection change started a read instead of binding from the latest one")
	}
	if m.region == nil || m.region.pr.Number != 2 || len(m.regionState().runs) != 1 {
		t.Fatalf("selection change did not bind the region from the latest read: %+v", m.region)
	}
	if view := stripANSI(m.View()); !strings.Contains(view, "Review · completed") || strings.Contains(view, "No local review runs") {
		t.Fatalf("region flashed empty after the selection change:\n%s", view)
	}
}

// setRegionData stores the region PR's local review state in the latest
// read, the way an overview read would.
func (m *Model) setRegionData(mutate func(*regionData)) {
	key := strings.ToLower(m.region.pr.URL)
	if m.overview.regions == nil {
		m.overview.regions = map[string]regionData{}
	}
	data := m.overview.regions[key]
	mutate(&data)
	m.overview.regions[key] = data
}

// setRegionRuns replaces the region PR's recorded runs in the latest read.
func (m *Model) setRegionRuns(runs ...reviewmemory.Run) {
	m.setRegionData(func(data *regionData) { data.runs = runs })
}

func TestSplitGeometryMatchesRenderedRowsAndMouse(t *testing.T) {
	m := splitModel(t, &reviewFake{})
	m.setRegionRuns(completedRun(12))
	lay := m.boardLayout()
	lines := strings.Split(m.View(), "\n")
	if len(lines) != m.height {
		t.Fatalf("rendered %d lines in a %d-line terminal:\n%s", len(lines), m.height, stripANSI(m.View()))
	}
	if !strings.Contains(stripANSI(lines[lay.firstPRRow]), "#1") || !strings.Contains(stripANSI(lines[lay.firstPRRow+1]), "#2") {
		t.Fatalf("rows moved:\n%s", stripANSI(m.View()))
	}
	if stripANSI(lines[lay.selectedURLRow]) != m.region.pr.URL {
		t.Fatalf("URL row %d = %q", lay.selectedURLRow, stripANSI(lines[lay.selectedURLRow]))
	}
	header := stripANSI(lines[lay.selectedURLRow+2])
	for _, want := range []string{"── Review", "completed", "agent", "current observed revision", "P0:3 P1:3 P2:3 P3:3", "run 1/1"} {
		if !strings.Contains(header, want) {
			t.Fatalf("region header lacks %q: %q", want, header)
		}
	}
	m.reviewRows = map[string]reviewOverviewRow{m.region.pr.URL: {identity: dispatch.Identity(m.region.pr), summary: reviewRowSummary{postedDetail: "GitHub · current revision"}}}
	if view := stripANSI(m.View()); !strings.Contains(view, "Posted: GitHub · current revision") {
		t.Fatalf("region lacks the posted detail:\n%s", view)
	}
	if lay.regionRows < regionMinRows || lay.regionRows != m.height-lay.selectedURLRow-1-len(m.footerHelpLines())-1 {
		t.Fatalf("region rows = %d in layout %+v", lay.regionRows, lay)
	}
	for _, line := range lines {
		if lipgloss.Width(line) > m.width {
			t.Fatalf("overflow %q", line)
		}
	}
	// The click selects the second PR, and the region binds to it at once.
	updated, cmd := m.Update(tea.MouseMsg{X: 2, Y: lay.firstPRRow + 1, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if cmd != nil || updated.(Model).cursor != 1 || updated.(Model).region.pr.Number != 2 {
		t.Fatal("row click did not select the second PR and bind its region")
	}
	opened := ""
	m.openBrowser = func(url string) tea.Cmd { opened = url; return nil }
	m.Update(tea.MouseMsg{X: 2, Y: lay.selectedURLRow, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if opened != m.region.pr.URL {
		t.Fatal("URL click did not open the selected PR")
	}
	updated, _ = m.Update(tea.MouseMsg{Button: tea.MouseButtonWheelDown})
	if scrolled := updated.(Model); scrolled.cursor != 0 || scrolled.region.offset != mouseStep {
		t.Fatalf("wheel moved the cursor or skipped the region: cursor=%d offset=%d", scrolled.cursor, scrolled.region.offset)
	}
}

func TestRegionScrollKeysLeaveTheSelectionAlone(t *testing.T) {
	m := splitModel(t, &reviewFake{})
	m.setRegionRuns(completedRun(30))
	if !strings.Contains(stripANSI(m.View()), "▼") {
		t.Fatalf("long region shows no marker:\n%s", stripANSI(m.View()))
	}
	press := func(key tea.KeyMsg) {
		next, _ := m.Update(key)
		m = next.(Model)
	}
	press(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	if m.cursor != 0 || m.region.offset != 1 {
		t.Fatalf("j moved the cursor or skipped the region: cursor=%d offset=%d", m.cursor, m.region.offset)
	}
	press(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("G")})
	if strings.Contains(stripANSI(m.View()), "▼") {
		t.Fatalf("marker survived at the end of the region:\n%s", stripANSI(m.View()))
	}
	press(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("g")})
	firstPage := stripANSI(strings.Join(strings.Split(m.View(), "\n")[m.boardLayout().selectedURLRow+1:], "\n"))
	press(tea.KeyMsg{Type: tea.KeyPgDown})
	if want := m.boardLayout().regionRows - 1; m.region.offset != want {
		t.Fatalf("PgDn scrolled %d rows, want %d", m.region.offset, want)
	}
	// The line the marker covered on the first page leads the second page.
	covered := stripANSI(m.regionLines(false)[m.region.offset])
	if strings.Contains(firstPage, covered) || !strings.Contains(stripANSI(m.View()), covered) {
		t.Fatalf("paging skipped %q", covered)
	}
	press(tea.KeyMsg{Type: tea.KeyDown})
	if m.cursor != 1 || m.region.pr.Number != 2 {
		t.Fatal("↓ did not select the next PR")
	}
	press(tea.KeyMsg{Type: tea.KeyHome})
	if m.cursor != 0 {
		t.Fatal("Home did not select the first PR")
	}
}

func TestShortTerminalCollapsesTheRegionAndZoomStillWorks(t *testing.T) {
	m := splitModel(t, &reviewFake{})
	m.setRegionRuns(completedRun(2))
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: regionSplitHeight - 1})
	m = next.(Model)
	view := stripANSI(m.View())
	if m.boardLayout().regionRows != 0 || strings.Contains(view, "── Review") || !strings.Contains(view, "Review: ") {
		t.Fatalf("short terminal kept the region:\n%s", view)
	}
	if lines := strings.Split(view, "\n"); len(lines) > m.height {
		t.Fatalf("rendered %d lines in %d rows", len(lines), m.height)
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("v")})
	m = next.(Model)
	zoomed := strings.Split(stripANSI(m.View()), "\n")
	if !m.zoom || zoomed[1] != m.region.pr.URL || !strings.Contains(strings.Join(zoomed, "\n"), "Latest review") {
		t.Fatalf("zoom unavailable on a short terminal:\n%s", strings.Join(zoomed, "\n"))
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if next.(Model).zoom {
		t.Fatal("Esc did not leave zoom")
	}
}

func TestBoardKeysActOnTheSelectedPR(t *testing.T) {
	m := splitModel(t, &reviewFake{})
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	m = next.(Model)
	if cmd == nil || m.reviewJobs[m.region.pr.URL] != "queued" || !strings.Contains(stripANSI(m.View()), "Request: queued") {
		t.Fatalf("n did not queue a review from the board:\n%s", stripANSI(m.View()))
	}
	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	m = next.(Model)
	if cmd == nil || !m.region.settingsLoading {
		t.Fatal("s did not load repository settings from the board")
	}
	m.region.settingsLoading = false
	m.setRegionRuns(completedRun(1))
	m = m.WithPublications("", &publicationFake{})
	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	if cmd == nil || !next.(Model).publishing[m.region.pr.URL] {
		t.Fatal("c did not publish from the board")
	}
}

func TestZoomPinsThePRAcrossARefreshThatReordersRows(t *testing.T) {
	m := splitModel(t, &reviewFake{})
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("v")})
	m = next.(Model)
	now := time.Now()
	reordered := []gh.PullRequest{splitPR(2, time.Minute), splitPR(1, time.Hour)}
	for i := range reordered {
		reordered[i].UpdatedAt = now.Add(-time.Duration(i+1) * time.Minute)
	}
	next, _ = m.Update(snapshotMsg{Snapshot: discovery.Snapshot{Views: []discovery.ViewData{{View: m.cfg.Views[0], PRs: reordered, UpdatedAt: now, ObservedAt: now}, {View: m.cfg.Views[1]}}, StartedAt: now, FinishedAt: now}})
	m = next.(Model)
	if !m.zoom || m.region.pr.Number != 1 {
		t.Fatalf("zoom lost its PR after a refresh: %+v", m.region.pr)
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("v")})
	m = next.(Model)
	if selected, _ := m.selectedPR(); m.zoom || selected.Number != 1 || m.cursor != 1 {
		t.Fatalf("leaving zoom did not follow the PR to its new row: cursor=%d selected=%d", m.cursor, selected.Number)
	}
}

func TestBoardFooterNamesTheStopTarget(t *testing.T) {
	m := splitModel(t, &reviewFake{})
	m.setRegionRuns(reviewmemory.Run{ID: "active-123456", Outcome: reviewmemory.Outcome{Status: reviewmemory.Running}})
	if footer := stripANSI(m.renderFooter(m.footerHelpLines())); !strings.Contains(footer, "t stop active-1") {
		t.Fatalf("footer does not name the stop target: %q", footer)
	}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")})
	if cmd == nil || next.(Model).stopping[m.region.pr.URL] != "active-123456" {
		t.Fatal("t did not stop the run from the board")
	}
}

func TestZoomWithoutLocalReviewsExplainsTheStateDirectory(t *testing.T) {
	m := layoutModel(t, 100)
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("v")})
	if zoomed := next.(Model); zoomed.zoom || !strings.Contains(zoomed.warning, "HERDR_PLUGIN_STATE_DIR") {
		t.Fatalf("v without reviews: zoom=%v warning=%q", zoomed.zoom, zoomed.warning)
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	if !strings.Contains(next.(Model).warning, "HERDR_PLUGIN_STATE_DIR") {
		t.Fatal("n without reviews stayed silent")
	}
}

func TestNarrowTerminalKeepsRevisionMetadataInTheRegion(t *testing.T) {
	m := splitModel(t, &reviewFake{})
	m.setRegionRuns(completedRun(1))
	next, _ := m.Update(tea.WindowSizeMsg{Width: 40, Height: 35})
	m = next.(Model)
	for _, zoom := range []bool{false, true} {
		m.zoom = zoom
		view := stripANSI(m.View())
		for _, want := range []string{"current observed revision", "P0:1 P1:0 P2:0 P3:0"} {
			if !strings.Contains(view, want) {
				t.Fatalf("zoom=%v width 40 lost %q:\n%s", zoom, want, view)
			}
		}
		for _, line := range strings.Split(m.View(), "\n") {
			if lipgloss.Width(line) > m.width {
				t.Fatalf("zoom=%v overflow %q", zoom, line)
			}
		}
	}
}

func TestSettingsFormKeepsThePinnedPRAcrossAReorder(t *testing.T) {
	m := splitModel(t, &reviewFake{})
	setup, err := newRepositorySetup(m.cfg, "acme/repo", installedAgents(fakeLookPath("pi")))
	if err != nil {
		t.Fatal(err)
	}
	m.region.setup = setup
	now := time.Now()
	reordered := []gh.PullRequest{splitPR(2, time.Minute), splitPR(1, time.Hour)}
	next, _ := m.Update(snapshotMsg{Snapshot: discovery.Snapshot{Views: []discovery.ViewData{{View: m.cfg.Views[0], PRs: reordered, UpdatedAt: now, ObservedAt: now}, {View: m.cfg.Views[1]}}, StartedAt: now, FinishedAt: now}})
	m = next.(Model)
	if m.region.pr.Number != 1 || m.region.setup == nil {
		t.Fatalf("refresh replaced the form's PR: %+v", m.region.pr)
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(Model)
	if selected, _ := m.selectedPR(); m.region.setup != nil || selected.Number != 1 || m.region.pr.Number != 1 {
		t.Fatalf("leaving the form lost the pinned PR: selected=%d region=%d", selected.Number, m.region.pr.Number)
	}
}

func TestSeparatorClosesTheTableAboveTheSelection(t *testing.T) {
	m := splitModel(t, &reviewFake{})
	lay := m.boardLayout()
	lines := strings.Split(stripANSI(m.View()), "\n")
	if rule := lines[lay.selectedURLRow-1]; rule != strings.Repeat("─", m.width) {
		t.Fatalf("row above the URL = %q", rule)
	}
	if lines[lay.selectedURLRow] != m.region.pr.URL {
		t.Fatalf("URL moved: %q", lines[lay.selectedURLRow])
	}
	empty := emptyViewModel(t, 80, defaultView(t, config.ViewAll))
	if view := stripANSI(empty.View()); strings.Contains(view, strings.Repeat("─", 80)+"\nNo PR selected") {
		t.Fatalf("empty view drew a separator:\n%s", view)
	}
}

func TestReadsCoalesceIntoOneActiveRead(t *testing.T) {
	m := splitModel(t, &reviewFake{})
	first := m.requestOverview()
	if first == nil || !m.overviewReading {
		t.Fatal("idle board did not start a read")
	}
	if second := m.requestOverview(); second != nil || !m.overviewQueued {
		t.Fatal("request during a read did not wait")
	}
	next, again := m.Update(first().(reviewOverviewMsg))
	m = next.(Model)
	if again == nil || m.overviewQueued || !m.overviewReading {
		t.Fatal("finished read did not run the queued request once")
	}
	next, more := m.Update(again().(reviewOverviewMsg))
	if more != nil || next.(Model).overviewReading {
		t.Fatal("idle board kept reading")
	}
}

func TestCollapsedRegionShowsActionFeedbackInTheFooter(t *testing.T) {
	m := splitModel(t, &reviewFake{})
	m = m.WithPublications("", &publicationFake{})
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: regionSplitHeight - 1})
	next, _ = next.(Model).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	m = next.(Model)
	if view := stripANSI(m.View()); !strings.Contains(view, "No completed local review is available for publication.") {
		t.Fatalf("collapsed region hid the publication feedback:\n%s", view)
	}
	// A multiline message keeps the meta line to one row.
	m.region.message = "unknown setting(s):\n  review.notify_me\n  review.other"
	lines := strings.Split(m.View(), "\n")
	if len(lines) > m.height || !strings.Contains(stripANSI(lines[len(lines)-1]), "unknown setting(s): review.notify_me review.other") {
		t.Fatalf("multiline feedback broke the footer budget (%d lines):\n%s", len(lines), stripANSI(m.View()))
	}
}

func TestPinnedPRKeepsItsPublicationsAndMetadataOutsideTheViews(t *testing.T) {
	m := splitModel(t, &reviewFake{})
	pr := m.region.pr
	run := reviewmemory.Run{ID: "local", Identity: dispatch.Identity(pr), Outcome: reviewmemory.Outcome{Status: reviewmemory.Completed}}
	backend := &overviewReviewBackend{snapshot: reviewmemory.Snapshot{Runs: []reviewmemory.Run{run}, Active: map[string]bool{}}}
	publisher := &overviewPublicationBackend{attempts: []publication.Attempt{{RunID: run.ID, Identity: run.Identity, Actor: "ada", GitHubID: 7, Status: publication.Published}}}
	m = m.WithReviews(context.Background(), backend).WithPublications("", publisher)
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("v")})
	m = next.(Model)
	// The PR leaves every view while it stays pinned in zoom, with a new head.
	moved := pr
	moved.HeadOID = strings.Repeat("c", 40)
	moved.MetadataObservedAt = time.Now()
	moved.ViewerReviews = &gh.ReviewObservation{Actor: "ada", Complete: true, ObservedAt: time.Now(), Reviews: []gh.SubmittedReview{{ID: 7, HeadOID: pr.HeadOID, State: "COMMENTED", SubmittedAt: time.Now()}}}
	m.views[0].PRs = []gh.PullRequest{splitPR(2, time.Hour)}
	m.views[1].PRs = []gh.PullRequest{moved}
	next, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 35})
	m = next.(Model)
	if m.region.pr.HeadOID != moved.HeadOID {
		t.Fatalf("pinned PR kept a stale head: %q", m.region.pr.HeadOID)
	}
	m.views[1].PRs = nil
	read := m.reviewOverviewCmd()().(reviewOverviewMsg)
	if data := read.read.regions[strings.ToLower(pr.URL)]; len(data.publications) != 1 || !strings.Contains(read.rows[pr.URL].summary.postedDetail, "PR Board") {
		t.Fatalf("pinned PR lost its publications: %+v row=%+v", data, read.rows[pr.URL].summary)
	}
}

func TestStartupReadHoldsTheGuardUntilItReturns(t *testing.T) {
	m, err := NewModel(testConfig(), fakeLoader{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	m = m.WithReviews(context.Background(), &reviewFake{})
	m.loading = false
	m.views[0].PRs = []gh.PullRequest{splitPR(1, time.Hour), splitPR(2, 2*time.Hour)}
	// The first tick is the startup read.
	next, cmd := m.Update(reviewOverviewTickMsg{})
	m = next.(Model)
	if cmd == nil || !m.overviewReading {
		t.Fatal("startup tick did not start a guarded read")
	}
	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = next.(Model)
	if cmd != nil || m.overviewQueued || m.region.pr.Number != 2 {
		t.Fatal("selection change overlapped the startup read")
	}
	next, _ = m.Update(reviewOverviewTickMsg{})
	if !next.(Model).overviewQueued {
		t.Fatal("tick during the startup read did not wait for it")
	}
}

func TestTinyPanesKeepContentAndWidth(t *testing.T) {
	m := splitModel(t, &reviewFake{})
	running := completedRun(3)
	running.Status = reviewmemory.Running
	m.setRegionRuns(running)
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("v")})
	m = next.(Model)
	for _, size := range [][2]int{{10, 5}, {12, 6}, {40, 5}, {40, 3}, {12, 3}} {
		next, _ = m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		m = next.(Model)
		lines := strings.Split(m.View(), "\n")
		if len(lines) > size[1] || stripANSI(lines[1]) != truncate(m.region.pr.URL, size[0]) {
			t.Fatalf("%v: %d lines, URL row %q", size, len(lines), stripANSI(lines[1]))
		}
		for _, line := range lines {
			if lipgloss.Width(line) > size[0] {
				t.Fatalf("%v: line overflows: %q", size, line)
			}
		}
		_, rows := m.zoomViewport()
		if rows != 1 {
			continue
		}
		next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
		m = next.(Model)
		if content := stripANSI(strings.Split(m.View(), "\n")[2]); strings.HasPrefix(content, "▼") {
			t.Fatalf("%v: one-row viewport shows only the marker: %q", size, content)
		}
	}
}

func TestInFlightActionsPinTheRegionAcrossAReorder(t *testing.T) {
	now := time.Now()
	reorder := func(m Model) Model {
		reordered := []gh.PullRequest{splitPR(2, time.Minute), splitPR(1, time.Hour)}
		next, _ := m.Update(snapshotMsg{Snapshot: discovery.Snapshot{Views: []discovery.ViewData{{View: m.cfg.Views[0], PRs: reordered, UpdatedAt: now, ObservedAt: now}, {View: m.cfg.Views[1]}}, StartedAt: now, FinishedAt: now}})
		return next.(Model)
	}
	for name, arm := range map[string]func(*Model){
		"settings loading": func(m *Model) { m.region.settingsLoading = true },
		"publishing":       func(m *Model) { m.publishing[m.region.pr.URL] = true },
		"stopping":         func(m *Model) { m.stopping[m.region.pr.URL] = "run" },
	} {
		m := splitModel(t, &reviewFake{})
		arm(&m)
		m = reorder(m)
		if selected, _ := m.selectedPR(); m.region.pr.Number != 1 || selected.Number != 1 || m.cursor != 1 {
			t.Fatalf("%s: refresh swapped the PR under an in-flight action or left the selection behind", name)
		}
		// The user can still move on; the region and the next action follow.
		next, _ := m.Update(tea.KeyMsg{Type: tea.KeyUp})
		m = next.(Model)
		next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
		m = next.(Model)
		if m.region.pr.Number != 2 || cmd == nil || m.reviewJobs[m.region.pr.URL] != "queued" {
			t.Fatalf("%s: navigation during the action left n on the old PR: region=%d jobs=%v", name, m.region.pr.Number, m.reviewJobs)
		}
	}
	m := splitModel(t, &reviewFake{})
	if m = reorder(m); m.region.pr.Number != 2 {
		t.Fatal("idle region did not follow the selection")
	}
}

func TestReadErrorsClearOnRecoveryAndKeepTheActionMessage(t *testing.T) {
	m := splitModel(t, &reviewFake{})
	m.region.message = "Publishing comment…"
	failed := reviewOverviewMsg{epoch: m.epoch, read: overviewRead{err: fmt.Errorf("cannot read history")}}
	next, _ := m.Update(failed)
	m = next.(Model)
	view := stripANSI(m.View())
	if !strings.Contains(view, "Local reviews unavailable: cannot read history") || !strings.Contains(view, "Publishing comment…") {
		t.Fatalf("read error hid or replaced the action message:\n%s", view)
	}
	next, _ = m.Update(reviewOverviewMsg{epoch: m.epoch})
	m = next.(Model)
	view = stripANSI(m.View())
	if strings.Contains(view, "Local reviews unavailable") || !strings.Contains(view, "Publishing comment…") {
		t.Fatalf("recovered read kept the error or lost the action message:\n%s", view)
	}
}

func TestPinnedPRThatLeavesTheViewYieldsToTheSelection(t *testing.T) {
	m := splitModel(t, &reviewFake{})
	m.stopping[m.region.pr.URL] = "run"
	now := time.Now()
	next, _ := m.Update(snapshotMsg{Snapshot: discovery.Snapshot{Views: []discovery.ViewData{{View: m.cfg.Views[0], PRs: []gh.PullRequest{splitPR(2, time.Hour)}, UpdatedAt: now, ObservedAt: now}, {View: m.cfg.Views[1]}}, StartedAt: now, FinishedAt: now}})
	m = next.(Model)
	lines := strings.Split(stripANSI(m.View()), "\n")
	shown := lines[m.boardLayout().selectedURLRow]
	if m.region.pr.Number != 2 || shown != m.region.pr.URL {
		t.Fatalf("displayed %q while actions target %s", shown, m.region.pr.URL)
	}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	if cmd == nil || next.(Model).reviewJobs[m.region.pr.URL] != "queued" {
		t.Fatal("n did not target the displayed PR")
	}
}

func TestSplitShowsThePublicationTargetBesideANewerFailure(t *testing.T) {
	m := splitModel(t, &reviewFake{})
	completed := completedRun(1)
	failed := completedRun(0)
	failed.ID, failed.Status, failed.Message = "run-2", reviewmemory.Failed, "reviewer crashed"
	m.setRegionRuns(completed, failed)
	view := stripANSI(m.View())
	for _, want := range []string{"Review · failed", "run 1/2", "Review · completed", "run 2/2", "Publication target"} {
		if !strings.Contains(view, want) {
			t.Fatalf("split lacks %q:\n%s", want, view)
		}
	}
}

// corruptPublications fails the publication read for one PR number only.
type corruptPublications struct {
	publicationFake
	broken int
}

func (b *corruptPublications) HistoryForRuns(runs []reviewmemory.Run) ([]publication.Attempt, error) {
	var attempts []publication.Attempt
	for _, run := range runs {
		if run.Identity.Number == b.broken {
			return nil, fmt.Errorf("publication record for #%d is corrupt", b.broken)
		}
		attempts = append(attempts, publication.Attempt{RunID: run.ID, Identity: run.Identity, Actor: "ada", GitHubID: 7, Status: publication.Published})
	}
	return attempts, nil
}

func TestOneCorruptPublicationRecordLeavesOtherPRsIntact(t *testing.T) {
	m := splitModel(t, &reviewFake{})
	first, second := m.views[0].PRs[0], m.views[0].PRs[1]
	for i := range m.views[0].PRs {
		m.views[0].PRs[i].ViewerReviews = &gh.ReviewObservation{Actor: "ada", Complete: true, ObservedAt: time.Now(), Reviews: []gh.SubmittedReview{{ID: 7, HeadOID: first.HeadOID, State: "COMMENTED", SubmittedAt: time.Now()}}}
	}
	runs := []reviewmemory.Run{
		{ID: "one", Identity: dispatch.Identity(first), Outcome: reviewmemory.Outcome{Status: reviewmemory.Completed}},
		{ID: "two", Identity: dispatch.Identity(second), Outcome: reviewmemory.Outcome{Status: reviewmemory.Completed}},
	}
	backend := &overviewReviewBackend{snapshot: reviewmemory.Snapshot{Runs: runs, Active: map[string]bool{}}}
	m = m.WithReviews(context.Background(), backend).WithPublications("", &corruptPublications{broken: second.Number})
	read := m.reviewOverviewCmd()().(reviewOverviewMsg)
	intact := read.read.regions[strings.ToLower(first.URL)]
	broken := read.read.regions[strings.ToLower(second.URL)]
	if len(intact.publications) != 1 || intact.err != nil || read.rows[first.URL].summary.posted != "PR Board" {
		t.Fatalf("intact PR lost its publications: %+v row=%+v", intact, read.rows[first.URL].summary)
	}
	if broken.err == nil || read.rows[second.URL].summary.posted != "?" {
		t.Fatalf("corrupt PR did not report its own failure: %+v row=%+v", broken, read.rows[second.URL].summary)
	}
}

func TestSelectingARunningPRKeepsItsRowVisible(t *testing.T) {
	m, err := NewModel(testConfig(), fakeLoader{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	m = m.WithReviews(context.Background(), &reviewFake{})
	m.loading = false
	for number := 1; number <= 12; number++ {
		m.views[0].PRs = append(m.views[0].PRs, splitPR(number, time.Duration(number)*time.Hour))
	}
	last := m.views[0].PRs[11]
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 18})
	m = next.(Model)
	running := reviewmemory.Run{ID: "active-123456", Identity: dispatch.Identity(last), Outcome: reviewmemory.Outcome{Status: reviewmemory.Running}}
	next, _ = m.Update(reviewOverviewMsg{epoch: m.epoch, read: overviewRead{regions: map[string]regionData{strings.ToLower(last.URL): {runs: []reviewmemory.Run{running}}}}})
	m = next.(Model)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnd})
	m = next.(Model)
	lay := m.boardLayout()
	if m.region.pr.Number != last.Number || m.cursor-m.offset >= lay.visibleRows {
		t.Fatalf("selected row %d is outside the %d visible rows (offset %d)", m.cursor, lay.visibleRows, m.offset)
	}
	if view := stripANSI(m.View()); !strings.Contains(view, "t stop active-1") || !strings.Contains(view, "#12") {
		t.Fatalf("footer or row missing:\n%s", view)
	}
}

func TestPostedSummaryFollowsTheRefreshedHeadNotAStaleRead(t *testing.T) {
	m := splitModel(t, &reviewFake{})
	pr := m.region.pr
	stale := reviewOverviewMsg{epoch: m.epoch, rows: map[string]reviewOverviewRow{pr.URL: {identity: dispatch.Identity(pr), summary: reviewRowSummary{postedDetail: "GitHub · current revision"}}}}
	next, _ := m.Update(stale)
	m = next.(Model)
	if view := stripANSI(m.View()); !strings.Contains(view, "Posted: GitHub · current revision") {
		t.Fatalf("read did not reach the region:\n%s", view)
	}
	// A new head arrives; the read above described the old one.
	moved := pr
	moved.HeadOID = strings.Repeat("c", 40)
	moved.MetadataObservedAt = time.Now()
	now := time.Now()
	next, _ = m.Update(snapshotMsg{Snapshot: discovery.Snapshot{Views: []discovery.ViewData{{View: m.cfg.Views[0], PRs: []gh.PullRequest{moved, splitPR(2, 2*time.Hour)}, UpdatedAt: now, ObservedAt: now}, {View: m.cfg.Views[1]}}, StartedAt: now, FinishedAt: now}})
	m = next.(Model)
	view := stripANSI(m.View())
	if strings.Contains(view, "Posted: GitHub · current revision") || !strings.Contains(view, "Posted: Loading posted reviews") {
		t.Fatalf("stale read labeled an old post as current:\n%s", view)
	}
}

func TestUnreadPRShowsLoadingNotNoRuns(t *testing.T) {
	m := splitModel(t, &reviewFake{})
	if view := stripANSI(m.View()); !strings.Contains(view, "Loading local reviews") || strings.Contains(view, "No local review runs") {
		t.Fatalf("unread PR claimed to have no runs:\n%s", view)
	}
	next, _ := m.Update(reviewOverviewMsg{epoch: m.epoch, read: overviewRead{regions: map[string]regionData{strings.ToLower(m.region.pr.URL): {}}}})
	if view := stripANSI(next.(Model).View()); !strings.Contains(view, "No local review runs") || strings.Contains(view, "Loading local reviews") {
		t.Fatalf("read without runs still shows loading:\n%s", view)
	}
}

func TestZoomFooterIgnoresTheBoardSummary(t *testing.T) {
	m := splitModel(t, &reviewFake{})
	next, _ := m.Update(tea.WindowSizeMsg{Width: 60, Height: 16})
	next, _ = next.(Model).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("v")})
	m = next.(Model)
	help, _ := m.zoomViewport()
	m.reviewRows = map[string]reviewOverviewRow{m.region.pr.URL: {identity: dispatch.Identity(m.region.pr), summary: reviewRowSummary{detail: strings.Repeat("detail ", 30), postedDetail: strings.Repeat("posted ", 30)}}}
	if again, _ := m.zoomViewport(); len(again) != len(help) {
		t.Fatalf("board summary changed the zoom controls from %d to %d rows", len(help), len(again))
	}
}

func TestPendingActionsSurviveNavigation(t *testing.T) {
	m := splitModel(t, &reviewFake{}).WithPublications("", &publicationFake{})
	m.setRegionRuns(completedRun(1))
	url := m.region.pr.URL
	press := func(key tea.KeyMsg) tea.Cmd {
		next, cmd := m.Update(key)
		m = next.(Model)
		return cmd
	}
	if press(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")}) == nil {
		t.Fatal("c did not publish")
	}
	press(tea.KeyMsg{Type: tea.KeyDown})
	press(tea.KeyMsg{Type: tea.KeyUp})
	m.setRegionRuns(completedRun(1))
	if press(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")}) != nil || !m.publishing[url] {
		t.Fatal("navigation let a second publication start while the first was in flight")
	}
	next, _ := m.Update(publicationDoneMsg{url: url})
	if next.(Model).publishing[url] {
		t.Fatal("finished publication stayed pending")
	}

	// A settings response for a request the user abandoned by moving on
	// never opens the form, even after a new request for the same PR.
	m = splitModel(t, &reviewFake{})
	m.configPath = filepath.Join(t.TempDir(), "config.toml")
	if _, err := config.Load(m.configPath); err != nil {
		t.Fatal(err)
	}
	abandoned := press(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	press(tea.KeyMsg{Type: tea.KeyDown})
	press(tea.KeyMsg{Type: tea.KeyUp})
	next, _ = m.Update(abandoned())
	if next.(Model).region.setup != nil {
		t.Fatal("late settings response reopened an abandoned form")
	}
	current := press(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	next, _ = m.Update(abandoned())
	m = next.(Model)
	if m.region.setup != nil || !m.region.settingsLoading {
		t.Fatal("abandoned response consumed the new request")
	}
	next, _ = m.Update(current())
	if next.(Model).region.setup == nil {
		t.Fatal("the current request's response did not open the form")
	}
}

func TestPendingStopSettlesForAPRThatLeftEveryView(t *testing.T) {
	m := splitModel(t, &reviewFake{})
	gone := "https://github.com/acme/repo/pull/9"
	m.stopping[gone] = "run-9"
	finished := reviewmemory.Run{ID: "run-9", Outcome: reviewmemory.Outcome{Status: reviewmemory.Failed, Message: reviewmemory.ErrStopped.Error()}}
	next, _ := m.Update(reviewOverviewMsg{epoch: m.epoch, read: overviewRead{runs: map[string]reviewmemory.Run{"run-9": finished}}})
	if next.(Model).stopping[gone] != "" {
		t.Fatal("stop for an unlisted PR stayed pending")
	}
	m.stopping[gone] = "run-9"
	next, _ = m.Update(reviewOverviewMsg{epoch: m.epoch, read: overviewRead{err: fmt.Errorf("cannot read history")}})
	if next.(Model).stopping[gone] != "run-9" {
		t.Fatal("failed read settled a pending stop")
	}
}
