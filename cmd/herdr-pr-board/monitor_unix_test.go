//go:build !windows

package main

import (
	"os/exec"
	"syscall"
)

func stopCommandProcess(cmd *exec.Cmd) error {
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		return err
	}
	return cmd.Wait()
}
