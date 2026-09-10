package board

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/cdowell09/herdr-pr-board/internal/dispatch"
	gh "github.com/cdowell09/herdr-pr-board/internal/github"
	"github.com/cdowell09/herdr-pr-board/internal/monitor"
	"github.com/cdowell09/herdr-pr-board/internal/publication"
	"github.com/cdowell09/herdr-pr-board/internal/reviewmemory"
)

func TestLocalReviewOverviewStates(t *testing.T) {
	m := autoPanel(t)
	pr := m.autoCandidates[0].PR
	id := dispatch.Identity(pr)
	cfg := m.cfg
	cfg.Review.MaxConcurrency = 1
	ready := dispatch.Decision{Eligible: true, Reason: dispatch.Ready}
	monitorReady := monitor.Status{State: monitor.Running, ObservationOK: true}
	run := func(status reviewmemory.Status) reviewmemory.Run {
		return reviewmemory.Run{ID: "run", Identity: id, Outcome: reviewmemory.Outcome{Status: status}}
	}
	withFindings := func(run reviewmemory.Run, severities ...string) reviewmemory.Run {
		for _, severity := range severities {
			run.Findings = append(run.Findings, reviewmemory.Finding{Severity: severity, Title: "t", Body: "b"})
		}
		return run
	}
	for _, tc := range []struct {
		name          string
		runs          []reviewmemory.Run
		active        map[string]bool
		decision      dispatch.Decision
		monitor       monitor.Status
		state, detail string
	}{
		{name: "idle", state: "none", detail: "No local review"},
		{name: "ready", decision: ready, monitor: monitorReady, state: "ready", detail: "Eligible · awaiting monitor dispatch"},
		{name: "full", decision: ready, monitor: monitorReady, active: map[string]bool{"another-pr": true}, state: "waiting", detail: "Waiting for review slot"},
		{name: "stopped", decision: ready, monitor: monitor.Status{State: monitor.Stopped}, active: map[string]bool{"another-pr": true}, state: "none", detail: "Automatic reviews: start the monitor"},
		{name: "running", runs: []reviewmemory.Run{run(reviewmemory.Running)}, active: map[string]bool{"run": true}, state: "running", detail: "Running"},
		{name: "completed", runs: []reviewmemory.Run{run(reviewmemory.Completed)}, state: "completed", detail: "Completed locally · no findings"},
		{name: "completed with findings", runs: []reviewmemory.Run{withFindings(run(reviewmemory.Completed), "P1", "P0", "P3", "P3")}, state: "completed", detail: "Completed locally · P0:1 P1:1 P2:0 P3:2"},
		{name: "failed", runs: []reviewmemory.Run{run(reviewmemory.Failed)}, state: "failed", detail: "failed · explicit retry required"},
		{name: "blocked", runs: []reviewmemory.Run{run(reviewmemory.Blocked)}, state: "blocked", detail: "blocked · explicit retry required"},
		{name: "abandoned", runs: []reviewmemory.Run{run(reviewmemory.Abandoned)}, state: "abandoned", detail: "abandoned · explicit retry required"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := localReviewSummary(pr, tc.runs, tc.active, tc.decision, cfg, tc.monitor)
			if got.state != tc.state || got.detail != tc.detail {
				t.Fatalf("%+v", got)
			}
		})
	}
	old := run(reviewmemory.Completed)
	old.Identity.HeadOID = strings.Repeat("c", 40)
	if got := localReviewSummary(pr, []reviewmemory.Run{old}, nil, ready, cfg, monitorReady); got.state != "ready" {
		t.Fatalf("older review completed current head: %+v", got)
	}
	old.Status = reviewmemory.Running
	old.ID = "old"
	current := run(reviewmemory.Running)
	if got := localReviewSummary(pr, []reviewmemory.Run{current, old}, map[string]bool{"run": true, "old": true}, ready, cfg, monitorReady); got.detail != "Running" {
		t.Fatalf("older active review hid current: %+v", got)
	}
	if got := localReviewSummary(pr, []reviewmemory.Run{old}, map[string]bool{"old": true}, ready, cfg, monitorReady); got.detail != "Running · older revision" {
		t.Fatalf("older active revision not identified: %+v", got)
	}
	pr.HeadOID = ""
	if got := localReviewSummary(pr, []reviewmemory.Run{current}, nil, ready, cfg, monitorReady); got.state != "unknown" {
		t.Fatalf("missing revision treated as reviewed: %+v", got)
	}
}

type overviewReviewBackend struct {
	reviewFake
	snapshot reviewmemory.Snapshot
	err      error
	reads    int
}

func (b *overviewReviewBackend) Snapshot() (reviewmemory.Snapshot, error) {
	b.reads++
	return b.snapshot, b.err
}
func (*overviewReviewBackend) History(string) ([]reviewmemory.Run, error) {
	panic("overview must not read each PR history")
}
func (*overviewReviewBackend) ReviewStatus(reviewmemory.Identity) error {
	panic("overview must use one shared snapshot")
}

type overviewPublicationBackend struct {
	publicationFake
	attempts    []publication.Attempt
	err         error
	reads, runs int
}

func (b *overviewPublicationBackend) HistoryForRuns(runs []reviewmemory.Run) ([]publication.Attempt, error) {
	b.reads++
	b.runs = len(runs)
	return b.attempts, b.err
}

func TestOverviewRefreshesAllRowsWithoutOpeningReviewPanel(t *testing.T) {
	m := autoPanel(t)
	m.reviewPanel = nil
	pr := m.autoCandidates[0].PR
	pr.ViewerReviews = &gh.ReviewObservation{Actor: "ada", Complete: true, ObservedAt: time.Now(), Reviews: []gh.SubmittedReview{{ID: 7, HeadOID: pr.HeadOID, State: "COMMENTED", SubmittedAt: time.Now()}}}
	m.views[0].PRs = []gh.PullRequest{pr}
	m.views[1].PRs = []gh.PullRequest{pr} // Same PR in two views must be read once.
	run := reviewmemory.Run{ID: "local", Identity: dispatch.Identity(pr), Outcome: reviewmemory.Outcome{Status: reviewmemory.Completed}}
	backend := &overviewReviewBackend{snapshot: reviewmemory.Snapshot{Runs: []reviewmemory.Run{run}, Active: map[string]bool{}}}
	publisher := &overviewPublicationBackend{attempts: []publication.Attempt{{RunID: run.ID, Identity: run.Identity, Actor: "ada", GitHubID: 7, Status: publication.Published}}}
	m = m.WithReviews(context.Background(), backend).WithPublications("", publisher)
	next, cmd := m.Update(m.reviewOverviewCmd()())
	m = next.(Model)
	if cmd == nil || backend.reads != 1 || publisher.reads != 1 || publisher.runs != 1 {
		t.Fatalf("unbatched or stopped polling: %d %d %d", backend.reads, publisher.reads, publisher.runs)
	}
	if got := m.rowReviewSummary(pr); got.state != "completed" || got.posted != "PR Board" {
		t.Fatalf("%+v", got)
	}
	if view := stripANSI(m.View()); !strings.Contains(view, "REVIEW") || !strings.Contains(view, "PR Board") || !strings.Contains(view, "Completed locally") {
		t.Fatalf("overview hidden: %s", view)
	}
	backend.snapshot.Active = map[string]bool{"local": true}
	backend.snapshot.Runs[0].Status = reviewmemory.Running
	_, cmd, _ = m.updateReviewOverview(reviewOverviewTickMsg{})
	next, _ = m.Update(cmd())
	m = next.(Model)
	if got := m.rowReviewSummary(pr); got.state != "running" {
		t.Fatalf("external progress not refreshed: %+v", got)
	}
	backend.err = errors.New("cannot read history")
	next, _ = m.Update(m.reviewOverviewCmd()())
	m = next.(Model)
	if got := m.rowReviewSummary(pr); got.state != "unknown" || got.posted != "?" {
		t.Fatalf("failure preserved false status: %+v", got)
	}
}

func TestOverviewDropsOldEpochAndRevisionAndCapturesCandidates(t *testing.T) {
	m := autoPanel(t)
	m.reviewPanel = nil
	pr := m.autoCandidates[0].PR
	command := m.reviewOverviewCmd()
	m.autoCandidates[0].Observed = false
	message := command().(reviewOverviewMsg)
	if got := message.rows[pr.URL].summary.detail; got == dispatch.ObservationFailed {
		t.Fatal("command shared mutable candidates")
	}
	m.reviewRows = message.rows
	m.epoch++
	next, _, handled := m.updateReviewOverview(reviewOverviewMsg{epoch: m.epoch - 1, rows: map[string]reviewOverviewRow{}})
	if !handled || len(next.reviewRows) != len(message.rows) {
		t.Fatal("old epoch replaced current rows")
	}
	pr.HeadOID = strings.Repeat("d", 40)
	if got := m.rowReviewSummary(pr); got.state != "unknown" || got.posted != "?" {
		t.Fatalf("old result applied to new revision: %+v", got)
	}
	m.reviewJobs[pr.URL] = "queued"
	if got := m.rowReviewSummary(pr); got.state != "queued" {
		t.Fatalf("manual request invisible: %+v", got)
	}
}

func TestOverviewKeepsSelectionVisibleWhenDetailGrows(t *testing.T) {
	m := layoutModel(t, 40)
	m.height = 20
	pr := m.views[0].PRs[0]
	m.views[0].PRs = nil
	m.reviewRows = map[string]reviewOverviewRow{}
	for number := 1; number <= 30; number++ {
		row := pr
		row.Number = number
		row.URL = fmt.Sprintf("https://github.com/acme/repo/pull/%d", number)
		m.views[0].PRs = append(m.views[0].PRs, row)
		m.reviewRows[row.URL] = reviewOverviewRow{identity: dispatch.Identity(row), summary: reviewRowSummary{state: "none", detail: "None", posted: "–", postedDetail: "None"}}
	}
	m.cursor = m.boardLayout().visibleRows - 1
	m.clampCursor()
	selected, _ := m.selectedPR()
	rows := make(map[string]reviewOverviewRow, len(m.reviewRows))
	for url, row := range m.reviewRows {
		rows[url] = row
	}
	row := rows[selected.URL]
	row.summary.postedDetail = "GitHub · current revision; PR Board · older revision"
	rows[selected.URL] = row
	next, _ := m.Update(reviewOverviewMsg{epoch: m.epoch, rows: rows})
	m = next.(Model)
	if m.cursor-m.offset >= m.boardLayout().visibleRows {
		t.Fatalf("selected row hidden after async detail expanded: cursor=%d offset=%d rows=%d", m.cursor, m.offset, m.boardLayout().visibleRows)
	}
}
