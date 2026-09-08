package review

import (
	"golang.org/x/sys/windows"
	"os"
	"testing"
)

func makeNonregularResult(path string) error { return os.Mkdir(path, 0700) }
func reviewChildAlive(t *testing.T, pid int) func() bool {
	t.Helper()
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = windows.CloseHandle(handle) })
	return func() bool {
		state, err := windows.WaitForSingleObject(handle, 0)
		if err != nil {
			t.Fatal(err)
		}
		return state != windows.WAIT_OBJECT_0
	}
}
