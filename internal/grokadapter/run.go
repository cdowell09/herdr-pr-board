// Package grokadapter runs the native Grok CLI with isolated review settings.
package grokadapter

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/cdowell09/herdr-pr-board/internal/agentadapter"
	"github.com/cdowell09/herdr-pr-board/internal/reviewercontract"
)

type Options struct{ Grok, Prompt, Skill string }

func Run(ctx context.Context, in reviewercontract.Input, opts Options) error {
	var session, checkout string
	return agentadapter.Run(ctx, in, agentadapter.Options{
		Name: "grok", Binary: opts.Grok, Prompt: opts.Prompt, Skill: opts.Skill,
		Command: func(binary, _ string, work, captured string) (*exec.Cmd, error) {
			session, checkout = filepath.Join(work, "grok-session"), captured
			return command(ctx, binary, session)
		},
		PrepareIO: func(cmd *exec.Cmd, prompt string) (func(), error) {
			// The runtime discovers configuration from its own empty repository.
			// Read tools use the captured checkout's absolute path from the prompt.
			cmd.Dir = session
			cmd.Stdin = nil
			prompt = fmt.Sprintf("The captured checkout is %q. Use absolute paths to read its files.\n\n%s", checkout, prompt)
			err := os.WriteFile(filepath.Join(session, "prompt.txt"), []byte(prompt), 0600)
			return func() {}, err
		},
		FinalText: finalText,
	})
}

func command(ctx context.Context, binary, session string) (*exec.Cmd, error) {
	for _, path := range []string{session, filepath.Join(session, "home"), filepath.Join(session, "state")} {
		if err := os.Mkdir(path, 0700); err != nil {
			return nil, err
		}
	}
	env, err := environment(session)
	if err != nil {
		return nil, err
	}
	config := filepath.Join(session, "state", "config.toml")
	if err := os.WriteFile(config, []byte(settings), 0600); err != nil {
		return nil, err
	}
	// A real repository boundary stops discovery at the session directory, even
	// when the configured result directory is inside another repository.
	if _, err := prepare(ctx, "git", env, session, "-c", "core.hooksPath="+os.DevNull, "init", "--quiet", "--template="); err != nil {
		return nil, fmt.Errorf("initialize Grok session directory: %w", err)
	}
	report, err := prepare(ctx, binary, env, session, "inspect", "--json")
	if err != nil {
		return nil, fmt.Errorf("inspect Grok review configuration: %w", err)
	}
	if err := checkInspection(report, config); err != nil {
		return nil, err
	}
	cmd := exec.Command(binary,
		"--cwd", session, "--prompt-file", filepath.Join(session, "prompt.txt"),
		"--output-format", "streaming-messages-json", "--model", "grok-build",
		"--tools", "read_file,grep,list_dir", "--disallowed-tools", "search_tool,use_tool",
		"--no-subagents", "--no-plan", "--disable-web-search", "--permission-mode", "dontAsk",
		"--system-prompt-override", "Review only the captured checkout. Follow the review instructions in the user message. Read repository standards as evidence. Do not change files, run commands, or publish. Return only the requested JSON result.",
	)
	cmd.Env = env
	return cmd, nil
}

func environment(session string) ([]string, error) {
	// Only the native CLI reads and refreshes this path. Never copy credentials
	// into the temporary configuration, logs, prompt, or process arguments.
	auth := os.Getenv("GROK_AUTH_PATH")
	if auth == "" {
		home := os.Getenv("GROK_HOME")
		if home == "" {
			userHome, err := os.UserHomeDir()
			if err != nil {
				return nil, err
			}
			home = filepath.Join(userHome, ".grok")
		}
		auth = filepath.Join(home, "auth.json")
	}
	auth, err := filepath.Abs(auth)
	if err != nil {
		return nil, err
	}
	var env []string
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		name = strings.ToUpper(name)
		if name == "GROK_AUTH" { // Native inline login is also handled by Grok.
			env = append(env, entry)
			continue
		}
		if strings.HasPrefix(name, "GROK_") || strings.HasPrefix(name, "GIT_") || strings.HasPrefix(name, "XDG_") ||
			name == "HOME" || name == "USERPROFILE" || name == "HOMEDRIVE" || name == "HOMEPATH" ||
			name == "NODE_OPTIONS" || name == "NODE_PATH" || name == "BASH_ENV" || name == "ENV" {
			continue
		}
		env = append(env, entry)
	}
	home := filepath.Join(session, "home")
	return append(env, "HOME="+home, "USERPROFILE="+home,
		"XDG_CONFIG_HOME="+home, "XDG_CACHE_HOME="+home, "XDG_DATA_HOME="+home,
		"GROK_HOME="+filepath.Join(session, "state"), "GROK_AUTH_PATH="+auth,
		"GROK_DISABLE_AUTOUPDATER=1", "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1"), nil
}

const settings = `[cli]
auto_update = false
use_leader = false
[toolset.bash]
login_shell_capture = false
[memory]
enabled = false
[managed_mcps]
enabled = false
gateway_tools_enabled = false
[subagents]
enabled = false
[features]
managed_config = false
campaigns = false
remote_fetch = false
telemetry = false
session_recap = false
session_search = false
title_refresh = false
turn_summary = false
backend_tools = false
codebase_indexing = false
lsp_tools = false
mcp_recursive_config_watch = false
[compat.claude]
agents = false
hooks = false
mcps = false
rules = false
skills = false
sessions = false
[compat.cursor]
agents = false
hooks = false
mcps = false
rules = false
skills = false
sessions = false
[compat.codex]
hooks = false
skills = false
sessions = false
`
