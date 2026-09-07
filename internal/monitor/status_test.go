package monitor

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/cdowell09/herdr-pr-board/internal/discovery"
	"github.com/cdowell09/herdr-pr-board/internal/localstate"
)

func TestInspectSeparatesOwnershipAndObservation(t *testing.T) {
	s, f := testSource(t)
	if got := Inspect(s.dir, s.cfg); got.State != Stopped {
		t.Fatalf("%+v", got)
	}
	owner, err := localstate.TryLock(filepath.Join(s.dir, "monitor.lock"))
	if err != nil {
		t.Fatal(err)
	}
	if got := Inspect(s.dir, s.cfg); got.State != Running || got.ObservationOK {
		t.Fatalf("missing snapshot: %+v", got)
	}
	if err := s.write(f.snapshot); err != nil {
		t.Fatal(err)
	}
	if got := Inspect(s.dir, s.cfg); got.State != Running || !got.ObservationOK {
		t.Fatalf("valid snapshot: %+v", got)
	}
	cfg := s.cfg
	cfg.GitHub.Scopes = []string{"org:changed"}
	if got := Inspect(s.dir, cfg); got.State != Running || got.ObservationOK {
		t.Fatalf("mismatch: %+v", got)
	}
	f.snapshot.Errors = []discovery.RetrievalError{{Stage: "search", Err: errors.New("failed")}}
	if err := s.write(f.snapshot); err != nil {
		t.Fatal(err)
	}
	if got := Inspect(s.dir, s.cfg); got.State != Running || got.ObservationOK {
		t.Fatalf("failure: %+v", got)
	}
	owner.Close()
	if got := Inspect(s.dir, s.cfg); got.State != Stopped || got.ObservationOK {
		t.Fatalf("stopped stale snapshot: %+v", got)
	}
	if f.calls != 0 {
		t.Fatal("status made discovery requests")
	}
	if got := Inspect("", s.cfg); got.State != Unknown {
		t.Fatalf("unset dir: %+v", got)
	}
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := Inspect(file, s.cfg); got.State != Unknown {
		t.Fatalf("invalid dir: %+v", got)
	}
}
