package board

import (
	"errors"
	"strings"
	"testing"

	"github.com/cdowell09/herdr-pr-board/internal/discovery"
	tea "github.com/charmbracelet/bubbletea"
)

func TestAppendWarningDropsEmptyAndDuplicateEntries(t *testing.T) {
	warning := appendWarning("", "")
	warning = appendWarning(warning, "search budget exceeded")
	warning = appendWarning(warning, "search budget exceeded")
	warning = appendWarning(warning, "")
	warning = appendWarning(warning, "CI refresh failed: boom")
	if warning != "search budget exceeded; CI refresh failed: boom" {
		t.Fatalf("warning = %q", warning)
	}
}

func TestModelListsSharedRefreshErrorOnce(t *testing.T) {
	cfg := testConfig()
	model, err := NewModel(cfg, fakeLoader{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	budget := errors.New("GitHub search rate limit has 0 requests remaining but refresh requires 2; resets at 13:00")
	failed := discovery.Snapshot{Views: []discovery.ViewData{
		{View: cfg.Views[0], Err: budget},
		{View: cfg.Views[1], Err: budget},
	}}
	failed.Errors = []discovery.RetrievalError{{Stage: "search_budget", Err: budget}}
	updated, _ := model.Update(snapshotMsg{Snapshot: failed})
	model = updated.(Model)
	if got := strings.Count(model.warning, "refresh requires 2"); got != 1 {
		t.Fatalf("shared error listed %d times in %q, want once", got, model.warning)
	}
}

func TestModelFormatsStructuredFailuresForFullAndActiveRefresh(t *testing.T) {
	cfg := testConfig()
	failures := []discovery.RetrievalError{
		{Stage: "rates", Err: errors.New("rate endpoint failed")},
		{Stage: "enrichment", Err: errors.New("GraphQL failed")},
		{Stage: "search", ViewID: cfg.Views[0].ID, Err: errors.New("search failed")},
		{Stage: "search", ViewID: cfg.Views[1].ID, Err: errors.New("search failed")},
	}
	data := discovery.ViewData{View: cfg.Views[0], Err: failures[2].Err}
	for _, message := range []tea.Msg{
		snapshotMsg{Snapshot: discovery.Snapshot{Views: []discovery.ViewData{data}, Errors: failures}},
		viewMsg{index: 0, snapshot: discovery.ViewSnapshot{Data: data, Errors: failures}},
	} {
		model, err := NewModel(cfg, fakeLoader{}, nil)
		if err != nil {
			t.Fatal(err)
		}
		updated, _ := model.Update(message)
		model = updated.(Model)
		want := "rate limits unavailable: rate endpoint failed; CI refresh failed: GraphQL failed; search failed"
		if model.warning != want {
			t.Fatalf("%T warning=%q want=%q", message, model.warning, want)
		}
		updated, _ = model.Update(browserMsg{err: errors.New("browser failed")})
		model = updated.(Model)
		if !strings.HasPrefix(model.warning, want+"; ") || strings.Count(model.warning, "browser failed") != 1 {
			t.Fatalf("browser warning=%q", model.warning)
		}
		updated, _ = model.Update(browserMsg{err: errors.New("browser failed")})
		model = updated.(Model)
		if strings.Count(model.warning, "browser failed") != 1 {
			t.Fatalf("duplicate browser warning=%q", model.warning)
		}
	}
}
