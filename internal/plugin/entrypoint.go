// Package plugin manages the board's native Herdr entrypoints.
package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/cdowell09/herdr-pr-board/internal/cli"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/cdowell09/herdr-pr-board/internal/localstate"
)

const ID = "cdowell09.pr-board"

// Open focuses the recorded board pane, or opens one dedicated tab.
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
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if pane := strings.TrimSpace(string(data)); pane != "" {
		if err := hostCommand(ctx, "plugin", "pane", "focus", pane).Run(); err == nil {
			return nil
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	command := hostCommand(ctx, "plugin", "pane", "open", "--plugin", ID, "--entrypoint", "board", "--placement", "tab", "--focus")
	var output, diagnostics bytes.Buffer
	bounded := &cli.LimitedWriter{Writer: &output, Remaining: 64 * 1024}
	command.Stdout = bounded
	command.Stderr = &cli.LimitedWriter{Writer: &diagnostics, Remaining: 8 * 1024}
	if err := command.Run(); err != nil {
		return fmt.Errorf("open board pane: %w: %s", err, strings.TrimSpace(diagnostics.String()))
	}
	if bounded.Truncated {
		return errors.New("pane response from Herdr exceeds 64 KiB")
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
	if err := json.Unmarshal(output.Bytes(), &response); err != nil {
		return fmt.Errorf("decode Herdr pane: %w", err)
	}
	pane := response.Result.PluginPane.Pane.ID
	if strings.TrimSpace(pane) == "" || strings.ContainsAny(pane, "\r\n") {
		return errors.New("pane response from Herdr has no valid pane ID")
	}
	// Record the synchronously created pane before another open can acquire the
	// lock, even when the new pane has not reached Prepare yet.
	return localstate.AtomicWrite(path, []byte(pane+"\n"))
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
			data, err := os.ReadFile(path)
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
