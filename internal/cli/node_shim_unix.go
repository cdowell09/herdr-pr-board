//go:build !windows

package cli

import "os/exec"

// ResolveNodeShim preserves native commands on Unix.
func ResolveNodeShim(cmd *exec.Cmd) error { return nil }
