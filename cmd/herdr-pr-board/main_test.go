package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultConfigPathReportsMissingUserConfigDirectory(t *testing.T) {
	path, err := resolveDefaultConfigPath("", func() (string, error) {
		return "", errors.New("config directory unavailable")
	})
	if err == nil {
		t.Fatalf("defaultConfigPath() = %q, nil; want an error", path)
	}
	if path != "" {
		t.Fatalf("defaultConfigPath() returned unsafe relative path %q", path)
	}
	if !strings.Contains(err.Error(), "user config directory") {
		t.Fatalf("error = %q", err)
	}
}

func TestDefaultConfigPathUsesHerdrPluginConfigDirectory(t *testing.T) {
	path, err := resolveDefaultConfigPath("/tmp/pr-board-config", func() (string, error) {
		t.Fatal("user config directory resolver was called")
		return "", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join("/tmp/pr-board-config", "config.toml")
	if path != want {
		t.Fatalf("defaultConfigPath() = %q, want %q", path, want)
	}
}

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

const validConfigTOML = `[ui]
title = "Board"
[github]
refresh_interval = "5m"
scopes = ["org:acme"]
[[views]]
id = "all"
title = "All"
query = "is:open"
scope = "configured"
`

func TestRunValidateValidConfig(t *testing.T) {
	path := writeConfig(t, validConfigTOML)
	var stdout, stderr bytes.Buffer
	code := run([]string{"-config", path, "-validate"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run() = %d, want 0; stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "valid") {
		t.Fatalf("stdout = %q, want validation success", stdout.String())
	}
}

func TestRunValidateInvalidConfig(t *testing.T) {
	path := writeConfig(t, `[github]
refresh_interval = "5m"
scopes = ["org:acme", "org:acme"]
[[views]]
id = "all"
title = "All"
query = "is:open"
scope = "configured"
`)
	var stdout, stderr bytes.Buffer
	code := run([]string{"-config", path, "-validate"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("run() = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "duplicate") {
		t.Fatalf("stderr = %q, want duplicate-scope error", stderr.String())
	}
	if stdout.String() != "" {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
}

func TestRunValidateMissingConfigFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.toml")
	var stdout, stderr bytes.Buffer
	code := run([]string{"-config", path, "-validate"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("run() = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "not found") {
		t.Fatalf("stderr = %q, want missing-file error", stderr.String())
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("validate created a config file for a missing path")
	}
}

func TestRunValidateDoesNotRequireGh(t *testing.T) {
	t.Setenv("PATH", "")
	path := writeConfig(t, validConfigTOML)
	var stdout, stderr bytes.Buffer
	code := run([]string{"-config", path, "-validate"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run() = %d, want 0; stderr = %q", code, stderr.String())
	}
}

func TestRunUnknownFlagExitsWithUsageError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"-bogus"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("run() = %d, want 2", code)
	}
}
