package monitor

import (
	"golang.org/x/sys/windows"
	"os/exec"
	"syscall"
)

func detachMonitor(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.DETACHED_PROCESS | windows.CREATE_NEW_PROCESS_GROUP}
}
