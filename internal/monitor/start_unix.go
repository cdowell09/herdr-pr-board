//go:build !windows

package monitor

import (
	"os/exec"
	"syscall"
)

func detachMonitor(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} }
