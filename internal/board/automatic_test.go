package board

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cdowell09/herdr-pr-board/internal/config"
	"github.com/cdowell09/herdr-pr-board/internal/discovery"
	"github.com/cdowell09/herdr-pr-board/internal/dispatch"
	gh "github.com/cdowell09/herdr-pr-board/internal/github"
	tea "github.com/charmbracelet/bubbletea"
)

func autoPanel(t *testing.T) Model {
	m := panelModel(t)
	m.cfg.Review.AutoViews = []string{m.cfg.Views[0].ID}
	m.cfg.Reviewers = []config.Reviewer{{ID: "agent", Command: []string{"unused"}}}
	m.cfg.Repositories[0].AutoLaunch = true
	pr := m.views[0].PRs[0]
	pr.BaseOID = strings.Repeat("b", 40)
	pr.State = gh.PROpen
	now := time.Now()
	pr.MetadataObservedAt = now
	views := append([]discovery.ViewData(nil), m.views...)
	for i := range views {
		views[i].UpdatedAt = now
		views[i].ObservedAt = now
	}
	views[0].PRs = []gh.PullRequest{pr}
	next, _ := m.Update(snapshotMsg{Snapshot: discovery.Snapshot{Views: views, StartedAt: now, FinishedAt: now}})
	return next.(Model)
}

func TestAutomaticPanelUsesCandidatesBeforeStaleRowRetention(t *testing.T) {
	m := autoPanel(t)
	url := m.reviewPanel.pr.URL
	if got := m.automaticDecision(url); !got.Eligible {
		t.Fatalf("decision=%+v", got)
	}
	now := time.Now()
	failed := discovery.Snapshot{StartedAt: now, FinishedAt: now, Errors: []discovery.RetrievalError{{Stage: "search", Err: errors.New("failed")}}, Views: []discovery.ViewData{{View: m.cfg.Views[0], Err: errors.New("failed")}}}
	next, _ := m.Update(snapshotMsg{Snapshot: failed})
	m = next.(Model)
	if len(m.views[0].PRs) == 0 {
		t.Fatal("test did not retain UI rows")
	}
	if len(m.autoCandidates) != 0 {
		t.Fatal("retained rows entered automatic candidates")
	}
	if got := m.automaticDecision(url); got.Eligible {
		t.Fatalf("retained row eligible: %+v", got)
	}
}

func TestActiveViewRevisionChangeInvalidatesAutomaticStatus(t *testing.T) {
	m := autoPanel(t)
	pr := m.views[0].PRs[0]
	pr.HeadOID = strings.Repeat("c", 40)
	pr.MetadataObservedAt = time.Now()
	now := time.Now()
	next, _ := m.Update(viewMsg{index: 0, snapshot: discovery.ViewSnapshot{Data: discovery.ViewData{View: m.cfg.Views[0], PRs: []gh.PullRequest{pr}, ObservedAt: now, UpdatedAt: now}, StartedAt: now, FinishedAt: now}})
	m = next.(Model)
	got := m.automaticDecision(pr.URL)
	if got.Eligible || got.Reason != dispatch.ObservationFailed {
		t.Fatalf("decision=%+v", got)
	}
	msg := m.reviewHistoryCmd(pr.URL)()
	next, _ = m.Update(msg)
	m = next.(Model)
	if !strings.Contains(stripANSI(m.View()), "Automatic (latest full observation): "+dispatch.ObservationFailed) {
		t.Fatal("panel omitted shared eligibility reason")
	}
}

func TestAutomaticPublicationSelectorUsesRenderedRow(t *testing.T) {
	m := panelModel(t)
	setup, err := newRepositorySetup(m.cfg, m.reviewPanel.pr.Repository)
	if err != nil {
		t.Fatal(err)
	}
	m.reviewPanel.setup = setup
	lines := strings.Split(stripANSI(m.View()), "\n")
	if !strings.Contains(lines[9], "local only · Automatic publication") {
		t.Fatalf("selector row=%q", lines[9])
	}
	next, _ := m.Update(tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, X: 1, Y: 9})
	m = next.(Model)
	if m.reviewPanel.setup.repo.AutoPublish != config.PublishComment || len(m.reviewPanel.setup.repo.PublishActions) != 1 {
		t.Fatalf("selection=%+v", m.reviewPanel.setup.repo)
	}
	next, _ = m.Update(tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, X: 1, Y: 6})
	m = next.(Model)
	if m.reviewPanel.setup.repo.AutoPublish != "" {
		t.Fatal("revocation retained automatic publication")
	}
}

func TestAutomaticHistoryCommandOwnsCandidateSnapshot(t *testing.T) {
	model := autoPanel(t)
	command := model.reviewHistoryCmd(model.reviewPanel.pr.URL)
	snapshot := discovery.ViewSnapshot{Data: discovery.ViewData{View: model.cfg.Views[0], Err: errors.New("failed")}}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 1000 {
			command()
		}
	}()
	for range 1000 {
		model.invalidateAutomatic(snapshot)
	}
	<-done
	result := command().(reviewHistoryMsg)
	if !result.automatic.Eligible {
		t.Fatalf("history command shared mutable candidates: %+v", result.automatic)
	}
}
