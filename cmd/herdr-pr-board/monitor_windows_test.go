package main

import "os/exec"

func stopCommandProcess(cmd *exec.Cmd) error {
	// Headless Windows processes have no console for terminal control events.
	if err := cmd.Process.Kill(); err != nil {
		return err
	}
	_ = cmd.Wait()
	return nil
}
