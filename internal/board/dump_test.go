package board

import (
	"context"
	"fmt"
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
		{140, 26, "wide"},
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
	repo := "cdowell09/herdr-pr-board"
	pr := func(number int, title string, age time.Duration, ci gh.CIState) gh.PullRequest {
		return gh.PullRequest{Repository: repo, Number: number, Title: title, URL: fmt.Sprintf("https://github.com/%s/pull/%d", repo, number), Author: "cdowell09", UpdatedAt: now.Add(-age), CI: ci, HeadOID: "ed2f6ff2a1b2c3d4e5f60718293a4b5c6d7e8f90", BaseRefName: "main", MetadataObservedAt: now}
	}
	model.views = []discovery.ViewData{
		{View: cfg.Views[0], PRs: []gh.PullRequest{
			pr(159, "feat(board): interaction polish and command shortcuts", 5*time.Minute, gh.CISuccess),
			pr(158, "feat(board): replace manual review keys with command shortcuts", 3*time.Hour, gh.CIPending),
			pr(73, "Test: automatic posting with multiple PRs", 41*24*time.Hour, gh.CISuccess),
			pr(72, "Test: juggle review results across PRs", 42*24*time.Hour, gh.CIFailure),
		}},
		{View: cfg.Views[1], PRs: []gh.PullRequest{
			{Repository: "acme/monorepo", Number: 999, Title: "Migrate CI pipelines to reusable workflows", URL: "https://github.com/acme/monorepo/pull/999", Author: "lin", UpdatedAt: now.Add(-20 * time.Minute), CI: gh.CISuccess},
			{Repository: "acme/design-system", Number: 87, Title: "Keep dark mode tokens in sync", URL: "https://github.com/acme/design-system/pull/87", Author: "sam", UpdatedAt: now.Add(-10 * time.Minute), CI: gh.CIError},
		}},
		{View: cfg.Views[2]},
	}
	model.reviewRows = map[string]reviewOverviewRow{}
	summaries := []reviewRowSummary{
		{state: "completed", detail: "Completed locally · ● P2 1", posted: "PR Board", postedDetail: "PR Board · current revision", findings: reviewmemory.SeverityCounts{0, 0, 1, 0}},
		{state: "running", detail: "Running", posted: noneMark.text, postedDetail: "No submitted reviews"},
		{state: "completed", detail: "Completed locally · ✓ clean", posted: "GitHub", postedDetail: "GitHub · current revision"},
		{state: "blocked", detail: "blocked · explicit retry required", posted: noneMark.text, postedDetail: "No submitted reviews"},
	}
	for i, pr := range model.views[0].PRs {
		model.reviewRows[pr.URL] = reviewOverviewRow{identity: dispatch.Identity(pr), summary: summaries[i]}
	}
	model.active = 0
	model.loading = false
	// The region shows the selected first row's completed review and its finding.
	model = model.WithReviews(context.Background(), &reviewFake{})
	reviewed := model.views[0].PRs[0]
	run := reviewmemory.Run{ID: "0209edd94356f594934f74fc81b64635", Reviewer: "codex", StartedAt: now.Add(-4 * time.Minute), Identity: dispatch.Identity(reviewed), Outcome: reviewmemory.Outcome{Status: reviewmemory.Completed,
		Message: "Standards: one documentation gap. Specification: all shortcut requirements met.\nNot run: full completion gates, live posting.",
		Findings: []reviewmemory.Finding{
			{Severity: "P2", Title: "Help overlay omits the new command shortcuts", Path: "internal/board/help.go", Line: 42, Body: "The overlay lists the removed manual review keys, so users cannot find the configured shortcuts."},
		}}}
	model.region = &reviewRegion{pr: reviewed}
	model.overview.monitor = monitor.Status{State: monitor.Running, ObservationOK: true}
	model.setRegionData(func(data *regionData) {
		data.runs = []reviewmemory.Run{run}
		data.publications = []publication.Attempt{{RunID: run.ID, Action: config.PublishComment, Status: publication.Published}}
	})
	model.cfg.Repositories = []config.Repository{{Name: reviewed.Repository, Reviewer: "codex", AutoLaunch: true, AutoPublish: config.PublishComment}}
	model.cfg.Review.AutoViews = []string{cfg.Views[0].ID}
	model.cursor = 0
	model.rates.Search = gh.RateResource{Limit: 30, Remaining: 27}
	model.rates.GraphQL = gh.RateResource{Limit: 5000, Remaining: 4823}
	model.views[0].UpdatedAt = now.Add(-5 * time.Minute)
	model.views[1].UpdatedAt = now.Add(-5 * time.Minute)
	model.views[2].UpdatedAt = now.Add(-5 * time.Minute)
	model.width, model.height = width, height
	return model
}
