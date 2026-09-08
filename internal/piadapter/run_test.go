package piadapter

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/cdowell09/herdr-pr-board/internal/reviewercontract"
	"github.com/cdowell09/herdr-pr-board/internal/reviewmemory"
	"github.com/cdowell09/herdr-pr-board/internal/testutil"
)

func TestMain(m *testing.M) {
	tool := strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe")
	if tool == "gh" || tool == "git" || tool == "pi" {
		if err := runFixtureTool(tool, os.Getenv("PI_TEST_DIR"), os.Args[1:]); err != nil {
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
	case "pi":
		data, err := json.Marshal(args)
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, "args"), data, 0600); err != nil {
			return err
		}

		prompt, err := io.ReadAll(os.Stdin)
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, "prompt"), prompt, 0600); err != nil {
			return err
		}
		output = "events"
	}
	data, err := os.ReadFile(filepath.Join(dir, output))
	if err != nil {
		return err
	}
	_, err = os.Stdout.Write(data)
	return err
}

func TestRunWiresPiCommandAndEvents(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"gh", "git", "pi"} {
		testutil.Executable(t, dir, name)
	}
	t.Setenv("GORACE", os.Getenv("GORACE")+" atexit_sleep_ms=0")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
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
	var gotArgs []string
	if err := json.Unmarshal(args, &gotArgs); err != nil {
		t.Fatal(err)
	}
	want := []string{"--print", "--mode", "json", "--no-session", "--no-extensions", "--no-skills", "--no-context-files", "--no-approve", "--skill", skill}
	if !slices.Equal(gotArgs, want) {
		t.Fatalf("Pi arguments=%q", gotArgs)
	}
	prompt, err := os.ReadFile(filepath.Join(dir, "prompt"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(prompt), "Selected Pi review skill") {
		t.Fatal("Pi did not receive the selected skill")
	}
}
