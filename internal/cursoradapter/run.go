// Package cursoradapter runs the installed Cursor SDK in an isolated review session.
package cursoradapter

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/cdowell09/herdr-pr-board/internal/agentadapter"
	"github.com/cdowell09/herdr-pr-board/internal/reviewercontract"
)

type Options struct{ Cursor, Prompt, Skill string }

//go:embed bridge.mjs
var bridge string

func Run(ctx context.Context, in reviewercontract.Input, opts Options) error {
	return agentadapter.Run(ctx, in, agentadapter.Options{
		Name: "cursor", Binary: opts.Cursor, Prompt: opts.Prompt, Skill: opts.Skill,
		Command: command, FinalText: finalText,
	})
}

func command(entry, _, work, checkout string) (*exec.Cmd, error) {
	if entry != "cursor" {
		var err error
		entry, err = filepath.Abs(entry)
		if err != nil {
			return nil, err
		}
	}
	script := filepath.Join(work, "cursor-review.mjs")
	if err := os.WriteFile(script, []byte(bridge), 0600); err != nil {
		return nil, err
	}
	cmd := exec.Command("node", script, entry, work, checkout)
	cmd.Env = slices.DeleteFunc(os.Environ(), func(entry string) bool {
		name, _, _ := strings.Cut(entry, "=")
		return strings.EqualFold(name, "NODE_OPTIONS") || strings.EqualFold(name, "NODE_PATH")
	})
	return cmd, nil
}

func finalText(data []byte) ([]byte, error) {
	var result struct {
		Type      string          `json:"type"`
		Status    string          `json:"status"`
		TurnEnded *int            `json:"turnEnded"`
		Pending   *int            `json:"pending"`
		Cancelled bool            `json:"cancelled"`
		Error     json.RawMessage `json:"error"`
		Text      string          `json:"text"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("decode Cursor SDK result: %w", err)
	}
	if result.Type != "result" || result.Status != "finished" || result.TurnEnded == nil || *result.TurnEnded != 1 ||
		result.Pending == nil || *result.Pending != 0 || result.Cancelled ||
		(len(result.Error) != 0 && !bytes.Equal(result.Error, []byte("null"))) {
		return nil, fmt.Errorf("cursor SDK did not return a successful terminal result")
	}
	if result.Text == "" {
		return nil, fmt.Errorf("cursor SDK returned no final assistant text")
	}
	return []byte(result.Text), nil
}
