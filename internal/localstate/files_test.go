package localstate

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLockCancellationAndAtomicReplacement(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "lock")
	owner, err := TryLock(path)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	if _, err := TryLock(path); !errors.Is(err, ErrLocked) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Lock(ctx, path); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	state := filepath.Join(dir, "data")
	for _, value := range []string{"first", "replacement"} {
		if err := AtomicWrite(state, []byte(value)); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(state)
		if err != nil || string(data) != value {
			t.Fatalf("%q %v", data, err)
		}
	}
	entries, err := filepath.Glob(filepath.Join(dir, ".state-*"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("temporary files: %v %v", entries, err)
	}
	t.Setenv("HERDR_PLUGIN_STATE_DIR", "")
	if _, err := Dir(); err == nil {
		t.Fatal("accepted missing state directory")
	}
}
