package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cdowell09/herdr-pr-board/internal/cli"
	"github.com/cdowell09/herdr-pr-board/internal/localstate"
)

func TestMonitorCommandProcess(t *testing.T) {
	if os.Getenv("PR_BOARD_MONITOR_TEST") != "1" {
		return
	}
	for i, arg := range os.Args {
		if arg == "--" {
			os.Exit(run(os.Args[i+1:], os.Stdout, os.Stderr))
		}
	}
	os.Exit(2)
}

func commandProcess(t *testing.T, args ...string) *exec.Cmd {
	t.Helper()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, append([]string{"-test.run=^TestMonitorCommandProcess$", "--"}, args...)...)
	cmd.Env = append(os.Environ(), "PR_BOARD_MONITOR_TEST=1")
	return cmd
}

func waitForSnapshot(t *testing.T, path string) []byte {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		data, err := localstate.ReadFile(path)
		if err == nil && json.Valid(data) {
			return data
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("monitor did not publish a complete snapshot")
	return nil
}

func TestMonitorCommandOwnershipCrashRecoveryAndFreshJSON(t *testing.T) {
	log := fakeSnapshotGH(t)
	state := t.TempDir()
	t.Setenv("HERDR_PLUGIN_STATE_DIR", state)
	configPath := writeConfig(t, strings.Replace(validConfigTOML, `"5m"`, `"0"`, 1))
	start := func() *exec.Cmd {
		cmd := commandProcess(t, "--monitor", "--config", configPath)
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if cmd.ProcessState == nil {
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
			}
		})
		return cmd
	}
	first := start()
	path := filepath.Join(state, "monitor-snapshot.json")
	before := waitForSnapshot(t, path)
	duplicate := commandProcess(t, "--monitor", "--config", configPath)
	output, err := duplicate.CombinedOutput()
	if err == nil || !bytes.Contains(output, []byte("already runs")) {
		t.Fatalf("duplicate: %v %s", err, output)
	}
	// An explicit command waits for the shared scan lock, then performs fresh requests.
	lock, err := localstate.TryLock(filepath.Join(state, "scan.lock"))
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	previousCalls, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	fresh := commandProcess(t, "--json", "--config", configPath)
	var stdout, stderr bytes.Buffer
	fresh.Stdout = &stdout
	fresh.Stderr = &stderr
	if err := fresh.Start(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	calls, _ := os.ReadFile(log)
	if !bytes.Equal(calls, previousCalls) {
		t.Fatal("fresh scan ignored shared lock")
	}
	lock.Close()
	if err := fresh.Wait(); err != nil {
		t.Fatalf("fresh scan: %v %s", err, &stderr)
	}
	if doc := decodeSnapshot(t, stdout.Bytes()); len(doc.Views) != 1 || !doc.Views[0].SearchSucceeded {
		t.Fatalf("fresh=%+v", doc)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("one-shot scan replaced monitor observation")
	}
	if err := first.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = first.Wait()
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	restarted := start()
	waitForSnapshot(t, path)
	if err := stopCommandProcess(restarted); err != nil {
		t.Fatalf("monitor stop: %v", err)
	}
	owner, err := localstate.TryLock(filepath.Join(state, "monitor.lock"))
	if err != nil {
		t.Fatalf("stopped monitor retained ownership: %v", err)
	}
	owner.Close()
}

func TestMonitorCommandUsage(t *testing.T) {
	for _, args := range [][]string{{"--monitor", "--json"}, {"--monitor", "--validate"}, {"--monitor", "--view", "all"}} {
		var out bytes.Buffer
		if code := run(args, &out, &out); code != 2 {
			t.Fatalf("%v: code %d", args, code)
		}
	}
	fakeSnapshotGH(t)
	t.Setenv("HERDR_PLUGIN_STATE_DIR", "")
	var out bytes.Buffer
	if code := run([]string{"--monitor", "--config", writeConfig(t, validConfigTOML)}, &out, &out); code != 1 || !strings.Contains(out.String(), "HERDR_PLUGIN_STATE_DIR") {
		t.Fatalf("code=%d output=%s", code, &out)
	}
}

func TestBackgroundReadinessPrecedesFirstGitHubScan(t *testing.T) {
	log := fakeSnapshotGH(t)
	state := t.TempDir()
	t.Setenv("HERDR_PLUGIN_STATE_DIR", state)
	path := writeConfig(t, validConfigTOML)
	scan, err := localstate.TryLock(filepath.Join(state, "scan.lock"))
	if err != nil {
		t.Fatal(err)
	}
	defer scan.Close()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	child := commandProcess(t, "--monitor", "--config", path)
	defer writer.Close()
	if err := cli.PassFile(child, writer, "HERDR_MONITOR_READY_FD"); err != nil {
		t.Fatal(err)
	}
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	writer.Close()
	t.Cleanup(func() { child.Process.Kill(); child.Wait() })
	if err := reader.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	var ack [1]byte
	if n, err := reader.Read(ack[:]); err != nil || n != 1 || ack[0] != 1 {
		t.Fatalf("readiness: %d %v %v", n, ack, err)
	}
	if calls, _ := os.ReadFile(log); len(calls) != 0 {
		t.Fatalf("GitHub request before scan release: %s", calls)
	}
	scan.Close()
	waitForSnapshot(t, filepath.Join(state, "monitor-snapshot.json"))
}

func TestReadOnlyCommandsNeverStartOptedInMonitor(t *testing.T) {
	fakeSnapshotGH(t)
	state := t.TempDir()
	t.Setenv("HERDR_PLUGIN_STATE_DIR", state)
	path := writeConfig(t, validConfigTOML+"\n[review]\nauto_views=['all']\n[[reviewers]]\nid='unused'\ncommand=['never-run']\n[[repositories]]\nname='acme/api'\nreviewer='unused'\nauto_launch=true\npublish_actions=['comment']\nauto_publish='comment'\n")
	for _, flag := range []string{"--validate", "--json", "--review-eligibility"} {
		var out, errout bytes.Buffer
		if code := run([]string{flag, "--config", path}, &out, &errout); code != 0 {
			t.Fatalf("%s code%d: %s", flag, code, &errout)
		}
	}
	for _, name := range []string{"monitor-start.lock", "monitor.log", "monitor-snapshot.json"} {
		if _, err := os.Stat(filepath.Join(state, name)); !os.IsNotExist(err) {
			t.Fatalf("read-only command started monitor: %s", name)
		}
	}
}
