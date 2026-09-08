package config

import (
	"errors"
	"fmt"
	"github.com/cdowell09/herdr-pr-board/internal/reviewmemory"
	"strings"
	"time"
)

type ReviewConfig struct {
	AutoViews      []string `toml:"auto_views"`
	MaxConcurrency int      `toml:"max_concurrency"`
	Timeout        string   `toml:"timeout"`
}

type Reviewer struct {
	ID         string   `toml:"id"`
	Command    []string `toml:"command"`
	PromptFile *string  `toml:"prompt_file"`
	SkillFile  *string  `toml:"skill_file"`
}

type Repository struct {
	AutoPublish    PublicationAction   `toml:"auto_publish"`
	Name           string              `toml:"name"`
	Reviewer       string              `toml:"reviewer"`
	AutoLaunch     bool                `toml:"auto_launch"`
	PublishActions []PublicationAction `toml:"publish_actions"`
}

func (r ReviewConfig) TimeoutDuration() (time.Duration, error) {
	value := r.Timeout
	if value == "" {
		value = defaultReviewTimeout
	}
	d, err := time.ParseDuration(value)
	if err != nil || d < time.Second || d > 24*time.Hour {
		return 0, errors.New("review.timeout must be between 1s and 24h")
	}
	return d, nil
}

func (c Config) validateReviews() error {
	knownViews := map[string]bool{}
	for _, view := range c.Views {
		knownViews[view.ID] = true
	}
	selected := map[string]bool{}
	for _, id := range c.Review.AutoViews {
		if !knownViews[id] || selected[id] {
			return fmt.Errorf("review.auto_views must contain unique configured view IDs: %q", id)
		}
		selected[id] = true
	}

	if c.Review.MaxConcurrency < 0 || c.Review.MaxConcurrency > 8 {
		return errors.New("review.max_concurrency must be between 1 and 8")
	}
	if _, err := c.Review.TimeoutDuration(); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, reviewer := range c.Reviewers {
		if !idPattern.MatchString(reviewer.ID) || seen[reviewer.ID] {
			return errors.New("reviewer IDs must be valid and unique")
		}
		seen[reviewer.ID] = true
		if len(reviewer.Command) == 0 || strings.TrimSpace(reviewer.Command[0]) == "" {
			return fmt.Errorf("reviewer %s command is required", reviewer.ID)
		}
		for _, arg := range reviewer.Command {
			if strings.ContainsRune(arg, 0) {
				return fmt.Errorf("reviewer %s command contains NUL", reviewer.ID)
			}
		}
		if reviewer.PromptFile != nil || reviewer.SkillFile != nil {
			if reviewer.Builtin() == "" {
				return fmt.Errorf("reviewer %s instruction files require a built-in adapter", reviewer.ID)
			}
			for _, path := range []*string{reviewer.PromptFile, reviewer.SkillFile} {
				if path != nil && strings.ContainsRune(*path, 0) {
					return fmt.Errorf("reviewer %s instruction path contains NUL", reviewer.ID)
				}
			}
		}
	}
	repositories := map[string]bool{}
	for _, repo := range c.Repositories {
		name := strings.ToLower(repo.Name)
		if reviewmemory.ValidateRepository(name) != nil || repositories[name] {
			return errors.New("repository names must use unique owner/name values")
		}
		repositories[name] = true
		if err := repo.validatePermissions(); err != nil {
			return err
		}
		if !seen[repo.Reviewer] {
			return fmt.Errorf("repository %s references unknown reviewer %q", repo.Name, repo.Reviewer)
		}
	}
	return nil
}

// ReviewerFor resolves an explicit reviewer or the repository's configured reviewer.
func (c Config) ReviewerFor(repository, id string) (Reviewer, error) {
	if id == "" {
		for _, repo := range c.Repositories {
			if strings.EqualFold(repo.Name, repository) {
				id = repo.Reviewer
				break
			}
		}
	}
	for _, reviewer := range c.Reviewers {
		if reviewer.ID == id {
			return reviewer, nil
		}
	}
	return Reviewer{}, fmt.Errorf("configure a reviewer for %s or select --reviewer", repository)
}

// SelectsAutomaticView matches captured definitions against current selected views.
func (c Config) SelectsAutomaticView(observed []View) bool {
	for _, selected := range c.Review.AutoViews {
		for _, current := range c.Views {
			if current.ID != selected {
				continue
			}
			for _, view := range observed {
				if view == current {
					return true
				}
			}
		}
	}
	return false
}
