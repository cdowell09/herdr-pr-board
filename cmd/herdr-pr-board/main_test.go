package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultConfigPathReportsMissingUserConfigDirectory(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_CONFIG_DIR", "")
	t.Setenv("HOME", "")

	path, err := defaultConfigPath()
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
	t.Setenv("HERDR_PLUGIN_CONFIG_DIR", "/tmp/pr-board-config")

	path, err := defaultConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join("/tmp/pr-board-config", "config.toml")
	if path != want {
		t.Fatalf("defaultConfigPath() = %q, want %q", path, want)
	}
}
