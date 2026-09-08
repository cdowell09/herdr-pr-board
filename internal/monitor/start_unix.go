//go:build !windows

package monitor

import (
	"errors"
	"os/exec"
	"syscall"
)

func detachMonitor(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} }

func brokenReadyPipe(err error) bool { return errors.Is(err, syscall.EPIPE) }
