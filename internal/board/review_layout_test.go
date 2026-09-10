package board

import (
	"strings"
	"testing"
	"time"

	"github.com/cdowell09/herdr-pr-board/internal/config"
	"github.com/cdowell09/herdr-pr-board/internal/monitor"
	"github.com/cdowell09/herdr-pr-board/internal/publication"
	"github.com/cdowell09/herdr-pr-board/internal/reviewmemory"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func reviewLayoutFixture(t *testing.T) Model {
	m := panelModel(t)
	m.cfg.Repositories[0].AutoLaunch = true
	m.cfg.Repositories[0].AutoPublish = config.PublishComment
	m.cfg.Repositories[0].PublishActions = []config.PublicationAction{config.PublishComment}
	m.cfg.Review.AutoViews = []string{m.cfg.Views[0].ID}
	m.overview.monitor = monitor.Status{State: monitor.Running, ObservationOK: true}
	for _, id := range []string{strings.Repeat("1", 32), strings.Repeat("2", 32)} {
		m.setRegionData(func(data *regionData) {
			data.runs = append(data.runs, reviewmemory.Run{ID: id, Reviewer: "pi", StartedAt: time.Date(2026, 9, 8, 9, 6, 0, 0, time.UTC), Identity: reviewmemory.Identity{HeadOID: strings.Repeat("a", 40), BaseRefName: "main"}, Outcome: reviewmemory.Outcome{Status: reviewmemory.Completed, Message: "Standards and specification review complete. No issues found."}})
			data.publications = append(data.publications, publication.Attempt{RunID: id, Action: config.PublishComment, Status: publication.Published, URL: m.region.pr.URL + "#pullrequestreview-" + id})
		})
	}
	return m
}

func TestReviewLayoutGroupsResultsBeforeDiagnostics(t *testing.T) {
	m := reviewLayoutFixture(t)
	all := stripANSI(strings.Join(m.regionLines(true), "\n"))
	latest, previous, automation, details := strings.Index(all, "Latest review"), strings.Index(all, "Previous review"), strings.Index(all, "Automation"), strings.Index(all, "Details")
	if !(latest >= 0 && latest < previous && previous < automation && automation < details) {
		t.Fatalf("reading order:\n%s", all)
	}
	for _, block := range []string{all[latest:previous], all[previous:automation]} {
		if !strings.Contains(block, "Publication comment: published") {
			t.Fatalf("publication not attached to run: %s", block)
		}
		if strings.Contains(block, strings.Repeat("a", 40)) || strings.Contains(block, "pullrequestreview-") || strings.Contains(block, "/state/reviews/") {
			t.Fatalf("details overwhelm summary: %s", block)
		}
	}
	if strings.Contains(all[previous:automation], "Publication target") || !strings.Contains(all[latest:previous], "Publication target") {
		t.Fatal("publication target not tied to latest completed run")
	}
	for _, value := range []string{strings.Repeat("a", 40), m.regionState().runs[0].ID, m.regionState().publications[0].URL, "/state/reviews/"} {
		if !strings.Contains(all[details:], value) {
			t.Fatalf("details lost %q", value)
		}
	}
}

func TestReviewLayoutBoundsAndPinnedURL(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {30, 10}, {100, 35}, {12, 4}, {3, 2}} {
		m := reviewLayoutFixture(t)
		m.width, m.height = size[0], size[1]
		for _, key := range []string{"g", "G", "k"} {
			next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
			m = next.(Model)
			view := strings.Split(m.View(), "\n")
			if len(view) > m.height {
				t.Fatalf("%v height %d", size, len(view))
			}
			for _, line := range view {
				if lipgloss.Width(line) > m.width {
					t.Fatalf("%v overflow %q", size, line)
				}
			}
			if stripANSI(view[1]) != truncate(m.region.pr.URL, m.width) {
				t.Fatalf("%v URL moved", size)
			}
		}
	}
}

func TestReviewLayoutPreservesErrorsWithoutRuns(t *testing.T) {
	m := panelModel(t)
	m.setRegionData(func(*regionData) {})
	m.region.message = "Could not load local history"
	all := stripANSI(strings.Join(m.regionLines(true), "\n"))
	for _, value := range []string{"No local review runs", "Could not load local history"} {
		if !strings.Contains(all, value) {
			t.Fatalf("missing %q: %s", value, all)
		}
	}
	for _, status := range []reviewmemory.Status{reviewmemory.Running, reviewmemory.Failed} {
		m.setRegionRuns(reviewmemory.Run{ID: "run", Outcome: reviewmemory.Outcome{Status: status, Message: "Reviewer state details"}})
		all = stripANSI(strings.Join(m.regionLines(true), "\n"))
		if !strings.Contains(all, string(status)) || !strings.Contains(all, "Reviewer state details") {
			t.Fatalf("state lost: %s", all)
		}
	}
}

func TestReviewLayoutUsesSharedAutomationReadiness(t *testing.T) {
	m := panelModel(t)
	repo := m.cfg.Repositories[0]
	repo.AutoLaunch = true
	m.cfg.Repositories[0] = repo
	for _, state := range []monitor.Status{{State: monitor.Running}, {State: monitor.Stopped}, {State: monitor.Running, ObservationOK: true}} {
		m.overview.monitor = state
		for _, views := range [][]string{nil, {m.cfg.Views[0].ID}} {
			m.cfg.Review.AutoViews = views
			expected := automaticSetupWait(repo, views, state)
			all := stripANSI(strings.Join(m.regionLines(true), "\n"))
			if expected != "" && !strings.Contains(all, "Waiting: "+expected) {
				t.Fatalf("missing readiness %q: %s", expected, all)
			}
			if expected == "" && strings.Contains(all, "Waiting:") {
				t.Fatalf("ready setup shown waiting: %s", all)
			}
		}
	}
}
