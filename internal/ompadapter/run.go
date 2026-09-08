// Package ompadapter runs the verified Oh My Pi CLI contract.
package ompadapter

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/cdowell09/herdr-pr-board/internal/agentadapter"
	"github.com/cdowell09/herdr-pr-board/internal/cli"
	"github.com/cdowell09/herdr-pr-board/internal/reviewercontract"
)

type Options struct{ OMP, Prompt, Skill string }

const supportedVersion = "18.1.14"

// Discovery provider IDs are versioned upstream. Reject other versions before
// launch because a new provider could bypass this exhaustive exclusion list.
const isolationSettings = `disabledProviders: [native, agents, agents-md, claude, claude-md, claude-plugins, codex, cursor, gemini, github, cline, opencode, windsurf, vscode, mcp-json, ssh-json, omp-plugins, agent-plugins, omp-managed, builtin-defaults]
personality: none
advisor:
  enabled: false
memory:
  backend: "off"
autolearn:
  enabled: false
externalThinking: false
speechgen:
  enabled: false
vault:
  enabled: false
workspace:
  additionalDirectories: []
`

func Run(ctx context.Context, in reviewercontract.Input, opts Options) error {
	return agentadapter.Run(ctx, in, agentadapter.Options{
		Name: "omp", Binary: opts.OMP, Prompt: opts.Prompt, Skill: opts.Skill,
		Command: func(binary, skill, work, checkout string) (*exec.Cmd, error) {
			if err := checkVersion(ctx, binary, checkout); err != nil {
				return nil, err
			}
			return command(binary, skill, work, checkout)
		}, FinalText: finalText,
	})
}

func checkVersion(ctx context.Context, binary, checkout string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.Command(binary, "--version", "--no-extensions")
	cmd.Dir = checkout
	if err := cli.ResolveNodeShim(cmd); err != nil {
		return err
	}
	var output bytes.Buffer
	limited := &cli.LimitedWriter{Writer: &output, Remaining: 1024}
	cmd.Stdout = limited
	if err := cli.RunProcess(ctx, cmd, time.Second); err != nil {
		return fmt.Errorf("check Oh My Pi version: %w", err)
	}
	if limited.Truncated || strings.TrimSpace(output.String()) != supportedVersion {
		return fmt.Errorf("omp adapter requires version %s", supportedVersion)
	}
	return nil
}

func command(binary, _, work, _ string) (*exec.Cmd, error) {
	settings := filepath.Join(work, "omp-review.yml")
	if err := os.WriteFile(settings, []byte(isolationSettings), 0600); err != nil {
		return nil, err
	}
	return exec.Command(binary,
		"--print", "--mode", "json", "--no-session", "--config", settings,
		"--no-extensions", "--no-skills", "--no-rules", "--no-lsp", "--no-title", "--no-prewalk",
		"--system-prompt", "", "--append-system-prompt", "", "--tools", "read,grep,glob",
	), nil
}
