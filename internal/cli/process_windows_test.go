package cli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/cdowell09/herdr-pr-board/internal/localstate"
	"golang.org/x/sys/windows"
)

func TestWindowsProcessHelper(t *testing.T) {
	role := os.Getenv("PR_BOARD_WINDOWS_ROLE")
	if role == "" {
		return
	}
	if role == "sleeper" {
		time.Sleep(time.Minute)
		return
	}
	child := exec.Command(os.Args[0], "-test.run=^TestWindowsProcessHelper$")
	child.Env = append(os.Environ(), "PR_BOARD_WINDOWS_ROLE=sleeper")
	child.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP}
	if role == "owner" {
		child.Env = append(os.Environ(), "PR_BOARD_WINDOWS_ROLE=tree")
		claim, err := InheritedFile("HERDR_REVIEW_CLAIM_FD")
		if err != nil {
			t.Fatal(err)
		}
		defer claim.Close()
		if err := PassFile(child, claim, "HERDR_REVIEW_CLAIM_FD"); err != nil {
			t.Fatal(err)
		}
		if err := RunProcess(context.Background(), child, time.Second); err != nil {
			t.Fatal(err)
		}
		return
	}
	if claim, err := InheritedFile("HERDR_REVIEW_CLAIM_FD"); err != nil {
		t.Fatal(err)
	} else if claim != nil {
		defer claim.Close()
		if err := PassFile(child, claim, "HERDR_REVIEW_CLAIM_FD"); err != nil {
			t.Fatal(err)
		}
	}
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer child.Process.Kill()
	data, _ := json.Marshal([]int{os.Getpid(), child.Process.Pid})
	if err := os.WriteFile(os.Getenv("PR_BOARD_WINDOWS_PIDS"), data, 0600); err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Minute)
}

func TestWindowsCancellationOwnsOnlySelectedTree(t *testing.T) {
	sibling := exec.Command(os.Args[0], "-test.run=^TestWindowsProcessHelper$")
	sibling.Env = append(os.Environ(), "PR_BOARD_WINDOWS_ROLE=sleeper")
	if err := sibling.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { sibling.Process.Kill(); sibling.Wait() }()
	path := filepath.Join(t.TempDir(), "process ids.json")
	cmd := exec.Command(os.Args[0], "-test.run=^TestWindowsProcessHelper$")
	cmd.Env = append(os.Environ(), "PR_BOARD_WINDOWS_ROLE=tree", "PR_BOARD_WINDOWS_PIDS="+path)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- RunProcess(ctx, cmd, 50*time.Millisecond) }()
	pids := windowsPIDs(t, path)
	identities := windowsIdentities(t, pids)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancel=%v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("tree cleanup did not complete")
	}
	for i, identity := range identities {
		state, waitErr := windows.WaitForSingleObject(identity.handle, 0)
		var exitCode uint32
		exitErr := windows.GetExitCodeProcess(identity.handle, &exitCode)
		if waitErr != nil || state != windows.WAIT_OBJECT_0 {
			t.Fatalf("selected process index=%d pid=%d remains: wait=%d error=%v exit=%d exit_error=%v", i, identity.pid, state, waitErr, exitCode, exitErr)
		}
	}
	if !windowsProcessAlive(sibling.Process.Pid) {
		t.Fatal("cancellation killed unrelated reviewer")
	}
}

func TestWindowsOwnerDeathCleansTreeAndRetainsClaim(t *testing.T) {
	dir := t.TempDir()
	claimPath, path := filepath.Join(dir, "claim.lock"), filepath.Join(dir, "pids.json")
	claim, err := localstate.TryLock(claimPath)
	if err != nil {
		t.Fatal(err)
	}
	defer claim.Close()
	owner := exec.Command(os.Args[0], "-test.run=^TestWindowsProcessHelper$")
	owner.Env = append(os.Environ(), "PR_BOARD_WINDOWS_ROLE=owner", "PR_BOARD_WINDOWS_PIDS="+path)
	if err := PassFile(owner, claim, "HERDR_REVIEW_CLAIM_FD"); err != nil {
		t.Fatal(err)
	}
	if err := owner.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { owner.Process.Kill(); owner.Wait() }()
	pids := windowsPIDs(t, path)
	identities := windowsIdentities(t, pids)
	claim.Close()
	if file, err := localstate.TryLock(claimPath); !errors.Is(err, localstate.ErrLocked) {
		if file != nil {
			file.Close()
		}
		t.Fatalf("live child lost claim: %v", err)
	}
	if err := owner.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = owner.Wait()
	deadline := time.Now().Add(10 * time.Second)
	for {
		file, err := localstate.TryLock(claimPath)
		alive := false
		for _, identity := range identities {
			state, _ := windows.WaitForSingleObject(identity.handle, 0)
			alive = alive || state != windows.WAIT_OBJECT_0
		}
		if err == nil {
			file.Close()
			// Job cleanup must kill all children even when only the reviewer inherits
			// the claim. Check process termination separately from lock release.
			if !alive {
				break
			}
		} else if !errors.Is(err, localstate.ErrLocked) {
			t.Fatal(err)
		}
		if !time.Now().Before(deadline) {
			t.Fatalf("owner death left process tree or claim: alive=%v lock=%v", alive, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func windowsPIDs(t *testing.T, path string) []int {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		var pids []int
		if err == nil && json.Unmarshal(data, &pids) == nil && len(pids) == 2 {
			return pids
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("native process tree did not start")
	return nil
}

func windowsProcessAlive(pid int) bool {
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(handle)
	result, err := windows.WaitForSingleObject(handle, 0)
	return err == nil && result == uint32(windows.WAIT_TIMEOUT)
}

type windowsIdentity struct {
	pid    int
	handle windows.Handle
}

func windowsIdentities(t *testing.T, pids []int) []windowsIdentity {
	t.Helper()
	var identities []windowsIdentity
	for _, pid := range pids {
		handle, err := windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
		if err != nil {
			t.Fatalf("open live process %d: %v", pid, err)
		}
		t.Cleanup(func() { windows.CloseHandle(handle) })
		identities = append(identities, windowsIdentity{pid, handle})
	}
	return identities
}
