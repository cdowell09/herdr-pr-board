package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/cdowell09/herdr-pr-board/internal/board"
	"github.com/cdowell09/herdr-pr-board/internal/config"
	gh "github.com/cdowell09/herdr-pr-board/internal/github"
	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	defaultPath, defaultPathErr := defaultConfigPath()
	configPath := flag.String("config", defaultPath, "path to config.toml")
	flag.Parse()
	if *configPath == "" && defaultPathErr != nil {
		fatal(defaultPathErr.Error())
	}

	if _, err := exec.LookPath("gh"); err != nil {
		fatal("GitHub CLI (gh) is required and must be on PATH")
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		fatal(err.Error())
	}

	client := gh.NewClient(gh.ExecRunner{}, cfg.GitHub)
	service := board.NewService(cfg, client)
	model, err := board.NewModel(cfg, service)
	if err != nil {
		fatal(err.Error())
	}
	if _, err := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion()).Run(); err != nil {
		fatal(err.Error())
	}
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

func fatal(message string) {
	fmt.Fprintln(os.Stderr, "herdr-pr-board:", message)
	os.Exit(1)
}
