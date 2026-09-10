package reviewmemory

import (
	"slices"
	"strconv"
	"strings"
)

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

// String lists every count in Severities order, for example "P0:1 P1:2 P2:0 P3:3".
func (c SeverityCounts) String() string {
	tokens := make([]string, len(c))
	for i, count := range c {
		tokens[i] = Severities[i] + ":" + strconv.Itoa(count)
	}
	return strings.Join(tokens, " ")
}
