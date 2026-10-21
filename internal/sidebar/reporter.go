package sidebar

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/cdowell09/herdr-pr-board/internal/cli"
	"github.com/cdowell09/herdr-pr-board/internal/config"
)

// DefaultBinary is the herdr CLI used when Reporter.Binary is empty.
const DefaultBinary = "herdr"

// Reporter sends sidebar tokens to Herdr workspace metadata through the
// `herdr` CLI. It never touches GitHub, so reporting costs no API budget.
type Reporter struct {
	// Runner executes the herdr CLI. It defaults to running Binary.
	Runner cli.Runner
	// Binary is the herdr CLI path. Empty means DefaultBinary on PATH.
	Binary string
	// WorkspaceID is the Herdr workspace that started this board process. It
	// marks a board inside Herdr; Report asks Herdr for the current workspace.
	WorkspaceID string
	// TTL is how long the reported tokens stay visible. Zero disables
	// expiry and sends no --ttl-ms flag.
	TTL time.Duration
}

// NewReporter builds the reporter for one board process. It returns nil
// when reporting is disabled in the configuration or when no workspace ID
// is known, which is the case when the board runs outside Herdr.
func NewReporter(cfg config.SidebarConfig, workspaceID, binary string) *Reporter {
	workspaceID = strings.TrimSpace(workspaceID)
	if !cfg.SidebarEnabled() || workspaceID == "" {
		return nil
	}
	ttl, _ := cfg.TTLEvery() // validated by config.Load
	return &Reporter{Binary: binary, WorkspaceID: workspaceID, TTL: ttl}
}

func (r Reporter) runner() cli.Runner {
	if r.Runner != nil {
		return r.Runner
	}
	bin := r.Binary
	if bin == "" {
		bin = DefaultBinary
	}
	return cli.Command(bin)
}

// ClearArgs returns the herdr arguments that clear every board token from a
// workspace. Herdr prints nothing when the command succeeds.
func ClearArgs(workspaceID string) []string {
	return []string{"workspace", "report-metadata", workspaceID, "--source", Source,
		"--clear-token", TokenOpen, "--clear-token", TokenReview, "--clear-token", TokenCI}
}

// Report writes the tokens to the metadata of the workspace that runs the
// board now. The open action can move the board pane to another workspace.
func (r Reporter) Report(ctx context.Context, tokens map[string]string) error {
	if strings.TrimSpace(r.WorkspaceID) == "" {
		return errors.New("workspace ID is required")
	}
	out, err := r.runner()(ctx, "pane", "current")
	if err != nil {
		return fmt.Errorf("find the board workspace: %w", err)
	}
	var current struct {
		Result struct {
			Pane struct {
				WorkspaceID string `json:"workspace_id"`
			} `json:"pane"`
		} `json:"result"`
	}
	if err := json.Unmarshal(out, &current); err != nil {
		return fmt.Errorf("decode the board workspace: %w", err)
	}
	workspaceID := current.Result.Pane.WorkspaceID
	if workspaceID == "" {
		return errors.New("herdr reported no workspace for the board pane")
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
	if _, err := r.runner()(ctx, args...); err != nil {
		return fmt.Errorf("report metadata to %s: %w", workspaceID, err)
	}
	return nil
}
