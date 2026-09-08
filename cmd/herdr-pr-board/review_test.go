package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cdowell09/herdr-pr-board/internal/reviewercontract"
	"github.com/cdowell09/herdr-pr-board/internal/reviewmemory"
	"github.com/cdowell09/herdr-pr-board/internal/testutil"
)

func TestCommandReviewerProcess(t *testing.T) {
	mode := os.Getenv("COMMAND_REVIEW_MODE")
	if mode == "" {
		return
	}
	var in reviewercontract.Input
	if err := json.NewDecoder(os.Stdin).Decode(&in); err != nil {
		t.Fatal(err)
	}
	if in.Title != "Review $(title) as data" || os.Args[len(os.Args)-1] != "argument with spaces" {
		t.Fatal("lost structured input or arguments")
	}
	result := reviewercontract.Result{Version: 1, Identity: in.Identity, BaseOID: in.BaseOID, Outcome: reviewmemory.Outcome{Status: reviewmemory.Completed, Message: "Reviewed", Findings: []reviewmemory.Finding{}}}
	data, _ := json.Marshal(result)
	if mode == "malformed" {
		data = []byte("{}")
	}
	if err := os.WriteFile(in.ResultPath, data, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestReviewCommandAndLocalHistory(t *testing.T) {
	for _, mode := range []string{"valid", "malformed"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("HERDR_PLUGIN_STATE_DIR", filepath.Join(dir, "state"))
			t.Setenv("COMMAND_REVIEW_MODE", mode)
			metadata := fmt.Sprintf(`{"number":7,"title":"Review $(title) as data","url":"https://github.com/acme/repo/pull/7","state":"OPEN","headRefOid":%q,"baseRefOid":%q,"baseRefName":"main"}`, strings.Repeat("a", 40), strings.Repeat("b", 40))
			testutil.Executable(t, dir, "gh")
			t.Setenv("GH_REVIEW_METADATA", metadata)
			t.Setenv("GORACE", os.Getenv("GORACE")+" atexit_sleep_ms=0")
			t.Setenv("PATH", dir)
			command, _ := json.Marshal([]string{os.Args[0], "-test.run=^TestCommandReviewerProcess$", "--", "argument with spaces"})
			path := writeConfig(t, validConfigTOML+fmt.Sprintf("\n[[reviewers]]\nid = \"fake\"\ncommand = %s\n[[repositories]]\nname = \"acme/repo\"\nreviewer = \"fake\"\n", command))
			var stdout, stderr bytes.Buffer
			code := run([]string{"--config", path, "--review", "https://github.com/acme/repo/pull/7"}, &stdout, &stderr)
			want := 0
			if mode == "malformed" {
				want = 1
			}
			if code != want {
				t.Fatalf("code=%d stdout=%s stderr=%s", code, &stdout, &stderr)
			}
			var result struct {
				Version int
				Run     reviewmemory.Run
			}
			if err := json.Unmarshal(stdout.Bytes(), &result); err != nil || result.Run.ID == "" {
				t.Fatalf("result=%s err=%v", &stdout, err)
			}
			t.Setenv("PATH", "")
			stdout.Reset()
			stderr.Reset()
			if code := run([]string{"--review-history", "https://github.com/acme/repo/pull/7"}, &stdout, &stderr); code != 0 {
				t.Fatalf("history requires gh: %d %s", code, &stderr)
			}
			var history struct{ Runs []reviewmemory.Run }
			if err := json.Unmarshal(stdout.Bytes(), &history); err != nil || len(history.Runs) != 1 {
				t.Fatalf("history=%s error=%v", &stdout, err)
			}
		})
	}
}

func TestReviewOptionModesRejectConflicts(t *testing.T) {
	for _, args := range [][]string{{"--monitor", "--review", "x"}, {"--monitor", "--pi-reviewer"}, {"--review", "x", "--json"}, {"--review-history", "x", "--review", "y"}, {"--reviewer", "a"}, {"--rerun"}, {"--pi-skill", "x"}, {"--pi-reviewer", "--json"}, {"--review", ""}} {
		var stderr bytes.Buffer
		if code := run(args, &bytes.Buffer{}, &stderr); code != 2 {
			t.Fatalf("args=%v code=%d stderr=%s", args, code, &stderr)
		}
	}
}

func TestBuiltinAdapterOptionsAreIsolated(t *testing.T) {
	for _, name := range []string{"pi", "codex", "claude"} {
		t.Run(name, func(t *testing.T) {
			args := []string{"--" + name + "-reviewer", "--" + name + "-executable", "/agent with spaces", "--" + name + "-skill", "/review skill/SKILL.md"}
			o, err := parseOptions(args, &bytes.Buffer{})
			if err != nil || o.adapter.name != name || o.adapter.executable != "/agent with spaces" || o.adapter.skill != "/review skill/SKILL.md" {
				t.Fatalf("options=%+v err=%v", o.adapter, err)
			}
			for _, invalid := range [][]string{
				{"--" + name + "-executable", "agent"},
				{"--" + name + "-skill", "skill"},
				{"--" + name + "-reviewer", "--config", "config.toml"},
				{"--" + name + "-reviewer", "--json"},
				{"--" + name + "-reviewer", "--repository-settings", "acme/repo"},
			} {
				if _, err := parseOptions(invalid, &bytes.Buffer{}); err == nil {
					t.Fatalf("accepted %v", invalid)
				}
			}
			for _, other := range []string{"pi", "codex", "claude"} {
				if other == name {
					continue
				}
				for _, invalid := range [][]string{
					{"--" + name + "-reviewer", "--" + other + "-reviewer"},
					{"--" + name + "-reviewer", "--" + other + "-skill", "skill"},
				} {
					if _, err := parseOptions(invalid, &bytes.Buffer{}); err == nil {
						t.Fatalf("accepted %v", invalid)
					}
				}
			}
			t.Setenv("HERDR_PLUGIN_CONFIG_DIR", filepath.Join(t.TempDir(), "absent"))
			var diagnostics bytes.Buffer
			if code := runAdapter(o.adapter, strings.NewReader("{}"), &diagnostics); code != 1 || strings.Contains(diagnostics.String(), "config") {
				t.Fatalf("adapter must validate stdin without configuration: code=%d diagnostic=%s", code, &diagnostics)
			}
		})
	}
}
