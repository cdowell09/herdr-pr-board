package board

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cdowell09/herdr-pr-board/internal/config"
	"github.com/cdowell09/herdr-pr-board/internal/discovery"
	"github.com/cdowell09/herdr-pr-board/internal/dispatch"
	gh "github.com/cdowell09/herdr-pr-board/internal/github"
	"github.com/cdowell09/herdr-pr-board/internal/monitor"
	"github.com/cdowell09/herdr-pr-board/internal/publication"
	"github.com/cdowell09/herdr-pr-board/internal/reviewmemory"
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
			{Repository: "cdowell09/herdr-pr-board", Number: 74, Title: "feat(ui): responsive layouts for narrow terminals", URL: "https://github.com/cdowell09/herdr-pr-board/pull/74", Author: "cdowell09", UpdatedAt: now.Add(-2 * time.Hour), CI: gh.CISuccess, HeadOID: "a1b2c3d4e5f60718293a4b5c6d7e8f9012345678", BaseRefName: "main", MetadataObservedAt: now},
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
		{state: "completed", detail: "Completed locally · P0:1 P1:2 P2:0 P3:3", posted: "Both", postedDetail: "GitHub · current revision; PR Board · older revision", findings: reviewmemory.SeverityCounts{1, 2, 0, 3}},
		{state: "running", detail: "Running", posted: "PR Board", postedDetail: "PR Board · older revision"},
		{state: "completed", detail: "Completed locally · no findings", posted: "GitHub", postedDetail: "GitHub · current revision"},
		{state: "blocked", detail: "blocked · explicit retry required", posted: "–", postedDetail: "No submitted reviews"},
	}
	for i, pr := range model.views[0].PRs {
		model.reviewRows[pr.URL] = reviewOverviewRow{identity: dispatch.Identity(pr), summary: summaries[i]}
	}
	model.active = 0
	model.loading = false
	// The region shows the fourth row's completed review with its findings.
	model = model.WithReviews(context.Background(), &reviewFake{})
	reviewed := model.views[0].PRs[0]
	run := reviewmemory.Run{ID: "0209edd94356f594934f74fc81b64635", Reviewer: "claude", StartedAt: now.Add(-90 * time.Minute), Identity: dispatch.Identity(reviewed), Outcome: reviewmemory.Outcome{Status: reviewmemory.Completed, Findings: []reviewmemory.Finding{
		{Severity: "P0", Title: "Narrow layout drops the URL line below 60 columns", Path: "internal/board/table.go", Line: 212, Body: "The narrow tier hides the URL row when the height is under 18, so Enter opens nothing the user can see."},
		{Severity: "P1", Title: "Tab labels truncate without an ellipsis", Path: "internal/board/model.go", Line: 711, Body: "Long view titles cut mid-word and the last visible tab has no marker that more tabs exist."},
		{Severity: "P1", Title: "Footer help wraps onto the rate line at 80 columns", Path: "internal/board/footer.go", Line: 52, Body: "The packed pairs assume one line and push the meta line off the screen."},
		{Severity: "P3", Title: "Comment names the old REV column", Path: "docs/journeys.md", Line: 8, Body: "The journey text still says REV where the header says REVIEW."},
		{Severity: "P3", Title: "Test fixture repeats the same title", Path: "internal/board/layout_test.go", Line: 29, Body: "Three rows share one title, which hides truncation differences."},
		{Severity: "P3", Title: "Unused import alias in the dump test", Path: "internal/board/dump_test.go", Line: 12, Body: "The alias is only used by the removed table fixture."},
	}}}
	model.region = &reviewRegion{pr: reviewed}
	model.overview.monitor = monitor.Status{State: monitor.Running, ObservationOK: true}
	model.setRegionData(func(data *regionData) {
		data.runs = []reviewmemory.Run{run}
		data.publications = []publication.Attempt{{RunID: run.ID, Action: config.PublishComment, Status: publication.Published}}
	})
	model.cfg.Repositories = []config.Repository{{Name: reviewed.Repository, Reviewer: "claude", AutoLaunch: true}}
	model.cursor = 3
	model.rates.Search = gh.RateResource{Limit: 30, Remaining: 27}
	model.rates.GraphQL = gh.RateResource{Limit: 5000, Remaining: 4823}
	model.views[0].UpdatedAt = now.Add(-5 * time.Minute)
	model.views[1].UpdatedAt = now.Add(-5 * time.Minute)
	model.views[2].UpdatedAt = now.Add(-5 * time.Minute)
	model.width, model.height = width, height
	return model
}
