package board

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cdowell09/herdr-pr-board/internal/reviewmemory"
	tea "github.com/charmbracelet/bubbletea"
)

type notifierFake struct {
	runs []reviewmemory.Run
	err  error
}

func (f *notifierFake) Notify(_ context.Context, run reviewmemory.Run) error {
	f.runs = append(f.runs, run)
	return f.err
}

const notificationWarning = "review notifications unavailable: no herdr server"

func TestManualReviewNotifiesThroughConfiguredNotifier(t *testing.T) {
	notifier := &notifierFake{err: errors.New("no herdr server")}
	m := panelModel(t).WithNotifications(notifier)
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	if cmd == nil {
		t.Fatalf("review did not start: %q", next.(Model).region.message)
	}
	msg, ok := cmd().(reviewDoneMsg)
	if !ok || msg.run.ID != "attempt" || msg.err != nil || msg.notification == nil {
		t.Fatalf("done message = %+v", msg)
	}
	if len(notifier.runs) != 1 || notifier.runs[0].ID != "attempt" {
		t.Fatalf("notified runs = %+v", notifier.runs)
	}
	updated, _ := next.(Model).Update(msg)
	m = updated.(Model)
	if view := stripANSI(m.View()); !strings.Contains(view, "Status: completed; "+notificationWarning) {
		t.Fatalf("open panel hides the warning:\n%s", view)
	}
	if m.warning != "review: completed; "+notificationWarning {
		t.Fatalf("footer warning = %q", m.warning)
	}
}

func TestNotificationFailureWarnsOncePerBoardSession(t *testing.T) {
	m := panelModel(t).WithNotifications(&notifierFake{})
	done := reviewDoneMsg{url: m.region.pr.URL, run: reviewmemory.Run{ID: "attempt", Outcome: reviewmemory.Outcome{Status: reviewmemory.Completed}}, notification: errors.New("no herdr server")}
	updated, _ := m.Update(done)
	m = updated.(Model)
	if view := stripANSI(m.View()); !strings.Contains(view, notificationWarning) {
		t.Fatalf("first failure silent:\n%s", view)
	}
	updated, _ = m.Update(done)
	m = updated.(Model)
	if view := stripANSI(m.View()); strings.Contains(view, notificationWarning) || !strings.Contains(view, "Status: completed") {
		t.Fatalf("second failure warned again:\n%s", view)
	}
	if m = m.applyConfig(m.cfg, m.loader, m.refresh); m.notifier == nil {
		t.Fatal("configuration edit dropped the notifier")
	}
	if m := panelModel(t).WithNotifications(nil); m.notifier != nil {
		t.Fatalf("nil notifier kept %#v", m.notifier)
	}
}
