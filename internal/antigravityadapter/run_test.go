package antigravityadapter

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		_, _ = os.Stdout.WriteString(os.Getenv("ANTIGRAVITY_TEST_VERSION"))
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestCommandPreservesNativeAuthenticationAndIsolatesCustomizations(t *testing.T) {
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	home, work, checkout := t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("ANTIGRAVITY_TEST_VERSION", "1.1.28\n")
	t.Setenv("GEMINI_API_KEY", "native-auth-sentinel")
	t.Setenv("AGY_CLI_NEW_HARNESS", "1")
	t.Setenv("JETSKI_APP_DATA_DIR", "unselected")
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
	defer cancel()
	cmd, err := command(ctx, binary, work, checkout)
	if err != nil {
		t.Fatal(err)
	}
	for _, flag := range []string{"--sandbox", "--disable-slash-commands", "--gemini_dir=" + filepath.Join(work, "gemini")} {
		if !slices.Contains(cmd.Args, flag) {
			t.Fatalf("missing %s", flag)
		}
	}
	for flag, value := range map[string]string{"--agent": filepath.Base(work), "--add-dir": filepath.Join(work, "control"), "--input-format": "stream-json", "--output-format": "stream-json"} {
		i := slices.Index(cmd.Args, flag)
		if i < 0 || cmd.Args[i+1] != value {
			t.Fatalf("wrong %s", flag)
		}
	}
	for _, arg := range cmd.Args {
		if relative, ok := strings.CutPrefix(arg, "--app_data_dir="); ok {
			if got := filepath.Clean(filepath.Join(work, "gemini", relative)); got != filepath.Join(home, ".gemini", "antigravity-cli") {
				t.Fatalf("native data directory changed to %s", got)
			}
		}
	}
	i := slices.Index(cmd.Args, "--print-timeout")
	timeout, err := time.ParseDuration(cmd.Args[i+1])
	if err != nil || timeout < time.Hour {
		t.Fatalf("native timeout=%s err=%v", timeout, err)
	}
	if !slices.Contains(cmd.Env, "GEMINI_API_KEY=native-auth-sentinel") || !slices.Contains(cmd.Env, "HOME="+home) {
		t.Fatal("changed native authentication environment")
	}
	for _, env := range cmd.Env {
		if strings.HasPrefix(env, "AGY_CLI_NEW_HARNESS=") || strings.HasPrefix(env, "JETSKI_APP_DATA_DIR=") {
			t.Fatalf("inherited unsafe override %s", env)
		}
	}
	definition, err := os.ReadFile(filepath.Join(work, "control", ".agents", "agents", filepath.Base(work)+".md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, directive := range []string{"inheritCustomizations: false", "inheritMcp: false", "subagent: false", "mcpServers: []", "tools: [view_file, grep_search]"} {
		if !strings.Contains(string(definition), directive) {
			t.Fatalf("missing %s", directive)
		}
	}
	prompt := "selected prompt\nselected skill\n" + strings.Repeat("large diff ", 30000)
	cleanup, err := prepareIO(cmd, prompt)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	data, err := io.ReadAll(cmd.Stdin)
	if err != nil {
		t.Fatal(err)
	}
	var message struct {
		Event   string
		Message struct{ Content string }
	}
	if err := json.Unmarshal(data, &message); err != nil || message.Event != "user" || message.Message.Content != prompt {
		t.Fatalf("prompt changed: %v", err)
	}
	cmd, err = command(context.Background(), binary, t.TempDir(), checkout)
	if err != nil {
		t.Fatal(err)
	}
	i = slices.Index(cmd.Args, "--print-timeout")
	if timeout, err := time.ParseDuration(cmd.Args[i+1]); err != nil || timeout < 365*24*time.Hour {
		t.Fatalf("no-deadline timeout=%s err=%v", timeout, err)
	}
}

func TestVersionAndSettingsFailClosed(t *testing.T) {
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, version := range []string{"1.1.27", "1.1.29", "", "unrecognized"} {
		t.Setenv("ANTIGRAVITY_TEST_VERSION", version)
		if _, err := command(context.Background(), binary, t.TempDir(), t.TempDir()); err == nil {
			t.Fatalf("accepted version %q", version)
		}
	}
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := checkSettings(path); err != nil {
		t.Fatal(err)
	}
	for _, data := range []string{`{}`, `{"modelProvider":"gemini"}`, `{"statusLine":{"enabled":false}}`} {
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if err := checkSettings(path); err != nil {
			t.Fatalf("safe settings: %v", err)
		}
	}
	for _, data := range []string{`{"statusLine":{"command":"touch marker"}}`, `{"title":{"command":"touch marker"}}`, `{"statusLine":{"command":"touch marker","enabled":false}}`, `{`} {
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if err := checkSettings(path); err == nil {
			t.Fatalf("accepted unsafe settings %s", data)
		}
	}
}
