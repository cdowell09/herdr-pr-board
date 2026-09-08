package ompadapter

import (
	"context"
	"os"
	"slices"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		_, _ = os.Stdout.WriteString(os.Getenv("OMP_TEST_VERSION"))
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestVersionCheckRequiresReviewedRelease(t *testing.T) {
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, version := range []string{supportedVersion + "\n", "18.1.15\n", "18.1.13\n", "", "unrecognized\n"} {
		t.Setenv("OMP_TEST_VERSION", version)
		err := checkVersion(context.Background(), binary, t.TempDir())
		if (err == nil) != (version == supportedVersion+"\n") {
			t.Fatalf("version %q: %v", version, err)
		}
	}
}

func TestCommandIsolatesDiscoveryWithoutReplacingAuthentication(t *testing.T) {
	cmd, err := command("/selected/omp", "skill.md", t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Path != "/selected/omp" || cmd.Env != nil {
		t.Fatal("changed executable or authentication environment")
	}
	for _, flag := range []string{"--print", "--no-session", "--no-extensions", "--no-skills", "--no-rules", "--no-lsp", "--no-title", "--no-prewalk"} {
		if !slices.Contains(cmd.Args, flag) {
			t.Fatalf("missing %s", flag)
		}
	}
	for flag, want := range map[string]string{"--mode": "json", "--tools": "read,grep,glob", "--system-prompt": "", "--append-system-prompt": ""} {
		i := slices.Index(cmd.Args, flag)
		if i < 0 || i+1 >= len(cmd.Args) || cmd.Args[i+1] != want {
			t.Fatalf("wrong %s", flag)
		}
	}
	i := slices.Index(cmd.Args, "--config")
	if i < 0 || i+1 >= len(cmd.Args) {
		t.Fatal("missing settings overlay")
	}
	data, err := os.ReadFile(cmd.Args[i+1])
	if err != nil || string(data) != isolationSettings {
		t.Fatalf("settings %s, %v", data, err)
	}
	if !strings.Contains(string(data), "\npersonality: none\n") {
		t.Fatal("ambient PERSONALITY.md must remain disabled")
	}
	if !strings.Contains(string(data), "\nspeechgen:\n  enabled: false\n") {
		t.Fatal("configured speech generation must remain disabled")
	}
	if !strings.Contains(string(data), "\nvault:\n  enabled: false\n") {
		t.Fatal("configured vault integration must remain disabled")
	}
}

func TestVersionCheckHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := checkVersion(ctx, "must-not-start", t.TempDir()); err == nil {
		t.Fatal("accepted canceled version check")
	}
}

func TestFinalTextRequiresExplicitTerminalCompletion(t *testing.T) {
	const good = `{"type":"agent_end","isTerminal":true,"messages":[{"role":"assistant","stopReason":"stop","content":[{"type":"text","text":"{}"}]}]}`
	for _, data := range []string{good, `{"type":"agent_end","isTerminal":false}` + "\n" + good} {
		got, err := finalText([]byte(data))
		if err != nil || string(got) != "{}" {
			t.Fatalf("result %s, %v", got, err)
		}
	}
	for _, data := range []string{
		``, `null`, `{}`, `[]`, `{"type":"agent_end"}`, `{"type":"agent_end","isTerminal":false}`,
		`{"type":"agent_end","isTerminal":true,"messages":[]}`,
		`{"type":"agent_end","isTerminal":true,"messages":[{"role":"assistant","stopReason":"error"}]}`,
		`{"type":"agent_end","isTerminal":true,"messages":[{"role":"assistant","stopReason":"stop","content":[]}]}`,
		`{"type":"agent_end","isTerminal":true,"messages":[{"role":"user","stopReason":"stop","content":[{"type":"text","text":"{}"}]}]}`,
		good + "\n" + good, good + `{"type":"message_start"}`, good[:len(good)-1],
	} {
		if _, err := finalText([]byte(data)); err == nil {
			t.Fatalf("accepted %s", data)
		}
	}
}
