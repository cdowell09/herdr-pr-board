package reviewmemory

import (
	"bufio"
	"errors"
	"fmt"
	"github.com/cdowell09/herdr-pr-board/internal/cli"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func request() Request {
	return Request{Identity: Identity{Repository: "acme/repo", Number: 1, HeadOID: strings.Repeat("a", 40), BaseRefName: "main"}, BaseOID: strings.Repeat("b", 40), Reviewer: "pi"}
}

func openStore(t *testing.T, dir string) *Store {
	t.Helper()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestHistoryAndEligibility(t *testing.T) {
	dir := t.TempDir()
	s := openStore(t, dir)
	r := request()
	c, err := s.Claim(r)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err := s.Claim(r); !errors.Is(err, ErrActive) {
		t.Fatal(err)
	}
	if err := c.Finish(Outcome{Status: Completed, Message: "done"}); err == nil {
		t.Fatal("accepted missing findings")
	}
	if err := c.Finish(Outcome{Status: Completed, Findings: []Finding{{Severity: "bad"}}, Message: "done"}); err == nil {
		t.Fatal("accepted malformed findings")
	}
	if err := c.Finish(Outcome{Status: Completed, Findings: []Finding{}, Message: "done"}); err != nil {
		t.Fatal(err)
	}
	s = openStore(t, dir)
	r.Reviewer = "other"
	r.BaseOID = strings.Repeat("c", 40)
	if _, err := s.Claim(r); !errors.Is(err, ErrReviewed) {
		t.Fatal(err)
	}
	r.Rerun = true
	retry, err := s.Claim(r)
	if err != nil {
		t.Fatal(err)
	}
	if err := retry.Finish(Outcome{Status: Failed, Message: "command failed"}); err != nil {
		t.Fatal(err)
	}
	h, err := s.History(r.Identity)
	if err != nil || len(h) != 2 || h[0].Status != Completed || h[1].Status != Failed {
		t.Fatalf("%+v %v", h, err)
	}
	r.Rerun = false
	r.Identity.HeadOID = strings.Repeat("d", 40)
	next, err := s.Claim(r)
	if err != nil {
		t.Fatal(err)
	}
	next.Close()
	if _, err := s.Claim(r); !errors.Is(err, ErrRetryRequired) {
		t.Fatal(err)
	}
	r.Identity.BaseRefName = "release"
	next, err = s.Claim(r)
	if err != nil {
		t.Fatal(err)
	}
	next.Close()
	all, err := s.PRHistory("ACME/repo", 1)
	if err != nil || len(all) != 4 {
		t.Fatalf("all revisions: %+v %v", all, err)
	}
}

func TestUppercaseCommitsCannotBypassHistory(t *testing.T) {
	s := openStore(t, t.TempDir())
	r := request()
	c, err := s.Claim(r)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for _, status := range []Status{Running, Completed} {
		if status == Completed {
			if err := c.Finish(Outcome{Status: Completed, Findings: []Finding{}, Message: "done"}); err != nil {
				t.Fatal(err)
			}
		}
		for _, field := range []string{"head", "base"} {
			upper := r
			upper.MaxConcurrent = 2
			if field == "head" {
				upper.Identity.HeadOID = strings.ToUpper(upper.Identity.HeadOID)
			} else {
				upper.BaseOID = strings.ToUpper(upper.BaseOID)
			}
			if err := ValidateRevision(upper.Identity, upper.BaseOID); err == nil {
				t.Fatalf("accepted uppercase %s commit", field)
			}
			if duplicate, err := s.Claim(upper); err == nil {
				duplicate.Close()
				t.Fatalf("uppercase %s bypassed %s history", field, status)
			}
		}
	}
	h, err := s.History(r.Identity)
	if err != nil || len(h) != 1 || h[0].Status != Completed {
		t.Fatalf("history changed: %+v %v", h, err)
	}
}

func TestRecoveryAndStaleOwner(t *testing.T) {
	s := openStore(t, t.TempDir())
	r := request()
	c, err := s.Claim(r)
	if err != nil {
		t.Fatal(err)
	}
	fd, err := c.LockFile()
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
	if _, err := s.Claim(r); !errors.Is(err, ErrActive) {
		t.Fatalf("duplicate descriptor lost ownership: %v", err)
	}
	fd.Close()
	r.Rerun = true
	next, err := s.Claim(r)
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	if err := c.Finish(Outcome{Status: Completed, Findings: []Finding{}, Message: "late"}); !errors.Is(err, ErrOwnership) {
		t.Fatal(err)
	}
	h, err := s.History(r.Identity)
	if err != nil || len(h) != 2 || h[0].Status != Abandoned || h[1].Status != Running {
		t.Fatalf("%+v %v", h, err)
	}
}

func TestCorruptHistoryFailsClosed(t *testing.T) {
	s := openStore(t, t.TempDir())
	path := filepath.Join(s.dir, "history.json")
	for _, data := range []string{"{", `{}`, `{"runs":[]}`, `{"version":1}`, `{"version":1,"runs":null}`, `{"version":2,"runs":[]}`, `{"version":1,"runs":[{"id":"../escape"}]}`} {
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Claim(request()); err == nil {
			t.Fatalf("accepted %s", data)
		}
		got, err := os.ReadFile(path)
		if err != nil || string(got) != data {
			t.Fatalf("overwrote corruption: %s %v", got, err)
		}
	}
}

func TestInheritedClaimProcess(t *testing.T) {
	if os.Getenv("REVIEW_MEMORY_INHERITED") != "1" {
		return
	}
	owner, err := cli.TakeInheritedFile("HERDR_REVIEW_CLAIM_FD")
	if err != nil || owner == nil {
		t.Fatalf("inherited claim: %v", err)
	}
	defer owner.Close()
	fmt.Println("ready")
	bufio.NewReader(os.Stdin).ReadString('\n')
}

func TestChildRetainsClaimAfterParentCloses(t *testing.T) {
	s := openStore(t, t.TempDir())
	c, err := s.Claim(request())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	fd, err := c.LockFile()
	if err != nil {
		t.Fatal(err)
	}
	defer fd.Close()
	cmd := exec.Command(os.Args[0], "-test.run=^TestInheritedClaimProcess$")
	cmd.Env = append(os.Environ(), "REVIEW_MEMORY_INHERITED=1")
	if err := cli.PassFile(cmd, fd, "HERDR_REVIEW_CLAIM_FD"); err != nil {
		t.Fatal(err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { cmd.Process.Kill(); cmd.Wait() }()
	if line, err := bufio.NewReader(out).ReadString('\n'); err != nil || line != "ready\n" {
		t.Fatalf("%q %v", line, err)
	}
	fd.Close()
	c.Close()
	if _, err := s.Claim(request()); !errors.Is(err, ErrActive) {
		t.Fatalf("child lost claim: %v", err)
	}
	in.Close()
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	h, err := s.History(request().Identity)
	if err != nil || len(h) != 1 || h[0].Status != Abandoned {
		t.Fatalf("%+v %v", h, err)
	}
}

func TestFailedOutcomeRetainsChildClaimUntilExit(t *testing.T) {
	s := openStore(t, t.TempDir())
	c, err := s.Claim(request())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	fd, err := c.LockFile()
	if err != nil {
		t.Fatal(err)
	}
	defer fd.Close()
	cmd := exec.Command(os.Args[0], "-test.run=^TestInheritedClaimProcess$")
	cmd.Env = append(os.Environ(), "REVIEW_MEMORY_INHERITED=1")
	if err := cli.PassFile(cmd, fd, "HERDR_REVIEW_CLAIM_FD"); err != nil {
		t.Fatal(err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { cmd.Process.Kill(); cmd.Wait() }()
	if line, err := bufio.NewReader(out).ReadString('\n'); err != nil || line != "ready\n" {
		t.Fatalf("%q %v", line, err)
	}
	fd.Close()
	if err := c.Finish(Outcome{Status: Failed, Message: "adapter exited"}); err != nil {
		t.Fatal(err)
	}
	r := request()
	r.Rerun = true
	if _, err := s.Claim(r); !errors.Is(err, ErrActive) {
		t.Fatalf("explicit rerun bypassed child: %v", err)
	}
	other := request()
	other.Identity.Number = 2
	if _, err := s.Claim(other); !errors.Is(err, ErrCapacity) {
		t.Fatalf("lost occupied slot: %v", err)
	}
	if available, err := s.HasCapacity(1); err != nil || available {
		t.Fatalf("capacity=%v err=%v", available, err)
	}

	if _, err := s.Claim(request()); !errors.Is(err, ErrActive) {
		t.Fatalf("child lost claim: %v", err)
	}
	in.Close()
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	h, err := s.History(request().Identity)
	if err != nil || len(h) != 1 || h[0].Status != Failed {
		t.Fatalf("%+v %v", h, err)
	}
	if available, err := s.HasCapacity(1); err != nil || !available {
		t.Fatalf("capacity remains held: %v %v", available, err)
	}
	retry, err := s.Claim(r)
	if err != nil {
		t.Fatal(err)
	}
	retry.Close()
}

func TestClaimProcess(t *testing.T) {
	if os.Getenv("REVIEW_MEMORY_HELPER") != "1" {
		return
	}
	s, err := Open(os.Getenv("REVIEW_MEMORY_DIR"))
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.Claim(request())
	if err != nil {
		fmt.Println("rejected")
		return
	}
	defer c.Close()
	fmt.Println("claimed")
	bufio.NewReader(os.Stdin).ReadString('\n')
}

func TestProcessesCoordinateAndRecoverAfterCrash(t *testing.T) {
	dir := t.TempDir()
	start := func() (*exec.Cmd, string) {
		cmd := exec.Command(os.Args[0], "-test.run=^TestClaimProcess$")
		cmd.Env = append(os.Environ(), "REVIEW_MEMORY_HELPER=1", "REVIEW_MEMORY_DIR="+dir)
		out, err := cmd.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		in, err := cmd.StdinPipe()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			in.Close()
			if cmd.Process != nil {
				cmd.Process.Kill()
			}
		})
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		line, err := bufio.NewReader(out).ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		return cmd, line
	}
	owner, line := start()
	if line != "claimed\n" {
		t.Fatal(line)
	}
	competitor, line := start()
	if line != "rejected\n" {
		t.Fatal(line)
	}
	if err := competitor.Wait(); err != nil {
		t.Fatal(err)
	}
	s := openStore(t, dir)
	other := request()
	other.Identity.Number = 2
	if _, err := s.Claim(other); !errors.Is(err, ErrCapacity) {
		t.Fatal(err)
	}
	owner.Process.Kill()
	owner.Wait()
	h, err := s.History(request().Identity)
	if err != nil || len(h) != 1 || h[0].Status != Abandoned {
		t.Fatalf("%+v %v", h, err)
	}
	r := request()
	r.Rerun = true
	recovered, err := s.Claim(r)
	if err != nil {
		t.Fatal(err)
	}
	defer recovered.Close()
}

func TestReviewStatusUsesLiveClaimsAndExactRevision(t *testing.T) {
	store := openStore(t, t.TempDir())
	request := request()
	claim, err := store.Claim(request)
	if err != nil {
		t.Fatal(err)
	}
	defer claim.Close()
	inherited, err := claim.LockFile()
	if err != nil {
		t.Fatal(err)
	}
	defer inherited.Close()
	if err := claim.Finish(Outcome{Status: Completed, Message: "complete", Findings: []Finding{}}); err != nil {
		t.Fatal(err)
	}
	if err := store.ReviewStatus(request.Identity); !errors.Is(err, ErrActive) {
		t.Fatalf("orphan owner status=%v", err)
	}
	inherited.Close()
	if err := store.ReviewStatus(request.Identity); !errors.Is(err, ErrReviewed) {
		t.Fatalf("completed status=%v", err)
	}
	for _, change := range []func(*Identity){func(id *Identity) { id.HeadOID = strings.Repeat("c", 40) }, func(id *Identity) { id.BaseRefName = "release" }} {
		id := request.Identity
		change(&id)
		if err := store.ReviewStatus(id); err != nil {
			t.Fatalf("new revision status=%v", err)
		}
	}
}
