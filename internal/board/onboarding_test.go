package board

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cdowell09/herdr-pr-board/internal/config"
	"github.com/cdowell09/herdr-pr-board/internal/localstate"
	"github.com/cdowell09/herdr-pr-board/internal/monitor"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestMonitorCommandRoundTripsHostileAndUnicodePaths(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "executable with 'quotes' $dollars `backticks` 日本語")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nprintf '%s\\0' \"$HERDR_PLUGIN_STATE_DIR\" \"$@\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config with 'single' \"double\" $(not-a-command) 日本語.toml")
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
		output, err := exec.Command("sh", "-c", strings.Join(lines, "\n")).CombinedOutput()
		if err != nil {
			t.Fatalf("%v: %s\n%s", err, output, strings.Join(lines, "\n"))
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
	m.reviewPanel.monitorCommand, _ = monitorCommand("/opt/PR Board/bin/herdr-pr-board", "/Users/example user/configuration with a long name/config.toml", "/Users/example user/plugin state with a long name")
	return m
}

func TestOnboardingManyViewsRemainSelectableAndScrollable(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {30, 10}} {
		m := onboardingModel(t, size[0], size[1], 30)
		setup := m.reviewPanel.setup
		if len(setup.automatic.Selected) != 0 || setup.repo.AutoPublish != "" {
			t.Fatal("setup silently selected automation")
		}
		for row := 6; row < len(setup.rows()); row++ {
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
			if setup.automatic.Selected[len(setup.automatic.Selected)-1] != setup.views[row-6].ID {
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
		if !strings.Contains(rendered, "config.toml") && !strings.Contains(rendered, "g.toml") {
			t.Fatalf("command end unreachable:\n%s", rendered)
		}
		updated, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
		m = updated.(Model)
		_, _, start, _ := m.repositoryViewport()
		if setup.offset != start {
			t.Fatal("resize did not clamp stored offset")
		}
	}
}

func TestSetupSummariesDoNotConfusePermissionWithScheduling(t *testing.T) {
	m := onboardingModel(t, 80, 24, 3)
	setup := m.reviewPanel.setup
	lines := strings.Join(m.monitorLines(setup.repo, setup.automatic.Selected), "\n")
	for _, want := range []string{"select global views", "Comments are allowed, but automatic posting is off", "Monitor: stopped"} {
		if !strings.Contains(lines, want) {
			t.Fatalf("missing %q: %s", want, lines)
		}
	}
	setup.automatic.Selected = []string{"review"}
	m.reviewPanel.monitor = monitor.Status{State: monitor.Running, Message: "configuration differs"}
	if strings.Contains(strings.Join(m.monitorLines(setup.repo, setup.automatic.Selected), "\n"), "setup is ready") {
		t.Fatal("lock alone implied readiness")
	}
	setup.repo.AutoLaunch = false
	if !strings.Contains(strings.Join(m.monitorLines(setup.repo, setup.automatic.Selected), "\n"), "Manual review only") {
		t.Fatal("manual mode unclear")
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
	setup.row = 7
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
	if !strings.Contains(strings.Join(m.reviewLines(), "\n"), "Comments are allowed, but automatic posting is off") {
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
