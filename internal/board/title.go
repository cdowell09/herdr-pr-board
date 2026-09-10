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

// versionSuffix returns the version part of the title bar. The version needs
// space, so the medium and wide tiers show it and the narrow tier omits it.
func (m Model) versionSuffix() string {
	if m.version == "" || m.width < tierMedium {
		return ""
	}
	return " v" + m.version
}

// renderTitle returns the title bar line. The version and the refresh status
// keep their width, and the configured title uses the remaining cells. This
// keeps the line inside the terminal and keeps the version visible.
func (m Model) renderTitle() string {
	status := ""
	if m.loading {
		status = refreshStatus
	}
	version := m.versionSuffix()
	budget := m.width - lipgloss.Width(status) - lipgloss.Width(version)
	if budget < 1 {
		version = ""
		budget = m.width - lipgloss.Width(status)
	}
	if budget < 1 {
		return warningStyle.Render(truncate(status, m.width))
	}
	return titleStyle.Render(truncate(m.cfg.UI.Title, budget)+version) + warningStyle.Render(status)
}
