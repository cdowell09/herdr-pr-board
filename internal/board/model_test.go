package board

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/cdowell09/herdr-pr-board/internal/config"
	"github.com/cdowell09/herdr-pr-board/internal/discovery"
	gh "github.com/cdowell09/herdr-pr-board/internal/github"
	"github.com/cdowell09/herdr-pr-board/internal/sidebar"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripANSI(value string) string {
	return ansiPattern.ReplaceAllString(value, "")
}

func displayColumn(line, needle string) (int, bool) {
	idx := strings.Index(line, needle)
	if idx < 0 {
		return 0, false
	}
	return lipgloss.Width(line[:idx]), true
}

type fakeLoader struct {
	snapshot discovery.Snapshot
}

func (f fakeLoader) RefreshAll(context.Context) discovery.Snapshot { return f.snapshot }
func (f fakeLoader) RefreshOne(_ context.Context, view config.View) discovery.ViewSnapshot {
	refresh := discovery.ViewSnapshot{
		Data:      discovery.ViewData{View: view},
		Rates:     f.snapshot.Rates,
		Errors:    f.snapshot.Errors,
		StartedAt: f.snapshot.StartedAt,
	}
	for _, data := range f.snapshot.Views {
		if data.View.ID == view.ID {
			refresh.Data = data
			break
		}
	}
	return refresh
}

func (f fakeLoader) Reconfigured(config.Config) discovery.Loader { return f }

type sidebarFakeRunner struct {
	calls [][]string
	err   error
}

func (f *sidebarFakeRunner) Run(_ context.Context, args ...string) ([]byte, error) {
	f.calls = append(f.calls, append([]string(nil), args...))
	if f.err != nil {
		return nil, f.err
	}
	return []byte(`{"id":"cli:workspace:report-metadata","result":{}}`), nil
}

func TestModelReportsSidebarTokensToCurrentWorkspaceAfterFullRefresh(t *testing.T) {
	cfg := testConfig()
	cfg.Sidebar.Enabled = boolPtr(true)
	cfg.Sidebar.ReviewView = "review"
	cfg.Sidebar.TTL = "15m"
	runner := &sidebarFakeRunner{}
	reporter := sidebar.NewReporter(cfg.Sidebar, "w1", "")
	reporter.Runner = runner.Run
	model, err := NewModel(cfg, fakeLoader{}, reporter)
	if err != nil {
		t.Fatal(err)
	}

	snapshot := discovery.Snapshot{Views: []discovery.ViewData{
		{View: cfg.Views[0], PRs: []gh.PullRequest{
			{Repository: "acme/api", Number: 1, Title: "One", URL: "https://github.com/acme/api/pull/1", CI: gh.CISuccess},
			{Repository: "acme/api", Number: 2, Title: "Two", URL: "https://github.com/acme/api/pull/2", CI: gh.CIFailure},
		}},
		{View: cfg.Views[1], PRs: []gh.PullRequest{
			{Repository: "acme/api", Number: 1, Title: "One", URL: "https://github.com/acme/api/pull/1", CI: gh.CISuccess},
			{Repository: "acme/api", Number: 3, Title: "Three", URL: "https://github.com/acme/api/pull/3", CI: gh.CIPending},
		}},
	}}
	updated, command := model.Update(snapshotMsg{Snapshot: snapshot})
	model = updated.(Model)
	if command == nil {
		t.Fatal("expected a sidebar report command")
	}
	updated, _ = model.Update(command())
	model = updated.(Model)
	if strings.Contains(model.warning, "sidebar") {
		t.Fatalf("warning = %q", model.warning)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("runner calls = %d, want 1", len(runner.calls))
	}
	args := runner.calls[0]
	joined := strings.Join(args, " ")
	if args[0] != "workspace" || args[1] != "report-metadata" || args[2] != "w1" {
		t.Fatalf("report call = %#v", args)
	}
	if args[3] != "--source" || args[4] != sidebar.Source {
		t.Fatalf("report source = %#v", args)
	}
	if !strings.Contains(joined, "prs_open=3 open") || !strings.Contains(joined, "prs_review=2 review") || !strings.Contains(joined, "prs_ci=1 fail") {
		t.Fatalf("missing tokens: %#v", args)
	}
	if !strings.Contains(joined, "--ttl-ms 900000") {
		t.Fatalf("missing ttl: %#v", args)
	}
}

func TestModelSkipsSidebarReportWhenViewFails(t *testing.T) {
	cfg := testConfig()
	model, err := NewModel(cfg, fakeLoader{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	model.sidebar = &sidebar.Reporter{Runner: (&sidebarFakeRunner{}).Run}

	failed := discovery.Snapshot{Views: []discovery.ViewData{
		{View: cfg.Views[0], Err: errors.New("rate limited")},
		{View: cfg.Views[1], PRs: []gh.PullRequest{{Repository: "acme/api", Number: 1, Title: "One", URL: "https://github.com/acme/api/pull/1"}}},
	}}
	updated, command := model.Update(snapshotMsg{Snapshot: failed})
	if command != nil {
		t.Fatal("no sidebar report expected after a failed view")
	}
	model = updated.(Model)
	if model.sidebar == nil {
		t.Fatal("sidebar reporter lost")
	}
}

func TestModelWarnsOnceOnSidebarFailureAndResets(t *testing.T) {
	cfg := testConfig()
	cfg.Sidebar.Enabled = boolPtr(true)
	runner := &sidebarFakeRunner{err: errors.New("no session")}
	model, err := NewModel(cfg, fakeLoader{}, &sidebar.Reporter{WorkspaceID: "w1", Runner: runner.Run})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := discovery.Snapshot{Views: []discovery.ViewData{
		{View: cfg.Views[0], PRs: []gh.PullRequest{{Repository: "acme/api", Number: 1, Title: "One", URL: "https://github.com/acme/api/pull/1"}}},
		{View: cfg.Views[1]},
	}}

	// First failure warns once.
	updated, command := model.Update(snapshotMsg{Snapshot: snapshot})
	model = updated.(Model)
	if command == nil {
		t.Fatal("expected a sidebar report command")
	}
	updated, _ = model.Update(command())
	model = updated.(Model)
	if !strings.Contains(model.warning, "sidebar reporting unavailable") {
		t.Fatalf("warning = %q", model.warning)
	}

	// A second failure does not repeat the warning.
	updated, command = model.Update(snapshotMsg{Snapshot: snapshot})
	model = updated.(Model)
	updated, _ = model.Update(command())
	model = updated.(Model)
	if strings.Contains(model.warning, "sidebar reporting unavailable") {
		t.Fatalf("warning repeated: %q", model.warning)
	}

	// A success resets the latch, so the next failure warns again.
	runner.err = nil
	updated, command = model.Update(snapshotMsg{Snapshot: snapshot})
	model = updated.(Model)
	updated, _ = model.Update(command())
	model = updated.(Model)
	runner.err = errors.New("no session")
	updated, command = model.Update(snapshotMsg{Snapshot: snapshot})
	model = updated.(Model)
	updated, _ = model.Update(command())
	model = updated.(Model)
	if !strings.Contains(model.warning, "sidebar reporting unavailable") {
		t.Fatalf("warning not restored after success: %q", model.warning)
	}
}

func TestModelRendersConfigTitlesPRAndCI(t *testing.T) {
	cfg := testConfig()
	updated := time.Now().Add(-2 * time.Hour)
	snapshot := discovery.Snapshot{
		Views: []discovery.ViewData{
			{View: cfg.Views[0], PRs: []gh.PullRequest{{Repository: "cdowell09/cookies", Number: 2, Title: "Cookie schedule", URL: "https://github.com/cdowell09/cookies/pull/2", Author: "cdowell09", UpdatedAt: updated, CI: gh.CISuccess}}},
			{View: cfg.Views[1]},
		},
		Rates:     gh.RateLimits{Search: gh.RateResource{Limit: 30, Remaining: 28}},
		StartedAt: time.Now(),
	}
	model, err := NewModel(cfg, fakeLoader{snapshot: snapshot}, nil)
	if err != nil {
		t.Fatal(err)
	}
	updatedModel, _ := model.Update(snapshotMsg{Snapshot: snapshot})
	model = updatedModel.(Model)
	updatedModel, _ = model.Update(tea.WindowSizeMsg{Width: 130, Height: 30})
	model = updatedModel.(Model)

	output := model.View()
	for _, want := range []string{"Engineering PRs", "Opened by me", "Cookie schedule", "https://github.com/cdowell09/cookies/pull/2", "✓", "Search 28/30"} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q:\n%s", want, output)
		}
	}
}

func TestModelCentersCIIconsInColumn(t *testing.T) {
	cfg := testConfig()
	prs := []gh.PullRequest{
		{Repository: "acme/api", Number: 1, Title: "Success PR", URL: "https://github.com/acme/api/pull/1", Author: "cdowell09", UpdatedAt: time.Now(), CI: gh.CISuccess},
		{Repository: "acme/api", Number: 2, Title: "Pending PR", URL: "https://github.com/acme/api/pull/2", Author: "cdowell09", UpdatedAt: time.Now(), CI: gh.CIPending},
		{Repository: "acme/api", Number: 3, Title: "Failure PR", URL: "https://github.com/acme/api/pull/3", Author: "cdowell09", UpdatedAt: time.Now(), CI: gh.CIFailure},
		{Repository: "acme/api", Number: 4, Title: "Error PR", URL: "https://github.com/acme/api/pull/4", Author: "cdowell09", UpdatedAt: time.Now(), CI: gh.CIError},
		{Repository: "acme/api", Number: 5, Title: "No checks PR", URL: "https://github.com/acme/api/pull/5", Author: "cdowell09", UpdatedAt: time.Now(), CI: gh.CINone},
		{Repository: "acme/api", Number: 6, Title: "Unknown PR", URL: "https://github.com/acme/api/pull/6", Author: "cdowell09", UpdatedAt: time.Now(), CI: gh.CIUnknown},
	}
	model, err := NewModel(cfg, fakeLoader{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	model.views = []discovery.ViewData{{View: cfg.Views[0], PRs: prs}, {View: cfg.Views[1]}}
	model.loading = false
	model.width, model.height = 130, 30

	output := model.View()
	lines := strings.Split(output, "\n")

	var header string
	for _, line := range lines {
		if strings.Contains(stripANSI(line), "REPOSITORY") {
			header = line
			break
		}
	}
	if header == "" {
		t.Fatalf("no header rendered:\n%s", output)
	}
	ciCol, ok := displayColumn(header, "CI")
	if !ok {
		t.Fatalf("header missing CI:\n%s", header)
	}
	titleCol, ok := displayColumn(header, "TITLE")
	if !ok {
		t.Fatalf("header missing TITLE:\n%s", header)
	}
	wantIconCol := ciCol + 1

	rows := map[string]string{}
	for _, line := range lines {
		plain := stripANSI(line)
		for _, pr := range prs {
			if strings.Contains(plain, pr.Title) {
				rows[pr.Title] = line
			}
		}
	}

	for _, pr := range prs {
		row, ok := rows[pr.Title]
		if !ok {
			t.Fatalf("no row rendered for %q:\n%s", pr.Title, output)
		}
		iconCol, ok := displayColumn(row, ciGlyph(pr.CI))
		if !ok {
			t.Fatalf("PR #%d icon missing (row: %q)", pr.Number, row)
		}
		if iconCol != wantIconCol {
			t.Fatalf("PR #%d icon column = %d, want %d (row: %q)", pr.Number, iconCol, wantIconCol, row)
		}
		gotTitleCol, ok := displayColumn(row, pr.Title)
		if !ok {
			t.Fatalf("PR #%d title missing (row: %q)", pr.Number, row)
		}
		if gotTitleCol != titleCol {
			t.Fatalf("PR #%d title column = %d, want %d (row: %q)", pr.Number, gotTitleCol, titleCol, row)
		}
	}
}

func ciGlyph(state gh.CIState) string {
	return strings.TrimSpace(stripANSI(renderCI(state)))
}

func TestModelSwitchesViewsAndFilters(t *testing.T) {
	cfg := testConfig()
	snapshot := discovery.Snapshot{Views: []discovery.ViewData{
		{View: cfg.Views[0], PRs: []gh.PullRequest{{Repository: "acme/api", Number: 1, Title: "API fix"}}},
		{View: cfg.Views[1], PRs: []gh.PullRequest{{Repository: "acme/web", Number: 2, Title: "Web fix"}}},
	}}
	model, err := NewModel(cfg, fakeLoader{snapshot: snapshot}, nil)
	if err != nil {
		t.Fatal(err)
	}
	model.views = snapshot.Views
	model.loading = false
	model.width, model.height = 120, 30

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	model = updated.(Model)
	if model.active != 1 || !strings.Contains(model.View(), "Web fix") {
		t.Fatalf("view did not switch: active=%d", model.active)
	}
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	model = updated.(Model)
	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("missing")})
	model = updated.(Model)
	if !strings.Contains(model.View(), "No pull requests match the filter") {
		t.Fatalf("filter not applied:\n%s", model.View())
	}
}

func TestModelConfigShortcutReloadsCompleteConfig(t *testing.T) {
	cfg := testConfig()
	model, err := NewModelWithConfigPath(cfg, "/tmp/custom-pr-board.toml", fakeLoader{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	model.views = []discovery.ViewData{
		{View: cfg.Views[0], PRs: []gh.PullRequest{{Title: "Mine"}}},
		{View: cfg.Views[1], PRs: []gh.PullRequest{
			{Title: "Review one", URL: "https://github.com/acme/api/pull/1"},
			{Title: "Review selected", URL: "https://github.com/acme/api/pull/2"},
		}},
	}
	model.active = 1
	model.cursor = 1
	model.filter = "review"
	model.loading = false
	model.rates.Search = gh.RateResource{Limit: 30, Remaining: 30}

	next := cfg
	next.UI.Title = "Updated board"
	next.GitHub.RefreshInterval = "1m"
	next.Views = []config.View{
		cfg.Views[1],
		{ID: "all", Title: "All", Query: "is:open", Scope: config.ScopeGlobal},
	}
	model.loader = fakeLoader{snapshot: discovery.Snapshot{Views: []discovery.ViewData{
		{View: next.Views[0], PRs: []gh.PullRequest{
			{Title: "Review selected", URL: "https://github.com/acme/api/pull/2"},
			{Title: "Review one", URL: "https://github.com/acme/api/pull/1"},
		}},
		{View: next.Views[1]},
	}}}
	var editedPath string
	model.editConfig = func(path string) (string, tea.Cmd) {
		editedPath = path
		return "", func() tea.Msg { return configEditMsg{cfg: next} }
	}

	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'E'}})
	if command == nil {
		t.Fatal("E did not start the configuration editor")
	}
	model = updated.(Model)
	updated, command = model.Update(command())
	if command == nil {
		t.Fatal("valid configuration edit did not start a refresh")
	}
	model = updated.(Model)
	updated, _ = model.Update(command())
	model = updated.(Model)

	if editedPath != "/tmp/custom-pr-board.toml" {
		t.Fatalf("edited path = %q", editedPath)
	}
	if model.cfg.UI.Title != "Updated board" || model.refresh != time.Minute {
		t.Fatalf("configuration was not replaced: title=%q refresh=%s", model.cfg.UI.Title, model.refresh)
	}
	if model.active != 0 || model.views[0].View.ID != "review" {
		t.Fatalf("active view was not preserved by ID: active=%d views=%#v", model.active, model.views)
	}
	selected, ok := model.selectedPR()
	if model.filter != "review" || !ok || selected.URL != "https://github.com/acme/api/pull/2" || model.cursor != 0 {
		t.Fatalf("board state was not preserved: filter=%q cursor=%d selected=%#v", model.filter, model.cursor, selected)
	}
	if model.loading {
		t.Fatal("board stayed in refresh state after the candidate refresh")
	}
}

func TestModelConfigShortcutKeepsFilterInputLiteral(t *testing.T) {
	model, err := NewModel(testConfig(), fakeLoader{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	model.editing = true

	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'E'}})
	model = updated.(Model)
	if command != nil {
		t.Fatal("E in filter mode started the configuration editor")
	}
	if model.filter != "E" {
		t.Fatalf("filter = %q, want E", model.filter)
	}
}

func TestModelConfigEditRejectsFailure(t *testing.T) {
	cfg := testConfig()
	model, err := NewModel(cfg, fakeLoader{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	model.loading = false

	updated, command := model.Update(configEditMsg{err: errors.New("invalid config")})
	model = updated.(Model)
	if command != nil {
		t.Fatal("failed configuration edit returned a command")
	}
	if !model.cfg.Equal(cfg) || model.loading {
		t.Fatal("failed configuration edit changed the board")
	}
	if !strings.Contains(model.warning, "invalid config") {
		t.Fatalf("warning = %q", model.warning)
	}
}

func TestModelConfigEditSkipsUnchangedConfig(t *testing.T) {
	cfg := testConfig()
	model, err := NewModel(cfg, fakeLoader{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	model.loading = false

	updated, command := model.Update(configEditMsg{cfg: cfg})
	model = updated.(Model)
	if command != nil {
		t.Fatal("unchanged configuration started a refresh")
	}
	if model.loading {
		t.Fatal("unchanged configuration changed loading state")
	}
}

func TestModelConfigEditRespectsSearchCapacity(t *testing.T) {
	cfg := testConfig()
	capacityErr := errors.New("rate limit exhausted")
	model, err := NewModel(cfg, fakeLoader{snapshot: discovery.Snapshot{CapacityErr: capacityErr, Errors: []discovery.RetrievalError{{Stage: "search_budget", Err: capacityErr}}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	model.loading = false
	next := cfg
	next.UI.Title = "Updated"

	updated, command := model.Update(configEditMsg{cfg: next})
	if command == nil {
		t.Fatal("configuration edit did not start a candidate refresh")
	}
	model = updated.(Model)
	updated, _ = model.Update(command())
	model = updated.(Model)
	if !model.cfg.Equal(cfg) || model.loading {
		t.Fatal("capacity-rejected configuration changed the board")
	}
	if !strings.Contains(model.warning, "rate limit exhausted") {
		t.Fatalf("warning = %q", model.warning)
	}
}

func TestModelIgnoresRefreshesFromAnEarlierConfig(t *testing.T) {
	cfg := testConfig()
	model, err := NewModel(cfg, fakeLoader{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	model.epoch = 2
	model.views[0].PRs = []gh.PullRequest{{Title: "Current"}}

	updated, command := model.Update(snapshotMsg{Snapshot: discovery.Snapshot{
		Views: []discovery.ViewData{{View: cfg.Views[0], PRs: []gh.PullRequest{{Title: "Old"}}}},
	}, epoch: 1})
	model = updated.(Model)
	if command != nil {
		t.Fatal("stale refresh returned a command")
	}
	if model.views[0].PRs[0].Title != "Current" {
		t.Fatalf("stale refresh replaced current rows: %#v", model.views[0].PRs)
	}
}

func TestModelEscapeClearsFilterWhileEditing(t *testing.T) {
	cfg := testConfig()
	model, err := NewModel(cfg, fakeLoader{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	model.editing = true
	model.filter = "api"
	model.cursor, model.offset = 2, 1

	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if command != nil {
		t.Fatal("Escape returned an unexpected command")
	}
	model = updated.(Model)
	if model.editing || model.filter != "" || model.cursor != 0 || model.offset != 0 {
		t.Fatalf("Escape did not clear filter state: editing=%v filter=%q cursor=%d offset=%d", model.editing, model.filter, model.cursor, model.offset)
	}
}

func TestModelMouseSelectsViewsRowsAndURL(t *testing.T) {
	cfg := testConfig()
	model, err := NewModel(cfg, fakeLoader{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	model.views = []discovery.ViewData{
		{View: cfg.Views[0], PRs: []gh.PullRequest{{Title: "Mine", URL: "https://github.com/acme/api/pull/1"}}},
		{View: cfg.Views[1], PRs: []gh.PullRequest{
			{Title: "First", URL: "https://github.com/acme/web/pull/1"},
			{Title: "Second", URL: "https://github.com/acme/web/pull/2"},
		}},
	}
	model.loading = false
	model.width, model.height = 120, 30

	secondTabX := 0
	for {
		index, ok := model.tabAtX(secondTabX)
		if ok && index == 1 {
			break
		}
		secondTabX++
		if secondTabX > model.width {
			t.Fatal("could not find the second tab")
		}
	}
	updated, command := model.Update(tea.MouseMsg(tea.MouseEvent{
		X: secondTabX, Y: tabRowY, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
	}))
	if command != nil {
		t.Fatal("tab click returned an unexpected command")
	}
	model = updated.(Model)
	if model.active != 1 {
		t.Fatalf("active view = %d, want 1", model.active)
	}

	lines := strings.Split(model.View(), "\n")
	secondRowY, urlY := -1, -1
	for y, line := range lines {
		switch {
		case strings.Contains(line, "Second"):
			secondRowY = y
		case strings.Contains(line, "https://github.com/acme/web/pull/1"):
			urlY = y
		}
	}
	if secondRowY != model.boardLayout().firstPRRow+1 {
		t.Fatalf("second rendered row Y = %d, mouse Y = %d", secondRowY, model.boardLayout().firstPRRow+1)
	}
	if urlY != model.boardLayout().selectedURLRow {
		t.Fatalf("rendered URL Y = %d, mouse URL Y = %d", urlY, model.boardLayout().selectedURLRow)
	}

	updated, command = model.Update(tea.MouseMsg(tea.MouseEvent{
		X: 2, Y: model.boardLayout().firstPRRow + 1, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
	}))
	if command != nil {
		t.Fatal("row click returned an unexpected command")
	}
	model = updated.(Model)
	if model.cursor != 1 {
		t.Fatalf("cursor = %d, want 1", model.cursor)
	}

	updated, command = model.Update(tea.MouseMsg(tea.MouseEvent{Button: tea.MouseButtonWheelUp}))
	if command != nil {
		t.Fatal("wheel returned an unexpected command")
	}
	model = updated.(Model)
	if model.cursor != 0 {
		t.Fatalf("wheel did not move the cursor: %d", model.cursor)
	}

	updated, command = model.Update(tea.MouseMsg(tea.MouseEvent{
		X: 2, Y: model.boardLayout().selectedURLRow, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
	}))
	if command == nil {
		t.Fatal("URL click did not return a browser command")
	}
	_ = updated
}

func TestModelDoesNotRefreshWhenSearchRateIsExhausted(t *testing.T) {
	cfg := testConfig()
	model, err := NewModel(cfg, fakeLoader{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	model.loading = false
	model.rates.Search = gh.RateResource{Limit: 30, Remaining: 0, Reset: time.Now().Add(time.Minute)}

	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	model = updated.(Model)
	if command != nil {
		t.Fatal("active refresh returned a command with an exhausted search rate limit")
	}
	if model.loading {
		t.Fatal("active refresh entered loading state with an exhausted search rate limit")
	}
}

func TestModelDoesNotRefreshAllBeyondSearchBudget(t *testing.T) {
	cfg := testConfig()
	model, err := NewModel(cfg, fakeLoader{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	model.loading = false
	model.rates.Search = gh.RateResource{Limit: 30, Remaining: 1, Reset: time.Now().Add(time.Minute)}

	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'R'}})
	model = updated.(Model)
	if command != nil {
		t.Fatal("full refresh returned a command without enough Search capacity")
	}
	if model.loading {
		t.Fatal("full refresh entered loading state without enough Search capacity")
	}
}

func TestModelUpdatesRatesAfterActiveRefresh(t *testing.T) {
	cfg := testConfig()
	model, err := NewModel(cfg, fakeLoader{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	model.loading = true
	model.rates.Search = gh.RateResource{Limit: 30, Remaining: 2}
	updatedAt := time.Now().Add(-time.Second)
	refresh := discovery.ViewSnapshot{
		Data:      discovery.ViewData{View: cfg.Views[0], UpdatedAt: updatedAt},
		Rates:     gh.RateLimits{Search: gh.RateResource{Limit: 30, Remaining: 1}},
		StartedAt: updatedAt,
	}

	updated, command := model.Update(viewMsg{index: 0, snapshot: refresh})
	if command != nil {
		t.Fatal("active refresh result returned an unexpected command")
	}
	model = updated.(Model)
	if model.rates.Search.Remaining != 1 {
		t.Fatalf("Search remaining = %d, want 1", model.rates.Search.Remaining)
	}
	if !model.views[0].UpdatedAt.Equal(updatedAt) {
		t.Fatalf("view updated time = %s, want %s", model.views[0].UpdatedAt, updatedAt)
	}
}

func TestModelKeepsLoadedRowsWhenRefreshFails(t *testing.T) {
	cfg := testConfig()
	model, err := NewModel(cfg, fakeLoader{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	model.views[0].PRs = []gh.PullRequest{{Repository: "acme/api", Number: 1, Title: "Keep me"}}
	model.views[0].UpdatedAt = time.Now()
	model.loading = true

	failed := discovery.Snapshot{Views: []discovery.ViewData{
		{View: cfg.Views[0], Err: errors.New("rate limited")},
		{View: cfg.Views[1]},
	}}
	failed.Errors = []discovery.RetrievalError{{Stage: "search", Err: failed.Views[0].Err}}
	updated, command := model.Update(snapshotMsg{Snapshot: failed})
	if command != nil {
		t.Fatal("snapshot update returned an unexpected command")
	}
	model = updated.(Model)
	if len(model.views[0].PRs) != 1 || model.views[0].PRs[0].Title != "Keep me" {
		t.Fatalf("stale rows were discarded: %#v", model.views[0].PRs)
	}
	if !strings.Contains(model.warning, "rate limited") {
		t.Fatalf("warning = %q", model.warning)
	}
}

func TestModelFreshnessAdvancesOnlyOnSuccessfulFullRefresh(t *testing.T) {
	cfg := testConfig()
	model, err := NewModel(cfg, fakeLoader{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	model.loading = true
	successAt := time.Now().Add(-10 * time.Minute)
	success := discovery.Snapshot{
		Views: []discovery.ViewData{
			{View: cfg.Views[0], PRs: []gh.PullRequest{{Repository: "acme/api", Number: 1, Title: "One"}}, UpdatedAt: successAt},
			{View: cfg.Views[1], PRs: []gh.PullRequest{{Repository: "acme/web", Number: 2, Title: "Two"}}, UpdatedAt: successAt},
		},
		StartedAt: successAt,
	}
	updated, _ := model.Update(snapshotMsg{Snapshot: success})
	model = updated.(Model)
	for i, view := range model.views {
		if !view.UpdatedAt.Equal(successAt) {
			t.Fatalf("view %d updated time = %s, want %s", i, view.UpdatedAt, successAt)
		}
	}

	failedAt := time.Now()
	model.loading = true
	failed := discovery.Snapshot{Views: []discovery.ViewData{
		{View: cfg.Views[0], Err: errors.New("GitHub search failed: timeout")},
		{View: cfg.Views[1], Err: errors.New("GitHub search failed: timeout")},
	}, StartedAt: failedAt}
	updated, _ = model.Update(snapshotMsg{Snapshot: failed})
	model = updated.(Model)
	for i, view := range model.views {
		if !view.UpdatedAt.Equal(successAt) {
			t.Fatalf("view %d updated time = %s, want %s after failed refresh", i, view.UpdatedAt, successAt)
		}
	}
}

func TestModelFreshnessPreservedPerViewInMixedFullRefresh(t *testing.T) {
	cfg := testConfig()
	model, err := NewModel(cfg, fakeLoader{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	firstSuccessAt := time.Now().Add(-20 * time.Minute)
	model.views[0] = discovery.ViewData{View: cfg.Views[0], PRs: []gh.PullRequest{{Repository: "acme/api", Number: 1, Title: "One"}}, UpdatedAt: firstSuccessAt}
	model.views[1] = discovery.ViewData{View: cfg.Views[1], PRs: []gh.PullRequest{{Repository: "acme/web", Number: 2, Title: "Two"}}, UpdatedAt: firstSuccessAt}
	model.loading = true

	secondSuccessAt := time.Now().Add(-time.Minute)
	mixed := discovery.Snapshot{Views: []discovery.ViewData{
		{View: cfg.Views[0], Err: errors.New("rate limited")},
		{View: cfg.Views[1], PRs: []gh.PullRequest{{Repository: "acme/web", Number: 3, Title: "Three"}}, UpdatedAt: secondSuccessAt},
	}, StartedAt: secondSuccessAt}
	updated, _ := model.Update(snapshotMsg{Snapshot: mixed})
	model = updated.(Model)
	if !model.views[0].UpdatedAt.Equal(firstSuccessAt) {
		t.Fatalf("failed view updated time = %s, want %s", model.views[0].UpdatedAt, firstSuccessAt)
	}
	if !model.views[1].UpdatedAt.Equal(secondSuccessAt) {
		t.Fatalf("successful view updated time = %s, want %s", model.views[1].UpdatedAt, secondSuccessAt)
	}
}

func TestModelFreshnessAdvancesOnlyOnSuccessfulActiveRefresh(t *testing.T) {
	cfg := testConfig()
	model, err := NewModel(cfg, fakeLoader{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	successAt := time.Now().Add(-15 * time.Minute)
	model.views[0] = discovery.ViewData{View: cfg.Views[0], PRs: []gh.PullRequest{{Repository: "acme/api", Number: 1, Title: "One"}}, UpdatedAt: successAt}

	failedAt := time.Now()
	model.loading = true
	failed := discovery.ViewSnapshot{
		Data:      discovery.ViewData{View: cfg.Views[0], Err: errors.New("GitHub search failed: timeout")},
		StartedAt: failedAt,
	}
	updated, _ := model.Update(viewMsg{index: 0, snapshot: failed})
	model = updated.(Model)
	if !model.views[0].UpdatedAt.Equal(successAt) {
		t.Fatalf("updated time = %s, want %s after failed active refresh", model.views[0].UpdatedAt, successAt)
	}
	if len(model.views[0].PRs) != 1 {
		t.Fatalf("retained rows were discarded: %#v", model.views[0].PRs)
	}
}

func TestModelFreshnessNotAdvancedByCapacityRejection(t *testing.T) {
	cfg := testConfig()
	model, err := NewModel(cfg, fakeLoader{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	successAt := time.Now().Add(-25 * time.Minute)
	model.views[0] = discovery.ViewData{View: cfg.Views[0], PRs: []gh.PullRequest{{Repository: "acme/api", Number: 1, Title: "One"}}, UpdatedAt: successAt}
	model.width, model.height = 120, 30
	model.loading = true

	capacityErr := errors.New("GitHub search rate limit exhausted")
	rejected := discovery.Snapshot{Views: []discovery.ViewData{
		{View: cfg.Views[0], Err: capacityErr},
		{View: cfg.Views[1], Err: capacityErr},
	}, StartedAt: time.Now()}
	rejected.Errors = []discovery.RetrievalError{{Stage: "search_budget", Err: rejected.Views[0].Err}}
	updated, _ := model.Update(snapshotMsg{Snapshot: rejected})
	model = updated.(Model)

	if !model.views[0].UpdatedAt.Equal(successAt) {
		t.Fatalf("updated time = %s, want %s after capacity rejection", model.views[0].UpdatedAt, successAt)
	}
	if len(model.views[0].PRs) != 1 {
		t.Fatalf("retained rows were discarded: %#v", model.views[0].PRs)
	}
	output := model.View()
	if !strings.Contains(output, "stale") {
		t.Fatalf("output missing stale marker:\n%s", output)
	}
	if !strings.Contains(output, "rate limit") {
		t.Fatalf("output missing capacity error:\n%s", output)
	}
}

func TestModelMarksRetainedRowsStaleWithoutHidingError(t *testing.T) {
	cfg := testConfig()
	model, err := NewModel(cfg, fakeLoader{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	successAt := time.Now().Add(-30 * time.Minute)
	model.views[0] = discovery.ViewData{View: cfg.Views[0], PRs: []gh.PullRequest{{Repository: "acme/api", Number: 1, Title: "One", URL: "https://github.com/acme/api/pull/1"}}, UpdatedAt: successAt}
	model.width, model.height = 120, 30
	model.active = 0

	model.loading = true
	failed := discovery.Snapshot{Views: []discovery.ViewData{
		{View: cfg.Views[0], Err: errors.New("GitHub search failed: timeout")},
	}, StartedAt: time.Now()}
	failed.Views = append(failed.Views, discovery.ViewData{View: cfg.Views[1]})
	failed.Errors = []discovery.RetrievalError{{Stage: "search", Err: failed.Views[0].Err}}
	updated, _ := model.Update(snapshotMsg{Snapshot: failed})
	model = updated.(Model)

	output := model.View()
	if !strings.Contains(output, "stale") {
		t.Fatalf("output missing stale marker:\n%s", output)
	}
	if !strings.Contains(output, "GitHub search failed: timeout") {
		t.Fatalf("output missing refresh error:\n%s", output)
	}
	if !strings.Contains(output, "One") {
		t.Fatalf("retained rows were not rendered:\n%s", output)
	}
	if len(model.views[0].PRs) != 1 {
		t.Fatalf("retained rows were discarded: %#v", model.views[0].PRs)
	}

	rowY, urlY := -1, -1
	for y, line := range strings.Split(output, "\n") {
		switch {
		case strings.Contains(line, "acme/api") && strings.Contains(line, "#1"):
			rowY = y
		case strings.HasPrefix(line, "https://github.com/acme/api/pull/1") || strings.HasSuffix(line, "https://github.com/acme/api/pull/1"):
			urlY = y
		}
	}
	if rowY != model.boardLayout().firstPRRow {
		t.Fatalf("stale rendered row Y = %d, mouse Y = %d", rowY, model.boardLayout().firstPRRow)
	}
	if urlY != model.boardLayout().selectedURLRow {
		t.Fatalf("stale rendered URL Y = %d, mouse URL Y = %d", urlY, model.boardLayout().selectedURLRow)
	}
	if urlY >= model.height {
		t.Fatalf("stale rendered URL Y = %d exceeds height %d", urlY, model.height)
	}
}

func TestBrowserCommandUsesPlatformLauncher(t *testing.T) {
	tests := []struct {
		name string
		goos string
		want string
	}{
		{name: "macOS", goos: "darwin", want: "open"},
		{name: "Linux", goos: "linux", want: "xdg-open"},
		{name: "Windows", goos: "windows", want: "rundll32.exe"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			command := browserCommand(tt.goos, "https://github.com/acme/api/pull/1")
			if command.Args[0] != tt.want {
				t.Fatalf("browser command = %q, want %q", command.Args[0], tt.want)
			}
		})
	}
}

func TestModelReportsBrowserOpenFailure(t *testing.T) {
	model := browserModel(t, testConfig())
	model.openBrowser = func(_ string) tea.Cmd {
		return func() tea.Msg { return browserMsg{err: errors.New("open: could not launch browser")} }
	}

	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if command == nil {
		t.Fatal("Enter did not return a browser command")
	}
	model = updated.(Model)
	msg := command()
	updated, command = model.Update(msg)
	if command != nil {
		t.Fatal("browser result returned an unexpected command")
	}
	model = updated.(Model)
	if !strings.Contains(model.warning, "could not launch browser") {
		t.Fatalf("warning = %q", model.warning)
	}
	output := model.View()
	if !strings.Contains(output, "https://github.com/acme/api/pull/1") {
		t.Fatalf("URL no longer visible after failure:\n%s", output)
	}
	if model.loading {
		t.Fatal("board entered loading state after a browser failure")
	}
}

func TestModelBrowserOpenSuccessKeepsBoardUsable(t *testing.T) {
	model := browserModel(t, testConfig())
	model.openBrowser = func(_ string) tea.Cmd {
		return func() tea.Msg { return browserMsg{} }
	}

	updated, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})
	if command == nil {
		t.Fatal("o did not return a browser command")
	}
	model = updated.(Model)
	msg := command()
	updated, command = model.Update(msg)
	if command != nil {
		t.Fatal("browser result returned an unexpected command")
	}
	model = updated.(Model)
	if strings.Contains(model.warning, "browser") {
		t.Fatalf("success produced a warning: %q", model.warning)
	}
	if model.loading {
		t.Fatal("board entered loading state after a successful open")
	}
}

func TestModelBrowserOpenTriggersFromURLMouseClick(t *testing.T) {
	model := browserModel(t, testConfig())
	var opened string
	model.openBrowser = func(url string) tea.Cmd {
		opened = url
		return func() tea.Msg { return browserMsg{} }
	}

	updated, command := model.Update(tea.MouseMsg(tea.MouseEvent{
		X: 2, Y: model.boardLayout().selectedURLRow, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
	}))
	if command == nil {
		t.Fatal("URL click did not return a browser command")
	}
	_ = updated
	if opened != "https://github.com/acme/api/pull/1" {
		t.Fatalf("opened = %q", opened)
	}
}

func browserModel(t *testing.T, cfg config.Config) Model {
	t.Helper()
	model, err := NewModel(cfg, fakeLoader{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	model.views = []discovery.ViewData{
		{View: cfg.Views[0], PRs: []gh.PullRequest{{Repository: "acme/api", Number: 1, Title: "API fix", URL: "https://github.com/acme/api/pull/1"}}},
		{View: cfg.Views[1]},
	}
	model.loading = false
	model.width, model.height = 120, 30
	return model
}

func testConfig() config.Config {
	return config.Config{
		UI: config.UIConfig{Title: "Engineering PRs"},
		GitHub: config.GitHubConfig{
			RefreshInterval: "5m",
			LimitPerScope:   100,
			MaxConcurrency:  2,
			CIBatchSize:     25,
		},
		Sidebar: config.SidebarConfig{Enabled: boolPtr(false)},
		Views: []config.View{
			{ID: "mine", Title: "Opened by me", Query: "is:open author:@me", Scope: "global"},
			{ID: "review", Title: "Review requested", Query: "is:open review-requested:@me", Scope: "global"},
		},
	}
}

func boolPtr(value bool) *bool {
	return &value
}

func TestCtrlCQuitsWhileEditingFilter(t *testing.T) {
	model, err := NewModel(testConfig(), fakeLoader{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	model = updated.(Model)
	if !model.editing {
		t.Fatal("expected filter editing mode")
	}
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("ctrl+c while editing the filter must quit")
	}
	if msg := cmd(); msg == nil {
		t.Fatal("quit command returned no message")
	} else if _, ok := msg.(tea.QuitMsg); !ok {
		t.Fatalf("message = %T, want tea.QuitMsg", msg)
	}
}

func TestConfigReloadRebuildsInjectedSidebar(t *testing.T) {
	cfg := testConfig()
	enabled := false
	cfg.Sidebar.Enabled = &enabled
	model, err := NewModelWithConfigPath(cfg, "config.toml", fakeLoader{}, func(settings config.SidebarConfig) *sidebar.Reporter {
		return sidebar.NewReporter(settings, "workspace", "/custom/herdr")
	})
	if err != nil {
		t.Fatal(err)
	}
	if model.sidebar != nil {
		t.Fatal("disabled sidebar has a reporter")
	}
	enabled = true
	cfg.Sidebar.TTL = "2m"
	model = model.applyConfig(cfg, fakeLoader{}, 0)
	if model.sidebar == nil || model.sidebar.WorkspaceID != "workspace" || model.sidebar.Binary != "/custom/herdr" || model.sidebar.TTL != 2*time.Minute {
		t.Fatalf("reloaded reporter = %#v", model.sidebar)
	}
	enabled = false
	model = model.applyConfig(cfg, fakeLoader{}, 0)
	if model.sidebar != nil {
		t.Fatal("sidebar remains enabled after reload")
	}
}

func TestModelPartialSearchKeepsPreviousSuccessfulObservation(t *testing.T) {
	cfg := testConfig()
	for _, previous := range []bool{false, true} {
		model, err := NewModel(cfg, fakeLoader{}, nil)
		if err != nil {
			t.Fatal(err)
		}
		old := time.Now().Add(-time.Minute)
		if previous {
			model.views[0] = discovery.ViewData{View: cfg.Views[0], PRs: []gh.PullRequest{{Title: "Old A"}, {Title: "Old B"}}, UpdatedAt: old, ObservedAt: old}
		}
		observed := time.Now()
		partial := discovery.ViewData{View: cfg.Views[0], PRs: []gh.PullRequest{{Title: "Fresh A"}}, ObservedAt: observed, Err: errors.New("one scope failed")}
		updated, _ := model.Update(viewMsg{index: 0, snapshot: discovery.ViewSnapshot{Data: partial}})
		view := updated.(Model).views[0]
		if previous {
			if len(view.PRs) != 2 || view.PRs[0].Title != "Old A" || !view.UpdatedAt.Equal(old) || !view.ObservedAt.Equal(old) || !stale(view) {
				t.Fatalf("retained view=%+v", view)
			}
		} else if len(view.PRs) != 1 || view.PRs[0].Title != "Fresh A" || !view.UpdatedAt.IsZero() || !view.ObservedAt.Equal(observed) || stale(view) {
			t.Fatalf("initial partial view=%+v", view)
		}
	}
}

func TestFailedConfigReloadPreservesSearchObservation(t *testing.T) {
	cfg := testConfig()
	model, err := NewModel(cfg, fakeLoader{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	observed := time.Now().Add(-time.Minute)
	model.views[0] = discovery.ViewData{View: cfg.Views[0], PRs: []gh.PullRequest{{Title: "Retained"}}, UpdatedAt: observed, ObservedAt: observed}
	cfg.Views[0].Title = "Renamed"
	snapshot := discovery.Snapshot{Views: []discovery.ViewData{{View: cfg.Views[0], Err: errors.New("search failed")}, {View: cfg.Views[1]}}}
	updated, _ := model.Update(configRefreshMsg{cfg: cfg, loader: fakeLoader{}, snapshot: snapshot})
	view := updated.(Model).views[0]
	if view.View.Title != "Renamed" || len(view.PRs) != 1 || view.PRs[0].Title != "Retained" || !view.UpdatedAt.Equal(observed) || !view.ObservedAt.Equal(observed) {
		t.Fatalf("reloaded view=%+v", view)
	}
}

// fallbackEditor is the executable the board uses when neither $VISUAL nor
// $EDITOR names one.
func fallbackEditor() string {
	if runtime.GOOS == "windows" {
		return "notepad.exe"
	}
	return "vi"
}

// TestEditorResolutionNamesTheLaunchedExecutable covers every resolution path
// and proves that the notice names the executable that the board runs.
func TestEditorResolutionNamesTheLaunchedExecutable(t *testing.T) {
	visual := "/Applications/Visual Studio Code.app/Contents/MacOS/Electron"
	cases := []struct {
		name   string
		visual string
		editor string
		want   string
	}{
		{"visual wins", visual, "nano", visual},
		{"editor follows visual", "", "vim", "vim"},
		{"blank values fall back", "   ", "\t", fallbackEditor()},
		{"unset values fall back", "", "", fallbackEditor()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("VISUAL", tc.visual)
			t.Setenv("EDITOR", tc.editor)
			if got := resolveEditor(); got != tc.want {
				t.Fatalf("resolveEditor() = %q, want %q", got, tc.want)
			}
			path := filepath.Join(t.TempDir(), "config.toml")
			launch := newEditorLaunch(path)
			if args := launch.command.Args; len(args) != 2 || args[0] != tc.want || args[1] != path {
				t.Fatalf("editor command = %#v, want %q %q", args, tc.want, path)
			}
			want := "Opening config in " + tc.want + ". Set $VISUAL or $EDITOR to change."
			if launch.notice != want {
				t.Fatalf("notice = %q, want %q", launch.notice, want)
			}
			notice, command := editConfigCmd(path)
			if notice != want || command == nil {
				t.Fatalf("editConfigCmd notice = %q, want %q", notice, want)
			}
		})
	}
}

// TestEditorLaunchWritesTheNoticeToTheReleasedTerminal covers the frame that
// the board cannot show. Bubble Tea leaves the alternate screen before it runs
// the editor, so the launch writes the notice to the terminal itself. The
// write comes first, so a missing editor still names itself.
func TestEditorLaunchWritesTheNoticeToTheReleasedTerminal(t *testing.T) {
	t.Setenv("VISUAL", "herdr-pr-board-editor-that-does-not-exist")
	t.Setenv("EDITOR", "")
	launch := newEditorLaunch(filepath.Join(t.TempDir(), "config.toml"))
	var terminal strings.Builder
	launch.SetStdin(strings.NewReader(""))
	launch.SetStdout(&terminal)
	launch.SetStderr(io.Discard)
	if err := launch.Run(); err == nil {
		t.Fatal("a missing editor reported success")
	}
	if got := strings.TrimRight(terminal.String(), "\r\n"); got != launch.notice {
		t.Fatalf("the terminal received %q, want %q", got, launch.notice)
	}
}

func TestEditConfigCommandStaysSilentWithoutAPath(t *testing.T) {
	notice, command := editConfigCmd("  ")
	if notice != "" {
		t.Fatalf("announced an editor for an unavailable path: %q", notice)
	}
	message, ok := command().(configEditMsg)
	if !ok || message.err == nil {
		t.Fatalf("message = %#v, want a configuration edit failure", message)
	}
}

func TestEditKeyAnnouncesTheEditorAndValidationReplacesTheNotice(t *testing.T) {
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "")
	model := layoutModel(t, 120)
	model.configPath = filepath.Join(t.TempDir(), "config.toml")
	model.loading = false
	launched := ""
	model.editConfig = func(path string) (string, tea.Cmd) {
		launch := newEditorLaunch(path)
		launched = launch.command.Args[0]
		return launch.notice, nil
	}

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("E")})
	announced := updated.(Model)
	notice := "Opening config in " + fallbackEditor() + ". Set $VISUAL or $EDITOR to change."
	if launched != fallbackEditor() {
		t.Fatalf("launched %q, want %q", launched, fallbackEditor())
	}
	if footer := stripANSI(announced.renderFooter()); !strings.Contains(footer, notice) {
		t.Fatalf("footer missing %q:\n%s", notice, footer)
	}

	after, _ := announced.Update(configEditMsg{err: errors.New("editor: exit status 1")})
	footer := stripANSI(after.(Model).renderFooter())
	if strings.Contains(footer, notice) {
		t.Fatalf("the notice outlived the editor:\n%s", footer)
	}
	if !strings.Contains(footer, "configuration edit failed: editor: exit status 1") {
		t.Fatalf("footer missing the validation message:\n%s", footer)
	}
}

func TestEditKeyStaysSilentWhenNoEditorLaunches(t *testing.T) {
	model := layoutModel(t, 120)
	model.loading = false
	model.editConfig = nil

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("E")})
	footer := stripANSI(updated.(Model).renderFooter())
	if strings.Contains(footer, "Opening config in") {
		t.Fatalf("announced an editor that does not open:\n%s", footer)
	}
	if !strings.Contains(footer, "configuration editor is unavailable") {
		t.Fatalf("footer missing the unavailable message:\n%s", footer)
	}
}
