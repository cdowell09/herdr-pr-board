package notification

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/cdowell09/herdr-pr-board/internal/config"
	"github.com/cdowell09/herdr-pr-board/internal/reviewmemory"
	"github.com/cdowell09/herdr-pr-board/internal/testutil"
)

// TestMain doubles as the fake herdr executable. It records its arguments in
// HERDR_FAKE_LOG and exits with HERDR_FAKE_EXIT.
func TestMain(m *testing.M) {
	if strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe") != "herdr" {
		os.Exit(m.Run())
	}
	if err := os.WriteFile(os.Getenv("HERDR_FAKE_LOG"), []byte(strings.Join(os.Args[1:], "\n")+"\n"), 0600); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	code, _ := strconv.Atoi(os.Getenv("HERDR_FAKE_EXIT"))
	if code != 0 {
		fmt.Fprintln(os.Stderr, "herdr: notification failed")
	}
	os.Exit(code)
}

type fakeRunner struct {
	calls [][]string
	err   error
}

func (f *fakeRunner) Run(_ context.Context, args ...string) ([]byte, error) {
	f.calls = append(f.calls, append([]string(nil), args...))
	return nil, f.err
}

// configWith writes a configuration file whose review.notify holds mode.
func configWith(t *testing.T, mode config.NotifyMode) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	writeMode(t, path, mode)
	return path
}

func writeMode(t *testing.T, path string, mode config.NotifyMode) {
	t.Helper()
	data := strings.Replace(config.DefaultFile, `notify = "all"`, `notify = "`+string(mode)+`"`, 1)
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
}

func finished(status reviewmemory.Status, message string, severities ...string) reviewmemory.Run {
	findings := make([]reviewmemory.Finding, len(severities))
	for i, severity := range severities {
		findings[i] = reviewmemory.Finding{Severity: severity, Title: "finding"}
	}
	return reviewmemory.Run{
		ID:       "run-1",
		Identity: reviewmemory.Identity{Repository: "acme/widgets", Number: 7},
		Outcome:  reviewmemory.Outcome{Status: status, Message: message, Findings: findings},
	}
}

func TestNewRequiresHerdrWorkspace(t *testing.T) {
	for _, workspace := range []string{"", "  "} {
		if notifier := New("config.toml", workspace, "/opt/herdr"); notifier != nil {
			t.Fatalf("workspace %q built %#v", workspace, notifier)
		}
	}
	notifier := New("config.toml", "w1", "/opt/herdr")
	if notifier == nil || notifier.Binary != "/opt/herdr" || notifier.ConfigPath != "config.toml" {
		t.Fatalf("notifier = %#v", notifier)
	}
}

func TestNotifyShowsRecordedOutcome(t *testing.T) {
	long := strings.Repeat("x", 130)
	cases := []struct {
		name string
		mode config.NotifyMode
		run  reviewmemory.Run
		want []string
	}{
		{
			name: "completed counts every severity",
			mode: config.NotifyAll,
			run:  finished(reviewmemory.Completed, "", "P1", "P3", "P1"),
			want: []string{"notification", "show", "Review completed: acme/widgets #7", "--body", "P0:0 P1:2 P2:0 P3:1", "--sound", "done"},
		},
		{
			name: "completed without findings keeps zero counts",
			mode: config.NotifyAll,
			run:  finished(reviewmemory.Completed, ""),
			want: []string{"notification", "show", "Review completed: acme/widgets #7", "--body", "P0:0 P1:0 P2:0 P3:0", "--sound", "done"},
		},
		{
			name: "blocked shows the reason without diagnostics",
			mode: config.NotifyProblems,
			run:  finished(reviewmemory.Blocked, "specification missing; diagnostics: /state/reviews/run-1"),
			want: []string{"notification", "show", "Review blocked: acme/widgets #7", "--body", "specification missing", "--sound", "request"},
		},
		{
			name: "failed caps a long reason",
			mode: config.NotifyAll,
			run:  finished(reviewmemory.Failed, long),
			want: []string{"notification", "show", "Review failed: acme/widgets #7", "--body", long[:120], "--sound", "request"},
		},
		{
			name: "failed without a message omits the body",
			mode: config.NotifyAll,
			run:  finished(reviewmemory.Failed, "  "),
			want: []string{"notification", "show", "Review failed: acme/widgets #7", "--sound", "request"},
		},
		{name: "problems skips completed", mode: config.NotifyProblems, run: finished(reviewmemory.Completed, "", "P0")},
		{name: "off skips failed", mode: config.NotifyOff, run: finished(reviewmemory.Failed, "boom")},
		{name: "running never notifies", mode: config.NotifyAll, run: finished(reviewmemory.Running, "")},
		{name: "abandoned never notifies", mode: config.NotifyAll, run: finished(reviewmemory.Abandoned, "owner exited")},
		{name: "launch failure without a run never notifies", mode: config.NotifyAll, run: reviewmemory.Run{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runner := &fakeRunner{}
			notifier := &Notifier{Runner: runner.Run, ConfigPath: configWith(t, tc.mode)}
			if err := notifier.Notify(context.Background(), tc.run); err != nil {
				t.Fatal(err)
			}
			if tc.want == nil {
				if len(runner.calls) != 0 {
					t.Fatalf("unexpected notification %#v", runner.calls)
				}
				return
			}
			if len(runner.calls) != 1 || !reflect.DeepEqual(runner.calls[0], tc.want) {
				t.Fatalf("args = %#v\nwant %#v", runner.calls, tc.want)
			}
		})
	}
}

func TestNotifyReadsModeWhenRunFinishes(t *testing.T) {
	runner := &fakeRunner{}
	path := configWith(t, config.NotifyAll)
	notifier := &Notifier{Runner: runner.Run, ConfigPath: path}
	writeMode(t, path, config.NotifyOff)
	if err := notifier.Notify(context.Background(), finished(reviewmemory.Completed, "")); err != nil || len(runner.calls) != 0 {
		t.Fatalf("stale mode: calls=%#v err=%v", runner.calls, err)
	}
	writeMode(t, path, config.NotifyAll)
	if err := notifier.Notify(context.Background(), finished(reviewmemory.Completed, "")); err != nil || len(runner.calls) != 1 {
		t.Fatalf("saved mode ignored: calls=%#v err=%v", runner.calls, err)
	}
}

func TestNotifyReportsUnreadableConfiguration(t *testing.T) {
	runner := &fakeRunner{}
	notifier := &Notifier{Runner: runner.Run, ConfigPath: filepath.Join(t.TempDir(), "missing.toml")}
	err := notifier.Notify(context.Background(), finished(reviewmemory.Failed, "boom"))
	if err == nil || !strings.Contains(err.Error(), "read review.notify") || len(runner.calls) != 0 {
		t.Fatalf("calls=%#v err=%v", runner.calls, err)
	}
}

func TestNotifyIgnoresNilNotifier(t *testing.T) {
	var notifier *Notifier
	if err := notifier.Notify(context.Background(), finished(reviewmemory.Failed, "boom")); err != nil {
		t.Fatal(err)
	}
}

func TestNotifyWrapsRunnerFailure(t *testing.T) {
	cause := errors.New("no herdr server")
	notifier := &Notifier{Runner: (&fakeRunner{err: cause}).Run, ConfigPath: configWith(t, config.NotifyAll)}
	err := notifier.Notify(context.Background(), finished(reviewmemory.Completed, ""))
	if !errors.Is(err, cause) || !strings.Contains(err.Error(), "show review notification") {
		t.Fatalf("err = %v", err)
	}
}

func TestNotifyRunsConfiguredHerdrExecutable(t *testing.T) {
	dir := t.TempDir()
	bin := testutil.Executable(t, dir, "herdr")
	log := filepath.Join(dir, "herdr.log")
	t.Setenv("HERDR_FAKE_LOG", log)
	t.Setenv("HERDR_FAKE_EXIT", "0")
	notifier := New(configWith(t, config.NotifyAll), "w1", bin)
	if err := notifier.Notify(context.Background(), finished(reviewmemory.Completed, "", "P2")); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	want := "notification\nshow\nReview completed: acme/widgets #7\n--body\nP0:0 P1:0 P2:1 P3:0\n--sound\ndone\n"
	if string(data) != want {
		t.Fatalf("herdr received %q, want %q", data, want)
	}

	t.Setenv("HERDR_FAKE_EXIT", "3")
	err = notifier.Notify(context.Background(), finished(reviewmemory.Failed, "reviewer crashed"))
	if err == nil || !strings.Contains(err.Error(), "herdr: notification failed") {
		t.Fatalf("err = %v", err)
	}

	missing := New(notifier.ConfigPath, "w1", filepath.Join(dir, "missing"))
	err = missing.Notify(context.Background(), finished(reviewmemory.Failed, "reviewer crashed"))
	if err == nil || !strings.Contains(err.Error(), "show review notification") {
		t.Fatalf("missing CLI err = %v", err)
	}
}
