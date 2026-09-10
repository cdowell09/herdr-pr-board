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
	"github.com/cdowell09/herdr-pr-board/internal/discovery"
	"github.com/cdowell09/herdr-pr-board/internal/dispatch"
	gh "github.com/cdowell09/herdr-pr-board/internal/github"
	"github.com/cdowell09/herdr-pr-board/internal/sidebar"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	tabRowY   = 1
	mouseStep = 3

	sidebarReportTimeout = 15 * time.Second
)

// tableHeaderRows is the height of the table header: the column row and its
// bottom border. A view with no rows renders neither, so its text has these
// rows too.
const tableHeaderRows = 2

// boardLayout is the row geometry shared by rendering and mouse hit-testing.
type boardLayout struct {
	firstPRRow     int
	selectedURLRow int
	visibleRows    int
	emptyRows      int
}

func (m Model) boardLayout() boardLayout {
	firstPRRow := 5
	if stale(m.currentView()) {
		firstPRRow++
	}
	detailRows := 0
	if pr, ok := m.selectedPR(); ok {
		detailRows = len(m.selectedReviewLines(pr))
	}
	// budget is the space between the first PR row and the footer. A view with
	// no rows renders no table header, so it keeps those rows too.
	budget := m.height - firstPRRow - 3 - len(m.footerHelpLines()) - detailRows
	visibleRows := max(1, budget)
	rows := m.filteredPRs()
	selectedURLRow := firstPRRow
	if len(rows) > 0 {
		selectedURLRow = firstPRRow + 1 + min(visibleRows, max(0, len(rows)-m.offset))
	}
	return boardLayout{
		firstPRRow:     firstPRRow,
		selectedURLRow: selectedURLRow,
		visibleRows:    visibleRows,
		emptyRows:      max(1, budget+tableHeaderRows),
	}
}

var (
	titleStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212"))
	activeTab     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("230")).Background(lipgloss.Color("62")).Padding(0, 1)
	inactiveTab   = lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Padding(0, 1)
	headerStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("252")).BorderBottom(true).BorderStyle(lipgloss.NormalBorder()).BorderForeground(lipgloss.Color("238"))
	selectedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("255")).Background(lipgloss.Color("237"))
	dimStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("242"))
	keyStyle      = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("252"))
	warningStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	errorStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	urlStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("81")).Underline(true)
)

// tabPadding is the horizontal padding the tab styles add to every label.
// Deriving it from the style keeps tab hitboxes correct if the padding
// changes.
var tabPadding = lipgloss.Width(inactiveTab.Render(""))

type snapshotMsg struct {
	discovery.Snapshot
	epoch uint64
}

type configEditMsg struct {
	cfg config.Config
	err error
}

type configRefreshMsg struct {
	cfg         config.Config
	loader      discovery.Loader
	refresh     time.Duration
	snapshot    discovery.Snapshot
	epoch       uint64
	selectedURL string
}

type viewMsg struct {
	index    int
	snapshot discovery.ViewSnapshot
	epoch    uint64
}

type tickMsg struct {
	epoch uint64
}

type browserMsg struct {
	err error
}

type sidebarMsg struct {
	err error
}

type keyHelpEntry struct {
	keys   string
	action string
}

// keyHelp is the single source of truth for the footer control list.
// The footer shows the most useful keys. The ? overlay shows all of them.
var keyHelp = []keyHelpEntry{
	{"Tab", "view"},
	{"↑↓", "select"},
	{"Enter", "open"},
	{"v", "reviews"},
	{"E", "config"},
	{"?", "help"},
}

// documentedKeys lists every key literal the board and review guides must document.
// The documentation drift test fails when one is missing. Add new bindings from
// updateKey, updateFilter, or updateReviewKey here, to helpSections, and to the
// corresponding guide.
var documentedKeys = []string{
	"1", "9", "Tab", "Shift+Tab", "h", "l", "←", "→",
	"j", "k", "↑", "↓", "g", "G", "Home", "End",
	"/", "?", "Enter", "Ctrl+U", "Esc", "Backspace", "E", "v",
	"n", "N", "s", "t", "c", "a", "x", "r", "R", "o", "q", "Ctrl+C",
}

// table tiers and their minimum terminal widths in cells.
const (
	tierWide   = 100
	tierMedium = 80
	tierNarrow = 60
)

type Model struct {
	reviewRows       map[string]reviewOverviewRow
	monitorStart     func() error
	monitorError     string
	autoCandidates   []dispatch.Candidate
	reviews          ReviewBackend
	publications     PublicationBackend
	stateDir         string
	reviewContext    context.Context
	reviewPanel      *reviewPanel
	reviewJobs       map[string]string
	reviewGeneration uint64
	cfg              config.Config
	configPath       string
	loader           discovery.Loader
	openBrowser      func(url string) tea.Cmd
	editConfig       func(path string) (notice string, cmd tea.Cmd)
	lookPath         func(string) (string, error) // repository setup probe; nil selects exec.LookPath
	refresh          time.Duration
	views            []discovery.ViewData
	active           int
	cursor           int
	offset           int
	helpOffset       int
	width            int
	height           int
	filter           string
	editorNotice     string
	editing          bool
	helpOverlay      bool
	loading          bool
	warning          string
	rates            gh.RateLimits
	sidebar          *sidebar.Reporter
	reporter         func(config.SidebarConfig) *sidebar.Reporter
	sidebarWarn      bool
	epoch            uint64
	observations     map[string]time.Time
}

// NewModel builds the board. A nil reporter disables sidebar reporting.
func NewModel(cfg config.Config, loader discovery.Loader, reporter *sidebar.Reporter) (Model, error) {
	return NewModelWithConfigPath(cfg, "", loader, func(settings config.SidebarConfig) *sidebar.Reporter {
		if reporter == nil || !settings.SidebarEnabled() {
			return nil
		}
		next := *reporter
		next.TTL, _ = settings.TTLEvery()
		return &next
	})
}

func NewModelWithConfigPath(cfg config.Config, configPath string, loader discovery.Loader, reporter func(config.SidebarConfig) *sidebar.Reporter) (Model, error) {
	refresh, err := cfg.RefreshEvery()
	if err != nil {
		return Model{}, err
	}
	views := make([]discovery.ViewData, len(cfg.Views))
	for i, view := range cfg.Views {
		views[i].View = view
	}
	var initialReporter *sidebar.Reporter
	if reporter != nil {
		initialReporter = reporter(cfg.Sidebar)
	}
	return Model{
		cfg:         cfg,
		configPath:  configPath,
		loader:      loader,
		openBrowser: openBrowserCmd,
		editConfig:  editConfigCmd,
		refresh:     refresh,
		views:       views,
		loading:     true,
		sidebar:     initialReporter,
		reporter:    reporter,
		epoch:       1,
	}, nil
}

func (m Model) Init() tea.Cmd {
	commands := []tea.Cmd{m.afterMonitorStart(m.observationCmd()), m.reviewOverviewCmd()}
	if m.tickInterval() > 0 {
		commands = append(commands, m.tickCmd())
	}
	return tea.Batch(commands...)
}

func (m Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	if next, cmd, handled := m.updateReviewOverview(message); handled {
		return next, cmd
	}
	if next, cmd, handled := m.updateMonitor(message); handled {
		return next, cmd
	}
	if next, cmd, handled := m.updateRepository(message); handled {
		return next, cmd
	}
	if next, cmd, handled := m.updatePublication(message); handled {
		return next, cmd
	}
	if next, cmd, handled := m.updateReview(message); handled {
		return next, cmd
	}
	switch msg := message.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.clampCursor()
		m.clampReviewOffset()
		m.clampHelpOffset()
		return m, nil
	case snapshotMsg:
		if msg.epoch != 0 && msg.epoch != m.epoch {
			return m, nil
		}
		current := m.observationCurrent(msg.FinishedAt)
		if current {
			m.autoCandidates = dispatch.Candidates(msg.Snapshot, m.cfg.Views, m.cfg.Review.AutoViews)
		}
		for i := range msg.Views {
			if !m.acceptObservation(msg.Views[i].View.ID, msg.FinishedAt) {
				msg.Views[i] = m.views[i]
				continue
			}
			m.settleView(&msg.Views[i], i)
		}
		m.views = msg.Views
		if current {
			m.rates = msg.Rates
			m.warning = discoveryWarnings(msg.Errors)
		}
		m.loading = false
		m.clampCursor()
		var cmd tea.Cmd
		if m.sidebar != nil && current {
			if tokens := sidebar.Tokens(m.cfg.Sidebar.ReviewView, adaptViews(m.views)); len(tokens) > 0 {
				cmd = m.sidebarReportCmd(tokens)
			}
		}
		return m, cmd
	case sidebarMsg:
		if msg.err == nil {
			m.sidebarWarn = false
		} else if !m.sidebarWarn {
			m.sidebarWarn = true
			m.warning = appendWarning(m.warning, "sidebar reporting unavailable: "+msg.err.Error())
		}
		return m, nil
	case viewMsg:
		if msg.epoch != 0 && msg.epoch != m.epoch {
			return m, nil
		}
		refresh := msg.snapshot
		current := m.observationCurrent(refresh.FinishedAt)
		if current {
			m.warning = discoveryWarnings(refresh.Errors)
			m.rates = refresh.Rates
		}
		if msg.index >= 0 && msg.index < len(m.views) && m.acceptObservation(refresh.Data.View.ID, refresh.FinishedAt) {
			data := refresh.Data
			m.invalidateAutomatic(refresh)
			m.settleView(&data, msg.index)
			m.views[msg.index] = data
		}
		m.loading = false
		m.clampCursor()
		return m, nil
	case observationUnchangedMsg:
		if msg.epoch == m.epoch {
			m.loading = false
		}
		return m, nil
	case tickMsg:
		if msg.epoch != 0 && msg.epoch != m.epoch {
			return m, nil
		}
		commands := []tea.Cmd{m.tickCmd()}
		if !m.loading {
			m.loading = true
			commands = append(commands, m.observationCmd())
		}
		return m, tea.Batch(commands...)
	case configRefreshMsg:
		return m.updateConfigRefresh(msg)
	case configEditMsg:
		return m.updateConfig(msg)
	case browserMsg:
		if msg.err != nil {
			m.warning = appendWarning(m.warning, "could not open the PR in a browser: "+msg.err.Error()+"; use the URL above or press Enter/click again")
		}
		return m, nil
	case tea.MouseMsg:
		if m.helpOverlay {
			return m.updateHelpMouse(msg)
		}
		if m.reviewPanel != nil {
			return m.updateReviewMouse(msg)
		}
		return m.updateMouse(msg)
	case tea.KeyMsg:
		if m.helpOverlay {
			return m.updateHelpKey(msg)
		}
		if m.reviewPanel != nil {
			return m.updateReviewKey(msg)
		}
		if m.editing {
			return m.updateFilter(msg)
		}
		return m.updateKey(msg)
	}
	return m, nil
}

// settleView preserves the previous successful observation after a failed search.
// Discovery supplies timestamps; the board does not advance them.
func (m Model) settleView(data *discovery.ViewData, index int) {
	if data.Err == nil {
		return
	}
	if index < len(m.views) {
		retainFrom(data, m.views[index])
	}
}

func (m Model) updateFilter(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "ctrl+c":
		return m, tea.Quit
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

func (m Model) updateConfig(message configEditMsg) (tea.Model, tea.Cmd) {
	// The editor exited, so the validation result replaces the launch notice.
	m.editorNotice = ""
	if message.err != nil {
		m.warning = appendWarning(m.warning, "configuration edit failed: "+message.err.Error())
		return m, nil
	}
	if message.cfg.Equal(m.cfg) {
		return m, m.startMonitorCmd()
	}
	refresh, err := message.cfg.RefreshEvery()
	if err != nil {
		m.warning = appendWarning(m.warning, "configuration edit failed: "+err.Error())
		return m, nil
	}
	nextLoader := m.loader.Reconfigured(message.cfg)
	if nextLoader == nil {
		m.warning = appendWarning(m.warning, "configuration editor cannot reload the board")
		return m, m.startMonitorCmd()
	}
	selectedURL := ""
	if pr, ok := m.selectedPR(); ok {
		selectedURL = pr.URL
	}
	m.epoch++
	m.loading = true
	return m, m.afterMonitorStart(m.refreshConfigCmd(message.cfg, nextLoader, refresh, selectedURL))
}

func (m Model) updateConfigRefresh(message configRefreshMsg) (tea.Model, tea.Cmd) {
	if message.epoch != 0 && message.epoch != m.epoch {
		return m, nil
	}
	if message.snapshot.CapacityErr != nil {
		m.loading = false
		m.warning = appendWarning(m.warning, discoveryWarnings(message.snapshot.Errors))
		if m.tickInterval() > 0 {
			return m, m.tickCmd()
		}
		return m, nil
	}

	m = m.applyConfig(message.cfg, message.loader, message.refresh)
	updated, command := m.Update(snapshotMsg{Snapshot: message.snapshot, epoch: m.epoch})
	model := updated.(Model)
	model.restoreSelection(message.selectedURL)
	if model.tickInterval() > 0 {
		if command == nil {
			command = model.tickCmd()
		} else {
			command = tea.Batch(command, model.tickCmd())
		}
	}
	return model, command
}

func (m Model) applyConfig(cfg config.Config, loader discovery.Loader, refresh time.Duration) Model {
	activeID := m.currentView().View.ID
	previous := make(map[string]discovery.ViewData, len(m.views))
	for _, view := range m.views {
		previous[view.View.ID] = view
	}
	nextViews := make([]discovery.ViewData, len(cfg.Views))
	active := 0
	for i, view := range cfg.Views {
		nextViews[i].View = view
		if old, ok := previous[view.ID]; ok {
			nextViews[i].PRs = old.PRs
			nextViews[i].UpdatedAt = old.UpdatedAt
			nextViews[i].ObservedAt = old.ObservedAt
		}
		if view.ID == activeID {
			active = i
		}
	}

	m.cfg = cfg
	m.autoCandidates = nil
	m.loader = loader
	m.refresh = refresh
	m.views = nextViews
	m.active = active
	if m.reporter != nil {
		m.sidebar = m.reporter(cfg.Sidebar)
	}
	m.sidebarWarn = false
	m.clampCursor()
	return m
}

func (m *Model) restoreSelection(url string) {
	if url != "" {
		for i, pr := range m.filteredPRs() {
			if pr.URL == url {
				m.cursor = i
				m.clampCursor()
				return
			}
		}
	}
	m.clampCursor()
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
	case "?":
		m.helpOverlay = true
	case "v":
		return m.openReviewPanel()
	case "E":
		if m.loading {
			return m, nil
		}
		if m.editConfig == nil {
			m.warning = appendWarning(m.warning, "configuration editor is unavailable")
			return m, nil
		}
		notice, command := m.editConfig(m.configPath)
		// The launch owns the notice, so the footer names the executable
		// that the board runs.
		m.editorNotice = notice
		return m, command
	case "r":
		requests := m.currentView().View.SearchRequestCount(len(m.cfg.GitHub.Scopes), m.cfg.GitHub.LimitPerScope)
		if !m.loading && m.rates.Search.HasCapacity(requests) {
			m.loading = true
			return m, m.refreshOneCmd(m.active)
		}
	case "R":
		if !m.loading && m.rates.Search.HasCapacity(m.cfg.SearchRequestCount()) {
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

	lay := m.boardLayout()
	rows := m.filteredPRs()
	row := m.offset + event.Y - lay.firstPRRow
	visible := min(lay.visibleRows, max(0, len(rows)-m.offset))
	if event.Y >= lay.firstPRRow && event.Y < lay.firstPRRow+visible && row >= 0 && row < len(rows) {
		m.cursor = row
		m.clampCursor()
		return m, nil
	}

	if event.Y == lay.selectedURLRow {
		if pr, ok := m.selectedPR(); ok {
			return m, m.openBrowser(pr.URL)
		}
	}
	return m, nil
}

// tabBar is one tab's rendered label and its column range on the tab row.
// renderTabs and tabAtX both come from tabBars, so a tab hitbox always
// covers exactly the rendered label.
type tabBar struct {
	label string
	x     int
	width int
}

// tabBars lays out the tabs across the tab row. The rendered labels and the
// mouse hitboxes share these positions. The x advance includes the single
// space renderTabs puts between tabs.
func (m Model) tabBars() []tabBar {
	bars := make([]tabBar, len(m.views))
	x := 0
	for i, view := range m.views {
		label := m.tabLabel(i, view)
		style := inactiveTab
		if i == m.active {
			style = activeTab
		}
		width := lipgloss.Width(style.Render(label))
		bars[i] = tabBar{label: label, x: x, width: width}
		x += width + 1
	}
	return bars
}

func (m Model) tabAtX(x int) (int, bool) {
	for i, bar := range m.tabBars() {
		if x >= bar.x && x < bar.x+bar.width {
			return i, true
		}
	}
	return 0, false
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
	visible := m.boardLayout().visibleRows
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+visible {
		m.offset = m.cursor - visible + 1
	}
	m.offset = max(0, min(m.offset, max(0, len(rows)-visible)))
}

func (m Model) View() string {
	if m.helpOverlay {
		return m.renderHelpOverlay()
	}
	if m.reviewPanel != nil {
		return m.renderReviewPanel()
	}
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
	output.WriteString(m.renderTable(m.boardLayout()))
	output.WriteString("\n" + m.renderSelected())
	output.WriteString("\n" + m.renderFooter())
	return output.String()
}

func (m Model) renderTabs() string {
	var tabs []string
	for i, bar := range m.tabBars() {
		if i == m.active {
			tabs = append(tabs, activeTab.Render(bar.label))
		} else {
			tabs = append(tabs, inactiveTab.Render(bar.label))
		}
	}
	return strings.Join(tabs, " ")
}

func (m Model) renderStaleNotice() string {
	view := m.currentView()
	if !stale(view) {
		return ""
	}
	notice := fmt.Sprintf("stale — showing %d rows retained from the last successful refresh", len(view.PRs))
	return warningStyle.Render(truncate(notice, m.width))
}

func (m Model) renderTable(lay boardLayout) string {
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
		return m.renderEmptyView(lay)
	}

	cols := m.tableLayout()
	header := m.renderHeader(cols)
	var output strings.Builder
	output.WriteString(header + "\n")
	end := min(m.offset+lay.visibleRows, len(rows))
	for i := m.offset; i < end; i++ {
		pr := rows[i]
		line := m.renderPRRow(pr, cols)
		if i == m.cursor {
			line = selectedStyle.Width(m.width).Render(line)
		}
		output.WriteString(line + "\n")
	}
	return output.String()
}

func (m Model) renderFooter() string {
	help := m.footerHelpLines()

	// Collect the meta parts, then join them. Prefixing a separator to each
	// part leaves a leading separator when an earlier part is absent.
	var parts []string
	if m.editorNotice != "" {
		// Keep the notice first. A narrow terminal truncates the tail.
		parts = append(parts, m.editorNotice)
	}
	if m.monitorError != "" {
		parts = append(parts, reviewText(m.monitorError))
	}
	if len(m.reviewJobs) > 0 {
		parts = append(parts, fmt.Sprintf("%d review requests · v reviews", len(m.reviewJobs)))
	}
	freshness := m.currentView().UpdatedAt
	if !freshness.IsZero() {
		parts = append(parts, "updated "+relativeTime(freshness))
	}
	if stale(m.currentView()) {
		parts = append(parts, "stale")
	}
	if m.rates.Search.Limit > 0 {
		parts = append(parts, fmt.Sprintf("Search %d/%d", m.rates.Search.Remaining, m.rates.Search.Limit))
	}
	if m.rates.GraphQL.Limit > 0 {
		parts = append(parts, fmt.Sprintf("GraphQL %d/%d", m.rates.GraphQL.Remaining, m.rates.GraphQL.Limit))
	}
	if m.warning != "" {
		parts = append(parts, m.warning)
	}
	meta := strings.Join(parts, " · ")
	return strings.Join(append(help, warningStyle.Render(truncate(meta, m.width))), "\n")
}

// footerHelpLines wraps the control reference at pair boundaries so each
// keybinding stays next to its action on narrow terminals. Keys render
// bright and actions dim so the two never blend together.
func (m Model) footerHelpLines() []string {
	width := max(1, m.width)
	lines := packLines(keyPairs(keyHelp), width)

	if m.editing {
		lines = append(lines, dimStyle.Render("filter: ")+truncate(m.filter+"▌", max(1, width-8)))
	} else if m.filter != "" {
		lines = append(lines, dimStyle.Render("filter: ")+truncate(m.filter, max(1, width-8)))
	}
	return lines
}

// keyPairs renders each control as a bright key and a dim action.
func keyPairs(entries []keyHelpEntry) []string {
	parts := make([]string, 0, len(entries))
	for _, entry := range entries {
		parts = append(parts, keyStyle.Render(entry.keys)+" "+dimStyle.Render(entry.action))
	}
	return parts
}

// keyLabels renders only the key literals. A pane that is too short for the
// actions keeps every key this way.
func keyLabels(entries []keyHelpEntry) []string {
	parts := make([]string, 0, len(entries))
	for _, entry := range entries {
		parts = append(parts, keyStyle.Render(entry.keys))
	}
	return parts
}

// packLines puts as many parts on each line as the width holds. A part never
// breaks, so no width separates a key from its action.
func packLines(parts []string, width int) []string {
	separator := dimStyle.Render(" · ")
	var lines []string
	current := ""
	for _, part := range parts {
		candidate := part
		if current != "" {
			candidate = current + separator + part
		}
		if lipgloss.Width(candidate) <= width {
			current = candidate
			continue
		}
		if current != "" {
			lines = append(lines, current)
		}
		current = part
	}
	if current != "" {
		lines = append(lines, current)
	}
	return lines
}

func (m Model) currentView() discovery.ViewData {
	if len(m.views) == 0 {
		return discovery.ViewData{}
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

// tabLabel builds the rendered label for a tab. Render and mouse hitboxes
// must share this function so both see the same label at every width. When a
// full label would not fit its share of the row, the label compacts and
// truncates.
func (m Model) tabLabel(index int, view discovery.ViewData) string {
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

// adaptViews converts retained view data into the sidebar token inputs.
func adaptViews(views []discovery.ViewData) []sidebar.View {
	adapted := make([]sidebar.View, len(views))
	for i, view := range views {
		adapted[i] = sidebar.View{ID: view.View.ID, PRs: view.PRs, Err: view.Err}
	}
	return adapted
}

// sidebarReportCmd reports the tokens to the workspace running the board.
// It returns a sidebarMsg so the model can warn once when reporting fails.
func (m Model) sidebarReportCmd(tokens map[string]string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), sidebarReportTimeout)
		defer cancel()
		return sidebarMsg{err: m.sidebar.Report(ctx, tokens)}
	}
}

func (m Model) refreshConfigCmd(cfg config.Config, loader discovery.Loader, refresh time.Duration, selectedURL string) tea.Cmd {
	epoch := m.epoch
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), discovery.RefreshAllTimeout)
		defer cancel()
		snapshot := loader.RefreshAll(ctx)
		return configRefreshMsg{cfg: cfg, loader: loader, refresh: refresh, snapshot: snapshot, epoch: epoch, selectedURL: selectedURL}
	}
}

func openBrowserCmd(url string) tea.Cmd {
	return tea.ExecProcess(browserCommand(runtime.GOOS, url), func(err error) tea.Msg {
		return browserMsg{err: err}
	})
}

func browserCommand(goos, url string) *exec.Cmd {
	if goos == "darwin" {
		return exec.Command("open", url)
	}
	if goos == "windows" {
		return exec.Command("rundll32.exe", "url.dll,FileProtocolHandler", url)
	}
	return exec.Command("xdg-open", url)
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

// Stale reports whether the view kept rows from an earlier refresh after a failed one.
func stale(v discovery.ViewData) bool {
	return v.Err != nil && len(v.PRs) > 0 && !v.UpdatedAt.IsZero()
}

// retainFrom keeps the previous view's rows and freshness after a failed refresh.
func retainFrom(v *discovery.ViewData, prev discovery.ViewData) {
	if !prev.UpdatedAt.IsZero() {
		v.PRs = prev.PRs
		v.ObservedAt = prev.ObservedAt
		v.UpdatedAt = prev.UpdatedAt
	}
}
