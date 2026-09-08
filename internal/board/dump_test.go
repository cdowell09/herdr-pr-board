package board

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cdowell09/herdr-pr-board/internal/config"
	"github.com/cdowell09/herdr-pr-board/internal/discovery"
	"github.com/cdowell09/herdr-pr-board/internal/dispatch"
	gh "github.com/cdowell09/herdr-pr-board/internal/github"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func TestRenderDump(t *testing.T) {
	dir := os.Getenv("BOARD_DUMP_DIR")
	if dir == "" {
		t.Skip("set BOARD_DUMP_DIR to write layout renders")
	}
	lipgloss.SetColorProfile(termenv.TrueColor)
	for _, size := range []struct {
		width, height int
		name          string
	}{
		{50, 22, "narrow"},
		{90, 28, "medium"},
		{140, 34, "wide"},
	} {
		model := dumpModel(t, size.width, size.height)
		if err := os.WriteFile(filepath.Join(dir, size.name+".ansi"), []byte(model.View()), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func dumpModel(t *testing.T, width, height int) Model {
	t.Helper()
	cfg := testConfig()
	cfg.Views = append(cfg.Views, config.View{ID: "all", Title: "All open", Query: "is:open", Scope: "configured"})
	now := time.Now()
	model, err := NewModel(cfg, fakeLoader{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	model.views = []discovery.ViewData{
		{View: cfg.Views[0], PRs: []gh.PullRequest{
			{Repository: "cdowell09/herdr-pr-board", Number: 74, Title: "feat(ui): responsive layouts for narrow terminals", URL: "https://github.com/cdowell09/herdr-pr-board/pull/74", Author: "cdowell09", UpdatedAt: now.Add(-2 * time.Hour), CI: gh.CISuccess},
			{Repository: "cdowell09/cookies", Number: 18, Title: "Add the cookie schedule export", URL: "https://github.com/cdowell09/cookies/pull/18", Author: "cdowell09", UpdatedAt: now.Add(-time.Hour), CI: gh.CIPending},
			{Repository: "acme/web-ui", Number: 452, Title: "Make the onboarding flow keyboard friendly", URL: "https://github.com/acme/web-ui/pull/452", Author: "ada", UpdatedAt: now.Add(-45 * time.Minute), CI: gh.CISuccess},
			{Repository: "acme/api-gateway", Number: 201, Title: "Add localized settings", URL: "https://github.com/acme/api-gateway/pull/201", Author: "grace", UpdatedAt: now.Add(-30 * time.Minute), CI: gh.CIFailure, Draft: true},
		}},
		{View: cfg.Views[1], PRs: []gh.PullRequest{
			{Repository: "acme/monorepo", Number: 999, Title: "Migrate CI pipelines to reusable workflows", URL: "https://github.com/acme/monorepo/pull/999", Author: "lin", UpdatedAt: now.Add(-20 * time.Minute), CI: gh.CISuccess},
			{Repository: "acme/design-system", Number: 87, Title: "Keep dark mode tokens in sync", URL: "https://github.com/acme/design-system/pull/87", Author: "sam", UpdatedAt: now.Add(-10 * time.Minute), CI: gh.CIError},
		}},
		{View: cfg.Views[2]},
	}
	model.reviewRows = map[string]reviewOverviewRow{}
	summaries := []reviewRowSummary{
		{state: "completed", detail: "Completed locally", posted: "Both", postedDetail: "GitHub · current revision; PR Board · older revision"},
		{state: "running", detail: "Running", posted: "PR Board", postedDetail: "PR Board · older revision"},
		{state: "waiting", detail: "Waiting for review slot", posted: "GitHub", postedDetail: "GitHub · current revision"},
		{state: "blocked", detail: "blocked · explicit retry required", posted: "–", postedDetail: "No submitted reviews"},
	}
	for i, pr := range model.views[0].PRs {
		model.reviewRows[pr.URL] = reviewOverviewRow{identity: dispatch.Identity(pr), summary: summaries[i]}
	}
	model.active = 0
	model.loading = false
	model.rates.Search = gh.RateResource{Limit: 30, Remaining: 27}
	model.rates.GraphQL = gh.RateResource{Limit: 5000, Remaining: 4823}
	model.views[0].UpdatedAt = now.Add(-5 * time.Minute)
	model.views[1].UpdatedAt = now.Add(-5 * time.Minute)
	model.views[2].UpdatedAt = now.Add(-5 * time.Minute)
	model.width, model.height = width, height
	return model
}
