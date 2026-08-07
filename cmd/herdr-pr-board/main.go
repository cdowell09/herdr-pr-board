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
	configPath := flag.String("config", defaultConfigPath(), "path to config.toml")
	flag.Parse()

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
	if _, err := tea.NewProgram(model, tea.WithAltScreen()).Run(); err != nil {
		fatal(err.Error())
	}
}

func defaultConfigPath() string {
	if directory := os.Getenv("HERDR_PLUGIN_CONFIG_DIR"); directory != "" {
		return filepath.Join(directory, "config.toml")
	}
	directory, err := os.UserConfigDir()
	if err != nil {
		return "config.toml"
	}
	return filepath.Join(directory, "herdr", "plugins", "config", "cdowell09.pr-board", "config.toml")
}

func fatal(message string) {
	fmt.Fprintln(os.Stderr, "herdr-pr-board:", message)
	os.Exit(1)
}
