package board

import (
	"context"
	"strings"
	"unicode"

	"github.com/cdowell09/herdr-pr-board/internal/config"
	gh "github.com/cdowell09/herdr-pr-board/internal/github"
	"github.com/cdowell09/herdr-pr-board/internal/monitor"
	"github.com/cdowell09/herdr-pr-board/internal/review"
	"github.com/cdowell09/herdr-pr-board/internal/reviewflow"
	"github.com/cdowell09/herdr-pr-board/internal/reviewmemory"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

type ReviewBackend interface {
	Stop(string) error
	Snapshot() (reviewmemory.Snapshot, error)
	Review(context.Context, review.Request, func(string)) (reviewmemory.Run, error)
	RunDirectory(string) string
}

const reviewsUnavailable = "Set HERDR_PLUGIN_STATE_DIR to an absolute path to use reviews."

// reviewRegion is the board's view of the selected PR's local reviews: the
// PR, the scroll position, the last action result, and the settings form.
// The review data itself stays in the latest read; regionState resolves it.
type reviewRegion struct {
	pr              gh.PullRequest
	message         string
	offset          int
	setup           *repositorySetup
	settingsLoading bool
	settingsRequest uint64 // the request whose response opens the form
}

// regionState is the region PR's local review state from the latest read.
// The region keeps no copy, so a refresh and a selection change never
// disagree.
type regionState struct {
	regionData
	monitor monitor.Status
	// readErr is the failure the region reports: the state read's, or this
	// PR's own publication read's. Every read replaces it.
	readErr error
	// loaded reports that a read has covered this PR. Until then the region
	// shows a loading state instead of claiming that no runs exist.
	loaded bool
}

func (m Model) regionState() regionState {
	data, loaded := m.overview.regions[strings.ToLower(m.region.pr.URL)]
	state := regionState{regionData: data, monitor: m.overview.monitor, readErr: m.overview.err, loaded: loaded}
	if state.readErr == nil {
		state.readErr = data.err
	}
	return state
}

// regionPinned reports that a background refresh must not swap the region's
// PR: zoom, the settings form, and every in-flight action target this PR.
// The selection follows the pinned PR instead. The user can still select
// another PR, which rebinds the region; pending actions stay keyed by PR on
// the model, so they finish and report against the right PR.
func (m Model) regionPinned() bool {
	r := m.region
	url := r.pr.URL
	return m.zoom || r.setup != nil || r.settingsLoading || m.publishing[url] || m.stopping[url] != ""
}

// userInput reports a key or mouse message, the only way the user changes
// the selection.
func userInput(message tea.Msg) bool {
	switch message.(type) {
	case tea.KeyMsg, tea.MouseMsg:
		return true
	}
	return false
}

type reviewDoneMsg struct {
	url          string
	run          reviewmemory.Run
	err          error
	notification error
}

func (m Model) WithReviews(ctx context.Context, backend ReviewBackend) Model {
	m.reviews, m.reviewContext = backend, ctx
	m.reviewJobs, m.publishing, m.stopping = map[string]string{}, map[string]bool{}, map[string]string{}
	return m
}

// WithNotifications sends a review notification when a manual review finishes.
// A nil notifier sends none. The first failed notification of a board session
// appears in the review region status line and the board footer.
func (m Model) WithNotifications(notifier reviewflow.Notifier) Model {
	m.notifier = notifier
	return m
}

// syncRegion keeps the region on the selected PR. A new selection binds from
// the latest read at once; the next tick refreshes it. A pinned region keeps
// its PR by URL across a refresh that reorders rows: the selection moves to
// the PR's new row and the PR's metadata follows the latest observation.
func (m Model) syncRegion(message tea.Msg, cmd tea.Cmd) (tea.Model, tea.Cmd) {
	if m.reviews == nil {
		return m, cmd
	}
	if m.region != nil && m.regionPinned() {
		if observed := m.reviewObservation(); observed.URL != "" {
			m.region.pr = observed
		}
		// Zoom and the form show the pinned PR themselves. The split shows the
		// selected row, so a pinned PR that left the view yields to it.
		if m.zoom || m.region.setup != nil {
			m.restoreSelection(m.region.pr.URL)
			return m, cmd
		}
		if !userInput(message) && m.restoreSelection(m.region.pr.URL) {
			return m, cmd
		}
	}
	pr, ok := m.selectedPR()
	if !ok {
		m.region = nil
		return m, cmd
	}
	if m.region != nil && m.region.pr.URL == pr.URL {
		if observed := m.reviewObservation(); observed.URL != "" {
			pr = observed
		}
		m.region.pr = pr
		return m, cmd
	}
	m.region = &reviewRegion{pr: pr}
	// The bound region can add a footer line, so the table clamps again.
	m.clampCursor()
	return m, cmd
}

func (m Model) updateReview(message tea.Msg) (Model, tea.Cmd, bool) {
	switch msg := message.(type) {
	case reviewStoppedMsg:
		return m.updateReviewStopped(msg)
	case reviewDoneMsg:
		delete(m.reviewJobs, msg.url)
		message := string(msg.run.Status)
		if msg.err != nil {
			message = msg.err.Error()
		}
		if msg.notification != nil && !m.notifyWarn {
			m.notifyWarn = true
			message += "; review notifications unavailable: " + msg.notification.Error()
		}
		m.warning = "review: " + message
		if m.region != nil && m.region.pr.URL == msg.url {
			m.region.message = message
			m.clampRegionOffset()
			cmd := m.requestOverview()
			return m, cmd, true
		}
		return m, nil, true
	}
	return m, nil, false
}

// updateReviewAction handles the keys that act on the selected PR's reviews.
// The board and the zoomed region share them.
func (m Model) updateReviewAction(key tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	switch key.String() {
	case "n", "N", "t", "s", "c", "a", "x":
	default:
		return m, nil, false
	}
	if m.region == nil {
		if m.reviews == nil {
			m.warning = appendWarning(m.warning, reviewsUnavailable)
		}
		return m, nil, true
	}
	r := m.region
	switch key.String() {
	case "s":
		cmd := m.repositorySettingsCmd()
		return m, cmd, true
	case "t":
		next, cmd := m.stopReviewCmd()
		return next, cmd, true
	case "c":
		next, cmd := m.publishCmd(config.PublishComment)
		return next, cmd, true
	case "a":
		next, cmd := m.publishCmd(config.PublishApprove)
		return next, cmd, true
	case "x":
		next, cmd := m.publishCmd(config.PublishRequestChanges)
		return next, cmd, true
	}
	if r.settingsLoading {
		return m, nil, true
	}
	if _, exists := m.cfg.RepositoryFor(r.pr.Repository); !exists {
		cmd := m.repositorySettingsCmd()
		return m, cmd, true
	}
	url := r.pr.URL
	if m.reviewJobs[url] != "" {
		r.message = "A review request is already queued or running."
		m.clampRegionOffset()
		return m, nil, true
	}
	m.reviewJobs[url] = "queued"
	r.message = ""
	m.clampRegionOffset()
	backend, publisher, notifier, ctx, rerun := m.reviews, m.publications, m.notifier, m.reviewContext, key.String() == "N"
	return m, func() tea.Msg {
		result, err := reviewflow.Run(ctx, backend, publisher, notifier, review.Request{URL: url, Rerun: rerun}, nil)
		return reviewDoneMsg{url: url, run: result.Run, err: err, notification: result.Notification}
	}, true
}

// zoomIn shows the region at full height. Without local reviews it explains
// what is missing instead.
func (m Model) zoomIn() Model {
	if m.region == nil {
		if m.reviews == nil {
			m.warning = appendWarning(m.warning, reviewsUnavailable)
		}
		return m
	}
	m.zoom = true
	m.clampRegionOffset()
	return m
}

// zoomOut returns to the split. The selection follows the zoomed PR, which a
// refresh may have moved to another row.
func (m Model) zoomOut() Model {
	m.zoom = false
	if m.region != nil {
		m.restoreSelection(m.region.pr.URL)
	}
	m.clampRegionOffset()
	return m
}

func (m Model) updateZoomKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	if next, cmd, handled := m.updateReviewAction(key); handled {
		return next, cmd
	}
	switch key.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "esc", "v":
		return m.zoomOut(), nil
	case "?":
		m.helpOverlay = true
	case "enter", "o":
		return m, m.openBrowser(m.region.pr.URL)
	default:
		m.scrollRegionKey(key.String())
	}
	return m, nil
}

// scrollRegionKey applies one scroll key to the region.
func (m *Model) scrollRegionKey(key string) {
	if m.region == nil {
		return
	}
	switch key {
	case "j", "down":
		m.scrollRegion(1)
	case "k", "up":
		m.scrollRegion(-1)
	case "g", "home":
		m.region.offset = 0
	case "G", "end":
		m.region.offset = len(m.regionLines(m.zoom))
	case "pgdown":
		m.scrollRegion(m.regionPage())
	case "pgup":
		m.scrollRegion(-m.regionPage())
	}
	m.clampRegionOffset()
}

// regionPage is one page of content lines. The viewport spends its last row
// on the marker while lines stay hidden, so a page is one line shorter.
func (m Model) regionPage() int { return max(1, m.regionSize()-1) }

func (m *Model) scrollRegion(delta int) {
	if m.region == nil {
		return
	}
	m.region.offset = max(0, m.region.offset+delta)
	m.clampRegionOffset()
}

func (m Model) updateZoomMouse(message tea.MouseMsg) (tea.Model, tea.Cmd) {
	event := tea.MouseEvent(message)
	switch event.Button {
	case tea.MouseButtonWheelUp:
		m.scrollRegion(-mouseStep)
	case tea.MouseButtonWheelDown:
		m.scrollRegion(mouseStep)
	case tea.MouseButtonLeft:
		if event.Action == tea.MouseActionPress && event.Y == 1 {
			return m, m.openBrowser(m.region.pr.URL)
		}
	}
	return m, nil
}

func reviewText(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, ansi.Strip(value))
}

// reviewObservation uses successful board observations without another GitHub request.
func (m Model) reviewObservation() gh.PullRequest {
	var current gh.PullRequest
	for _, view := range m.views {
		if view.Err != nil {
			continue
		}
		for _, pr := range view.PRs {
			if pr.URL == m.region.pr.URL && !pr.MetadataObservedAt.IsZero() && (current.MetadataObservedAt.IsZero() || pr.MetadataObservedAt.After(current.MetadataObservedAt)) {
				current = pr
			}
		}
	}
	return current
}
