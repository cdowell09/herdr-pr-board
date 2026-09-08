//go:build !windows

package review

import (
	"golang.org/x/sys/unix"
	"syscall"
	"testing"
)

func makeNonregularResult(path string) error { return unix.Mkfifo(path, 0600) }
func reviewChildAlive(t *testing.T, pid int) func() bool {
	t.Helper()
	return func() bool { return syscall.Kill(pid, 0) != syscall.ESRCH }
}
