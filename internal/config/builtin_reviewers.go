package config

// BuiltinReviewers returns the commands available during repository setup.
func BuiltinReviewers(binary string) []Reviewer {
	return []Reviewer{
		{ID: "pi", Command: []string{binary, "--pi-reviewer"}},
		{ID: "codex", Command: []string{binary, "--codex-reviewer"}},
		{ID: "claude", Command: []string{binary, "--claude-reviewer"}},
	}
}
