// Package hermesadapter runs the installed Hermes agent in an isolated review session.
package hermesadapter

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/cdowell09/herdr-pr-board/internal/agentadapter"
	"github.com/cdowell09/herdr-pr-board/internal/reviewercontract"
)

type Options struct{ Hermes, Prompt, Skill string }

//go:embed bridge.py
var bridge string

func Run(ctx context.Context, in reviewercontract.Input, opts Options) error {
	if opts.Hermes == "" {
		opts.Hermes = "python3"
	}
	return agentadapter.Run(ctx, in, agentadapter.Options{
		Name: "hermes", Binary: opts.Hermes, Prompt: opts.Prompt, Skill: opts.Skill,
		Command: command, FinalText: finalText,
	})
}

func command(binary, _, work, checkout string) (*exec.Cmd, error) {
	control := filepath.Join(work, "control")
	if err := os.MkdirAll(control, 0700); err != nil {
		return nil, err
	}
	script := filepath.Join(work, "hermes-review.py")
	if err := os.WriteFile(script, []byte(bridge), 0600); err != nil {
		return nil, err
	}
	// Isolated mode excludes checkout imports and ambient Python startup options.
	return exec.Command(binary, "-I", script, control, checkout), nil
}

func finalText(data []byte) ([]byte, error) {
	var result struct {
		Type           string `json:"type"`
		Completed      bool   `json:"completed"`
		FinalAssistant bool   `json:"finalAssistant"`
		Text           string `json:"text"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("decode Hermes result: %w", err)
	}
	if result.Type != "result" || !result.Completed || !result.FinalAssistant {
		return nil, fmt.Errorf("hermes did not return a successful terminal result")
	}
	if result.Text == "" {
		return nil, fmt.Errorf("hermes returned no final assistant text")
	}
	return []byte(result.Text), nil
}
