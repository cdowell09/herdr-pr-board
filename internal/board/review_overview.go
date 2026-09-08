package board

import (
	"fmt"
	"strings"
	"time"

	"github.com/cdowell09/herdr-pr-board/internal/config"
	"github.com/cdowell09/herdr-pr-board/internal/dispatch"
	gh "github.com/cdowell09/herdr-pr-board/internal/github"
	"github.com/cdowell09/herdr-pr-board/internal/monitor"
	"github.com/cdowell09/herdr-pr-board/internal/publication"
	"github.com/cdowell09/herdr-pr-board/internal/reviewmemory"
	tea "github.com/charmbracelet/bubbletea"
)

type reviewRowSummary struct{ state, detail, posted, postedDetail string }

type reviewOverviewRow struct {
	identity reviewmemory.Identity
	summary  reviewRowSummary
}

type reviewOverviewMsg struct {
	epoch uint64
	rows  map[string]reviewOverviewRow
}

type reviewOverviewTickMsg struct{}

func reviewOverviewTick() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return reviewOverviewTickMsg{} })
}

// Only one local read runs at a time. Its completion schedules the next tick.
func (m Model) updateReviewOverview(message tea.Msg) (Model, tea.Cmd, bool) {
	switch msg := message.(type) {
	case reviewOverviewMsg:
		if msg.epoch == m.epoch {
			m.reviewRows = msg.rows
			m.clampCursor()
		}
		return m, reviewOverviewTick(), true
	case reviewOverviewTickMsg:
		if m.reviewPanel != nil {
			return m, reviewOverviewTick(), true
		}
		return m, m.reviewOverviewCmd(), true
	}
	return m, nil, false
}

// The overview reads each local history once, independently of GitHub refreshes.
func (m Model) reviewOverviewCmd() tea.Cmd {
	prs := map[string]gh.PullRequest{}
	wanted := map[string]bool{}
	for _, view := range m.views {
		for _, pr := range view.PRs {
			wanted[strings.ToLower(pr.URL)] = true
			if previous, ok := prs[pr.URL]; !ok || pr.MetadataObservedAt.After(previous.MetadataObservedAt) {
				prs[pr.URL] = pr
			}
		}
	}
	candidates := append([]dispatch.Candidate(nil), m.autoCandidates...)
	cfg, path, state, epoch := m.cfg, m.configPath, m.stateDir, m.epoch
	reviews, publisher := m.reviews, m.publications
	return func() tea.Msg {
		var snapshot reviewmemory.Snapshot
		var stateErr error
		if reviews != nil {
			snapshot, stateErr = reviews.Snapshot()
		}
		latest := cfg
		var configErr error
		if path != "" {
			latest, configErr = config.LoadExisting(path)
		}
		status := monitor.Status{}
		if configErr == nil && state != "" {
			status = monitor.Inspect(state, latest)
		}
		if !config.SameDiscovery(cfg, latest) {
			for i := range candidates {
				candidates[i].Observed = false
			}
		}
		decisions := map[string]dispatch.Decision{}
		if configErr == nil && stateErr == nil && reviews != nil {
			for _, decision := range dispatch.Decisions(candidates, latest, snapshot) {
				decisions[decision.URL] = decision
			}
		}
		runs := map[string][]reviewmemory.Run{}
		var relevant []reviewmemory.Run
		for _, run := range snapshot.Runs {
			url := fmt.Sprintf("https://github.com/%s/pull/%d", run.Identity.Repository, run.Identity.Number)
			key := strings.ToLower(url)
			runs[key] = append(runs[key], run)
			if wanted[key] {
				relevant = append(relevant, run)
			}
		}
		var attempts []publication.Attempt
		publicationErr := stateErr
		if publisher != nil && publicationErr == nil {
			attempts, publicationErr = publisher.HistoryForRuns(relevant)
		}
		posts := map[string][]publication.Attempt{}
		for _, attempt := range attempts {
			key := fmt.Sprintf("https://github.com/%s/pull/%d", strings.ToLower(attempt.Identity.Repository), attempt.Identity.Number)
			posts[key] = append(posts[key], attempt)
		}
		rows := make(map[string]reviewOverviewRow, len(prs))
		for url, pr := range prs {
			key := strings.ToLower(url)
			summary := localReviewSummary(pr, runs[key], snapshot.Active, decisions[url], latest, status)
			if stateErr != nil {
				summary.state, summary.detail = "unknown", "Local review state unavailable"
			} else if reviews == nil {
				summary.state, summary.detail = "none", "Local reviews are not configured"
			} else if configErr != nil && summary.state == "none" {
				summary.state, summary.detail = "unknown", "Review configuration unavailable"
			}
			summary.posted, summary.postedDetail = postedReviewSummary(pr, posts[key], publicationErr)
			rows[url] = reviewOverviewRow{dispatch.Identity(pr), summary}
		}
		return reviewOverviewMsg{epoch, rows}
	}
}

func localReviewSummary(pr gh.PullRequest, runs []reviewmemory.Run, active map[string]bool, decision dispatch.Decision, cfg config.Config, status monitor.Status) reviewRowSummary {
	summary := reviewRowSummary{state: "none", detail: "No local review"}
	id := dispatch.Identity(pr)
	known := reviewmemory.ValidateIdentity(id) == nil && !pr.MetadataObservedAt.IsZero()
	var latest, running *reviewmemory.Run
	for i := range runs {
		run := &runs[i]
		if active[run.ID] && (running == nil || run.Identity == id || running.Identity != id) {
			running = run
		}
		if known && run.Identity == id {
			latest = run
		}
	}
	if running != nil {
		summary.state, summary.detail = "running", "Running"
		if !known {
			summary.detail += " · revision unknown"
		} else if running.Identity != id {
			summary.detail += " · older revision"
		}
		return summary
	}
	if !known {
		summary.state, summary.detail = "unknown", "Current revision unavailable"
		return summary
	}
	if latest != nil {
		summary.state = string(latest.Status)
		switch latest.Status {
		case reviewmemory.Completed:
			summary.detail = "Completed locally"
		case reviewmemory.Failed, reviewmemory.Blocked, reviewmemory.Abandoned:
			summary.detail = string(latest.Status) + " · explicit retry required"
		default:
			summary.state, summary.detail = "unknown", "Review state unavailable"
		}
		return summary
	}
	if !decision.Eligible {
		if decision.Reason != "" {
			summary.detail = decision.Reason
		}
		return summary
	}
	repo, _ := cfg.RepositoryFor(pr.Repository)
	if reason := automaticSetupWait(repo, cfg.Review.AutoViews, status); reason != "" {
		summary.detail = "Automatic reviews: " + reason
	} else if len(active) >= max(1, cfg.Review.MaxConcurrency) {
		summary.state, summary.detail = "waiting", "Waiting for review slot"
	} else {
		summary.state, summary.detail = "ready", "Eligible · awaiting monitor dispatch"
	}
	return summary
}

func (m Model) rowReviewSummary(pr gh.PullRequest) reviewRowSummary {
	summary := reviewRowSummary{state: "unknown", detail: "Loading review status", posted: "?", postedDetail: "Loading posted reviews"}
	if row, ok := m.reviewRows[pr.URL]; ok && row.identity == dispatch.Identity(pr) {
		summary = row.summary
	}
	if job := m.reviewJobs[pr.URL]; job != "" && summary.state != "running" && summary.state != "waiting" {
		summary.state, summary.detail = "queued", "Review request in progress"
	}
	return summary
}
