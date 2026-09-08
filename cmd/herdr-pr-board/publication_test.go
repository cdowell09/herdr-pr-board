package main

import (
	"bytes"
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/cdowell09/herdr-pr-board/internal/config"
)

func TestRepositorySettingsCommandPreservesUnspecifiedPermissions(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	t.Setenv("PATH", t.TempDir()) // Setup does not need GitHub CLI.
	path := writeConfig(t, validConfigTOML+"\n# Preserve custom reviewer arguments.\n[[reviewers]]\nid = \"agent\"\ncommand = [\"agent\",\"custom argument\"]\n")
	var stdout, stderr bytes.Buffer
	args := []string{"--config", path, "--repository-settings", "acme/api", "--set-reviewer", "agent", "--auto-launch=true", "--publish-actions", "comment"}
	if code := run(args, &stdout, &stderr); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, &stderr)
	}
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"--config", path, "--repository-settings", "acme/api", "--auto-launch=false"}, &stdout, &stderr); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, &stderr)
	}
	var repo config.Repository
	if err := json.Unmarshal(stdout.Bytes(), &repo); err != nil {
		t.Fatal(err)
	}
	if repo.AutoLaunch || len(repo.PublishActions) != 1 || repo.Reviewer != "agent" {
		t.Fatalf("%+v", repo)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "# Preserve custom reviewer arguments.") {
		t.Fatal("lost configuration comment")
	}
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"--publication-history", "https://github.com/acme/api/pull/7"}, &stdout, &stderr); code != 0 || !strings.Contains(stdout.String(), `"attempts":[]`) {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, &stdout, &stderr)
	}
}

func TestPublicationCommandUsageRequiresExplicitActionAndRun(t *testing.T) {
	for _, args := range [][]string{
		{"--publish", "https://github.com/acme/api/pull/7"},
		{"--publish", "https://github.com/acme/api/pull/7", "--run", "id", "--action", "unknown"},
		{"--publish", ""}, {"--repository-settings", ""}, {"--auto-launch=true"},
		{"--repository-settings", "acme/api", "--json"}, {"--publication-history", "https://github.com/acme/api/pull/7", "--review", "https://github.com/acme/api/pull/7"},
		{"--repository-settings", "acme/api", "--use-pi-reviewer", "--set-reviewer", "agent"},
	} {
		var output bytes.Buffer
		if code := run(args, &output, &output); code != 2 {
			t.Fatalf("%v: code=%d output=%s", args, code, &output)
		}
	}
}

func TestRepositorySettingsRevokesAutomaticPublication(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	path := writeConfig(t, validConfigTOML+"\n[[reviewers]]\nid = \"agent\"\ncommand = [\"agent\"]\n")
	var stdout, stderr bytes.Buffer
	base := []string{"--config", path, "--repository-settings", "acme/api"}
	if code := run(append(base, "--set-reviewer", "agent", "--publish-actions", "comment", "--auto-publish", "comment"), &stdout, &stderr); code != 0 {
		t.Fatalf("setup: %d %s", code, &stderr)
	}
	stdout.Reset()
	if code := run(append(base, "--publish-actions", ""), &stdout, &stderr); code != 0 {
		t.Fatalf("revoke: %d %s", code, &stderr)
	}
	cfg, err := config.LoadExisting(path)
	if err != nil {
		t.Fatal(err)
	}
	repo, _ := cfg.RepositoryFor("acme/api")
	if len(repo.PublishActions) != 0 || repo.AutoPublish != "" {
		t.Fatalf("authority remains: %+v", repo)
	}
	if code := run(append(base, "--publish-actions", "", "--auto-publish", "comment"), &stdout, &stderr); code == 0 {
		t.Fatal("explicit unauthorized selector accepted")
	}
}

func TestBuiltinRepositorySetupAddsOnlySelectedReviewer(t *testing.T) {
	for _, name := range []string{"pi", "codex", "claude", "qwen", "omp", "qodercli", "kimi"} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
			t.Setenv("PATH", "")
			original := validConfigTOML + "\n# Preserve custom reviewer.\n[[reviewers]]\nid = \"custom\"\ncommand = [\"custom-program\", \"literal argument\"]\n"
			path := writeConfig(t, original)
			args := []string{"--config", path, "--repository-settings", "acme/repo", "--use-" + name + "-reviewer"}
			var output bytes.Buffer
			if code := run(args, &output, &output); code != 0 {
				t.Fatalf("code=%d output=%s", code, &output)
			}
			cfg, err := config.LoadExisting(path)
			if err != nil {
				t.Fatal(err)
			}
			repo, _ := cfg.RepositoryFor("acme/repo")
			if len(cfg.Reviewers) != 2 || repo.Reviewer != name || repo.AutoLaunch || len(repo.PublishActions) != 0 || repo.AutoPublish != "" {
				t.Fatalf("unexpected setup: reviewers=%+v repository=%+v", cfg.Reviewers, repo)
			}
			binary, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			want := []string{binary, "--" + name + "-reviewer"}
			if !slices.Equal(cfg.Reviewers[1].Command, want) {
				t.Fatalf("command=%v want=%v", cfg.Reviewers[1].Command, want)
			}
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.HasPrefix(before, []byte(original)) {
				t.Fatal("rewrote existing configuration")
			}
			output.Reset()
			if code := run(args, &output, &output); code != 1 || !strings.Contains(output.String(), "already exists") {
				t.Fatalf("duplicate reviewer accepted: code=%d output=%s", code, &output)
			}
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) {
				t.Fatal("duplicate setup changed configuration")
			}
			for _, conflicting := range []string{"--set-reviewer=custom", "--use-pi-reviewer", "--use-codex-reviewer", "--use-claude-reviewer"} {
				if conflicting == "--use-"+name+"-reviewer" {
					continue
				}
				if _, err := parseOptions(append(args, conflicting), &bytes.Buffer{}); err == nil {
					t.Fatalf("accepted conflicting %s", conflicting)
				}
			}
			if _, err := parseOptions([]string{"--use-" + name + "-reviewer"}, &bytes.Buffer{}); err == nil {
				t.Fatal("setup flag accepted outside setup")
			}
		})
	}
}
