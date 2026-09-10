// Package notification shows review notifications through the herdr CLI.
package notification

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/cdowell09/herdr-pr-board/internal/cli"
	"github.com/cdowell09/herdr-pr-board/internal/config"
	"github.com/cdowell09/herdr-pr-board/internal/reviewmemory"
)

const (
	// DefaultBinary is the herdr CLI used when Notifier.Binary is empty.
	DefaultBinary = "herdr"
	// Timeout bounds one herdr notification command.
	Timeout = 10 * time.Second
	// reasonLimit caps the run message shown for blocked and failed runs.
	reasonLimit = 120
)

// Notifier shows a review notification for a finished run. It reads
// review.notify from the configuration file when the run finishes, so a saved
// change applies to reviews that were already queued or running. Every shown
// value comes from the recorded run, so a notification never touches GitHub.
type Notifier struct {
	// Runner executes the herdr CLI. It defaults to running Binary.
	Runner cli.Runner
	// Binary is the herdr CLI path. Empty means DefaultBinary on PATH.
	Binary string
	// ConfigPath is the configuration file that holds review.notify.
	ConfigPath string
}

// New builds the notifier for one process. It returns nil when no workspace
// ID is known, which is the case when the process runs outside Herdr.
func New(configPath, workspaceID, binary string) *Notifier {
	if strings.TrimSpace(workspaceID) == "" {
		return nil
	}
	return &Notifier{Binary: binary, ConfigPath: configPath}
}

// Notify shows the notification for a finished run. A nil notifier and a run
// outcome outside the configured mode do nothing. A missing herdr CLI returns
// the command error, which callers report without failing the run.
func (n *Notifier) Notify(ctx context.Context, run reviewmemory.Run) error {
	if n == nil {
		return nil
	}
	cfg, err := config.LoadExisting(n.ConfigPath)
	if err != nil {
		return fmt.Errorf("read review.notify: %w", err)
	}
	if !selects(cfg.Review.Notify, run.Status) {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	args := []string{"notification", "show", title(run)}
	if body := body(run); body != "" {
		args = append(args, "--body", body)
	}
	args = append(args, "--sound", sound(run.Status))
	if _, err := n.runner()(ctx, args...); err != nil {
		return fmt.Errorf("show review notification: %w", err)
	}
	return nil
}

func (n *Notifier) runner() cli.Runner {
	if n.Runner != nil {
		return n.Runner
	}
	binary := n.Binary
	if binary == "" {
		binary = DefaultBinary
	}
	return cli.Command(binary)
}

// selects reports whether the mode notifies on this run outcome. Stopped runs
// never reach here: the caller recognizes them by their error, not the status.
func selects(mode config.NotifyMode, status reviewmemory.Status) bool {
	switch status {
	case reviewmemory.Completed:
		return mode == config.NotifyAll
	case reviewmemory.Blocked, reviewmemory.Failed:
		return mode != config.NotifyOff
	}
	return false
}

func title(run reviewmemory.Run) string {
	return "Review " + string(run.Status) + ": " + run.Identity.Repository + " #" + strconv.Itoa(run.Identity.Number)
}

// body lists every severity count for a completion and the reason otherwise.
func body(run reviewmemory.Run) string {
	if run.Status != reviewmemory.Completed {
		return reason(run.Message)
	}
	return reviewmemory.CountSeverities(run.Findings).String()
}

// reason keeps the first clause of a run message. Later clauses hold reviewer
// stderr and diagnostics paths that do not fit a notification.
func reason(message string) string {
	message, _, _ = strings.Cut(message, ";")
	message = strings.TrimSpace(message)
	if runes := []rune(message); len(runes) > reasonLimit {
		message = string(runes[:reasonLimit])
	}
	return message
}

func sound(status reviewmemory.Status) string {
	if status == reviewmemory.Completed {
		return "done"
	}
	return "request"
}
