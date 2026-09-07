package config

import (
	"errors"
	"fmt"
	"github.com/cdowell09/herdr-pr-board/internal/reviewmemory"
	"strings"
	"time"
)

type ReviewConfig struct {
	MaxConcurrency int    `toml:"max_concurrency"`
	Timeout        string `toml:"timeout"`
}

type Reviewer struct {
	ID      string   `toml:"id"`
	Command []string `toml:"command"`
}

type Repository struct {
	Name     string `toml:"name"`
	Reviewer string `toml:"reviewer"`
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
	}
	repositories := map[string]bool{}
	for _, repo := range c.Repositories {
		name := strings.ToLower(repo.Name)
		if reviewmemory.ValidateRepository(name) != nil || repositories[name] {
			return errors.New("repository names must use unique owner/name values")
		}
		repositories[name] = true
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
