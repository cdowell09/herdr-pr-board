package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/cdowell09/herdr-pr-board/internal/board"
	"github.com/cdowell09/herdr-pr-board/internal/config"
	"github.com/cdowell09/herdr-pr-board/internal/discovery"
	"github.com/cdowell09/herdr-pr-board/internal/dispatch"
	gh "github.com/cdowell09/herdr-pr-board/internal/github"
	"github.com/cdowell09/herdr-pr-board/internal/localstate"
	"github.com/cdowell09/herdr-pr-board/internal/monitor"
	"github.com/cdowell09/herdr-pr-board/internal/publication"
	"github.com/cdowell09/herdr-pr-board/internal/review"
	"github.com/cdowell09/herdr-pr-board/internal/sidebar"
	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	o, err := parseOptions(args, stderr)
	if err != nil {
		fmt.Fprintln(stderr, "herdr-pr-board:", err)
		return 2
	}
	if o.pi {
		return runPiAdapter(o, os.Stdin, stderr)
	}
	if o.publication.history != "" {
		return printPublicationHistory(o.publication.history, stdout, stderr)
	}
	if o.history != "" {
		return printReviewHistory(o.history, stdout, stderr)
	}
	if o.configPath == "" {
		path, err := defaultConfigPath()
		if err != nil {
			return fail(stderr, err)
		}
		o.configPath = path
	}

	if o.validate {
		if err := config.Check(o.configPath); err != nil {
			return fail(stderr, err)
		}
		fmt.Fprintf(stdout, "configuration is valid: %s\n", o.configPath)
		return 0
	}

	cfg, err := config.Load(o.configPath)
	if err != nil {
		return fail(stderr, err)
	}
	if o.publication.repository != "" {
		return configureRepository(o.configPath, o.publication, stdout, stderr)
	}
	if o.view != "" {
		found := false
		for _, view := range cfg.Views {
			if view.ID == o.view {
				cfg.Views = []config.View{view}
				found = true
				break
			}
		}
		if !found {
			fmt.Fprintf(stderr, "herdr-pr-board: unknown view %q\n", o.view)
			return 2
		}
	}
	if _, err := exec.LookPath("gh"); err != nil {
		return fail(stderr, errors.New("GitHub CLI (gh) is required and must be on PATH"))
	}

	client := gh.NewClient(nil, cfg.GitHub)
	client.SetTokenVars(setTokenVars(gh.TokenVars, os.Getenv))
	var service discovery.Loader = discovery.NewService(cfg, client)
	stateDir := os.Getenv("HERDR_PLUGIN_STATE_DIR")
	if o.monitor && stateDir == "" {
		return fail(stderr, errors.New("--monitor requires HERDR_PLUGIN_STATE_DIR"))
	}
	var monitorSource *monitor.Source
	if stateDir != "" {
		stateDir, err = localstate.Dir()
		if err != nil {
			return fail(stderr, err)
		}
		if err := os.MkdirAll(stateDir, 0o700); err != nil {
			return fail(stderr, err)
		}
		source := monitor.New(stateDir, cfg, service)
		monitorSource = source
		service = source
	}
	if o.json {
		return printSnapshot(cfg, service, stdout, stderr)
	}
	var publisher *publication.Service
	if o.publication.publish != "" || stateDir != "" {
		if stateDir == "" {
			return fail(stderr, errors.New("publication requires HERDR_PLUGIN_STATE_DIR"))
		}
		publisher, err = publication.New(stateDir, o.configPath, client)
		if err != nil {
			return fail(stderr, err)
		}
	}
	if o.publication.publish != "" {
		return printPublication(o.publication, publisher, stdout, stderr)
	}
	var reviews *review.Service
	if o.review != "" || stateDir != "" {
		if stateDir == "" {
			return fail(stderr, errors.New("reviews require HERDR_PLUGIN_STATE_DIR"))
		}
		reviews, err = review.New(stateDir, o.configPath, client)
		if err != nil {
			return fail(stderr, err)
		}
	}
	if o.monitor {
		return runMonitor(monitorSource, cfg, dispatch.New(o.configPath, reviews, publisher), stderr)
	}
	if o.eligibility {
		if reviews == nil {
			return fail(stderr, errors.New("review eligibility requires HERDR_PLUGIN_STATE_DIR"))
		}
		return printEligibility(cfg, service, reviews, stdout, stderr)
	}
	if o.review != "" {
		return printReview(o, reviews, publisher, stdout, stderr)
	}

	model, err := board.NewModelWithConfigPath(cfg, o.configPath, service, func(settings config.SidebarConfig) *sidebar.Reporter {
		return sidebar.NewReporter(settings, os.Getenv("HERDR_WORKSPACE_ID"), os.Getenv("HERDR_BIN_PATH"))
	})
	if err != nil {
		return fail(stderr, err)
	}
	binary, err := os.Executable()
	if err != nil {
		return fail(stderr, err)
	}
	model = model.WithMonitorStarter(func() error { return monitor.EnsureRunning(context.Background(), binary, o.configPath, stateDir) })
	reviewCtx, cancelReviews := context.WithCancel(context.Background())
	defer func() {
		cancelReviews()
		if reviews != nil {
			reviews.Wait()
		}
		if publisher != nil {
			publisher.Wait()
		}
	}()
	if reviews != nil {
		model = model.WithReviews(reviewCtx, reviews).WithPublications(stateDir, publisher)
	}
	if _, err := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion()).Run(); err != nil {
		return fail(stderr, err)
	}
	return 0
}

func fail(stderr io.Writer, err error) int {
	fmt.Fprintln(stderr, "herdr-pr-board:", err)
	return 1
}

func defaultConfigPath() (string, error) {
	return resolveDefaultConfigPath(os.Getenv("HERDR_PLUGIN_CONFIG_DIR"), os.UserConfigDir)
}

// setTokenVars returns the names in names whose process environment value,
// read through getenv, is non-empty.
func setTokenVars(names []string, getenv func(string) string) []string {
	var set []string
	for _, name := range names {
		if getenv(name) != "" {
			set = append(set, name)
		}
	}
	return set
}

func resolveDefaultConfigPath(pluginConfigDir string, userConfigDir func() (string, error)) (string, error) {
	if pluginConfigDir != "" {
		return filepath.Join(pluginConfigDir, "config.toml"), nil
	}
	directory, err := userConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user config directory: %w", err)
	}
	return filepath.Join(directory, "herdr", "plugins", "config", "cdowell09.pr-board", "config.toml"), nil
}
