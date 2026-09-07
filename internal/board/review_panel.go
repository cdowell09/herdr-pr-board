package board

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/cdowell09/herdr-pr-board/internal/config"
	"github.com/cdowell09/herdr-pr-board/internal/dispatch"
	gh "github.com/cdowell09/herdr-pr-board/internal/github"
	"github.com/cdowell09/herdr-pr-board/internal/publication"
	"github.com/cdowell09/herdr-pr-board/internal/review"
	"github.com/cdowell09/herdr-pr-board/internal/reviewmemory"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

type ReviewBackend interface {
	ReviewStatus(reviewmemory.Identity) error
	History(string) ([]reviewmemory.Run, error)
	Review(context.Context, review.Request, func(string)) (reviewmemory.Run, error)
	RunDirectory(string) string
}

type reviewPanel struct {
	pr              gh.PullRequest
	automatic       dispatch.Decision
	runs            []reviewmemory.Run
	message         string
	offset          int
	setup           *repositorySetup
	settingsLoading bool
	publishing      bool
	publications    []publication.Attempt
}

type reviewHistoryMsg struct {
	automatic dispatch.Decision
	url       string
	runs      []reviewmemory.Run
	err       error
}
type reviewDoneMsg struct {
	url string
	run reviewmemory.Run
	err error
}
type reviewTickMsg struct {
	url        string
	generation uint64
}

func (m Model) WithReviews(ctx context.Context, backend ReviewBackend) Model {
	m.reviews, m.reviewContext = backend, ctx
	m.reviewJobs = map[string]string{}
	return m
}

func (m Model) openReviewPanel() (tea.Model, tea.Cmd) {
	pr, ok := m.selectedPR()
	if !ok {
		return m, nil
	}
	m.reviewGeneration++
	m.reviewPanel = &reviewPanel{pr: pr}
	if m.reviews == nil {
		m.reviewPanel.message = "Set HERDR_PLUGIN_STATE_DIR to an absolute path to use reviews."
		return m, nil
	}
	m.reviewPanel.settingsLoading = true
	return m, tea.Batch(m.reviewHistoryCmd(pr.URL), m.publicationHistoryCmd(pr.URL), m.repositorySettingsCmd(false), reviewTick(pr.URL, m.reviewGeneration))
}

func (m Model) reviewHistoryCmd(url string) tea.Cmd {
	m.autoCandidates = append([]dispatch.Candidate(nil), m.autoCandidates...)
	backend := m.reviews
	return func() tea.Msg {
		runs, err := backend.History(url)
		return reviewHistoryMsg{url: url, runs: runs, err: err, automatic: m.automaticDecision(url)}
	}
}

func reviewTick(url string, generation uint64) tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return reviewTickMsg{url, generation} })
}

func (m Model) updateReview(message tea.Msg) (Model, tea.Cmd, bool) {
	switch msg := message.(type) {
	case reviewHistoryMsg:
		if m.reviewPanel == nil || m.reviewPanel.pr.URL != msg.url {
			return m, nil, true
		}
		m.reviewPanel.runs = msg.runs
		m.reviewPanel.automatic = msg.automatic
		if msg.err != nil {
			m.reviewPanel.message = msg.err.Error()
		}
		for _, run := range msg.runs {
			if run.Status == reviewmemory.Running && m.reviewJobs[msg.url] != "" {
				m.reviewJobs[msg.url] = "running"
			}
		}
		m.clampReviewOffset()
		return m, nil, true
	case reviewTickMsg:
		if m.reviewPanel != nil && m.reviewPanel.pr.URL == msg.url && msg.generation == m.reviewGeneration && m.reviews != nil {
			return m, tea.Batch(m.reviewHistoryCmd(msg.url), m.publicationHistoryCmd(msg.url), reviewTick(msg.url, msg.generation)), true
		}
		return m, nil, true
	case reviewDoneMsg:
		delete(m.reviewJobs, msg.url)
		message := string(msg.run.Status)
		if msg.err != nil {
			message = msg.err.Error()
		}
		m.warning = "review: " + message
		if m.reviewPanel != nil && m.reviewPanel.pr.URL == msg.url {
			m.reviewPanel.message = message
			m.clampReviewOffset()
			return m, m.reviewHistoryCmd(msg.url), true
		}
		return m, nil, true
	}
	return m, nil, false
}

func (m Model) updateReviewKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.clampReviewOffset()
	if m.reviewPanel.setup != nil {
		return m.updateRepositoryKey(key)
	}
	switch key.String() {
	case "esc", "v":
		m.reviewPanel = nil
	case "q", "ctrl+c":
		return m, tea.Quit
	case "j", "down":
		m.reviewPanel.offset++
	case "k", "up":
		m.reviewPanel.offset = max(0, m.reviewPanel.offset-1)
	case "g", "home":
		m.reviewPanel.offset = 0
	case "G", "end":
		m.reviewPanel.offset = len(m.reviewLines())
	case "o":
		return m, m.openBrowser(m.reviewPanel.pr.URL)
	case "s":
		m.reviewPanel.settingsLoading = true
		return m, m.repositorySettingsCmd(true)
	case "c":
		return m.publishCmd(config.PublishComment)
	case "a":
		return m.publishCmd(config.PublishApprove)
	case "x":
		return m.publishCmd(config.PublishRequestChanges)
	case "n", "N":
		if m.reviewPanel.settingsLoading {
			return m, nil
		}
		if _, exists := m.cfg.RepositoryFor(m.reviewPanel.pr.Repository); !exists {
			m.reviewPanel.settingsLoading = true
			return m, m.repositorySettingsCmd(true)
		}
		if m.reviews == nil {
			return m, nil
		}
		url := m.reviewPanel.pr.URL
		if m.reviewJobs[url] != "" {
			m.reviewPanel.message = "A review request is already queued or running."
			m.clampReviewOffset()
			return m, nil
		}
		m.reviewJobs[url] = "queued"
		m.reviewPanel.message = ""
		m.clampReviewOffset()
		backend, ctx, rerun := m.reviews, m.reviewContext, key.String() == "N"
		return m, func() tea.Msg {
			run, err := backend.Review(ctx, review.Request{URL: url, Rerun: rerun}, nil)
			return reviewDoneMsg{url, run, err}
		}
	}
	m.clampReviewOffset()
	return m, nil
}

func (m Model) updateReviewMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	m.clampReviewOffset()
	if m.reviewPanel.setup != nil {
		return m.updateRepositoryMouse(msg)
	}
	event := tea.MouseEvent(msg)
	switch event.Button {
	case tea.MouseButtonWheelUp:
		m.reviewPanel.offset = max(0, m.reviewPanel.offset-mouseStep)
	case tea.MouseButtonWheelDown:
		m.reviewPanel.offset += mouseStep
	case tea.MouseButtonLeft:
		if event.Action == tea.MouseActionPress && event.Y == 1 {
			return m, m.openBrowser(m.reviewPanel.pr.URL)
		}
	}
	m.clampReviewOffset()
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
			if pr.URL == m.reviewPanel.pr.URL && !pr.MetadataObservedAt.IsZero() && (current.MetadataObservedAt.IsZero() || pr.MetadataObservedAt.After(current.MetadataObservedAt)) {
				current = pr
			}
		}
	}
	return current
}

func (m Model) reviewLines() []string {
	p := m.reviewPanel
	current := m.reviewObservation()
	lines := []string{}
	add := func(value string) {
		lines = append(lines, strings.Split(ansi.Wrap(reviewText(value), max(1, m.width), ""), "\n")...)
	}
	for _, line := range m.publicationLines() {
		add(line)
	}
	if p.automatic.Reason != "" {
		add("Automatic (latest full observation): " + p.automatic.Reason)
	}
	if status := m.reviewJobs[p.pr.URL]; status != "" {
		add("Request: " + status)
	}
	if p.message != "" {
		add(p.message)
	}
	if len(p.runs) == 0 {
		add("No local review runs.")
	}
	for i := len(p.runs) - 1; i >= 0; i-- {
		run := p.runs[i]
		comparison := "older revision"
		if current.HeadOID == "" || current.BaseRefName == "" {
			comparison = "current revision unknown"
		} else if run.Identity.HeadOID == current.HeadOID && run.Identity.BaseRefName == current.BaseRefName {
			comparison = "current observed revision"
		}
		add(fmt.Sprintf("%s · %s · %s", run.Status, run.Reviewer, comparison))
		add(fmt.Sprintf("Head %s → %s · %s", run.Identity.HeadOID, run.Identity.BaseRefName, run.StartedAt.Format(time.RFC3339)))
		if run.Message != "" {
			add(run.Message)
		}
		for _, finding := range run.Findings {
			add(finding.Severity + " " + finding.Title)
			if finding.Path != "" {
				add(fmt.Sprintf("%s:%d", finding.Path, finding.Line))
			}
			add(finding.Body)
		}
		if m.reviews != nil {
			add("Diagnostics: " + m.reviews.RunDirectory(run.ID))
		}
		add("")
	}
	return lines
}

func (m Model) reviewViewport() ([]string, int) {
	help := strings.Split(ansi.Wrap("n run · N explicit rerun · s settings · c comment · a approve · x request changes · j/k scroll · o open · Esc back · q quit", max(1, m.width), ""), "\n")
	return help, max(1, m.height-3-len(help))
}
func (m *Model) clampReviewOffset() {
	if m.reviewPanel == nil {
		return
	}
	_, visible := m.reviewViewport()
	m.reviewPanel.offset = max(0, min(m.reviewPanel.offset, max(0, len(m.reviewLines())-visible)))
}

func (m Model) renderReviewPanel() string {
	if m.reviewPanel.setup != nil {
		return m.renderRepositoryPanel()
	}
	lines := m.reviewLines()
	help, visible := m.reviewViewport()
	offset := max(0, min(m.reviewPanel.offset, max(0, len(lines)-visible)))
	body := append([]string{titleStyle.Render("Local reviews"), urlStyle.Render(truncate(m.reviewPanel.pr.URL, m.width)), ""}, lines[offset:min(len(lines), offset+visible)]...)
	return strings.Join(append(body, help...), "\n")
}
