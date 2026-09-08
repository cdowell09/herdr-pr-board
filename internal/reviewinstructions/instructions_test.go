package reviewinstructions

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestDefaultsRequireNoExternalFiles(t *testing.T) {
	got, err := (Files{}).Load(filepath.Join(t.TempDir(), "does-not-exist"))
	if err != nil || got.Prompt != DefaultPrompt || got.CustomPrompt || got.Skill != "" || got.Files != (Files{}) {
		t.Fatalf("defaults=%+v, error=%v", got, err)
	}
}

func TestSelectedFilesResolveAgainstConfigurationDirectory(t *testing.T) {
	dir := t.TempDir()
	prompt, skill := "security prompt.md", "selected skill.md"
	for name, content := range map[string]string{prompt: "Review security only.\n", skill: "Check authorization.\n"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	got, err := (Files{Prompt: prompt, Skill: skill}).Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !got.CustomPrompt || got.Prompt != "Review security only.\n" || got.Skill != "Check authorization.\n" || got.Files.Prompt != filepath.Join(dir, prompt) || got.Files.Skill != filepath.Join(dir, skill) {
		t.Fatalf("selections=%+v", got)
	}
	defaultCriteria, err := (Files{Skill: skill}).Load(dir)
	if err != nil || defaultCriteria.Prompt != DefaultPrompt || defaultCriteria.CustomPrompt {
		t.Fatalf("skill replaced default criteria: %+v, %v", defaultCriteria, err)
	}
}

func TestInstructionFilesRejectInvalidSources(t *testing.T) {
	for _, mode := range []string{"missing", "directory", "empty", "invalid-utf8", "nul", "oversized"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "instructions.md")
			data := []byte(" \n\t")
			switch mode {
			case "directory":
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
			case "invalid-utf8":
				data = []byte{0xff}
			case "nul":
				data = []byte("review\x00security")
			case "oversized":
				data = []byte(strings.Repeat("x", maxFileBytes+1))
			}
			if mode != "missing" && mode != "directory" {
				if err := os.WriteFile(path, data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			for _, files := range []Files{{Prompt: path}, {Skill: path}} {
				if _, err := files.Load(dir); err == nil || !strings.Contains(err.Error(), strconv.Quote(path)) {
					t.Fatalf("invalid source accepted or path omitted: %v", err)
				}
			}
		})
	}
}
