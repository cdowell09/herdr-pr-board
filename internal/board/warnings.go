package board

import (
	"strings"

	"github.com/cdowell09/herdr-pr-board/internal/discovery"
)

// discoveryWarnings formats retrieval failures for the board footer.
func discoveryWarnings(failures []discovery.RetrievalError) string {
	var warning string
	for _, failure := range failures {
		message := failure.Err.Error()
		switch failure.Stage {
		case "rates":
			message = "rate limits unavailable: " + message
		case "enrichment":
			message = "CI refresh failed: " + message
		}
		warning = appendWarning(warning, message)
	}
	return warning
}

// appendWarning joins footer warnings. It drops an empty or already listed
// warning, so one error shared by every view appears once.
func appendWarning(current, next string) string {
	if next == "" {
		return current
	}
	if current == "" {
		return next
	}
	if strings.Contains(current, next) {
		return current
	}
	return current + "; " + next
}
