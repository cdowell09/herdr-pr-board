// Package monitor coordinates local scans and stores headless observations.
package monitor

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"github.com/cdowell09/herdr-pr-board/internal/config"
	"github.com/cdowell09/herdr-pr-board/internal/discovery"
	"github.com/cdowell09/herdr-pr-board/internal/localstate"
)

type Source struct {
	dir         string
	cfg         config.Config
	loader      discovery.Loader
	mu          sync.Mutex
	next        time.Time
	seen        time.Time
	initialized bool
}

func New(dir string, cfg config.Config, loader discovery.Loader) *Source {
	return &Source{dir: dir, cfg: cfg, loader: loader}
}

func (s *Source) Reconfigured(cfg config.Config) discovery.Loader {
	return New(s.dir, cfg, s.loader.Reconfigured(cfg))
}

func (s *Source) RefreshAll(ctx context.Context) discovery.Snapshot {
	lock, err := localstate.Lock(ctx, filepath.Join(s.dir, "scan.lock"))
	if err != nil {
		return s.failure(err)
	}
	defer lock.Close()
	return s.loader.RefreshAll(ctx)
}

func (s *Source) RefreshOne(ctx context.Context, view config.View) discovery.ViewSnapshot {
	lock, err := localstate.Lock(ctx, filepath.Join(s.dir, "scan.lock"))
	if err != nil {
		failed := s.failure(err)
		return discovery.ViewSnapshot{Data: discovery.ViewData{View: view, Err: err}, StartedAt: failed.StartedAt, FinishedAt: failed.FinishedAt, Errors: failed.Errors}
	}
	defer lock.Close()
	return s.loader.RefreshOne(ctx, view)
}

// Observe serves the board's scheduled reads. Explicit refresh methods always scan.
// A nil result means that the board already has this observation.
func (s *Source) Observe(ctx context.Context) *discovery.Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	owner, err := localstate.TryLock(filepath.Join(s.dir, "monitor.lock"))
	if err != nil && !errors.Is(err, localstate.ErrLocked) {
		failed := s.failure(err)
		return &failed
	}
	if errors.Is(err, localstate.ErrLocked) {
		return s.observedSnapshot()
	}
	owner.Close()
	interval, _ := s.cfg.RefreshEvery()
	if s.initialized && (interval == 0 || time.Now().Before(s.next)) {
		return nil
	}
	lock, err := localstate.Lock(ctx, filepath.Join(s.dir, "scan.lock"))
	if err != nil {
		failed := s.failure(err)
		return &failed
	}
	defer lock.Close()
	// Recheck ownership after waiting for another scan.
	owner, err = localstate.TryLock(filepath.Join(s.dir, "monitor.lock"))
	if errors.Is(err, localstate.ErrLocked) {
		return s.observedSnapshot()
	}
	if err != nil {
		failed := s.failure(err)
		return &failed
	}
	owner.Close()
	snapshot := s.loader.RefreshAll(ctx)
	s.initialized = true
	s.next = time.Now().Add(interval)
	s.seen = snapshot.FinishedAt
	return &snapshot
}

func (s *Source) observedSnapshot() *discovery.Snapshot {
	// Allow an immediate fallback scan when the owner stops.
	s.initialized = false
	snapshot, err := s.read()
	if err != nil {
		// Replay the stored observation after a read failure clears.
		s.seen = time.Time{}
		failed := s.failure(err)
		return &failed
	}
	if snapshot.FinishedAt.Equal(s.seen) {
		return nil
	}
	s.seen = snapshot.FinishedAt
	return &snapshot
}

// Run holds ownership until cancellation. Kernel locks release after a crash.
func (s *Source) Run(ctx context.Context, report func(discovery.Snapshot), ready func() error) error {
	owner, err := localstate.TryLock(filepath.Join(s.dir, "monitor.lock"))
	if errors.Is(err, localstate.ErrLocked) {
		return errors.New("a monitor already runs in HERDR_PLUGIN_STATE_DIR")
	}
	if err != nil {
		return err
	}
	defer owner.Close()
	interval, err := s.cfg.RefreshEvery()
	if err != nil {
		return err
	}
	if ready != nil {
		if err := ready(); err != nil {
			return err
		}
	}
	for {
		scanCtx, cancel := context.WithTimeout(ctx, discovery.RefreshAllTimeout)
		snapshot := s.RefreshAll(scanCtx)
		cancel()
		if ctx.Err() != nil {
			return nil
		}
		if err := s.write(snapshot); err != nil {
			return fmt.Errorf("store monitor snapshot: %w", err)
		}
		if report != nil {
			report(snapshot)
		}
		if interval == 0 {
			<-ctx.Done()
			return nil
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
}

func (s *Source) failure(err error) discovery.Snapshot {
	now := time.Now()
	snapshot := discovery.Snapshot{StartedAt: now, FinishedAt: now, Errors: []discovery.RetrievalError{{Stage: "monitor", Err: err}}}
	for _, view := range s.cfg.Views {
		snapshot.Views = append(snapshot.Views, discovery.ViewData{View: view, Err: err})
	}
	return snapshot
}
