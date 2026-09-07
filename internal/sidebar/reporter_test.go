package sidebar

import (
	"context"
	"errors"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/cdowell09/herdr-pr-board/internal/config"
)

type fakeRunner struct {
	calls [][]string
	err   error
}

func (f *fakeRunner) Run(_ context.Context, args ...string) ([]byte, error) {
	f.calls = append(f.calls, append([]string(nil), args...))
	if f.err != nil {
		return nil, f.err
	}
	return []byte(`{"id":"cli:workspace:report-metadata","result":{}}`), nil
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
	bin := t.TempDir() + "/herdr"
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}

	if err := (Reporter{Binary: bin, WorkspaceID: "w1"}).Report(context.Background(), map[string]string{TokenOpen: "1 open"}); err != nil {
		t.Fatal(err)
	}
}

func TestReportBuildsMetadataArguments(t *testing.T) {
	runner := &fakeRunner{}
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
	runner := &fakeRunner{}
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
	reporter := Reporter{Runner: (&fakeRunner{err: errors.New("bad workspace")}).Run, WorkspaceID: "w1"}
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
