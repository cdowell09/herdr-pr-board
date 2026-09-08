package claudeadapter

import (
	"bytes"
	"encoding/json"
	"slices"
	"testing"

	"github.com/cdowell09/herdr-pr-board/internal/agentadapter"
)

// CLI flags: https://code.claude.com/docs/en/cli-reference
// Child tool inheritance: https://code.claude.com/docs/en/sub-agents#available-tools
func TestCommandRestrictsToolsAndPreservesAuthentication(t *testing.T) {
	cmd, err := command("/selected/claude", "/selected/SKILL.md", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Path != "/selected/claude" || cmd.Env != nil {
		t.Fatalf("must use the selected executable and inherited authentication: path %q, custom environment %v", cmd.Path, cmd.Env != nil)
	}
	for _, flag := range []string{"--print", "--safe-mode", "--restricted", "--strict-mcp-config", "--no-session-persistence"} {
		if !slices.Contains(cmd.Args, flag) {
			t.Fatalf("missing isolation flag %s", flag)
		}
	}
	for flag, want := range map[string]string{
		"--output-format": "json", "--tools": "Read,Glob,Grep,Agent", "--allowedTools": "Agent", "--permission-mode": "dontAsk",
		"--permission-prompts": "none", "--mcp-config": `{"mcpServers":{}}`, "--json-schema": agentadapter.ResultSchema,
	} {
		index := slices.Index(cmd.Args, flag)
		if index < 0 || index+1 == len(cmd.Args) || cmd.Args[index+1] != want {
			t.Fatalf("%s must be %q", flag, want)
		}
	}
	if slices.Contains(cmd.Args, "--bare") {
		t.Fatal("bare mode discards the user's subscription authentication")
	}
}

func TestFinalTextRequiresSuccessfulStructuredResult(t *testing.T) {
	output := json.RawMessage(`{"version":1,"outcome":{"status":"completed"}}`)
	base := map[string]any{
		"type": "result", "subtype": "success", "is_error": false,
		"stop_reason": "end_turn", "terminal_reason": "completed", "structured_output": output,
		"session_id": "diagnostic metadata is not part of the review contract",
	}
	for _, tc := range []struct {
		name  string
		field string
		value any
		valid bool
	}{
		{name: "complete", valid: true},
		{name: "structured tool completion", field: "stop_reason", value: "tool_use", valid: true},
		{name: "stop sequence completion", field: "stop_reason", value: "stop_sequence", valid: true},
		{name: "absent optional terminal reason", field: "terminal_reason", value: nil, valid: true},
		{name: "wrong type", field: "type", value: "assistant"},
		{name: "turn limit", field: "subtype", value: "error_max_turns"},
		{name: "budget limit", field: "subtype", value: "error_max_budget_usd"},
		{name: "invalid schema", field: "subtype", value: "error_max_structured_output_retries"},
		{name: "error status", field: "is_error", value: true},
		{name: "missing error status", field: "is_error", value: nil},
		{name: "refusal", field: "stop_reason", value: "refusal"},
		{name: "token limit", field: "stop_reason", value: "max_tokens"},
		{name: "paused turn", field: "stop_reason", value: "pause_turn"},
		{name: "context limit", field: "stop_reason", value: "model_context_window_exceeded"},
		{name: "cancelled", field: "terminal_reason", value: "aborted_tools"},
		{name: "hook stopped", field: "terminal_reason", value: "hook_stopped"},
		{name: "missing structured output", field: "structured_output", value: nil},
		{name: "text is not structured output", field: "structured_output", value: string(output)},
		{name: "array is not result", field: "structured_output", value: []string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			value := make(map[string]any, len(base))
			for key, item := range base {
				value[key] = item
			}
			if tc.field != "" {
				if tc.value == nil {
					delete(value, tc.field)
				} else {
					value[tc.field] = tc.value
				}
			}
			data, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			got, err := finalText(data)
			if tc.valid {
				if err != nil || !bytes.Equal(got, output) {
					t.Fatalf("result %s, error %v", got, err)
				}
			} else if err == nil {
				t.Fatal("accepted a failed or incomplete Claude result")
			}
		})
	}
	data, err := json.Marshal(base)
	if err != nil {
		t.Fatal(err)
	}
	for _, invalid := range [][]byte{nil, []byte("null"), []byte("[]"), []byte("{}"), append(data, []byte("\n{}")...), data[:len(data)-1]} {
		if _, err := finalText(invalid); err == nil {
			t.Fatalf("accepted invalid Claude envelope: %s", invalid)
		}
	}
}
