package reviewmemory

import (
	"errors"
	"testing"
)

func TestSnapshotSharesLiveClaimsAndHistoryPolicy(t *testing.T) {
	dir := t.TempDir()
	writer, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	req := request()
	claim, err := writer.Claim(req)
	if err != nil {
		t.Fatal(err)
	}
	defer claim.Close()
	before, err := reader.Snapshot()
	if err != nil || len(before.Active) != 1 || !errors.Is(before.ReviewStatus(req.Identity), ErrActive) {
		t.Fatalf("active snapshot: %+v, %v", before, err)
	}
	if err := claim.Finish(Outcome{Status: Completed, Message: "Reviewed", Findings: []Finding{}}); err != nil {
		t.Fatal(err)
	}
	if err := claim.Close(); err != nil {
		t.Fatal(err)
	}
	after, err := reader.Snapshot()
	if err != nil || len(after.Active) != 0 || !errors.Is(after.ReviewStatus(req.Identity), ErrReviewed) {
		t.Fatalf("completed snapshot: %+v, %v", after, err)
	}
	if before.Runs[0].Status != Running || len(before.Active) != 1 {
		t.Fatal("later reads mutated earlier snapshot")
	}
	other := req.Identity
	other.Number++
	if err := after.ReviewStatus(other); err != nil {
		t.Fatalf("another PR affected: %v", err)
	}
}
