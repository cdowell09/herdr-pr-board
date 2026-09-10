package board

import (
	"strings"
	"testing"

	"github.com/cdowell09/herdr-pr-board/internal/reviewmemory"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type stopReviewBackend struct {
	reviewFake
	stopped string
	err     error
}

func (b *stopReviewBackend) Stop(id string) error { b.stopped = id; return b.err }

func TestReviewPanelStopsExactRunAsynchronously(t *testing.T) {
	for _, failure := range []error{nil, reviewmemory.ErrNotRunning, reviewmemory.ErrStopUnavailable} {
		m := panelModel(t)
		backend := &stopReviewBackend{err: failure}
		m.reviews = backend
		m.setRegionRuns(
			reviewmemory.Run{ID: "older-running", Outcome: reviewmemory.Outcome{Status: reviewmemory.Running}},
			reviewmemory.Run{ID: "newer-running", Outcome: reviewmemory.Outcome{Status: reviewmemory.Running}},
		)
		if !strings.Contains(stripANSI(m.View()), "t stop run newer-ru") {
			t.Fatalf("stop target not visible: %s", stripANSI(m.View()))
		}
		next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")})
		m = next.(Model)
		if cmd == nil || backend.stopped != "" || m.stopping[m.region.pr.URL] != "newer-running" {
			t.Fatal("stop did not capture target asynchronously")
		}
		if _, duplicate := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")}); duplicate != nil {
			t.Fatal("stop request duplicated")
		}
		next, refresh := m.Update(cmd())
		m = next.(Model)
		if backend.stopped != "newer-running" || refresh == nil {
			t.Fatal("wrong run stopped or history not refreshed")
		}
		if failure != nil {
			if m.stopping[m.region.pr.URL] != "" || !strings.Contains(m.region.message, failure.Error()) {
				t.Fatalf("stop error hidden: %+v", m.region)
			}
			continue
		}
		if !strings.Contains(m.region.message, "waiting for reviewer cleanup") {
			t.Fatal("accepted request presented as completed")
		}
		terminal := reviewmemory.Run{ID: "newer-running", Outcome: reviewmemory.Outcome{Status: reviewmemory.Failed, Message: reviewmemory.ErrStopped.Error()}}
		next, _ = m.Update(reviewOverviewMsg{epoch: m.epoch, read: overviewRead{regions: map[string]regionData{m.region.pr.URL: {runs: []reviewmemory.Run{terminal}}}, runs: map[string]reviewmemory.Run{terminal.ID: terminal}}})
		m = next.(Model)
		if m.stopping[m.region.pr.URL] != "" || !strings.Contains(m.region.message, reviewmemory.ErrStopped.Error()) {
			t.Fatal("final stop outcome missing")
		}
		if _, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")}); cmd != nil {
			t.Fatal("terminal review remained stoppable")
		}
	}
}

func TestReviewStopResultCannotOverwriteNewPanelOrFinishedRun(t *testing.T) {
	m := panelModel(t)
	m.setRegionRuns(reviewmemory.Run{ID: "active", Outcome: reviewmemory.Outcome{Status: reviewmemory.Running}})
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("t")})
	m = next.(Model)
	message := cmd()
	finished := reviewmemory.Run{ID: "active", Outcome: reviewmemory.Outcome{Status: reviewmemory.Completed, Message: "Finished before stop"}}
	next, _ = m.Update(reviewOverviewMsg{epoch: m.epoch, read: overviewRead{regions: map[string]regionData{m.region.pr.URL: {runs: []reviewmemory.Run{finished}}}, runs: map[string]reviewmemory.Run{finished.ID: finished}}})
	m = next.(Model)
	terminal := m.region.message
	next, _ = m.Update(message)
	m = next.(Model)
	if m.region.message != terminal {
		t.Fatal("late stop result overwrote terminal history")
	}
	// A result for a run other than the pending one is not this PR's answer.
	m.stopping[m.region.pr.URL] = "other"
	m.region.message = "pending other"
	next, _ = m.Update(message)
	if next.(Model).region.message != "pending other" {
		t.Fatal("stop response for another run changed the region")
	}
}

func TestStopControlPreservesPanelLayoutAndURLHitbox(t *testing.T) {
	for _, width := range []int{30, 60, 100} {
		m := panelModel(t)
		m.width = width
		m.setRegionRuns(reviewmemory.Run{ID: "active", Outcome: reviewmemory.Outcome{Status: reviewmemory.Running}})
		lines := strings.Split(m.View(), "\n")
		if len(lines) > m.height || stripANSI(lines[1]) != truncate(m.region.pr.URL, width) {
			t.Fatalf("width %d: stop control moved URL or overflowed height", width)
		}
		for _, line := range lines {
			if lipgloss.Width(line) > width {
				t.Fatalf("width %d: stop control overflowed: %q", width, line)
			}
		}
		opened := ""
		m.openBrowser = func(url string) tea.Cmd { opened = url; return nil }
		m.Update(tea.MouseMsg{X: 0, Y: 1, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
		if opened != m.region.pr.URL {
			t.Fatal("stop control changed URL hitbox")
		}
	}
}

func TestStopTargetRemainsVisibleOutsideHistoryViewport(t *testing.T) {
	for _, height := range []int{6, 20} {
		m := panelModel(t)
		m.width, m.height = 60, height
		m.setRegionRuns(
			reviewmemory.Run{ID: "active-123456", Outcome: reviewmemory.Outcome{Status: reviewmemory.Running}},
			reviewmemory.Run{ID: "completed-123456", Outcome: reviewmemory.Outcome{Status: reviewmemory.Completed, Message: strings.Repeat("Completed review details. ", 100)}},
		)
		view := stripANSI(m.View())
		if strings.Contains(view, "t stop run active-1") {
			t.Fatal("fixture must place the running review outside the history viewport")
		}
		if !strings.Contains(view, "t stop active-1") {
			t.Fatalf("height %d: footer does not identify the stop target: %s", height, view)
		}
	}
}
