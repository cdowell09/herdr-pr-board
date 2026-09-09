package antigravityadapter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cdowell09/herdr-pr-board/internal/cli"
)

// Opt in with a separately installed CLI. All model requests use a local mock.
func TestNativeIsolation(t *testing.T) {
	const expected = `{"version":1,"identity":{"repository":"owner/repo","number":1,"head_oid":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","base_ref_name":"main"},"base_oid":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","outcome":{"status":"completed","message":"No findings.","findings":[]}}`
	binary := os.Getenv("ANTIGRAVITY_NATIVE_TEST_BINARY")
	if binary == "" {
		t.Skip("set ANTIGRAVITY_NATIVE_TEST_BINARY to test CLI 1.1.28")
	}
	root, home, checkout, work := t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()
	work, err := os.MkdirTemp(work, "antigravity-")
	if err != nil {
		t.Fatal(err)
	}
	write := func(path, data string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(home, ".gemini", "antigravity-cli", "settings.json"), `{"modelProvider":"gemini"}`)
	for i, base := range []string{filepath.Join(home, ".gemini", "config"), filepath.Join(home, ".gemini", "antigravity-cli"), filepath.Join(checkout, ".agents")} {
		write(filepath.Join(base, "rules", "poison.md"), "---\ntrigger: always_on\n---\nUNSELECTED_RULE_SENTINEL\n")
		write(filepath.Join(base, "skills", "poison", "SKILL.md"), "---\nname: poison\ndescription: UNSELECTED_SKILL_SENTINEL\n---\nUNSELECTED_SKILL_SENTINEL\n")
		command := "touch " + filepath.Join(root, fmt.Sprintf("executed-%d", i))
		mcp, _ := json.Marshal(map[string]any{"mcpServers": map[string]any{"poison": map[string]any{"command": "/bin/sh", "args": []string{"-c", command}}}})
		write(filepath.Join(base, "mcp_config.json"), string(mcp))
		hooks, _ := json.Marshal(map[string]any{"poison": map[string]any{"PreInvocation": []map[string]string{{"type": "command", "command": command}}}})
		write(filepath.Join(base, "hooks.json"), string(hooks))
	}
	write(filepath.Join(checkout, "AGENTS.md"), "UNSELECTED_REPOSITORY_SENTINEL")
	write(filepath.Join(checkout, "example.txt"), "CAPTURED_SOURCE_SENTINEL")
	var mu sync.Mutex
	var requests []string
	modelCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		requests = append(requests, string(body))
		if strings.Contains(r.URL.Path, "gemini-3.8") {
			modelCalls++
		}
		readSource := strings.Contains(r.URL.Path, "gemini-3.8") && modelCalls == 1
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		if readSource {
			args := map[string]string{"AbsolutePath": filepath.Join(checkout, "example.txt"), "toolAction": "Reading file", "toolSummary": "Captured source"}
			response := map[string]any{"candidates": []any{map[string]any{"content": map[string]any{"role": "model", "parts": []any{map[string]any{"functionCall": map[string]any{"name": "view_file", "args": args}}}}, "finishReason": "STOP"}}}
			data, _ := json.Marshal(response)
			_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
			return
		}
		text, _ := json.Marshal(expected)
		_, _ = fmt.Fprintf(w, "data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":%s}]},\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":1,\"candidatesTokenCount\":1,\"totalTokenCount\":2}}\n\n", text)
	}))
	defer server.Close()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	t.Setenv("GEMINI_API_KEY", "synthetic-native-test-key")
	t.Setenv("GOOGLE_GEMINI_BASE_URL", server.URL)
	t.Setenv("GEMINI_BASE_URL", server.URL)
	t.Setenv("AGY_ADC_AUTH", "")
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	cmd, err := command(ctx, binary, work, checkout)
	if err != nil {
		t.Fatal(err)
	}
	cmd.Args = append(cmd.Args, "--model", "Gemini 3.8 Flash (Low)")
	cmd.Dir = checkout
	cleanup, err := prepareIO(cmd, "SELECTED_PROMPT_SENTINEL\nSELECTED_SKILL_SENTINEL")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cli.RunProcess(ctx, cmd, time.Second); err != nil {
		t.Fatalf("native run: %v: %s", err, &stderr)
	}
	if text, err := finalText(stdout.Bytes(), filepath.Base(work)); err != nil || strings.TrimSpace(string(text)) != expected {
		t.Fatalf("native result: %s %v\n%s\n%s", text, err, &stdout, &stderr)
	}
	if markers, err := filepath.Glob(filepath.Join(root, "executed-*")); err != nil || len(markers) > 0 {
		t.Fatalf("native process started an unselected hook or MCP server: %v, %v", markers, err)
	}
	mu.Lock()
	captured := strings.Join(requests, "\n")
	mu.Unlock()
	if strings.Contains(captured, "UNSELECTED_") {
		t.Fatal("native request inherited unselected instructions")
	}
	if !strings.Contains(captured, "SELECTED_PROMPT_SENTINEL") || !strings.Contains(captured, "SELECTED_SKILL_SENTINEL") {
		t.Fatal("native request omitted selected instructions")
	}
	if !strings.Contains(captured, "CAPTURED_SOURCE_SENTINEL") {
		t.Fatal("native reviewer did not read the captured source")
	}
	for _, name := range []string{"run_command", "call_mcp_tool", "search_web", "invoke_subagent", "write_to_file"} {
		if strings.Contains(captured, `"name":"`+name+`"`) {
			t.Fatalf("native request exposed %s", name)
		}
	}
}
