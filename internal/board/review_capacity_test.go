package board

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cdowell09/herdr-pr-board/internal/dispatch"
	"github.com/cdowell09/herdr-pr-board/internal/monitor"
	"github.com/cdowell09/herdr-pr-board/internal/review"
	"github.com/cdowell09/herdr-pr-board/internal/reviewmemory"
	"github.com/pelletier/go-toml/v2"
)

func TestReviewPanelWaitsForSharedSlotAndReloadsLimit(t *testing.T) {
	m := autoPanel(t)
	m.width, m.height = 80, 24
	m.reviewPanel.monitor = monitor.Status{State: monitor.Running, ObservationOK: true}
	m.cfg.Review.MaxConcurrency = 1
	m.cfg.GitHub.Scopes = []string{"user:@me"}
	m.configPath = filepath.Join(t.TempDir(), "config.toml")
	save := func(limit int) {
		t.Helper()
		cfg := m.cfg
		cfg.Review.MaxConcurrency = limit
		data, err := toml.Marshal(cfg)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(m.configPath, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	save(1)
	dir := t.TempDir()
	backend, err := review.New(dir, m.configPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	m = m.WithReviews(context.Background(), backend)
	store, err := reviewmemory.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	id := dispatch.Identity(m.autoCandidates[0].PR)
	id.Number++ // Another PR owns the installation's only review slot.
	claim, err := store.Claim(reviewmemory.Request{Identity: id, BaseOID: strings.Repeat("b", 40), Reviewer: "agent"})
	if err != nil {
		t.Fatal(err)
	}
	defer claim.Close()
	check := func(waiting bool) {
		t.Helper()
		next, _ := m.Update(m.reviewHistoryCmd(m.reviewPanel.pr.URL)())
		m = next.(Model)
		view := stripANSI(m.View())
		if strings.Contains(view, "Waiting for review slot") != waiting {
			t.Fatalf("waiting=%v:\n%s", waiting, view)
		}
		if !m.reviewPanel.automatic.Eligible {
			t.Fatalf("capacity changed eligibility: %+v", m.reviewPanel.automatic)
		}
	}
	check(true)
	m.reviewPanel.monitor.State = monitor.Stopped
	check(false)
	m.reviewPanel.monitor.State = monitor.Running
	m.reviewPanel.monitor.ObservationOK = false
	check(false)
	m.reviewPanel.monitor.ObservationOK = true
	check(true)
	save(2)
	check(false)
	save(1)
	check(true)
	if err := claim.Close(); err != nil {
		t.Fatal(err)
	}
	check(false)
	claim, err = store.Claim(reviewmemory.Request{Identity: dispatch.Identity(m.autoCandidates[0].PR), BaseOID: strings.Repeat("b", 40), Reviewer: "agent"})
	if err != nil {
		t.Fatal(err)
	}
	defer claim.Close()
	next, _ := m.Update(m.reviewHistoryCmd(m.reviewPanel.pr.URL)())
	m = next.(Model)
	if m.reviewPanel.automatic.Reason != reviewmemory.ErrActive.Error() || strings.Contains(stripANSI(m.View()), "Waiting for review slot") {
		t.Fatalf("active review mislabeled as waiting: %s", stripANSI(m.View()))
	}
}

type capacityErrorBackend struct {
	reviewFake
	err error
}

func (b *capacityErrorBackend) ReviewCapacity() error { return b.err }

func TestReviewPanelReportsCapacityReadFailureAndRecovery(t *testing.T) {
	m := autoPanel(t)
	m.reviewPanel.monitor = monitor.Status{State: monitor.Running, ObservationOK: true}
	backend := &capacityErrorBackend{err: errors.New("cannot read claims")}
	m = m.WithReviews(context.Background(), backend)
	next, _ := m.Update(m.reviewHistoryCmd(m.reviewPanel.pr.URL)())
	m = next.(Model)
	view := stripANSI(m.View())
	if !strings.Contains(view, "Review capacity unavailable: cannot read claims") || strings.Contains(view, "Waiting for review slot") {
		t.Fatalf("capacity error hidden or presented as full: %s", view)
	}
	backend.err = nil
	next, _ = m.Update(m.reviewHistoryCmd(m.reviewPanel.pr.URL)())
	m = next.(Model)
	if strings.Contains(stripANSI(m.View()), "Review capacity unavailable") {
		t.Fatal("capacity error persisted after recovery")
	}
}
