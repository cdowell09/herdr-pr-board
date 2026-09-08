package codexadapter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
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
	"golang.org/x/sys/windows"
)

func runCancellationAgent(dir string) error {
	if os.Getenv("CODEX_TEST_TOOL") == "1" {
		time.Sleep(30 * time.Second)
		return nil
	}
	claim, err := cli.InheritedFile("HERDR_REVIEW_CLAIM_FD")
	if err != nil || claim == nil {
		return fmt.Errorf("missing inherited claim: %v", err)
	}
	defer claim.Close()
	child := exec.Command(os.Args[0])
	child.Env = append(os.Environ(), "CODEX_TEST_TOOL=1")
	child.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP}
	if err := cli.PassFile(child, claim, "HERDR_REVIEW_CLAIM_FD"); err != nil {
		return err
	}
	if err := child.Start(); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "processes"), []byte(fmt.Sprintf("%d %d", os.Getpid(), child.Process.Pid)), 0600); err != nil {
		return err
	}
	return child.Wait()
}

func TestCodexWindowsCancellationStopsToolsBeforeClaimRelease(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"gh", "git", "codex"} {
		testutil.Executable(t, dir, name)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("CODEX_TEST_DIR", dir)
	t.Setenv("CODEX_TEST_CANCEL", "1")
	t.Setenv("GORACE", os.Getenv("GORACE")+" atexit_sleep_ms=0")
	in := reviewercontract.Input{Version: 1, Identity: reviewmemory.Identity{Repository: "owner/repo", Number: 42, HeadOID: strings.Repeat("a", 40), BaseRefName: "main"}, BaseOID: strings.Repeat("b", 40), ResultPath: filepath.Join(dir, "result.json")}
	metadata, _ := json.Marshal(map[string]any{"body": "Review captured change", "headRefOid": in.Identity.HeadOID, "baseRefOid": in.BaseOID, "baseRefName": "main"})
	if err := os.WriteFile(filepath.Join(dir, "pr.json"), metadata, 0600); err != nil {
		t.Fatal(err)
	}
	skill := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(skill, []byte("Review both axes"), 0600); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(dir, "claim.lock")
	claim, err := localstate.TryLock(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	defer claim.Close()
	t.Setenv("HERDR_REVIEW_CLAIM_FD", strconv.FormatUint(uint64(claim.Fd()), 10))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- Run(ctx, in, Options{Skill: skill}) }()
	var handles []windows.Handle
	deadline := time.Now().Add(5 * time.Second)
	for len(handles) == 0 {
		if data, err := os.ReadFile(filepath.Join(dir, "processes")); err == nil {
			for _, raw := range strings.Fields(string(data)) {
				pid, err := strconv.Atoi(raw)
				if err != nil {
					t.Fatal(err)
				}
				handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
				if err != nil {
					t.Fatal(err)
				}
				handles = append(handles, handle)
				t.Cleanup(func() { _ = windows.CloseHandle(handle) })
			}
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatalf("Codex tools did not start: %v", <-done)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(handles) != 2 {
		t.Fatalf("process identities=%v", handles)
	}
	acquired, err := localstate.TryLock(lockPath)
	if acquired != nil {
		acquired.Close()
	}
	if !errors.Is(err, localstate.ErrLocked) {
		t.Fatalf("claim not held: %v", err)
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation=%v", err)
	}
	for _, handle := range handles {
		state, err := windows.WaitForSingleObject(handle, 0)
		if err != nil || state != windows.WAIT_OBJECT_0 {
			t.Fatalf("Codex or tool survives return: %d %v", state, err)
		}
	}
	if err := claim.Close(); err != nil {
		t.Fatal(err)
	}
	acquired, err = localstate.TryLock(lockPath)
	if err != nil {
		t.Fatalf("claim survives process cleanup: %v", err)
	}
	acquired.Close()
}
