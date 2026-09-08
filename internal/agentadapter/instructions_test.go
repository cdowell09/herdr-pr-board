package agentadapter

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cdowell09/herdr-pr-board/internal/reviewinstructions"
	"github.com/cdowell09/herdr-pr-board/internal/reviewmemory"
)

func instructionCommand(t *testing.T, wantSkill string) func(string, string, string, string) (*exec.Cmd, error) {
	t.Helper()
	return func(binary, skill, _, checkout string) (*exec.Cmd, error) {
		if skill != wantSkill {
			t.Fatalf("native skill=%q, want %q", skill, wantSkill)
		}
		return exec.Command(binary, checkout), nil
	}
}

func TestRunUsesEmbeddedDefaultsWithoutSkillFile(t *testing.T) {
	in, opts := prepareRun(t, "completed")
	opts.Skill = ""
	opts.Command = instructionCommand(t, "")
	if err := Run(context.Background(), in, opts); err != nil {
		t.Fatal(err)
	}
	if result := readResult(t, in); result.Outcome.Status != reviewmemory.Completed {
		t.Fatalf("result=%+v", result)
	}
	data, err := os.ReadFile(filepath.Join(filepath.Dir(in.ResultPath), "prompt"))
	if err != nil || !strings.Contains(string(data), reviewinstructions.DefaultPrompt) {
		t.Fatalf("embedded criteria missing: %s, %v", data, err)
	}
}

func TestCustomPromptDoesNotRequireDefaultSpecificationEvidence(t *testing.T) {
	for _, mode := range []string{"empty_spec", "missing_issue", "changed_pr"} {
		t.Run(mode, func(t *testing.T) {
			in, opts := prepareRun(t, mode)
			opts.Prompt = filepath.Join(filepath.Dir(in.ResultPath), "security.md")
			writeTestFile(t, opts.Prompt, []byte("Review only security vulnerabilities."), 0600)
			opts.Skill = ""
			opts.Command = instructionCommand(t, "")
			if err := Run(context.Background(), in, opts); err != nil {
				t.Fatal(err)
			}
			result := readResult(t, in)
			if mode == "changed_pr" {
				if result.Outcome.Status != reviewmemory.Blocked || !strings.Contains(result.Outcome.Message, "revision changed") {
					t.Fatalf("custom instructions lost revision check: %+v", result)
				}
				return
			}
			if result.Outcome.Status != reviewmemory.Completed {
				t.Fatalf("security-only review required spec: %+v", result)
			}
			data, err := os.ReadFile(filepath.Join(filepath.Dir(in.ResultPath), "prompt"))
			if err != nil {
				t.Fatal(err)
			}
			for _, required := range []string{"Review only security vulnerabilities.", "Never publish", "Preserve version, identity and base_oid exactly", "Do not change source files", "CAPTURED_DIFF", "CAPTURED_LOG"} {
				if !strings.Contains(string(data), required) {
					t.Fatalf("prompt omitted %q", required)
				}
			}
			for _, unwanted := range []string{"Review both standards and specification", "Review both axes", "Do not claim completion without performing both"} {
				if strings.Contains(string(data), unwanted) {
					t.Fatalf("custom prompt retained implicit requirement %q", unwanted)
				}
			}
			if mode == "missing_issue" && !strings.Contains(string(data), "retrieve linked specification") {
				t.Fatal("missing optional context was not represented as evidence")
			}
		})
	}
}

func TestConfiguredInstructionsOverrideLegacyOptionsAndCombineExplicitSkill(t *testing.T) {
	for _, skillSelected := range []bool{false, true} {
		t.Run(map[bool]string{false: "clear skill", true: "selected skill"}[skillSelected], func(t *testing.T) {
			in, opts := prepareRun(t, "empty_spec")
			promptPath := filepath.Join(filepath.Dir(in.ResultPath), "security.md")
			writeTestFile(t, promptPath, []byte("Review security only."), 0600)
			files := reviewinstructions.Files{Prompt: promptPath}
			if skillSelected {
				files.Skill = opts.Skill
				writeTestFile(t, files.Skill, []byte("Check authorization in every changed route."), 0600)
			}
			opts.Prompt, opts.Skill = "missing legacy prompt.md", "missing legacy skill.md"
			opts.Command = instructionCommand(t, files.Skill)
			selection, err := json.Marshal(files)
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv(reviewinstructions.Environment, string(selection))
			if err := Run(context.Background(), in, opts); err != nil {
				t.Fatal(err)
			}
			if result := readResult(t, in); result.Outcome.Status != reviewmemory.Completed {
				t.Fatalf("profile override failed: %+v", result)
			}
			data, err := os.ReadFile(filepath.Join(filepath.Dir(in.ResultPath), "prompt"))
			if err != nil || strings.Contains(string(data), reviewinstructions.DefaultPrompt) || strings.Contains(string(data), "Check authorization in every changed route.") != skillSelected {
				t.Fatalf("incorrect instruction combination: %s, %v", data, err)
			}
		})
	}
}

func TestCustomPromptStillRejectsWrongResultRevision(t *testing.T) {
	in, opts := prepareRun(t, "wrong_revision")
	opts.Prompt = filepath.Join(filepath.Dir(in.ResultPath), "security.md")
	writeTestFile(t, opts.Prompt, []byte("Review security only."), 0600)
	if err := Run(context.Background(), in, opts); err == nil {
		t.Fatal("custom prompt accepted a result for another revision")
	}
	if _, err := os.Stat(in.ResultPath); !os.IsNotExist(err) {
		t.Fatalf("invalid result written: %v", err)
	}
}
