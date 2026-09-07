package board

import (
	"context"
	"time"

	"github.com/cdowell09/herdr-pr-board/internal/discovery"
	tea "github.com/charmbracelet/bubbletea"
)

// observationSource coordinates scheduled reads with the local monitor.
// Manual refreshes continue through discovery.Loader's fresh-scan methods.
type observationSource interface {
	Observe(context.Context) *discovery.Snapshot
}

func (m Model) observationCmd() tea.Cmd {
	source, ok := m.loader.(observationSource)
	if !ok {
		return m.refreshAllCmd()
	}
	epoch := m.epoch
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), discovery.RefreshAllTimeout)
		defer cancel()
		if snapshot := source.Observe(ctx); snapshot != nil {
			return snapshotMsg{Snapshot: *snapshot, epoch: epoch}
		}
		return observationUnchangedMsg{epoch: epoch}
	}
}

type observationUnchangedMsg struct{ epoch uint64 }

func (m Model) tickInterval() time.Duration {
	if _, ok := m.loader.(observationSource); ok {
		return time.Second
	}
	return m.refresh
}

func (m Model) refreshAllCmd() tea.Cmd {
	loader, epoch := m.loader, m.epoch
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), discovery.RefreshAllTimeout)
		defer cancel()
		snapshot := loader.RefreshAll(ctx)
		return snapshotMsg{Snapshot: snapshot, epoch: epoch}
	}
}

func (m Model) refreshOneCmd(index int) tea.Cmd {
	loader, epoch := m.loader, m.epoch
	view := m.views[index].View
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), discovery.RefreshOneTimeout)
		defer cancel()
		snapshot := loader.RefreshOne(ctx, view)
		return viewMsg{index: index, snapshot: snapshot, epoch: epoch}
	}
}

func (m Model) tickCmd() tea.Cmd {
	epoch := m.epoch
	return tea.Tick(m.tickInterval(), func(time.Time) tea.Msg { return tickMsg{epoch: epoch} })
}

// A fresh active-view scan can finish before the board reads an older monitor
// snapshot. Track attempts, including failures, so that snapshot cannot undo it.
func (m Model) observationCurrent(finished time.Time) bool {
	if finished.IsZero() {
		return true
	}
	for _, previous := range m.observations {
		if finished.Before(previous) {
			return false
		}
	}
	return true
}

func (m *Model) acceptObservation(id string, finished time.Time) bool {
	if finished.IsZero() {
		return true
	}
	if finished.Before(m.observations[id]) {
		return false
	}
	if m.observations == nil {
		m.observations = make(map[string]time.Time)
	}
	m.observations[id] = finished
	return true
}
