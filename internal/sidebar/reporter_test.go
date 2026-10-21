package sidebar

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/cdowell09/herdr-pr-board/internal/config"
	"github.com/cdowell09/herdr-pr-board/internal/testutil"
)

func TestMain(m *testing.M) {
	if strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe") == "herdr" {
		if strings.Join(os.Args[1:], " ") == "pane current" {
			os.Stdout.Write(paneIn("w1"))
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func paneIn(workspace string) []byte {
	return []byte(`{"result":{"pane":{"pane_id":"` + workspace + `:p1","workspace_id":"` + workspace + `"}}}`)
}

// fakeRunner records report calls. It answers `pane current` with current,
// or fails that lookup when current is nil.
type fakeRunner struct {
	calls   [][]string
	err     error
	current []byte
}

func (f *fakeRunner) Run(_ context.Context, args ...string) ([]byte, error) {
	if len(args) == 2 && args[0] == "pane" && args[1] == "current" {
		if f.current == nil {
			return nil, errors.New("no pane")
		}
		return f.current, nil
	}
	f.calls = append(f.calls, append([]string(nil), args...))
	if f.err != nil {
		return nil, f.err
	}
	return []byte(`{"id":"cli:workspace:report-metadata","result":{}}`), nil
}

// The open action moves the board pane between workspaces. The process keeps
// its startup workspace ID, so each report asks Herdr where the pane runs now.
func TestReportFollowsBoardPaneToItsCurrentWorkspace(t *testing.T) {
	runner := &fakeRunner{current: paneIn("w5")}
	reporter := Reporter{Runner: runner.Run, WorkspaceID: "w1"}
	if err := reporter.Report(context.Background(), map[string]string{TokenOpen: "1 open"}); err != nil {
		t.Fatal(err)
	}
	if got := runner.calls[0][2]; got != "w5" {
		t.Fatalf("reported to %q, want the current workspace w5", got)
	}
}

// The startup workspace can be obsolete, so an unknown location is an error.
func TestReportFailsWhenBoardWorkspaceIsUnknown(t *testing.T) {
	for name, current := range map[string][]byte{
		"lookup fails": nil,
		"malformed":    []byte("not json"),
		"no workspace": []byte(`{"result":{"pane":{"pane_id":"w1:p1"}}}`),
	} {
		t.Run(name, func(t *testing.T) {
			runner := &fakeRunner{current: current}
			reporter := Reporter{Runner: runner.Run, WorkspaceID: "w1"}
			if err := reporter.Report(context.Background(), map[string]string{TokenOpen: "1 open"}); err == nil {
				t.Fatal("expected an unknown workspace error")
			}
			if len(runner.calls) != 0 {
				t.Fatalf("reported without a known workspace: %#v", runner.calls)
			}
		})
	}
}

func TestReportRequiresWorkspaceID(t *testing.T) {
	runner := &fakeRunner{}
	reporter := Reporter{Runner: runner.Run}
	if err := reporter.Report(context.Background(), map[string]string{TokenOpen: "1 open"}); err == nil {
		t.Fatal("expected missing workspace ID error")
	}
	if len(runner.calls) != 0 {
		t.Fatalf("runner calls = %#v, want none", runner.calls)
	}
}

func TestReportUsesConfiguredBinary(t *testing.T) {
	bin := testutil.Executable(t, t.TempDir(), "herdr")

	if err := (Reporter{Binary: bin, WorkspaceID: "w1"}).Report(context.Background(), map[string]string{TokenOpen: "1 open"}); err != nil {
		t.Fatal(err)
	}
}

func TestReportBuildsMetadataArguments(t *testing.T) {
	runner := &fakeRunner{current: paneIn("w2")}
	reporter := Reporter{Runner: runner.Run, WorkspaceID: "w2", TTL: 15 * time.Minute}
	err := reporter.Report(context.Background(), map[string]string{
		TokenOpen:   "3 open",
		TokenReview: "2 review",
		TokenCI:     "1 fail",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"workspace", "report-metadata", "w2",
		"--source", Source,
		"--token", "prs_ci=1 fail",
		"--token", "prs_open=3 open",
		"--token", "prs_review=2 review",
		"--ttl-ms", "900000",
	}
	if !reflect.DeepEqual(runner.calls[0], want) {
		t.Fatalf("args = %#v\nwant %#v", runner.calls[0], want)
	}
}

func TestReportOmitsTTLWhenZero(t *testing.T) {
	runner := &fakeRunner{current: paneIn("w1")}
	reporter := Reporter{Runner: runner.Run, WorkspaceID: "w1"}
	if err := reporter.Report(context.Background(), map[string]string{TokenOpen: "1 open"}); err != nil {
		t.Fatal(err)
	}
	args := runner.calls[0]
	for _, arg := range args {
		if arg == "--ttl-ms" {
			t.Fatalf("unexpected --ttl-ms in args: %#v", args)
		}
	}
}

func TestReportPropagatesRunnerError(t *testing.T) {
	reporter := Reporter{Runner: (&fakeRunner{current: paneIn("w1"), err: errors.New("bad workspace")}).Run, WorkspaceID: "w1"}
	if err := reporter.Report(context.Background(), map[string]string{TokenOpen: "1 open"}); err == nil {
		t.Fatal("expected error")
	}
}

func TestNewReporterRequiresEnabledConfigAndWorkspace(t *testing.T) {
	disabled := false
	cases := []struct {
		name      string
		cfg       config.SidebarConfig
		workspace string
		want      bool
	}{
		{"enabled with workspace", config.SidebarConfig{TTL: "15m"}, "w1", true},
		{"outside Herdr", config.SidebarConfig{TTL: "15m"}, "", false},
		{"blank workspace", config.SidebarConfig{TTL: "15m"}, "  ", false},
		{"disabled", config.SidebarConfig{Enabled: &disabled, TTL: "15m"}, "w1", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reporter := NewReporter(tc.cfg, tc.workspace, "/opt/herdr")
			if (reporter != nil) != tc.want {
				t.Fatalf("reporter = %#v, want present=%v", reporter, tc.want)
			}
			if reporter == nil {
				return
			}
			if reporter.WorkspaceID != "w1" || reporter.Binary != "/opt/herdr" || reporter.TTL != 15*time.Minute {
				t.Fatalf("reporter = %#v", reporter)
			}
		})
	}
}
