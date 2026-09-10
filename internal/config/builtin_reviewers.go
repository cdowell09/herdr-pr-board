package config

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
)

// builtins is the single source for built-in reviewer IDs and for the agent
// program repository setup must find on PATH.
//
// Executable is the program the adapter launches when the user selects no
// --<id>-executable. The adapter packages do not export that name. Most adapters
// pass their ID as agentadapter.Options.Name, and agentadapter.Run uses Name as
// the binary when Binary is empty. Four adapters differ. Keep this table equal
// to these sources, and add one row with each new adapter:
//
//   - internal/antigravityadapter/run.go defaults its binary to "agy".
//   - internal/mastraadapter/run.go finds "mastracode" and runs it with Node.
//   - internal/hermesadapter/run.go runs an embedded bridge with "python3".
//   - internal/cursoradapter/run.go runs an embedded bridge with "node".
//
// An empty Executable means PATH cannot show whether the reviewer is available.
// The Hermes and Cursor adapters start a shared language runtime, and that
// runtime does not prove the agent SDK is installed. Setup does not probe for
// these reviewers, and it does not select one of them as the default.
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
	{"hermes", ""},
	{"cursor", ""},
	{"antigravity", "agy"},
	{"grok", "grok"},
}

// DetectableExecutables returns each agent program repository setup probes on
// PATH, in table order.
func DetectableExecutables() []string {
	executables := make([]string, 0, len(builtins))
	for _, builtin := range builtins {
		if builtin.Executable != "" {
			executables = append(executables, builtin.Executable)
		}
	}
	return executables
}

// BuiltinReviewers returns the commands available during repository setup.
func BuiltinReviewers(binary string) []Reviewer {
	reviewers := make([]Reviewer, 0, len(builtins))
	for _, builtin := range builtins {
		reviewers = append(reviewers, Reviewer{ID: builtin.ID, Command: []string{binary, "--" + builtin.ID + "-reviewer"}})
	}
	return reviewers
}

// Executable returns the agent program repository setup must find on PATH for
// this reviewer. An empty result means setup must not probe PATH, and setup
// then reports no install status and does not select the reviewer as the
// default. Setup does not probe three kinds of reviewer:
//
//   - A custom command, because it names its own program.
//   - A nonempty --<id>-executable path, because it names its own program.
//   - A reviewer whose adapter starts a shared language runtime.
//
// An empty --<id>-executable value keeps the adapter default, so setup probes
// that default.
func (r Reviewer) Executable() string {
	builtin := r.Builtin()
	if builtin == "" {
		return ""
	}
	if selected, _ := commandOption(r.Command, builtin+"-executable", false); selected != "" {
		return ""
	}
	for _, entry := range builtins {
		if entry.ID == builtin {
			return entry.Executable
		}
	}
	return ""
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
