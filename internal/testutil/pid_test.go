package testutil

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestWaitForPIDRequiresValidReadiness(t *testing.T) {
	for _, contents := range []string{"", "partial", "0", "-1", "42\n"} {
		t.Run(contents, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "pid")
			if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			pid, err := WaitForPID(ctx, path, nil)
			if contents == "42\n" {
				if pid != 42 || err != nil {
					t.Fatalf("pid=%d error=%v", pid, err)
				}
			} else if pid != 0 || !errors.Is(err, context.Canceled) {
				t.Fatalf("invalid readiness: pid=%d error=%v", pid, err)
			}
		})
	}
	closed := make(chan struct{})
	close(closed)
	if pid, err := WaitForPID(context.Background(), filepath.Join(t.TempDir(), "absent"), closed); pid != 0 || err == nil {
		t.Fatalf("owner exit ignored: pid=%d error=%v", pid, err)
	}
}
