package config

import (
	"fmt"
	"slices"
	"strings"
)

type PublicationAction string

const (
	PublishComment        PublicationAction = "comment"
	PublishApprove        PublicationAction = "approve"
	PublishRequestChanges PublicationAction = "request_changes"
)

func PublicationActions() []PublicationAction {
	return []PublicationAction{PublishComment, PublishApprove, PublishRequestChanges}
}

func (action PublicationAction) Valid() bool {
	return action == PublishComment || action == PublishApprove || action == PublishRequestChanges
}

func (repo Repository) validatePermissions() error {
	seen := map[PublicationAction]bool{}
	for _, action := range repo.PublishActions {
		if !action.Valid() || seen[action] {
			return fmt.Errorf("repository %s publication actions must be unique comment, approve, or request_changes values", repo.Name)
		}
		seen[action] = true
	}
	return nil
}

func (c Config) RepositoryFor(name string) (Repository, bool) {
	for _, repo := range c.Repositories {
		if strings.EqualFold(repo.Name, name) {
			return repo, true
		}
	}
	return Repository{Name: name}, false
}

func (c Config) AllowPublication(repository string, action PublicationAction) error {
	repo, exists := c.RepositoryFor(repository)
	if !action.Valid() || !exists || !slices.Contains(repo.PublishActions, action) {
		return fmt.Errorf("repository %s does not allow %s publication; edit repository settings", repository, action)
	}
	return nil
}

// ResolveLaunch keeps manual reviewer selection separate from automatic permission.
func (c Config) ResolveLaunch(repository, reviewerID string, automatic bool) (Reviewer, error) {
	if automatic {
		repo, exists := c.RepositoryFor(repository)
		if !exists || !repo.AutoLaunch {
			return Reviewer{}, fmt.Errorf("repository %s does not allow automatic reviews", repository)
		}
		if reviewerID != "" && reviewerID != repo.Reviewer {
			return Reviewer{}, fmt.Errorf("automatic review must use the configured reviewer for %s", repository)
		}
	}
	return c.ReviewerFor(repository, reviewerID)
}
