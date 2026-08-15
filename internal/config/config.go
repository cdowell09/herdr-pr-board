package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
)

type ScopeMode string

const (
	ScopeGlobal     ScopeMode = "global"
	ScopeConfigured ScopeMode = "configured"

	defaultTitle           = "Pull Requests"
	defaultRefreshInterval = "5m"
	defaultLimitPerScope   = 100
	defaultMaxConcurrency  = 4
	defaultCIBatchSize     = 25
	defaultSidebarTTL      = "15m"
	defaultSidebarReview   = "review"

	defaultFileTemplate = `[ui]
title = %q

[github]
refresh_interval = %q
limit_per_scope = %d
max_concurrency = %d
ci_batch_size = %d
scopes = ["user:@me"]

[[views]]
id = "authored"
title = "Opened by me"
query = "is:open author:@me"
scope = %q

[[views]]
id = "review"
title = "Review requested"
query = "is:open review-requested:@me"
scope = %q

[[views]]
id = "all"
title = "All open"
query = "is:open"
scope = %q

[sidebar]
enabled = true
ttl = %q
review_view = %q
`
)

var (
	DefaultFile = fmt.Sprintf(
		defaultFileTemplate,
		defaultTitle,
		defaultRefreshInterval,
		defaultLimitPerScope,
		defaultMaxConcurrency,
		defaultCIBatchSize,
		ScopeGlobal,
		ScopeGlobal,
		ScopeConfigured,
		defaultSidebarTTL,
		defaultSidebarReview,
	)
	idPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)
)

type Config struct {
	UI      UIConfig      `toml:"ui"`
	GitHub  GitHubConfig  `toml:"github"`
	Sidebar SidebarConfig `toml:"sidebar"`
	Views   []View        `toml:"views"`
}

// SidebarConfig controls reporting PR counts into Herdr sidebar tokens.
type SidebarConfig struct {
	// Enabled defaults to true when the field is absent.
	Enabled *bool `toml:"enabled"`
	// TTL is how long reported tokens stay visible after the last report.
	TTL string `toml:"ttl"`
	// ReviewView is the view whose PR count reports as the prs_review token.
	ReviewView string `toml:"review_view"`
}

// SidebarEnabled reports whether sidebar reporting is enabled.
func (s SidebarConfig) SidebarEnabled() bool {
	return s.Enabled == nil || *s.Enabled
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
	ID    string    `toml:"id"`
	Title string    `toml:"title"`
	Query string    `toml:"query"`
	Scope ScopeMode `toml:"scope"`
}

func (v View) SearchRequestCount(configuredScopes, limitPerScope int) int {
	queries := 1
	if v.Scope == ScopeConfigured {
		queries = configuredScopes
	}
	pagesPerQuery := (limitPerScope + 99) / 100
	return queries * pagesPerQuery
}

func (c Config) SearchRequestCount() int {
	count := 0
	for _, view := range c.Views {
		count += view.SearchRequestCount(len(c.GitHub.Scopes), c.GitHub.LimitPerScope)
	}
	return count
}

// Equal reports whether two configurations have the same effective settings.
func (c Config) Equal(other Config) bool {
	if c.Sidebar.SidebarEnabled() != other.Sidebar.SidebarEnabled() {
		return false
	}
	c.Sidebar.Enabled = nil
	other.Sidebar.Enabled = nil
	return reflect.DeepEqual(c, other)
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
	return parseFile(path)
}

// LoadExisting parses and validates an existing configuration without creating it.
func LoadExisting(path string) (Config, error) {
	if path == "" {
		return Config{}, errors.New("config path is required")
	}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return Config{}, fmt.Errorf("config file not found: %s", path)
	} else if err != nil {
		return Config{}, fmt.Errorf("stat config: %w", err)
	}
	return parseFile(path)
}

// Check parses and validates an existing configuration without creating it.
func Check(path string) error {
	_, err := LoadExisting(path)
	return err
}

func parseFile(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}
	var cfg Config
	if err := decodeStrict(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}
	applyDefaults(&cfg)
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func decodeStrict(data []byte, cfg *Config) error {
	decoder := toml.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	err := decoder.Decode(cfg)
	if err == nil {
		return nil
	}
	var strictErr *toml.StrictMissingError
	if errors.As(err, &strictErr) {
		return fmt.Errorf("unknown setting(s):\n%s", strictErr.String())
	}
	return err
}

func applyDefaults(cfg *Config) {
	if strings.TrimSpace(cfg.UI.Title) == "" {
		cfg.UI.Title = defaultTitle
	}
	if strings.TrimSpace(cfg.GitHub.RefreshInterval) == "" {
		cfg.GitHub.RefreshInterval = defaultRefreshInterval
	}
	if cfg.GitHub.LimitPerScope == 0 {
		cfg.GitHub.LimitPerScope = defaultLimitPerScope
	}
	if cfg.GitHub.MaxConcurrency == 0 {
		cfg.GitHub.MaxConcurrency = defaultMaxConcurrency
	}
	if cfg.GitHub.CIBatchSize == 0 {
		cfg.GitHub.CIBatchSize = defaultCIBatchSize
	}
	if strings.TrimSpace(cfg.Sidebar.TTL) == "" {
		cfg.Sidebar.TTL = defaultSidebarTTL
	}
	if strings.TrimSpace(cfg.Sidebar.ReviewView) == "" {
		cfg.Sidebar.ReviewView = defaultSidebarReview
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
	if c.Sidebar.ReviewView != "" && !idPattern.MatchString(c.Sidebar.ReviewView) {
		return fmt.Errorf("sidebar.review_view must match %s", idPattern)
	}
	if _, err := c.Sidebar.TTLEvery(); err != nil {
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
		if view.Scope != ScopeGlobal && view.Scope != ScopeConfigured {
			return fmt.Errorf("%s.scope must be global or configured", prefix)
		}
		if view.Scope == ScopeConfigured && len(c.GitHub.Scopes) == 0 {
			return fmt.Errorf("%s uses configured scope but github.scopes is empty", prefix)
		}
	}
	if err := validateScopes(c.GitHub.Scopes); err != nil {
		return err
	}
	return nil
}

func validateScopes(scopes []string) error {
	seen := make(map[string]int, len(scopes))
	for i, scope := range scopes {
		if strings.TrimSpace(scope) == "" {
			return fmt.Errorf("github.scopes[%d] is empty", i)
		}
		prefix := scopePrefix(scope)
		if prefix == "" {
			return fmt.Errorf("github.scopes[%d] %q must start with user:, org:, or repo:", i, scope)
		}
		if len(scope) == len(prefix) {
			return fmt.Errorf("github.scopes[%d] %q has no target after %q", i, scope, prefix)
		}
		key := strings.ToLower(scope)
		if prev, exists := seen[key]; exists {
			return fmt.Errorf("duplicate github scope %q (github.scopes[%d] and github.scopes[%d])", scope, prev, i)
		}
		seen[key] = i
	}
	return nil
}

func scopePrefix(scope string) string {
	for _, prefix := range []string{"user:", "org:", "repo:"} {
		if len(scope) >= len(prefix) && strings.EqualFold(scope[:len(prefix)], prefix) {
			return scope[:len(prefix)]
		}
	}
	return ""
}

// TTLEvery parses the sidebar token lifetime. It mirrors RefreshEvery:
// "0" disables expiry, otherwise the value must be at least 1m.
// An empty value means the default applies and reports no TTL.
func (s SidebarConfig) TTLEvery() (time.Duration, error) {
	value := strings.TrimSpace(s.TTL)
	if value == "" || value == "0" {
		return 0, nil
	}
	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("sidebar.ttl: %w", err)
	}
	if duration < time.Minute {
		return 0, errors.New("sidebar.ttl must be 0 or at least 1m")
	}
	return duration, nil
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
