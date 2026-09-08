//go:build !windows

package cli

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestRunProcessCancelsSubtree(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := RunProcess(ctx, exec.Command("sh", "-c", "trap '' TERM; sleep 30 & wait"), time.Second)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error=%v", err)
	}
	if time.Since(start) > 3*time.Second {
		t.Fatal("cancellation waited for child")
	}
}

func TestRunProcessNestedCancellation(t *testing.T) {
	if os.Getenv("PR_BOARD_PROCESS_HELPER") == "1" {
		ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM)
		defer cancel()
		_ = RunProcess(ctx, exec.Command("sh", "-c", `trap '' TERM; echo $$ > "$PR_BOARD_CHILD_PID"; sleep 30 & wait`), time.Second)
		return
	}
	pidPath := filepath.Join(t.TempDir(), "child.pid")
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	cmd := exec.Command(os.Args[0], "-test.run=^TestRunProcessNestedCancellation$")
	cmd.Env = append(os.Environ(), "PR_BOARD_PROCESS_HELPER=1", "PR_BOARD_CHILD_PID="+pidPath)
	err := RunProcess(ctx, cmd, 3*time.Second)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error=%v", err)
	}
	raw, err := os.ReadFile(pidPath)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Kill(pid, 0); err != syscall.ESRCH {
		t.Fatalf("nested child %d still exists: %v", pid, err)
	}
}
