package board

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/cdowell09/herdr-pr-board/internal/config"
	gh "github.com/cdowell09/herdr-pr-board/internal/github"
	"github.com/cdowell09/herdr-pr-board/internal/reviewmemory"
	tea "github.com/charmbracelet/bubbletea"
)

type launchFake struct {
	shortcuts []config.Shortcut
	prs       []gh.PullRequest
	err       error
}

func (f *launchFake) launch(_ context.Context, shortcut config.Shortcut, pr gh.PullRequest) error {
	f.shortcuts = append(f.shortcuts, shortcut)
	f.prs = append(f.prs, pr)
	return f.err
}

var piReview = config.Shortcut{Key: "r", Name: "Pi review", Run: "pi {url}", Where: config.ShortcutTab}

func withShortcut(m Model, fake *launchFake) Model {
	m.cfg.Shortcuts = []config.Shortcut{piReview}
	return m.WithShortcutLauncher(fake.launch)
}

// settle runs a command and feeds its message back, as the program does.
func settle(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	if cmd == nil {
		t.Fatal("no command")
	}
	next, _ := m.Update(cmd())
	return next.(Model)
}

func TestShortcutKeyLaunchesForTheSelectedPR(t *testing.T) {
	fake := &launchFake{}
	m := withShortcut(layoutModel(t, 120), fake)
	url := selectedURL(t, m)
	next, cmd := m.Update(runes("r"))
	m = settle(t, next.(Model), cmd)
	if len(fake.prs) != 1 || fake.prs[0].URL != url || fake.shortcuts[0] != piReview {
		t.Fatalf("launched %+v for %+v, want %s", fake.shortcuts, fake.prs, url)
	}
	pr := fake.prs[0]
	if want := "Pi review → " + pr.Repository + "#" + strconv.Itoa(pr.Number); !strings.Contains(stripANSI(m.View()), want) {
		t.Fatalf("launch notice %q missing:\n%s", want, stripANSI(m.View()))
	}

	fake.err = errors.New("no herdr server")
	next, cmd = m.Update(runes("r"))
	m = settle(t, next.(Model), cmd)
	view := stripANSI(m.View())
	if !strings.Contains(view, "Pi review failed: no herdr server") || strings.Contains(view, "Pi review →") {
		t.Fatalf("launch failure did not replace the notice with a warning:\n%s", view)
	}
}

func TestHelpOverlayListsCommandShortcuts(t *testing.T) {
	m := press(withShortcut(layoutModel(t, 120), &launchFake{}), runes("?"), runes("G"))
	if view := stripANSI(m.View()); !strings.Contains(view, "Command shortcuts") || !strings.Contains(view, "r  Pi review") {
		t.Fatalf("help overlay lacks the shortcut:\n%s", view)
	}
	// The controls that work everywhere stay last, after the shortcuts.
	var plain []string
	for _, line := range m.helpLayout().lines {
		plain = append(plain, stripANSI(line))
	}
	content := strings.Join(plain, "\n")
	if strings.Index(content, "Command shortcuts") > strings.Index(content, "General") {
		t.Fatalf("command shortcuts come after the general controls:\n%s", content)
	}
}

func TestShortcutFooterHintClickLaunches(t *testing.T) {
	fake := &launchFake{}
	m := withShortcut(layoutModel(t, 120), fake)
	m, cmd := clickText(t, m, "r Pi review")
	settle(t, m, cmd)
	if len(fake.prs) != 1 {
		t.Fatalf("footer click launched %d times", len(fake.prs))
	}
}

func TestShortcutLaunchesInZoomForTheZoomedPR(t *testing.T) {
	fake := &launchFake{}
	m := withShortcut(panelModel(t), fake)
	if !strings.Contains(stripANSI(m.View()), "r Pi review") {
		t.Fatalf("zoom footer lacks the shortcut:\n%s", stripANSI(m.View()))
	}
	next, cmd := m.Update(runes("r"))
	settle(t, next.(Model), cmd)
	if len(fake.prs) != 1 || fake.prs[0].URL != m.region.pr.URL {
		t.Fatalf("zoom launched for %+v", fake.prs)
	}
}

func TestShortcutDoesNotFireInFilterInputOrAConfirmation(t *testing.T) {
	fake := &launchFake{}
	m := withShortcut(layoutModel(t, 120), fake)
	m = press(m, runes("/"), runes("r"))
	if !strings.Contains(stripANSI(m.View()), "/ r▌") || len(fake.prs) != 0 {
		t.Fatal("r in filter input did not type r")
	}

	m = withShortcut(panelModel(t), fake)
	m = m.WithPublications(t.TempDir(), &publicationFake{})
	m.setRegionRuns(reviewmemory.Run{ID: "completed", Outcome: reviewmemory.Outcome{Status: reviewmemory.Completed}})
	m = press(m, runes("a"))
	if next, cmd := m.Update(runes("r")); cmd != nil || len(fake.prs) != 0 || strings.Contains(stripANSI(next.View()), "enter/y") {
		t.Fatal("r during a confirmation launched or kept the prompt")
	}
}

func TestCtrlRRefreshesTheCurrentView(t *testing.T) {
	m := layoutModel(t, 120)
	m.rates.Search = gh.RateResource{Limit: 30, Remaining: 30}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	if cmd == nil || !next.(Model).loading {
		t.Fatal("ctrl+r did not refresh the current view")
	}
}

// TestUnreservedKeysDoNothing fails when the board binds a key that
// config.ReservedKeys does not list, so a shortcut could not silently shadow
// a built-in key.
func TestUnreservedKeysDoNothing(t *testing.T) {
	boards := map[string]Model{"board": splitModel(t, &reviewFake{}), "zoom": panelModel(t)}
	for name, m := range boards {
		m.rates.Search = gh.RateResource{Limit: 30, Remaining: 30}
		before := m.View()
		for r := rune('!'); r <= '~'; r++ {
			if config.Reserved(string(r)) {
				continue
			}
			next, cmd := m.Update(runes(string(r)))
			if cmd != nil || next.View() != before {
				t.Fatalf("%s: unreserved key %q acted", name, r)
			}
		}
	}
}
