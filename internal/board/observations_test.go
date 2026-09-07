package board

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cdowell09/herdr-pr-board/internal/discovery"
	gh "github.com/cdowell09/herdr-pr-board/internal/github"
	"github.com/cdowell09/herdr-pr-board/internal/sidebar"
)

type observedLoader struct {
	fakeLoader
	next *discovery.Snapshot
}

func (s *observedLoader) Observe(context.Context) *discovery.Snapshot {
	next := s.next
	s.next = nil
	return next
}

func TestMonitorObservationsPreserveSidebarAndStaleRows(t *testing.T) {
	cfg := testConfig()
	cfg.GitHub.RefreshInterval = "0"
	cfg.Sidebar.Enabled = boolPtr(true)
	runner := &sidebarFakeRunner{}
	reporter := sidebar.NewReporter(cfg.Sidebar, "workspace", "")
	reporter.Runner = runner.Run
	now := time.Now()
	snapshot := discovery.Snapshot{FinishedAt: now}
	for _, v := range cfg.Views {
		snapshot.Views = append(snapshot.Views, discovery.ViewData{View: v, UpdatedAt: now, ObservedAt: now, PRs: []gh.PullRequest{{URL: "https://github.com/a/b/pull/1"}}})
	}
	source := &observedLoader{next: &snapshot}
	m, err := NewModel(cfg, source, reporter)
	if err != nil {
		t.Fatal(err)
	}
	if m.tickInterval() != time.Second {
		t.Fatal("zero refresh disables monitor observation")
	}
	updated, report := m.Update(m.observationCmd()())
	m = updated.(Model)
	if report == nil {
		t.Fatal("full successful monitor scan did not report")
	}
	report()
	updated, report = m.Update(m.observationCmd()())
	m = updated.(Model)
	if report != nil || len(runner.calls) != 1 {
		t.Fatal("unchanged snapshot reported again")
	}
	failed := discovery.Snapshot{FinishedAt: now.Add(time.Second)}
	for _, v := range cfg.Views {
		failed.Views = append(failed.Views, discovery.ViewData{View: v, Err: errors.New("search failed")})
	}
	source.next = &failed
	updated, report = m.Update(m.observationCmd()())
	m = updated.(Model)
	if report != nil || !stale(m.views[0]) || !m.views[0].UpdatedAt.Equal(now) || len(m.views[0].PRs) != 1 {
		t.Fatalf("failure lost stale state or reported: %+v", m.views[0])
	}
}

func TestOlderMonitorSnapshotCannotUndoFreshActiveAttempt(t *testing.T) {
	for _, failure := range []error{nil, errors.New("fresh search failed")} {
		cfg := testConfig()
		m, err := NewModel(cfg, fakeLoader{}, nil)
		if err != nil {
			t.Fatal(err)
		}
		now := time.Now()
		old := discovery.Snapshot{FinishedAt: now, Rates: gh.RateLimits{Search: gh.RateResource{Remaining: 1}}}
		for _, v := range cfg.Views {
			old.Views = append(old.Views, discovery.ViewData{View: v, UpdatedAt: now, ObservedAt: now, PRs: []gh.PullRequest{{Title: "monitor"}}})
		}
		fresh := discovery.ViewSnapshot{Data: discovery.ViewData{View: cfg.Views[0], Err: failure, PRs: []gh.PullRequest{{Title: "fresh"}}}, FinishedAt: now.Add(time.Second), Rates: gh.RateLimits{Search: gh.RateResource{Remaining: 9}}}
		updated, _ := m.Update(viewMsg{index: 0, snapshot: fresh})
		m = updated.(Model)
		updated, report := m.Update(snapshotMsg{Snapshot: old})
		m = updated.(Model)
		if m.views[0].PRs[0].Title != "fresh" || m.views[0].Err != failure || m.views[1].PRs[0].Title != "monitor" || m.rates.Search.Remaining != 9 || report != nil {
			t.Fatalf("older observation undid fresh attempt: %+v", m.views)
		}
	}
}
