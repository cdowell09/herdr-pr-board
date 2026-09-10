package board

import (
	"fmt"
	"os"
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

type reviewRowSummary struct {
	state, detail, posted, postedDetail string
	findings                            reviewmemory.SeverityCounts // set for a completed review
}

type reviewOverviewRow struct {
	identity reviewmemory.Identity
	summary  reviewRowSummary
}

type reviewOverviewMsg struct {
	epoch uint64
	rows  map[string]reviewOverviewRow
	read  overviewRead
	// cfg is the configuration the read used; zero when it failed to load.
	cfg config.Config
}

// overviewRead is the shared part of one local read. The board keeps the
// latest one, so binding the region to another PR is a lookup, not a read.
type overviewRead struct {
	regions map[string]regionData
	// runs holds every recorded run by ID, so pending stops settle even for
	// a PR that no view lists.
	runs           map[string]reviewmemory.Run
	monitor        monitor.Status
	monitorCommand monitorInvocation
	err            error
}

// regionData is one PR's local review state from an overview read. err is
// that PR's own publication read failure, so one corrupt record never blanks
// another PR.
type regionData struct {
	runs         []reviewmemory.Run
	publications []publication.Attempt
	automatic    dispatch.Decision
	slotBusy     bool
	err          error
}

type reviewOverviewTickMsg struct{}

func reviewOverviewTick() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return reviewOverviewTickMsg{} })
}

// The tick asks for a read each second. Only one local read runs at a time,
// so results never overlap or arrive out of order.
func (m Model) updateReviewOverview(message tea.Msg) (Model, tea.Cmd, bool) {
	switch msg := message.(type) {
	case reviewOverviewMsg:
		m.overviewReading = false
		if msg.epoch == m.epoch {
			m.reviewRows, m.overview = msg.rows, msg.read
			if msg.cfg.Views != nil {
				m.cfg.Reviewers, m.cfg.Repositories, m.cfg.Review.AutoViews = msg.cfg.Reviewers, msg.cfg.Repositories, msg.cfg.Review.AutoViews
			}
			m.settleJobs()
			m.settleStops()
			m.clampRegionOffset()
			// The region can add a footer line, so clamp after the read lands.
			m.clampCursor()
		}
		var cmd tea.Cmd
		if m.overviewQueued {
			m.overviewQueued = false
			cmd = m.requestOverview()
		}
		return m, cmd, true
	case reviewOverviewTickMsg:
		cmd := m.requestOverview()
		return m, tea.Batch(reviewOverviewTick(), cmd), true
	}
	return m, nil, false
}

// requestOverview starts a local read unless one is in flight. A request
// during a read runs once more when that read returns, so key repeat never
// queues more than one extra read.
func (m *Model) requestOverview() tea.Cmd {
	if m.overviewReading {
		m.overviewQueued = true
		return nil
	}
	m.overviewReading = true
	return m.reviewOverviewCmd()
}

// settleJobs marks a queued manual review as running once the latest read
// records its run, for every PR with a request in flight.
func (m *Model) settleJobs() {
	for url := range m.reviewJobs {
		for _, run := range m.overview.regions[strings.ToLower(url)].runs {
			if run.Status == reviewmemory.Running {
				m.reviewJobs[url] = "running"
			}
		}
	}
}

// The overview reads each local history once, independently of GitHub
// refreshes, and returns every listed PR's region data from the same read.
// Every read goes through requestOverview.
func (m Model) reviewOverviewCmd() tea.Cmd {
	prs := map[string]gh.PullRequest{}
	for _, view := range m.views {
		for _, pr := range view.PRs {
			if previous, ok := prs[pr.URL]; !ok || pr.MetadataObservedAt.After(previous.MetadataObservedAt) {
				prs[pr.URL] = pr
			}
		}
	}
	if m.region != nil {
		// The pinned PR stays in the read even after it leaves every view.
		if _, listed := prs[m.region.pr.URL]; !listed {
			prs[m.region.pr.URL] = m.region.pr
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
		read := overviewRead{regions: make(map[string]regionData, len(prs))}
		if configErr == nil && state != "" {
			read.monitor = monitor.Inspect(state, latest)
			binary, err := os.Executable()
			if err == nil {
				read.monitorCommand, err = monitorCommand(binary, path, state)
			}
			if err != nil {
				read.monitor.Message += "; monitor command unavailable: " + err.Error()
			}
		}
		if configErr != nil {
			read.monitor = monitor.Status{State: monitor.Unknown, Message: configErr.Error()}
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
		read.runs = make(map[string]reviewmemory.Run, len(snapshot.Runs))
		for _, run := range snapshot.Runs {
			key := strings.ToLower(fmt.Sprintf("https://github.com/%s/pull/%d", run.Identity.Repository, run.Identity.Number))
			runs[key] = append(runs[key], run)
			read.runs[run.ID] = run
		}
		read.err = stateErr
		rows := make(map[string]reviewOverviewRow, len(prs))
		for url, pr := range prs {
			key := strings.ToLower(url)
			// Publication records are read per PR, so one PR's corrupt record
			// leaves every other PR's records intact.
			var attempts []publication.Attempt
			publicationErr := stateErr
			if publisher != nil && stateErr == nil {
				attempts, publicationErr = publisher.HistoryForRuns(runs[key])
			}
			summary := localReviewSummary(pr, runs[key], snapshot.Active, decisions[url], latest, read.monitor)
			if stateErr != nil {
				summary.state, summary.detail = "unknown", "Local review state unavailable"
			} else if reviews == nil {
				summary.state, summary.detail = "none", "Local reviews are not configured"
			} else if configErr != nil && summary.state == "none" {
				summary.state, summary.detail = "unknown", "Review configuration unavailable"
			}
			summary.posted, summary.postedDetail = postedReviewSummary(pr, attempts, publicationErr)
			rows[url] = reviewOverviewRow{dispatch.Identity(pr), summary}
			data := regionData{runs: runs[key], publications: attempts, err: publicationErr}
			switch {
			case configErr != nil:
				data.automatic = dispatch.Decision{URL: url, Reason: configErr.Error()}
			case stateErr == nil:
				decision, ok := decisions[url]
				if !ok {
					decision = dispatch.Decision{URL: url, Reason: "waiting for a full observation"}
				}
				data.automatic = decision
				// The same claims and limit that mark rows as waiting.
				data.slotBusy = decision.Eligible && len(snapshot.Active) >= max(1, latest.Review.MaxConcurrency)
			}
			read.regions[key] = data
		}
		msg := reviewOverviewMsg{epoch: epoch, rows: rows, read: read}
		if configErr == nil {
			msg.cfg = latest
		}
		return msg
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
			summary.findings = reviewmemory.CountSeverities(latest.Findings)
			summary.detail = "Completed locally · " + findingsDetail(summary.findings)
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
