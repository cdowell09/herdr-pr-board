package claudeadapter

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cdowell09/herdr-pr-board/internal/reviewercontract"
	"github.com/cdowell09/herdr-pr-board/internal/reviewmemory"
	"github.com/cdowell09/herdr-pr-board/internal/testutil"
)

func TestMain(m *testing.M) {
	tool := strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe")
	if tool == "gh" || tool == "git" || tool == "claude" {
		if err := runFixtureTool(tool, os.Getenv("CLAUDE_TEST_DIR"), os.Args[1:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func runFixtureTool(tool, dir string, args []string) error {
	output := ""
	switch tool {
	case "gh":
		switch args[0] + " " + args[1] {
		case "pr view":
			output = "pr.json"
		case "repo clone":
			return os.MkdirAll(args[3], 0700)
		default:
			return fmt.Errorf("unexpected gh command: %v", args)
		}
	case "git":
		if args[0] == "rev-parse" {
			_, err := fmt.Fprint(os.Stdout, strings.Repeat("a", 40))
			return err
		}
		return nil
	case "claude":
		prompt, err := io.ReadAll(os.Stdin)
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, "prompt"), prompt, 0600); err != nil {
			return err
		}
		output = "response"
	}
	data, err := os.ReadFile(filepath.Join(dir, output))
	if err != nil {
		return err
	}
	_, err = os.Stdout.Write(data)
	return err
}

func TestRunWiresClaudeStructuredOutput(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"gh", "git", "claude"} {
		testutil.Executable(t, dir, name)
	}
	t.Setenv("GORACE", os.Getenv("GORACE")+" atexit_sleep_ms=0")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
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
