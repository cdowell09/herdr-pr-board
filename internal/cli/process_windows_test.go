package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
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
	if role == "churn" {
		var children []windowsChild
		for range 16 {
			child := exec.Command(os.Args[0], "-test.run=^TestWindowsProcessHelper$")
			child.Env = append(os.Environ(), "PR_BOARD_WINDOWS_ROLE=sleeper")
			child.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP}
			if err := child.Start(); err != nil {
				t.Fatal(err)
			}
			handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(child.Process.Pid))
			if err != nil {
				t.Fatal(err)
			}
			created, err := processCreated(handle)
			windows.CloseHandle(handle)
			if err != nil {
				t.Fatal(err)
			}
			children = append(children, windowsChild{child.Process.Pid, created})
			data, _ := json.Marshal(children)
			if err := os.WriteFile(os.Getenv("PR_BOARD_WINDOWS_PIDS"), data, 0600); err != nil {
				t.Fatal(err)
			}
		}
		time.Sleep(time.Minute)
		return
	}
	if role == "echo" {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			t.Fatal(err)
		}
		cwd, _ := os.Getwd()
		_ = json.NewEncoder(os.Stdout).Encode(struct {
			Args            []string
			Input, Env, CWD string
		}{os.Args[3:], string(data), os.Getenv("PR_BOARD_ARG_VALUE"), cwd})
		_, _ = os.Stderr.WriteString("diagnostic")
		os.Exit(0)
	}
	if role == "sleeper" {
		if marker := os.Getenv("PR_BOARD_WINDOWS_MARKER"); marker != "" {
			_ = os.WriteFile(marker, []byte("ran"), 0600)
		}
		time.Sleep(time.Minute)
		return
	}
	child := exec.Command(os.Args[0], "-test.run=^TestWindowsProcessHelper$")
	child.Env = append(os.Environ(), "PR_BOARD_WINDOWS_ROLE=sleeper")
	child.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP}
	if role == "owner" || role == "owner-suspended" {
		child.Env = append(os.Environ(), "PR_BOARD_WINDOWS_ROLE=tree")
		claim, err := InheritedFile("HERDR_REVIEW_CLAIM_FD")
		if err != nil {
			t.Fatal(err)
		}
		defer claim.Close()
		if err := PassFile(child, claim, "HERDR_REVIEW_CLAIM_FD"); err != nil {
			t.Fatal(err)
		}
		if role == "owner-suspended" {
			child.Env = append(child.Env, "PR_BOARD_WINDOWS_ROLE=sleeper")
			created, err := createOwned(child)
			if err != nil {
				t.Fatal(err)
			}
			defer created.close()
			data, _ := json.Marshal([]int{os.Getpid(), int(created.pid)})
			if err := os.WriteFile(os.Getenv("PR_BOARD_WINDOWS_PIDS"), data, 0600); err != nil {
				t.Fatal(err)
			}
			time.Sleep(time.Minute)
			return
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
	for _, role := range []string{"owner", "owner-suspended"} {
		t.Run(role, func(t *testing.T) { windowsOwnerDeath(t, role) })
	}
}

func windowsOwnerDeath(t *testing.T, role string) {
	dir := t.TempDir()
	claimPath, path := filepath.Join(dir, "claim.lock"), filepath.Join(dir, "pids.json")
	claim, err := localstate.TryLock(claimPath)
	if err != nil {
		t.Fatal(err)
	}
	defer claim.Close()
	owner := exec.Command(os.Args[0], "-test.run=^TestWindowsProcessHelper$")
	owner.Env = append(os.Environ(), "PR_BOARD_WINDOWS_ROLE="+role, "PR_BOARD_WINDOWS_PIDS="+path, "PR_BOARD_WINDOWS_MARKER="+filepath.Join(dir, "ran"))
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
	if role == "owner-suspended" {
		if _, err := os.Stat(filepath.Join(dir, "ran")); !os.IsNotExist(err) {
			t.Fatal("suspended child executed before ownership cleanup")
		}
	}

}

func TestWindowsNativeStreamsPreserveArguments(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "working & 日本語 directory")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	args := []string{`{"schema":"quoted & literal"}`, `C:\space path\trailing\`, "%PATH% ! ^ & | $(literal)", "日本語"}
	cmd := exec.Command(os.Args[0], append([]string{"-test.run=^TestWindowsProcessHelper$", "--"}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "PR_BOARD_WINDOWS_ROLE=echo", "PR_BOARD_ARG_VALUE=value & %PATH%")
	cmd.Stdin = strings.NewReader("prompt with quotes and 日本語")
	var output, diagnostics bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &diagnostics
	if err := RunProcess(context.Background(), cmd, time.Second); err != nil {
		t.Fatalf("native command: %v: %s", err, &diagnostics)
	}
	var got struct {
		Args            []string
		Input, Env, CWD string
	}
	if err := json.Unmarshal(output.Bytes(), &got); err != nil {
		t.Fatalf("decode %q: %v", &output, err)
	}
	if !reflect.DeepEqual(got.Args, args) || got.Input != "prompt with quotes and 日本語" || got.Env != "value & %PATH%" || got.CWD != dir || diagnostics.String() != "diagnostic" {
		t.Fatalf("native stream/argument drift: %+v diagnostics=%q", got, &diagnostics)
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

type windowsChild struct {
	PID     int
	Created windows.Filetime
}

func processCreated(handle windows.Handle) (windows.Filetime, error) {
	var created, exited, kernel, user windows.Filetime
	err := windows.GetProcessTimes(handle, &created, &exited, &kernel, &user)
	return created, err
}

func TestWindowsCleanupDuringChildCreation(t *testing.T) {
	for range 8 {
		path := filepath.Join(t.TempDir(), "churn.json")
		command := exec.Command(os.Args[0], "-test.run=^TestWindowsProcessHelper$")
		command.Env = append(os.Environ(), "PR_BOARD_WINDOWS_ROLE=churn", "PR_BOARD_WINDOWS_PIDS="+path)
		owner, err := startOwned(command)
		if err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { done <- owner.wait() }()
		deadline := time.Now().Add(10 * time.Second)
		var children []windowsChild
		for len(children) < 3 && time.Now().Before(deadline) {
			data, _ := os.ReadFile(path)
			_ = json.Unmarshal(data, &children)
			if len(children) < 3 {
				time.Sleep(time.Millisecond)
			}
		}
		if len(children) < 3 {
			_ = owner.kill()
			owner.close()
			t.Fatal("churning children did not start")
		}
		if err := owner.kill(); err != nil {
			owner.close()
			t.Fatal(err)
		}
		<-done
		data, _ := os.ReadFile(path)
		var latest []windowsChild
		if json.Unmarshal(data, &latest) == nil {
			children = latest
		}
		for _, child := range children {
			handle, err := windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(child.PID))
			if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
				continue
			}
			if err != nil {
				owner.close()
				t.Fatal(err)
			}
			created, err := processCreated(handle)
			state, waitErr := windows.WaitForSingleObject(handle, 0)
			windows.CloseHandle(handle)
			if err != nil {
				owner.close()
				t.Fatal(err)
			}
			if created == child.Created && (waitErr != nil || state != windows.WAIT_OBJECT_0) {
				owner.close()
				t.Fatalf("child born during cleanup remains: pid=%d state=%d error=%v", child.PID, state, waitErr)
			}
		}
		owner.close()
	}
}
