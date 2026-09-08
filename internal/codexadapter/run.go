// Package codexadapter translates Codex CLI events into validated review results.
package codexadapter

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"

	"github.com/cdowell09/herdr-pr-board/internal/agentadapter"
	"github.com/cdowell09/herdr-pr-board/internal/reviewercontract"
)

type Options struct{ Codex, Skill string }

// Run uses Codex's existing authentication without reading or copying credentials.
func Run(ctx context.Context, in reviewercontract.Input, opts Options) error {
	return agentadapter.Run(ctx, in, agentadapter.Options{Name: "codex", Binary: opts.Codex, Skill: opts.Skill,
		Command: command, FinalText: finalText, CancelSignal: syscall.SIGINT})
}

// These switches target Codex CLI 0.153.4. An unsupported switch fails closed.
func command(binary, _, work string) (*exec.Cmd, error) {
	schema := filepath.Join(work, "result-schema.json")
	if err := os.WriteFile(schema, []byte(agentadapter.ResultSchema), 0600); err != nil {
		return nil, err
	}
	args := []string{"exec", "--json", "--ephemeral", "--ignore-user-config", "--ignore-rules", "--sandbox", "read-only", "--color", "never", "--output-schema", schema,
		"-c", `approval_policy="never"`,
		"-c", `project_doc_max_bytes=0`,
		"-c", "projects." + strconv.Quote(filepath.Join(work, "checkout")) + `.trust_level="untrusted"`,
		"-c", `allow_login_shell=false`,
		"-c", `web_search="disabled"`,
		"--enable", "skip_host_skill_discovery",
		"--enable", "multi_agent",
	}
	for _, feature := range []string{"hooks", "plugins", "remote_plugin", "apps", "memories", "skill_search", "skill_mcp_dependency_install", "shell_snapshot", "browser_use", "computer_use"} {
		args = append(args, "--disable", feature)
	}
	return exec.Command(binary, args...), nil
}
