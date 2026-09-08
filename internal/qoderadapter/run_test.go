package qoderadapter

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

// Verified against @qoder-ai/qodercli 1.1.47 and the official CLI/SDK
// settings-source, skills, permissions and script-mode references.
func TestCommandExcludesAmbientInstructionsAndPreservesLogin(t *testing.T) {
	t.Setenv("QODER_PERSONAL_ACCESS_TOKEN", "test-token")
	t.Setenv("QODER_CONFIG_DIR", "/existing-login")
	for _, name := range []string{"QODER_APPEND_SYSTEM_PROMPT", "QODER_SESSION_ID", "QODER_WORKING_DIR", "QODER_MCP_CONFIG", "QODER_CLI_EXTENSION_REGISTRY_URI", "GEMINI_CLI_EXTENSION_REGISTRY_URI"} {
		t.Setenv(name, "ambient")
		t.Setenv(strings.ToLower(name), "ambient")
	}
	checkout := t.TempDir()
	cmd, err := command("/selected/qodercli", "skill.md", t.TempDir(), checkout)
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Path != "/selected/qodercli" {
		t.Fatal("changed selected executable")
	}
	for _, entry := range []string{"QODER_PERSONAL_ACCESS_TOKEN=test-token", "QODER_CONFIG_DIR=/existing-login"} {
		if !slices.Contains(cmd.Env, entry) {
			t.Fatalf("lost authentication setting %s", entry)
		}
	}
	for _, entry := range cmd.Env {
		if strings.HasSuffix(entry, "=ambient") {
			t.Fatalf("inherited behavior override %s", entry)
		}
	}
	for _, flag := range []string{"--print", "--disable-builtin-skills", "--strict-mcp-config", "--no-session-persistence"} {
		if !slices.Contains(cmd.Args, flag) {
			t.Fatalf("missing %s", flag)
		}
	}
	for flag, want := range map[string]string{"--output-format": "json", "--input-format": "text", "--setting-sources": "", "--settings": isolationSettings, "--mcp-config": `{"mcpServers":{}}`, "--permission-mode": "dont_ask", "--tools": "Read,Grep,Glob", "--allowed-tools": "Read,Grep,Glob", "--cwd": checkout} {
		i := slices.Index(cmd.Args, flag)
		if i < 0 || i+1 >= len(cmd.Args) || cmd.Args[i+1] != want {
			t.Fatalf("wrong %s", flag)
		}
	}
	var settings struct {
		DisableAllHooks bool
		Skills          struct{ Enabled bool }
		SecurityScan    struct{ L1StaticCheck, L2LightweightScan, L3DeepScan bool }
	}
	if err := json.Unmarshal([]byte(isolationSettings), &settings); err != nil {
		t.Fatal(err)
	}
	if !settings.DisableAllHooks || settings.Skills.Enabled || settings.SecurityScan.L1StaticCheck || settings.SecurityScan.L2LightweightScan || settings.SecurityScan.L3DeepScan {
		t.Fatal("ambient customization remains enabled")
	}
}

func TestFinalTextRequiresSuccessfulCompletedResult(t *testing.T) {
	base := map[string]any{"type": "result", "subtype": "success", "is_error": false, "stop_reason": "end_turn", "result": `{"version":1}`}
	for _, tc := range []struct {
		name, field string
		value       any
		valid       bool
	}{
		{name: "completed", valid: true},
		{name: "stop sequence", field: "stop_reason", value: "stop_sequence", valid: true},
		{name: "explicit completion", field: "terminal_reason", value: "completed", valid: true},
		{name: "wrong envelope", field: "type", value: "assistant"},
		{name: "failed", field: "is_error", value: true},
		{name: "missing success flag", field: "is_error", value: nil},
		{name: "turn limit", field: "subtype", value: "error_max_turns"},
		{name: "truncated", field: "stop_reason", value: "max_tokens"},
		{name: "refusal", field: "stop_reason", value: "refusal"},
		{name: "tool call", field: "stop_reason", value: "tool_use"},
		{name: "missing stop", field: "stop_reason", value: nil},
		{name: "interrupted", field: "terminal_reason", value: "cancelled"},
		{name: "empty result", field: "result", value: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			value := make(map[string]any, len(base))
			for k, v := range base {
				value[k] = v
			}
			if tc.field != "" {
				value[tc.field] = tc.value
			}
			data, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			got, err := finalText(data)
			if tc.valid {
				if err != nil || string(got) != `{"version":1}` {
					t.Fatalf("result %s, %v", got, err)
				}
			} else if err == nil {
				t.Fatal("accepted incomplete result")
			}
		})
	}
	for _, data := range []string{"", "null", "[]", "{}", "{}{}"} {
		if _, err := finalText([]byte(data)); err == nil {
			t.Fatalf("accepted %s", data)
		}
	}
}
