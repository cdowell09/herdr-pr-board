package board

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/cdowell09/herdr-pr-board/internal/config"
	"github.com/cdowell09/herdr-pr-board/internal/discovery"
	gh "github.com/cdowell09/herdr-pr-board/internal/github"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

var layoutWidths = []int{40, 50, 60, 70, 80, 100, 120, 160}

func layoutModel(t *testing.T, width int) Model {
	t.Helper()
	cfg := testConfig()
	now := time.Now()
	model, err := NewModel(cfg, fakeLoader{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	model.views = []discovery.ViewData{
		{View: cfg.Views[0], PRs: []gh.PullRequest{
			{Repository: "cdowell09/herdr-pr-board", Number: 1234, Title: "Add responsive layouts and cell-width truncation", URL: "https://github.com/cdowell09/herdr-pr-board/pull/1234", Author: "cdowell09", UpdatedAt: now.Add(-2 * time.Hour), CI: gh.CISuccess},
			{Repository: "acme/web-ui", Number: 42, Title: "🎉 Ship the onboarding revamp", URL: "https://github.com/acme/web-ui/pull/42", Author: "ada", UpdatedAt: now.Add(-time.Hour), CI: gh.CIPending},
			{Repository: "acme/api-gateway", Number: 7, Title: "日本語のタイトルで広いグリフ", URL: "https://github.com/acme/api-gateway/pull/7", Author: "grace", UpdatedAt: now.Add(-30 * time.Minute), CI: gh.CIFailure, Draft: true},
		}},
		{View: cfg.Views[1]},
	}
	model.loading = false
	model.width = width
	model.height = 30
	return model
}

func TestModelLayoutsFitTerminalWidth(t *testing.T) {
	for _, width := range layoutWidths {
		model := layoutModel(t, width)
		output := model.View()
		for _, line := range strings.Split(output, "\n") {
			plain := stripANSI(line)
			got := lipgloss.Width(plain)
			if got > width {
				t.Fatalf("width %d: rendered line is %d cells wide:\n%q", width, got, line)
			}
		}
	}
}

func TestModelLayoutsDropColumnsIntentionally(t *testing.T) {
	cases := []struct {
		width   int
		present []string
		absent  []string
	}{
		{width: 40, present: []string{"TITLE", "PR", "CI", "REVIEW"}, absent: []string{"REPOSITORY", "AUTHOR", "UPDATED"}},
		// The title column keeps two cells at 60, so its header truncates to "T…".
		{width: 60, present: []string{"REPOSITORY", "REVIEW", "POSTED"}, absent: []string{"AUTHOR", "UPDATED"}},
		{width: 80, present: []string{"REPOSITORY", "TITLE", "REVIEW", "POSTED", "UPDATED"}, absent: []string{"AUTHOR"}},
		{width: 120, present: []string{"REPOSITORY", "TITLE", "REVIEW", "POSTED", "AUTHOR", "UPDATED"}},
	}
	for _, tc := range cases {
		model := layoutModel(t, tc.width)
		header := ""
		for _, line := range strings.Split(model.View(), "\n") {
			plain := stripANSI(line)
			if strings.Contains(plain, "REPOSITORY") || strings.Contains(plain, "TITLE") {
				header = plain
				break
			}
		}
		if header == "" {
			t.Fatalf("width %d: no header rendered:\n%s", tc.width, model.View())
		}
		for _, want := range tc.present {
			if !strings.Contains(header, want) {
				t.Fatalf("width %d: header missing %q: %q", tc.width, want, header)
			}
		}
		for _, absent := range tc.absent {
			if strings.Contains(header, absent) {
				t.Fatalf("width %d: header unexpectedly keeps %q: %q", tc.width, absent, header)
			}
		}
	}
}

func TestModelTabGeometryMatchesRenderedOutput(t *testing.T) {
	for _, width := range layoutWidths {
		model := layoutModel(t, width)
		tabLine := stripANSI(strings.Split(model.View(), "\n")[tabRowY])
		for i, bar := range model.tabBars() {
			style := inactiveTab
			if i == model.active {
				style = activeTab
			}
			rendered := stripANSI(style.Render(bar.label))
			// The dim dot separator is multibyte, so measure cells, not bytes.
			x := strings.Index(tabLine, rendered)
			if x >= 0 {
				x = lipgloss.Width(tabLine[:x])
			}
			if x != bar.x || lipgloss.Width(rendered) != bar.width {
				t.Fatalf("width %d: tab %d renders at x %d width %d, hitbox x %d width %d", width, i, x, lipgloss.Width(rendered), bar.x, bar.width)
			}
		}
	}
}

func TestModelURLRemainsVisibleInEveryLayout(t *testing.T) {
	for _, width := range layoutWidths {
		model := layoutModel(t, width)
		output := model.View()
		if !strings.Contains(output, "https://github.com/") {
			t.Fatalf("width %d: URL not visible:\n%s", width, output)
		}
		urlY := -1
		for y, line := range strings.Split(output, "\n") {
			if strings.Contains(line, "https://github.com/") {
				urlY = y
				break
			}
		}
		if urlY != model.boardLayout().selectedURLRow {
			t.Fatalf("width %d: rendered URL Y = %d, mouse Y = %d", width, urlY, model.boardLayout().selectedURLRow)
		}
		if urlY >= model.height {
			t.Fatalf("width %d: rendered URL Y = %d exceeds height %d", width, urlY, model.height)
		}
	}
}

func TestModelMouseCoordinatesMatchRenderedOutputInEachLayout(t *testing.T) {
	for _, width := range []int{50, 80, 90, 120, 140} {
		model := layoutModel(t, width)
		lines := strings.Split(model.View(), "\n")

		tabLine := stripANSI(lines[tabRowY])
		for i, view := range model.views {
			label := model.tabLabel(i, view)
			idx := strings.Index(tabLine, label)
			if idx < 0 {
				t.Fatalf("width %d: tab %d label %q not rendered: %q", width, i, label, tabLine)
			}
			col := lipgloss.Width(tabLine[:idx])
			if got, ok := model.tabAtX(col); !ok || got != i {
				t.Fatalf("width %d: tabAtX(%d) = %d, %v, want %d", width, col, got, ok, i)
			}
			if i > 0 {
				if got, ok := model.tabAtX(col - 2); ok && got == i {
					t.Fatalf("width %d: tabAtX(%d) still selects tab %d", width, col-2, i)
				}
			}
		}

		firstRowY, urlY := -1, -1
		for y, line := range lines {
			plain := stripANSI(line)
			switch {
			case strings.Contains(plain, "#7"):
				firstRowY = y
			case strings.Contains(plain, "https://github.com/"):
				urlY = y
			}
		}
		if firstRowY != model.boardLayout().firstPRRow {
			t.Fatalf("width %d: rendered first row Y = %d, mouse Y = %d", width, firstRowY, model.boardLayout().firstPRRow)
		}
		if urlY != model.boardLayout().selectedURLRow {
			t.Fatalf("width %d: rendered URL Y = %d, mouse Y = %d", width, urlY, model.boardLayout().selectedURLRow)
		}

		updated, command := model.Update(tea.MouseMsg(tea.MouseEvent{
			X: 2, Y: firstRowY, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
		}))
		// The first row is already selected, so the click opens it.
		if command == nil {
			t.Fatalf("width %d: click on the selected row did not return a browser command", width)
		}
		model = updated.(Model)
		if model.cursor != 0 {
			t.Fatalf("width %d: row click selected cursor %d, want 0", width, model.cursor)
		}

		updated, command = model.Update(tea.MouseMsg(tea.MouseEvent{
			X: 2, Y: urlY, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
		}))
		if command == nil {
			t.Fatalf("width %d: URL click did not return a browser command", width)
		}
		model = updated.(Model)

		// A click on a lower row, inside the frame, selects the row that
		// renders there.
		secondY := -1
		for y, line := range lines {
			if strings.Contains(stripANSI(line), "#42 ") {
				secondY = y
			}
		}
		x, _ := displayColumn(stripANSI(lines[secondY]), "#42")
		updated, _ = model.Update(tea.MouseMsg(tea.MouseEvent{X: x, Y: secondY, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}))
		if got := updated.(Model).cursor; got != 1 {
			t.Fatalf("width %d: click on rendered #42 at y %d selected %d, want 1", width, secondY, got)
		}

		// The footer keys stay outside the frame. A click on the rendered
		// help key opens the help.
		for y := len(lines) - 1; y >= 0; y-- {
			if x, ok := displayColumn(stripANSI(lines[y]), "? help"); ok {
				updated, _ = model.Update(tea.MouseMsg(tea.MouseEvent{X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}))
				if !updated.(Model).helpOverlay {
					t.Fatalf("width %d: click on the rendered help key at %d,%d did nothing", width, x, y)
				}
				break
			}
		}
	}
}

// TestFrameTitleNamesTheViewAndItsCount covers the rounded table frame. Its
// top edge names the active view, not the board title, and an active filter
// shows its matches.
func TestFrameTitleNamesTheViewAndItsCount(t *testing.T) {
	for _, width := range []int{50, 90, 140} {
		model := layoutModel(t, width)
		lines := strings.Split(stripANSI(model.View()), "\n")
		top := lines[tabRowY+1]
		if !strings.HasPrefix(top, "╭─ Opened by me (3) ─") || !strings.HasSuffix(top, "─╮") || lipgloss.Width(top) != width {
			t.Fatalf("width %d: frame top = %q", width, top)
		}
		if strings.Contains(top, model.cfg.UI.Title) {
			t.Fatalf("width %d: frame top repeats the board title: %q", width, top)
		}
		for _, line := range lines[tabRowY+2 : model.boardLayout().selectedURLRow-1] {
			if !strings.HasPrefix(line, "│") && !strings.HasPrefix(line, "├") || lipgloss.Width(line) != width {
				t.Fatalf("width %d: table line outside the frame: %q", width, line)
			}
		}
		filtered := press(model, runes("/"), runes("acme"))
		if top := strings.Split(stripANSI(filtered.View()), "\n")[tabRowY+1]; !strings.HasPrefix(top, "╭─ Opened by me (2 of 3) ─") {
			t.Fatalf("width %d: filtered frame top = %q", width, top)
		}
	}
}

// TestFrameKeepsShortMessagesInside covers messages that replace the table.
// Each one fits inside the frame, so the right side stays on screen.
func TestFrameKeepsShortMessagesInside(t *testing.T) {
	for _, width := range []int{22, 34} {
		for name, setup := range map[string]func(*Model){
			"loading":  func(m *Model) { m.loading = true },
			"no match": func(m *Model) { m.filter = "nothing" },
			"error":    func(m *Model) { m.views[0].Err = errors.New("timeout while searching") },
		} {
			model := emptyViewModel(t, width, defaultView(t, config.ViewAuthored))
			setup(&model)
			for _, line := range strings.Split(stripANSI(model.renderTable(model.boardLayout())), "\n") {
				if line == "" {
					continue
				}
				if lipgloss.Width(line) != width || !strings.HasSuffix(line, "│") && !strings.HasSuffix(line, "╮") && !strings.HasSuffix(line, "╯") {
					t.Fatalf("%s width %d: line outside the frame: %q", name, width, line)
				}
			}
		}
	}
}

// TestSharedRepositoryHeaderIsBoldAccentWithCount covers the group header
// that names the shared repository.
func TestSharedRepositoryHeaderIsBoldAccentWithCount(t *testing.T) {
	colorProfile(t)
	model := layoutModel(t, 120)
	for i := range model.views[0].PRs {
		model.views[0].PRs[i].Repository = "acme/web-ui"
	}
	line := strings.Split(model.View(), "\n")[model.boardLayout().firstPRRow-1]
	if !strings.Contains(line, "\x1b[1;38;5;105m") || !strings.Contains(stripANSI(line), "acme/web-ui (3)") {
		t.Fatalf("group header = %q", line)
	}
}

func TestTruncateUsesTerminalCellWidth(t *testing.T) {
	cases := []struct {
		value string
		width int
		want  string
	}{
		{value: "abc", width: 3, want: "abc"},
		{value: "abcd", width: 3, want: "ab…"},
		{value: "🎉 party time", width: 6, want: "🎉 pa…"},
		{value: "日本語のタイトル", width: 4, want: "日…"},
		{value: "e\u0301x", width: 2, want: "e\u0301x"},
		{value: "e\u0301xtra", width: 2, want: "e\u0301…"},
		{value: "abc", width: 1, want: "…"},
		{value: "abc", width: 0, want: "…"},
	}
	for _, tc := range cases {
		got := truncate(tc.value, tc.width)
		if got != tc.want {
			t.Fatalf("truncate(%q, %d) = %q, want %q", tc.value, tc.width, got, tc.want)
		}
		if lipgloss.Width(got) > max(1, tc.width) {
			t.Fatalf("truncate(%q, %d) = %q is %d cells wide", tc.value, tc.width, got, lipgloss.Width(got))
		}
	}
}

var implementedKeys = documentedKeys

func TestReferenceDocumentsEveryImplementedKey(t *testing.T) {
	var reference strings.Builder
	for _, path := range []string{"../../docs/board.md", "../../docs/reviews.md"} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		reference.Write(data)
	}
	for _, key := range implementedKeys {
		literal := "`" + key + "`"
		if !strings.Contains(reference.String(), literal) {
			t.Fatalf("board and review guides do not document the %q control (missing %q)", key, literal)
		}
	}
}

// helpOverlayKeys returns every key literal the ? overlay names.
func helpOverlayKeys() map[string]bool {
	keys := map[string]bool{}
	separator := func(r rune) bool { return r == '/' || r == '–' }
	for _, section := range helpSections {
		for _, entry := range section.entries {
			for _, field := range strings.Fields(entry.keys) {
				keys[field] = true
				for _, part := range strings.FieldsFunc(field, separator) {
					keys[part] = true
				}
			}
		}
	}
	return keys
}

func TestHelpOverlayNamesEveryImplementedControl(t *testing.T) {
	keys := helpOverlayKeys()
	for _, key := range implementedKeys {
		if !keys[key] {
			t.Fatalf("the ? overlay does not name the %q control", key)
		}
	}
}

func TestFooterShowsTheTopControls(t *testing.T) {
	model := layoutModel(t, 200)
	footer := stripANSI(model.renderFooter(model.footerHelpLines()))
	want := "Tab view · ↑↓ select · Enter open · / filter · j/k scroll · s settings · v zoom · ? help"
	if !strings.Contains(footer, want) {
		t.Fatalf("footer missing %q:\n%s", want, footer)
	}
	for _, absent := range []string{"first/last", "edit config", "wheel/click"} {
		if strings.Contains(footer, absent) {
			t.Fatalf("footer still lists %q:\n%s", absent, footer)
		}
	}
}

func TestHelpOverlayOpensAndClosesFromBoardAndReviewPanel(t *testing.T) {
	question := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")}
	board := layoutModel(t, 80)
	next, _ := board.Update(question)
	opened := next.(Model)
	if !opened.helpOverlay {
		t.Fatal("? did not open the help overlay on the board")
	}
	overlay := stripANSI(opened.View())
	for _, want := range []string{"Board", "Review region", "Esc"} {
		if !strings.Contains(overlay, want) {
			t.Fatalf("overlay missing %q:\n%s", want, overlay)
		}
	}
	for _, key := range []tea.KeyMsg{question, {Type: tea.KeyEsc}} {
		closed, _ := opened.Update(key)
		if closed.(Model).helpOverlay {
			t.Fatalf("%q did not close the help overlay", key.String())
		}
	}

	panel := panelModel(t)
	next, _ = panel.Update(question)
	opened = next.(Model)
	if !opened.helpOverlay {
		t.Fatal("? did not open the help overlay in the review panel")
	}
	back, _ := opened.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if closed := back.(Model); closed.helpOverlay || !closed.zoom {
		t.Fatal("Esc did not return from the help overlay to the zoomed region")
	}
}

func TestHelpOverlayKeepsTheFilterLineBehavior(t *testing.T) {
	model := layoutModel(t, 80)
	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	next, _ = next.(Model).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	filtering := next.(Model)
	if filtering.helpOverlay {
		t.Fatal("? opened the help overlay during filter input")
	}
	if filtering.filter != "?" {
		t.Fatalf("filter = %q, want %q", filtering.filter, "?")
	}
	if view := stripANSI(filtering.View()); !strings.Contains(view, "/ ?") {
		t.Fatalf("filter line missing:\n%s", view)
	}
}

func TestHelpOverlayFitsNarrowTerminalsAndKeepsEveryControl(t *testing.T) {
	// Wrapping keeps every character, but it can consume a space at a break.
	// Comparing without spaces makes the check exact at every width.
	compact := func(value string) string { return strings.ReplaceAll(value, " ", "") }
	for _, size := range [][2]int{{tierNarrow, 24}, {30, 10}, {12, 6}, {80, 24}, {120, 40}} {
		model := layoutModel(t, size[0])
		model.height = size[1]
		model.helpOverlay = true
		lines := model.helpOverlayLines()
		model.helpOffset = len(lines)
		model.clampHelpOffset()
		rendered := strings.Split(model.View(), "\n")
		if len(rendered) > size[1] {
			t.Fatalf("%v: overlay rendered %d lines", size, len(rendered))
		}
		for _, line := range rendered {
			if got := lipgloss.Width(stripANSI(line)); got > size[0] {
				t.Fatalf("%v: overlay line is %d cells wide: %q", size, got, line)
			}
		}
		var plain strings.Builder
		for _, line := range lines {
			plain.WriteString(stripANSI(line))
		}
		content := compact(plain.String())
		for _, section := range helpSections {
			for _, entry := range section.entries {
				for _, want := range []string{section.title, entry.keys, entry.action} {
					if !strings.Contains(content, compact(want)) {
						t.Fatalf("%v: overlay lost %q", size, want)
					}
				}
			}
		}
		if last := stripANSI(lines[len(lines)-1]); !strings.Contains(stripANSI(model.View()), last) {
			t.Fatalf("%v: scrolling does not reach %q:\n%s", size, last, stripANSI(model.View()))
		}
	}
}

func TestReviewPanelHelpLineOffersTheHelpOverlay(t *testing.T) {
	model := panelModel(t)
	help, _ := model.zoomViewport()
	if line := stripANSI(strings.Join(help, " ")); !strings.Contains(line, "? help") {
		t.Fatalf("review panel help line missing %q: %q", "? help", line)
	}
}

func TestFooterMetaLineStartsWithoutASeparator(t *testing.T) {
	model := layoutModel(t, 200)
	model.views[model.active].UpdatedAt = time.Now()
	metaLine := func(m Model) string {
		lines := strings.Split(stripANSI(m.renderFooter(m.footerHelpLines())), "\n")
		return lines[len(lines)-1]
	}
	if got, want := metaLine(model), "updated now"; got != want {
		t.Fatalf("meta line = %q, want %q", got, want)
	}
	model.monitorError = "monitor stopped"
	if got, want := metaLine(model), "monitor stopped · updated now"; got != want {
		t.Fatalf("meta line with a monitor error = %q, want %q", got, want)
	}
}

func TestFooterWrapsWithinWidthAndHeight(t *testing.T) {
	for _, width := range []int{40, 60, 80, 120, 200} {
		model := layoutModel(t, width)
		for _, line := range model.footerHelpLines() {
			if got := lipgloss.Width(stripANSI(line)); got > width {
				t.Fatalf("width %d: footer line is %d cells wide:\n%q", width, got, line)
			}
		}
		if lines := len(strings.Split(model.View(), "\n")); lines > model.height {
			t.Fatalf("width %d: rendered %d lines in a %d-line terminal", width, lines, model.height)
		}
	}
}

func TestModelNarrowLayoutsFitStaleAndErrorLines(t *testing.T) {
	for _, width := range []int{40, 50, 60} {
		cfg := testConfig()
		model, err := NewModel(cfg, fakeLoader{}, nil)
		if err != nil {
			t.Fatal(err)
		}
		model.views = []discovery.ViewData{
			{View: cfg.Views[0], PRs: []gh.PullRequest{{Repository: "acme/api", Number: 1, Title: "Keep me"}}, UpdatedAt: time.Now(), Err: errors.New("GitHub search failed: timeout")},
			{View: cfg.Views[1], Err: errors.New("GitHub search failed: timeout")},
		}
		model.loading = false
		model.width, model.height = width, 30

		for active, want := range map[int]string{0: "stale", 1: "GitHub query failed"} {
			model.active = active
			output := model.View()
			if !strings.Contains(output, want) {
				t.Fatalf("width %d active %d: %q missing:\n%s", width, active, want, output)
			}
			for _, line := range strings.Split(output, "\n") {
				if lipgloss.Width(stripANSI(line)) > width {
					t.Fatalf("width %d active %d: rendered line is too wide:\n%q", width, active, line)
				}
			}
		}
	}
}

// emptyViewModel builds a board where every view has no pull requests.
func emptyViewModel(t *testing.T, width int, views ...config.View) Model {
	t.Helper()
	cfg := testConfig()
	cfg.Views = views
	model, err := NewModel(cfg, fakeLoader{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := range model.views {
		model.views[i].UpdatedAt = time.Now()
	}
	model.loading = false
	model.width, model.height = width, 30
	return model
}

// flatten removes the line breaks that wrapping adds, so one assertion covers
// every terminal width.
// flatten removes white space and the table frame sides, so wrapped text
// inside the frame compares as one string.
func flatten(value string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(value, "│", "")), "")
}

// defaultView returns a copy of the default view with this identifier.
func defaultView(t *testing.T, id string) config.View {
	t.Helper()
	view, ok := config.DefaultView(id)
	if !ok {
		t.Fatalf("no default view %q", id)
	}
	return view
}

func TestEmptyViewsExplainTheViewAndNameTheNextKeys(t *testing.T) {
	custom := config.View{ID: "team", Title: "Team", Query: "is:open label:team", Scope: config.ScopeGlobal}
	editedQuery := defaultView(t, config.ViewAuthored)
	editedQuery.Query = "is:open author:@me label:bug"
	editedScope := defaultView(t, config.ViewAll)
	editedScope.Scope = config.ScopeGlobal
	cases := []struct {
		view config.View
		want string
	}{
		{defaultView(t, config.ViewAuthored), "You have no open pull requests."},
		{defaultView(t, config.ViewReview), "No open pull requests wait for your review."},
		{defaultView(t, config.ViewAll), "No open pull requests are in the configured scopes."},
		{custom, `No pull requests match "is:open label:team".`},
		{editedQuery, `No pull requests match "is:open author:@me label:bug".`},
		{editedScope, `No pull requests match "is:open".`},
	}
	for _, tc := range cases {
		for _, width := range []int{30, 40, 60, 80, 120} {
			model := emptyViewModel(t, width, tc.view, custom)
			rendered := stripANSI(model.View())
			if !strings.Contains(flatten(rendered), flatten(tc.want)) {
				t.Fatalf("view %q width %d: missing %q:\n%s", tc.view.ID, width, tc.want, rendered)
			}
			if strings.Contains(rendered, "No pull requests in this view.") {
				t.Fatalf("view %q width %d: kept the untailored text:\n%s", tc.view.ID, width, rendered)
			}
			for _, pair := range []string{"Tab next view", "E edit config", "Ctrl+R refresh"} {
				if !strings.Contains(rendered, pair) {
					t.Fatalf("view %q width %d: missing %q:\n%s", tc.view.ID, width, pair, rendered)
				}
			}
			for _, line := range strings.Split(rendered, "\n") {
				if got := lipgloss.Width(line); got > width {
					t.Fatalf("view %q width %d: line is %d cells wide:\n%q", tc.view.ID, width, got, line)
				}
			}
			// The footer stays at the bottom, and no line repeats that no PR is listed.
			if lines := len(strings.Split(rendered, "\n")); lines != model.height {
				t.Fatalf("view %q width %d: rendered %d lines in a %d-line terminal", tc.view.ID, width, lines, model.height)
			}
			if strings.Contains(rendered, "No PR selected") {
				t.Fatalf("view %q width %d: kept the redundant selection line:\n%s", tc.view.ID, width, rendered)
			}
		}
	}
}

func TestEmptyViewNamesTabOnlyWhenAnotherViewExists(t *testing.T) {
	single := stripANSI(emptyViewModel(t, 80, defaultView(t, config.ViewAll)).View())
	if strings.Contains(single, "Tab next view") {
		t.Fatalf("one view offers a next view:\n%s", single)
	}
	for _, pair := range []string{"E edit config", "Ctrl+R refresh"} {
		if !strings.Contains(single, pair) {
			t.Fatalf("one view drops %q:\n%s", pair, single)
		}
	}
}

func TestEmptyViewKeepsLoadingFilterAndErrorText(t *testing.T) {
	model := emptyViewModel(t, 80, defaultView(t, config.ViewAuthored))

	model.loading = true
	if got := stripANSI(model.renderTable(model.boardLayout())); !strings.Contains(got, "Loading pull requests…") {
		t.Fatalf("loading text changed: %q", got)
	}

	model.loading = false
	model.filter = "nothing"
	if got := stripANSI(model.renderTable(model.boardLayout())); !strings.Contains(got, "No pull requests match the filter.") {
		t.Fatalf("filter text changed: %q", got)
	}

	model.filter = ""
	model.views[0].Err = errors.New("timeout")
	if got := stripANSI(model.renderTable(model.boardLayout())); !strings.Contains(got, "GitHub query failed: timeout") {
		t.Fatalf("error text changed: %q", got)
	}
}

// TestRowlessViewsKeepTheFooterAtTheBottom covers each state with no table
// rows. Each one fills the terminal, so the footer controls stay on the last
// rows and do not float under a short message.
func TestRowlessViewsKeepTheFooterAtTheBottom(t *testing.T) {
	for name, setup := range map[string]func(*Model){
		"empty":    func(*Model) {},
		"loading":  func(m *Model) { m.loading = true },
		"no match": func(m *Model) { m.filter = "nothing" },
		"error":    func(m *Model) { m.views[0].Err = errors.New("timeout") },
	} {
		model := emptyViewModel(t, 80, defaultView(t, config.ViewAuthored))
		model.height = 30
		setup(&model)
		lines := strings.Split(stripANSI(model.View()), "\n")
		if len(lines) != model.height {
			t.Fatalf("%s: rendered %d lines in a %d-line terminal", name, len(lines), model.height)
		}
		if !strings.Contains(lines[len(lines)-2], "? help") {
			t.Fatalf("%s: the controls are not above the meta line:\n%s", name, strings.Join(lines, "\n"))
		}
	}
}

// TestEmptyViewCentersItsText covers the empty view layout: the message
// sits in the middle of the table area, not under the tabs.
func TestEmptyViewCentersItsText(t *testing.T) {
	model := emptyViewModel(t, 80, defaultView(t, config.ViewAuthored))
	lines := strings.Split(stripANSI(model.View()), "\n")
	want := "You have no open pull requests."
	for y, line := range lines {
		if x := strings.Index(line, want); x >= 0 {
			// The frame side takes one column, and the text centers in the
			// 78 columns inside the frame.
			if x, left := lipgloss.Width(line[:x]), 1+(78-len(want))/2; x != left {
				t.Fatalf("message starts at x %d, want %d", x, left)
			}
			if y <= model.boardLayout().firstPRRow {
				t.Fatalf("message at row %d is not centered vertically:\n%s", y, strings.Join(lines, "\n"))
			}
			return
		}
	}
	t.Fatalf("message missing:\n%s", strings.Join(lines, "\n"))
}

// TestTabLabelsNameTheViewAndItsCount covers the tab label format. The
// number keys select views, so the label does not repeat the index.
func TestTabLabelsNameTheViewAndItsCount(t *testing.T) {
	model := layoutModel(t, 120)
	tabs := stripANSI(strings.Split(model.View(), "\n")[tabRowY])
	// The active tab fill ends one cell after its label. One space then
	// separates the fill from the dot on each side.
	if !strings.Contains(tabs, " Opened by me (3)  · Review requested (0)") {
		t.Fatalf("tab row = %q", tabs)
	}
	if next := press(model, runes("2")); next.active != 1 {
		t.Fatalf("2 selected view %d, want 1", next.active)
	}
}

// TestFilterShowsItsCountWithoutMovingRows covers the inline filter. The
// filter and its match count open the meta line, so starting a filter keeps
// every table, URL, and footer row where it was.
func TestFilterShowsItsCountWithoutMovingRows(t *testing.T) {
	model := layoutModel(t, 120)
	before := strings.Split(stripANSI(model.View()), "\n")
	filtering := press(model, runes("/"), runes("acme"))
	after := strings.Split(stripANSI(filtering.View()), "\n")
	if len(after) != len(before) {
		t.Fatalf("filter changed the line count from %d to %d", len(before), len(after))
	}
	if got := after[len(after)-1]; !strings.HasPrefix(got, "/ acme▌ · 2 of 3") {
		t.Fatalf("meta line = %q", got)
	}
	if model.boardLayout().firstPRRow != filtering.boardLayout().firstPRRow || len(model.footerHelpLines()) != len(filtering.footerHelpLines()) {
		t.Fatal("filter moved the table or the footer controls")
	}
}

// TestMetaLineWarnsOnlyWhenSomethingNeedsAttention covers the meta line
// colors. A healthy refresh age and rate limits are dim. A low rate, a stale
// view, or a warning uses the warning color.
func TestMetaLineWarnsOnlyWhenSomethingNeedsAttention(t *testing.T) {
	previous := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(previous) })
	meta := func(m Model) string {
		lines := strings.Split(m.renderFooter(m.footerHelpLines()), "\n")
		return lines[len(lines)-1]
	}
	model := layoutModel(t, 120)
	model.rates.Search = gh.RateResource{Limit: 30, Remaining: 27}
	model.rates.GraphQL = gh.RateResource{Limit: 5000, Remaining: 4823}
	healthy := meta(model)
	for _, part := range []string{"Search 27/30", "GraphQL 4823/5000"} {
		if !strings.Contains(healthy, dimStyle.Render(part)) {
			t.Fatalf("healthy %q is not dim: %q", part, healthy)
		}
	}

	low := model
	low.rates.Search.Remaining = 0
	low.rates.Search.Reset = time.Now().Add(time.Minute)
	if got := meta(low); !strings.Contains(got, warningStyle.Render("Search 0/30")) || !strings.Contains(got, dimStyle.Render("GraphQL 4823/5000")) {
		t.Fatalf("a low Search rate is not the only warning: %q", got)
	}
	graphQL := model
	graphQL.rates.GraphQL = gh.RateResource{Limit: 5000, Remaining: 10, Cost: 50, Reset: time.Now().Add(time.Minute)}
	if got := meta(graphQL); !strings.Contains(got, warningStyle.Render("GraphQL 10/5000")) {
		t.Fatalf("a low GraphQL rate is not a warning: %q", got)
	}

	warned := model
	warned.warning = "refresh failed"
	if got := meta(warned); !strings.Contains(got, warningStyle.Render("refresh failed")) || !strings.Contains(got, dimStyle.Render("Search 27/30")) {
		t.Fatalf("a warning did not keep the healthy rates dim: %q", got)
	}
}

// TestEmptyViewFitsShortTerminalsAndKeepsTheKeys covers a long custom query in
// a small pane. The empty state must never push the tabs off the screen,
// because the mouse rows assume that the tabs stay at their rendered row.
func TestEmptyViewFitsShortTerminalsAndKeepsTheKeys(t *testing.T) {
	long := config.View{ID: "team", Title: "Team", Scope: config.ScopeGlobal,
		Query: "is:open " + strings.Repeat("label:needs-a-very-long-triage-label ", 8)}
	for _, width := range []int{30, 40, 60, 80} {
		for height := 10; height <= 30; height++ {
			model := emptyViewModel(t, width, long, defaultView(t, config.ViewAll))
			model.height = height
			rendered := stripANSI(model.View())
			lines := strings.Split(rendered, "\n")
			if len(lines) != height {
				t.Fatalf("width %d height %d: rendered %d lines:\n%s", width, height, len(lines), rendered)
			}
			for _, line := range lines {
				if got := lipgloss.Width(line); got > width {
					t.Fatalf("width %d height %d: line is %d cells wide:\n%q", width, height, got, line)
				}
			}
			label := model.tabLabel(0, model.views[0])
			tabs := stripANSI(lines[tabRowY])
			start := strings.Index(tabs, label)
			if start < 0 {
				t.Fatalf("width %d height %d: tab row %q lost label %q", width, height, tabs, label)
			}
			if index, ok := model.tabAtX(lipgloss.Width(tabs[:start])); !ok || index != 0 {
				t.Fatalf("width %d height %d: tabAtX = %d, %v, want 0", width, height, index, ok)
			}
			// A short pane drops the actions, then the keys. The frame takes
			// two columns, so the narrowest pane needs one more row.
			if height < 12 || width < 40 && height < 14 {
				continue
			}
			for _, pair := range []string{"Tab next view", "E edit config", "Ctrl+R refresh"} {
				if !strings.Contains(rendered, pair) {
					t.Fatalf("width %d height %d: missing %q:\n%s", width, height, pair, rendered)
				}
			}
		}
	}
}

// TestEmptyViewKeepsTheKeysWithoutTheActions covers the pane that holds the
// keys but not the actions next to them.
func TestEmptyViewKeepsTheKeysWithoutTheActions(t *testing.T) {
	model := emptyViewModel(t, 30, defaultView(t, config.ViewAll), defaultView(t, config.ViewAuthored))
	model.height = 9
	rendered := stripANSI(model.View())
	if !strings.Contains(rendered, "Tab · E · Ctrl+R") {
		t.Fatalf("a short pane dropped the keys:\n%s", rendered)
	}
	if lines := strings.Split(rendered, "\n"); len(lines) > model.height {
		t.Fatalf("rendered %d lines in a %d-line terminal:\n%s", len(lines), model.height, rendered)
	}
}

// TestShortPaneKeepsRowsAndURLOnScreen covers a pane where the wrapped footer
// competes with the collapsed summary. Overflow would push the tabs, rows, and
// URL off their mouse rows.
func TestShortPaneKeepsRowsAndURLOnScreen(t *testing.T) {
	// The filter row and the stale notice each need one more row than the
	// tightest panes hold.
	for _, size := range [][3]int{{80, 11, 0}, {60, 12, 0}, {40, 14, 1}, {80, 12, 1}} {
		for _, extra := range []string{"", "filter", "stale"} {
			if extra != "" && size[2] == 0 {
				continue
			}
			model := layoutModel(t, size[0])
			model.height = size[1]
			model.views[0].PRs = model.views[0].PRs[:1]
			switch extra {
			case "filter":
				next, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
				model = next.(Model)
			case "stale":
				model.views[0].UpdatedAt = time.Now()
				model.views[0].Err = errors.New("search failed")
			}
			lines := strings.Split(model.View(), "\n")
			if len(lines) > size[1] {
				t.Fatalf("%v %s: rendered %d lines:\n%s", size, extra, len(lines), stripANSI(model.View()))
			}
			lay := model.boardLayout()
			if !strings.Contains(stripANSI(lines[lay.selectedURLRow]), "https://github.com/") {
				t.Fatalf("%v %s: URL row %d = %q", size, extra, lay.selectedURLRow, stripANSI(lines[lay.selectedURLRow]))
			}
		}
	}
}

func TestSharedRepositoryMovesOutOfTheTable(t *testing.T) {
	for _, width := range []int{50, 80, 120} {
		model := layoutModel(t, width)
		for i := range model.views[0].PRs {
			model.views[0].PRs[i].Repository = "acme/web-ui"
		}
		lines := strings.Split(stripANSI(model.View()), "\n")
		lay := model.boardLayout()
		if header := lines[lay.firstPRRow-3]; strings.Contains(header, "REPOSITORY") || !strings.Contains(header, "TITLE") && width >= 80 {
			t.Fatalf("width %d: header = %q", width, header)
		}
		if got := strings.TrimRight(insideFrame(lines[lay.firstPRRow-1]), " "); got != "   acme/web-ui (3)" {
			t.Fatalf("width %d: shared repository line = %q", width, got)
		}
		for i, number := range []string{"#7", "#42", "#1234"} {
			if row := lines[lay.firstPRRow+i]; !strings.Contains(row, number+" ") || strings.Contains(row, "acme/web-ui") {
				t.Fatalf("width %d: row %d = %q", width, i, row)
			}
		}
		if !strings.Contains(lines[lay.selectedURLRow], "https://github.com/") {
			t.Fatalf("width %d: URL row %d = %q", width, lay.selectedURLRow, lines[lay.selectedURLRow])
		}
		next, _ := model.Update(tea.MouseMsg{X: 4, Y: lay.firstPRRow + 2, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
		if got := next.(Model).cursor; got != 2 {
			t.Fatalf("width %d: click on the third row selected %d", width, got)
		}
	}
}

func TestFilterToOneRepositoryHidesTheRepositoryColumn(t *testing.T) {
	model := press(layoutModel(t, 120), runes("/"), runes("acme/web-ui"))
	lines := strings.Split(stripANSI(model.View()), "\n")
	lay := model.boardLayout()
	if got := strings.TrimSpace(insideFrame(lines[lay.firstPRRow-1])); got != "acme/web-ui (1)" || !strings.Contains(lines[lay.firstPRRow], "#42 ") {
		t.Fatalf("filtered view:\n%s", strings.Join(lines, "\n"))
	}
	model = press(model, tea.KeyMsg{Type: tea.KeyEsc})
	if lines := strings.Split(stripANSI(model.View()), "\n"); !strings.Contains(lines[model.boardLayout().firstPRRow-2], "REPOSITORY") {
		t.Fatalf("mixed view hides the repository column:\n%s", strings.Join(lines, "\n"))
	}
}

// TestConfirmationStaysVisibleWithALongFilter covers a narrow pane with a
// long filter. The armed confirmation comes first on the meta line, so Enter
// never publishes behind a hidden prompt.
func TestConfirmationStaysVisibleWithALongFilter(t *testing.T) {
	model := layoutModel(t, 40)
	model.filter = "responsive layouts and cell-width truncation"
	pr, _ := model.selectedPR()
	model.confirm = &pendingPublication{pr, config.PublishApprove}
	lines := strings.Split(stripANSI(model.renderFooter(model.footerHelpLines())), "\n")
	prompt := stripANSI(model.confirm.prompt())
	if got := lines[len(lines)-1]; !strings.HasPrefix(got, prompt[:min(len(prompt), 30)]) {
		t.Fatalf("meta line %q does not open with the confirmation %q", got, prompt)
	}
}

// TestLongFilterKeepsNoticesAndWarningsVisible covers a long filter next to
// the rate limits. The filter takes at most half the meta line, and every
// alert comes before the routine status, so action feedback stays visible.
func TestLongFilterKeepsNoticesAndWarningsVisible(t *testing.T) {
	for _, width := range []int{40, 80} {
		model := layoutModel(t, width)
		model.filter = "responsive layouts and cell-width truncation"
		model.rates.Search = gh.RateResource{Limit: 30, Remaining: 27}
		model.rates.GraphQL = gh.RateResource{Limit: 5000, Remaining: 4823}
		model.views[0].UpdatedAt = time.Now()
		for _, alert := range []func(*Model){
			func(m *Model) { m.notice = "browser failed" },
			func(m *Model) { m.warning = "browser failed" },
		} {
			alerted := model
			alert(&alerted)
			lines := strings.Split(stripANSI(alerted.renderFooter(alerted.footerHelpLines())), "\n")
			if got := lines[len(lines)-1]; !strings.Contains(got, "browser failed") || !strings.HasPrefix(got, "/ responsi") {
				t.Fatalf("width %d: meta line = %q", width, got)
			}
		}
	}
}
