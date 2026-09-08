package cli

import (
	"context"
	"errors"
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
	return RunProcessWithSignal(ctx, cmd, grace, 0)
}

// RunProcessWithSignal selects the graceful cancellation signal for programs
// with a different shutdown contract. Zero preserves the default SIGTERM.
func RunProcessWithSignal(ctx context.Context, cmd *exec.Cmd, grace time.Duration, cancelSignal syscall.Signal) error {
	if cancelSignal == 0 {
		cancelSignal = syscall.SIGTERM
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	cmd.WaitDelay = time.Second
	owner, err := startOwned(cmd)
	if err != nil {
		return err
	}
	defer owner.close()
	done := make(chan error, 1)
	go func() { done <- owner.wait() }()
	select {
	case err := <-done:
		return errors.Join(err, owner.kill())
	case <-ctx.Done():
		_ = owner.interrupt(cancelSignal)
		timer := time.NewTimer(grace)
		defer timer.Stop()
		select {
		case <-done:
		case <-timer.C:
			cleanupErr := owner.kill()
			<-done
			return errors.Join(fmt.Errorf("process canceled: %w", ctx.Err()), cleanupErr)
		}
		return errors.Join(fmt.Errorf("process canceled: %w", ctx.Err()), owner.kill())
	}
}
