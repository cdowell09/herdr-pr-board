package reviewmemory

import "slices"

// Severities lists finding severities from most to least severe.
var Severities = [...]string{"P0", "P1", "P2", "P3"}

// SeverityCounts holds the number of findings at each severity, in Severities order.
type SeverityCounts [len(Severities)]int

// CountSeverities tallies findings by severity. Stored findings always carry a
// listed severity because validOutcome rejects any other value.
func CountSeverities(findings []Finding) SeverityCounts {
	var counts SeverityCounts
	for _, f := range findings {
		if i := slices.Index(Severities[:], f.Severity); i >= 0 {
			counts[i]++
		}
	}
	return counts
}
