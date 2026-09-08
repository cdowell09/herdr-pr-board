package claudeadapter

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cdowell09/herdr-pr-board/internal/reviewercontract"
	"github.com/cdowell09/herdr-pr-board/internal/reviewmemory"
)

func TestRunValidatesClaudeResultBeforeWriting(t *testing.T) {
	for _, mode := range []string{"completed", "blocked", "wrong revision", "invalid contract", "error envelope", "nonzero exit"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			bin := filepath.Join(dir, "bin")
			if err := os.Mkdir(bin, 0700); err != nil {
				t.Fatal(err)
			}
			write := func(path string, data []byte, perm os.FileMode) {
				t.Helper()
				if err := os.WriteFile(path, data, perm); err != nil {
					t.Fatal(err)
				}
			}
			script := `#!/bin/sh
case "$(basename "$0")" in
 gh)
  case "$1 $2" in
   'pr view') cat "$CLAUDE_TEST_PR";;
   'repo clone') mkdir -p "$4";;
  esac;;
 git)
  case "$1 $2" in
   'rev-parse HEAD') printf '%s\n' "$CLAUDE_TEST_HEAD";;
   '--no-pager diff') printf '%s\n' 'captured-diff-evidence';;
   '--no-pager log') printf '%s\n' 'captured-log-evidence';;
  esac;;
 claude)
  cat > "$CLAUDE_TEST_PROMPT"
  pwd > "$CLAUDE_TEST_CWD"
  cat "$CLAUDE_TEST_RESPONSE"
  exit "$CLAUDE_TEST_EXIT";;
esac
`
			for _, name := range []string{"gh", "git", "claude"} {
				write(filepath.Join(bin, name), []byte(script), 0700)
			}
			t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
			in := reviewercontract.Input{
				Version: 1, Identity: reviewmemory.Identity{Repository: "owner/repo", Number: 42, HeadOID: strings.Repeat("a", 40), BaseRefName: "main"},
				BaseOID: strings.Repeat("b", 40), ResultPath: filepath.Join(dir, "result.json"),
			}
			pr, err := json.Marshal(map[string]any{
				"body": "Review the implementation against this specification.", "title": "Test PR",
				"headRefOid": in.Identity.HeadOID, "baseRefOid": in.BaseOID, "baseRefName": in.Identity.BaseRefName,
			})
			if err != nil {
				t.Fatal(err)
			}
			result := reviewercontract.Result{Version: 1, Identity: in.Identity, BaseOID: in.BaseOID,
				Outcome: reviewmemory.Outcome{Status: reviewmemory.Completed, Message: "Both axes reviewed.", Findings: []reviewmemory.Finding{}}}
			if mode == "blocked" {
				result.Outcome.Status, result.Outcome.Message = reviewmemory.Blocked, "Required specification details are unavailable."
			}
			if mode == "wrong revision" {
				result.Identity.HeadOID = strings.Repeat("c", 40)
			}
			if mode == "invalid contract" {
				result.Version = 0
			}
			envelope := map[string]any{"type": "result", "subtype": "success", "is_error": false, "stop_reason": "end_turn", "structured_output": result}
			if mode == "error envelope" {
				envelope["subtype"] = "error_during_execution"
			}
			response, err := json.Marshal(envelope)
			if err != nil {
				t.Fatal(err)
			}
			prPath, responsePath := filepath.Join(dir, "pr.json"), filepath.Join(dir, "response.json")
			write(prPath, pr, 0600)
			write(responsePath, response, 0600)
			promptPath, cwdPath := filepath.Join(dir, "prompt"), filepath.Join(dir, "cwd")
			t.Setenv("CLAUDE_TEST_PR", prPath)
			t.Setenv("CLAUDE_TEST_HEAD", in.Identity.HeadOID)
			t.Setenv("CLAUDE_TEST_RESPONSE", responsePath)
			t.Setenv("CLAUDE_TEST_PROMPT", promptPath)
			t.Setenv("CLAUDE_TEST_CWD", cwdPath)
			exit := "0"
			if mode == "nonzero exit" {
				exit = "1"
			}
			t.Setenv("CLAUDE_TEST_EXIT", exit)
			skillPath := filepath.Join(dir, "SKILL.md")
			write(skillPath, []byte("Explicit selected review skill: check both standards and specification."), 0600)
			err = Run(context.Background(), in, Options{Skill: skillPath})
			if mode != "completed" && mode != "blocked" {
				if err == nil {
					t.Fatal("accepted an invalid Claude review")
				}
				if _, err := os.Stat(in.ResultPath); !os.IsNotExist(err) {
					t.Fatalf("invalid result must not be written: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(in.ResultPath)
			if err != nil {
				t.Fatal(err)
			}
			var got reviewercontract.Result
			if err := reviewercontract.Decode(data, &got); err != nil {
				t.Fatal(err)
			}
			if err := got.Validate(in); err != nil || got.Outcome.Status != result.Outcome.Status {
				t.Fatalf("unexpected validated result %+v: %v", got, err)
			}
			prompt, err := os.ReadFile(promptPath)
			if err != nil {
				t.Fatal(err)
			}
			for _, evidence := range []string{"Explicit selected review skill", "captured-diff-evidence", "captured-log-evidence", in.Identity.HeadOID, in.BaseOID} {
				if !strings.Contains(string(prompt), evidence) {
					t.Fatalf("Claude prompt omitted %s", evidence)
				}
			}
			cwd, err := os.ReadFile(cwdPath)
			if err != nil {
				t.Fatal(err)
			}
			checkout := strings.TrimSpace(string(cwd))
			if filepath.Base(checkout) != "checkout" || !strings.HasPrefix(filepath.Base(filepath.Dir(checkout)), "claude-") {
				t.Fatalf("Claude did not run in its isolated checkout: %s", checkout)
			}
			if _, err := os.Stat(checkout); !os.IsNotExist(err) {
				t.Fatalf("disposable checkout was not removed: %v", err)
			}
		})
	}
}
