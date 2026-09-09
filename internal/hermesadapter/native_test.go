package hermesadapter

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNativeIsolation(t *testing.T) {
	python := os.Getenv("PR_BOARD_HERMES_NATIVE")
	if python == "" {
		t.Skip("set PR_BOARD_HERMES_NATIVE to the Python executable from Hermes 2026.9.7")
	}
	work := t.TempDir()
	cmd, err := command(python, "", work, filepath.Join(work, "checkout"))
	if err != nil {
		t.Fatal(err)
	}
	probe, err := filepath.Abs("testdata/native_probe.py")
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []struct{ mode, finish string }{
		{"chat_completions", "stop"}, {"chat_completions", "truncated"}, {"chat_completions", "cancel"},
		{"anthropic_messages", "stop"}, {"anthropic_messages", "truncated"},
		{"codex_responses", "stop"}, {"codex_responses", "truncated"},
	} {
		t.Run(scenario.mode+"/"+scenario.finish, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			run := exec.CommandContext(ctx, python, "-I", probe, python, cmd.Args[2], scenario.mode, scenario.finish)
			run.WaitDelay = time.Second
			output, err := run.CombinedOutput()
			if err != nil {
				t.Fatalf("native probe: %v\n%s", err, output)
			}
			lines := strings.SplitN(string(output), "\n", 3)
			if len(lines) < 2 {
				t.Fatalf("native probe returned no result: %s", output)
			}
			text, err := finalText([]byte(lines[1]))
			if scenario.finish == "stop" {
				if err != nil || string(text) != "{}" {
					t.Fatalf("native success text=%s err=%v\n%s", text, err, output)
				}
			} else if err == nil {
				t.Fatalf("accepted native %s result: %s", scenario.finish, output)
			}
		})
	}
}
