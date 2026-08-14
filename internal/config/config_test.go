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

func writeConfigFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadRejectsUnknownFields(t *testing.T) {
	tests := map[string]struct {
		content string
		want    string
	}{
		"top-level key": {
			content: `
extra = "value"
[github]
refresh_interval = "5m"
scopes = ["user:@me"]
[[views]]
id = "all"
title = "All"
query = "is:open"
scope = "configured"
`,
			want: "extra",
		},
		"ui key": {
			content: `
[ui]
title = "Board"
subtitle = "extra"
[github]
refresh_interval = "5m"
scopes = ["user:@me"]
[[views]]
id = "all"
title = "All"
query = "is:open"
scope = "configured"
`,
			want: "subtitle",
		},
		"github key": {
			content: `
[github]
refresh_interval = "5m"
limit_per_scop = 50
scopes = ["user:@me"]
[[views]]
id = "all"
title = "All"
query = "is:open"
scope = "configured"
`,
			want: "limit_per_scop",
		},
		"view key": {
			content: `
[github]
refresh_interval = "5m"
scopes = ["user:@me"]
[[views]]
id = "all"
title = "All"
query = "is:open"
scope = "configured"
color = "green"
`,
			want: "color",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			path := writeConfigFile(t, test.content)
			_, err := Load(path)
			if err == nil {
				t.Fatal("expected unknown-field error")
			}
			if !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error %q does not mention unknown field %q", err, test.want)
			}
		})
	}
}

func TestValidateScopeErrors(t *testing.T) {
	tests := map[string]struct {
		scopes []string
		want   string
	}{
		"empty": {
			scopes: []string{"org:acme", ""},
			want:   "github.scopes[1] is empty",
		},
		"whitespace only": {
			scopes: []string{"org:acme", "   "},
			want:   "github.scopes[1] is empty",
		},
		"malformed": {
			scopes: []string{"team:acme"},
			want:   `github.scopes[0] "team:acme" must start with user:, org:, or repo:`,
		},
		"prefix without target": {
			scopes: []string{"org:"},
			want:   `github.scopes[0] "org:" has no target after "org:"`,
		},
		"duplicate": {
			scopes: []string{"org:acme", "user:@me", "org:acme"},
			want:   `duplicate github scope "org:acme"`,
		},
		"duplicate ignoring case": {
			scopes: []string{"org:acme", "org:ACME"},
			want:   `duplicate github scope "org:ACME"`,
		},
		"duplicate ignoring prefix case": {
			scopes: []string{"org:acme", "ORG:acme"},
			want:   `duplicate github scope "ORG:acme"`,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			cfg := Config{
				GitHub: GitHubConfig{
					RefreshInterval: "5m",
					LimitPerScope:   100,
					MaxConcurrency:  4,
					CIBatchSize:     25,
					Scopes:          test.scopes,
				},
				Views: []View{{ID: "all", Title: "All", Query: "is:open", Scope: ScopeConfigured}},
			}
			err := cfg.Validate()
			if err == nil {
				t.Fatal("expected validation error")
			}
			if !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error %q does not contain %q", err, test.want)
			}
		})
	}
}

func TestCheckReportsValidConfig(t *testing.T) {
	path := writeConfigFile(t, `[ui]
title = "Engineering queue"
[github]
refresh_interval = "10m"
scopes = ["org:acme"]
[[views]]
id = "security"
title = "Security review"
query = 'is:open label:"security"'
scope = "configured"
`)
	if err := Check(path); err != nil {
		t.Fatalf("Check() = %v, want nil", err)
	}
}

func TestCheckRejectsInvalidConfig(t *testing.T) {
	tests := map[string]string{
		"unknown field": `
[ui]
title = "Board"
subtitle = "extra"
[github]
refresh_interval = "5m"
[[views]]
id = "all"
title = "All"
query = "is:open"
scope = "configured"
`,
		"empty scope": `
[github]
refresh_interval = "5m"
scopes = ["org:acme", ""]
[[views]]
id = "all"
title = "All"
query = "is:open"
scope = "configured"
`,
		"duplicate scope": `
[github]
refresh_interval = "5m"
scopes = ["org:acme", "org:acme"]
[[views]]
id = "all"
title = "All"
query = "is:open"
scope = "configured"
`,
	}
	for name, content := range tests {
		t.Run(name, func(t *testing.T) {
			path := writeConfigFile(t, content)
			if err := Check(path); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestCheckDoesNotCreateMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.toml")
	err := Check(path)
	if err == nil {
		t.Fatal("expected error for missing config")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("error = %q, want missing-file message", err)
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatal("Check created a config file for a missing path")
	}
}
