package codexadapter

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
	if tool == "gh" || tool == "git" || tool == "codex" {
		if err := runFixtureTool(tool, os.Getenv("CODEX_TEST_DIR"), os.Args[1:]); err != nil {
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
	case "codex":
		if os.Getenv("CODEX_TEST_CANCEL") == "1" {
			return runCancellationAgent(dir)
		}
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
	if tool == "codex" {
		fmt.Fprintln(os.Stdout, "{\"type\":\"thread.started\"}\n{\"type\":\"turn.started\"}")
	}
	_, err = os.Stdout.Write(data)
	if err == nil && tool == "codex" {
		_, err = fmt.Fprintln(os.Stdout, "\n{\"type\":\"turn.completed\"}")
	}
	return err
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestRunWiresCodexFinalOutput(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"gh", "git", "codex"} {
		testutil.Executable(t, dir, name)
	}
	t.Setenv("GORACE", os.Getenv("GORACE")+" atexit_sleep_ms=0")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("CODEX_TEST_DIR", dir)
	in := reviewercontract.Input{Version: 1, Identity: reviewmemory.Identity{Repository: "owner/repo", Number: 42, HeadOID: strings.Repeat("a", 40), BaseRefName: "main"}, BaseOID: strings.Repeat("b", 40), ResultPath: filepath.Join(dir, "result.json")}
	result := reviewercontract.Result{Version: 1, Identity: in.Identity, BaseOID: in.BaseOID, Outcome: reviewmemory.Outcome{Status: reviewmemory.Completed, Message: "Both axes reviewed", Findings: []reviewmemory.Finding{}}}
	for name, value := range map[string]any{
		"pr.json":  map[string]any{"body": "Review this specified change.", "headRefOid": in.Identity.HeadOID, "baseRefOid": in.BaseOID, "baseRefName": "main"},
		"response": map[string]any{"type": "item.completed", "item": map[string]any{"type": "agent_message", "text": string(mustJSON(t, result))}},
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
	if err := os.WriteFile(skill, []byte("Selected Codex review skill"), 0600); err != nil {
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
	if !strings.Contains(string(prompt), "Selected Codex review skill") {
		t.Fatal("Codex did not receive the selected skill")
	}
}
