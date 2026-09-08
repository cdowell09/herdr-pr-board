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

func TestRunWiresClaudeStructuredOutput(t *testing.T) {
	dir := t.TempDir()
	script := `#!/bin/sh
case "$(basename "$0")" in
 gh) case "$1 $2" in
  'pr view') cat "$CLAUDE_TEST_DIR/pr.json";;
  'repo clone') mkdir -p "$4";;
 esac;;
 git) if [ "$1" = rev-parse ]; then printf '%s' aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa; fi;;
 claude)
  cat > "$CLAUDE_TEST_DIR/prompt"
  cat "$CLAUDE_TEST_DIR/response";;
esac
`
	for _, name := range []string{"gh", "git", "claude"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	t.Setenv("CLAUDE_TEST_DIR", dir)
	in := reviewercontract.Input{Version: 1, Identity: reviewmemory.Identity{Repository: "owner/repo", Number: 42, HeadOID: strings.Repeat("a", 40), BaseRefName: "main"}, BaseOID: strings.Repeat("b", 40), ResultPath: filepath.Join(dir, "result.json")}
	result := reviewercontract.Result{Version: 1, Identity: in.Identity, BaseOID: in.BaseOID, Outcome: reviewmemory.Outcome{Status: reviewmemory.Completed, Message: "Both axes reviewed", Findings: []reviewmemory.Finding{}}}
	for name, value := range map[string]any{
		"pr.json":  map[string]any{"body": "Review this specified change.", "headRefOid": in.Identity.HeadOID, "baseRefOid": in.BaseOID, "baseRefName": "main"},
		"response": map[string]any{"type": "result", "subtype": "success", "is_error": false, "stop_reason": "end_turn", "structured_output": result},
	} {
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	skill := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(skill, []byte("Selected Claude review skill"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := Run(context.Background(), in, Options{Skill: skill}); err != nil {
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
	if err := got.Validate(in); err != nil || got.Outcome.Status != reviewmemory.Completed {
		t.Fatalf("result=%+v error=%v", got, err)
	}
	prompt, err := os.ReadFile(filepath.Join(dir, "prompt"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(prompt), "Selected Claude review skill") {
		t.Fatal("Claude did not receive the selected skill")
	}
}
