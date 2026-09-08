package qwenadapter

import (
	"slices"
	"testing"

	"github.com/cdowell09/herdr-pr-board/internal/agentadapter"
)

// Native contract: QwenLM/qwen-code v0.23.0, packages/cli/src/config/config.ts
// and packages/cli/src/nonInteractive/io/{BaseJsonOutputAdapter,JsonOutputAdapter}.ts.
func TestCommandPreservesAuthenticationAndSuppressesCustomizations(t *testing.T) {
	cmd, err := command("/selected/qwen", "skill.md", t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Path != "/selected/qwen" || cmd.Env != nil {
		t.Fatal("changed executable or authentication environment")
	}
	for _, flag := range []string{"--safe-mode", "--chat-recording=false"} {
		if !slices.Contains(cmd.Args, flag) {
			t.Fatalf("missing %s", flag)
		}
	}
	for flag, want := range map[string]string{"--approval-mode": "default", "--output-format": "json", "--json-schema": agentadapter.ResultSchema, "--json-file": "", "--input-file": ""} {
		i := slices.Index(cmd.Args, flag)
		if i < 0 || i+1 >= len(cmd.Args) || cmd.Args[i+1] != want {
			t.Fatalf("wrong %s", flag)
		}
	}
	if slices.Contains(cmd.Args, "--bare") {
		t.Fatal("bare mode loses configured authentication")
	}
}

func TestFinalTextRequiresOneSuccessfulTerminalRootResult(t *testing.T) {
	const good = `{"type":"result","subtype":"success","is_error":false,"structured_result":{"version":1}}`
	for _, data := range []string{"[" + good + "]", `[{"type":"assistant"},` + good + `]`, `[{"type":"result","parent_tool_use_id":"child","is_error":true},` + good + `]`} {
		got, err := finalText([]byte(data))
		if err != nil || string(got) != `{"version":1}` {
			t.Fatalf("result %s: %s, %v", data, got, err)
		}
	}
	for _, data := range []string{
		``, `null`, `[]`, `{}`, good, "[" + good + "," + good + "]", "[" + good + `,{"type":"assistant"}]`,
		`[{"type":"result","subtype":"success","structured_result":{}}]`,
		`[{"type":"result","subtype":"success","is_error":true,"structured_result":{}}]`,
		`[{"type":"result","subtype":"error_max_turns","is_error":false,"structured_result":{}}]`,
		`[{"type":"result","subtype":"success","is_error":false,"result":"{}"}]`,
		`[{"type":"result","subtype":"success","is_error":false,"structured_result":null}]`,
		`[{"type":"result","subtype":"success","is_error":false,"structured_result":"{}"}]`,
		`[{"type":"result","subtype":"success","is_error":false,"structured_result":[]}]`,
		`[{"type":"result","parent_tool_use_id":"child","subtype":"success","is_error":false,"structured_result":{}}]`,
		"[" + good + "]{}",
	} {
		if _, err := finalText([]byte(data)); err == nil {
			t.Fatalf("accepted %s", data)
		}
	}
}
