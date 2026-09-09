// Package copilotadapter runs isolated reviews through Copilot's native SDK protocol.
package copilotadapter

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/cdowell09/herdr-pr-board/internal/agentadapter"
	"github.com/cdowell09/herdr-pr-board/internal/reviewercontract"
)

type Options struct{ Copilot, Prompt, Skill string }

func Run(ctx context.Context, in reviewercontract.Input, opts Options) error {
	return agentadapter.Run(ctx, in, agentadapter.Options{
		Name: "copilot", Binary: opts.Copilot, Prompt: opts.Prompt, Skill: opts.Skill,
		Command: command, PrepareIO: prepareIO, FinalText: finalText,
	})
}

func command(binary, _, work, _ string) (*exec.Cmd, error) {
	if err := checkPolicy(); err != nil {
		return nil, err
	}
	config := filepath.Join(work, "copilot-config")
	if err := os.Mkdir(config, 0700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(config, "settings.json"), []byte(`{"disableAllHooks":true}`), 0600); err != nil {
		return nil, err
	}
	// The host retains its normal login. Only the SDK session uses this config.
	return exec.Command(binary, "--headless", "--stdio", "--no-auto-update", "--no-remote", "--no-experimental"), nil
}

func sessionOptions(checkout string) map[string]any {
	return map[string]any{
		"workingDirectory":      checkout,
		"configDir":             filepath.Join(filepath.Dir(checkout), "copilot-config"),
		"enableConfigDiscovery": false, "enableFileHooks": false,
		"enableSkills": false, "enableOnDemandInstructionDiscovery": false,
		"enableManagedSettings": false, "enableSessionStore": false,
		"enableHostGitOperations": false, "skipEmbeddingRetrieval": true,
		"embeddingCacheStorage": "in-memory", "memory": map[string]bool{"enabled": false},
		"requestExtensions": false, "requestPermission": true,
		"skipCustomInstructions": true, "customAgentsLocalOnly": true,
		"coauthorEnabled": false, "manageScheduleEnabled": false,
		"availableTools": []string{"view", "grep", "glob"},
		"mcpServers":     map[string]any{}, "customAgents": []any{},
		"pluginDirectories": []string{}, "skillDirectories": []string{}, "instructionDirectories": []string{},
		"systemMessage": map[string]string{"mode": "replace", "content": "Review only the captured checkout. Follow the review instructions in the user message. Read repository standards as evidence. Do not change files, run commands, or publish. Return only the requested JSON result."},
	}
}
