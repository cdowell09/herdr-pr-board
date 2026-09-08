// Package qoderadapter translates Qoder CLI's final JSON into a review result.
package qoderadapter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/cdowell09/herdr-pr-board/internal/agentadapter"
	"github.com/cdowell09/herdr-pr-board/internal/reviewercontract"
)

type Options struct{ Qoder, Prompt, Skill string }

const isolationSettings = `{"disableAllHooks":true,"skills":{"enabled":false,"loadFromAgentsDirectory":false},"experimental":{"enableAgents":false,"extensionReloading":false},"autoMemoryEnabled":false,"security":{"folderTrust":{"enabled":false}},"securityScan":{"l1StaticCheck":false,"l2LightweightScan":false,"l3DeepScan":false},"general":{"enableAutoUpdate":false,"enableNotifications":false,"sessionRetention":{"enabled":false}}}`

func Run(ctx context.Context, in reviewercontract.Input, opts Options) error {
	return agentadapter.Run(ctx, in, agentadapter.Options{
		Name: "qodercli", Binary: opts.Qoder, Prompt: opts.Prompt, Skill: opts.Skill,
		Command: command, FinalText: finalText,
	})
}

func command(binary, _, _, checkout string) (*exec.Cmd, error) {
	// Qoder 1.1.47 scopes filesystem instructions and skills with settings
	// sources. Keep the authentication directory while excluding those sources.
	cmd := exec.Command(binary,
		"--print", "--output-format", "json", "--input-format", "text",
		"--setting-sources", "", "--settings", isolationSettings,
		"--disable-builtin-skills", "--strict-mcp-config", "--mcp-config", `{"mcpServers":{}}`,
		"--no-session-persistence", "--permission-mode", "dont_ask",
		"--tools", "Read,Grep,Glob", "--allowed-tools", "Read,Grep,Glob", "--cwd", checkout,
	)
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		// Windows environment names are case-insensitive.
		switch strings.ToUpper(key) {
		case "QODER_APPEND_SYSTEM_PROMPT", "QODER_SESSION_ID", "QODER_WORKING_DIR", "QODER_MCP_CONFIG",
			"QODER_CLI_EXTENSION_REGISTRY_URI", "GEMINI_CLI_EXTENSION_REGISTRY_URI":
			continue
		}
		cmd.Env = append(cmd.Env, entry)
	}
	return cmd, nil
}

func finalText(data []byte) ([]byte, error) {
	var result struct {
		Type           string `json:"type"`
		Subtype        string `json:"subtype"`
		IsError        *bool  `json:"is_error"`
		StopReason     string `json:"stop_reason"`
		TerminalReason string `json:"terminal_reason"`
		Result         string `json:"result"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("decode Qoder result: %w", err)
	}
	if result.Type != "result" || result.Subtype != "success" || result.IsError == nil || *result.IsError {
		return nil, errors.New("qoder did not finish with a successful result")
	}
	if result.StopReason != "end_turn" && result.StopReason != "stop_sequence" {
		return nil, errors.New("qoder did not finish its assistant response")
	}
	if result.TerminalReason != "" && result.TerminalReason != "completed" {
		return nil, errors.New("qoder stopped before completing its result")
	}
	if strings.TrimSpace(result.Result) == "" {
		return nil, errors.New("qoder returned no final result")
	}
	return []byte(result.Result), nil
}
