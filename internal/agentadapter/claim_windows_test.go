package agentadapter

import (
	"testing"

	"golang.org/x/sys/windows"
)

func checkClaimAgentAfterOwnerDeath(t *testing.T, pid int, _ string) func() {
	t.Helper()
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = windows.TerminateProcess(handle, 1)
		_ = windows.CloseHandle(handle)
	})
	return func() {
		state, err := windows.WaitForSingleObject(handle, 5000)
		if err != nil || state != windows.WAIT_OBJECT_0 {
			t.Fatalf("agent survives adapter death: wait=%d error=%v", state, err)
		}
	}
}
