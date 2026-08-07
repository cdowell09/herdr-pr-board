package main

import (
	"errors"
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
