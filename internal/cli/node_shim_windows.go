package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ResolveNodeShim runs npm Node launchers without interpreting batch arguments.
// Call this before configuring streams, inherited handles, or environment.
func ResolveNodeShim(cmd *exec.Cmd) error {
	if cmd.Err != nil {
		return nil
	}
	ext := filepath.Ext(cmd.Path)
	if !strings.EqualFold(ext, ".cmd") && !strings.EqualFold(ext, ".bat") {
		return nil
	}
	path, err := filepath.Abs(cmd.Path)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	target, err := npmNodeTarget(data)
	if err != nil {
		return fmt.Errorf("%s: %w", cmd.Path, err)
	}
	script := filepath.Join(filepath.Dir(path), target)
	info, err := os.Stat(script)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("npm Node target is not a regular file")
	}
	node := filepath.Join(filepath.Dir(path), "node.exe")
	if _, err := os.Stat(node); os.IsNotExist(err) {
		node = "node.exe"
	}
	resolved := exec.Command(node, append([]string{script}, cmd.Args[1:]...)...)
	if resolved.Err != nil {
		return fmt.Errorf("resolve npm Node runtime: %w", resolved.Err)
	}
	cmd.Path, cmd.Args = resolved.Path, resolved.Args
	return nil
}
