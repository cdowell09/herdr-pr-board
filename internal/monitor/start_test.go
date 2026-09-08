package monitor

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/cdowell09/herdr-pr-board/internal/cli"
	"github.com/cdowell09/herdr-pr-board/internal/config"
	"github.com/cdowell09/herdr-pr-board/internal/discovery"
	"github.com/cdowell09/herdr-pr-board/internal/localstate"
	"github.com/cdowell09/herdr-pr-board/internal/testutil"
)

// The helper uses real process/session/pipe boundaries and no GitHub transport.
func TestMain(m *testing.M) {
	if os.Getenv("PR_BOARD_START_ROLE") != "" {
		monitorStartupProcess()
	}
	os.Exit(m.Run())
}

func monitorStartupProcess() {
	role := os.Getenv("PR_BOARD_START_ROLE")
	if role == "" {
		return
	}
	path, dir := os.Getenv("PR_BOARD_START_CONFIG"), os.Getenv("HERDR_PLUGIN_STATE_DIR")
	if role == "parent" {
		os.Setenv("PR_BOARD_START_ROLE", "child")
		if err := EnsureRunning(context.Background(), os.Getenv("PR_BOARD_START_BINARY"), path, dir); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	mode := os.Getenv("PR_BOARD_START_MODE")
	record, _ := os.OpenFile(filepath.Join(dir, "starts"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	fmt.Fprintln(record, os.Getpid())
	record.Close()
	if mode == "exit" {
		fmt.Fprintln(os.Stderr, "fixture startup failed")
		os.Exit(7)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM)
	defer stop()
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(10 * time.Millisecond):
				if _, err := os.Stat(filepath.Join(dir, "stop")); err == nil {
					stop()
					return
				}
			}
		}
	}()
	if mode == "silent" {
		<-ctx.Done()
		os.Exit(0)
	}
	if mode == "hung" {
		signal.Ignore(syscall.SIGTERM)
		for {
			select {
			case <-ctx.Done():
				os.Exit(0)
			case <-time.After(time.Second):
			}
		}
	}
	pipe, err := ReadyPipe()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	cfg, err := config.LoadExisting(path)
	if err != nil {
		os.Exit(3)
	}
	if mode == "compete" {
		for {
			if _, err := os.Stat(filepath.Join(dir, "compete-go")); err == nil {
				break
			}
			select {
			case <-ctx.Done():
				os.Exit(5)
			case <-time.After(10 * time.Millisecond):
			}
		}
	}
	s := New(dir, cfg, startupLoader{dir: dir})
	err = s.Run(ctx, nil, func() error {
		if mode == "delayed" {
			for {
				if _, err := os.Stat(filepath.Join(dir, "ack-allowed")); err == nil {
					break
				}
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(10 * time.Millisecond):
				}
			}
		}
		if mode == "badack" {
			_, err := pipe.Write([]byte{2})
			pipe.Close()
			return err
		}
		return AcknowledgeReady(pipe)
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(4)
	}
	os.Exit(0)
}

type startupLoader struct{ dir string }

func (l startupLoader) RefreshAll(ctx context.Context) discovery.Snapshot {
	detached := monitorDetached()
	data, _ := json.Marshal(struct {
		Args     []string
		Ready    string
		Detached bool
		PID      int
	}{os.Args, os.Getenv(readyEnvironment), detached, os.Getpid()})
	os.WriteFile(filepath.Join(l.dir, "scan-started"), data, 0600)
	<-ctx.Done()
	return discovery.Snapshot{}
}
func (l startupLoader) RefreshOne(context.Context, config.View) discovery.ViewSnapshot {
	return discovery.ViewSnapshot{}
}
func (l startupLoader) Reconfigured(config.Config) discovery.Loader { return l }

func startupFixture(t *testing.T) (binary, path, dir string) {
	t.Helper()
	dir = t.TempDir()
	path = filepath.Join(t.TempDir(), "saved 'configuration' $(literal).toml")
	text := strings.Replace(config.DefaultFile, "auto_views = []", `auto_views = ["review"]`, 1) + "\n[[reviewers]]\nid='fixture'\ncommand=['unused-reviewer']\n[[repositories]]\nname='acme/repo'\nreviewer='fixture'\nauto_launch=true\n"
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	binary = testutil.Executable(t, t.TempDir(), "fake monitor 'executable'")
	t.Setenv("PR_BOARD_START_ROLE", "child")
	t.Setenv("PR_BOARD_START_CONFIG", path)
	t.Setenv("PR_BOARD_START_BINARY", binary)
	t.Setenv("HERDR_PLUGIN_STATE_DIR", dir)
	t.Cleanup(func() {
		if err := os.WriteFile(filepath.Join(dir, "stop"), nil, 0600); err != nil {
			t.Error(err)
		}
		waitStartup(t, func() bool { running, _ := hasOwner(dir); return !running })
	})
	return
}

func waitStartup(t *testing.T, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if ready() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("startup fixture did not settle")
}

func TestConcurrentStartupReusesOneOwnerBeforeSlowScan(t *testing.T) {
	binary, path, dir := startupFixture(t)
	if err := os.WriteFile(filepath.Join(dir, "monitor.log"), []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	failures := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); failures <- EnsureRunning(context.Background(), binary, path, dir) }()
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	starts, _ := os.ReadFile(filepath.Join(dir, "starts"))
	if len(strings.Fields(string(starts))) != 1 {
		t.Fatalf("duplicate processes: %s", starts)
	}
	waitStartup(t, func() bool { _, err := os.Stat(filepath.Join(dir, "scan-started")); return err == nil })
	var record struct {
		Args     []string
		Ready    string
		Detached bool
		PID      int
	}
	data, _ := os.ReadFile(filepath.Join(dir, "scan-started"))
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatal(err)
	}
	if !record.Detached {
		t.Fatalf("monitor did not detach session: %+v", record)
	}
	if record.Ready != "" || strings.Join(record.Args[len(record.Args)-3:], "\x00") != strings.Join([]string{"--monitor", "--config", path}, "\x00") {
		t.Fatalf("argument/env drift: %+v", record)
	}
	info, err := os.Stat(filepath.Join(dir, "monitor.log"))
	if err != nil || !privateLogMode(info.Mode()) {
		t.Fatalf("private log: %v %v", info, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "monitor-snapshot.json")); !os.IsNotExist(err) {
		t.Fatal("test scan should remain blocked")
	}
}

func TestStartupOptInsAndExistingOwnerDoNotSpawn(t *testing.T) {
	binary, path, dir := startupFixture(t)
	owner, err := localstate.TryLock(filepath.Join(dir, "monitor.lock"))
	if err != nil {
		t.Fatal(err)
	}
	if err := EnsureRunning(context.Background(), "/missing", path, dir); err != nil {
		t.Fatal(err)
	}
	owner.Close()
	text, _ := os.ReadFile(path)
	for _, disabled := range []string{strings.Replace(string(text), "auto_launch=true", "auto_launch=false", 1), strings.Replace(string(text), `auto_views = ["review"]`, `auto_views = []`, 1)} {
		if err := os.WriteFile(path, []byte(disabled), 0600); err != nil {
			t.Fatal(err)
		}
		if err := EnsureRunning(context.Background(), binary, path, dir); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "starts")); !os.IsNotExist(err) {
		t.Fatal("spawned without saved permission")
	}
}

func TestStartupFailureAndTimeoutCleanOwnedProcess(t *testing.T) {
	for _, mode := range []string{"exit", "silent", "hung", "badack"} {
		t.Run(mode, func(t *testing.T) {
			binary, path, dir := startupFixture(t)
			t.Setenv("PR_BOARD_START_MODE", mode)
			ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
			defer cancel()
			if err := EnsureRunning(ctx, binary, path, dir); err == nil || !strings.Contains(err.Error(), "monitor.log") {
				t.Fatalf("missing actionable error: %v", err)
			}
			starts, _ := os.ReadFile(filepath.Join(dir, "starts"))
			for _, value := range strings.Fields(string(starts)) {
				pid, _ := strconv.Atoi(value)
				if monitorProcessAlive(pid) {
					t.Fatalf("startup process %d remains", pid)
				}
			}
		})
	}
}

func TestMonitorSurvivesLauncherExit(t *testing.T) {
	binary, _, dir := startupFixture(t)
	parent := exec.Command(binary)
	parent.Env = append(os.Environ(), "PR_BOARD_START_ROLE=parent")
	if output, err := parent.CombinedOutput(); err != nil {
		t.Fatalf("parent: %v %s", err, output)
	}
	if running, err := hasOwner(dir); err != nil || !running {
		t.Fatalf("monitor died with launcher: %v", err)
	}
}

func TestClosedLauncherPipeDoesNotStopMonitor(t *testing.T) {
	binary, path, dir := startupFixture(t)
	t.Setenv("PR_BOARD_START_MODE", "delayed")
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	child := exec.Command(binary, "--monitor", "--config", path)
	child.Env = monitorEnvironment(dir)
	if err := cli.PassFile(child, writer, readyEnvironment); err != nil {
		t.Fatal(err)
	}
	child.Stdout, child.Stderr = os.Stdout, os.Stderr
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	writer.Close()
	done := make(chan error, 1)
	go func() { done <- child.Wait() }()
	t.Cleanup(func() {
		os.WriteFile(filepath.Join(dir, "stop"), nil, 0600)
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			child.Process.Kill()
			<-done
			t.Error("helper did not stop")
		}
	})
	waitStartup(t, func() bool { running, _ := hasOwner(dir); return running })
	reader.Close() // The launching board exits before the ownership acknowledgement.
	if err := os.WriteFile(filepath.Join(dir, "ack-allowed"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	waitStartup(t, func() bool { _, err := os.Stat(filepath.Join(dir, "scan-started")); return err == nil })
	if running, _ := hasOwner(dir); !running {
		t.Fatal("vanished launcher stopped the monitor")
	}
}

func TestSavedPermissionRevokedWhileWaitingForStartupLock(t *testing.T) {
	binary, path, dir := startupFixture(t)
	lock, err := localstate.TryLock(filepath.Join(dir, "monitor-start.lock"))
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- EnsureRunning(context.Background(), binary, path, dir) }()
	text, _ := os.ReadFile(path)
	if err := localstate.AtomicWrite(path, []byte(strings.Replace(string(text), "auto_launch=true", "auto_launch=false", 1))); err != nil {
		t.Fatal(err)
	}
	lock.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "starts")); !os.IsNotExist(err) {
		t.Fatal("revoked saved opt-in launched a process")
	}
}

func TestStartupRejectsUnsafeDiagnosticFile(t *testing.T) {
	binary, path, dir := startupFixture(t)
	target := filepath.Join(t.TempDir(), "preserve")
	if err := os.WriteFile(target, []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "monitor.log")); err != nil {
		t.Fatal(err)
	}
	if err := EnsureRunning(context.Background(), binary, path, dir); err == nil {
		t.Fatal("followed diagnostic symlink")
	}
	data, _ := os.ReadFile(target)
	if string(data) != "preserve" {
		t.Fatal("modified unrelated file")
	}
}

func TestManualMonitorWinningStartupRaceIsReused(t *testing.T) {
	binary, path, dir := startupFixture(t)
	t.Setenv("PR_BOARD_START_MODE", "compete")
	done := make(chan error, 1)
	go func() { done <- EnsureRunning(context.Background(), binary, path, dir) }()
	waitStartup(t, func() bool { _, err := os.Stat(filepath.Join(dir, "starts")); return err == nil })
	owner, err := localstate.TryLock(filepath.Join(dir, "monitor.lock"))
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	if err := os.WriteFile(filepath.Join(dir, "compete-go"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatalf("manual winner reported as startup failure: %v", err)
	}
}
