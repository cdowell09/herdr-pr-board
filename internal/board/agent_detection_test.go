package board

import (
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/cdowell09/herdr-pr-board/internal/config"
	"github.com/cdowell09/herdr-pr-board/internal/testutil"
)

const noAgentHint = "No agent CLI found on PATH. Install one, then reopen settings."

// fakeLookPath reports only the named programs as present on PATH.
func fakeLookPath(names ...string) func(string) (string, error) {
	return func(name string) (string, error) {
		if slices.Contains(names, name) {
			return filepath.Join(string(filepath.Separator)+"agents", name), nil
		}
		return "", exec.ErrNotFound
	}
}

// setupWith builds repository setup for a repository with no saved reviewer.
func setupWith(t *testing.T, installed map[string]bool) *repositorySetup {
	t.Helper()
	cfg := testConfig()
	setup, err := newRepositorySetup(cfg, "acme/repo", installed)
	if err != nil {
		t.Fatal(err)
	}
	return setup
}

func TestSetupDefaultsToAnInstalledAgentAndLabelsMissingOnes(t *testing.T) {
	for name, test := range map[string]struct {
		installed []string
		reviewer  string
		missing   bool
		hint      bool
	}{
		"installed agent wins":         {installed: []string{"claude"}, reviewer: "claude"},
		"table order breaks ties":      {installed: []string{"grok", "codex"}, reviewer: "codex"},
		"first builtin without a find": {reviewer: "pi", missing: true, hint: true},
		// antigravityadapter starts agy, not a program named after its ID.
		"antigravity starts agy": {installed: []string{"agy"}, reviewer: "antigravity"},
		// The reviewer ID is not the program, so PATH holding it proves nothing.
		"the ID is not the program": {installed: []string{"antigravity"}, reviewer: "pi", missing: true, hint: true},
	} {
		t.Run(name, func(t *testing.T) {
			setup := setupWith(t, installedAgents(fakeLookPath(test.installed...)))
			if setup.repo.Reviewer != test.reviewer {
				t.Fatalf("default reviewer %q, want %q", setup.repo.Reviewer, test.reviewer)
			}
			label := setup.rows()[repositoryReviewerRow]
			if strings.Contains(label, "not installed") != test.missing {
				t.Fatalf("reviewer row %q does not match missing=%v", label, test.missing)
			}
			m := panelModel(t)
			m.region.setup = setup
			view := stripANSI(m.View())
			if strings.Contains(view, noAgentHint) != test.hint {
				t.Fatalf("install hint present=%v, want %v:\n%s", !test.hint, test.hint, view)
			}
		})
	}
}

// A user who saved a reviewer keeps it. Setup reports the missing program
// instead of moving the selection without asking.
func TestSetupKeepsASavedReviewerWithAMissingProgram(t *testing.T) {
	cfg := testConfig()
	cfg.Repositories = []config.Repository{{Name: "acme/repo", Reviewer: "grok"}}
	setup, err := newRepositorySetup(cfg, "acme/repo", installedAgents(fakeLookPath("claude")))
	if err != nil {
		t.Fatal(err)
	}
	if setup.repo.Reviewer != "grok" {
		t.Fatalf("saved reviewer became %q", setup.repo.Reviewer)
	}
	if label := setup.rows()[repositoryReviewerRow]; !strings.Contains(label, "not installed") {
		t.Fatalf("saved reviewer row hides the missing program: %q", label)
	}
	m := panelModel(t)
	m.region.setup = setup
	if view := stripANSI(m.View()); strings.Contains(view, noAgentHint) {
		t.Fatal("install hint appeared while an agent is installed")
	}
}

// The Hermes and Cursor adapters start a shared language runtime. PATH cannot
// show whether the agent is installed, so setup reports no install status and
// never selects one of them as the default.
func TestSetupLeavesSharedRuntimeReviewersUnchecked(t *testing.T) {
	if slices.Contains(config.DetectableExecutables(), "python3") || slices.Contains(config.DetectableExecutables(), "node") {
		t.Fatal("setup probes a shared language runtime")
	}
	// Every agent program is absent, but both shared runtimes are present.
	setup := setupWith(t, installedAgents(fakeLookPath("python3", "node")))
	if setup.repo.Reviewer != "pi" {
		t.Fatalf("a shared runtime selected %q as the default", setup.repo.Reviewer)
	}
	for _, id := range []string{"hermes", "cursor"} {
		setup.repo.Reviewer = id
		if label := setup.rows()[repositoryReviewerRow]; strings.Contains(label, "not installed") {
			t.Fatalf("setup reported an install status it cannot know: %q", label)
		}
	}
}

// A custom command is always available, so it wins the default over a built-in
// reviewer that PATH holds. Setup takes the first available reviewer in
// configuration order, and configured reviewers come before missing built-ins.
func TestSetupDefaultsToACustomReviewerBeforeAnInstalledBuiltin(t *testing.T) {
	for name, test := range map[string]struct {
		reviewers []config.Reviewer
		reviewer  string
	}{
		"a custom command comes first": {reviewers: []config.Reviewer{{ID: "agent", Command: []string{"fake-reviewer"}}}, reviewer: "agent"},
		"only built-ins":               {reviewer: "claude"},
	} {
		t.Run(name, func(t *testing.T) {
			cfg := testConfig()
			cfg.Reviewers = test.reviewers
			setup, err := newRepositorySetup(cfg, "acme/repo", installedAgents(fakeLookPath("claude")))
			if err != nil {
				t.Fatal(err)
			}
			if setup.repo.Reviewer != test.reviewer {
				t.Fatalf("default reviewer %q, want %q", setup.repo.Reviewer, test.reviewer)
			}
		})
	}
}

// A custom command needs no built-in agent CLI, so the install hint must stay
// hidden while setup selects one.
func TestSetupHidesTheInstallHintForACustomCommand(t *testing.T) {
	cfg := testConfig()
	cfg.Reviewers = []config.Reviewer{{ID: "agent", Command: []string{"fake-reviewer"}}}
	setup, err := newRepositorySetup(cfg, "acme/repo", installedAgents(fakeLookPath()))
	if err != nil {
		t.Fatal(err)
	}
	if setup.repo.Reviewer != "agent" {
		t.Fatalf("default reviewer %q, want the custom command", setup.repo.Reviewer)
	}
	m := panelModel(t)
	m.region.setup = setup
	if view := stripANSI(m.View()); strings.Contains(view, noAgentHint) {
		t.Fatalf("install hint appeared for a custom command:\n%s", view)
	}
	// The same setup shows the hint once a built-in reviewer is selected.
	setup.repo.Reviewer = "pi"
	if view := stripANSI(m.View()); !strings.Contains(view, noAgentHint) {
		t.Fatalf("install hint missing for a built-in reviewer:\n%s", view)
	}
}

// The install hint tells the user to install an agent CLI. Setup must show it
// only for a reviewer whose agent CLI it probed and did not find.
func TestInstallHintOnlyCoversAProbedReviewer(t *testing.T) {
	for name, test := range map[string]struct {
		reviewer config.Reviewer
		hint     bool
	}{
		"probed and absent":     {reviewer: config.Reviewer{ID: "pi", Command: []string{"board", "--pi-reviewer"}}, hint: true},
		"custom command":        {reviewer: config.Reviewer{ID: "agent", Command: []string{"fake-reviewer"}}},
		"shared runtime":        {reviewer: config.Reviewer{ID: "hermes", Command: []string{"board", "--hermes-reviewer"}}},
		"selected executable":   {reviewer: config.Reviewer{ID: "claude", Command: []string{"board", "--claude-reviewer", "--claude-executable", "/opt/claude"}}},
		"empty executable kept": {reviewer: config.Reviewer{ID: "codex", Command: []string{"board", "--codex-reviewer", "--codex-executable="}}, hint: true},
	} {
		t.Run(name, func(t *testing.T) {
			cfg := testConfig()
			cfg.Reviewers = []config.Reviewer{test.reviewer}
			cfg.Repositories = []config.Repository{{Name: "acme/repo", Reviewer: test.reviewer.ID}}
			setup, err := newRepositorySetup(cfg, "acme/repo", installedAgents(fakeLookPath()))
			if err != nil {
				t.Fatal(err)
			}
			m := panelModel(t)
			m.region.setup = setup
			view := stripANSI(m.View())
			if strings.Contains(view, noAgentHint) != test.hint {
				t.Fatalf("install hint present=%v, want %v:\n%s", !test.hint, test.hint, view)
			}
		})
	}
}

// Setup takes the first available reviewer in configuration order. When no
// reviewer is available it keeps the first configured reviewer, rather than
// replacing the user's own choice with an equally absent built-in.
func TestSetupFallsBackToTheFirstConfiguredReviewer(t *testing.T) {
	hermes := config.Reviewer{ID: "hermes", Command: []string{"board", "--hermes-reviewer"}}
	for name, test := range map[string]struct {
		installed []string
		reviewer  string
	}{
		"nothing available keeps the configured reviewer": {reviewer: "hermes"},
		"an installed agent wins over an unchecked one":   {installed: []string{"claude"}, reviewer: "claude"},
	} {
		t.Run(name, func(t *testing.T) {
			cfg := testConfig()
			cfg.Reviewers = []config.Reviewer{hermes}
			setup, err := newRepositorySetup(cfg, "acme/repo", installedAgents(fakeLookPath(test.installed...)))
			if err != nil {
				t.Fatal(err)
			}
			if setup.repo.Reviewer != test.reviewer {
				t.Fatalf("default reviewer %q, want %q", setup.repo.Reviewer, test.reviewer)
			}
		})
	}
}

// A custom command names its own program, so setup must not probe PATH for it.
func TestSetupTreatsCustomCommandsAsAvailable(t *testing.T) {
	cfg := testConfig()
	cfg.Reviewers = []config.Reviewer{{ID: "agent", Command: []string{"fake-reviewer"}}}
	cfg.Repositories = []config.Repository{{Name: "acme/repo", Reviewer: "agent"}}
	setup, err := newRepositorySetup(cfg, "acme/repo", installedAgents(fakeLookPath()))
	if err != nil {
		t.Fatal(err)
	}
	if label := setup.rows()[repositoryReviewerRow]; strings.Contains(label, "not installed") {
		t.Fatalf("custom command probed PATH: %q", label)
	}
}

// The settings command uses the replaced probe, so a test controls the
// detected programs without changing PATH.
func TestRepositorySettingsCommandUsesTheReplacedProbe(t *testing.T) {
	m := panelModel(t)
	m.configPath = filepath.Join(t.TempDir(), "config.toml")
	if _, err := config.Load(m.configPath); err != nil {
		t.Fatal(err)
	}
	m.lookPath = fakeLookPath("grok")
	message, ok := m.repositorySettingsCmd()().(repositorySettingsMsg)
	if !ok {
		t.Fatal("settings command returned another message")
	}
	if !message.installed["grok"] || len(message.installed) != 1 {
		t.Fatalf("probe reported %v, want only grok", message.installed)
	}
	next, _, _ := m.updateRepository(message)
	if next.region.setup.repo.Reviewer != "grok" {
		t.Fatalf("panel selected %q, want grok", next.region.setup.repo.Reviewer)
	}
}

// The probe runs inside the settings command, never on the update path, and
// reads PATH through exec.LookPath when no test replaces it.
func TestRepositorySettingsCommandProbesPath(t *testing.T) {
	dir := t.TempDir()
	testutil.Executable(t, dir, "codex")
	t.Setenv("PATH", dir)
	m := panelModel(t)
	m.configPath = filepath.Join(t.TempDir(), "config.toml")
	if _, err := config.Load(m.configPath); err != nil {
		t.Fatal(err)
	}
	message, ok := m.repositorySettingsCmd()().(repositorySettingsMsg)
	if !ok {
		t.Fatal("settings command returned another message")
	}
	if !message.installed["codex"] || len(message.installed) != 1 {
		t.Fatalf("probe reported %v, want only codex", message.installed)
	}
	next, _, _ := m.updateRepository(message)
	if next.region.setup.repo.Reviewer != "codex" {
		t.Fatalf("panel selected %q, want codex", next.region.setup.repo.Reviewer)
	}
}
