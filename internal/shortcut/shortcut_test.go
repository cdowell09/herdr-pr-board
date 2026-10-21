package shortcut

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/cdowell09/herdr-pr-board/internal/config"
	gh "github.com/cdowell09/herdr-pr-board/internal/github"
)

type fakeRunner struct {
	calls  [][]string
	output string
	err    error
	runErr error
}

func (f *fakeRunner) Run(_ context.Context, args ...string) ([]byte, error) {
	f.calls = append(f.calls, append([]string(nil), args...))
	if len(f.calls) == 2 {
		return nil, f.runErr
	}
	return []byte(f.output), f.err
}

var pr = gh.PullRequest{Repository: "acme/widgets", Number: 12, URL: "https://github.com/acme/widgets/pull/12"}

const (
	tabCreated  = `{"id":"1","result":{"type":"tab_created","tab":{"tab_id":"w1:t4"},"root_pane":{"pane_id":"w1:p9"}}}`
	paneCreated = `{"id":"1","result":{"type":"pane_info","pane":{"pane_id":"w1:p7"}}}`
)

func TestLaunchOpensTabAndRunsCommand(t *testing.T) {
	runner := &fakeRunner{output: tabCreated}
	shortcut := config.Shortcut{Key: "r", Name: "Pi review", Run: `pi "review {url} in {repo} #{number}"`, Where: config.ShortcutTab}
	if err := (Launcher{Runner: runner.Run}).Launch(context.Background(), shortcut, pr); err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"tab", "create", "--label", "Pi review acme/widgets#12", "--focus", "--env", `PR_BOARD_COMMAND=pi "review https://github.com/acme/widgets/pull/12 in acme/widgets #12"`},
		{"pane", "run", "w1:p9", `sh -c "$PR_BOARD_COMMAND"`},
	}
	if !reflect.DeepEqual(runner.calls, want) {
		t.Fatalf("calls = %#v\nwant %#v", runner.calls, want)
	}
}

// testHome points os.UserHomeDir at a temporary directory on every platform.
func testHome(t *testing.T) string {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return home
}

func TestLaunchSplitsRightWithExpandedCwd(t *testing.T) {
	home := testHome(t)
	runner := &fakeRunner{output: paneCreated}
	shortcut := config.Shortcut{Key: "r", Name: "Shell", Run: "echo {url}", Where: config.ShortcutSplit, Cwd: "~/src"}
	if err := (Launcher{Runner: runner.Run}).Launch(context.Background(), shortcut, pr); err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"pane", "split", "--current", "--direction", "right", "--focus", "--cwd", filepath.Join(home, "src"), "--env", "PR_BOARD_COMMAND=echo https://github.com/acme/widgets/pull/12"},
		{"pane", "run", "w1:p7", `sh -c "$PR_BOARD_COMMAND"`},
	}
	if !reflect.DeepEqual(runner.calls, want) {
		t.Fatalf("calls = %#v\nwant %#v", runner.calls, want)
	}
}

func TestLaunchExpandsBareTildeAndKeepsOtherPaths(t *testing.T) {
	home := testHome(t)
	for cwd, want := range map[string]string{"~": home, "/srv/~x": "/srv/~x", "~other": "~other"} {
		runner := &fakeRunner{output: tabCreated}
		shortcut := config.Shortcut{Name: "A", Run: "a", Where: config.ShortcutTab, Cwd: cwd}
		if err := (Launcher{Runner: runner.Run}).Launch(context.Background(), shortcut, pr); err != nil {
			t.Fatal(err)
		}
		if got := runner.calls[0][6]; got != want {
			t.Fatalf("cwd %q became %q, want %q", cwd, got, want)
		}
	}
}

func TestLaunchReportsHerdrFailures(t *testing.T) {
	shortcut := config.Shortcut{Name: "A", Run: "a", Where: config.ShortcutTab}
	cases := []struct {
		name   string
		runner *fakeRunner
		want   string
		calls  int
	}{
		{"create fails", &fakeRunner{err: errors.New("no herdr server")}, "create Herdr pane: no herdr server", 1},
		{"no pane id", &fakeRunner{output: `{"result":{"type":"tab_created"}}`}, "create Herdr pane: no pane ID in Herdr output", 1},
		{"not JSON", &fakeRunner{output: "created"}, "create Herdr pane: no pane ID in Herdr output", 1},
		{"run fails", &fakeRunner{output: tabCreated, runErr: errors.New("pane gone")}, "run command in Herdr pane w1:p9: pane gone", 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := (Launcher{Runner: tc.runner.Run}).Launch(context.Background(), shortcut, pr)
			if err == nil || !strings.Contains(err.Error(), tc.want) || len(tc.runner.calls) != tc.calls {
				t.Fatalf("err = %v, calls = %d", err, len(tc.runner.calls))
			}
		})
	}
}
