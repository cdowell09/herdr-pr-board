// Package plugin manages the board's native Herdr entrypoints.
package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/cdowell09/herdr-pr-board/internal/cli"
	"github.com/cdowell09/herdr-pr-board/internal/localstate"
	"github.com/cdowell09/herdr-pr-board/internal/sidebar"
)

const ID = "cdowell09.pr-board"

// Entrypoint names the manifest pane for this platform. Herdr 0.8 on Unix
// resolves pane commands through PATH only, so the Unix pane starts through a
// bash wrapper while the Windows pane runs the binary from the plugin root.
func Entrypoint() string {
	if runtime.GOOS == "windows" {
		return "board-windows"
	}
	return "board"
}

// Open shows the recorded board pane in the active workspace, or opens one
// dedicated tab. A board in another workspace moves to the active workspace.
func Open(ctx context.Context) error {
	dir, err := localstate.Dir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	lock, err := localstate.Lock(ctx, filepath.Join(dir, "pane.lock"))
	if err != nil {
		return err
	}
	defer lock.Close()
	path := filepath.Join(dir, "pane-id")
	data, err := localstate.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if recorded := strings.TrimSpace(string(data)); recorded != "" {
		err := showPane(ctx, recorded)
		if err == nil {
			return nil
		}
		// Replace only a board that no longer exists. Any other failure keeps the
		// record so that the open action never starts a second board.
		if !errors.Is(err, errPaneNotFound) {
			return err
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	var response struct {
		Result struct {
			PluginPane struct {
				Pane struct {
					ID string `json:"pane_id"`
				} `json:"pane"`
			} `json:"plugin_pane"`
		} `json:"result"`
	}
	if err := hostJSON(ctx, &response, "plugin", "pane", "open", "--plugin", ID, "--entrypoint", Entrypoint(), "--placement", "tab", "--focus"); err != nil {
		return err
	}
	pane := response.Result.PluginPane.Pane.ID
	if strings.TrimSpace(pane) == "" || strings.ContainsAny(pane, "\r\n") {
		return errors.New("pane response from Herdr has no valid pane ID")
	}
	// Record the synchronously created pane before another open can acquire the
	// lock, even when the new pane has not reached Prepare yet.
	return localstate.AtomicWrite(path, []byte(pane+"\n"))
}

// showPane shows the recorded board in the workspace that the user views.
// Herdr runs plugin actions with that workspace; a board elsewhere moves to a
// new tab there. Plugin focus does not switch the user's workspace. It reports
// where the pane runs now and fails when the ID no longer names a plugin pane,
// for example a plain shell that Herdr restored after a restart. Herdr gives a
// moved pane a new ID and keeps earlier IDs as aliases, so the record keeps the
// board's first ID and cleanup still finds it.
func showPane(ctx context.Context, recorded string) error {
	var focused struct {
		Result struct {
			PluginPane struct {
				Pane struct {
					ID          string `json:"pane_id"`
					WorkspaceID string `json:"workspace_id"`
				} `json:"pane"`
			} `json:"plugin_pane"`
		} `json:"result"`
	}
	if err := hostJSON(ctx, &focused, "plugin", "pane", "focus", recorded); err != nil {
		return err
	}
	pane := focused.Result.PluginPane.Pane
	if pane.ID == "" || pane.WorkspaceID == "" {
		return errors.New("pane response from Herdr has no pane location")
	}
	workspace := os.Getenv("HERDR_WORKSPACE_ID")
	if workspace == "" || workspace == pane.WorkspaceID {
		return nil
	}
	var response struct {
		Result struct {
			MoveResult struct {
				Changed           bool    `json:"changed"`
				Reason            *string `json:"reason"`
				ClosedWorkspaceID *string `json:"closed_workspace_id"`
			} `json:"move_result"`
		} `json:"result"`
	}
	if err := hostJSON(ctx, &response, "pane", "move", pane.ID, "--new-tab", "--workspace", workspace, "--label", "PR Board", "--focus"); err != nil {
		return err
	}
	move := response.Result.MoveResult
	if !move.Changed {
		reason := "no reason given"
		if move.Reason != nil {
			reason = *move.Reason
		}
		return fmt.Errorf("herdr did not move the board pane: %s", reason)
	}
	if move.ClosedWorkspaceID != nil && *move.ClosedWorkspaceID == pane.WorkspaceID {
		return nil
	}
	// Sidebar tokens without a TTL never expire. The board reports to its new
	// workspace on the next refresh, so clear the tokens in the workspace it left.
	// ponytail: one clear, no retry; a failed clear or a report that races the
	// move leaves stale tokens only with sidebar.ttl = "0", the same as closing
	// the board. Persist pending clears in the sidebar package if that matters.
	if out, err := hostCommand(ctx, sidebar.ClearArgs(pane.WorkspaceID)...).CombinedOutput(); err != nil {
		return fmt.Errorf("board moved, but clearing its sidebar tokens in %s failed: %w: %s", pane.WorkspaceID, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// errPaneNotFound reports that Herdr has no pane, or no plugin pane, for an ID.
var errPaneNotFound = errors.New("herdr pane not found")

// hostJSON runs one Herdr command and decodes its bounded JSON response.
func hostJSON(ctx context.Context, response any, args ...string) error {
	command := hostCommand(ctx, args...)
	var output, diagnostics bytes.Buffer
	bounded := &cli.LimitedWriter{Writer: &output, Remaining: 64 * 1024}
	command.Stdout = bounded
	command.Stderr = &cli.LimitedWriter{Writer: &diagnostics, Remaining: 8 * 1024}
	if err := command.Run(); err != nil {
		var failure struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		if json.Unmarshal(diagnostics.Bytes(), &failure) == nil && (failure.Error.Code == "pane_not_found" || failure.Error.Code == "plugin_pane_not_found") {
			return errPaneNotFound
		}
		return fmt.Errorf("herdr %s %s: %w: %s", args[0], args[1], err, strings.TrimSpace(diagnostics.String()))
	}
	if bounded.Truncated {
		return errors.New("pane response from Herdr exceeds 64 KiB")
	}
	if err := json.Unmarshal(output.Bytes(), response); err != nil {
		return fmt.Errorf("decode Herdr pane: %w", err)
	}
	return nil
}

// Prepare records pane ownership before the board runs in this process. The
// cleanup function removes only this pane's record and never changes config.
func Prepare(ctx context.Context) (string, func() error, error) {
	configDir := os.Getenv("HERDR_PLUGIN_CONFIG_DIR")
	if strings.TrimSpace(configDir) == "" {
		return "", nil, errors.New("HERDR_PLUGIN_CONFIG_DIR is required")
	}
	dir, err := localstate.Dir()
	if err != nil {
		return "", nil, err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", nil, err
	}
	pane := os.Getenv("HERDR_PANE_ID")
	if pane != "" {
		if err := changePane(ctx, dir, func(path string) error { return localstate.AtomicWrite(path, []byte(pane+"\n")) }); err != nil {
			return "", nil, err
		}
	}
	if tab := os.Getenv("HERDR_TAB_ID"); tab != "" {
		_ = hostCommand(ctx, "tab", "rename", tab, "PR Board").Run()
	}
	cleanup := func() error {
		if pane == "" {
			return nil
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return changePane(ctx, dir, func(path string) error {
			data, err := localstate.ReadFile(path)
			if os.IsNotExist(err) {
				return nil
			}
			if err != nil {
				return err
			}
			if strings.TrimSpace(string(data)) != pane {
				return nil
			}
			return os.Remove(path)
		})
	}
	return filepath.Join(configDir, "config.toml"), cleanup, nil
}

func changePane(ctx context.Context, dir string, change func(string) error) error {
	lock, err := localstate.Lock(ctx, filepath.Join(dir, "pane.lock"))
	if err != nil {
		return err
	}
	defer lock.Close()
	return change(filepath.Join(dir, "pane-id"))
}

func hostCommand(ctx context.Context, args ...string) *exec.Cmd {
	binary := os.Getenv("HERDR_BIN_PATH")
	if binary == "" {
		binary = "herdr"
	}
	return exec.CommandContext(ctx, binary, args...)
}
