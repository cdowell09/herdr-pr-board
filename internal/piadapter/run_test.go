package piadapter

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

func TestRunWiresPiCommandAndEvents(t *testing.T) {
	dir := t.TempDir()
	script := `#!/bin/sh
case "$(basename "$0")" in
 gh) case "$1 $2" in
  'pr view') cat "$PI_TEST_DIR/pr.json";;
  'repo clone') mkdir -p "$4";;
 esac;;
 git) if [ "$1" = rev-parse ]; then printf '%s' aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa; fi;;
 pi)
  printf '%s\n' "$*" > "$PI_TEST_DIR/args"
  cat > "$PI_TEST_DIR/prompt"
  cat "$PI_TEST_DIR/events";;
esac
`
	for _, name := range []string{"gh", "git", "pi"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	t.Setenv("PI_TEST_DIR", dir)
	in := reviewercontract.Input{Version: 1, Identity: reviewmemory.Identity{Repository: "owner/repo", Number: 42, HeadOID: strings.Repeat("a", 40), BaseRefName: "main"}, BaseOID: strings.Repeat("b", 40), ResultPath: filepath.Join(dir, "result.json")}
	pr, err := json.Marshal(map[string]any{"body": "Review this specified change.", "headRefOid": in.Identity.HeadOID, "baseRefOid": in.BaseOID, "baseRefName": "main"})
	if err != nil {
		t.Fatal(err)
	}
	events, err := os.ReadFile("testdata/completed.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{"pr.json": pr, "events": events, "SKILL.md": []byte("Selected Pi review skill")} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	skill := filepath.Join(dir, "SKILL.md")
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
	args, err := os.ReadFile(filepath.Join(dir, "args"))
	if err != nil {
		t.Fatal(err)
	}
	want := "--print --mode json --no-session --no-extensions --no-skills --no-context-files --no-approve --skill " + skill
	if strings.TrimSpace(string(args)) != want {
		t.Fatalf("Pi arguments=%s", args)
	}
	prompt, err := os.ReadFile(filepath.Join(dir, "prompt"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(prompt), "Selected Pi review skill") {
		t.Fatal("Pi did not receive the selected skill")
	}
}
