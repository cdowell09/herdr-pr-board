package config

import "testing"

// Repository setup probes PATH for these programs. A missing or duplicated name
// makes setup report the wrong install status for a built-in reviewer.
func TestBuiltinReviewersNameOneExecutableEach(t *testing.T) {
	seen := map[string]string{}
	for _, reviewer := range BuiltinReviewers("board") {
		executable := BuiltinExecutable(reviewer.ID)
		if executable == "" {
			t.Fatalf("built-in reviewer %q has no executable", reviewer.ID)
		}
		if previous, duplicate := seen[executable]; duplicate {
			t.Fatalf("built-in reviewers %q and %q share executable %q", previous, reviewer.ID, executable)
		}
		seen[executable] = reviewer.ID
		if got := reviewer.Executable(); got != executable {
			t.Fatalf("built-in reviewer %q probes %q, want %q", reviewer.ID, got, executable)
		}
	}
	if BuiltinExecutable("custom") != "" {
		t.Fatal("a custom reviewer ID reported a built-in executable")
	}
}

// Setup must not probe PATH for a command that names its own program.
func TestReviewerExecutableSkipsSelectedPrograms(t *testing.T) {
	for name, reviewer := range map[string]Reviewer{
		"custom command":      {ID: "wrapper", Command: []string{"my-wrapper", "--review"}},
		"selected executable": {ID: "claude", Command: []string{"board", "--claude-reviewer", "--claude-executable", "/opt/claude"}},
		"inline executable":   {ID: "claude", Command: []string{"board", "--claude-reviewer", "--claude-executable=/opt/claude"}},
	} {
		if got := reviewer.Executable(); got != "" {
			t.Fatalf("%s probes %q, want no probe", name, got)
		}
	}
	reviewer := Reviewer{ID: "profile", Command: []string{"board", "--claude-reviewer", "--claude-skill", "review.md"}}
	if got := reviewer.Executable(); got != "claude" {
		t.Fatalf("named profile probes %q, want claude", got)
	}
}
