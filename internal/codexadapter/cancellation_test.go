//go:build !windows

package codexadapter

import (
	"bytes"
	"context"
	"encoding/json"
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

	"github.com/cdowell09/herdr-pr-board/internal/cli"
	"github.com/cdowell09/herdr-pr-board/internal/localstate"
	"github.com/cdowell09/herdr-pr-board/internal/reviewercontract"
	"github.com/cdowell09/herdr-pr-board/internal/reviewmemory"
	"github.com/cdowell09/herdr-pr-board/internal/testutil"
)

// The fake agent handles only SIGINT, like Codex 0.153.4, and owns a tool in a
// separate process group. Killing only the agent cannot clean up that tool.
func TestCodexInterruptCancellation(t *testing.T) {
	dir := os.Getenv("PR_BOARD_CODEX_CANCEL_DIR")
	switch os.Getenv("PR_BOARD_CODEX_CANCEL_ROLE") {
	case "agent":
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		defer stop()
		tool := exec.Command("sh", "-c", "trap '' INT TERM; sleep 30 & wait")
		tool.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		tool.ExtraFiles = []*os.File{os.NewFile(3, "inherited-claim")}
		if err := tool.Start(); err != nil {
			t.Fatal(err)
		}
		defer syscall.Kill(-tool.Process.Pid, syscall.SIGKILL)
		if err := os.WriteFile(filepath.Join(dir, "tool.pid"), []byte(strconv.Itoa(tool.Process.Pid)), 0600); err != nil {
			t.Fatal(err)
		}
		<-ctx.Done()
		_ = syscall.Kill(-tool.Process.Pid, syscall.SIGKILL)
		_ = tool.Wait()
		if err := os.WriteFile(filepath.Join(dir, "interrupted"), []byte("SIGINT"), 0600); err != nil {
			t.Fatal(err)
		}
		return
	case "adapter":
		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM)
		defer stop()
		err := Run(ctx, cancellationInput(dir), Options{Codex: filepath.Join(dir, "codex"), Skill: filepath.Join(dir, "SKILL.md")})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("adapter cancellation: %v", err)
		}
		return
	}

	dir = t.TempDir()
	in := cancellationInput(dir)
	metadata, _ := json.Marshal(map[string]any{"body": "Review the captured change", "headRefOid": in.Identity.HeadOID, "baseRefOid": in.BaseOID, "baseRefName": "main"})
	scripts := map[string]string{
		"gh": `#!/bin/sh
case "$1 $2" in
 'pr view') cat "$PR_BOARD_CODEX_CANCEL_DIR/pr.json";;
 'repo clone') mkdir -p "$4";;
esac
`,
		"git": `#!/bin/sh
case "$1" in
 rev-parse) printf '%s' aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa;;
 merge-base) printf '%s' bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb;;
esac
`,
		"codex": `#!/bin/sh
export PR_BOARD_CODEX_CANCEL_ROLE=agent
exec "$PR_BOARD_CODEX_CANCEL_BINARY" -test.run=^TestCodexInterruptCancellation$
`,
	}
	for name, script := range scripts {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "pr.json"), metadata, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("Review both axes."), 0600); err != nil {
		t.Fatal(err)
	}
	claimPath := filepath.Join(dir, "claim.lock")
	claim, err := localstate.TryLock(claimPath)
	if err != nil {
		t.Fatal(err)
	}
	defer claim.Close()
	cmd := exec.Command(os.Args[0], "-test.run=^TestCodexInterruptCancellation$")
	cmd.Env = append(os.Environ(), "PATH="+dir+":"+os.Getenv("PATH"), "PR_BOARD_CODEX_CANCEL_DIR="+dir, "PR_BOARD_CODEX_CANCEL_ROLE=adapter", "PR_BOARD_CODEX_CANCEL_BINARY="+os.Args[0], "HERDR_REVIEW_CLAIM_FD=3")
	cmd.ExtraFiles = []*os.File{claim}
	var diagnostics bytes.Buffer
	cmd.Stdout = &diagnostics
	cmd.Stderr = &diagnostics
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	var exitErr error
	go func() { exitErr = cli.RunProcess(ctx, cmd, 3*time.Second); close(done) }()
	defer func() { cancel(); <-done }()
	startup, cancelStartup := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancelStartup()
	pid, err := testutil.WaitForPID(startup, filepath.Join(dir, "tool.pid"), done)
	if err != nil {
		cancel()
		<-done
		t.Fatalf("fake Codex tool did not start: %v; adapter exit: %v; %s", err, exitErr, &diagnostics)
	}
	defer syscall.Kill(-pid, syscall.SIGKILL)
	acquired, err := localstate.TryLock(claimPath)
	if acquired != nil {
		acquired.Close()
	}
	if !errors.Is(err, localstate.ErrLocked) {
		t.Fatalf("claim released before cancellation: %v", err)
	}
	cancel()
	<-done
	if !errors.Is(exitErr, context.Canceled) {
		t.Fatalf("outer cancellation: %v, %s", exitErr, &diagnostics)
	}
	if data, err := os.ReadFile(filepath.Join(dir, "interrupted")); err != nil || string(data) != "SIGINT" {
		t.Fatalf("Codex missed graceful interrupt: %s, %v; %s", data, err, &diagnostics)
	}
	if err := syscall.Kill(pid, 0); err != syscall.ESRCH {
		t.Fatalf("detached tool %d survives cancellation: %v", pid, err)
	}
	if err := claim.Close(); err != nil {
		t.Fatal(err)
	}
	// Wait reaps the tool shell, but its killed child can close the inherited
	// claim later. Observe the lock itself rather than assume signal delivery.
	deadline := time.Now().Add(5 * time.Second)
	for {
		acquired, err = localstate.TryLock(claimPath)
		if err == nil {
			acquired.Close()
			break
		}
		if !errors.Is(err, localstate.ErrLocked) || !time.Now().Before(deadline) {
			t.Fatalf("claim remains locked after tool cleanup: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func cancellationInput(dir string) reviewercontract.Input {
	return reviewercontract.Input{Version: 1, Identity: reviewmemory.Identity{Repository: "owner/repo", Number: 42, HeadOID: strings.Repeat("a", 40), BaseRefName: "main"}, BaseOID: strings.Repeat("b", 40), ResultPath: filepath.Join(dir, "result.json")}
}
