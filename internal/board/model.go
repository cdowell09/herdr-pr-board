package board

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/cdowell09/herdr-pr-board/internal/config"
	gh "github.com/cdowell09/herdr-pr-board/internal/github"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	tabRowY     = 1
	firstPRRowY = 5
	mouseStep   = 3
)

// firstPRRowYFor returns the screen row of the first PR row, which shifts down
// by one when the stale notice occupies the separator line.
func firstPRRowYFor(stale bool) int {
	if stale {
		return firstPRRowY + 1
	}
	return firstPRRowY
}

func (m Model) firstPRRow() int {
	return firstPRRowYFor(m.currentView().Stale())
}

var (
	titleStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212"))
	activeTab     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("230")).Background(lipgloss.Color("62")).Padding(0, 1)
	inactiveTab   = lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Padding(0, 1)
	headerStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("252")).BorderBottom(true).BorderStyle(lipgloss.NormalBorder()).BorderForeground(lipgloss.Color("238"))
	selectedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("255")).Background(lipgloss.Color("237"))
	dimStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("242"))
	warningStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	errorStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	urlStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("81")).Underline(true)
)

// tabPadding is the horizontal padding the tab styles add to every label.
// Deriving it from the style keeps tab hitboxes correct if the padding
// changes.
var tabPadding = lipgloss.Width(inactiveTab.Render(""))

type snapshotMsg Snapshot

type viewMsg struct {
	index    int
	snapshot ViewSnapshot
}

type tickMsg struct{}

type browserMsg struct {
	err error
}

type keyHelpEntry struct {
	keys   string
	action string
}

// keyHelp is the single source of truth for the footer control list.
var keyHelp = []keyHelpEntry{
	{"1–9 · Tab · ⇧Tab · h/l · ←/→", "view"},
	{"j/k · ↑/↓", "select"},
	{"g/G · Home/End", "first/last"},
	{"/ · Enter", "filter"},
	{"Ctrl+U · Esc", "clear"},
	{"r · R", "refresh"},
	{"Enter · o", "open"},
	{"wheel · click", "mouse"},
	{"q · Ctrl+C", "quit"},
}

// documentedKeys lists every key literal the README must document. The
// README drift test fails when one is missing, so a new binding added to
// updateKey or updateFilter must be added here and to the README.
var documentedKeys = []string{
	"1", "9", "Tab", "Shift+Tab", "h", "l", "←", "→",
	"j", "k", "↑", "↓", "g", "G", "Home", "End",
	"/", "Enter", "Ctrl+U", "Esc", "r", "R", "o", "q", "Ctrl+C",
}

// table tiers and their minimum terminal widths in cells.
const (
	tierWide   = 100
	tierMedium = 80
	tierNarrow = 60
)

type Model struct {
	cfg         config.Config
	loader      Loader
	openBrowser func(url string) tea.Cmd
	refresh     time.Duration
	views       []ViewData
	active      int
	cursor      int
	offset      int
	width       int
	height      int
	filter      string
	editing     bool
	loading     bool
	warning     string
	rates       gh.RateLimits
}

func NewModel(cfg config.Config, loader Loader) (Model, error) {
	refresh, err := cfg.RefreshEvery()
	if err != nil {
		return Model{}, err
	}
	views := make([]ViewData, len(cfg.Views))
	for i, view := range cfg.Views {
		views[i].View = view
	}
	return Model{cfg: cfg, loader: loader, openBrowser: openBrowserCmd, refresh: refresh, views: views, loading: true}, nil
}

func (m Model) Init() tea.Cmd {
	commands := []tea.Cmd{m.refreshAllCmd()}
	if m.refresh > 0 {
		commands = append(commands, m.tickCmd())
	}
	return tea.Batch(commands...)
}

func (m Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := message.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.clampCursor()
		return m, nil
	case snapshotMsg:
		nextViews := msg.Views
		warning := msg.Warning
		for i := range nextViews {
			if nextViews[i].Err == nil {
				nextViews[i].UpdatedAt = msg.UpdatedAt
				continue
			}
			warning = appendWarning(warning, nextViews[i].View.Title+": "+nextViews[i].Err.Error())
			if i < len(m.views) {
				nextViews[i].retainFrom(m.views[i])
			}
		}
		m.views = nextViews
		m.rates = msg.Rates
		m.warning = warning
		m.loading = false
		m.clampCursor()
		return m, nil
	case viewMsg:
		refresh := msg.snapshot
		data := refresh.Data
		if msg.index >= 0 && msg.index < len(m.views) {
			if data.Err == nil {
				data.UpdatedAt = refresh.UpdatedAt
			} else {
				data.retainFrom(m.views[msg.index])
			}
			m.views[msg.index] = data
		}
		m.rates = refresh.Rates
		m.warning = refresh.Warning
		if data.Err != nil {
			m.warning = appendWarning(m.warning, data.Err.Error())
		}
		m.loading = false
		m.clampCursor()
		return m, nil
	case tickMsg:
		commands := []tea.Cmd{m.tickCmd()}
		if !m.loading && searchCapacityAvailable(m.rates.Search, m.cfg.SearchRequestCount()) {
			m.loading = true
			commands = append(commands, m.refreshAllCmd())
		}
		return m, tea.Batch(commands...)
	case browserMsg:
		if msg.err != nil {
			m.warning = appendWarning(m.warning, "could not open the PR in a browser: "+msg.err.Error()+"; use the URL above or press Enter/click again")
		}
		return m, nil
	case tea.MouseMsg:
		return m.updateMouse(msg)
	case tea.KeyMsg:
		if m.editing {
			return m.updateFilter(msg)
		}
		return m.updateKey(msg)
	}
	return m, nil
}

func (m Model) updateFilter(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "esc":
		m.editing = false
		m.filter = ""
		m.cursor, m.offset = 0, 0
	case "enter":
		m.editing = false
		m.cursor, m.offset = 0, 0
	case "backspace":
		if m.filter != "" {
			_, size := utf8.DecodeLastRuneInString(m.filter)
			m.filter = m.filter[:len(m.filter)-size]
			m.cursor, m.offset = 0, 0
		}
	case "ctrl+u":
		m.filter = ""
		m.cursor, m.offset = 0, 0
	default:
		if key.Type == tea.KeyRunes {
			m.filter += string(key.Runes)
			m.cursor, m.offset = 0, 0
		}
	}
	return m, nil
}

func (m Model) updateKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "tab", "right", "l":
		m.selectView((m.active + 1) % len(m.views))
	case "shift+tab", "left", "h":
		m.selectView((m.active - 1 + len(m.views)) % len(m.views))
	case "j", "down":
		m.cursor++
		m.clampCursor()
	case "k", "up":
		m.cursor--
		m.clampCursor()
	case "g", "home":
		m.cursor, m.offset = 0, 0
	case "G", "end":
		m.cursor = len(m.filteredPRs()) - 1
		m.clampCursor()
	case "/":
		m.editing = true
	case "esc":
		if m.filter != "" {
			m.filter = ""
			m.cursor, m.offset = 0, 0
		}
	case "r":
		requests := m.currentView().View.SearchRequestCount(len(m.cfg.GitHub.Scopes), m.cfg.GitHub.LimitPerScope)
		if !m.loading && searchCapacityAvailable(m.rates.Search, requests) {
			m.loading = true
			return m, m.refreshOneCmd(m.active)
		}
	case "R":
		if !m.loading && searchCapacityAvailable(m.rates.Search, m.cfg.SearchRequestCount()) {
			m.loading = true
			return m, m.refreshAllCmd()
		}
	case "enter", "o":
		if pr, ok := m.selectedPR(); ok {
			return m, m.openBrowser(pr.URL)
		}
	default:
		if number, err := strconv.Atoi(key.String()); err == nil && number >= 1 && number <= len(m.views) {
			m.selectView(number - 1)
		}
	}
	return m, nil
}

func (m Model) updateMouse(message tea.MouseMsg) (tea.Model, tea.Cmd) {
	event := tea.MouseEvent(message)
	switch event.Button {
	case tea.MouseButtonWheelUp:
		m.cursor -= mouseStep
		m.clampCursor()
		return m, nil
	case tea.MouseButtonWheelDown:
		m.cursor += mouseStep
		m.clampCursor()
		return m, nil
	case tea.MouseButtonLeft:
		if event.Action != tea.MouseActionPress {
			return m, nil
		}
	default:
		return m, nil
	}

	if event.Y == tabRowY {
		if index, ok := m.tabAtX(event.X); ok {
			m.selectView(index)
		}
		return m, nil
	}

	rows := m.filteredPRs()
	firstRow := m.firstPRRow()
	row := m.offset + event.Y - firstRow
	visible := min(m.visibleRows(), max(0, len(rows)-m.offset))
	if event.Y >= firstRow && event.Y < firstRow+visible && row >= 0 && row < len(rows) {
		m.cursor = row
		m.clampCursor()
		return m, nil
	}

	if event.Y == m.selectedURLY() {
		if pr, ok := m.selectedPR(); ok {
			return m, m.openBrowser(pr.URL)
		}
	}
	return m, nil
}

func (m Model) tabAtX(x int) (int, bool) {
	position := 0
	for i, view := range m.views {
		label := m.tabLabel(i, view)
		width := lipgloss.Width(inactiveTab.Render(label))
		if x >= position && x < position+width {
			return i, true
		}
		position += width + 1
	}
	return 0, false
}

func (m Model) selectedURLY() int {
	firstRow := m.firstPRRow()
	if len(m.filteredPRs()) == 0 {
		return firstRow
	}
	visible := min(m.visibleRows(), max(0, len(m.filteredPRs())-m.offset))
	return firstRow + 1 + visible
}

func (m *Model) selectView(index int) {
	m.active = index
	m.cursor, m.offset = 0, 0
}

func (m *Model) clampCursor() {
	rows := m.filteredPRs()
	if len(rows) == 0 {
		m.cursor, m.offset = 0, 0
		return
	}
	m.cursor = max(0, min(m.cursor, len(rows)-1))
	visible := m.visibleRows()
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+visible {
		m.offset = m.cursor - visible + 1
	}
	m.offset = max(0, min(m.offset, max(0, len(rows)-visible)))
}

func (m Model) View() string {
	if m.width == 0 || m.height == 0 {
		return "Loading PR board…"
	}
	var output strings.Builder
	status := ""
	if m.loading {
		status = warningStyle.Render("  refreshing…")
	}
	output.WriteString(titleStyle.Render(m.cfg.UI.Title) + status + "\n")
	output.WriteString(m.renderTabs() + "\n")
	if notice := m.renderStaleNotice(); notice != "" {
		output.WriteString(notice + "\n")
	}
	output.WriteString("\n")
	output.WriteString(m.renderTable())
	output.WriteString("\n" + m.renderSelected())
	output.WriteString("\n" + m.renderFooter())
	return output.String()
}

func (m Model) renderTabs() string {
	var tabs []string
	for i, view := range m.views {
		label := m.tabLabel(i, view)
		if i == m.active {
			tabs = append(tabs, activeTab.Render(label))
		} else {
			tabs = append(tabs, inactiveTab.Render(label))
		}
	}
	return strings.Join(tabs, " ")
}

func (m Model) renderStaleNotice() string {
	view := m.currentView()
	if !view.Stale() {
		return ""
	}
	notice := fmt.Sprintf("stale — showing %d rows retained from the last successful refresh", len(view.PRs))
	return warningStyle.Render(truncate(notice, m.width))
}

func (m Model) renderTable() string {
	view := m.currentView()
	if view.Err != nil && len(view.PRs) == 0 {
		return errorStyle.Render(truncate("GitHub query failed: "+view.Err.Error(), m.width)) + "\n"
	}
	rows := m.filteredPRs()
	if len(rows) == 0 {
		if m.loading {
			return dimStyle.Render("Loading pull requests…") + "\n"
		}
		if m.filter != "" {
			return dimStyle.Render("No pull requests match the filter.") + "\n"
		}
		return dimStyle.Render("No pull requests in this view.") + "\n"
	}

	layout := m.tableLayout()
	header := m.renderHeader(layout)
	var output strings.Builder
	output.WriteString(header + "\n")
	end := min(m.offset+m.visibleRows(), len(rows))
	for i := m.offset; i < end; i++ {
		pr := rows[i]
		line := m.renderPRRow(pr, layout)
		if i == m.cursor {
			line = selectedStyle.Width(m.width).Render(line)
		}
		output.WriteString(line + "\n")
	}
	return output.String()
}

func (m Model) renderSelected() string {
	pr, ok := m.selectedPR()
	if !ok {
		return dimStyle.Render("No PR selected")
	}
	return urlStyle.Render(truncate(pr.URL, m.width))
}

func (m Model) renderFooter() string {
	filter := ""
	if m.editing {
		filter = "  filter: " + m.filter + "▌"
	} else if m.filter != "" {
		filter = "  filter: " + m.filter
	}
	var help []string
	for _, entry := range keyHelp {
		help = append(help, entry.keys+" "+entry.action)
	}
	keys := strings.Join(help, " · ")
	meta := ""
	freshness := m.currentView().UpdatedAt
	if !freshness.IsZero() {
		meta = fmt.Sprintf("updated %s", relativeTime(freshness))
	}
	if m.currentView().Stale() {
		meta += " · stale"
	}
	if m.rates.Search.Limit > 0 {
		meta += fmt.Sprintf(" · Search %d/%d", m.rates.Search.Remaining, m.rates.Search.Limit)
	}
	if m.rates.GraphQL.Limit > 0 {
		meta += fmt.Sprintf(" · GraphQL %d/%d", m.rates.GraphQL.Remaining, m.rates.GraphQL.Limit)
	}
	if m.warning != "" {
		meta += " · " + m.warning
	}
	return dimStyle.Render(truncate(keys+filter, m.width)) + "\n" + warningStyle.Render(truncate(meta, m.width))
}

func (m Model) currentView() ViewData {
	if len(m.views) == 0 {
		return ViewData{}
	}
	return m.views[m.active]
}

func (m Model) filteredPRs() []gh.PullRequest {
	rows := append([]gh.PullRequest(nil), m.currentView().PRs...)
	if m.filter != "" {
		needle := strings.ToLower(m.filter)
		filtered := rows[:0]
		for _, pr := range rows {
			haystack := strings.ToLower(fmt.Sprintf("%s %s %s #%d", pr.Repository, pr.Title, pr.Author, pr.Number))
			if strings.Contains(haystack, needle) {
				filtered = append(filtered, pr)
			}
		}
		rows = filtered
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].UpdatedAt.After(rows[j].UpdatedAt) })
	return rows
}

func (m Model) selectedPR() (gh.PullRequest, bool) {
	rows := m.filteredPRs()
	if len(rows) == 0 || m.cursor < 0 || m.cursor >= len(rows) {
		return gh.PullRequest{}, false
	}
	return rows[m.cursor], true
}

func (m Model) visibleRows() int {
	rows := m.height - 9
	if m.currentView().Stale() {
		rows--
	}
	return max(1, rows)
}

// tableLayout picks column sizes for the current terminal width. A width of
// zero hides the column. The title absorbs all remaining width.
//
// Tiers: wide keeps every column; medium compacts repository and author;
// narrow drops the author; very narrow drops the repository, author, and
// updated columns while the selected PR URL stays visible.
type tableLayout struct {
	repo    int
	title   int
	author  int
	updated bool
}

func (m Model) tableLayout() tableLayout {
	var layout tableLayout
	switch {
	case m.width >= tierWide:
		layout.repo, layout.author, layout.updated = 24, 14, true
	case m.width >= tierMedium:
		layout.repo, layout.author, layout.updated = 18, 10, true
	case m.width >= tierNarrow:
		layout.repo, layout.updated = 16, true
	}
	layout.title = max(0, m.width-layout.fixedWidth())
	return layout
}

// fixedWidth returns the cell width of every column except the title.
func (l tableLayout) fixedWidth() int {
	width := 6 + 2 + 3 + 2 // PR column, CI column, and their separators
	if l.repo > 0 {
		width += l.repo + 2
	}
	if l.author > 0 {
		width += l.author + 2
	}
	if l.updated {
		width += 8 + 1 // UPDATED column
	}
	return width
}

func (m Model) renderHeader(layout tableLayout) string {
	var b strings.Builder
	if layout.repo > 0 {
		b.WriteString(padCells("REPOSITORY", layout.repo))
		b.WriteString("  ")
	}
	b.WriteString("PR    ")
	b.WriteString("  ")
	b.WriteString("CI ")
	b.WriteString("  ")
	b.WriteString(padCells("TITLE", layout.title))
	if layout.author > 0 {
		b.WriteString("  ")
		b.WriteString(padCells("AUTHOR", layout.author))
	}
	if layout.updated {
		b.WriteString(" ")
		b.WriteString(padCells("UPDATED", 8))
	}
	return headerStyle.Width(m.width).Render(truncate(b.String(), m.width))
}

func (m Model) renderPRRow(pr gh.PullRequest, layout tableLayout) string {
	var b strings.Builder
	if layout.repo > 0 {
		b.WriteString(padCells(truncate(pr.Repository, layout.repo), layout.repo))
		b.WriteString("  ")
	}
	b.WriteString(fmt.Sprintf("#%-5d", pr.Number))
	b.WriteString("  ")
	b.WriteString(renderCI(pr.CI))
	b.WriteString("  ")
	title := pr.Title
	if pr.Draft {
		title = "[draft] " + title
	}
	b.WriteString(padCells(truncate(title, layout.title), layout.title))
	if layout.author > 0 {
		b.WriteString("  ")
		b.WriteString(padCells(truncate(pr.Author, layout.author), layout.author))
	}
	if layout.updated {
		b.WriteString(" ")
		b.WriteString(padCells(relativeTime(pr.UpdatedAt), 8))
	}
	return truncate(b.String(), m.width)
}

// tabLabel builds the rendered label for a tab. Render and mouse hitboxes
// must share this function so both see the same label at every width. When a
// full label would not fit its share of the row, the label compacts and
// truncates.
func (m Model) tabLabel(index int, view ViewData) string {
	budget := m.tabBudget()
	label := fmt.Sprintf("%d %s %d", index+1, view.View.Title, len(view.PRs))
	if lipgloss.Width(label) <= budget {
		return label
	}
	return truncate(fmt.Sprintf("%d %s", index+1, view.View.Title), budget)
}

func (m Model) tabBudget() int {
	count := max(1, len(m.views))
	available := m.width - (count - 1) // separators between tabs
	return max(6, available/count-tabPadding)
}

func (m Model) refreshAllCmd() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		return snapshotMsg(m.loader.RefreshAll(ctx))
	}
}

func (m Model) refreshOneCmd(index int) tea.Cmd {
	view := m.views[index].View
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		return viewMsg{index: index, snapshot: m.loader.RefreshOne(ctx, view)}
	}
}

func (m Model) tickCmd() tea.Cmd {
	return tea.Tick(m.refresh, func(time.Time) tea.Msg { return tickMsg{} })
}

func openBrowserCmd(url string) tea.Cmd {
	var command *exec.Cmd
	if runtime.GOOS == "darwin" {
		command = exec.Command("open", url)
	} else {
		command = exec.Command("xdg-open", url)
	}
	return tea.ExecProcess(command, func(err error) tea.Msg {
		return browserMsg{err: err}
	})
}

func renderCI(state gh.CIState) string {
	var icon string
	switch state {
	case gh.CISuccess:
		icon = lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Render("✓")
	case gh.CIPending:
		icon = lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Render("●")
	case gh.CIFailure, gh.CIError:
		icon = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render("✗")
	case gh.CINone:
		icon = dimStyle.Render("–")
	default:
		icon = dimStyle.Render("?")
	}
	return " " + icon + " "
}

// truncate shortens value to at most width terminal cells, adding an
// ellipsis when it cuts. It measures display width, so emoji, combining
// characters, and wide glyphs stay aligned.
func truncate(value string, width int) string {
	if lipgloss.Width(value) <= width {
		return value
	}
	if width <= 1 {
		return "…"
	}
	budget := width - 1
	used := 0
	var keep strings.Builder
	for _, r := range value {
		cellWidth := lipgloss.Width(string(r))
		if used+cellWidth > budget {
			break
		}
		keep.WriteRune(r)
		used += cellWidth
	}
	return keep.String() + "…"
}

// padCells right-pads value with spaces to exactly width terminal cells.
func padCells(value string, width int) string {
	padding := width - lipgloss.Width(value)
	if padding <= 0 {
		return value
	}
	return value + strings.Repeat(" ", padding)
}

func relativeTime(value time.Time) string {
	if value.IsZero() {
		return "–"
	}
	delta := time.Since(value)
	if delta < time.Minute {
		return "now"
	}
	if delta < time.Hour {
		return fmt.Sprintf("%dm", int(delta.Minutes()))
	}
	if delta < 24*time.Hour {
		return fmt.Sprintf("%dh", int(delta.Hours()))
	}
	if delta < 30*24*time.Hour {
		return fmt.Sprintf("%dd", int(delta.Hours()/24))
	}
	return value.Format("Jan 2")
}
