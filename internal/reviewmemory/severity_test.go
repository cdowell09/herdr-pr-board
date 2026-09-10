package reviewmemory

import "testing"

func TestCountSeveritiesKeepsSeverityOrder(t *testing.T) {
	findings := []Finding{
		{Severity: "P3", Title: "nit", Body: "b"},
		{Severity: "P0", Title: "crash", Body: "b"},
		{Severity: "P1", Title: "leak", Body: "b"},
		{Severity: "P0", Title: "data loss", Body: "b"},
		{Severity: "P3", Title: "typo", Body: "b"},
		{Severity: "P3", Title: "style", Body: "b"},
	}
	got := CountSeverities(findings)
	if want := (SeverityCounts{2, 1, 0, 3}); got != want {
		t.Fatalf("CountSeverities = %v, want %v", got, want)
	}
}

func TestCountSeveritiesOfNoFindingsIsZero(t *testing.T) {
	for _, findings := range [][]Finding{nil, {}} {
		if got := CountSeverities(findings); got != (SeverityCounts{}) {
			t.Fatalf("CountSeverities(%v) = %v", findings, got)
		}
	}
}

func TestValidOutcomeRejectsUnlistedSeverity(t *testing.T) {
	outcome := Outcome{Status: Completed, Message: "done", Findings: []Finding{{Severity: "P4", Title: "t", Body: "b"}}}
	if validOutcome(outcome) {
		t.Fatal("validOutcome accepted severity P4")
	}
	outcome.Findings[0].Severity = "P3"
	if !validOutcome(outcome) {
		t.Fatal("validOutcome rejected severity P3")
	}
}

func TestSeverityCountsStringListsEveryCount(t *testing.T) {
	if got := (SeverityCounts{1, 2, 0, 3}).String(); got != "P0:1 P1:2 P2:0 P3:3" {
		t.Fatalf("String() = %q", got)
	}
	if got := (SeverityCounts{}).String(); got != "P0:0 P1:0 P2:0 P3:0" {
		t.Fatalf("zero String() = %q", got)
	}
}
