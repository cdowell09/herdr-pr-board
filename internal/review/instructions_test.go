package review

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/cdowell09/herdr-pr-board/internal/cli"
	"github.com/cdowell09/herdr-pr-board/internal/config"
	"github.com/cdowell09/herdr-pr-board/internal/reviewercontract"
	"github.com/cdowell09/herdr-pr-board/internal/reviewinstructions"
	"github.com/cdowell09/herdr-pr-board/internal/reviewmemory"
	"github.com/cdowell09/herdr-pr-board/internal/testutil"
)

func TestMain(m *testing.M) {
	if strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe") == "instruction-reviewer" {
		if err := runInstructionReviewer(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func runInstructionReviewer() error {
	var in reviewercontract.Input
	if err := json.NewDecoder(os.Stdin).Decode(&in); err != nil {
		return err
	}
	claim, err := cli.InheritedFile("HERDR_REVIEW_CLAIM_FD")
	if err != nil || claim == nil {
		return fmt.Errorf("missing inherited claim: %v", err)
	}
	defer claim.Close()
	data, err := json.Marshal(struct {
		Selection string
		Arguments []string
	}{os.Getenv(reviewinstructions.Environment), os.Args[1:]})
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(in.ResultPath), "selections.json"), data, 0600); err != nil {
		return err
	}
	result := reviewercontract.Result{Version: reviewercontract.Version, Identity: in.Identity, BaseOID: in.BaseOID,
		Outcome: reviewmemory.Outcome{Status: reviewmemory.Completed, Message: "Reviewed selected criteria", Findings: []reviewmemory.Finding{}}}
	data, err = json.Marshal(result)
	if err != nil {
		return err
	}
	return os.WriteFile(in.ResultPath, data, 0600)
}

func configuredInstructionService(t *testing.T, adapter string) (*Service, []string) {
	t.Helper()
	t.Setenv("GORACE", os.Getenv("GORACE")+" atexit_sleep_ms=0")
	dir := t.TempDir()
	binary := testutil.Executable(t, dir, "instruction-reviewer")
	command := []string{binary, "--" + adapter + "-reviewer", "--" + adapter + "-skill", "missing legacy; $(literal).md"}
	encoded, _ := json.Marshal(command)
	path := filepath.Join(dir, "config.toml")
	data := config.DefaultFile + "\n[[reviewers]]\nid='security'\ncommand=" + string(encoded) + "\nprompt_file='security prompt.md'\nskill_file=''\n[[repositories]]\nname='acme/repo'\nreviewer='security'\n"
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "security prompt.md"), []byte("Review security only."), 0600); err != nil {
		t.Fatal(err)
	}
	service, err := New(dir, path, &revisionStub{})
	if err != nil {
		t.Fatal(err)
	}
	return service, command
}

func TestLaunchPassesProfileInstructionsWithoutChangingArguments(t *testing.T) {
	for _, builtin := range config.BuiltinReviewers("") {
		t.Run(builtin.ID, func(t *testing.T) {
			service, command := configuredInstructionService(t, builtin.ID)
			run, err := service.Review(context.Background(), Request{URL: testPRURL}, nil)
			if err != nil || run.Status != reviewmemory.Completed {
				t.Fatalf("run=%+v error=%v", run, err)
			}
			data, err := os.ReadFile(filepath.Join(service.RunDirectory(run.ID), "selections.json"))
			if err != nil {
				t.Fatal(err)
			}
			var got struct {
				Selection string
				Arguments []string
			}
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatal(err)
			}
			var files reviewinstructions.Files
			if err := json.Unmarshal([]byte(got.Selection), &files); err != nil {
				t.Fatal(err)
			}
			if files.Prompt != filepath.Join(filepath.Dir(service.configPath), "security prompt.md") || files.Skill != "" || !slices.Equal(got.Arguments, command[1:]) {
				t.Fatalf("selection=%+v arguments=%q", files, got.Arguments)
			}
		})
	}
}

func TestLaunchRejectsChangedInstructionProfile(t *testing.T) {
	for _, field := range []string{"prompt_file", "skill_file"} {
		t.Run(field, func(t *testing.T) {
			service, _ := configuredInstructionService(t, "pi")
			run, err := service.Review(context.Background(), Request{URL: testPRURL}, func(status string) {
				if status != "running" {
					return
				}
				data, err := os.ReadFile(service.configPath)
				if err != nil {
					t.Fatal(err)
				}
				old := field + "=''"
				if field == "prompt_file" {
					old = field + "='security prompt.md'"
				}
				data = []byte(strings.Replace(string(data), old, field+"='changed.md'", 1))
				if err := os.WriteFile(service.configPath, data, 0600); err != nil {
					t.Fatal(err)
				}
			})
			if err == nil || !strings.Contains(err.Error(), "configuration changed") || run.Status != reviewmemory.Failed {
				t.Fatalf("stale instructions launched: %+v, %v", run, err)
			}
			if _, err := os.Stat(filepath.Join(service.RunDirectory(run.ID), "selections.json")); !os.IsNotExist(err) {
				t.Fatalf("reviewer ran after settings changed: %v", err)
			}
		})
	}
}
