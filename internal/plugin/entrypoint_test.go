package plugin

import (
	"context"
	"fmt"
	"github.com/cdowell09/herdr-pr-board/internal/testutil"
	"github.com/pelletier/go-toml/v2"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
)

const pluginID = "cdowell09.pr-board"

func openCmd() string {
	return "plugin pane open --plugin " + pluginID + " --entrypoint " + Entrypoint() + " --placement tab --focus"
}

func TestMain(m *testing.M) {
	if strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe") != "herdr" {
		switch os.Getenv("PR_BOARD_PLUGIN_HELPER") {
		case "open":
			if err := Open(context.Background()); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			os.Exit(0)
		case "run":
			path, cleanup, err := Prepare(context.Background())
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			appendLog(os.Getenv("HERDR_FAKE_BOARD_LOG"), "args=--config "+path)
			panePath := filepath.Join(os.Getenv("HERDR_PLUGIN_STATE_DIR"), "pane-id")
			if data, err := os.ReadFile(panePath); err == nil {
				appendLog(os.Getenv("HERDR_FAKE_BOARD_LOG"), "owned_pane="+strings.TrimSpace(string(data)))
			}
			if owner := os.Getenv("HERDR_FAKE_BOARD_NEW_OWNER"); owner != "" {
				_ = os.WriteFile(panePath, []byte(owner+"\n"), 0600)
			}
			// Invoke the open action from another workspace while this board runs.
			if workspace := os.Getenv("HERDR_FAKE_OPEN_FROM"); workspace != "" {
				os.Setenv("HERDR_WORKSPACE_ID", workspace)
				if err := Open(context.Background()); err != nil {
					fmt.Fprintln(os.Stderr, err)
					os.Exit(1)
				}
			}
			if err := cleanup(); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			os.Exit(0)
		}
	}
	if strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe") == "herdr" {
		args := strings.Join(os.Args[1:], " ")
		appendLog(os.Getenv("HERDR_FAKE_LOG"), args)
		if strings.HasPrefix(args, "plugin pane focus ") {
			// HERDR_FAKE_PANE names the pane's current workspace, or a failure.
			switch workspace := os.Getenv("HERDR_FAKE_PANE"); workspace {
			case "missing":
				fmt.Fprintln(os.Stderr, `{"error":{"code":"pane_not_found","message":"pane not found"}}`)
				os.Exit(1)
			case "plain":
				fmt.Fprintln(os.Stderr, `{"error":{"code":"plugin_pane_not_found","message":"plugin pane not found"}}`)
				os.Exit(1)
			case "error":
				fmt.Fprintln(os.Stderr, `{"error":{"code":"server_unavailable","message":"no server"}}`)
				os.Exit(1)
			default:
				pane := os.Getenv("HERDR_FAKE_PANE_ID")
				if pane == "" {
					pane = os.Args[4]
				}
				fmt.Printf(`{"result":{"type":"plugin_pane_focused","plugin_pane":{"pane":{"pane_id":%q,"workspace_id":%q}}}}`+"\n", pane, workspace)
			}
		}
		if strings.HasPrefix(args, "pane move ") {
			// Herdr names a new tab with --label; --tab-label is a usage error here.
			if strings.Contains(args, "--new-tab") && strings.Contains(args, "--tab-label") {
				os.Exit(2)
			}
			switch os.Getenv("HERDR_FAKE_MOVE") {
			case "workspace":
				fmt.Fprintln(os.Stderr, `{"error":{"code":"workspace_not_found","message":"workspace w5 not found"}}`)
				os.Exit(1)
			case "unchanged":
				fmt.Println(`{"result":{"move_result":{"changed":false,"reason":"zoomed_tab","closed_workspace_id":null}}}`)
			case "closed":
				fmt.Println(`{"result":{"move_result":{"changed":true,"reason":null,"closed_workspace_id":"w1"}}}`)
			default:
				fmt.Println(`{"result":{"move_result":{"changed":true,"reason":null,"closed_workspace_id":null}}}`)
			}
		}
		// Herdr's report-metadata prints nothing when it succeeds.
		if strings.HasPrefix(args, "workspace report-metadata ") && os.Getenv("HERDR_FAKE_CLEAR") == "error" {
			os.Exit(1)
		}
		if strings.HasPrefix(args, "plugin pane open ") {
			_, err := os.Stat(filepath.Join(os.Getenv("HERDR_PLUGIN_STATE_DIR"), "pane-id"))
			state := "no"
			if err == nil {
				state = "yes"
			}
			appendLog(os.Getenv("HERDR_FAKE_LOG"), "open_saw_pane_file="+state)
			fmt.Println(`{"result":{"plugin_pane":{"pane":{"pane_id":"pane-created"}}}}`)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func appendLog(path, line string) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	_, err = fmt.Fprintln(file, line)
	file.Close()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

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
	herdr := testutil.Executable(t, binDir, "herdr")
	f.env = map[string]string{
		"HERDR_BIN_PATH":          herdr,
		"HERDR_PLUGIN_ROOT":       f.root,
		"HERDR_PLUGIN_STATE_DIR":  f.stateDir,
		"HERDR_PLUGIN_CONFIG_DIR": f.configDir,
		"HERDR_FAKE_LOG":          f.herdrLog,
		"HERDR_FAKE_BOARD_LOG":    f.boardLog,
		// Keep the developer's live Herdr session out of the fixture.
		"HERDR_WORKSPACE_ID": "",
		"HERDR_TAB_ID":       "",
		"HERDR_PANE_ID":      "",
		"HERDR_FAKE_PANE":    "w1",
	}
	return f
}

func (f *fixture) run(t *testing.T, script string) (string, error) {
	t.Helper()
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(envEntries(f.env), "PR_BOARD_PLUGIN_HELPER="+script)
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
	f.env["HERDR_FAKE_PANE"] = "missing"
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
	if got := readPaneFile(t, f.stateDir); got != "pane-created" {
		t.Fatalf("replacement pane not recorded: %s", got)
	}
}

func TestOpenFocusesPaneInActiveWorkspace(t *testing.T) {
	f := newFixture(t)
	f.env["HERDR_WORKSPACE_ID"] = "w5"
	f.env["HERDR_FAKE_PANE"] = "w5"
	writePaneFile(t, f.stateDir, "w5:pane-live")

	if out, err := f.run(t, "open"); err != nil {
		t.Fatalf("open failed: %v (%s)", err, out)
	}
	contains(t, f.herdrLines(t), "plugin pane focus w5:pane-live")
	notContainsFragment(t, f.herdrLines(t), "pane move")
}

// Herdr keeps a pane's earlier ID as an alias after the user moves the pane.
// The ID prefix then names the wrong workspace, so Open asks Herdr instead.
func TestOpenUsesCurrentLocationOfPaneMovedByUser(t *testing.T) {
	t.Run("into the active workspace", func(t *testing.T) {
		f := newFixture(t)
		f.env["HERDR_WORKSPACE_ID"] = "w5"
		f.env["HERDR_FAKE_PANE"] = "w5"
		f.env["HERDR_FAKE_PANE_ID"] = "w5:pane-current"
		writePaneFile(t, f.stateDir, "w1:pane-alias")

		if out, err := f.run(t, "open"); err != nil {
			t.Fatalf("open failed: %v (%s)", err, out)
		}
		contains(t, f.herdrLines(t), "plugin pane focus w1:pane-alias")
		notContainsFragment(t, f.herdrLines(t), "pane move")
		if got := readPaneFile(t, f.stateDir); got != "w1:pane-alias" {
			t.Fatalf("pane file = %q, want the board's first pane ID", got)
		}
	})
	t.Run("out of the active workspace", func(t *testing.T) {
		f := newFixture(t)
		f.env["HERDR_WORKSPACE_ID"] = "w1"
		f.env["HERDR_FAKE_PANE"] = "w7"
		f.env["HERDR_FAKE_PANE_ID"] = "w7:pane-current"
		writePaneFile(t, f.stateDir, "w1:pane-alias")

		if out, err := f.run(t, "open"); err != nil {
			t.Fatalf("open failed: %v (%s)", err, out)
		}
		contains(t, f.herdrLines(t), "pane move w7:pane-current --new-tab --workspace w1 --label PR Board --focus")
	})
}

func TestOpenMovesPaneFromOtherWorkspace(t *testing.T) {
	f := newFixture(t)
	f.env["HERDR_WORKSPACE_ID"] = "w5"
	writePaneFile(t, f.stateDir, "w1:pane-live")

	if out, err := f.run(t, "open"); err != nil {
		t.Fatalf("open failed: %v (%s)", err, out)
	}
	lines := f.herdrLines(t)
	focus, move := "plugin pane focus w1:pane-live", "pane move w1:pane-live --new-tab --workspace w5 --label PR Board --focus"
	contains(t, lines, focus)
	contains(t, lines, move)
	if lineIndex(lines, focus) >= lineIndex(lines, move) {
		t.Fatalf("plugin focus must confirm the board before the move: %#v", lines)
	}
	notContainsFragment(t, lines, "plugin pane open")
	contains(t, lines, "workspace report-metadata w1 --source "+pluginID+" --clear-token prs_open --clear-token prs_review --clear-token prs_ci")
	if got := readPaneFile(t, f.stateDir); got != "w1:pane-live" {
		t.Fatalf("pane file = %q, want the board's first pane ID", got)
	}
}

// Herdr closes a workspace that the move leaves empty. It has no tokens left.
func TestOpenSkipsTokenClearForClosedWorkspace(t *testing.T) {
	f := newFixture(t)
	f.env["HERDR_WORKSPACE_ID"] = "w5"
	f.env["HERDR_FAKE_MOVE"] = "closed"
	writePaneFile(t, f.stateDir, "w1:pane-live")

	if out, err := f.run(t, "open"); err != nil {
		t.Fatalf("open failed: %v (%s)", err, out)
	}
	notContainsFragment(t, f.herdrLines(t), "workspace report-metadata")
}

// After an unclean restart, Herdr can restore the recorded ID as a plain shell.
// That shell is not the board: Open must not move it and must open a new board.
func TestOpenReplacesRecordRestoredAsPlainShell(t *testing.T) {
	for name, workspace := range map[string]string{"same workspace": "w1", "other workspace": "w5"} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			f.env["HERDR_WORKSPACE_ID"] = workspace
			f.env["HERDR_FAKE_PANE"] = "plain"
			writePaneFile(t, f.stateDir, "w1:pane-shell")

			if out, err := f.run(t, "open"); err != nil {
				t.Fatalf("open failed: %v (%s)", err, out)
			}
			notContainsFragment(t, f.herdrLines(t), "pane move")
			contains(t, f.herdrLines(t), openCmd())
			if got := readPaneFile(t, f.stateDir); got != "pane-created" {
				t.Fatalf("pane file = %q, want the new board", got)
			}
		})
	}
}

// The record keeps the board's first pane ID through a move. Herdr resolves
// that ID as an alias, so the board's cleanup still finds its own record.
func TestMovedBoardCleansUpItsRecord(t *testing.T) {
	f := newFixture(t)
	if out, err := f.run(t, "open"); err != nil {
		t.Fatalf("open failed: %v (%s)", err, out)
	}
	// The board starts, a second open moves it, and then the board exits.
	f.env["HERDR_PANE_ID"] = "pane-created"
	f.env["HERDR_FAKE_OPEN_FROM"] = "w5"
	if out, err := f.run(t, "run"); err != nil {
		t.Fatalf("run failed: %v (%s)", err, out)
	}
	lines := f.herdrLines(t)
	move := "pane move pane-created --new-tab --workspace w5 --label PR Board --focus"
	contains(t, lines, move)
	if lineIndex(lines, "plugin pane focus pane-created") > lineIndex(lines, move) {
		t.Fatalf("plugin focus must confirm the board before the move: %#v", lines)
	}
	if paneFileExists(t, f.stateDir) {
		t.Fatal("moved board left its pane record behind")
	}
}

// A live board that Herdr cannot find, focus, or move must stay the only board.
func TestOpenKeepsBoardRecordWhenHerdrFails(t *testing.T) {
	for name, env := range map[string]map[string]string{
		"focus":     {"HERDR_FAKE_PANE": "error"},
		"workspace": {"HERDR_WORKSPACE_ID": "w5", "HERDR_FAKE_MOVE": "workspace"},
		"unchanged": {"HERDR_WORKSPACE_ID": "w5", "HERDR_FAKE_MOVE": "unchanged"},
		"clear":     {"HERDR_WORKSPACE_ID": "w5", "HERDR_FAKE_CLEAR": "error"},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			for key, value := range env {
				f.env[key] = value
			}
			writePaneFile(t, f.stateDir, "w1:pane-live")

			if out, err := f.run(t, "open"); err == nil {
				t.Fatalf("open succeeded without showing the board (%s)", out)
			}
			notContainsFragment(t, f.herdrLines(t), "plugin pane open")
			if got := readPaneFile(t, f.stateDir); got != "w1:pane-live" {
				t.Fatalf("pane file = %q, want the live board kept", got)
			}
		})
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

	writePaneFile(t, f.stateDir, "pane-live")
	if out, err := f.run(t, "open"); err != nil {
		t.Fatalf("open (live) failed: %v (%s)", err, out)
	}
	f.env["HERDR_FAKE_PANE"] = "missing"
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

// Herdr starts the manifest pane that Open names. A manifest without that pane
// stops the board on a new installation.
func TestEntrypointNamesTheManifestPaneForThisPlatform(t *testing.T) {
	pane := manifestPaneByID(t, Entrypoint())
	if !slices.Contains(pane.Platforms, hostPlatform()) {
		t.Fatalf("pane %q serves %v, want %s", pane.ID, pane.Platforms, hostPlatform())
	}
}

// Herdr 0.8 finds macOS and Linux pane commands on PATH only. It finds Windows
// pane commands in the plugin root. Each platform needs the command it can run.
func TestManifestDeclaresOneBoardPaneForEachPlatform(t *testing.T) {
	wantCommand := map[string][]string{
		"macos":   {"bash", "bin/run"},
		"linux":   {"bash", "bin/run"},
		"windows": {"bin/herdr-pr-board.exe", "--plugin-action", "run"},
	}
	platforms, panes := readManifest(t)
	paneByPlatform := make(map[string]string)
	for _, pane := range panes {
		if pane.Title != "PR Board" || pane.Placement != "tab" {
			t.Fatalf("pane %q = %q at %q, want the reusable %q tab", pane.ID, pane.Title, pane.Placement, "PR Board")
		}
		for _, platform := range pane.Platforms {
			if other, ok := paneByPlatform[platform]; ok {
				t.Fatalf("platform %q has panes %q and %q", platform, other, pane.ID)
			}
			paneByPlatform[platform] = pane.ID
			if want := wantCommand[platform]; !slices.Equal(pane.Command, want) {
				t.Fatalf("pane %q on %s = %v, want %v", pane.ID, platform, pane.Command, want)
			}
		}
	}
	for _, platform := range platforms {
		if paneByPlatform[platform] == "" {
			t.Fatalf("platform %q has no pane", platform)
		}
	}
}

// The macOS and Linux pane command must start the plugin binary in the plugin
// root. Herdr runs it from the plugin root with HERDR_PLUGIN_ROOT set.
func TestTheManifestPaneCommandStartsTheBoard(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the Windows pane starts the native binary, not the wrapper")
	}
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	arguments := filepath.Join(root, "arguments")
	fake := "#!/bin/sh\nprintf '%s\\n' \"$@\" >\"$PR_BOARD_TEST_ARGUMENTS\"\n"
	if err := os.WriteFile(filepath.Join(root, "bin", "herdr-pr-board"), []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}

	command := manifestPaneByID(t, Entrypoint()).Command
	pane := exec.Command(command[0], command[1:]...)
	pane.Dir = repoRoot(t)
	pane.Env = append(os.Environ(), "HERDR_PLUGIN_ROOT="+root, "PR_BOARD_TEST_ARGUMENTS="+arguments)
	if out, err := pane.CombinedOutput(); err != nil {
		t.Fatalf("pane command %v failed: %v (%s)", command, err, out)
	}

	got, err := os.ReadFile(arguments)
	if err != nil {
		t.Fatal(err)
	}
	if want := "--plugin-action\nrun\n"; string(got) != want {
		t.Fatalf("the pane started the board with %q, want %q", got, want)
	}
}

type manifestPane struct {
	ID        string   `toml:"id"`
	Title     string   `toml:"title"`
	Placement string   `toml:"placement"`
	Platforms []string `toml:"platforms"`
	Command   []string `toml:"command"`
}

func readManifest(t *testing.T) ([]string, []manifestPane) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "herdr-plugin.toml"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Platforms []string       `toml:"platforms"`
		Panes     []manifestPane `toml:"panes"`
	}
	if err := toml.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest.Platforms) == 0 || len(manifest.Panes) == 0 {
		t.Fatal("herdr-plugin.toml declares no platforms or no panes")
	}
	return manifest.Platforms, manifest.Panes
}

func manifestPaneByID(t *testing.T, id string) manifestPane {
	t.Helper()
	_, panes := readManifest(t)
	for _, pane := range panes {
		if pane.ID == id {
			return pane
		}
	}
	t.Fatalf("the manifest declares no pane %q", id)
	return manifestPane{}
}

// hostPlatform names this operating system the way the manifest names it.
func hostPlatform() string {
	if runtime.GOOS == "darwin" {
		return "macos"
	}
	return runtime.GOOS
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve test file path")
	}
	return filepath.Dir(filepath.Dir(filepath.Dir(file)))
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

func TestConcurrentOpenRecordsPaneBeforePrepare(t *testing.T) {
	f := newFixture(t)
	var group sync.WaitGroup
	failures := make(chan error, 8)
	for range 8 {
		group.Add(1)
		go func() { defer group.Done(); _, err := f.run(t, "open"); failures <- err }()
	}
	group.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	count := 0
	for _, line := range f.herdrLines(t) {
		if line == openCmd() {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("created %d tabs before Prepare", count)
	}
	if got := readPaneFile(t, f.stateDir); got != "pane-created" {
		t.Fatalf("pane=%s", got)
	}
}
