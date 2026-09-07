package cli

import (
	"context"
	"fmt"
	"os/exec"
	"syscall"
	"time"
)

// RunProcess runs an owned process group. Cancellation gives the program time
// to stop detached children before killing the remaining process group.
// Give an outer wrapper a longer grace period than its child processes.
// Callers supply standard streams and must use exec.Command, not CommandContext.
func RunProcess(ctx context.Context, cmd *exec.Cmd, grace time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = time.Second
	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		return err
	case <-ctx.Done():
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
		timer := time.NewTimer(grace)
		defer timer.Stop()
		select {
		case <-done:
		case <-timer.C:
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			<-done
		}
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		return fmt.Errorf("process canceled: %w", ctx.Err())
	}
}
