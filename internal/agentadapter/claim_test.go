package agentadapter

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cdowell09/herdr-pr-board/internal/cli"
	"github.com/cdowell09/herdr-pr-board/internal/localstate"
	"github.com/cdowell09/herdr-pr-board/internal/reviewercontract"
	"github.com/cdowell09/herdr-pr-board/internal/reviewmemory"
	"github.com/cdowell09/herdr-pr-board/internal/testutil"
)

func TestAdapterDeathKeepsClaimUntilAgentExits(t *testing.T) {
	if dir := os.Getenv("PR_BOARD_CLAIM_HELPER"); dir != "" {
		in := reviewercontract.Input{Version: 1, Identity: reviewmemory.Identity{Repository: "owner/repo", Number: 42, HeadOID: strings.Repeat("a", 40), BaseRefName: "main"}, BaseOID: strings.Repeat("b", 40), ResultPath: filepath.Join(dir, "result.json")}
		if err := runAgent(context.Background(), in, Options{Name: "pi", Binary: os.Getenv("PR_BOARD_CLAIM_AGENT"), Command: func(binary, skill, work, checkout string) (*exec.Cmd, error) { return exec.Command(binary), nil }}, dir, dir, ""); err != nil {
			t.Fatal(err)
		}
		return
	}
	t.Setenv("GORACE", os.Getenv("GORACE")+" atexit_sleep_ms=0")
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "claim.lock")
	claim, err := localstate.TryLock(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	defer claim.Close()
	agent := testutil.Executable(t, dir, "claim-agent")
	adapter := exec.Command(os.Args[0], "-test.run=^TestAdapterDeathKeepsClaimUntilAgentExits$")
	adapter.Env = append(os.Environ(), "PR_BOARD_CLAIM_HELPER="+dir, "PR_BOARD_CLAIM_AGENT="+agent)
	if err := cli.PassFile(adapter, claim, "HERDR_REVIEW_CLAIM_FD"); err != nil {
		t.Fatal(err)
	}
	if err := adapter.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { adapter.Process.Kill(); adapter.Wait() }()
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
	afterDeath := checkClaimAgentAfterOwnerDeath(t, pid, lockPath)
	acquired, err := localstate.TryLock(lockPath)
	if acquired != nil {
		acquired.Close()
	}
	if !errors.Is(err, localstate.ErrLocked) {
		t.Fatalf("claim released while agent runs: %v", err)
	}
	if err := adapter.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = adapter.Wait()
	afterDeath()
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
	values := []string{"", "0", "-1", "not-a-descriptor", "999999999"}
	if runtime.GOOS != "windows" {
		values = append(values, "4")
	}
	for _, value := range values {
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
