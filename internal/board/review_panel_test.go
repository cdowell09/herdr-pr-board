package board

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cdowell09/herdr-pr-board/internal/config"
	gh "github.com/cdowell09/herdr-pr-board/internal/github"
	"github.com/cdowell09/herdr-pr-board/internal/review"
	"github.com/cdowell09/herdr-pr-board/internal/reviewmemory"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type reviewFake struct{ request review.Request }

func (*reviewFake) History(string) ([]reviewmemory.Run, error) { return nil, nil }
func (f *reviewFake) Review(_ context.Context, request review.Request, _ func(string)) (reviewmemory.Run, error) {
	f.request = request
	return reviewmemory.Run{ID: "attempt", Outcome: reviewmemory.Outcome{Status: reviewmemory.Completed, Message: "done", Findings: []reviewmemory.Finding{}}}, nil
}
func (*reviewFake) RunDirectory(id string) string { return "/state/reviews/" + id }

func panelModel(t *testing.T) Model {
	t.Helper()
	m, err := NewModel(testConfig(), fakeLoader{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	m = m.WithReviews(context.Background(), &reviewFake{})
	m.cfg.Repositories = []config.Repository{{Name: "acme/repo", Reviewer: "agent"}}
	m.width, m.height = 100, 35
	m.loading = false
	m.views[0].PRs = []gh.PullRequest{{Repository: "acme/repo", Number: 1, URL: "https://github.com/acme/repo/pull/1", HeadOID: strings.Repeat("a", 40), BaseRefName: "main", MetadataObservedAt: time.Now()}}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("v")})
	loaded, _ := next.(Model).Update(repositorySettingsMsg{url: m.views[0].PRs[0].URL, cfg: m.cfg})
	return loaded.(Model)
}

func TestReviewPanelHistoryCoordinatesAndControls(t *testing.T) {
	m := panelModel(t)
	current := reviewmemory.Run{ID: "current", Reviewer: "agent", StartedAt: time.Now(), Identity: reviewmemory.Identity{HeadOID: strings.Repeat("a", 40), BaseRefName: "main"}, Outcome: reviewmemory.Outcome{Status: reviewmemory.Completed, Message: "Summary", Findings: []reviewmemory.Finding{{Severity: "P2", Title: "Finding", Body: "Diagnostic details", Path: "main.go", Line: 2}}}}
	old := current
	old.ID = "old"
	old.Identity.HeadOID = strings.Repeat("b", 40)
	m.reviewPanel.runs = []reviewmemory.Run{old, current}
	view := stripANSI(m.View())
	for _, value := range []string{"current observed revision", "older revision", "P2 Finding", "Diagnostic details", "main.go:2", "Diagnostics:"} {
		if !strings.Contains(view, value) {
			t.Fatalf("missing %q:\n%s", value, view)
		}
	}
	lines := strings.Split(view, "\n")
	if lines[1] != m.reviewPanel.pr.URL {
		t.Fatalf("URL coordinate drift: %q", lines[1])
	}
	opened := ""
	m.openBrowser = func(url string) tea.Cmd { opened = url; return nil }
	m.Update(tea.MouseMsg{X: 0, Y: 1, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if opened != m.reviewPanel.pr.URL {
		t.Fatal("click did not open rendered URL")
	}
	for _, width := range []int{30, 60, 100} {
		m.width = width
		for _, line := range strings.Split(m.View(), "\n") {
			if lipgloss.Width(line) > width {
				t.Fatalf("width %d overflow %q", width, line)
			}
		}
	}
}

func TestReviewPanelQueuesWithoutBlockingAndExplicitlyReruns(t *testing.T) {
	m := panelModel(t)
	next, command := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("N")})
	m = next.(Model)
	if command == nil || !strings.Contains(m.View(), "queued") {
		t.Fatal("launch did not show queued state")
	}
	if _, duplicate := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")}); duplicate != nil {
		t.Fatal("duplicate request queued")
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(Model)
	if m.reviewPanel != nil || len(m.reviewJobs) != 1 {
		t.Fatal("closing panel stopped the request")
	}
	msg := command()
	next, _ = m.Update(msg)
	m = next.(Model)
	if len(m.reviewJobs) != 0 || !m.reviews.(*reviewFake).request.Rerun {
		t.Fatal("explicit rerun or completion lost")
	}
}

func TestReviewPanelDoesNotMultiplyPollingAndStripsControlText(t *testing.T) {
	m := panelModel(t)
	url := m.reviewPanel.pr.URL
	next, cmd := m.Update(reviewHistoryMsg{url: url})
	if cmd != nil {
		t.Fatal("history response created another polling loop")
	}
	m = next.(Model)
	if _, cmd := m.Update(reviewTickMsg{url: url, generation: m.reviewGeneration - 1}); cmd != nil {
		t.Fatal("old panel poll survived")
	}
	m.reviewPanel.message = "untrusted\x1b]52;c;payload\a\x1b[2Jtext"
	if strings.Contains(reviewText(m.reviewPanel.message), "\x1b") {
		t.Fatal("terminal control text survived")
	}
}

func TestReviewPanelUsesLatestSuccessfulBoardRevision(t *testing.T) {
	m := panelModel(t)
	m.reviewPanel.runs = []reviewmemory.Run{{Identity: reviewmemory.Identity{HeadOID: strings.Repeat("a", 40), BaseRefName: "main"}, Outcome: reviewmemory.Outcome{Status: reviewmemory.Completed}}}
	if !strings.Contains(m.View(), "current observed revision") {
		t.Fatal("current revision missing")
	}
	m.views[0].PRs[0].HeadOID = strings.Repeat("b", 40)
	if strings.Contains(m.View(), "current observed revision") || !strings.Contains(m.View(), "older revision") {
		t.Fatal("panel retained superseded revision")
	}
	m.views[0].Err = errors.New("refresh failed")
	if !strings.Contains(m.View(), "current revision unknown") {
		t.Fatal("failed observation appears current")
	}
}

func TestReviewPanelScrollBoundariesRemainResponsive(t *testing.T) {
	m := panelModel(t)
	m.height = 12
	m.reviewPanel.message = strings.Repeat("History line\n", 40)
	update := func(message tea.Msg) {
		next, _ := m.Update(message)
		m = next.(Model)
	}
	key := func(value string) { update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(value)}) }
	key("G")
	_, visible := m.reviewViewport()
	bottom := len(m.reviewLines()) - visible
	if m.reviewPanel.offset != bottom {
		t.Fatalf("stored end offset = %d, want %d", m.reviewPanel.offset, bottom)
	}
	endView := m.View()
	key("k")
	if m.reviewPanel.offset != bottom-1 || m.View() == endView {
		t.Fatal("G then k did not scroll immediately")
	}
	key("G")
	key("j")
	key("k")
	if m.reviewPanel.offset != bottom-1 {
		t.Fatal("down at the bottom accumulated hidden overscroll")
	}
	key("G")
	update(tea.MouseMsg{Button: tea.MouseButtonWheelDown})
	update(tea.MouseMsg{Button: tea.MouseButtonWheelUp})
	if m.reviewPanel.offset != bottom-mouseStep {
		t.Fatal("wheel at the bottom accumulated hidden overscroll")
	}
	key("G")
	update(tea.WindowSizeMsg{Width: 100, Height: 100})
	if m.reviewPanel.offset != 0 {
		t.Fatal("resize retained an invisible offset")
	}
	update(tea.WindowSizeMsg{Width: 100, Height: 12})
	m.reviewPanel.message = ""
	m.reviewPanel.runs = []reviewmemory.Run{{Outcome: reviewmemory.Outcome{Message: strings.Repeat("History line\n", 40)}}}
	key("G")
	update(reviewHistoryMsg{url: m.reviewPanel.pr.URL})
	if m.reviewPanel.offset != 0 {
		t.Fatal("shorter history retained an invisible offset")
	}
}
