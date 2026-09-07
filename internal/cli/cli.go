// Package cli runs the external command-line tools the board depends on.
package cli

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"
)

// Runner executes one command with arguments and returns its stdout.
// On failure it returns the error together with any stdout the command
// produced, because tools such as gh print a response body even when they
// exit non-zero.
type Runner func(context.Context, ...string) ([]byte, error)

// Command returns a Runner that executes binary. The error text comes from
// stderr, then stdout, then the exit status, whichever is first non-empty.
func Command(binary string) Runner {
	return func(ctx context.Context, args ...string) ([]byte, error) {
		cmd := exec.CommandContext(ctx, binary, args...)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			message := strings.TrimSpace(stderr.String())
			if message == "" {
				message = strings.TrimSpace(stdout.String())
			}
			if message == "" {
				message = err.Error()
			}
			return stdout.Bytes(), errors.New(message)
		}
		return stdout.Bytes(), nil
	}
}
