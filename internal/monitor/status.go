package monitor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/cdowell09/herdr-pr-board/internal/config"
)

type State string

const (
	Running State = "running"
	Stopped State = "stopped"
	Unknown State = "unknown"
)

// Status separates process ownership from the latest observation's usability.
type Status struct {
	State         State
	ObservedAt    time.Time
	ObservationOK bool
	Message       string
}

// Inspect reads local ownership and the stored snapshot without GitHub requests.
func Inspect(dir string, cfg config.Config) Status {
	if !filepath.IsAbs(dir) {
		return Status{State: Unknown, Message: "absolute state directory is required"}
	}
	info, err := os.Stat(dir)
	if errors.Is(err, os.ErrNotExist) {
		return Status{State: Stopped, Message: "start the monitor in another terminal"}
	}
	if err != nil {
		return Status{State: Unknown, Message: err.Error()}
	}
	if !info.IsDir() {
		return Status{State: Unknown, Message: "state path must be a directory"}
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	running, err := hasOwner(ctx, dir)
	if errors.Is(err, os.ErrNotExist) || err == nil && !running {
		return Status{State: Stopped, Message: "start the monitor in another terminal"}
	}
	if err != nil {
		return Status{State: Unknown, Message: err.Error()}
	}
	status := Status{State: Running}
	snapshot, err := (&Source{dir: dir, cfg: cfg}).read()
	if err != nil {
		status.Message = err.Error()
		return status
	}
	status.ObservedAt = snapshot.FinishedAt
	status.ObservationOK = len(snapshot.Errors) == 0 && snapshot.CapacityErr == nil && !snapshot.StartedAt.IsZero() && !snapshot.FinishedAt.Before(snapshot.StartedAt)
	for _, view := range snapshot.Views {
		status.ObservationOK = status.ObservationOK && view.Err == nil && !view.ObservedAt.Before(snapshot.StartedAt) && !view.UpdatedAt.Before(snapshot.StartedAt)
	}
	if !status.ObservationOK {
		status.Message = "waiting for a successful full observation"
	}
	return status
}
