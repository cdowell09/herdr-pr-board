package plugin

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const (
	pluginID   = "cdowell09.pr-board"
	entrypoint = "board"
)

func openCmd() string {
	return "plugin pane open --plugin " + pluginID + " --entrypoint " + entrypoint + " --placement tab --focus"
}

const fakeHerdrScript = `#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >>"${HERDR_FAKE_LOG:?missing HERDR_FAKE_LOG}"
case "${1:-}" in
  plugin)
    case "${2:-}" in
      pane)
        case "${3:-}" in
          focus) exit "${HERDR_FAKE_FOCUS_EXIT:-0}" ;;
          open)
            if [[ -f "${HERDR_PLUGIN_STATE_DIR:?missing HERDR_PLUGIN_STATE_DIR}/pane-id" ]]; then
              printf 'open_saw_pane_file=yes\n' >>"$HERDR_FAKE_LOG"
            else
              printf 'open_saw_pane_file=no\n' >>"$HERDR_FAKE_LOG"
            fi
            exit 0
            ;;
        esac
        ;;
    esac
    ;;
  tab)
    case "${2:-}" in
      rename) exit 0 ;;
    esac
    ;;
esac
exit 0
`

const fakeBoardScript = `#!/usr/bin/env bash
set -euo pipefail
log="${HERDR_FAKE_BOARD_LOG:?missing HERDR_FAKE_BOARD_LOG}"
printf 'args=%s\n' "$*" >>"$log"
pane_file="${HERDR_PLUGIN_STATE_DIR:?missing HERDR_PLUGIN_STATE_DIR}/pane-id"
if [[ -f "$pane_file" ]]; then
  printf 'owned_pane=%s\n' "$(<"$pane_file")" >>"$log"
fi
if [[ -n "${HERDR_FAKE_BOARD_NEW_OWNER:-}" ]]; then
  printf '%s\n' "$HERDR_FAKE_BOARD_NEW_OWNER" >"$pane_file"
fi
exit 0
`

type fixture struct {
	root       string
	stateDir   string
	configDir  string
	configFile string
	herdrLog   string
	boardLog   string
	env        map[string]string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	base := t.TempDir()
	f := &fixture{
		root:       filepath.Join(base, "root"),
		stateDir:   filepath.Join(base, "state"),
		configDir:  filepath.Join(base, "config"),
		configFile: filepath.Join(base, "config", "config.toml"),
		herdrLog:   filepath.Join(base, "herdr.log"),
		boardLog:   filepath.Join(base, "board.log"),
	}
	for _, dir := range []string{f.root, f.stateDir, f.configDir, filepath.Join(f.root, "bin")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	binDir := filepath.Join(base, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	herdr := filepath.Join(binDir, "herdr")
	writeScript(t, herdr, fakeHerdrScript)
	writeScript(t, filepath.Join(f.root, "bin", "herdr-pr-board"), fakeBoardScript)
	f.env = map[string]string{
		"HERDR_BIN_PATH":          herdr,
		"HERDR_PLUGIN_ROOT":       f.root,
		"HERDR_PLUGIN_STATE_DIR":  f.stateDir,
		"HERDR_PLUGIN_CONFIG_DIR": f.configDir,
		"HERDR_FAKE_LOG":          f.herdrLog,
		"HERDR_FAKE_BOARD_LOG":    f.boardLog,
	}
	return f
}

func (f *fixture) run(t *testing.T, script string) (string, error) {
	t.Helper()
	cmd := exec.Command("bash", filepath.Join(repoRoot(t), "bin", script))
	cmd.Env = append(os.Environ(), envEntries(f.env)...)
	cmd.Dir = repoRoot(t)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (f *fixture) herdrLines(t *testing.T) []string {
	t.Helper()
	return logLines(t, f.herdrLog)
}

func (f *fixture) boardLines(t *testing.T) []string {
	t.Helper()
	return logLines(t, f.boardLog)
}

func TestOpenFocusesLivePane(t *testing.T) {
	f := newFixture(t)
	f.env["HERDR_FAKE_FOCUS_EXIT"] = "0"
	writePaneFile(t, f.stateDir, "pane-live")

	if out, err := f.run(t, "open"); err != nil {
		t.Fatalf("open failed: %v (%s)", err, out)
	}
	contains(t, f.herdrLines(t), "plugin pane focus pane-live")
	notContainsFragment(t, f.herdrLines(t), "plugin pane open")
	if got := readPaneFile(t, f.stateDir); got != "pane-live" {
		t.Fatalf("pane file = %q, want %q", got, "pane-live")
	}
}

func TestOpenReplacesStalePane(t *testing.T) {
	f := newFixture(t)
	f.env["HERDR_FAKE_FOCUS_EXIT"] = "1"
	writePaneFile(t, f.stateDir, "pane-stale")

	if out, err := f.run(t, "open"); err != nil {
		t.Fatalf("open failed: %v (%s)", err, out)
	}
	lines := f.herdrLines(t)
	focus := "plugin pane focus pane-stale"
	contains(t, lines, focus)
	contains(t, lines, openCmd())
	contains(t, lines, "open_saw_pane_file=no")
	if focusIdx, openIdx := lineIndex(lines, focus), lineIndex(lines, openCmd()); focusIdx >= openIdx {
		t.Fatalf("focus must precede the replacement open: %#v", lines)
	}
	if paneFileExists(t, f.stateDir) {
		t.Fatal("stale pane file was not removed")
	}
}

func TestOpenOpensTabWhenNoPaneRecorded(t *testing.T) {
	f := newFixture(t)

	if out, err := f.run(t, "open"); err != nil {
		t.Fatalf("open failed: %v (%s)", err, out)
	}
	contains(t, f.herdrLines(t), openCmd())
	notContainsFragment(t, f.herdrLines(t), "plugin pane focus")
}

func TestRunRecordsOwnershipAndNamesTab(t *testing.T) {
	f := newFixture(t)
	f.env["HERDR_PANE_ID"] = "pane-run"
	f.env["HERDR_TAB_ID"] = "tab-run"

	if out, err := f.run(t, "run"); err != nil {
		t.Fatalf("run failed: %v (%s)", err, out)
	}
	contains(t, f.herdrLines(t), "tab rename tab-run PR Board")
	contains(t, f.boardLines(t), "owned_pane=pane-run")
	contains(t, f.boardLines(t), "args=--config "+f.configFile)
}

func TestRunCleanupRemovesOwnedPaneState(t *testing.T) {
	f := newFixture(t)
	f.env["HERDR_PANE_ID"] = "pane-owned"

	if out, err := f.run(t, "run"); err != nil {
		t.Fatalf("run failed: %v (%s)", err, out)
	}
	if paneFileExists(t, f.stateDir) {
		t.Fatal("owned pane state was not cleaned up")
	}
}

func TestRunCleanupKeepsReplacedPaneState(t *testing.T) {
	f := newFixture(t)
	f.env["HERDR_PANE_ID"] = "pane-original"
	f.env["HERDR_FAKE_BOARD_NEW_OWNER"] = "pane-replacement"

	if out, err := f.run(t, "run"); err != nil {
		t.Fatalf("run failed: %v (%s)", err, out)
	}
	contains(t, f.boardLines(t), "owned_pane=pane-original")
	if got := readPaneFile(t, f.stateDir); got != "pane-replacement" {
		t.Fatalf("pane file = %q, want replacement %q", got, "pane-replacement")
	}
}

func TestEntrypointsLeaveUserConfigUntouched(t *testing.T) {
	f := newFixture(t)
	const want = "title = \"user board\"\n"
	if err := os.WriteFile(f.configFile, []byte(want), 0o644); err != nil {
		t.Fatal(err)
	}

	f.env["HERDR_FAKE_FOCUS_EXIT"] = "0"
	writePaneFile(t, f.stateDir, "pane-live")
	if out, err := f.run(t, "open"); err != nil {
		t.Fatalf("open (live) failed: %v (%s)", err, out)
	}
	f.env["HERDR_FAKE_FOCUS_EXIT"] = "1"
	if out, err := f.run(t, "open"); err != nil {
		t.Fatalf("open (stale) failed: %v (%s)", err, out)
	}
	f.env["HERDR_PANE_ID"] = "pane-config"
	if out, err := f.run(t, "run"); err != nil {
		t.Fatalf("run failed: %v (%s)", err, out)
	}

	entries, err := os.ReadDir(f.configDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "config.toml" {
		t.Fatalf("config dir contents changed: %v", entries)
	}
	got, err := os.ReadFile(f.configFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("config.toml overwritten: %q", got)
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve test file path")
	}
	return filepath.Dir(filepath.Dir(filepath.Dir(file)))
}

func writeScript(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}

func writePaneFile(t *testing.T, stateDir, pane string) {
	t.Helper()
	if err := os.WriteFile(paneFile(stateDir), []byte(pane+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readPaneFile(t *testing.T, stateDir string) string {
	t.Helper()
	data, err := os.ReadFile(paneFile(stateDir))
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(data))
}

func paneFileExists(t *testing.T, stateDir string) bool {
	t.Helper()
	_, err := os.Stat(paneFile(stateDir))
	return err == nil
}

func paneFile(stateDir string) string {
	return filepath.Join(stateDir, "pane-id")
}

func logLines(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatal(err)
	}
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "\n")
}

func envEntries(extra map[string]string) []string {
	env := os.Environ()
	for key, value := range extra {
		env = append(env, key+"="+value)
	}
	return env
}

func contains(t *testing.T, lines []string, want string) {
	t.Helper()
	if lineIndex(lines, want) == -1 {
		t.Fatalf("missing %q in lines %#v", want, lines)
	}
}

func notContainsFragment(t *testing.T, lines []string, fragment string) {
	t.Helper()
	for _, line := range lines {
		if strings.Contains(line, fragment) {
			t.Fatalf("unexpected %q in line %q", fragment, line)
		}
	}
}

func lineIndex(lines []string, want string) int {
	for i, line := range lines {
		if line == want {
			return i
		}
	}
	return -1
}
