package board

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/cdowell09/herdr-pr-board/internal/config"
	"github.com/cdowell09/herdr-pr-board/internal/localstate"
	"github.com/cdowell09/herdr-pr-board/internal/monitor"
	"github.com/cdowell09/herdr-pr-board/internal/testutil"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "--monitor" && os.Getenv("PR_BOARD_MONITOR_ARGUMENTS") != "" {
		values := append([]string{os.Getenv("HERDR_PLUGIN_STATE_DIR")}, os.Args[1:]...)
		if err := os.WriteFile(os.Getenv("PR_BOARD_MONITOR_ARGUMENTS"), []byte(strings.Join(append(values, ""), "\x00")), 0600); err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestMonitorCommandRoundTripsHostileAndUnicodePaths(t *testing.T) {
	dir := t.TempDir()
	binary := testutil.Executable(t, dir, "executable with 'quotes' $dollars `backticks` 日本語")
	result := filepath.Join(dir, "arguments")
	t.Setenv("PR_BOARD_MONITOR_ARGUMENTS", result)
	t.Setenv("GORACE", os.Getenv("GORACE")+" atexit_sleep_ms=0")
	filename := "config with 'single' \"double\" $(not-a-command) 日本語.toml"
	if runtime.GOOS == "windows" {
		// Double quotes are not valid Windows filename characters.
		filename = strings.ReplaceAll(filename, "\"", "")
	}
	path := filepath.Join(dir, filename)
	state := filepath.Join(dir, "state with 'quotes' $value 日本語")
	command, err := monitorCommand(binary, path, state)
	if err != nil {
		t.Fatal(err)
	}
	for _, width := range []int{12, 30, 80} {
		lines := command.lines(width)
		for _, line := range lines {
			if ansi.StringWidth(line) > width {
				t.Fatalf("width%d: %q", width, line)
			}
		}
		shell := exec.Command("sh", "-c", strings.Join(lines, "\n"))
		if runtime.GOOS == "windows" {
			shell = exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", strings.Join(lines, "\n"))
		}
		output, err := shell.CombinedOutput()
		if err != nil {
			t.Fatalf("%v: %s\n%s", err, output, strings.Join(lines, "\n"))
		}
		output, err = os.ReadFile(result)
		if err != nil {
			t.Fatal(err)
		}
		want := strings.Join([]string{state, "--monitor", "--config", path, ""}, "\x00")
		if string(output) != want {
			t.Fatalf("changed args: %q want %q", output, want)
		}
	}
}

func onboardingModel(t *testing.T, width, height, views int) Model {
	t.Helper()
	m := panelModel(t)
	m.width, m.height = width, height
	for i := len(m.cfg.Views); i < views; i++ {
		m.cfg.Views = append(m.cfg.Views, config.View{ID: fmt.Sprintf("view-%02d", i), Title: "A configured view with a descriptive title", Query: "is:open", Scope: config.ScopeGlobal})
	}
	setup, err := newRepositorySetup(m.cfg, "acme/repo")
	if err != nil {
		t.Fatal(err)
	}
	setup.repo.AutoLaunch = true
	setup.repo.PublishActions = []config.PublicationAction{config.PublishComment}
	m.reviewPanel.setup = setup
	m.reviewPanel.monitor = monitor.Status{State: monitor.Stopped, Message: "start the monitor in another terminal"}
	root := filepath.VolumeName(t.TempDir()) + string(filepath.Separator)
	m.reviewPanel.monitorCommand, _ = monitorCommand(filepath.Join(root, "opt", "PR Board", "bin", "herdr-pr-board"), filepath.Join(root, "Users", "example user", "configuration with a long name", "config.toml"), filepath.Join(root, "Users", "example user", "plugin state with a long name"))
	return m
}

func TestOnboardingManyViewsRemainSelectableAndScrollable(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {30, 10}} {
		m := onboardingModel(t, size[0], size[1], 30)
		setup := m.reviewPanel.setup
		if len(setup.automatic.Selected) != 0 || setup.repo.AutoPublish != "" {
			t.Fatal("setup silently selected automation")
		}
		for row := repositoryViewsRow; row < setup.promptRow(); row++ {
			setup.row = row
			m.revealRepositoryRow()
			header, content, start, visible := m.repositoryViewport()
			y := -1
			for i := start; i < min(len(content), start+visible); i++ {
				if content[i].row == row {
					y = len(header) + i - start
					break
				}
			}
			if y < 0 {
				t.Fatalf("view row%d not visible", row)
			}
			updated, _ := m.Update(tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, Y: y, X: 1})
			m = updated.(Model)
			if setup.automatic.Selected[len(setup.automatic.Selected)-1] != setup.views[row-repositoryViewsRow].ID {
				t.Fatalf("wrong hitbox row%d", row)
			}
		}
		for i := 0; i < 100; i++ {
			updated, _ := m.Update(tea.MouseMsg{Button: tea.MouseButtonWheelDown})
			m = updated.(Model)
		}
		bottom := setup.offset
		updated, _ := m.Update(tea.MouseMsg{Button: tea.MouseButtonWheelUp})
		m = updated.(Model)
		if setup.offset >= bottom {
			t.Fatal("overscroll prevented upward movement")
		}
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnd})
		m = updated.(Model)
		rendered := stripANSI(m.View())
		if len(strings.Split(rendered, "\n")) > size[1] {
			t.Fatalf("height overflow:\n%s", rendered)
		}
		for _, line := range strings.Split(rendered, "\n") {
			if ansi.StringWidth(line) > size[0] {
				t.Fatalf("width overflow %q", line)
			}
		}
		// The selected automatic views keep the monitor command in this panel.
		commandLines := m.monitorCommandLines()
		end := len(commandLines) - 1
		if runtime.GOOS == "windows" {
			end--
		}
		if end < 0 || !strings.Contains(rendered, commandLines[end]) {
			t.Fatalf("command end unreachable:\n%s", rendered)
		}
		selected := setup.automatic.Selected
		setup.repo.AutoLaunch, setup.automatic.Selected = false, nil
		if rendered := stripANSI(m.View()); strings.Contains(rendered, "Run in another terminal") {
			t.Fatalf("manual setup shows the monitor command:\n%s", rendered)
		}
		setup.repo.AutoLaunch, setup.automatic.Selected = true, selected
		updated, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
		m = updated.(Model)
		_, _, start, _ := m.repositoryViewport()
		if setup.offset != start {
			t.Fatal("resize did not clamp stored offset")
		}
	}
}

func TestSetupSummariesDoNotConfusePermissionWithScheduling(t *testing.T) {
	m := onboardingModel(t, 80, 40, 3)
	setup := m.reviewPanel.setup
	lines := stripANSI(m.View())
	for _, want := range []string{"Waiting: select global views", "[x] Comments", "After review: Keep local", "Monitor: stopped"} {
		if !strings.Contains(lines, want) {
			t.Fatalf("missing %q: %s", want, lines)
		}
	}
	setup.automatic.Selected = []string{"review"}
	m.reviewPanel.monitor = monitor.Status{State: monitor.Running, Message: "configuration differs"}
	if lines := stripANSI(m.View()); !strings.Contains(lines, "Waiting: wait for a matching observation") || strings.Contains(lines, "Setup ready") {
		t.Fatalf("lock alone implied readiness: %s", lines)
	}
	setup.repo.AutoLaunch = false
	if lines := stripANSI(m.View()); !strings.Contains(lines, "Manual reviews ready. Press Enter to save, then n to run.") || strings.Contains(lines, "Waiting:") {
		t.Fatalf("manual setup still reads as blocked: %s", lines)
	}
	m.cfg.Repositories = []config.Repository{setup.repo}
	m.cfg.Review.AutoViews = setup.automatic.Selected
	m.reviewPanel.setup = nil
	lines = stripANSI(strings.Join(m.reviewLines(), "\n"))
	for _, want := range []string{"Waiting: enable automatic launches", "Automatic launches: off", "After review: keep local"} {
		if !strings.Contains(lines, want) {
			t.Fatalf("missing %q: %s", want, lines)
		}
	}
}

func repositoryText(lines []repositoryLine) string {
	text := make([]string, 0, len(lines))
	for _, line := range lines {
		text = append(text, line.text)
	}
	return stripANSI(strings.Join(text, "\n"))
}

func TestSetupWithoutAutomationInvitesManualReview(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {30, 10}} {
		ready, running := "Manual reviews ready. Press Enter to save, then n to run.", "Monitor: stopped"
		if size[0] < 60 {
			ready, running = "Ready · Enter save · n run", "Settings · monitor stopped"
		}
		m := onboardingModel(t, size[0], size[1], 3)
		setup := m.reviewPanel.setup
		setup.repo.AutoLaunch = false
		header, content, _, _ := m.repositoryViewport()
		top, body := stripANSI(strings.Join(header, "\n")), repositoryText(content)
		if !strings.Contains(top, ready) {
			t.Fatalf("width%d: manual invitation missing:\n%s", size[0], top)
		}
		for _, unwanted := range []string{"waiting:", "monitor"} {
			if strings.Contains(strings.ToLower(top), unwanted) {
				t.Fatalf("width%d: header keeps %q:\n%s", size[0], unwanted, top)
			}
		}
		for _, unwanted := range []string{"Run in another terminal", "start the monitor in another terminal"} {
			if strings.Contains(body, unwanted) {
				t.Fatalf("width%d: content keeps %q:\n%s", size[0], unwanted, body)
			}
		}
		m.monitorError = "Monitor startup failed"
		_, content, _, _ = m.repositoryViewport()
		if !strings.Contains(repositoryText(content), m.monitorError) {
			t.Fatalf("width%d: startup failure hidden:\n%s", size[0], repositoryText(content))
		}
		m.monitorError = ""
		// Either automation choice makes the monitor state relevant again.
		for _, enable := range []func(){func() { setup.repo.AutoLaunch = true }, func() {
			setup.repo.AutoLaunch, setup.automatic.Selected = false, []string{setup.views[0].ID}
		}} {
			enable()
			header, content, _, _ = m.repositoryViewport()
			top, body = stripANSI(strings.Join(header, "\n")), repositoryText(content)
			if !strings.Contains(top, running) {
				t.Fatalf("width%d: monitor state hidden:\n%s", size[0], top)
			}
			if !strings.Contains(body, "Run in another terminal:") {
				t.Fatalf("width%d: stopped monitor kept its command hidden:\n%s", size[0], body)
			}
		}
	}
}

func TestOnboardingSaveAndStatusRefresh(t *testing.T) {
	m := panelModel(t)
	m.configPath = filepath.Join(t.TempDir(), "config.toml")
	m.stateDir = t.TempDir()
	cfg, err := config.Load(m.configPath)
	if err != nil {
		t.Fatal(err)
	}
	setup, err := newRepositorySetup(cfg, m.reviewPanel.pr.Repository)
	if err != nil {
		t.Fatal(err)
	}
	m.reviewPanel.setup = setup
	setup.repo.AutoLaunch = true
	setup.repo.PublishActions = []config.PublicationAction{config.PublishComment}
	setup.row = repositoryViewsRow + 1
	setup.toggle() // The explicitly selected existing review view.
	updated, save := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	updated, _ = m.Update(save())
	m = updated.(Model)
	loaded, err := config.LoadExisting(m.configPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Review.AutoViews) != 1 || loaded.Review.AutoViews[0] != "review" {
		t.Fatalf("unsaved selection: %+v", loaded.Review)
	}
	if m.reviewPanel.setup != nil || len(m.cfg.Review.AutoViews) != 1 {
		t.Fatal("saved selection not reflected")
	}
	updated, _, _ = m.updateReview(m.monitorStatusCmd()())
	m = updated.(Model)
	if m.reviewPanel.monitor.State != monitor.Stopped || m.reviewPanel.monitorCommand.path != m.configPath {
		t.Fatalf("status %+v", m.reviewPanel.monitor)
	}
	if !strings.Contains(strings.Join(m.reviewLines(), "\n"), "After review: keep local") {
		t.Fatal("saved local-only publication unclear")
	}
	owner, err := localstate.TryLock(filepath.Join(m.stateDir, "monitor.lock"))
	if err != nil {
		t.Fatal(err)
	}
	updated, _, _ = m.updateReview(m.monitorStatusCmd()())
	m = updated.(Model)
	if m.reviewPanel.monitor.State != monitor.Running || m.reviewPanel.monitor.ObservationOK {
		t.Fatal("status did not refresh or fabricated observation")
	}
	owner.Close()
	updated, _, _ = m.updateReview(m.monitorStatusCmd()())
	m = updated.(Model)
	if m.reviewPanel.monitor.State != monitor.Stopped {
		t.Fatal("stop not reflected")
	}
	// View uses the captured state and never reloads the now-missing configuration.
	if err := os.Remove(m.configPath); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stripANSI(m.View()), "Monitor: stopped") {
		t.Fatal("render lost captured status")
	}
}

func TestMonitorCommandRejectsControlPaths(t *testing.T) {
	for _, path := range []string{"/tmp/escape\x1b[2J", "/tmp/tab\there", "/tmp/line\nbreak"} {
		if _, err := monitorCommand("/bin/board", path, "/tmp/state"); err == nil {
			t.Fatalf("unsafe display accepted: %q", path)
		}
	}
}
