package sidebar

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	gh "github.com/cdowell09/herdr-pr-board/internal/github"
)

// Reporter sends sidebar tokens to Herdr workspace metadata through the
// `herdr` CLI. It never touches GitHub, so reporting costs no API budget.
type Reporter struct {
	// Runner executes the herdr CLI. It defaults to ExecRunner.
	Runner gh.Runner
	// Bin is the herdr binary. It defaults to "herdr" or the value of
	// HERDR_BIN_PATH set by the caller.
	Bin string
	// WorkspaceID is the Herdr workspace that owns this board process.
	WorkspaceID string
	// TTL is how long the reported tokens stay visible. Zero disables
	// expiry and sends no --ttl-ms flag.
	TTL time.Duration
}

// ExecRunner runs the herdr CLI.
type ExecRunner struct{}

// Run executes the herdr binary with the given arguments.
func (ExecRunner) Run(ctx context.Context, args ...string) ([]byte, error) {
	bin := "herdr"
	if value := os.Getenv("HERDR_BIN_PATH"); value != "" {
		bin = value
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			message = err.Error()
		}
		return nil, errors.New(message)
	}
	return output, nil
}

func (r Reporter) runner() gh.Runner {
	if r.Runner != nil {
		return r.Runner
	}
	return ExecRunner{}
}

// Report writes the tokens to one workspace's metadata.
func (r Reporter) Report(ctx context.Context, workspaceID string, tokens map[string]string) error {
	if strings.TrimSpace(workspaceID) == "" {
		return errors.New("workspace ID is required")
	}
	args := []string{"workspace", "report-metadata", workspaceID, "--source", Source}
	names := make([]string, 0, len(tokens))
	for name := range tokens {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		args = append(args, "--token", name+"="+tokens[name])
	}
	if r.TTL > 0 {
		args = append(args, "--ttl-ms", strconv.FormatInt(r.TTL.Milliseconds(), 10))
	}
	if _, err := r.runner().Run(ctx, args...); err != nil {
		return fmt.Errorf("report metadata to %s: %w", workspaceID, err)
	}
	return nil
}
