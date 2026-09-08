package config

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
)

// BuiltinReviewers returns the commands available during repository setup.
func BuiltinReviewers(binary string) []Reviewer {
	return []Reviewer{
		{ID: "pi", Command: []string{binary, "--pi-reviewer"}},
		{ID: "codex", Command: []string{binary, "--codex-reviewer"}},
		{ID: "claude", Command: []string{binary, "--claude-reviewer"}},
	}
}

// Builtin identifies the selected native adapter without changing command arguments.
func (r Reviewer) Builtin() string {
	selected := ""
	for _, builtin := range BuiltinReviewers("") {
		value, exists := commandOption(r.Command, builtin.ID+"-reviewer", true)
		enabled, err := strconv.ParseBool(value)
		if exists && err == nil && enabled {
			if selected != "" {
				return ""
			}
			selected = builtin.ID
		}
	}
	return selected
}

// InstructionFiles returns profile selections, falling back to legacy command options.
// Legacy paths become absolute against the launcher's working directory.
// An explicit empty profile value clears a legacy command selection.
func (r Reviewer) InstructionFiles() (prompt, skill string, err error) {
	if builtin := r.Builtin(); builtin != "" {
		prompt, _ = commandOption(r.Command, builtin+"-prompt", false)
		skill, _ = commandOption(r.Command, builtin+"-skill", false)
	}
	if r.PromptFile != nil {
		prompt = *r.PromptFile
	} else if prompt != "" {
		prompt, err = filepath.Abs(prompt)
		if err != nil {
			return "", "", fmt.Errorf("resolve legacy prompt path: %w", err)
		}
	}
	if r.SkillFile != nil {
		skill = *r.SkillFile
	} else if skill != "" {
		skill, err = filepath.Abs(skill)
		if err != nil {
			return "", "", fmt.Errorf("resolve legacy skill path: %w", err)
		}
	}
	return
}

func commandOption(command []string, name string, boolean bool) (string, bool) {
	value, found := "", false
	for i := 1; i < len(command); i++ {
		arg := command[i]
		if arg == "--" || !strings.HasPrefix(arg, "-") {
			break
		}
		key, inline, hasValue := strings.Cut(strings.TrimPrefix(strings.TrimPrefix(arg, "-"), "-"), "=")
		if key != name {
			// Non-boolean adapter options consume their next argument, even if it starts with '-'.
			if !hasValue && (strings.HasSuffix(key, "-skill") || strings.HasSuffix(key, "-prompt") || strings.HasSuffix(key, "-executable")) {
				i++
			}
			continue
		}
		if hasValue {
			value, found = inline, true
		} else if boolean {
			value, found = "true", true
		} else if i+1 < len(command) {
			i++
			value, found = command[i], true
		}
	}
	return value, found
}
