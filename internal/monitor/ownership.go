package monitor

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/cdowell09/herdr-pr-board/internal/localstate"
)

// Serialize probes and claims so a temporary probe cannot look like a monitor.
// This guard is separate from the startup lock held during the child handshake.
func hasOwner(ctx context.Context, dir string) (bool, error) {
	guard, err := localstate.Lock(ctx, filepath.Join(dir, "monitor-owner.lock"))
	if err != nil {
		return false, err
	}
	defer guard.Close()
	owner, err := localstate.TryLock(filepath.Join(dir, "monitor.lock"))
	if errors.Is(err, localstate.ErrLocked) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return false, owner.Close()
}

func claimOwner(ctx context.Context, dir string) (*os.File, error) {
	guard, err := localstate.Lock(ctx, filepath.Join(dir, "monitor-owner.lock"))
	if err != nil {
		return nil, err
	}
	defer guard.Close()
	return localstate.TryLock(filepath.Join(dir, "monitor.lock"))
}
