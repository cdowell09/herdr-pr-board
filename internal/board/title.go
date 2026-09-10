package board

import "github.com/charmbracelet/lipgloss"

// refreshStatus is the suffix the title bar shows during a refresh.
const refreshStatus = "  refreshing…"

// WithVersion shows the version in the title bar. The caller supplies the
// version, which keeps build information out of the board.
func (m Model) WithVersion(version string) Model {
	m.version = version
	return m
}

// titleText returns the title bar text without the refresh status. The version
// needs space, so the medium and wide tiers show it and the narrow tier omits
// it.
func (m Model) titleText() string {
	if m.version == "" || m.width < tierMedium {
		return m.cfg.UI.Title
	}
	return m.cfg.UI.Title + " v" + m.version
}

// renderTitle returns the title bar line. The status keeps its width, and the
// title uses the remaining cells. This keeps the line inside the terminal.
func (m Model) renderTitle() string {
	status := ""
	if m.loading {
		status = refreshStatus
	}
	budget := m.width - lipgloss.Width(status)
	if budget < 1 {
		return warningStyle.Render(truncate(status, m.width))
	}
	return titleStyle.Render(truncate(m.titleText(), budget)) + warningStyle.Render(status)
}
