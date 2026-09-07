package monitor

import (
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/cdowell09/herdr-pr-board/internal/config"
	"github.com/cdowell09/herdr-pr-board/internal/localstate"
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
	owner, err := localstate.TryLock(filepath.Join(dir, "monitor.lock"))
	if err == nil {
		owner.Close()
		return Status{State: Stopped, Message: "start the monitor in another terminal"}
	}
	if errors.Is(err, os.ErrNotExist) {
		return Status{State: Stopped, Message: "start the monitor in another terminal"}
	}
	if !errors.Is(err, localstate.ErrLocked) {
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
