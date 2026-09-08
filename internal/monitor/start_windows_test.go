package monitor

import (
	"golang.org/x/sys/windows"
	"os"
)

func monitorDetached() bool { cp, _ := windows.GetConsoleCP(); return cp == 0 }
func monitorProcessAlive(pid int) bool {
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(handle)
	result, err := windows.WaitForSingleObject(handle, 0)
	return err == nil && result == uint32(windows.WAIT_TIMEOUT)
}

// Windows uses the state directory's ACL, rather than POSIX permission bits.
func privateLogMode(mode os.FileMode) bool { return mode.IsRegular() && mode.Perm()&0200 != 0 }
