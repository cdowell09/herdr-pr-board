package agentadapter

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// inheritedClaim duplicates the explicit claim descriptor without closing the
// adapter's inherited descriptor. The agent retains the duplicate if this adapter dies.
func inheritedClaim() (*os.File, error) {
	value, present := os.LookupEnv("HERDR_REVIEW_CLAIM_FD")
	if !present {
		return nil, nil
	}
	if value != "3" {
		return nil, errors.New("HERDR_REVIEW_CLAIM_FD must be 3")
	}
	fd, err := unix.Dup(3)
	if err != nil {
		return nil, fmt.Errorf("duplicate review claim descriptor: %w", err)
	}
	unix.CloseOnExec(fd)
	file := os.NewFile(uintptr(fd), "review-claim")
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		file.Close()
		return nil, errors.New("review claim descriptor must refer to a regular lock file")
	}
	return file, nil
}
