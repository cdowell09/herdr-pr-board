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
func (f fakeLoader) RefreshOne(_ context.Context, view config.View) ViewData {
	for _, data := range f.snapshot.Views {
		if data.View.ID == view.ID {
			return data
		}
	}
	return ViewData{View: view}
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
