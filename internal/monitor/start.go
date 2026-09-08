package monitor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/cdowell09/herdr-pr-board/internal/cli"
	"github.com/cdowell09/herdr-pr-board/internal/config"
	"github.com/cdowell09/herdr-pr-board/internal/localstate"
)

const readyEnvironment = "HERDR_MONITOR_READY_FD"
const BackgroundLogLimit = 1 << 20

// EnsureRunning starts only from saved opt-ins. Its context bounds startup, not
// the monitor lifetime. Callers must not attach it to the board's lifetime.
func EnsureRunning(ctx context.Context, binary, path, dir string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	enabled, err := automaticSaved(path)
	if err != nil || !enabled {
		return err
	}
	if !filepath.IsAbs(dir) {
		return errors.New("automatic monitor requires an absolute state directory")
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return err
	}
	binary, err = filepath.Abs(binary)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	start, err := localstate.Lock(ctx, filepath.Join(dir, "monitor-start.lock"))
	if err != nil {
		return err
	}
	defer start.Close()
	running, err := hasOwner(dir)
	if err != nil || running {
		return err
	}
	// Permissions may change while another board holds the startup lock.
	enabled, err = automaticSaved(path)
	if err != nil || !enabled {
		return err
	}
	logPath := filepath.Join(dir, "monitor.log")
	if err := launch(ctx, binary, path, dir, logPath); err != nil {
		if running, checkErr := hasOwner(dir); checkErr == nil && running {
			return nil
		}
		return fmt.Errorf("start monitor: %w (log: %s)", err, logPath)
	}
	return nil
}

func hasOwner(dir string) (bool, error) {
	owner, err := localstate.TryLock(filepath.Join(dir, "monitor.lock"))
	if errors.Is(err, localstate.ErrLocked) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	owner.Close()
	return false, nil
}

func launch(ctx context.Context, binary, path, dir, logPath string) error {
	log, err := localstate.OpenRegular(logPath, true)
	if err != nil {
		return err
	}
	defer log.Close()
	input, err := os.Open(os.DevNull)
	if err != nil {
		return err
	}
	defer input.Close()
	reader, writer, err := os.Pipe()
	if err != nil {
		return err
	}
	defer reader.Close()
	defer writer.Close()
	cmd := exec.Command(binary, "--monitor", "--config", path)
	detachMonitor(cmd)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = input, log, log
	cmd.Env = monitorEnvironment(dir)
	if err := cli.PassFile(cmd, writer, readyEnvironment); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	writer.Close()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	ready := make(chan error, 1)
	go func() {
		var b [1]byte
		_, err := io.ReadFull(reader, b[:])
		if err == nil && b[0] != 1 {
			err = errors.New("invalid monitor readiness acknowledgement")
		}
		ready <- err
	}()
	select {
	case err = <-ready:
		if err == nil {
			var running bool
			running, err = hasOwner(dir)
			if err == nil && !running {
				err = errors.New("monitor exited before retaining ownership")
			}
		}
		if err == nil {
			return nil
		}
	case err = <-done:
		if err == nil {
			err = errors.New("monitor exited before readiness")
		}
		return err
	case <-ctx.Done():
		err = ctx.Err()
	}
	stopStartup(cmd, done)
	return err
}

func monitorEnvironment(dir string) []string {
	var env []string
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, readyEnvironment+"=") && !strings.HasPrefix(entry, "HERDR_PLUGIN_STATE_DIR=") {
			env = append(env, entry)
		}
	}
	return append(env, "HERDR_PLUGIN_STATE_DIR="+dir)
}

func stopStartup(cmd *exec.Cmd, done <-chan error) {
	_ = cmd.Process.Signal(syscall.SIGTERM)
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
		_ = cmd.Process.Kill()
		<-done
	}
}

// ReadyPipe consumes the private launcher handshake before subprocesses start.
// A foreground monitor has no readiness pipe.
func ReadyPipe() (*os.File, error) {
	pipe, err := cli.TakeInheritedFile(readyEnvironment)
	if unsetErr := os.Unsetenv(readyEnvironment); unsetErr != nil {
		if pipe != nil {
			pipe.Close()
		}
		return nil, unsetErr
	}
	if err != nil || pipe == nil {
		return pipe, err
	}
	info, err := pipe.Stat()
	if err != nil {
		pipe.Close()
		return nil, err
	}
	if info.Mode()&os.ModeNamedPipe == 0 {
		pipe.Close()
		return nil, errors.New("monitor readiness descriptor must be a pipe")
	}
	return pipe, nil
}

func automaticSaved(path string) (bool, error) {
	cfg, err := config.LoadExisting(path)
	if err != nil {
		return false, err
	}
	if len(cfg.Review.AutoViews) == 0 {
		return false, nil
	}
	for _, repo := range cfg.Repositories {
		if repo.AutoLaunch {
			return true, nil
		}
	}
	return false, nil
}

// AcknowledgeReady releases the handshake before discovery. A vanished launcher
// does not own the monitor lifetime; explicit startup cancellation sends TERM.
func AcknowledgeReady(pipe *os.File) error {
	if pipe == nil {
		return nil
	}
	_, err := pipe.Write([]byte{1})
	closeErr := pipe.Close()
	if errors.Is(err, syscall.EPIPE) {
		err = nil
	}
	return errors.Join(err, closeErr)
}
