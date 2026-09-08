package kimiadapter

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestCommandIsolatesInstructionsAndReferencesNativeLogin(t *testing.T) {
	work := t.TempDir()
	cmd, err := command("/selected/kimi", "selected-skill.md", work, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Path != "/selected/kimi" || cmd.Env != nil {
		t.Fatal("changed executable or authentication environment")
	}
	for _, flag := range []string{"--wire", "--afk"} {
		if !slices.Contains(cmd.Args, flag) {
			t.Fatalf("missing %s", flag)
		}
	}
	for flag, name := range map[string]string{"--config-file": "kimi-config.toml", "--agent-file": "kimi-agent.yaml", "--mcp-config-file": "kimi-mcp.json", "--skills-dir": "kimi-skills"} {
		i := slices.Index(cmd.Args, flag)
		if i < 0 || i+1 == len(cmd.Args) || cmd.Args[i+1] != filepath.Join(work, name) {
			t.Fatalf("wrong %s", flag)
		}
	}
	config, err := os.ReadFile(filepath.Join(work, "kimi-config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, setting := range []string{`hooks = []`, `api_key = ""`, `storage = "keyring"`, `key = "oauth/kimi-code"`, `model = "kimi-for-coding"`} {
		if !strings.Contains(string(config), setting) {
			t.Fatalf("missing native authentication/isolation setting %s", setting)
		}
	}
	agent, err := os.ReadFile(filepath.Join(work, "kimi-agent.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(agent), "extend:") || !strings.Contains(string(agent), "subagents: {}") || strings.Contains(string(agent), "Shell") {
		t.Fatalf("agent inherited configuration or execution tools: %s", agent)
	}
	system, err := os.ReadFile(filepath.Join(work, "kimi-system.md"))
	if err != nil || strings.Contains(string(system), "${") {
		t.Fatalf("system prompt interpolates ambient instructions: %s %v", system, err)
	}
	skills, err := os.ReadDir(filepath.Join(work, "kimi-skills"))
	if err != nil || len(skills) != 0 {
		t.Fatalf("skills directory is not empty: %v", err)
	}
	mcp, err := os.ReadFile(filepath.Join(work, "kimi-mcp.json"))
	if err != nil || string(mcp) != `{"mcpServers":{}}` {
		t.Fatalf("MCP config=%s error=%v", mcp, err)
	}
}

func event(kind string, payload any) string {
	data, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": "event", "params": map[string]any{"type": kind, "payload": payload}})
	return string(data) + "\n"
}

const finished = `{"jsonrpc":"2.0","id":"pr-board-review","result":{"status":"finished"}}` + "\n"

func transcript(text string) string {
	return event("TurnBegin", map[string]string{"user_input": "review"}) + event("StepBegin", map[string]int{"n": 1}) +
		event("ContentPart", map[string]string{"type": "text", "text": text}) + event("TurnEnd", map[string]any{}) + finished
}

func TestFinalTextRequiresSuccessfulRootResponseAndFinalStep(t *testing.T) {
	good := transcript(`{"version":1}`)
	got, err := finalText([]byte(good))
	if err != nil || string(got) != `{"version":1}` {
		t.Fatalf("result=%s error=%v", got, err)
	}
	// Earlier assistant text, tool results and failed attempts must not become findings.
	prefix := event("TurnBegin", nil) + event("StepBegin", nil) + event("ContentPart", map[string]string{"type": "text", "text": "earlier"}) +
		event("ToolCall", nil) + event("ToolResult", map[string]string{"type": "text", "text": "untrusted result"}) +
		event("StepBegin", nil) + event("ContentPart", map[string]string{"type": "text", "text": "failed attempt"}) + event("StepRetry", nil)
	finalStep := event("ContentPart", map[string]string{"type": "text", "text": `{"version":`}) + event("ContentPart", map[string]string{"type": "think", "think": "private thought"}) +
		event("ContentPart", map[string]string{"type": "text", "text": `1}`}) + event("TurnEnd", nil) + finished
	got, err = finalText([]byte(prefix + finalStep))
	if err != nil || string(got) != `{"version":1}` {
		t.Fatalf("result=%s error=%v", got, err)
	}
	for _, data := range []string{
		"", "null", "{}", "not json", finished,
		strings.TrimSuffix(good, finished),
		strings.Replace(good, `"finished"`, `"cancelled"`, 1),
		strings.Replace(good, `"finished"`, `"max_steps_reached"`, 1),
		strings.Replace(good, `"pr-board-review"`, `"another-request"`, 1),
		strings.Replace(good, `"2.0"`, `"1.0"`, 1),
		strings.Replace(good, `"TurnEnd"`, `"StepInterrupted"`, 1),
		strings.Replace(good, `"ContentPart"`, `"ToolCall"`, 1),
		strings.Replace(good, `"StepBegin"`, `"OtherEvent"`, 1),
		strings.Replace(good, event("TurnEnd", map[string]any{}), "", 1),
		good + finished,
		good + event("ContentPart", map[string]string{"type": "text", "text": "trailing"}),
		good + "{",
		event("TurnBegin", nil) + good,
		`{"jsonrpc":"2.0","method":"request","id":"approval","params":{}}`,
		`{"jsonrpc":"2.0","id":"pr-board-review","error":{"code":1,"message":"failed"}}`,
	} {
		if _, err := finalText([]byte(data)); err == nil {
			t.Fatalf("accepted %s", data)
		}
	}
}

// Captured from kimi-cli 1.50.0 with its local scripted echo provider.
func TestFinalTextNativeWireTranscript(t *testing.T) {
	data, err := os.ReadFile("testdata/native-wire.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	got, err := finalText(data)
	if err != nil || string(got) != `{"version":1}` {
		t.Fatalf("result=%s error=%v", got, err)
	}
}
