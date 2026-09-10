package config

import (
	"slices"
	"testing"
)

// Repository setup probes PATH for these programs. A wrong or duplicated name
// makes setup report the wrong install status for a built-in reviewer.
func TestBuiltinReviewersProbeTheProgramTheAdapterStarts(t *testing.T) {
	// Each adapter that does not start a program named after its ID.
	// internal/antigravityadapter/run.go defaults its binary to "agy".
	// internal/hermesadapter/run.go and internal/cursoradapter/run.go start a
	// shared language runtime, so PATH cannot prove the agent is installed.
	special := map[string]string{"antigravity": "agy", "hermes": "", "cursor": ""}
	var detectable []string
	for _, reviewer := range BuiltinReviewers("board") {
		want, exception := special[reviewer.ID]
		if !exception {
			want = reviewer.ID
		}
		if got := reviewer.Executable(); got != want {
			t.Fatalf("built-in reviewer %q probes %q, want %q", reviewer.ID, got, want)
		}
		if want != "" {
			detectable = append(detectable, want)
		}
	}
	if got := DetectableExecutables(); !slices.Equal(got, detectable) {
		t.Fatalf("probe list %v, want %v", got, detectable)
	}
	for i, executable := range detectable {
		if slices.Index(detectable, executable) != i {
			t.Fatalf("two built-in reviewers share executable %q", executable)
		}
	}
}

// Setup must not probe PATH for a command that names its own program.
func TestReviewerExecutableSkipsSelectedPrograms(t *testing.T) {
	for name, reviewer := range map[string]Reviewer{
		"custom command":      {ID: "wrapper", Command: []string{"my-wrapper", "--review"}},
		"selected executable": {ID: "claude", Command: []string{"board", "--claude-reviewer", "--claude-executable", "/opt/claude"}},
		"inline executable":   {ID: "claude", Command: []string{"board", "--claude-reviewer", "--claude-executable=/opt/claude"}},
		"shared runtime":      {ID: "hermes", Command: []string{"board", "--hermes-reviewer"}},
	} {
		if got := reviewer.Executable(); got != "" {
			t.Fatalf("%s probes %q, want no probe", name, got)
		}
	}
	// An empty value keeps the adapter default, so setup probes that default.
	for name, reviewer := range map[string]Reviewer{
		"named profile":        {ID: "profile", Command: []string{"board", "--claude-reviewer", "--claude-skill", "review.md"}},
		"inline empty value":   {ID: "claude", Command: []string{"board", "--claude-reviewer", "--claude-executable="}},
		"separate empty value": {ID: "claude", Command: []string{"board", "--claude-reviewer", "--claude-executable", ""}},
	} {
		if got := reviewer.Executable(); got != "claude" {
			t.Fatalf("%s probes %q, want claude", name, got)
		}
	}
}
