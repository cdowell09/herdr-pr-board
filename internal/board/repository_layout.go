package board

import (
	"strings"

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
			lines = append(lines, repositoryLine{line, row})
		}
	}
	for i, row := range s.rows() {
		if i == 6 {
			add("Global automatic views: these selections apply to ALL opted-in repositories.", -1)
		}
		prefix := "  "
		if i == s.row {
			prefix = "› "
		}
		add(prefix+row, i)
	}
	if len(s.views) == 0 {
		add("No configured views are available.", -1)
	}
	add("Allowed publication permits an action. It does not schedule publication.", -1)
	add("Automatic publication chooses the action after a completed automatic review. Local only sends nothing.", -1)
	add("Preview of these settings. Save to apply changes.", -1)
	for _, line := range m.monitorLines(s.repo, s.automatic.Selected) {
		add(line, -1)
	}
	if s.builtin != nil {
		add("Pi setup adds a reusable reviewer. Pi and its review skill must be installed.", -1)
	}
	if m.reviewPanel.message != "" {
		add(m.reviewPanel.message, -1)
	}
	if s.saving {
		add("Saving…", -1)
	}
	for _, line := range m.monitorCommandLines() {
		lines = append(lines, repositoryLine{line, -1})
	}
	return lines
}

// The same viewport defines visible lines and clickable row coordinates.
func (m Model) repositoryViewport() (header []string, content []repositoryLine, start, size int) {
	status := m.reviewPanel.monitor.State
	if status == "" {
		status = "unknown"
	}
	summary := "Setup ready; save to apply"
	if reason := automaticSetupWait(m.reviewPanel.setup.repo, m.reviewPanel.setup.automatic.Selected, m.reviewPanel.monitor); reason != "" {
		summary = "Waiting: " + reason
	}
	if m.reviewPanel.message != "" {
		summary = m.reviewPanel.message
	}
	if m.reviewPanel.setup.saving {
		summary = "Saving settings…"
	}
	header = []string{titleStyle.Render(truncate("Repository settings", m.width)), urlStyle.Render(truncate(reviewText(m.reviewPanel.pr.URL), m.width)), truncate("Monitor: "+string(status), m.width), truncate(reviewText(summary), m.width)}
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
	if s.saving {
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
