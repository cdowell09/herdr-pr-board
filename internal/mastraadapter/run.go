// Package mastraadapter runs the installed Mastra Code SDK with isolated settings.
package mastraadapter

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
	"github.com/cdowell09/herdr-pr-board/internal/cli"
	"github.com/cdowell09/herdr-pr-board/internal/reviewercontract"
)

type Options struct{ MastraCode, Prompt, Skill string }

//go:embed bridge.mjs
var bridge string

func Run(ctx context.Context, in reviewercontract.Input, opts Options) error {
	return agentadapter.Run(ctx, in, agentadapter.Options{
		Name: "mastracode", Binary: opts.MastraCode, Prompt: opts.Prompt, Skill: opts.Skill,
		Command: command, FinalText: finalText,
	})
}

func command(binary, _, work, checkout string) (*exec.Cmd, error) {
	provider := exec.Command(binary)
	if provider.Err != nil {
		return nil, provider.Err
	}
	if err := cli.ResolveNodeShim(provider); err != nil {
		return nil, err
	}
	node, entry := "node", provider.Path
	if len(provider.Args) == 2 { // npm's Windows launcher resolves to Node plus its script.
		node, entry = provider.Path, provider.Args[1]
	}
	entry, err := filepath.Abs(entry)
	if err != nil {
		return nil, err
	}
	script := filepath.Join(work, "mastracode-review.mjs")
	if err := os.WriteFile(script, []byte(bridge), 0600); err != nil {
		return nil, err
	}
	settings := filepath.Join(work, "settings.json")
	if err := os.WriteFile(settings, []byte("{}\n"), 0600); err != nil {
		return nil, err
	}
	cmd := exec.Command(node, script, entry, work, checkout, settings)
	cmd.Env = slices.DeleteFunc(os.Environ(), func(entry string) bool {
		name, _, _ := strings.Cut(entry, "=")
		return strings.EqualFold(name, "NODE_OPTIONS") || strings.EqualFold(name, "NODE_PATH")
	})
	return cmd, nil
}

func finalText(data []byte) ([]byte, error) {
	var result struct {
		Type                 string          `json:"type"`
		Status               string          `json:"status"`
		FinishReason         string          `json:"finishReason"`
		ProviderFinishReason string          `json:"providerFinishReason"`
		ExitCode             *int            `json:"exitCode"`
		Error                json.RawMessage `json:"error"`
		Text                 string          `json:"text"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("decode Mastra Code result: %w", err)
	}
	if result.Type != "result" || result.Status != "completed" || result.FinishReason != "complete" ||
		(result.ProviderFinishReason != "stop" && result.ProviderFinishReason != "end-turn") ||
		result.ExitCode == nil || *result.ExitCode != 0 || (len(result.Error) != 0 && !bytes.Equal(result.Error, []byte("null"))) {
		return nil, fmt.Errorf("mastra code did not return a successful terminal result")
	}
	if result.Text == "" {
		return nil, fmt.Errorf("mastra code returned no final assistant text")
	}
	return []byte(result.Text), nil
}
