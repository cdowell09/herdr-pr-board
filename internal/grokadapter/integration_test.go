package grokadapter

import (
	"context"
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

func TestMain(m *testing.M) {
	tool := strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe")
	if tool == "gh" || tool == "git" || tool == "grok" {
		if err := fixtureTool(tool, os.Getenv("PR_BOARD_GROK_TEST_DIR"), os.Args[1:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func fixtureTool(tool, dir string, args []string) error {
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
	case "grok":
		if args[0] == "inspect" {
			return json.NewEncoder(os.Stdout).Encode(inspection(filepath.Join(os.Getenv("GROK_HOME"), "config.toml")))
		}
		var promptPath string
		for i := 0; i+1 < len(args); i++ {
			if args[i] == "--prompt-file" {
				promptPath = args[i+1]
			}
		}
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		actual, err := filepath.EvalSymlinks(filepath.Dir(promptPath))
		if err != nil || actual != cwd {
			return fmt.Errorf("runtime cwd is not the isolated session: %v", err)
		}
		prompt, err := os.ReadFile(promptPath)
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

func TestRunUsesSharedCapturedRevisionAndSelectedInstructions(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"gh", "git", "grok"} {
		testutil.Executable(t, dir, name)
	}
	t.Setenv("GORACE", os.Getenv("GORACE")+" atexit_sleep_ms=0")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("PR_BOARD_GROK_TEST_DIR", dir)
	in := reviewercontract.Input{Version: 1, Identity: reviewmemory.Identity{Repository: "owner/repo", Number: 42, HeadOID: strings.Repeat("a", 40), BaseRefName: "main"}, BaseOID: strings.Repeat("b", 40), ResultPath: filepath.Join(dir, "result.json")}
	result := reviewercontract.Result{Version: 1, Identity: in.Identity, BaseOID: in.BaseOID, Outcome: reviewmemory.Outcome{Status: reviewmemory.Completed, Message: "Both axes reviewed", Findings: []reviewmemory.Finding{}}}
	pr, err := json.Marshal(map[string]any{"body": "Review this specified change.", "headRefOid": in.Identity.HeadOID, "baseRefOid": in.BaseOID, "baseRefName": "main"})
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(dir, "pr.json"), string(pr))
	skill := filepath.Join(dir, "SKILL.md")
	writeTestFile(t, skill, "Selected Grok review skill")
	for _, wrongRevision := range []bool{false, true} {
		if wrongRevision {
			result.Identity.HeadOID = strings.Repeat("c", 40)
			in.ResultPath = filepath.Join(dir, "wrong-revision.json")
		}
		payload, err := json.Marshal(result)
		if err != nil {
			t.Fatal(err)
		}
		writeTestFile(t, filepath.Join(dir, "response"), string(transcript(string(payload))))
		err = Run(context.Background(), in, Options{Skill: skill})
		if wrongRevision {
			if err == nil {
				t.Fatal("wrong captured revision accepted")
			}
			if _, err := os.Stat(in.ResultPath); !os.IsNotExist(err) {
				t.Fatal("invalid result was written")
			}
			continue
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
		if err := got.Validate(in); err != nil || got.Outcome.Status != reviewmemory.Completed {
			t.Fatalf("result=%+v error=%v", got, err)
		}
	}
	prompt, err := os.ReadFile(filepath.Join(dir, "prompt"))
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"Selected Grok review skill", "The captured checkout is", "/checkout", in.Identity.HeadOID} {
		if !strings.Contains(filepath.ToSlash(string(prompt)), expected) {
			t.Errorf("Grok prompt omits %s", expected)
		}
	}
	leftovers, err := filepath.Glob(filepath.Join(dir, "grok-*"))
	if err != nil || len(leftovers) != 2 { // Only the durable events and diagnostics remain.
		t.Fatalf("unexpected Grok temporary files: %v error=%v", leftovers, err)
	}
}
