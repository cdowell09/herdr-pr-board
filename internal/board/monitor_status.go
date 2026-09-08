package board

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/cdowell09/herdr-pr-board/internal/config"
	"github.com/cdowell09/herdr-pr-board/internal/monitor"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

type monitorStatusMsg struct {
	url        string
	generation uint64
	status     monitor.Status
	cfg        config.Config
	command    monitorInvocation
}

func (m Model) monitorStatusCmd() tea.Cmd {
	path, state, url, generation := m.configPath, m.stateDir, m.reviewPanel.pr.URL, m.reviewGeneration
	return func() tea.Msg {
		msg := monitorStatusMsg{url: url, generation: generation}
		cfg, err := config.LoadExisting(path)
		if err != nil {
			msg.status = monitor.Status{State: monitor.Unknown, Message: err.Error()}
			return msg
		}
		msg.cfg = cfg
		msg.status = monitor.Inspect(state, cfg)
		binary, err := os.Executable()
		if err == nil {
			msg.command, err = monitorCommand(binary, path, state)
		}
		if err != nil {
			msg.status.Message += "; monitor command unavailable: " + err.Error()
		}
		return msg
	}
}

// Quote each argument independently so the displayed command is safe to paste.
type monitorInvocation struct{ binary, path, state string }

func monitorCommand(binary, path, state string) (monitorInvocation, error) {
	if !filepath.IsAbs(state) {
		return monitorInvocation{}, fmt.Errorf("absolute state directory is required")
	}
	binary, err := filepath.Abs(binary)
	if err != nil {
		return monitorInvocation{}, err
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return monitorInvocation{}, err
	}
	for _, value := range []string{binary, path, state} {
		if strings.ContainsFunc(value, unicode.IsControl) {
			return monitorInvocation{}, fmt.Errorf("paths with control characters cannot be displayed as a command")
		}
	}
	return monitorInvocation{binary, path, state}, nil
}

func (m Model) monitorCommandLines() []string {
	if m.reviewPanel.monitor.State != monitor.Stopped || m.reviewPanel.monitorCommand.binary == "" {
		return nil
	}
	return m.reviewPanel.monitorCommand.lines(m.width)
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

// Quote known argument chunks independently. Continuations concatenate chunks
// without inserting literal newlines into a path when the user copies the command.
func (command monitorInvocation) lines(width int) []string {
	if width < 12 {
		return []string{truncate("Widen terminal to copy command.", width)}
	}
	args := []string{"env", "HERDR_PLUGIN_STATE_DIR=" + command.state, command.binary, "--monitor", "--config", command.path}
	var lines []string
	for i, arg := range args {
		runes := []rune(arg)
		for len(runes) > 0 {
			n := 0
			for n < len(runes) && ansi.StringWidth(shellQuote(string(runes[:n+1]))) <= width-2 {
				n++
			}
			line := shellQuote(string(runes[:n]))
			runes = runes[n:]
			if len(runes) > 0 {
				line += "\\"
			} else if i < len(args)-1 {
				line += " \\"
			}
			lines = append(lines, line)
		}
	}
	return lines
}

func automaticSetupWait(repo config.Repository, views []string, status monitor.Status) string {
	switch {
	case !repo.AutoLaunch:
		return "enable automatic launches"
	case len(views) == 0:
		return "select global views"
	case status.State != monitor.Running:
		return "start the monitor"
	case !status.ObservationOK:
		return "wait for a matching observation"
	default:
		return ""
	}
}
