package config

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
)

// builtins is the single source for built-in reviewer IDs and for the agent
// program each adapter runs when the user selects no --<id>-executable.
//
// The adapter packages do not export that program name. Each adapter passes its
// ID as agentadapter.Options.Name, and agentadapter.Run uses Name as the binary
// when Binary is empty. Keep each executable below equal to the Name literal in
// internal/<id>adapter. Add one row here with each new adapter.
var builtins = []struct{ ID, Executable string }{
	{"pi", "pi"},
	{"codex", "codex"},
	{"claude", "claude"},
	{"qwen", "qwen"},
	{"omp", "omp"},
	{"kimi", "kimi"},
	{"qodercli", "qodercli"},
	{"copilot", "copilot"},
	{"mastracode", "mastracode"},
	{"hermes", "hermes"},
	{"cursor", "cursor"},
	{"antigravity", "antigravity"},
	{"grok", "grok"},
}

// BuiltinReviewers returns the commands available during repository setup.
func BuiltinReviewers(binary string) []Reviewer {
	reviewers := make([]Reviewer, 0, len(builtins))
	for _, builtin := range builtins {
		reviewers = append(reviewers, Reviewer{ID: builtin.ID, Command: []string{binary, "--" + builtin.ID + "-reviewer"}})
	}
	return reviewers
}

// BuiltinExecutable returns the agent program a built-in reviewer ID runs by
// default. It returns an empty string for a custom reviewer ID.
func BuiltinExecutable(id string) string {
	for _, builtin := range builtins {
		if builtin.ID == id {
			return builtin.Executable
		}
	}
	return ""
}

// Executable returns the agent program repository setup must find on PATH for
// this reviewer. It returns an empty string when setup must not probe PATH.
// A custom command and an explicit --<id>-executable path both name their own
// program, so setup counts them as available.
func (r Reviewer) Executable() string {
	builtin := r.Builtin()
	if builtin == "" {
		return ""
	}
	if _, selected := commandOption(r.Command, builtin+"-executable", false); selected {
		return ""
	}
	return BuiltinExecutable(builtin)
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
