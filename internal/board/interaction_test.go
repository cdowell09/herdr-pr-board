package board

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cdowell09/herdr-pr-board/internal/config"
	"github.com/cdowell09/herdr-pr-board/internal/discovery"
	"github.com/cdowell09/herdr-pr-board/internal/dispatch"
	gh "github.com/cdowell09/herdr-pr-board/internal/github"
	"github.com/cdowell09/herdr-pr-board/internal/reviewmemory"
	tea "github.com/charmbracelet/bubbletea"
)

func press(m Model, keys ...tea.KeyMsg) Model {
	for _, key := range keys {
		next, _ := m.Update(key)
		m = next.(Model)
	}
	return m
}

func runes(value string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(value)} }

// selectedURL reads the selected PR's URL from the rendered board.
func selectedURL(t *testing.T, m Model) string {
	t.Helper()
	lines := strings.Split(stripANSI(m.View()), "\n")
	return strings.TrimSpace(lines[m.boardLayout().selectedURLRow])
}

func TestFilterInputMovesTheSelectionAndBackspaceLeavesIt(t *testing.T) {
	m := press(layoutModel(t, 120), runes("/"), runes("acme"))
	first := selectedURL(t, m)
	m = press(m, tea.KeyMsg{Type: tea.KeyDown})
	second := selectedURL(t, m)
	if second == first || !strings.Contains(second, "acme") {
		t.Fatalf("down did not move the filtered selection: %q then %q", first, second)
	}
	if !strings.Contains(stripANSI(m.View()), "filter: acme▌") {
		t.Fatal("down left filter input")
	}
	m = press(m, tea.KeyMsg{Type: tea.KeyUp})
	if got := selectedURL(t, m); got != first {
		t.Fatalf("up selected %q, want %q", got, first)
	}

	// Enter keeps the filter and the selection and leaves filter input.
	m = press(m, tea.KeyMsg{Type: tea.KeyDown}, tea.KeyMsg{Type: tea.KeyEnter})
	if got := selectedURL(t, m); got != second {
		t.Fatalf("enter moved the selection to %q, want %q", got, second)
	}
	view := stripANSI(m.View())
	if !strings.Contains(view, "filter: acme") || strings.Contains(view, "filter: acme▌") {
		t.Fatalf("enter did not keep the filter and leave input:\n%s", view)
	}

	// Backspace on an empty filter leaves filter input.
	m = press(m, runes("/"))
	for range "acme" {
		m = press(m, tea.KeyMsg{Type: tea.KeyBackspace})
	}
	if !strings.Contains(stripANSI(m.View()), "filter: ▌") {
		t.Fatal("backspace did not empty the filter")
	}
	m = press(m, tea.KeyMsg{Type: tea.KeyBackspace})
	if strings.Contains(stripANSI(m.View()), "filter:") {
		t.Fatal("backspace on an empty filter did not leave filter input")
	}
	// Board keys work again: q quits.
	if _, cmd := m.Update(runes("q")); cmd == nil {
		t.Fatal("q did not quit after the filter closed")
	}
}

func TestFilterMatchesReviewAndCICellText(t *testing.T) {
	m := layoutModel(t, 120)
	prs := m.views[0].PRs
	m.reviewRows = map[string]reviewOverviewRow{
		prs[0].URL: {identity: dispatch.Identity(prs[0]), summary: reviewRowSummary{state: "completed", findings: reviewmemory.SeverityCounts{1, 0, 2, 0}}},
		prs[1].URL: {identity: dispatch.Identity(prs[1]), summary: reviewRowSummary{state: "failed"}},
	}
	for filter, want := range map[string]string{
		"failure": prs[2].URL, // CI
		"p0":      prs[0].URL, // REVIEW worst severity
		"FAILED":  prs[1].URL, // REVIEW state, case-insensitive
	} {
		view := stripANSI(press(m, runes("/"), runes(filter)).View())
		for _, pr := range prs {
			if shown := strings.Contains(view, "#"+strconv.Itoa(pr.Number)+" "); shown != (pr.URL == want) {
				t.Fatalf("filter %q shows #%d = %v:\n%s", filter, pr.Number, shown, view)
			}
		}
	}
}

func TestFilteredSelectionFollowsThePRAcrossARefresh(t *testing.T) {
	m := press(layoutModel(t, 120), runes("/"), runes("acme"), tea.KeyMsg{Type: tea.KeyDown}, tea.KeyMsg{Type: tea.KeyEnter})
	selected := selectedURL(t, m)
	// The refresh makes the selected PR the newest, so the rows swap.
	views := append([]discovery.ViewData(nil), m.views...)
	views[0].PRs = append([]gh.PullRequest(nil), views[0].PRs...)
	for i := range views[0].PRs {
		if views[0].PRs[i].URL == selected {
			views[0].PRs[i].UpdatedAt = time.Now()
		}
	}
	next, _ := m.Update(snapshotMsg{Snapshot: discovery.Snapshot{Views: views, FinishedAt: time.Now()}})
	if got := selectedURL(t, next.(Model)); got != selected {
		t.Fatalf("refresh moved the selection to %q, want %q", got, selected)
	}
}

// renderedRow returns the View() row that contains text.
func renderedRow(t *testing.T, m Model, text string) int {
	t.Helper()
	for y, line := range strings.Split(stripANSI(m.View()), "\n") {
		if strings.Contains(line, text) {
			return y
		}
	}
	t.Fatalf("no rendered row contains %q:\n%s", text, stripANSI(m.View()))
	return -1
}

func click(m Model, x, y int) (Model, tea.Cmd) {
	next, cmd := m.Update(tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	return next.(Model), cmd
}

func TestClickSelectsARowAndAClickOnTheSelectedRowOpensIt(t *testing.T) {
	m := layoutModel(t, 120)
	opened := ""
	m.openBrowser = func(url string) tea.Cmd { opened = url; return nil }
	y := renderedRow(t, m, "#42 ")
	m, _ = click(m, 2, y)
	if opened != "" || selectedURL(t, m) != "https://github.com/acme/web-ui/pull/42" {
		t.Fatalf("first click opened %q or did not select the row", opened)
	}
	m, _ = click(m, 2, renderedRow(t, m, "#42 "))
	if opened != "https://github.com/acme/web-ui/pull/42" {
		t.Fatalf("click on the selected row opened %q", opened)
	}
}

func TestWheelActsOnTheAreaUnderThePointer(t *testing.T) {
	m := splitModel(t, &reviewFake{})
	m.setRegionRuns(completedRun(12))
	wheel := func(m Model, y int) Model {
		next, _ := m.Update(tea.MouseMsg{X: 2, Y: y, Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress})
		return next.(Model)
	}
	regionY := renderedRow(t, m, "REVIEW      ●")
	if scrolled := wheel(m, regionY); scrolled.cursor != 0 || scrolled.region.offset != mouseStep {
		t.Fatalf("wheel over the region: cursor=%d offset=%d", scrolled.cursor, scrolled.region.offset)
	}
	if moved := wheel(m, renderedRow(t, m, "#1 ")); selectedURL(t, moved) != splitPR(2, 0).URL {
		t.Fatalf("wheel over the table did not move the selection: %s", selectedURL(t, moved))
	}
}

// clickText clicks the first cell of text where View() renders it.
func clickText(t *testing.T, m Model, text string) (Model, tea.Cmd) {
	t.Helper()
	y := renderedRow(t, m, text)
	x, _ := displayColumn(stripANSI(strings.Split(m.View(), "\n")[y]), text)
	return click(m, x, y)
}

func TestFooterHintClicksReplayTheKey(t *testing.T) {
	for _, width := range []int{50, 120} {
		m := layoutModel(t, width)
		if help, _ := clickText(t, m, "? help"); !strings.Contains(stripANSI(help.View()), "Keyboard controls") {
			t.Fatalf("width %d: ? help click did not open help", width)
		}
		filtering, _ := clickText(t, m, "/ filter")
		if !strings.Contains(stripANSI(filtering.View()), "filter: ▌") {
			t.Fatalf("width %d: / filter click did not start the filter", width)
		}
		// In filter input the click types the key, the same as the key does.
		if typed, _ := clickText(t, filtering, "v zoom"); !strings.Contains(stripANSI(typed.View()), "filter: v▌") {
			t.Fatalf("width %d: v click in filter input did not type v", width)
		}
		// The separator between hints is not a hitbox.
		y := renderedRow(t, m, "? help")
		x, _ := displayColumn(stripANSI(strings.Split(m.View(), "\n")[y]), "? help")
		if next, cmd := click(m, x-2, y); cmd != nil || next.View() != m.View() {
			t.Fatalf("width %d: separator click acted", width)
		}
	}
	zoomed := panelModel(t)
	if board, _ := clickText(t, zoomed, "v board"); board.zoom {
		t.Fatal("v board click did not leave zoom")
	}
}

func TestPublicationActionsAskForConfirmation(t *testing.T) {
	for _, confirm := range []tea.KeyMsg{{Type: tea.KeyEnter}, runes("y")} {
		m := panelModel(t)
		backend := &publicationFake{}
		m = m.WithPublications(t.TempDir(), backend)
		m.setRegionRuns(reviewmemory.Run{ID: "completed", Outcome: reviewmemory.Outcome{Status: reviewmemory.Completed}})

		next, cmd := m.Update(runes("a"))
		m = next.(Model)
		if cmd != nil || backend.action != "" {
			t.Fatal("a published without confirmation")
		}
		if view := stripANSI(m.View()); !strings.Contains(view, "approve acme/repo#1? enter/y") {
			t.Fatalf("a did not show the confirmation prompt:\n%s", view)
		}
		next, cmd = m.Update(confirm)
		m = next.(Model)
		if cmd == nil {
			t.Fatalf("%s did not publish", confirm)
		}
		m.Update(cmd())
		if backend.action != config.PublishApprove || strings.Contains(stripANSI(m.View()), "enter/y") {
			t.Fatalf("%s published %q or kept the prompt", confirm, backend.action)
		}
	}

	m := panelModel(t)
	backend := &publicationFake{}
	m = m.WithPublications(t.TempDir(), backend)
	m.setRegionRuns(reviewmemory.Run{ID: "completed", Outcome: reviewmemory.Outcome{Status: reviewmemory.Completed}})
	m = press(m, runes("x"))
	if !strings.Contains(stripANSI(m.View()), "request changes acme/repo#1? enter/y") {
		t.Fatalf("x did not show the confirmation prompt:\n%s", stripANSI(m.View()))
	}
	next, cmd := m.Update(runes("t"))
	m = next.(Model)
	if cmd != nil || len(m.stopping) != 0 || strings.Contains(stripANSI(m.View()), "enter/y") {
		t.Fatal("another key did not cancel the confirmation without acting")
	}
	// A mouse press outside the footer controls also cancels.
	m = press(m, runes("x"))
	if m, _ = click(m, 0, 0); strings.Contains(stripANSI(m.View()), "enter/y") {
		t.Fatal("a click did not cancel the confirmation")
	}
	// A click on the Enter footer control cancels instead of confirming.
	m.zoom = false
	m = press(m, runes("x"))
	m, cmd = clickText(t, m, "Enter open")
	if cmd != nil || backend.action != "" || strings.Contains(stripANSI(m.View()), "enter/y") {
		t.Fatal("an Enter footer click confirmed the publication or kept the prompt")
	}
	// After the cancel, Enter is a plain key again.
	opened := ""
	m.openBrowser = func(url string) tea.Cmd { opened = url; return nil }
	if m.Update(tea.KeyMsg{Type: tea.KeyEnter}); opened != m.region.pr.URL {
		t.Fatal("enter after a cancel did not open the PR")
	}
}
