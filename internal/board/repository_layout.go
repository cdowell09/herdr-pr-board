package board

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

type repositoryLine struct {
	text string
	row  int
}

func (m Model) repositoryContent() []repositoryLine {
	s := m.reviewPanel.setup
	var lines []repositoryLine
	add := func(text string, row int) {
		for _, line := range strings.Split(ansi.Wrap(reviewText(text), max(1, m.width), ""), "\n") {
			if row >= 0 && row == s.row {
				line = selectedStyle.Width(max(1, m.width)).Render(line)
			}
			lines = append(lines, repositoryLine{line, row})
		}
	}
	section := func(title string) {
		if len(lines) > 0 && m.height >= 20 {
			add("", -1)
		}
		start := len(lines)
		add(title, -1)
		for i := start; i < len(lines); i++ {
			lines[i].text = keyStyle.Render(lines[i].text)
		}
	}
	promptRow, skillRow := s.promptRow(), s.skillRow()
	for i, row := range s.rows() {
		switch i {
		case repositoryReviewerRow:
			section("Reviews")
		case repositoryPermissionsRow:
			section("GitHub permissions")
			add("Allowed actions, not automatic posts.", -1)
		case repositoryPostingRow:
			section("Automatic posting")
		case repositoryViewsRow:
			section("Global views")
			if len(s.views) == 0 {
				add("No configured views are available.", -1)
			} else {
				add("Shared by all opted-in repositories.", -1)
			}
		}
		// Without configured views, the prompt row is also the first view row.
		if i == promptRow {
			section("Advanced")
			if s.selectedReviewer().Builtin() != "" {
				add("Default review checks repository standards and specification.", -1)
				add("Choose prompt and skill files, or keep defaults.", -1)
				add("All repositories using "+s.repo.Reviewer+" share these files.", -1)
			} else {
				add("This custom command manages its own instructions.", -1)
			}
		}
		prefix := "  "
		if i == s.row {
			prefix = "› "
		}
		if i == s.row && s.editing != nil {
			label := "Prompt file: "
			if i == skillRow {
				label = "Skill file: "
			}
			e := s.editing
			width := max(1, m.width-ansi.StringWidth(prefix+label))
			left := reviewText(string(e.value[:e.cursor]))
			left = ansi.TruncateLeft(left, max(0, ansi.StringWidth(left)-width+1), "")
			right := ansi.Truncate(reviewText(string(e.value[e.cursor:])), max(0, width-ansi.StringWidth(left)-1), "")
			row = label + left + "▏" + right
		}
		add(prefix+row, i)
		if i == skillRow && s.selectedReviewer().Builtin() != "" {
			add("A custom prompt replaces default criteria. A skill adds requirements.", -1)
			add("New relative paths start in the configuration directory.", -1)
		}
		if i == repositoryPostingRow {
			add("For every completed review.", -1)
		}
	}
	if builtin := s.selectedBuiltin(); builtin != nil {
		section("New reviewer: " + builtin.ID)
		add("Install and authenticate the agent CLI before running a review.", -1)
	}
	automatic := s.automationSelected()
	if automatic || m.reviewPanel.message != "" || m.monitorError != "" || s.saving {
		section("Monitor")
	}
	if automatic {
		if observed := m.reviewPanel.monitor.ObservedAt; !observed.IsZero() {
			add("Latest observation: "+observed.Format(time.RFC3339), -1)
		}
		if message := m.reviewPanel.monitor.Message; message != "" {
			add(message, -1)
		}
	}
	if m.reviewPanel.message != "" {
		add(m.reviewPanel.message, -1)
	}
	if m.monitorError != "" {
		add(m.monitorError, -1)
	}
	if s.saving {
		add("Saving…", -1)
	}
	if automatic {
		if command := m.monitorCommandLines(); len(command) > 0 {
			add("Run in another terminal:", -1)
			for _, line := range command {
				lines = append(lines, repositoryLine{line, -1})
			}
		}
	}
	return lines
}

// The same viewport defines visible lines and clickable row coordinates.
func (m Model) repositoryViewport() (header []string, content []repositoryLine, start, size int) {
	s := m.reviewPanel.setup
	status := m.reviewPanel.monitor.State
	if status == "" {
		status = "unknown"
	}
	narrow, automatic := m.width < 60, s.automationSelected()
	summary := "Setup ready; save to apply"
	switch reason := automaticSetupWait(s.repo, s.automatic.Selected, m.reviewPanel.monitor); {
	case !s.repo.AutoLaunch && narrow:
		summary = "Ready · Enter save · n run"
	case !s.repo.AutoLaunch:
		summary = "Manual reviews ready. Press Enter to save, then n to run."
	case reason != "":
		summary = "Waiting: " + reason
	}
	if m.reviewPanel.message != "" {
		summary = m.reviewPanel.message
	}
	if m.monitorError != "" {
		summary = m.monitorError
	}
	if s.saving {
		summary = "Saving settings…"
	}
	title := "Repository settings"
	if narrow {
		title = "Settings"
		if automatic {
			title += " · monitor " + string(status)
		}
	}
	header = []string{titleStyle.Render(truncate(title, m.width)), urlStyle.Render(truncate(reviewText(m.reviewPanel.pr.URL), m.width))}
	if automatic && !narrow {
		header = append(header, truncate("Monitor: "+string(status), m.width))
	}
	header = append(header, truncate(reviewText(summary), m.width))
	height := max(3, m.height)
	help := m.repositoryHelp()
	header = header[:min(len(header), max(0, height-len(help)-1))]
	size = max(1, height-len(header)-len(help))
	content = m.repositoryContent()
	start = min(max(0, m.reviewPanel.setup.offset), max(0, len(content)-size))
	return
}

func (m *Model) revealRepositoryRow() {
	_, lines, start, size := m.repositoryViewport()
	first, last := -1, -1
	for i, line := range lines {
		if line.row == m.reviewPanel.setup.row {
			if first < 0 {
				first = i
			}
			last = i
		}
	}
	if first < start {
		start = first
	} else if last >= start+size {
		start = min(first, last-size+1)
	}
	m.reviewPanel.setup.offset = max(0, start)
}

func (m Model) renderRepositoryPanel() string {
	header, content, start, size := m.repositoryViewport()
	lines := append([]string(nil), header...)
	end := min(len(content), start+size)
	for _, line := range content[start:end] {
		lines = append(lines, line.text)
	}
	for len(lines) < len(header)+size {
		lines = append(lines, "")
	}
	lines = append(lines, m.repositoryHelp()...)
	return strings.Join(lines, "\n")
}

func (m Model) updateRepositoryMouse(message tea.MouseMsg) (tea.Model, tea.Cmd) {
	s := m.reviewPanel.setup
	if s.saving || s.editing != nil {
		return m, nil
	}
	event := tea.MouseEvent(message)
	switch event.Button {
	case tea.MouseButtonWheelUp:
		s.offset = max(0, s.offset-mouseStep)
	case tea.MouseButtonWheelDown:
		s.offset += mouseStep
	case tea.MouseButtonLeft:
		if event.Action != tea.MouseActionPress {
			return m, nil
		}
		header, lines, start, size := m.repositoryViewport()
		if event.Y == 1 && len(header) > 1 {
			return m, m.openBrowser(m.reviewPanel.pr.URL)
		}
		index := event.Y - len(header)
		if index >= 0 && index < size && start+index < len(lines) && lines[start+index].row >= 0 {
			s.row = lines[start+index].row
			s.toggle()
		}
	}
	m.clampRepositoryOffset()
	return m, nil
}

func (m Model) repositoryHelp() []string {
	text := "↑↓ select · Space change · Enter save · PgUp/Dn scroll · Esc cancel"
	if m.reviewPanel.setup.editing != nil {
		text = "Type or paste path · Enter use · Ctrl+U clear · Esc discard"
	}
	lines := strings.Split(ansi.Wrap(text, max(1, m.width), ""), "\n")
	if len(lines) > max(1, m.height-2) {
		return []string{truncate("Enlarge panel for controls.", m.width)}
	}
	return lines
}

func (m *Model) clampRepositoryOffset() {
	if m.reviewPanel != nil && m.reviewPanel.setup != nil {
		_, _, start, _ := m.repositoryViewport()
		m.reviewPanel.setup.offset = start
	}
}
