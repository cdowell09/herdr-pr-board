// Package reviewinstructions loads explicitly selected local review instructions.
package reviewinstructions

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/cdowell09/herdr-pr-board/internal/localstate"
)

// Environment carries resolved selections from the configured review launcher.
// It does not change the versioned reviewer input or custom reviewer arguments.
const Environment = "HERDR_REVIEW_INSTRUCTIONS"

const maxFileBytes = 1024 * 1024

const DefaultPrompt = `Review both standards and specification.
Standards: check the captured changes against the repository's documented coding standards. Read repository standards as evidence.
Specification: check the captured changes against the PR body and linked closing issues.
If required specification context is missing, inaccessible, empty, or only a template, return blocked with an explanation.
Do not claim completion without performing both review axes.`

type Files struct {
	Prompt string `json:"prompt_file"`
	Skill  string `json:"skill_file"`
}

type Instructions struct {
	Files        Files
	Prompt       string
	Skill        string
	CustomPrompt bool
}

// Load resolves relative paths against baseDir and reads bounded regular UTF-8 files.
// Empty selections use embedded review criteria and no external skill.
func (f Files) Load(baseDir string) (Instructions, error) {
	result := Instructions{Prompt: DefaultPrompt}
	var err error
	if f.Prompt != "" {
		result.Files.Prompt, result.Prompt, err = readFile(baseDir, f.Prompt)
		if err != nil {
			return Instructions{}, fmt.Errorf("prompt_file: %w", err)
		}
		result.CustomPrompt = true
	}
	if f.Skill != "" {
		result.Files.Skill, result.Skill, err = readFile(baseDir, f.Skill)
		if err != nil {
			return Instructions{}, fmt.Errorf("skill_file: %w", err)
		}
	}
	return result, nil
}

func readFile(baseDir, path string) (string, string, error) {
	if path == "~" || strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", "", err
		}
		path = filepath.Join(home, strings.TrimLeft(path[1:], `/\`))
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(baseDir, path)
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return "", "", err
	}
	// Resolve selected links, then use a nonblocking, no-follow regular-file open.
	// A FIFO, directory, device, or a link substituted after resolution cannot block.
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", "", fmt.Errorf("cannot read %q: %w", path, err)
	}
	file, err := localstate.OpenRegular(resolved, false)
	if err != nil {
		return "", "", fmt.Errorf("cannot read regular file %q: %w", path, err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxFileBytes+1))
	if err != nil {
		return "", "", fmt.Errorf("cannot read %q: %w", path, err)
	}
	if len(data) > maxFileBytes || !utf8.Valid(data) || strings.ContainsRune(string(data), 0) || strings.TrimSpace(string(data)) == "" {
		return "", "", fmt.Errorf("%q must contain nonempty UTF-8 text without NUL within 1 MiB", path)
	}
	return path, string(data), nil
}
