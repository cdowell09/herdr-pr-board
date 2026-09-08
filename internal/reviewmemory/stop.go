package reviewmemory

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/cdowell09/herdr-pr-board/internal/localstate"
)

var (
	ErrStopped         = errors.New("review stopped by user; explicit retry required")
	ErrNotRunning      = errors.New("review is no longer running")
	ErrStopUnavailable = errors.New("review owner cannot accept stop requests")
)

func (s *Store) stopOwnerPath(id string) string { return filepath.Join(s.dir, id+".control.lock") }
func (s *Store) stopPath(id string) string      { return filepath.Join(s.dir, id+".stop") }

// EnableStop advertises a live controller. Unlike the claim, this descriptor is
// never inherited by the reviewer, so an orphan cannot accept a stop request.
func (c *Claim) EnableStop() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.file == nil {
		return ErrOwnership
	}
	if c.stopOwner != nil {
		return nil
	}
	var err error
	c.stopOwner, err = localstate.TryLock(c.store.stopOwnerPath(c.id))
	return err
}

// RequestStop targets an exact live attempt, never a process ID. The history
// transaction orders it against Finish, which must honor an accepted request.
func (s *Store) RequestStop(id string) error {
	return s.transaction(func(h *history) error {
		for _, run := range h.Runs {
			if run.ID != id {
				continue
			}
			if run.Status != Running {
				return ErrNotRunning
			}
			owner, err := localstate.TryLock(s.stopOwnerPath(id))
			if err == nil {
				owner.Close()
				return ErrStopUnavailable
			}
			if !errors.Is(err, localstate.ErrLocked) {
				return err
			}
			return localstate.AtomicWrite(s.stopPath(id), []byte(ErrStopped.Error()+"\n"))
		}
		return ErrNotRunning
	})
}

// StopRequested reads only this attempt's marker, without scanning history.
func (c *Claim) StopRequested() (bool, error) {
	_, err := os.Stat(c.store.stopPath(c.id))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}
