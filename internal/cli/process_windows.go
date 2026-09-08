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
	cmd *exec.Cmd
	job windows.Handle
}

func startOwned(cmd *exec.Cmd) (*ownedProcess, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, err
	}
	owner := &ownedProcess{cmd: cmd, job: job}
	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits))); err != nil {
		owner.close()
		return nil, err
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= windows.CREATE_SUSPENDED | windows.CREATE_NEW_PROCESS_GROUP
	if err := cmd.Start(); err != nil {
		owner.close()
		return nil, err
	}
	process, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
	if err == nil {
		err = windows.AssignProcessToJobObject(job, process)
		windows.CloseHandle(process)
	}
	if err == nil {
		err = resumeProcess(uint32(cmd.Process.Pid))
	}
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		owner.close()
		return nil, fmt.Errorf("own reviewer process tree: %w", err)
	}
	return owner, nil
}

// The process starts suspended, so its primary thread cannot create children
// before the job owns it. Resume the thread through documented Windows APIs.
func resumeProcess(pid uint32) error {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPTHREAD, 0)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(snapshot)
	entry := windows.ThreadEntry32{Size: uint32(unsafe.Sizeof(windows.ThreadEntry32{}))}
	for err = windows.Thread32First(snapshot, &entry); err == nil; err = windows.Thread32Next(snapshot, &entry) {
		if entry.OwnerProcessID != pid {
			continue
		}
		thread, err := windows.OpenThread(windows.THREAD_SUSPEND_RESUME, false, entry.ThreadID)
		if err != nil {
			return err
		}
		_, err = windows.ResumeThread(thread)
		windows.CloseHandle(thread)
		return err
	}
	return errors.New("suspended process primary thread not found")
}

func (p *ownedProcess) interrupt(_ syscall.Signal) error {
	// CTRL_BREAK targets this new process group. CTRL_C cannot target a group.
	return windows.GenerateConsoleCtrlEvent(windows.CTRL_BREAK_EVENT, uint32(p.cmd.Process.Pid))
}
func (p *ownedProcess) kill() error {
	// Keep the caller's claim alive until Windows confirms every job member
	// exited. A cleanup timeout must not make a live review slot available.
	var cleanupErr error
	terminated := false
	for {
		if !terminated {
			err := windows.TerminateJobObject(p.job, 1)
			terminated = err == nil
			if cleanupErr == nil {
				cleanupErr = err
			}
		}
		var info jobAccounting
		err := windows.QueryInformationJobObject(p.job, windows.JobObjectBasicAccountingInformation, uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)), nil)
		if err == nil && info.ActiveProcesses == 0 {
			return cleanupErr
		}
		if cleanupErr == nil {
			cleanupErr = err
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (p *ownedProcess) close() { _ = windows.CloseHandle(p.job) }

// JOBOBJECT_BASIC_ACCOUNTING_INFORMATION from winnt.h.
type jobAccounting struct {
	TotalUserTime, TotalKernelTime, ThisPeriodTotalUserTime, ThisPeriodTotalKernelTime int64
	TotalPageFaultCount, TotalProcesses, ActiveProcesses, TotalTerminatedProcesses     uint32
}
