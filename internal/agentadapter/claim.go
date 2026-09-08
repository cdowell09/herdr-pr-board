package agentadapter

import (
	"errors"
	"os"

	"github.com/cdowell09/herdr-pr-board/internal/cli"
)

// inheritedClaim duplicates the explicit claim descriptor without closing the
// adapter's inherited descriptor. The agent retains the duplicate if this adapter dies.
func inheritedClaim() (*os.File, error) {
	file, err := cli.InheritedFile("HERDR_REVIEW_CLAIM_FD")
	if err != nil || file == nil {
		return file, err
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		file.Close()
		return nil, errors.New("review claim descriptor must refer to a regular lock file")
	}
	return file, nil
}
