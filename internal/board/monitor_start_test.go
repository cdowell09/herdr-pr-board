package board

import (
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/cdowell09/herdr-pr-board/internal/config"
	"github.com/cdowell09/herdr-pr-board/internal/discovery"
	tea "github.com/charmbracelet/bubbletea"
)

type startupCommands struct {
	command   tea.Cmd
	remaining int
	messages  []tea.Msg
}

func (m startupCommands) Init() tea.Cmd { return m.command }
func (m startupCommands) View() string  { return "" }
func (m startupCommands) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg.(type) {
	case monitorStartedMsg, snapshotMsg, configRefreshMsg, monitorStatusMsg:
		m.messages = append(m.messages, msg)
		m.remaining--
		if m.remaining == 0 {
			return m, tea.Quit
		}
	}
	return m, nil
}
func executeStartupCommands(t *testing.T, command tea.Cmd, count int) []tea.Msg {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	p := tea.NewProgram(startupCommands{command: command, remaining: count}, tea.WithContext(ctx), tea.WithInput(strings.NewReader("")), tea.WithOutput(io.Discard), tea.WithoutRenderer(), tea.WithoutSignalHandler())
	result, err := p.Run()
	if err != nil {
		t.Fatal(err)
	}
	return result.(startupCommands).messages
}

type startupOrderLoader struct {
	order    *[]string
	snapshot discovery.Snapshot
}

func (l startupOrderLoader) RefreshAll(context.Context) discovery.Snapshot {
	*l.order = append(*l.order, "discovery")
	return l.snapshot
}
func (l startupOrderLoader) RefreshOne(context.Context, config.View) discovery.ViewSnapshot {
	return discovery.ViewSnapshot{}
}
func (l startupOrderLoader) Reconfigured(config.Config) discovery.Loader { return l }

func TestBoardStartsMonitorBeforeInitialAndReloadDiscovery(t *testing.T) {
	for _, reload := range []bool{false, true} {
		t.Run(map[bool]string{false: "open", true: "reload failure"}[reload], func(t *testing.T) {
			var order []string
			loader := startupOrderLoader{order: &order, snapshot: discovery.Snapshot{CapacityErr: errors.New("discovery unavailable")}}
			m, err := NewModel(testConfig(), loader, nil)
			if err != nil {
				t.Fatal(err)
			}
			m.refresh = 0
			m = m.WithMonitorStarter(func() error { order = append(order, "start"); return nil })
			command := m.Init()
			if reload {
				cfg := m.cfg
				cfg.UI.Title = "Changed title"
				_, command = m.Update(configEditMsg{cfg: cfg})
			}
			messages := executeStartupCommands(t, command, 2)
			if !reflect.DeepEqual(order, []string{"start", "discovery"}) {
				t.Fatalf("order=%v messages=%v", order, messages)
			}
		})
	}
}

func TestBoardSavedSettingsStartEvenAfterPanelCloses(t *testing.T) {
	m := panelModel(t)
	calls := 0
	m = m.WithMonitorStarter(func() error { calls++; return nil })
	m.reviewPanel = nil
	saved := m.cfg
	saved.Review.AutoViews = []string{saved.Views[0].ID}
	saved.Repositories = []config.Repository{{Name: "owner/repo", AutoLaunch: true}}
	updated, command := m.Update(repositorySavedMsg{cfg: saved, url: "closed panel"})
	m = updated.(Model)
	if !reflect.DeepEqual(m.cfg.Review.AutoViews, saved.Review.AutoViews) || !m.cfg.Repositories[0].AutoLaunch {
		t.Fatal("closed panel discarded saved automation settings")
	}
	executeStartupCommands(t, command, 1)
	if calls != 1 {
		t.Fatal("successful save did not start monitor")
	}
	_, command = m.Update(repositorySavedMsg{cfg: m.cfg, err: errors.New("stale save")})
	if command != nil {
		t.Fatal("failed save attempted startup")
	}
	_, command = m.Update(configEditMsg{cfg: m.cfg})
	executeStartupCommands(t, command, 1)
	if calls != 2 {
		t.Fatal("unchanged reload did not retry stopped monitor")
	}
}

func TestMonitorStartupErrorPersistsAcrossObservationAndStatus(t *testing.T) {
	m := panelModel(t)
	next, _ := m.Update(monitorStartedMsg{err: errors.New("timeout; log: /state/monitor.log")})
	m = next.(Model)
	next, _ = m.Update(monitorStatusMsg{url: m.reviewPanel.pr.URL, generation: m.reviewGeneration})
	m = next.(Model)
	if !strings.Contains(stripANSI(m.View()), "Monitor startup failed") {
		t.Fatal("status erased startup failure")
	}
	m.reviewPanel = nil
	m.width = 120
	next, _ = m.Update(snapshotMsg{Snapshot: discovery.Snapshot{FinishedAt: time.Now()}})
	m = next.(Model)
	if !strings.Contains(stripANSI(m.View()), "Monitor startup failed") {
		t.Fatal("discovery erased startup failure")
	}
	next, _ = m.Update(monitorStartedMsg{})
	m = next.(Model)
	if m.monitorError != "" {
		t.Fatal("successful retry did not clear error")
	}
}

func TestUnwiredModelsDoNotStartMonitors(t *testing.T) {
	m := panelModel(t)
	if m.startMonitorCmd() != nil {
		t.Fatal("model started a monitor without interactive wiring")
	}
}

func TestFailedConfigReloadDoesNotStartMonitor(t *testing.T) {
	m := panelModel(t).WithMonitorStarter(func() error {
		t.Fatal("invalid configuration attempted monitor startup")
		return nil
	})
	updated, command := m.Update(configEditMsg{err: errors.New("invalid saved configuration")})
	if command != nil {
		t.Fatal("failed reload returned a startup command")
	}
	if !strings.Contains(updated.(Model).warning, "invalid saved configuration") {
		t.Fatal("failed reload lost its diagnostic")
	}
}

func TestUnwiredSavedSettingsDoNotStartMonitor(t *testing.T) {
	m := panelModel(t)
	m.reviewPanel = nil
	_, command := m.Update(repositorySavedMsg{cfg: m.cfg})
	if command != nil {
		t.Fatal("unwired settings save returned a startup command")
	}
}
