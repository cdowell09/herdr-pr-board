package reviewmemory

import (
	"errors"
	"testing"
)

func TestStopTargetsOwnedAttemptAndRetainsChildCapacity(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	req := request()
	req.MaxConcurrent = 2
	first, err := s.Claim(req)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	if err := first.EnableStop(); err != nil {
		t.Fatal(err)
	}
	child, err := first.LockFile()
	if err != nil {
		t.Fatal(err)
	}
	defer child.Close()
	other := req
	other.Identity.Number++
	second, err := s.Claim(other)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if err := second.EnableStop(); err != nil {
		t.Fatal(err)
	}
	if err := s.RequestStop(first.ID()); err != nil {
		t.Fatal(err)
	}
	if stopped, err := second.StopRequested(); err != nil || stopped {
		t.Fatalf("stop affected another run: %v %v", stopped, err)
	}
	completed := Outcome{Status: Completed, Message: "done", Findings: []Finding{}}
	if err := first.Finish(completed); !errors.Is(err, ErrStopped) {
		t.Fatalf("accepted stop lost to completion: %v", err)
	}
	runs, err := s.History(req.Identity)
	if err != nil || len(runs) != 1 || runs[0].Status != Failed || runs[0].Message != ErrStopped.Error() || len(runs[0].Findings) != 0 {
		t.Fatalf("stopped outcome: %+v %v", runs, err)
	}
	if available, err := s.HasCapacity(2); err != nil || available {
		t.Fatalf("slot released while child owns claim: %v %v", available, err)
	}
	if err := child.Close(); err != nil {
		t.Fatal(err)
	}
	if available, err := s.HasCapacity(2); err != nil || !available {
		t.Fatalf("slot not released after child exit: %v %v", available, err)
	}
	if _, err := s.Claim(req); !errors.Is(err, ErrRetryRequired) {
		t.Fatalf("stopped revision did not require retry: %v", err)
	}
	req.Rerun = true
	retry, err := s.Claim(req)
	if err != nil {
		t.Fatal(err)
	}
	defer retry.Close()
	if stopped, err := retry.StopRequested(); err != nil || stopped || retry.ID() == first.ID() {
		t.Fatalf("old stop affected retry: %v %v", stopped, err)
	}
	if err := second.Finish(completed); err != nil {
		t.Fatal(err)
	}
	if err := s.RequestStop(second.ID()); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("stop accepted after completion: %v", err)
	}
	if err := s.RequestStop("../../not-a-run"); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("unknown run accepted: %v", err)
	}
}

func TestStopRejectsOlderAndExitedOwners(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	claim, err := s.Claim(request())
	if err != nil {
		t.Fatal(err)
	}
	defer claim.Close()
	if err := s.RequestStop(claim.ID()); !errors.Is(err, ErrStopUnavailable) {
		t.Fatalf("older owner accepted stop: %v", err)
	}
	if err := claim.EnableStop(); err != nil {
		t.Fatal(err)
	}
	child, err := claim.LockFile()
	if err != nil {
		t.Fatal(err)
	}
	defer child.Close()
	if err := claim.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.RequestStop(claim.ID()); !errors.Is(err, ErrStopUnavailable) {
		t.Fatalf("orphan accepted stop: %v", err)
	}
	if stopped, err := claim.StopRequested(); err != nil || stopped {
		t.Fatalf("unavailable owner received request: %v %v", stopped, err)
	}
	if available, err := s.HasCapacity(1); err != nil || available {
		t.Fatalf("orphan's slot released: %v %v", available, err)
	}
}

func TestStopAndCompletionRaceHasOneOutcome(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for i := range 12 {
		req := request()
		req.Identity.Number += i
		claim, err := s.Claim(req)
		if err != nil {
			t.Fatal(err)
		}
		if err := claim.EnableStop(); err != nil {
			t.Fatal(err)
		}
		start := make(chan struct{})
		stopped, finished := make(chan error, 1), make(chan error, 1)
		go func() { <-start; stopped <- s.RequestStop(claim.ID()) }()
		go func() {
			<-start
			finished <- claim.Finish(Outcome{Status: Completed, Message: "done", Findings: []Finding{}})
		}()
		close(start)
		stopErr, finishErr := <-stopped, <-finished
		if stopErr == nil {
			if !errors.Is(finishErr, ErrStopped) {
				t.Fatalf("accepted stop lost: %v", finishErr)
			}
		} else if !errors.Is(stopErr, ErrNotRunning) || finishErr != nil {
			t.Fatalf("completion race: %v %v", stopErr, finishErr)
		}
	}
}
