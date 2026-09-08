//go:build !windows

package reviewinstructions

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestInstructionFIFOAndUnreadableFileDoNotStartReads(t *testing.T) {
	dir := t.TempDir()
	fifo := filepath.Join(dir, "instructions.fifo")
	if err := unix.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	finished := make(chan error, 1)
	go func() { _, err := (Files{Prompt: fifo}).Load(dir); finished <- err }()
	select {
	case err := <-finished:
		if err == nil {
			t.Fatal("FIFO was accepted")
		}
	case <-time.After(time.Second):
		t.Fatal("FIFO read blocked")
	}
	if os.Geteuid() == 0 {
		return // Root bypasses ordinary Unix file permissions.
	}
	path := filepath.Join(dir, "unreadable.md")
	if err := os.WriteFile(path, []byte("Review security."), 0000); err != nil {
		t.Fatal(err)
	}
	if _, err := (Files{Skill: path}).Load(dir); err == nil {
		t.Fatal("unreadable file was accepted")
	}
}
