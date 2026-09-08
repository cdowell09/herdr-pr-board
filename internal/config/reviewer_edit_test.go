package config

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func instructionPath(value string) *string { return &value }

func TestReviewerInstructionPrecedence(t *testing.T) {
	for _, builtin := range BuiltinReviewers("board") {
		r := builtin
		r.Command = append(r.Command, "--"+r.ID+"-skill", "legacy skill.md", "--"+r.ID+"-prompt=legacy prompt.md")
		if r.Builtin() != r.ID {
			t.Fatalf("adapter=%q", r.Builtin())
		}
		prompt, skill, err := r.InstructionFiles()
		wantPrompt, _ := filepath.Abs("legacy prompt.md")
		wantSkill, _ := filepath.Abs("legacy skill.md")
		if err != nil || prompt != wantPrompt || skill != wantSkill {
			t.Fatalf("legacy selections=%q %q", prompt, skill)
		}
		r.PromptFile, r.SkillFile = instructionPath("custom prompt.md"), instructionPath("")
		prompt, skill, err = r.InstructionFiles()
		if err != nil || prompt != "custom prompt.md" || skill != "" {
			t.Fatalf("profile selections=%q %q", prompt, skill)
		}
	}
	for _, command := range [][]string{{"custom", "--", "--pi-reviewer"}, {"custom", "--pi-skill", "--pi-reviewer"}, {"board", "--pi-reviewer=false"}, {"board", "--pi-reviewer", "--codex-reviewer"}} {
		if got := (Reviewer{Command: command}).Builtin(); got != "" {
			t.Fatalf("ambiguous command %q identified as %s", command, got)
		}
	}
}

func TestLegacyRelativeInstructionsKeepLauncherDirectory(t *testing.T) {
	working, configuration := t.TempDir(), t.TempDir()
	t.Chdir(working)
	for dir, content := range map[string]string{working: "Legacy skill", configuration: "Profile skill"} {
		if err := os.WriteFile(filepath.Join(dir, "selected.md"), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	reviewer := Reviewer{ID: "pi", Command: []string{"board", "--pi-reviewer", "--pi-skill", "selected.md"}}
	path := filepath.Join(configuration, "config.toml")
	legacy, err := reviewer.LoadInstructions(path)
	if err != nil || legacy.Skill != "Legacy skill" || legacy.Files.Skill != filepath.Join(working, "selected.md") {
		t.Fatalf("legacy path changed: %+v, %v", legacy, err)
	}
	_, displayed, err := reviewer.InstructionFiles()
	if err != nil || displayed != legacy.Files.Skill {
		t.Fatalf("board path differs from legacy target: %q, %v", displayed, err)
	}
	reviewer.SkillFile = &displayed
	accepted, err := reviewer.LoadInstructions(path)
	if err != nil || accepted.Skill != legacy.Skill {
		t.Fatalf("accepting unchanged displayed path changed target: %+v, %v", accepted, err)
	}
	reviewer.SkillFile = instructionPath("selected.md")
	profile, err := reviewer.LoadInstructions(path)
	if err != nil || profile.Skill != "Profile skill" {
		t.Fatalf("profile did not use configuration directory: %+v, %v", profile, err)
	}
}

func TestReviewerInstructionEditsPreserveCommandAndUnrelatedSettings(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	content := DefaultFile + `
[[reviewers]]
id = 'security'
command = ['board', '--pi-reviewer', '--pi-executable', 'agent with spaces', '--pi-skill', 'missing legacy.md'] # exact argv
skill_file = '' # explicit default

[[reviewers]]
id = 'custom'
command = ['custom wrapper', 'a; $(literal)', 'with spaces'] # keep custom

[[repositories]]
name = 'acme/api'
reviewer = 'security'

[[repositories]]
name = 'other/api'
reviewer = 'security'
`
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "security.md"), []byte("Review security only."), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadExisting(path)
	if err != nil {
		t.Fatal(err)
	}
	expected := cfg.Reviewers[0]
	updated := expected
	updated.PromptFile = instructionPath("security.md")
	edit := ReviewerEdit{Value: updated, Expected: &expected}
	repo := repositoryExpectation(t, path, "acme/api")
	saved, err := SaveRepository(context.Background(), path, t.TempDir(), *repo, &edit, repo, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !saved.Reviewers[0].Equal(updated) || saved.Repositories[1].Reviewer != "security" {
		t.Fatalf("saved=%+v", saved)
	}
	data, _ := os.ReadFile(path)
	for _, want := range []string{DefaultFile, "command = ['board', '--pi-reviewer', '--pi-executable', 'agent with spaces', '--pi-skill', 'missing legacy.md'] # exact argv", "command = ['custom wrapper', 'a; $(literal)', 'with spaces'] # keep custom", "# explicit default", "name = 'other/api'\nreviewer = 'security'"} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("lost %q:\n%s", want, data)
		}
	}
	if _, err := SaveRepository(context.Background(), path, t.TempDir(), *repo, &edit, repo, nil); err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("stale shared profile edit accepted: %v", err)
	}
	after, _ := os.ReadFile(path)
	if string(after) != string(data) {
		t.Fatal("stale edit changed configuration")
	}
}

func TestReviewerSetupRejectsMissingFilesWithoutChangingConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(DefaultFile), 0600); err != nil {
		t.Fatal(err)
	}
	value := BuiltinReviewers("board")[0]
	value.PromptFile = instructionPath("missing.md")
	_, err := SaveRepository(context.Background(), path, t.TempDir(), Repository{Name: "acme/api", Reviewer: value.ID}, &ReviewerEdit{Value: value}, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "prompt_file") || !strings.Contains(err.Error(), "missing.md") {
		t.Fatalf("setup error=%v", err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != DefaultFile {
		t.Fatal("invalid instructions changed configuration")
	}
	content := DefaultFile + "\n[[reviewers]]\nid='pi'\ncommand=['board','--pi-reviewer']\nprompt_file='missing.md'\n"
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadExisting(path); err != nil {
		t.Fatalf("board cannot load missing path for repair: %v", err)
	}
	if err := Check(path); err == nil || !strings.Contains(err.Error(), "missing.md") {
		t.Fatalf("configuration validation missed instructions: %v", err)
	}
}
