package cli

import (
	"errors"
	"fmt"
	"os/exec"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

type ownedProcess struct {
	job, process, thread windows.Handle
	pid                  uint32
	streams              *processStreams
}

func startOwned(cmd *exec.Cmd) (*ownedProcess, error) {
	owner, err := createOwned(cmd)
	if err != nil {
		return nil, err
	}
	if _, err := windows.ResumeThread(owner.thread); err != nil {
		_ = owner.kill()
		owner.close()
		return nil, err
	}
	return owner, nil
}

func (p *ownedProcess) interrupt(_ syscall.Signal) error {
	// CTRL_BREAK targets this new process group. CTRL_C cannot target a group.
	return windows.GenerateConsoleCtrlEvent(windows.CTRL_BREAK_EVENT, p.pid)
}
func (p *ownedProcess) kill() error {
	// Job accounting can reach zero before an exiting process handle becomes
	// signaled. Retain exact member handles before termination and wait for them.
	members := map[uint32]windows.Handle{}
	defer func() {
		for _, handle := range members {
			windows.CloseHandle(handle)
		}
	}()
	var cleanupErr error
	for {
		if err := p.retainMembers(members); cleanupErr == nil {
			cleanupErr = err
		}
		if err := windows.TerminateJobObject(p.job, 1); cleanupErr == nil {
			cleanupErr = err
		}
		// A child can start while the first membership snapshot is being read.
		if err := p.retainMembers(members); cleanupErr == nil {
			cleanupErr = err
		}
		var info jobAccounting
		err := windows.QueryInformationJobObject(p.job, windows.JobObjectBasicAccountingInformation, uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)), nil)
		if err == nil && info.ActiveProcesses == 0 {
			break
		}
		if cleanupErr == nil {
			cleanupErr = err
		}
		time.Sleep(10 * time.Millisecond)
	}
	for _, handle := range members {
		for {
			state, err := windows.WaitForSingleObject(handle, windows.INFINITE)
			if err == nil && state == windows.WAIT_OBJECT_0 {
				break
			}
			if cleanupErr == nil {
				cleanupErr = fmt.Errorf("wait for reviewer process termination: state=%d error=%v", state, err)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	return cleanupErr
}

func (p *ownedProcess) retainMembers(members map[uint32]windows.Handle) error {
	capacity := 16
	for {
		buffer := make([]byte, 8+capacity*int(unsafe.Sizeof(uintptr(0))))
		err := windows.QueryInformationJobObject(p.job, windows.JobObjectBasicProcessIdList, uintptr(unsafe.Pointer(&buffer[0])), uint32(len(buffer)), nil)
		if errors.Is(err, windows.ERROR_MORE_DATA) {
			capacity *= 2
			continue
		}
		if err != nil {
			return err
		}
		count := *(*uint32)(unsafe.Pointer(&buffer[4]))
		if count > uint32(capacity) {
			capacity = int(count)
			continue
		}
		ids := unsafe.Slice((*uintptr)(unsafe.Pointer(&buffer[8])), int(count))
		for _, id := range ids {
			pid := uint32(id)
			if _, known := members[pid]; known {
				continue
			}
			handle, err := windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
			if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
				continue
			}
			if err != nil {
				return err
			}
			var belongs bool
			if err := processInJob(handle, p.job, &belongs); err != nil {
				windows.CloseHandle(handle)
				return err
			}
			if belongs {
				members[pid] = handle
			} else {
				windows.CloseHandle(handle)
			}
		}
		return nil
	}
}

func (p *ownedProcess) close() {
	if p.streams != nil {
		p.streams.close()
	}
	if p.thread != 0 {
		_ = windows.CloseHandle(p.thread)
	}
	if p.process != 0 {
		_ = windows.CloseHandle(p.process)
	}
	if p.job != 0 {
		_ = windows.CloseHandle(p.job)
	}
}

func (p *ownedProcess) wait() error {
	if _, err := windows.WaitForSingleObject(p.process, windows.INFINITE); err != nil {
		return err
	}
	var code uint32
	if err := windows.GetExitCodeProcess(p.process, &code); err != nil {
		return err
	}
	streamErr := p.streams.wait()
	if code != 0 {
		return fmt.Errorf("exit status %d", code)
	}
	return streamErr
}

var isProcessInJobProc = windows.NewLazySystemDLL("kernel32.dll").NewProc("IsProcessInJob")

func processInJob(process, job windows.Handle, belongs *bool) error {
	var value uint32
	result, _, err := isProcessInJobProc.Call(uintptr(process), uintptr(job), uintptr(unsafe.Pointer(&value)))
	if result == 0 {
		return err
	}
	*belongs = value != 0
	return nil
}

// JOBOBJECT_BASIC_ACCOUNTING_INFORMATION from winnt.h.
type jobAccounting struct {
	TotalUserTime, TotalKernelTime, ThisPeriodTotalUserTime, ThisPeriodTotalKernelTime int64
	TotalPageFaultCount, TotalProcesses, ActiveProcesses, TotalTerminatedProcesses     uint32
}
