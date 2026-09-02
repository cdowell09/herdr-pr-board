package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/cdowell09/herdr-pr-board/internal/board"
	"github.com/cdowell09/herdr-pr-board/internal/config"
	gh "github.com/cdowell09/herdr-pr-board/internal/github"
	"github.com/cdowell09/herdr-pr-board/internal/sidebar"
	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("herdr-pr-board", flag.ContinueOnError)
	flags.SetOutput(stderr)
	configPath := flags.String("config", "", "path to config.toml")
	validateOnly := flags.Bool("validate", false, "validate the configuration and exit")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *configPath == "" {
		path, err := defaultConfigPath()
		if err != nil {
			return fail(stderr, err)
		}
		*configPath = path
	}

	if *validateOnly {
		if err := config.Check(*configPath); err != nil {
			return fail(stderr, err)
		}
		fmt.Fprintf(stdout, "configuration is valid: %s\n", *configPath)
		return 0
	}

	if _, err := exec.LookPath("gh"); err != nil {
		return fail(stderr, errors.New("GitHub CLI (gh) is required and must be on PATH"))
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		return fail(stderr, err)
	}

	client := gh.NewClient(nil, cfg.GitHub)
	service := board.NewService(cfg, client)
	reporter := sidebar.NewReporter(cfg.Sidebar, os.Getenv("HERDR_WORKSPACE_ID"), os.Getenv("HERDR_BIN_PATH"))
	model, err := board.NewModel(cfg, service, reporter)
	if err != nil {
		return fail(stderr, err)
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
