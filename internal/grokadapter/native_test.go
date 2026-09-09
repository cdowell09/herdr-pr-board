package grokadapter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cdowell09/herdr-pr-board/internal/cli"
)

// Run explicitly with PR_BOARD_GROK_NATIVE pointing to the official 1.0.24 binary.
// All credentials and model traffic belong to this temporary local fixture.
func TestNativeIsolation(t *testing.T) {
	binary := os.Getenv("PR_BOARD_GROK_NATIVE")
	if binary == "" {
		t.Skip("set PR_BOARD_GROK_NATIVE to run the native CLI probe")
	}
	for _, mode := range []string{"api_key", "oidc"} {
		t.Run(mode, func(t *testing.T) { testNativeIsolation(t, binary, mode, "stop") })
	}
	for _, finish := range []string{"length", "missing", "unknown", "missing_no_done", "stop_no_done", "cut_json", "transport_eof"} {
		t.Run(finish, func(t *testing.T) { testNativeIsolation(t, binary, "api_key", finish) })
	}
}

func testNativeIsolation(t *testing.T, binary, mode, providerFinish string) {
	root := t.TempDir()
	user := filepath.Join(root, "user")
	checkout := filepath.Join(root, "checkout")
	for _, dir := range []string{user, checkout, filepath.Join(user, ".grok"), filepath.Join(checkout, ".grok")} {
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := exec.Command("git", "-C", checkout, "init", "--quiet", "--template=").Run(); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{user, checkout} {
		writeTestFile(t, filepath.Join(dir, "AGENTS.md"), "UNSELECTED-INSTRUCTION-SENTINEL")
		writeTestFile(t, filepath.Join(dir, ".grok", "AGENTS.md"), "UNSELECTED-INSTRUCTION-SENTINEL")
		skillDir := filepath.Join(dir, ".grok", "skills", "unselected")
		if err := os.MkdirAll(skillDir, 0700); err != nil {
			t.Fatal(err)
		}
		writeTestFile(t, filepath.Join(skillDir, "SKILL.md"), "---\nname: unselected\ndescription: Unselected fixture skill\n---\nUNSELECTED-SKILL-SENTINEL")
		rulesDir := filepath.Join(dir, ".cursor", "rules")
		if err := os.MkdirAll(rulesDir, 0700); err != nil {
			t.Fatal(err)
		}
		writeTestFile(t, filepath.Join(rulesDir, "unselected.mdc"), "---\nglobs: '**/*'\nalwaysApply: true\n---\nUNSELECTED-RULE-SENTINEL")
		writeTestFile(t, filepath.Join(dir, ".grok", "config.toml"), "[[hooks.SessionStart]]\n[[hooks.SessionStart.hooks]]\ntype = \"command\"\ncommand = \"touch ignored-hook-marker\"\n[mcp_servers.unselected]\ncommand = \"false\"\n")
	}
	readPath := filepath.Join(checkout, "source.txt")
	writeTestFile(t, readPath, "CAPTURED-SOURCE-SENTINEL")
	authPath := filepath.Join(user, ".grok", "auth.json")
	scope := "xai::api_key"
	if mode == "oidc" {
		scope = "https://auth.x.ai::b1a00492-073a-47ea-816f-4c329264a828"
	}
	writeTestFile(t, authPath, fmt.Sprintf(`{%q:{"key":"fixture-native-auth-only","auth_mode":%q,"create_time":%q,"user_id":"fixture-user"}}`, scope, mode, time.Now().UTC().Format(time.RFC3339)))
	t.Setenv("HOME", user)
	t.Setenv("GROK_HOME", filepath.Join(user, ".grok"))
	t.Setenv("GROK_AUTH_PATH", authPath)
	t.Setenv("GROK_AUTH", "")
	t.Setenv("XAI_API_KEY", "")
	if err := os.Unsetenv("XAI_API_KEY"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	session := filepath.Join(checkout, "session")
	cmd, err := command(ctx, binary, session)
	if err != nil {
		t.Fatal(err)
	}
	if providerFinish != "stop" {
		cmd.Args = append(cmd.Args, "--max-turns", "1")
	}
	var mu sync.Mutex
	var capturedRead, nativeAuth bool
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		defer mu.Unlock()
		requests = append(requests, string(body))
		if r.Method == "GET" {
			_, _ = io.WriteString(w, `{"data":[]}`)
			return
		}
		nativeAuth = nativeAuth || r.Header.Get("Authorization") == "Bearer fixture-native-auth-only"
		for _, forbidden := range []string{"UNSELECTED-INSTRUCTION-SENTINEL", "UNSELECTED-SKILL-SENTINEL", "UNSELECTED-RULE-SENTINEL"} {
			if strings.Contains(string(body), forbidden) {
				t.Errorf("%s reached the model", forbidden)
			}
		}
		var delta map[string]any
		var finish any = providerFinish
		if providerFinish == "missing" || providerFinish == "missing_no_done" {
			finish = nil
		}
		if providerFinish == "stop_no_done" || providerFinish == "cut_json" || providerFinish == "transport_eof" {
			finish = "stop"
		}
		if providerFinish != "stop" {
			delta = map[string]any{"role": "assistant", "content": `{"probe":"native-auth-path"}`}
		} else if strings.Contains(string(body), "CAPTURED-SOURCE-SENTINEL") {
			capturedRead = true
			delta = map[string]any{"role": "assistant", "content": `{"probe":"native-auth-path"}`}
		} else {
			finish = "tool_calls"
			arguments, _ := json.Marshal(map[string]string{"target_file": readPath})
			delta = map[string]any{"role": "assistant", "tool_calls": []any{
				map[string]any{"index": 0, "id": "read-source", "type": "function", "function": map[string]any{"name": "read_file", "arguments": string(arguments)}},
			}}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for index, choice := range []any{map[string]any{"index": 0, "delta": delta, "finish_reason": nil}, map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": finish}} {
			chunk, _ := json.Marshal(map[string]any{"id": "probe", "created": 1, "model": "grok-build", "object": "chat.completion.chunk", "choices": []any{choice}})
			if providerFinish == "transport_eof" {
				conn, writer, err := w.(http.Hijacker).Hijack()
				if err != nil {
					t.Error(err)
					return
				}
				_, _ = fmt.Fprintf(writer, "HTTP/1.1 200 OK\r\nContent-Type: text/event-stream\r\nContent-Length: 99999\r\n\r\ndata: %s\n\n", chunk)
				_ = writer.Flush()
				_ = conn.Close()
				return
			}
			if providerFinish == "cut_json" && index == 1 {
				_, _ = fmt.Fprintf(w, "data: %s", chunk[:len(chunk)/2])
				return
			}
			_, _ = fmt.Fprintf(w, "data: %s\n\n", chunk)
		}
		if providerFinish == "missing_no_done" || providerFinish == "stop_no_done" {
			return
		}
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer server.Close()
	config := settings + fmt.Sprintf("\n[model.grok-build]\nmodel = \"grok-build\"\nbase_url = %q\napi_base_url = %q\napi_backend = \"chat_completions\"\nsupported_in_api = true\nmax_retries = 0\n", server.URL+"/v1", server.URL+"/v1")
	writeTestFile(t, filepath.Join(session, "state", "config.toml"), config)
	writeTestFile(t, filepath.Join(session, "prompt.txt"), "Read the file at "+readPath+" and return the requested JSON.")
	cmd.Env = append(cmd.Env, "GROK_XAI_API_BASE_URL="+server.URL+"/v1", "GROK_CLI_CHAT_PROXY_BASE_URL="+server.URL+"/v1", "GROK_MODELS_BASE_URL="+server.URL+"/v1")
	cmd.Dir = session
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	runErr := cli.RunProcess(ctx, cmd, time.Second)
	final, err := finalText(stdout.Bytes())
	if providerFinish != "stop" {
		if providerFinish == "length" || providerFinish == "unknown" || providerFinish == "transport_eof" {
			if runErr == nil && err == nil {
				t.Fatalf("native Grok normalized provider %q into a completed review:\n%s", providerFinish, stdout.String())
			}
		} else if runErr != nil || err != nil || string(final) != `{"probe":"native-auth-path"}` {
			t.Fatalf("native completion behavior changed for %q:\n%s", providerFinish, stdout.String())
		}
		t.Logf("provider=%s process=%v parser=%v final=%s\n%s\n%s", providerFinish, runErr, err, final, stdout.String(), stderr.String())
		return
	}
	if runErr != nil {
		t.Fatalf("native Grok: %v\n%s\n%s", runErr, stdout.String(), stderr.String())
	}
	if err != nil || string(final) != `{"probe":"native-auth-path"}` {
		t.Fatalf("final=%s error=%v\n%s\n%s", final, err, stdout.String(), stderr.String())
	}
	mu.Lock()
	defer mu.Unlock()
	if !capturedRead || !nativeAuth {
		t.Fatalf("capturedRead=%v nativeAuth=%v requests=%v", capturedRead, nativeAuth, requests)
	}
	if _, err := os.Stat(filepath.Join(session, "state", "auth.json")); !os.IsNotExist(err) {
		t.Fatal("native authentication was copied into the isolated home")
	}
	t.Logf("native terminal events:\n%s", stdout.String())
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}
