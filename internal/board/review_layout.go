package board

import (
	"fmt"
	"slices"
	"strings"

	gh "github.com/cdowell09/herdr-pr-board/internal/github"
	"github.com/cdowell09/herdr-pr-board/internal/publication"
	"github.com/cdowell09/herdr-pr-board/internal/reviewmemory"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

var reviewSecondaryStyle = lipgloss.NewStyle().Foreground(subtleColor)

const (
	// regionSplitHeight is the terminal height that fits the table and the
	// detail pane. A shorter terminal shows the one-line summary instead.
	regionSplitHeight = 24
	// regionMinRows keeps the pane's facts and some review rows on screen
	// when the table has many rows.
	regionMinRows = 9
)

// regionSplit reports whether the board shows the detail pane under the table.
func (m Model) regionSplit() bool { return m.region != nil && m.height >= regionSplitHeight }

// regionLines renders the detail pane's review section: labeled rows for
// the split, or every run with the full automation and details sections in
// zoom. Removing local reviews removes this section and its callers.
func (m Model) regionLines(zoom bool) []string {
	// The section sits inside the pane's frame.
	m.width = m.paneWidth()
	if zoom {
		return m.zoomRegionLines()
	}
	return m.splitRegionLines()
}

func (m Model) zoomRegionLines() []string {
	r, s := m.region, m.regionState()
	var lines []string
	add := func(value string, style lipgloss.Style) {
		for _, line := range strings.Split(ansi.Wrap(reviewText(value), max(1, m.width), ""), "\n") {
			lines = append(lines, style.Render(line))
		}
	}
	heading := func(text, suffix string, style lipgloss.Style) {
		if len(lines) > 0 && m.height >= 20 {
			add("", reviewSecondaryStyle)
		}
		lines = append(lines, rule(text, suffix, m.width, style))
	}
	body := regionBodyStyle
	addPublication := func(a publication.Attempt) {
		add("Publication "+string(a.Action)+": "+string(a.Status), keyStyle)
		if a.URL != "" {
			add(a.URL, reviewSecondaryStyle)
		}
	}
	for _, warning := range m.regionWarnings() {
		add(warning, warningStyle)
	}
	// The posted summary belongs to the PR, not to a run, so it leads the
	// region and stays visible above the findings. The row summary checks the
	// revision identity, so a read against an older head never labels an old
	// post as current.
	add("Posted: "+m.rowReviewSummary(r.pr).postedDetail, reviewSecondaryStyle)
	current := m.reviewObservation()
	target, hasTarget := latestCompleted(s.runs)
	for i := len(s.runs) - 1; i >= 0; i-- {
		run := s.runs[i]
		label := "Previous review"
		if i == len(s.runs)-1 {
			label = "Latest review"
		}
		header := []string{label, string(run.Status)}
		if run.Reviewer != "" {
			header = append(header, run.Reviewer)
		}
		header = append(header, revisionComparison(run, current))
		if run.Status == reviewmemory.Completed {
			header = append(header, severitySummary(reviewmemory.CountSeverities(run.Findings)))
		}
		style := keyStyle
		if run.Status == reviewmemory.Failed || run.Status == reviewmemory.Blocked || run.Status == reviewmemory.Abandoned {
			style = warningStyle.Bold(true)
		}
		// A narrow terminal keeps the label and outcome on the rule and lists
		// the rest below it, one part per line, so no metadata is lost.
		if lipgloss.Width("── "+strings.Join(header, " · ")+" ") > m.width {
			heading(strings.Join(header[:2], " · "), "", style)
			for _, part := range header[2:] {
				add(part, reviewSecondaryStyle)
			}
		} else {
			heading(strings.Join(header, " · "), "", style)
		}
		if stop, ok := m.stopLine(run); ok {
			add(stop.text, stop.style)
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
		meta := fmt.Sprintf("%s → %s · %s", shortRevision(run.Identity.HeadOID), run.Identity.BaseRefName, run.StartedAt.Local().Format(zoomTimeFormat))
		if hasTarget && run.ID == target.ID {
			meta += " · Publication target"
		}
		if !slices.ContainsFunc(s.publications, func(a publication.Attempt) bool { return a.RunID == run.ID }) {
			meta += " · Publication: no recorded posts"
		}
		add(meta, reviewSecondaryStyle)
		for _, state := range m.publicationStates(run.ID) {
			add("Publication "+state.text, state.style)
		}
	}
	if len(s.runs) == 0 {
		heading("Reviews", "", keyStyle)
		add(m.noRunsText(), body)
	}

	auto := m.automationState()
	heading("Automation", "", keyStyle)
	add("Monitor: "+auto.monitor, body)
	if s.automatic.Reason != "" {
		add("Latest full observation: "+s.automatic.Reason, body)
	}
	if auto.waiting != "" {
		add(auto.waiting, warningStyle)
	}
	add("Automatic launches: "+auto.launches, reviewSecondaryStyle)
	add("After review: "+auto.after, reviewSecondaryStyle)
	if s.monitor.Message != "" {
		add(s.monitor.Message, warningStyle)
	}
	if !s.monitor.ObservedAt.IsZero() {
		add("Latest monitor observation: "+s.monitor.ObservedAt.Local().Format(zoomTimeFormat), reviewSecondaryStyle)
	}
	if command := m.monitorCommandLines(); len(command) > 0 {
		add("Run in another terminal:", body)
		lines = append(lines, command...)
	}
	if len(s.runs) > 0 || len(s.publications) > 0 {
		heading("Details", "", keyStyle)
		// Each run names its ID once, with its publications under it. The run
		// header above already shows when it started.
		listed := map[string]bool{}
		for i := len(s.runs) - 1; i >= 0; i-- {
			run := s.runs[i]
			listed[run.ID] = true
			add("Run "+run.ID, keyStyle)
			add("Head "+run.Identity.HeadOID+" → "+run.Identity.BaseRefName, reviewSecondaryStyle)
			if m.reviews != nil {
				add("Diagnostics: "+m.reviews.RunDirectory(run.ID), reviewSecondaryStyle)
			}
			for _, a := range s.publications {
				if a.RunID == run.ID {
					addPublication(a)
				}
			}
		}
		for _, a := range s.publications {
			if !listed[a.RunID] {
				add("Run "+a.RunID, keyStyle)
				addPublication(a)
			}
		}
	}
	return lines
}

// regionWarnings lists the status, read, and monitor problems that lead the region.
func (m Model) regionWarnings() []string {
	var warnings []string
	if m.region.message != "" {
		warnings = append(warnings, "Status: "+m.region.message)
	}
	if err := m.regionState().readErr; err != nil {
		warnings = append(warnings, "Local reviews unavailable: "+err.Error())
	}
	if m.monitorError != "" {
		warnings = append(warnings, m.monitorError)
	}
	return warnings
}

// stopLine offers or reports the stop of run when it is the stop target.
func (m Model) stopLine(run reviewmemory.Run) (span, bool) {
	target, _ := m.stopTarget()
	if run.ID == "" || run.ID != target.ID {
		return span{}, false
	}
	if m.stopping[m.region.pr.URL] == run.ID {
		return span{text: "Stopping run " + shortRevision(run.ID) + " · waiting for reviewer cleanup", style: warningStyle}, true
	}
	return span{text: "t stop run " + shortRevision(run.ID), style: keyStyle}, true
}

// publicationStates lists one run's publication results, for example
// "comment published". A result that did not publish carries its message.
func (m Model) publicationStates(runID string) []span {
	var states []span
	for _, a := range m.regionState().publications {
		if a.RunID != runID {
			continue
		}
		state := string(a.Action) + " " + string(a.Status)
		style := regionBodyStyle
		if a.Status != publication.Published {
			style = warningStyle
			if a.Message != "" {
				state += ": " + a.Message
			}
		}
		states = append(states, span{text: reviewText(state), style: style})
	}
	return states
}

func (m Model) noRunsText() string {
	if m.regionState().loaded {
		return "No local review runs"
	}
	return "Loading local reviews…"
}

// automation is the region PR's automatic review setup in display words.
type automation struct {
	monitor, launches, after, waiting string
}

func (m Model) automationState() automation {
	s := m.regionState()
	repo, _ := m.cfg.RepositoryFor(m.region.pr.Repository)
	auto := automation{monitor: string(s.monitor.State), launches: "off", after: "keep local"}
	if auto.monitor == "" {
		auto.monitor = "unknown"
	}
	if repo.AutoLaunch {
		auto.launches = "on"
	}
	if repo.AutoPublish != "" {
		auto.after = string(repo.AutoPublish)
	}
	if reason := automaticSetupWait(repo, m.cfg.Review.AutoViews, s.monitor); reason != "" {
		auto.waiting = "Waiting: " + reason
	} else if s.slotBusy {
		auto.waiting = "Waiting for review slot"
	}
	return auto
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

// zoomTimeFormat is the one time format of the zoomed review section.
const zoomTimeFormat = "02 Jan 2006 15:04 MST"

func shortRevision(value string) string { r := []rune(value); return string(r[:min(8, len(r))]) }

// regionSize is the number of review section lines the pane shows right now.
func (m Model) regionSize() int {
	if m.zoom {
		_, size := m.zoomViewport()
		return size
	}
	if lay := m.boardLayout(); lay.regionRows > 0 {
		return m.paneReviewRows(lay.regionRows + 1)
	}
	return 0
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

// zoomViewport returns the zoom footer controls and the review section rows
// of the full-height pane above them and the meta line.
func (m Model) zoomViewport() ([]string, int) {
	help := renderHints(m.zoomHints())
	return help, m.paneReviewRows(zoomPaneHeight(m.height, help))
}

func zoomPaneHeight(height int, help []string) int { return max(0, height-1-len(help)) }

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
	help, _ := m.zoomViewport()
	// The shortest pane still shows its edge and the URL.
	pane := m.renderPane(max(paneURLOffset+1, zoomPaneHeight(m.height, help)))
	body := append(pane, strings.Split(m.renderFooter(help), "\n")...)
	return strings.Join(body[:min(len(body), max(1, m.height))], "\n")
}
