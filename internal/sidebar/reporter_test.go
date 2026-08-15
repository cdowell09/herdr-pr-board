package sidebar

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
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
	reporter := Reporter{Runner: runner}
	if err := reporter.Report(context.Background(), "", map[string]string{TokenOpen: "1 open"}); err == nil {
		t.Fatal("expected missing workspace ID error")
	}
	if len(runner.calls) != 0 {
		t.Fatalf("runner calls = %#v, want none", runner.calls)
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
