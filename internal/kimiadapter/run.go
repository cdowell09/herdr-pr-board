// Package kimiadapter runs isolated reviews through Kimi's Wire protocol.
package kimiadapter

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/cdowell09/herdr-pr-board/internal/agentadapter"
	"github.com/cdowell09/herdr-pr-board/internal/reviewercontract"
)

type Options struct{ Kimi, Prompt, Skill string }

// Kimi CLI 1.50.0 stores normal login credentials under this OAuth reference.
// Kimi resolves the reference itself; the adapter never reads credentials.
// The keyring reference also lets Kimi migrate older logins to its file store.
const reviewConfig = `default_model = "review"
telemetry = false
merge_all_available_skills = false
hooks = []

[providers.review]
type = "kimi"
base_url = "https://api.kimi.com/coding/v1"
api_key = ""
oauth = { storage = "keyring", key = "oauth/kimi-code" }

[models.review]
provider = "review"
model = "kimi-for-coding"
max_context_size = 262144
`

const reviewAgent = `version: 1
agent:
  name: pr-board
  system_prompt_path: kimi-system.md
  tools:
    - "kimi_cli.tools.file:ReadFile"
    - "kimi_cli.tools.file:Glob"
    - "kimi_cli.tools.file:Grep"
  subagents: {}
`

func Run(ctx context.Context, in reviewercontract.Input, opts Options) error {
	return agentadapter.Run(ctx, in, agentadapter.Options{
		Name: "kimi", Binary: opts.Kimi, Prompt: opts.Prompt, Skill: opts.Skill,
		Command: command, PrepareIO: prepareIO, FinalText: finalText,
	})
}

func command(binary, _, work, _ string) (*exec.Cmd, error) {
	for name, content := range map[string]string{
		"kimi-config.toml": reviewConfig,
		"kimi-agent.yaml":  reviewAgent,
		"kimi-system.md":   "Follow the review instructions in the user message. Return only the requested JSON result.\n",
		"kimi-mcp.json":    `{"mcpServers":{}}`,
	} {
		if err := os.WriteFile(filepath.Join(work, name), []byte(content), 0600); err != nil {
			return nil, err
		}
	}
	skills := filepath.Join(work, "kimi-skills")
	if err := os.Mkdir(skills, 0700); err != nil {
		return nil, err
	}
	return exec.Command(binary,
		"--wire", "--afk", "--config-file", filepath.Join(work, "kimi-config.toml"),
		"--agent-file", filepath.Join(work, "kimi-agent.yaml"),
		"--mcp-config-file", filepath.Join(work, "kimi-mcp.json"), "--skills-dir", skills,
	), nil
}
