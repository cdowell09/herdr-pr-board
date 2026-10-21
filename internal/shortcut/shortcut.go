// Package shortcut launches command shortcuts in new Herdr panes through the
// herdr CLI. The board does not track a launched pane.
package shortcut

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/cdowell09/herdr-pr-board/internal/cli"
	"github.com/cdowell09/herdr-pr-board/internal/config"
	gh "github.com/cdowell09/herdr-pr-board/internal/github"
)

const (
	// DefaultBinary is the herdr CLI used when Launcher.Binary is empty.
	DefaultBinary = "herdr"
	// Timeout bounds the herdr commands of one launch.
	Timeout = 10 * time.Second
	// commandVar carries the command into the new pane. The text typed into
	// the pane's shell is constant, so no shell parses the command except sh.
	commandVar = "PR_BOARD_COMMAND"
)

// Launcher runs command shortcuts through the herdr CLI.
type Launcher struct {
	// Runner executes the herdr CLI. It defaults to running Binary.
	Runner cli.Runner
	// Binary is the herdr CLI path. Empty means DefaultBinary on PATH.
	Binary string
}

// Launch opens a focused Herdr tab or a split right of the calling pane and
// runs the shortcut command for pr under sh -c.
func (l Launcher) Launch(ctx context.Context, shortcut config.Shortcut, pr gh.PullRequest) error {
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	args := []string{"tab", "create", "--label", fmt.Sprintf("%s %s#%d", shortcut.Name, pr.Repository, pr.Number), "--focus"}
	if shortcut.Where == config.ShortcutSplit {
		args = []string{"pane", "split", "--current", "--direction", "right", "--focus"}
	}
	if shortcut.Cwd != "" {
		cwd, err := expandHome(shortcut.Cwd)
		if err != nil {
			return err
		}
		args = append(args, "--cwd", cwd)
	}
	command := strings.NewReplacer("{url}", pr.URL, "{repo}", pr.Repository, "{number}", strconv.Itoa(pr.Number)).Replace(shortcut.Run)
	args = append(args, "--env", commandVar+"="+command)
	run := l.runner()
	output, err := run(ctx, args...)
	if err != nil {
		return fmt.Errorf("create Herdr pane: %w", err)
	}
	pane, err := paneID(output)
	if err != nil {
		return fmt.Errorf("create Herdr pane: %w", err)
	}
	// pane run types this text and Enter into the new pane's shell.
	if _, err := run(ctx, "pane", "run", pane, `sh -c "$`+commandVar+`"`); err != nil {
		return fmt.Errorf("run command in Herdr pane %s: %w", pane, err)
	}
	return nil
}

func (l Launcher) runner() cli.Runner {
	if l.Runner != nil {
		return l.Runner
	}
	binary := l.Binary
	if binary == "" {
		binary = DefaultBinary
	}
	return cli.Command(binary)
}

// paneID reads the new pane from a tab create or pane split response.
func paneID(output []byte) (string, error) {
	var response struct {
		Result struct {
			RootPane struct {
				PaneID string `json:"pane_id"`
			} `json:"root_pane"`
			Pane struct {
				PaneID string `json:"pane_id"`
			} `json:"pane"`
		} `json:"result"`
	}
	_ = json.Unmarshal(output, &response)
	if id := cmp.Or(response.Result.RootPane.PaneID, response.Result.Pane.PaneID); id != "" {
		return id, nil
	}
	return "", errors.New("no pane ID in Herdr output")
}

// expandHome replaces a leading ~ with the home directory.
func expandHome(path string) (string, error) {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("expand shortcut cwd: %w", err)
	}
	return filepath.Join(home, path[1:]), nil
}
