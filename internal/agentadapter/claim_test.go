package agentadapter

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/cdowell09/herdr-pr-board/internal/localstate"
	"github.com/cdowell09/herdr-pr-board/internal/reviewercontract"
	"github.com/cdowell09/herdr-pr-board/internal/reviewmemory"
)

func TestAgentRetainsClaimAfterAdapterIsKilled(t *testing.T) {
	if dir := os.Getenv("PR_BOARD_CLAIM_HELPER"); dir != "" {
		in := reviewercontract.Input{Version: 1, Identity: reviewmemory.Identity{Repository: "owner/repo", Number: 42, HeadOID: strings.Repeat("a", 40), BaseRefName: "main"}, BaseOID: strings.Repeat("b", 40), ResultPath: filepath.Join(dir, "result.json")}
		if err := runAgent(context.Background(), in, Options{Name: "pi", Binary: filepath.Join(dir, "pi"), Command: func(binary, skill, work, checkout string) (*exec.Cmd, error) { return exec.Command(binary), nil }}, dir, dir, ""); err != nil {
			t.Fatal(err)
		}
		return
	}
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "claim.lock")
	claim, err := localstate.TryLock(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	defer claim.Close()
	script := `#!/bin/sh
printf '%s\n' "$$" > "$PR_BOARD_CLAIM_HELPER/pi.pid"
sleep 30 &
wait
`
	if err := os.WriteFile(filepath.Join(dir, "pi"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	adapter := exec.Command(os.Args[0], "-test.run=^TestAgentRetainsClaimAfterAdapterIsKilled$")
	adapter.Env = append(os.Environ(), "PR_BOARD_CLAIM_HELPER="+dir, "HERDR_REVIEW_CLAIM_FD=3")
	adapter.ExtraFiles = []*os.File{claim}
	if err := adapter.Start(); err != nil {
		t.Fatal(err)
	}
	defer adapter.Process.Kill()
	if err := claim.Close(); err != nil {
		t.Fatal(err)
	}
	var pid int
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		raw, err := os.ReadFile(filepath.Join(dir, "pi.pid"))
		if err == nil {
			pid, _ = strconv.Atoi(strings.TrimSpace(string(raw)))
			if pid > 0 {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if pid == 0 {
		t.Fatal("fake agent did not start")
	}
	defer syscall.Kill(-pid, syscall.SIGKILL)
	if err := adapter.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = adapter.Wait()
	acquired, err := localstate.TryLock(lockPath)
	if acquired != nil {
		acquired.Close()
	}
	if !errors.Is(err, localstate.ErrLocked) {
		t.Fatalf("claim released while orphan agent runs: %v", err)
	}
	if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		acquired, err = localstate.TryLock(lockPath)
		if err == nil {
			acquired.Close()
			return
		}
		if !errors.Is(err, localstate.ErrLocked) {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("claim remains locked after the agent exits")
}

func TestInheritedClaimRejectsInvalidDescriptorVariable(t *testing.T) {
	for _, value := range []string{"", "0", "-1", "4", "not-a-descriptor"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("HERDR_REVIEW_CLAIM_FD", value)
			if file, err := inheritedClaim(); err == nil {
				if file != nil {
					file.Close()
				}
				t.Fatal("accepted invalid claim descriptor")
			}
		})
	}
}
