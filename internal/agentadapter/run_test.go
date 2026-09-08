package agentadapter

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cdowell09/herdr-pr-board/internal/reviewercontract"
	"github.com/cdowell09/herdr-pr-board/internal/reviewmemory"
)

func TestRunPreparesCapturedReview(t *testing.T) {
	for _, mode := range []string{"completed", "oversized_diff", "empty_spec", "missing_issue", "changed_pr", "missing_skill"} {
		t.Run(mode, func(t *testing.T) {
			in, opts := prepareRun(t, mode)
			if err := Run(context.Background(), in, opts); err != nil {
				t.Fatal(err)
			}
			result := readResult(t, in)
			trace, err := os.ReadFile(filepath.Join(filepath.Dir(in.ResultPath), "trace"))
			if err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			if mode != "completed" {
				if result.Outcome.Status != reviewmemory.Blocked {
					t.Fatalf("outcome=%+v", result.Outcome)
				}
				if strings.Contains(string(trace), "agent") {
					t.Fatal("blocked preparation launched agent")
				}
				if mode != "oversized_diff" && strings.Contains(string(trace), "repo clone") {
					t.Fatal("blocked context started checkout")
				}
				return
			}
			if result.Outcome.Status != reviewmemory.Completed {
				t.Fatalf("outcome=%+v", result.Outcome)
			}
			for _, arg := range []string{"fetch --no-tags origin " + in.Identity.HeadOID + " " + in.BaseOID, "checkout --detach " + in.Identity.HeadOID, "--no-pager diff --no-ext-diff --no-textconv " + in.BaseOID + "...HEAD", "--no-pager log --no-show-signature --format=fuller " + in.BaseOID + "..HEAD"} {
				if !strings.Contains(string(trace), arg) {
					t.Fatalf("missing %q: %s", arg, trace)
				}
			}
			dir := filepath.Dir(in.ResultPath)
			prompt, err := os.ReadFile(filepath.Join(dir, "prompt"))
			if err != nil {
				t.Fatal(err)
			}
			for _, evidence := range []string{"Implement the widget. $(touch PWNED) `touch PWNED`", "CAPTURED_DIFF", "CAPTURED_LOG", "Review both axes", in.Identity.HeadOID, in.BaseOID} {
				if !strings.Contains(string(prompt), evidence) {
					t.Fatalf("prompt omitted %q", evidence)
				}
			}
			cwd, err := os.ReadFile(filepath.Join(dir, "cwd"))
			if err != nil {
				t.Fatal(err)
			}
			checkout := strings.TrimSpace(string(cwd))
			if checkout == "" {
				t.Fatal("missing agent working directory")
			}
			if _, err := os.Stat(checkout); !os.IsNotExist(err) {
				t.Fatalf("checkout not removed: %v", err)
			}
			if _, err := os.Stat(filepath.Join(dir, "PWNED")); !os.IsNotExist(err) {
				t.Fatal("shell text executed")
			}
		})
	}
}

func TestRunValidatesBeforeReplacingResult(t *testing.T) {
	for _, mode := range []string{"completed", "blocked", "wrong_revision", "invalid_json", "bad_result", "nonzero_exit", "terminal_error", "omitted_identity", "omitted_version", "omitted_base_oid", "omitted_outcome"} {
		t.Run(mode, func(t *testing.T) {
			in, opts := prepareRun(t, mode)
			prior := []byte("previous result")
			writeTestFile(t, in.ResultPath, prior, 0600)
			err := Run(context.Background(), in, opts)
			data, readErr := os.ReadFile(in.ResultPath)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if mode != "completed" && mode != "blocked" {
				if err == nil || string(data) != string(prior) {
					t.Fatalf("unvalidated result replaced prior result: %s, %v", data, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			got := readResult(t, in)
			if string(got.Outcome.Status) != mode {
				t.Fatalf("outcome=%+v", got.Outcome)
			}

		})
	}
}

func TestRunBoundsAgentStreams(t *testing.T) {
	in, opts := prepareRun(t, "oversized_events")
	opts.FinalText = func([]byte) ([]byte, error) { t.Fatal("parsed truncated events"); return nil, nil }
	if err := Run(context.Background(), in, opts); err == nil || !strings.Contains(err.Error(), "32 MiB") {
		t.Fatalf("error=%v", err)
	}
	for name, want := range map[string]int64{"fake-events.jsonl": 32 * 1024 * 1024, "fake-stderr.log": 1024 * 1024} {
		info, err := os.Stat(filepath.Join(filepath.Dir(in.ResultPath), name))
		if err != nil || info.Size() != want {
			t.Fatalf("%s info=%v error=%v", name, info, err)
		}
	}
	if _, err := os.Stat(in.ResultPath); !os.IsNotExist(err) {
		t.Fatalf("truncated result written: %v", err)
	}
}

func prepareRun(t *testing.T, mode string) (reviewercontract.Input, Options) {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	if err := os.Mkdir(bin, 0700); err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
printf '%s\n' "$(basename "$0") $*" >> "$TEST_TRACE"
case "$(basename "$0")" in
 gh)
  case "$1 $2" in
   'pr view') cat "$TEST_PR";;
   'issue view') echo unavailable >&2; exit 1;;
   'repo clone') mkdir -p "$4";;
  esac;;
 git)
  case "$1" in
   rev-parse) printf '%s\n' "$TEST_HEAD";;
   --no-pager)
    if [ "$2" = diff ]; then
     if [ "$TEST_MODE" = oversized_diff ]; then head -c 4194305 /dev/zero; else printf CAPTURED_DIFF; fi
    else printf CAPTURED_LOG; fi;;
   merge-base) printf '%s\n' "$TEST_BASE";;
  esac;;
 agent)
  [ "$PWD" = "$1" ] || exit 2
  cat > "$TEST_PROMPT"
  pwd > "$TEST_CWD"
  if [ -e PWNED ]; then touch "$TEST_PWNED"; fi
  if [ "$TEST_MODE" = oversized_events ]; then
   head -c 1048577 /dev/zero >&2
   head -c 33554433 /dev/zero
  else cat "$TEST_RESPONSE"; fi
  if [ "$TEST_MODE" = nonzero_exit ]; then exit 1; fi;;
esac
`
	for _, name := range []string{"gh", "git", "agent"} {
		writeTestFile(t, filepath.Join(bin, name), []byte(script), 0700)
	}
	in := reviewercontract.Input{Version: 1, Identity: reviewmemory.Identity{Repository: "owner/repo", Number: 42, HeadOID: strings.Repeat("a", 40), BaseRefName: "main"}, BaseOID: strings.Repeat("b", 40), ResultPath: filepath.Join(dir, "result.json")}
	body := "Implement the widget. $(touch PWNED) `touch PWNED`"
	if mode == "empty_spec" {
		body = ""
	}
	pr := map[string]any{"body": body, "title": "spec", "headRefOid": in.Identity.HeadOID, "baseRefOid": in.BaseOID, "baseRefName": "main"}
	if mode == "missing_issue" {
		pr["closingIssuesReferences"] = []any{map[string]string{"url": "https://github.com/owner/repo/issues/1"}}
	}
	if mode == "changed_pr" {
		pr["headRefOid"] = strings.Repeat("c", 40)
	}
	data, err := json.Marshal(pr)
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(dir, "pr.json"), data, 0600)
	result := reviewercontract.Result{Version: 1, Identity: in.Identity, BaseOID: in.BaseOID, Outcome: reviewmemory.Outcome{Status: reviewmemory.Completed, Message: "Both axes reviewed", Findings: []reviewmemory.Finding{}}}
	if mode == "blocked" {
		result.Outcome.Status = reviewmemory.Blocked
	}
	if mode == "wrong_revision" {
		result.Identity.HeadOID = strings.Repeat("c", 40)
	}
	if mode == "bad_result" {
		result.Outcome.Status = "unknown"
	}
	output, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if mode == "invalid_json" {
		output = []byte("invalid JSON")
	}
	if strings.HasPrefix(mode, "omitted_") {
		var fields map[string]any
		if err := json.Unmarshal(output, &fields); err != nil {
			t.Fatal(err)
		}
		delete(fields, strings.TrimPrefix(mode, "omitted_"))
		output, err = json.Marshal(fields)
		if err != nil {
			t.Fatal(err)
		}
	}
	writeTestFile(t, filepath.Join(dir, "response"), output, 0600)
	skill := filepath.Join(dir, "SKILL.md")
	writeTestFile(t, skill, []byte("Review both axes"), 0600)
	if mode == "missing_skill" {
		skill += ".missing"
	}
	for key, value := range map[string]string{"PATH": bin + ":" + os.Getenv("PATH"), "TEST_TRACE": filepath.Join(dir, "trace"), "TEST_PR": filepath.Join(dir, "pr.json"), "TEST_HEAD": in.Identity.HeadOID, "TEST_BASE": in.BaseOID, "TEST_MODE": mode, "TEST_PROMPT": filepath.Join(dir, "prompt"), "TEST_CWD": filepath.Join(dir, "cwd"), "TEST_PWNED": filepath.Join(dir, "PWNED"), "TEST_RESPONSE": filepath.Join(dir, "response")} {
		t.Setenv(key, value)
	}
	opts := Options{Name: "fake", Binary: filepath.Join(bin, "agent"), Skill: skill,
		Command: func(binary, selectedSkill, work, checkout string) (*exec.Cmd, error) {
			if selectedSkill != skill || filepath.Dir(checkout) != work {
				t.Fatalf("incorrect command context: %s %s %s", selectedSkill, work, checkout)
			}
			return exec.Command(binary, checkout), nil
		}, FinalText: func(data []byte) ([]byte, error) {
			if mode == "terminal_error" {
				return nil, errors.New("fake terminal error")
			}
			return data, nil
		},
	}
	return in, opts
}

func writeTestFile(t *testing.T, path string, data []byte, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, data, mode); err != nil {
		t.Fatal(err)
	}
}

func readResult(t *testing.T, in reviewercontract.Input) reviewercontract.Result {
	t.Helper()
	data, err := os.ReadFile(in.ResultPath)
	if err != nil {
		t.Fatal(err)
	}
	var got reviewercontract.Result
	if err := reviewercontract.Decode(data, &got); err != nil {
		t.Fatal(err)
	}
	if err := got.Validate(in); err != nil {
		t.Fatal(err)
	}
	return got
}
