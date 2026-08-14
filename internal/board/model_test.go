package board

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cdowell09/herdr-pr-board/internal/config"
	gh "github.com/cdowell09/herdr-pr-board/internal/github"
	tea "github.com/charmbracelet/bubbletea"
)

type fakeLoader struct {
	snapshot Snapshot
}

func (f fakeLoader) RefreshAll(context.Context) Snapshot { return f.snapshot }
func (f fakeLoader) RefreshOne(_ context.Context, view config.View) ViewSnapshot {
	refresh := ViewSnapshot{
		Data:      ViewData{View: view},
		Rates:     f.snapshot.Rates,
		Warning:   f.snapshot.Warning,
		UpdatedAt: f.snapshot.UpdatedAt,
	}
	for _, data := range f.snapshot.Views {
		if data.View.ID == view.ID {
			refresh.Data = data
			break
		}
	}
	return refresh
}

func TestModelRendersConfigTitlesPRAndCI(t *testing.T) {
	cfg := testConfig()
	updated := time.Now().Add(-2 * time.Hour)
	snapshot := Snapshot{
		Views: []ViewData{
			{View: cfg.Views[0], PRs: []gh.PullRequest{{Repository: "cdowell09/cookies", Number: 2, Title: "Cookie schedule", URL: "https://github.com/cdowell09/cookies/pull/2", Author: "cdowell09", UpdatedAt: updated, CI: gh.CISuccess}}},
			{View: cfg.Views[1]},
		},
		Rates:     gh.RateLimits{Search: gh.RateResource{Limit: 30, Remaining: 28}},
		UpdatedAt: time.Now(),
	}
	model, err := NewModel(cfg, fakeLoader{snapshot: snapshot})
	if err != nil {
		t.Fatal(err)
	}
	updatedModel, _ := model.Update(snapshotMsg(snapshot))
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

func TestModelSwitchesViewsAndFilters(t *testing.T) {
	cfg := testConfig()
	snapshot := Snapshot{Views: []ViewData{
		{View: cfg.Views[0], PRs: []gh.PullRequest{{Repository: "acme/api", Number: 1, Title: "API fix"}}},
		{View: cfg.Views[1], PRs: []gh.PullRequest{{Repository: "acme/web", Number: 2, Title: "Web fix"}}},
	}}
	model, err := NewModel(cfg, fakeLoader{snapshot: snapshot})
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

func TestModelEscapeClearsFilterWhileEditing(t *testing.T) {
	cfg := testConfig()
	model, err := NewModel(cfg, fakeLoader{})
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
	model, err := NewModel(cfg, fakeLoader{})
	if err != nil {
		t.Fatal(err)
	}
	model.views = []ViewData{
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
	if secondRowY != firstPRRowY+1 {
		t.Fatalf("second rendered row Y = %d, mouse Y = %d", secondRowY, firstPRRowY+1)
	}
	if urlY != model.selectedURLY() {
		t.Fatalf("rendered URL Y = %d, mouse URL Y = %d", urlY, model.selectedURLY())
	}

	updated, command = model.Update(tea.MouseMsg(tea.MouseEvent{
		X: 2, Y: firstPRRowY + 1, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
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
		X: 2, Y: model.selectedURLY(), Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
	}))
	if command == nil {
		t.Fatal("URL click did not return a browser command")
	}
	_ = updated
}

func TestModelDoesNotRefreshWhenSearchRateIsExhausted(t *testing.T) {
	cfg := testConfig()
	model, err := NewModel(cfg, fakeLoader{})
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
	model, err := NewModel(cfg, fakeLoader{})
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
	model, err := NewModel(cfg, fakeLoader{})
	if err != nil {
		t.Fatal(err)
	}
	model.loading = true
	model.rates.Search = gh.RateResource{Limit: 30, Remaining: 2}
	updatedAt := time.Now().Add(-time.Second)
	refresh := ViewSnapshot{
		Data:      ViewData{View: cfg.Views[0]},
		Rates:     gh.RateLimits{Search: gh.RateResource{Limit: 30, Remaining: 1}},
		UpdatedAt: updatedAt,
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
	model, err := NewModel(cfg, fakeLoader{})
	if err != nil {
		t.Fatal(err)
	}
	model.views[0].PRs = []gh.PullRequest{{Repository: "acme/api", Number: 1, Title: "Keep me"}}
	model.loading = true

	failed := Snapshot{Views: []ViewData{
		{View: cfg.Views[0], Err: errors.New("rate limited")},
		{View: cfg.Views[1]},
	}}
	updated, command := model.Update(snapshotMsg(failed))
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
	model, err := NewModel(cfg, fakeLoader{})
	if err != nil {
		t.Fatal(err)
	}
	model.loading = true
	successAt := time.Now().Add(-10 * time.Minute)
	success := Snapshot{
		Views: []ViewData{
			{View: cfg.Views[0], PRs: []gh.PullRequest{{Repository: "acme/api", Number: 1, Title: "One"}}},
			{View: cfg.Views[1], PRs: []gh.PullRequest{{Repository: "acme/web", Number: 2, Title: "Two"}}},
		},
		UpdatedAt: successAt,
	}
	updated, _ := model.Update(snapshotMsg(success))
	model = updated.(Model)
	for i, view := range model.views {
		if !view.UpdatedAt.Equal(successAt) {
			t.Fatalf("view %d updated time = %s, want %s", i, view.UpdatedAt, successAt)
		}
	}

	failedAt := time.Now()
	model.loading = true
	failed := Snapshot{Views: []ViewData{
		{View: cfg.Views[0], Err: errors.New("GitHub search failed: timeout")},
		{View: cfg.Views[1], Err: errors.New("GitHub search failed: timeout")},
	}, UpdatedAt: failedAt}
	updated, _ = model.Update(snapshotMsg(failed))
	model = updated.(Model)
	for i, view := range model.views {
		if !view.UpdatedAt.Equal(successAt) {
			t.Fatalf("view %d updated time = %s, want %s after failed refresh", i, view.UpdatedAt, successAt)
		}
	}
}

func TestModelFreshnessPreservedPerViewInMixedFullRefresh(t *testing.T) {
	cfg := testConfig()
	model, err := NewModel(cfg, fakeLoader{})
	if err != nil {
		t.Fatal(err)
	}
	firstSuccessAt := time.Now().Add(-20 * time.Minute)
	model.views[0] = ViewData{View: cfg.Views[0], PRs: []gh.PullRequest{{Repository: "acme/api", Number: 1, Title: "One"}}, UpdatedAt: firstSuccessAt}
	model.views[1] = ViewData{View: cfg.Views[1], PRs: []gh.PullRequest{{Repository: "acme/web", Number: 2, Title: "Two"}}, UpdatedAt: firstSuccessAt}
	model.loading = true

	secondSuccessAt := time.Now().Add(-time.Minute)
	mixed := Snapshot{Views: []ViewData{
		{View: cfg.Views[0], Err: errors.New("rate limited")},
		{View: cfg.Views[1], PRs: []gh.PullRequest{{Repository: "acme/web", Number: 3, Title: "Three"}}},
	}, UpdatedAt: secondSuccessAt}
	updated, _ := model.Update(snapshotMsg(mixed))
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
	model, err := NewModel(cfg, fakeLoader{})
	if err != nil {
		t.Fatal(err)
	}
	successAt := time.Now().Add(-15 * time.Minute)
	model.views[0] = ViewData{View: cfg.Views[0], PRs: []gh.PullRequest{{Repository: "acme/api", Number: 1, Title: "One"}}, UpdatedAt: successAt}

	failedAt := time.Now()
	model.loading = true
	failed := ViewSnapshot{
		Data:      ViewData{View: cfg.Views[0], Err: errors.New("GitHub search failed: timeout")},
		UpdatedAt: failedAt,
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
	model, err := NewModel(cfg, fakeLoader{})
	if err != nil {
		t.Fatal(err)
	}
	successAt := time.Now().Add(-25 * time.Minute)
	model.views[0] = ViewData{View: cfg.Views[0], PRs: []gh.PullRequest{{Repository: "acme/api", Number: 1, Title: "One"}}, UpdatedAt: successAt}
	model.width, model.height = 120, 30
	model.loading = true

	capacityErr := searchCapacityError(gh.RateResource{Limit: 30, Remaining: 1, Reset: time.Now().Add(time.Minute)}, cfg.SearchRequestCount())
	rejected := Snapshot{Views: []ViewData{
		{View: cfg.Views[0], Err: capacityErr},
		{View: cfg.Views[1], Err: capacityErr},
	}, UpdatedAt: time.Now()}
	updated, _ := model.Update(snapshotMsg(rejected))
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
	model, err := NewModel(cfg, fakeLoader{})
	if err != nil {
		t.Fatal(err)
	}
	successAt := time.Now().Add(-30 * time.Minute)
	model.views[0] = ViewData{View: cfg.Views[0], PRs: []gh.PullRequest{{Repository: "acme/api", Number: 1, Title: "One", URL: "https://github.com/acme/api/pull/1"}}, UpdatedAt: successAt}
	model.width, model.height = 120, 30
	model.active = 0

	model.loading = true
	failed := Snapshot{Views: []ViewData{
		{View: cfg.Views[0], Err: errors.New("GitHub search failed: timeout")},
	}, UpdatedAt: time.Now()}
	failed.Views = append(failed.Views, ViewData{View: cfg.Views[1]})
	updated, _ := model.Update(snapshotMsg(failed))
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
	if rowY != firstPRRowYFor(true) {
		t.Fatalf("stale rendered row Y = %d, mouse Y = %d", rowY, firstPRRowYFor(true))
	}
	if urlY != model.selectedURLY() {
		t.Fatalf("stale rendered URL Y = %d, mouse URL Y = %d", urlY, model.selectedURLY())
	}
	if urlY >= model.height {
		t.Fatalf("stale rendered URL Y = %d exceeds height %d", urlY, model.height)
	}
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
		Views: []config.View{
			{ID: "mine", Title: "Opened by me", Query: "is:open author:@me", Scope: "global"},
			{ID: "review", Title: "Review requested", Query: "is:open review-requested:@me", Scope: "global"},
		},
	}
}
