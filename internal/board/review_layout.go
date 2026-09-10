package board

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	gh "github.com/cdowell09/herdr-pr-board/internal/github"
	"github.com/cdowell09/herdr-pr-board/internal/publication"
	"github.com/cdowell09/herdr-pr-board/internal/reviewmemory"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

var reviewSecondaryStyle = dimStyle.Foreground(lipgloss.Color("246"))

const (
	// regionSplitHeight is the terminal height that fits the table and a
	// review region. A shorter terminal shows the one-line summary instead.
	regionSplitHeight = 24
	// regionMinRows keeps the region readable when the table has many rows.
	regionMinRows = 6
)

// regionSplit reports whether the board shows the review region under the table.
func (m Model) regionSplit() bool { return m.region != nil && m.height >= regionSplitHeight }

// regionLines renders the selected PR's review state. The split shows the
// latest run and one line each for automation and details. Zoom shows every
// run and the full automation and details sections.
func (m Model) regionLines(full bool) []string {
	r, s := m.region, m.regionState()
	var lines []string
	add := func(value string, style lipgloss.Style) {
		for _, line := range strings.Split(ansi.Wrap(reviewText(value), max(1, m.width), ""), "\n") {
			lines = append(lines, style.Render(line))
		}
	}
	heading := func(text, suffix string, style lipgloss.Style) {
		if full && len(lines) > 0 && m.height >= 20 {
			add("", reviewSecondaryStyle)
		}
		lines = append(lines, rule(text, suffix, m.width, style))
	}
	body := keyStyle.Bold(false)
	if status := m.reviewJobs[r.pr.URL]; status != "" {
		add("Request: "+status, keyStyle)
	}
	if r.message != "" {
		add("Status: "+r.message, warningStyle)
	}
	if s.readErr != nil {
		add("Local reviews unavailable: "+s.readErr.Error(), warningStyle)
	}
	if m.monitorError != "" {
		add(m.monitorError, warningStyle)
	}
	// The posted summary belongs to the PR, not to a run, so it leads the
	// region and stays visible above the findings. The row summary checks the
	// revision identity, so a read against an older head never labels an old
	// post as current.
	add("Posted: "+m.rowReviewSummary(r.pr).postedDetail, reviewSecondaryStyle)
	current := m.reviewObservation()
	target, hasTarget := latestCompleted(s.runs)
	stopTarget, _ := m.stopTarget()
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
	// Zoom lists every run. The split lists the newest run and, when a newer
	// run did not complete, the completion that publication would target.
	for i := len(s.runs) - 1; i >= 0; i-- {
		run := s.runs[i]
		if !full && i != len(s.runs)-1 && !(hasTarget && run.ID == target.ID) {
			continue
		}
		label, suffix := "Review", fmt.Sprintf("run %d/%d", len(s.runs)-i, len(s.runs))
		if full {
			label, suffix = "Previous review", ""
			if i == len(s.runs)-1 {
				label = "Latest review"
			}
		}
		header := []string{label, string(run.Status)}
		if run.Reviewer != "" {
			header = append(header, run.Reviewer)
		}
		header = append(header, revisionComparison(run, current))
		if run.Status == reviewmemory.Completed {
			header = append(header, reviewmemory.CountSeverities(run.Findings).String())
		}
		style := keyStyle
		if run.Status == reviewmemory.Failed || run.Status == reviewmemory.Blocked || run.Status == reviewmemory.Abandoned {
			style = warningStyle.Bold(true)
		}
		// A narrow terminal keeps the label and outcome on the rule and lists
		// the rest below it, one part per line, so no metadata is lost.
		if lipgloss.Width("── "+strings.Join(header, " · ")+" ") > m.width {
			heading(strings.Join(header[:2], " · "), suffix, style)
			for _, part := range header[2:] {
				add(part, reviewSecondaryStyle)
			}
		} else {
			heading(strings.Join(header, " · "), suffix, style)
		}
		if run.ID == stopTarget.ID && run.ID != "" {
			if m.stopping[r.pr.URL] == run.ID {
				add("Stopping run "+shortRevision(run.ID)+" · waiting for reviewer cleanup", warningStyle)
			} else {
				add("t stop run "+shortRevision(run.ID), keyStyle)
			}
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
		meta := fmt.Sprintf("%s → %s · %s", shortRevision(run.Identity.HeadOID), run.Identity.BaseRefName, run.StartedAt.Local().Format("02 Jan 2006 15:04 MST"))
		if hasTarget && run.ID == target.ID {
			meta += " · Publication target"
		}
		if !slices.ContainsFunc(s.publications, func(a publication.Attempt) bool { return a.RunID == run.ID }) {
			meta += " · Publication: no recorded posts"
		}
		add(meta, reviewSecondaryStyle)
		for _, a := range s.publications {
			if a.RunID == run.ID {
				publicationResult(a)
			}
		}
	}
	if len(s.runs) == 0 {
		heading("Reviews", "", keyStyle)
		if s.loaded {
			add("No local review runs · n run", body)
		} else {
			add("Loading local reviews…", reviewSecondaryStyle)
		}
	}

	repo, _ := m.cfg.RepositoryFor(r.pr.Repository)
	state := string(s.monitor.State)
	if state == "" {
		state = "unknown"
	}
	launches := "off"
	if repo.AutoLaunch {
		launches = "on"
	}
	after := "keep local"
	if repo.AutoPublish != "" {
		after = string(repo.AutoPublish)
	}
	waiting := ""
	if reason := automaticSetupWait(repo, m.cfg.Review.AutoViews, s.monitor); reason != "" {
		waiting = "Waiting: " + reason
	} else if s.slotBusy {
		waiting = "Waiting for review slot"
	}
	if !full {
		line := "Automation: monitor " + state + " · launches " + launches + " · after review: " + after
		if waiting != "" {
			line += " · " + waiting
		}
		add(line, reviewSecondaryStyle)
		if s.automatic.Reason != "" {
			add("Latest full observation: "+s.automatic.Reason, reviewSecondaryStyle)
		}
		if s.monitor.Message != "" {
			add(s.monitor.Message, warningStyle)
		}
		if len(s.runs) > 0 {
			latest := s.runs[len(s.runs)-1]
			details := "Details: run " + latest.ID
			if m.reviews != nil {
				details += " · diagnostics " + m.reviews.RunDirectory(latest.ID)
			}
			add(details, reviewSecondaryStyle)
		}
		return lines
	}
	heading("Automation", "", keyStyle)
	add("Monitor: "+state, body)
	if s.automatic.Reason != "" {
		add("Latest full observation: "+s.automatic.Reason, body)
	}
	if waiting != "" {
		add(waiting, warningStyle)
	}
	add("Automatic launches: "+launches, reviewSecondaryStyle)
	add("After review: "+after, reviewSecondaryStyle)
	if s.monitor.Message != "" {
		add(s.monitor.Message, warningStyle)
	}
	if !s.monitor.ObservedAt.IsZero() {
		add("Latest monitor observation: "+s.monitor.ObservedAt.Format(time.RFC3339), reviewSecondaryStyle)
	}
	if command := m.monitorCommandLines(); len(command) > 0 {
		add("Run in another terminal:", body)
		lines = append(lines, command...)
	}
	if len(s.runs) > 0 || len(s.publications) > 0 {
		heading("Details", "", keyStyle)
		for i := len(s.runs) - 1; i >= 0; i-- {
			run := s.runs[i]
			add("Run "+run.ID, keyStyle)
			add("Head "+run.Identity.HeadOID+" → "+run.Identity.BaseRefName, reviewSecondaryStyle)
			add("Started: "+run.StartedAt.Format(time.RFC3339), reviewSecondaryStyle)
			if m.reviews != nil {
				add("Diagnostics: "+m.reviews.RunDirectory(run.ID), reviewSecondaryStyle)
			}
		}
		for _, a := range s.publications {
			add("Publication "+string(a.Action)+": "+string(a.Status), keyStyle)
			add("Run "+a.RunID, reviewSecondaryStyle)
			if a.URL != "" {
				add(a.URL, reviewSecondaryStyle)
			}
		}
	}
	return lines
}

// revisionComparison relates a run to the latest successful board observation.
func revisionComparison(run reviewmemory.Run, current gh.PullRequest) string {
	switch {
	case current.HeadOID == "" || current.BaseRefName == "":
		return "current revision unknown"
	case run.Identity.HeadOID == current.HeadOID && run.Identity.BaseRefName == current.BaseRefName:
		return "current observed revision"
	default:
		return "older revision"
	}
}

// rule renders a section heading as a horizontal rule with the text at the
// left and an optional suffix at the right. A narrow terminal drops the
// suffix before it truncates the text.
func rule(text, suffix string, width int, style lipgloss.Style) string {
	line := "── " + reviewText(text) + " "
	if suffix != "" {
		suffix = " " + suffix + " ──"
		if lipgloss.Width(line)+lipgloss.Width(suffix) > width {
			suffix = ""
		}
	}
	line += strings.Repeat("─", max(0, width-lipgloss.Width(line)-lipgloss.Width(suffix))) + suffix
	return style.Render(truncate(line, max(1, width)))
}

func shortRevision(value string) string { r := []rune(value); return string(r[:min(8, len(r))]) }

// regionSize is the number of lines the region may show right now.
func (m Model) regionSize() int {
	if m.zoom {
		_, size := m.zoomViewport()
		return size
	}
	return m.boardLayout().regionRows
}

// regionViewport returns exactly size lines: the visible window, with the
// last line replaced by a marker while lines stay hidden below it.
func (m Model) regionViewport(lines []string, size int) []string {
	if size <= 0 {
		return nil
	}
	offset := max(0, min(m.region.offset, max(0, len(lines)-size)))
	end := min(len(lines), offset+size)
	out := make([]string, size)
	shown := copy(out, lines[offset:end])
	// A one-row viewport shows content, or scrolling could never reveal it.
	if hidden := len(lines) - end; hidden > 0 && size > 1 {
		out[shown-1] = reviewSecondaryStyle.Render(truncate(fmt.Sprintf("▼ %d more lines · j/k scroll", hidden+1), m.width))
	}
	return out
}

func (m Model) renderRegion(lay boardLayout) string {
	return strings.Join(m.regionViewport(m.regionLines(false), lay.regionRows), "\n")
}

// zoomViewport returns the zoom footer controls and the lines left for the
// region under the title and URL rows and above the footer. The controls
// leave those rows, one content row, and the meta line on screen.
func (m Model) zoomViewport() ([]string, int) {
	help := m.helpLines(zoomKeyHelp, m.height-4)
	return help, max(0, m.height-3-len(help))
}

func (m *Model) clampRegionOffset() {
	if m.region == nil {
		return
	}
	if m.region.setup != nil {
		m.clampRepositoryOffset()
		return
	}
	m.region.offset = max(0, min(m.region.offset, max(0, len(m.regionLines(m.zoom))-m.regionSize())))
}

func (m Model) renderZoom() string {
	pr := m.region.pr
	help, size := m.zoomViewport()
	title := m.cfg.UI.Title + " · " + pr.Repository + " #" + strconv.Itoa(pr.Number) + " · " + pr.Title
	body := []string{titleStyle.Render(truncate(reviewText(title), m.width)), urlStyle.Render(truncate(reviewText(pr.URL), m.width))}
	body = append(body, m.regionViewport(m.regionLines(true), size)...)
	body = append(body, strings.Split(m.renderFooter(help), "\n")...)
	return strings.Join(body[:min(len(body), max(1, m.height))], "\n")
}
