package monitor

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/cdowell09/herdr-pr-board/internal/config"
	"github.com/cdowell09/herdr-pr-board/internal/localstate"
)

func TestOwnershipWaitsForProbeToClose(t *testing.T) {
	for _, operation := range []string{"check", "claim"} {
		t.Run(operation, func(t *testing.T) {
			dir := t.TempDir()
			guard, err := localstate.TryLock(filepath.Join(dir, "monitor-owner.lock"))
			if err != nil {
				t.Fatal(err)
			}
			defer guard.Close()
			probe, err := localstate.TryLock(filepath.Join(dir, "monitor.lock"))
			if err != nil {
				t.Fatal(err)
			}
			defer probe.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			defer cancel()
			if operation == "check" {
				_, err = hasOwner(ctx, dir)
			} else {
				owner, claimErr := claimOwner(ctx, dir)
				if owner != nil {
					owner.Close()
				}
				err = claimErr
			}
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("treated a probe as ownership: %v", err)
			}
		})
	}
}

func TestInspectionDoesNotSuppressStartup(t *testing.T) {
	_, path, dir := startupFixture(t)
	cfg, err := config.LoadExisting(path)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	var wg sync.WaitGroup
	for n := 0; n < 8; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-done:
					return
				default:
					Inspect(dir, cfg)
				}
			}
		}()
	}
	defer func() { close(done); wg.Wait() }()
	for i := 0; i < 100; i++ {
		err := EnsureRunning(context.Background(), filepath.Join(dir, "missing-binary"), path, dir)
		if !errors.Is(err, os.ErrNotExist) && !errors.Is(err, exec.ErrNotFound) {
			t.Fatalf("startup must try the missing executable on iteration %d: %v", i, err)
		}
	}
}
