package board

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cdowell09/herdr-pr-board/internal/publication"
	"github.com/cdowell09/herdr-pr-board/internal/reviewmemory"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

var reviewSecondaryStyle = dimStyle.Foreground(lipgloss.Color("246"))

func (m Model) reviewLines() []string {
	p := m.reviewPanel
	var lines []string
	add := func(value string, style lipgloss.Style) {
		for _, line := range strings.Split(ansi.Wrap(reviewText(value), max(1, m.width), ""), "\n") {
			lines = append(lines, style.Render(line))
		}
	}
	section := func(title string) {
		if len(lines) > 0 && m.height >= 20 {
			add("", reviewSecondaryStyle)
		}
		add(title, keyStyle)
	}
	body := keyStyle.Bold(false)
	if status := m.reviewJobs[p.pr.URL]; status != "" {
		add("Request: "+status, keyStyle)
	}
	if p.message != "" {
		add("Status: "+p.message, warningStyle)
	}
	if m.monitorError != "" {
		add(m.monitorError, warningStyle)
	}
	current := m.reviewObservation()
	target, hasTarget := latestCompleted(p.runs)
	seen := map[string]bool{}
	publicationResult := func(a publication.Attempt) {
		style := keyStyle
		if a.Status != publication.Published {
			style = warningStyle
		}
		add("Publication "+string(a.Action)+": "+string(a.Status), style)
		if a.Message != "" {
			add(a.Message, warningStyle)
		}
	}
	for i := len(p.runs) - 1; i >= 0; i-- {
		run := p.runs[i]
		label := "Previous review"
		if i == len(p.runs)-1 {
			label = "Latest review"
		}
		section(label)
		statusStyle := keyStyle
		if run.Status == reviewmemory.Failed || run.Status == reviewmemory.Blocked || run.Status == reviewmemory.Abandoned {
			statusStyle = warningStyle.Bold(true)
		}
		add(string(run.Status)+" · "+run.Reviewer, statusStyle)
		comparison := "older revision"
		if current.HeadOID == "" || current.BaseRefName == "" {
			comparison = "current revision unknown"
		} else if run.Identity.HeadOID == current.HeadOID && run.Identity.BaseRefName == current.BaseRefName {
			comparison = "current observed revision"
		}
		if run.Message != "" {
			add(run.Message, body)
		}
		for _, f := range run.Findings {
			add(f.Severity+" "+f.Title, keyStyle)
			if f.Path != "" {
				add(fmt.Sprintf("%s:%d", f.Path, f.Line), reviewSecondaryStyle)
			}
			add(f.Body, body)
		}
		add(comparison, reviewSecondaryStyle)
		add(fmt.Sprintf("%s → %s · %s", shortRevision(run.Identity.HeadOID), run.Identity.BaseRefName, run.StartedAt.Local().Format("02 Jan 2006 15:04 MST")), reviewSecondaryStyle)
		if hasTarget && run.ID == target.ID {
			add("Publication target · latest completed review", reviewSecondaryStyle)
		}
		published := false
		for _, a := range p.publications {
			if a.RunID == run.ID {
				publicationResult(a)
				published = true
			}
		}
		if !published {
			add("Publication: no recorded posts", reviewSecondaryStyle)
		}
		seen[run.ID] = true
	}
	if len(p.runs) == 0 {
		section("Reviews")
		add("No local review runs.", body)
	}
	orphanHeading := false
	for _, a := range p.publications {
		if !seen[a.RunID] {
			if !orphanHeading {
				section("Other publication records")
				orphanHeading = true
			}
			publicationResult(a)
			add("Run "+a.RunID, reviewSecondaryStyle)
		}
	}
	section("Automation")
	state := string(p.monitor.State)
	if state == "" {
		state = "unknown"
	}
	add("Monitor: "+state, body)
	if p.automatic.Reason != "" {
		add("Latest full observation: "+p.automatic.Reason, body)
	}
	repo, _ := m.cfg.RepositoryFor(p.pr.Repository)
	if reason := automaticSetupWait(repo, m.cfg.Review.AutoViews, p.monitor); reason != "" {
		add("Waiting: "+reason, warningStyle)
	} else if p.capacityErr != nil {
		if errors.Is(p.capacityErr, reviewmemory.ErrCapacity) {
			add("Waiting for review slot", warningStyle)
		} else {
			add("Review capacity unavailable: "+p.capacityErr.Error(), warningStyle)
		}
	}
	if !repo.AutoLaunch {
		add("Automatic launches: off", reviewSecondaryStyle)
	} else {
		add("Automatic launches: on", reviewSecondaryStyle)
	}
	if repo.AutoPublish == "" {
		add("After review: keep local", reviewSecondaryStyle)
	} else {
		add("After review: "+string(repo.AutoPublish), reviewSecondaryStyle)
	}
	if p.monitor.Message != "" {
		add(p.monitor.Message, warningStyle)
	}
	if !p.monitor.ObservedAt.IsZero() {
		add("Latest monitor observation: "+p.monitor.ObservedAt.Format(time.RFC3339), reviewSecondaryStyle)
	}
	if command := m.monitorCommandLines(); len(command) > 0 {
		add("Run in another terminal:", body)
		lines = append(lines, command...)
	}
	if len(p.runs) > 0 || len(p.publications) > 0 {
		section("Details")
		for i := len(p.runs) - 1; i >= 0; i-- {
			run := p.runs[i]
			add("Run "+run.ID, keyStyle)
			add("Head "+run.Identity.HeadOID+" → "+run.Identity.BaseRefName, reviewSecondaryStyle)
			add("Started: "+run.StartedAt.Format(time.RFC3339), reviewSecondaryStyle)
			if m.reviews != nil {
				add("Diagnostics: "+m.reviews.RunDirectory(run.ID), reviewSecondaryStyle)
			}
		}
		for _, a := range p.publications {
			add("Publication "+string(a.Action)+": "+string(a.Status), keyStyle)
			add("Run "+a.RunID, reviewSecondaryStyle)
			if a.URL != "" {
				add(a.URL, reviewSecondaryStyle)
			}
		}
	}
	return lines
}

func shortRevision(value string) string { r := []rune(value); return string(r[:min(8, len(r))]) }

func (m Model) reviewViewport() ([]string, int) {
	text := "n run · N rerun · s settings · c comment · a approve · x changes · j/k scroll · o open · Esc back · q quit"
	help := strings.Split(ansi.Wrap(text, max(1, m.width), ""), "\n")
	if len(help) > max(1, m.height-4) {
		help = []string{truncate("Enlarge panel for controls.", m.width)}
	}
	return help, max(0, m.height-2-len(help))
}
func (m *Model) clampReviewOffset() {
	if m.reviewPanel == nil {
		return
	}
	if m.reviewPanel.setup != nil {
		m.clampRepositoryOffset()
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
	body := []string{titleStyle.Render(truncate("Local reviews", m.width)), urlStyle.Render(truncate(reviewText(m.reviewPanel.pr.URL), m.width))}
	body = append(body, lines[offset:min(len(lines), offset+visible)]...)
	for len(body) < 2+visible {
		body = append(body, "")
	}
	for _, line := range help {
		body = append(body, reviewSecondaryStyle.Render(line))
	}
	return strings.Join(body[:min(len(body), max(0, m.height))], "\n")
}
