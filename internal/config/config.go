package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
)

const DefaultFile = `[ui]
title = "Pull Requests"

[github]
refresh_interval = "5m"
limit_per_scope = 100
max_concurrency = 4
ci_batch_size = 25
scopes = ["user:@me"]

[[views]]
id = "authored"
title = "Opened by me"
query = "is:open author:@me"
scope = "global"

[[views]]
id = "review"
title = "Review requested"
query = "is:open review-requested:@me"
scope = "global"

[[views]]
id = "all"
title = "All open"
query = "is:open"
scope = "configured"
`

var idPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)

type Config struct {
	UI     UIConfig     `toml:"ui"`
	GitHub GitHubConfig `toml:"github"`
	Views  []View       `toml:"views"`
}

type UIConfig struct {
	Title string `toml:"title"`
}

type GitHubConfig struct {
	RefreshInterval string   `toml:"refresh_interval"`
	LimitPerScope   int      `toml:"limit_per_scope"`
	MaxConcurrency  int      `toml:"max_concurrency"`
	CIBatchSize     int      `toml:"ci_batch_size"`
	Scopes          []string `toml:"scopes"`
}

type View struct {
	ID    string `toml:"id"`
	Title string `toml:"title"`
	Query string `toml:"query"`
	Scope string `toml:"scope"`
}

func Load(path string) (Config, error) {
	if path == "" {
		return Config{}, errors.New("config path is required")
	}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return Config{}, fmt.Errorf("create config directory: %w", err)
		}
		if err := os.WriteFile(path, []byte(DefaultFile), 0o644); err != nil {
			return Config{}, fmt.Errorf("write default config: %w", err)
		}
	} else if err != nil {
		return Config{}, fmt.Errorf("stat config: %w", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}
	var cfg Config
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}
	applyDefaults(&cfg)
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func applyDefaults(cfg *Config) {
	if strings.TrimSpace(cfg.UI.Title) == "" {
		cfg.UI.Title = "Pull Requests"
	}
	if strings.TrimSpace(cfg.GitHub.RefreshInterval) == "" {
		cfg.GitHub.RefreshInterval = "5m"
	}
	if cfg.GitHub.LimitPerScope == 0 {
		cfg.GitHub.LimitPerScope = 100
	}
	if cfg.GitHub.MaxConcurrency == 0 {
		cfg.GitHub.MaxConcurrency = 4
	}
	if cfg.GitHub.CIBatchSize == 0 {
		cfg.GitHub.CIBatchSize = 25
	}
}

func (c Config) Validate() error {
	if c.GitHub.LimitPerScope < 1 || c.GitHub.LimitPerScope > 1000 {
		return errors.New("github.limit_per_scope must be between 1 and 1000")
	}
	if c.GitHub.MaxConcurrency < 1 || c.GitHub.MaxConcurrency > 8 {
		return errors.New("github.max_concurrency must be between 1 and 8")
	}
	if c.GitHub.CIBatchSize < 1 || c.GitHub.CIBatchSize > 50 {
		return errors.New("github.ci_batch_size must be between 1 and 50")
	}
	if _, err := c.RefreshEvery(); err != nil {
		return err
	}
	if len(c.Views) == 0 {
		return errors.New("at least one [[views]] entry is required")
	}

	seen := make(map[string]struct{}, len(c.Views))
	for i, view := range c.Views {
		prefix := fmt.Sprintf("views[%d]", i)
		if !idPattern.MatchString(view.ID) {
			return fmt.Errorf("%s.id must match %s", prefix, idPattern)
		}
		if _, exists := seen[view.ID]; exists {
			return fmt.Errorf("duplicate view id %q", view.ID)
		}
		seen[view.ID] = struct{}{}
		if strings.TrimSpace(view.Title) == "" {
			return fmt.Errorf("%s.title is required", prefix)
		}
		if strings.TrimSpace(view.Query) == "" {
			return fmt.Errorf("%s.query is required", prefix)
		}
		if view.Scope != "global" && view.Scope != "configured" {
			return fmt.Errorf("%s.scope must be global or configured", prefix)
		}
		if view.Scope == "configured" && len(c.GitHub.Scopes) == 0 {
			return fmt.Errorf("%s uses configured scope but github.scopes is empty", prefix)
		}
	}
	for i, scope := range c.GitHub.Scopes {
		if !validScope(scope) {
			return fmt.Errorf("github.scopes[%d] must start with user:, org:, or repo:", i)
		}
	}
	return nil
}

func validScope(scope string) bool {
	for _, prefix := range []string{"user:", "org:", "repo:"} {
		if strings.HasPrefix(scope, prefix) && len(scope) > len(prefix) {
			return true
		}
	}
	return false
}

func (c Config) RefreshEvery() (time.Duration, error) {
	value := strings.TrimSpace(c.GitHub.RefreshInterval)
	if value == "0" {
		return 0, nil
	}
	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("github.refresh_interval: %w", err)
	}
	if duration < time.Minute {
		return 0, errors.New("github.refresh_interval must be 0 or at least 1m")
	}
	return duration, nil
}
