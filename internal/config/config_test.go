package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadCreatesDefaultConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UI.Title != "Pull Requests" {
		t.Fatalf("title = %q", cfg.UI.Title)
	}
	if len(cfg.Views) != 3 {
		t.Fatalf("views = %d, want 3", len(cfg.Views))
	}
	if cfg.Views[0].Title != "Opened by me" || cfg.Views[2].Scope != "configured" {
		t.Fatalf("unexpected default views: %#v", cfg.Views)
	}
	if got, err := cfg.RefreshEvery(); err != nil || got != 5*time.Minute {
		t.Fatalf("refresh = %s, %v", got, err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("default config not written: %v", err)
	}
}

func TestLoadCustomViewTitlesAndQueries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	content := `[ui]
title = "Engineering queue"
[github]
refresh_interval = "10m"
limit_per_scope = 50
max_concurrency = 2
ci_batch_size = 10
scopes = ["org:acme"]
[[views]]
id = "security"
title = "Security review"
query = 'is:open label:"security"'
scope = "configured"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UI.Title != "Engineering queue" || cfg.Views[0].Title != "Security review" {
		t.Fatalf("config titles not preserved: %#v", cfg)
	}
	if cfg.Views[0].Query != `is:open label:"security"` {
		t.Fatalf("query = %q", cfg.Views[0].Query)
	}
}

func TestLoadRejectsInvalidConfig(t *testing.T) {
	tests := map[string]string{
		"duplicate id": `
[github]
refresh_interval = "5m"
scopes = ["user:@me"]
[[views]]
id = "mine"
title = "One"
query = "is:open"
scope = "global"
[[views]]
id = "mine"
title = "Two"
query = "is:open"
scope = "global"
`,
		"bad scope": `
[github]
refresh_interval = "5m"
scopes = ["team:acme"]
[[views]]
id = "all"
title = "All"
query = "is:open"
scope = "configured"
`,
		"too frequent": `
[github]
refresh_interval = "10s"
[[views]]
id = "mine"
title = "Mine"
query = "is:open author:@me"
scope = "global"
`,
	}
	for name, content := range tests {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
			_, err := Load(path)
			if err == nil || strings.TrimSpace(err.Error()) == "" {
				t.Fatal("expected validation error")
			}
		})
	}
}
