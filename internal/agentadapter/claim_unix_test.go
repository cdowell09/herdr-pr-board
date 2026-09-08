//go:build !windows

package agentadapter

import (
	"errors"
	"syscall"
	"testing"

	"github.com/cdowell09/herdr-pr-board/internal/localstate"
)

func checkClaimAgentAfterOwnerDeath(t *testing.T, pid int, lockPath string) func() {
	t.Helper()
	t.Cleanup(func() { _ = syscall.Kill(-pid, syscall.SIGKILL) })
	return func() {
		acquired, err := localstate.TryLock(lockPath)
		if acquired != nil {
			acquired.Close()
		}
		if !errors.Is(err, localstate.ErrLocked) {
			t.Fatalf("claim released while orphan agent runs: %v", err)
		}
		if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil {
			t.Fatal(err)
		}
	}
}
