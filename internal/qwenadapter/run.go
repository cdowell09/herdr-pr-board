// Package qwenadapter translates Qwen Code's terminal JSON into a local review result.
package qwenadapter

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

type Options struct{ Qwen, Prompt, Skill string }

func Run(ctx context.Context, in reviewercontract.Input, opts Options) error {
	return agentadapter.Run(ctx, in, agentadapter.Options{
		Name: "qwen", Binary: opts.Qwen, Prompt: opts.Prompt, Skill: opts.Skill,
		Command: command, FinalText: finalText,
	})
}

func command(binary, _, _, _ string) (*exec.Cmd, error) {
	// Qwen Code 0.23.0 safe mode suppresses ambient instructions, hooks,
	// skills, extensions and MCP while retaining configured authentication.
	// Default noninteractive permissions deny shell execution and file writes.
	return exec.Command(binary,
		"--safe-mode", "--approval-mode", "default", "--output-format", "json",
		"--json-schema", agentadapter.ResultSchema,
		"--json-file", "", "--input-file", "", "--chat-recording=false",
	), nil
}

func finalText(data []byte) ([]byte, error) {
	var messages []struct {
		Type       string          `json:"type"`
		Subtype    string          `json:"subtype"`
		IsError    *bool           `json:"is_error"`
		Parent     *string         `json:"parent_tool_use_id"`
		Structured json.RawMessage `json:"structured_result"`
	}
	if err := json.Unmarshal(data, &messages); err != nil {
		return nil, fmt.Errorf("decode Qwen Code result: %w", err)
	}
	for i, message := range messages {
		if message.Type != "result" || message.Parent != nil {
			continue
		}
		if i != len(messages)-1 || message.Subtype != "success" || message.IsError == nil || *message.IsError {
			return nil, errors.New("qwen code did not finish with one successful terminal result")
		}
		output := bytes.TrimSpace(message.Structured)
		if len(output) == 0 || output[0] != '{' {
			return nil, errors.New("qwen code returned no structured review result")
		}
		return output, nil
	}
	return nil, errors.New("qwen code returned no terminal result")
}
