package board

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/cdowell09/herdr-pr-board/internal/config"
	tea "github.com/charmbracelet/bubbletea"
)

// resolveEditor returns the editor executable for the configuration file. It
// is the only resolution point, so the notice and the launched program cannot
// disagree.
func resolveEditor() string {
	if editor := strings.TrimSpace(os.Getenv("VISUAL")); editor != "" {
		return editor
	}
	if editor := strings.TrimSpace(os.Getenv("EDITOR")); editor != "" {
		return editor
	}
	if runtime.GOOS == "windows" {
		return "notepad.exe"
	}
	return "vi"
}

// editorLaunch pairs the editor command with the notice that names it. One
// resolution builds both, so the notice always names the launched executable.
type editorLaunch struct {
	command *exec.Cmd
	output  io.Writer
	notice  string
}

// newEditorLaunch resolves the editor once and prepares the launch.
func newEditorLaunch(path string) *editorLaunch {
	editor := resolveEditor()
	return &editorLaunch{
		command: exec.Command(editor, path),
		notice:  "Opening config in " + editor + ". Set $VISUAL or $EDITOR to change.",
	}
}

// Run writes the notice, then runs the editor. Bubble Tea leaves the alternate
// screen before this call, so the footer frame is gone. The written line stays
// above the editor and in the terminal history.
func (e *editorLaunch) Run() error {
	if e.output != nil {
		fmt.Fprintln(e.output, e.notice)
	}
	return e.command.Run()
}

func (e *editorLaunch) SetStdin(reader io.Reader) {
	if e.command.Stdin == nil {
		e.command.Stdin = reader
	}
}

func (e *editorLaunch) SetStdout(writer io.Writer) {
	e.output = writer
	if e.command.Stdout == nil {
		e.command.Stdout = writer
	}
}

func (e *editorLaunch) SetStderr(writer io.Writer) {
	if e.command.Stderr == nil {
		e.command.Stderr = writer
	}
}

// editConfigCmd returns the footer notice and the command that opens the
// configuration file. The notice is empty when no editor starts.
func editConfigCmd(path string) (string, tea.Cmd) {
	if strings.TrimSpace(path) == "" {
		return "", func() tea.Msg {
			return configEditMsg{err: fmt.Errorf("config path is unavailable")}
		}
	}
	launch := newEditorLaunch(path)
	return launch.notice, tea.Exec(launch, func(err error) tea.Msg {
		if err != nil {
			return configEditMsg{err: fmt.Errorf("%s: editor: %w", path, err)}
		}
		cfg, err := config.LoadExisting(path)
		if err != nil {
			return configEditMsg{err: fmt.Errorf("%s: %w", path, err)}
		}
		return configEditMsg{cfg: cfg}
	})
}
