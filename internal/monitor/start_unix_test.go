//go:build !windows

package monitor

import (
	"golang.org/x/sys/unix"
	"os"
	"syscall"
)

func monitorDetached() bool                { sid, err := unix.Getsid(0); return err == nil && sid == os.Getpid() }
func monitorProcessAlive(pid int) bool     { return syscall.Kill(pid, 0) == nil }
func privateLogMode(mode os.FileMode) bool { return mode.Perm() == 0600 }
