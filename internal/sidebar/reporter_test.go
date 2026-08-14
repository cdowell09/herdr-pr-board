package sidebar

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

type fakeRunner struct {
	calls      [][]string
	err        error
	listOutput []byte
}

func (f *fakeRunner) Run(_ context.Context, args ...string) ([]byte, error) {
	f.calls = append(f.calls, append([]string(nil), args...))
	if f.err != nil {
		return nil, f.err
	}
	if len(args) >= 2 && args[0] == "workspace" && args[1] == "list" {
		if f.listOutput != nil {
			return f.listOutput, nil
		}
		return []byte(`{"id":"cli:workspace:list","result":{"type":"workspace_list","workspaces":[{"workspace_id":"w1","label":"one"},{"workspace_id":"w2","label":"two"}]}}`), nil
	}
	return []byte(`{"id":"cli:workspace:report-metadata","result":{}}`), nil
}

func TestWorkspacesParsesIDs(t *testing.T) {
	runner := &fakeRunner{}
	reporter := Reporter{Runner: runner}
	ids, err := reporter.Workspaces(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ids, []string{"w1", "w2"}) {
		t.Fatalf("workspaces = %#v", ids)
	}
	want := []string{"workspace", "list"}
	if !reflect.DeepEqual(runner.calls[0], want) {
		t.Fatalf("args = %#v, want %#v", runner.calls[0], want)
	}
}

func TestWorkspacesPropagatesRunnerError(t *testing.T) {
	reporter := Reporter{Runner: &fakeRunner{err: errors.New("no session")}}
	if _, err := reporter.Workspaces(context.Background()); err == nil {
		t.Fatal("expected error")
	}
}

func TestWorkspacesRejectsMalformedOutput(t *testing.T) {
	reporter := Reporter{Runner: &fakeRunner{listOutput: []byte("not json")}}
	if _, err := reporter.Workspaces(context.Background()); err == nil {
		t.Fatal("expected error for malformed output")
	}
}

func TestReportBuildsMetadataArguments(t *testing.T) {
	runner := &fakeRunner{}
	reporter := Reporter{Runner: runner, TTL: 15 * time.Minute}
	err := reporter.Report(context.Background(), "w2", map[string]string{
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
	reporter := Reporter{Runner: runner}
	if err := reporter.Report(context.Background(), "w1", map[string]string{TokenOpen: "1 open"}); err != nil {
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
	reporter := Reporter{Runner: &fakeRunner{err: errors.New("bad workspace")}}
	if err := reporter.Report(context.Background(), "w1", map[string]string{TokenOpen: "1 open"}); err == nil {
		t.Fatal("expected error")
	}
}
