//go:build !windows

package cli

import (
	"errors"
	"os/exec"
	"syscall"
)

type ownedProcess struct{ cmd *exec.Cmd }

func startOwned(cmd *exec.Cmd) (*ownedProcess, error) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &ownedProcess{cmd: cmd}, nil
}
func (p *ownedProcess) interrupt(signal syscall.Signal) error {
	return syscall.Kill(-p.cmd.Process.Pid, signal)
}
func (p *ownedProcess) kill() error {
	err := syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}
func (p *ownedProcess) close() {}
