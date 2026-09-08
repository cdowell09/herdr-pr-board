package agentadapter

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cdowell09/herdr-pr-board/internal/reviewercontract"
	"github.com/cdowell09/herdr-pr-board/internal/reviewmemory"
)

func TestSharedRunnerValidatesBeforeReplacingResult(t *testing.T) {
	for _, mode := range []string{"completed", "wrong_revision", "invalid_json", "nonzero_exit", "terminal_error"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			in := reviewercontract.Input{Version: 1, Identity: reviewmemory.Identity{Repository: "owner/repo", Number: 42, HeadOID: strings.Repeat("a", 40), BaseRefName: "main"}, BaseOID: strings.Repeat("b", 40), ResultPath: filepath.Join(dir, "result.json")}
			result := reviewercontract.Result{Version: 1, Identity: in.Identity, BaseOID: in.BaseOID, Outcome: reviewmemory.Outcome{Status: reviewmemory.Completed, Message: "Both axes reviewed", Findings: []reviewmemory.Finding{}}}
			if mode == "wrong_revision" {
				result.Identity.HeadOID = strings.Repeat("c", 40)
			}
			output, _ := json.Marshal(result)
			if mode == "invalid_json" {
				output = []byte("invalid JSON")
			}
			exit := "0"
			if mode == "nonzero_exit" {
				exit = "1"
			}
			prior := []byte("previous result")
			if err := os.WriteFile(in.ResultPath, prior, 0600); err != nil {
				t.Fatal(err)
			}
			opts := Options{Name: "fake", Command: func(_, _, _ string) (*exec.Cmd, error) {
				return exec.Command("sh", "-c", `cat >/dev/null; printf '%s' "$1"; printf 'diagnostic' >&2; exit "$2"`, "fake", string(output), exit), nil
			}, FinalText: func(data []byte) ([]byte, error) {
				if mode == "terminal_error" {
					return nil, context.Canceled
				}
				return data, nil
			}}
			err := runAgent(context.Background(), in, opts, dir, dir, "review prompt")
			data, readErr := os.ReadFile(in.ResultPath)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if mode != "completed" {
				if err == nil || string(data) != string(prior) {
					t.Fatalf("unvalidated result replaced prior result: %s, %v", data, err)
				}
				return
			}
			var got reviewercontract.Result
			if err != nil || reviewercontract.Decode(data, &got) != nil || got.Validate(in) != nil {
				t.Fatalf("result=%s error=%v", data, err)
			}
		})
	}
}

func TestSharedRunnerBoundsAgentStreams(t *testing.T) {
	dir := t.TempDir()
	in := reviewercontract.Input{ResultPath: filepath.Join(dir, "result.json")}
	opts := Options{Name: "fake", Command: func(_, _, _ string) (*exec.Cmd, error) {
		return exec.Command("sh", "-c", `head -c 1048577 /dev/zero >&2; head -c 33554433 /dev/zero`), nil
	}, FinalText: func([]byte) ([]byte, error) { t.Fatal("parsed truncated events"); return nil, nil }}
	if err := runAgent(context.Background(), in, opts, dir, dir, ""); err == nil || !strings.Contains(err.Error(), "32 MiB") {
		t.Fatalf("error=%v", err)
	}
	for name, want := range map[string]int64{"fake-events.jsonl": 32 * 1024 * 1024, "fake-stderr.log": 1024 * 1024} {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil || info.Size() != want {
			t.Fatalf("%s info=%v error=%v", name, info, err)
		}
	}
	if _, err := os.Stat(in.ResultPath); !os.IsNotExist(err) {
		t.Fatalf("truncated result written: %v", err)
	}
}
