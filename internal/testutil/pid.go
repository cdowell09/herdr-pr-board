package testutil

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// WaitForPID waits for a fixture's published PID before testing process behavior.
// A missing or invalid PID is not readiness. Owner exit ends startup early.
func WaitForPID(ctx context.Context, path string, exited <-chan struct{}) (int, error) {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		data, err := os.ReadFile(path)
		if err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil && pid > 0 {
				return pid, nil
			}
		} else if !os.IsNotExist(err) {
			return 0, err
		}
		select {
		case <-exited:
			return 0, errors.New("fixture owner exited before publishing a PID")
		case <-ctx.Done():
			return 0, fmt.Errorf("waiting for fixture PID: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}
