// Package claudeadapter translates Claude Code's final JSON into a local review result.
package claudeadapter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"

	"github.com/cdowell09/herdr-pr-board/internal/agentadapter"
	"github.com/cdowell09/herdr-pr-board/internal/reviewercontract"
)

type Options struct{ Claude, Prompt, Skill string }

func Run(ctx context.Context, in reviewercontract.Input, opts Options) error {
	return agentadapter.Run(ctx, in, agentadapter.Options{
		Name: "claude", Binary: opts.Claude, Prompt: opts.Prompt, Skill: opts.Skill,
		Command: command, FinalText: finalText,
	})
}

func command(binary, _, _, _ string) (*exec.Cmd, error) {
	// Safe mode preserves the user's authentication while disabling automatically
	// loaded instructions and customizations. Restricted mode confines file reads
	// to the checkout; the shared runner supplies diff and log evidence without Bash.
	// Subagents inherit this tool pool, so the selected skill can split its review.
	return exec.Command(binary,
		"--print", "--output-format", "json", "--json-schema", agentadapter.ResultSchema,
		"--safe-mode", "--restricted", "--permission-mode", "dontAsk", "--permission-prompts", "none",
		"--tools", "Read,Glob,Grep,Agent", "--allowedTools", "Agent", "--strict-mcp-config", "--mcp-config", `{"mcpServers":{}}`,
		"--no-session-persistence",
	), nil
}

func finalText(data []byte) ([]byte, error) {
	// Claude includes version-dependent usage and session metadata. Only its
	// terminal status and structured output belong to the adapter boundary.
	var result struct {
		Type             string          `json:"type"`
		Subtype          string          `json:"subtype"`
		IsError          *bool           `json:"is_error"`
		StopReason       string          `json:"stop_reason"`
		TerminalReason   string          `json:"terminal_reason"`
		StructuredOutput json.RawMessage `json:"structured_output"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("decode Claude Code result: %w", err)
	}
	if result.Type != "result" || result.Subtype != "success" || result.IsError == nil || *result.IsError {
		return nil, errors.New("claude code did not finish with a successful result")
	}
	if result.TerminalReason != "" && result.TerminalReason != "completed" {
		return nil, errors.New("claude code stopped before completing its result")
	}
	// Structured output can finish through its output tool. A refusal or
	// truncated assistant message cannot establish a completed review.
	if result.StopReason != "" && result.StopReason != "end_turn" && result.StopReason != "tool_use" && result.StopReason != "stop_sequence" {
		return nil, errors.New("claude code did not finish its final response")
	}
	output := bytes.TrimSpace(result.StructuredOutput)
	if len(output) == 0 || output[0] != '{' {
		return nil, errors.New("claude code returned no structured review result")
	}
	return output, nil
}
