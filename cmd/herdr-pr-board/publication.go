package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/cdowell09/herdr-pr-board/internal/config"
	"github.com/cdowell09/herdr-pr-board/internal/localstate"
	"github.com/cdowell09/herdr-pr-board/internal/publication"
)

type publicationOptions struct {
	repository, publish, history, runID, action, reviewer, actions, autoPublish string
	autoLaunch, autoSet, actionsSet, autoPublishSet                             bool
	builtins                                                                    map[string]*bool
}

func addPublicationFlags(flags *flag.FlagSet) *publicationOptions {
	p := &publicationOptions{builtins: map[string]*bool{}}
	flags.StringVar(&p.repository, "repository-settings", "", "configure owner/repository without a terminal")
	flags.StringVar(&p.reviewer, "set-reviewer", "", "reviewer ID for repository settings")
	for _, builtin := range config.BuiltinReviewers("") {
		p.builtins[builtin.ID] = flags.Bool("use-"+builtin.ID+"-reviewer", false, "add the built-in "+builtin.ID+" reviewer during repository setup")
	}
	flags.BoolVar(&p.autoLaunch, "auto-launch", false, "allow automatic repository reviews (true or false)")
	flags.StringVar(&p.autoPublish, "auto-publish", "", "automatic publication action; empty keeps findings local")
	flags.StringVar(&p.actions, "publish-actions", "", "comma-separated allowed actions; empty keeps findings local")
	flags.StringVar(&p.publish, "publish", "", "publish a completed local review for this PR URL")
	flags.StringVar(&p.runID, "run", "", "completed review run ID to publish")
	flags.StringVar(&p.action, "action", "", "publication action: comment, approve, or request_changes")
	flags.StringVar(&p.history, "publication-history", "", "print local publication attempts for this PR URL")
	return p
}

func (p *publicationOptions) validate(flags *flag.FlagSet) error {
	var emptyMode bool
	flags.Visit(func(f *flag.Flag) {
		if (f.Name == "repository-settings" || f.Name == "publish" || f.Name == "publication-history") && f.Value.String() == "" {
			emptyMode = true
		}
	})
	if emptyMode {
		return errors.New("publication mode requires a nonempty repository or PR URL")
	}
	flags.Visit(func(f *flag.Flag) {
		if f.Name == "auto-publish" {
			p.autoPublishSet = true
		}
		if f.Name == "auto-launch" {
			p.autoSet = true
		}
		if f.Name == "publish-actions" {
			p.actionsSet = true
		}
	})
	if p.repository == "" && (p.reviewer != "" || p.builtin() != "" || p.autoSet || p.actionsSet || p.autoPublishSet) {
		return errors.New("repository settings flags require --repository-settings")
	}
	selections := 0
	if p.reviewer != "" {
		selections++
	}
	for _, enabled := range p.builtins {
		if *enabled {
			selections++
		}
	}
	if selections > 1 {
		return errors.New("choose one reviewer with --set-reviewer or one --use-AGENT-reviewer flag")
	}
	if p.publish != "" {
		if p.runID == "" || !config.PublicationAction(p.action).Valid() {
			return errors.New("--publish requires --run and --action comment, approve, or request_changes")
		}
	} else if p.runID != "" || p.action != "" {
		return errors.New("--run and --action require --publish")
	}
	return nil
}

func (p *publicationOptions) modes() int {
	count := 0
	for _, value := range []string{p.repository, p.publish, p.history} {
		if value != "" {
			count++
		}
	}
	return count
}

func configureRepository(path string, p *publicationOptions, stdout, stderr io.Writer) int {
	cfg, err := config.LoadExisting(path)
	if err != nil {
		return fail(stderr, err)
	}
	original, exists := cfg.RepositoryFor(p.repository)
	repo := original
	var expected *config.Repository
	if exists {
		expected = &original
	}
	if p.reviewer != "" {
		repo.Reviewer = p.reviewer
	}
	var builtin *config.Reviewer
	if id := p.builtin(); id != "" {
		binary, err := os.Executable()
		if err != nil {
			return fail(stderr, err)
		}
		repo.Reviewer = id
		for _, r := range cfg.Reviewers {
			if r.ID == id {
				return fail(stderr, fmt.Errorf("reviewer %s already exists; select it with --set-reviewer %s", id, id))
			}
		}
		for _, r := range config.BuiltinReviewers(binary) {
			if r.ID == id {
				builtin = &r
				break
			}
		}
	}
	if p.autoSet {
		repo.AutoLaunch = p.autoLaunch
	}
	if p.actionsSet {
		var actions []config.PublicationAction
		if p.actions != "" {
			for _, action := range strings.Split(p.actions, ",") {
				actions = append(actions, config.PublicationAction(strings.TrimSpace(action)))
			}
		}
		repo.SetPublishActions(actions)
	}
	if p.autoPublishSet {
		repo.AutoPublish = config.PublicationAction(p.autoPublish)
	}
	dir, err := localstate.Dir()
	if err != nil {
		return fail(stderr, err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	var reviewerEdit *config.ReviewerEdit
	if builtin != nil {
		reviewerEdit = &config.ReviewerEdit{Value: *builtin}
	}
	if _, err := config.SaveRepository(ctx, path, dir, repo, reviewerEdit, expected, nil); err != nil {
		return fail(stderr, err)
	}
	if err := json.NewEncoder(stdout).Encode(repo); err != nil {
		return fail(stderr, err)
	}
	return 0
}

func printPublication(p *publicationOptions, service *publication.Service, stdout, stderr io.Writer) int {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	attempt, err := service.Publish(ctx, p.publish, p.runID, config.PublicationAction(p.action))
	if attempt.ID != "" {
		if writeErr := json.NewEncoder(stdout).Encode(struct {
			Version int                 `json:"version"`
			Attempt publication.Attempt `json:"attempt"`
		}{1, attempt}); writeErr != nil {
			return fail(stderr, writeErr)
		}
	}
	if err != nil {
		return fail(stderr, err)
	}
	return 0
}

func printPublicationHistory(prURL string, stdout, stderr io.Writer) int {
	dir, err := localstate.Dir()
	if err != nil {
		return fail(stderr, err)
	}
	service, err := publication.New(dir, "", nil)
	if err != nil {
		return fail(stderr, err)
	}
	attempts, err := service.History(prURL)
	if err != nil {
		return fail(stderr, err)
	}
	if err := json.NewEncoder(stdout).Encode(struct {
		Version  int                   `json:"version"`
		Attempts []publication.Attempt `json:"attempts"`
	}{1, attempts}); err != nil {
		return fail(stderr, fmt.Errorf("write publication history: %w", err))
	}
	return 0
}

func (p *publicationOptions) builtin() string {
	for id, enabled := range p.builtins {
		if *enabled {
			return id
		}
	}
	return ""
}
