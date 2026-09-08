package cli

import (
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

// PROC_THREAD_ATTRIBUTE_JOB_LIST assigns the job atomically with process
// creation. A wrapper crash cannot strand an unowned suspended child or claim.
const jobListAttribute = 0x0002000d

func createOwned(cmd *exec.Cmd) (_ *ownedProcess, err error) {
	if cmd.Err != nil {
		return nil, cmd.Err
	}
	if cmd.Cancel != nil || len(cmd.ExtraFiles) != 0 {
		return nil, errors.New("owned process requires exec.Command and explicit inherited handles")
	}
	var extra []windows.Handle
	if attrs := cmd.SysProcAttr; attrs != nil {
		if attrs.HideWindow || attrs.CmdLine != "" || attrs.CreationFlags&^windows.CREATE_NEW_PROCESS_GROUP != 0 || attrs.Token != 0 || attrs.ProcessAttributes != nil || attrs.ThreadAttributes != nil || attrs.NoInheritHandles || attrs.ParentProcess != 0 {
			return nil, errors.New("unsupported owned-process attributes")
		}
		for _, handle := range attrs.AdditionalInheritedHandles {
			extra = append(extra, windows.Handle(handle))
		}
	}
	path, err := filepath.Abs(cmd.Path)
	if err != nil {
		return nil, err
	}
	app, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	args, err := windows.UTF16PtrFromString(windows.ComposeCommandLine(cmd.Args))
	if err != nil {
		return nil, err
	}
	var dir *uint16
	if cmd.Dir != "" {
		dir, err = windows.UTF16PtrFromString(cmd.Dir)
		if err != nil {
			return nil, err
		}
	}
	env := cmd.Environ()
	for _, entry := range env {
		if strings.ContainsRune(entry, 0) {
			return nil, errors.New("environment contains a NUL character")
		}
	}
	environment := utf16.Encode([]rune(strings.Join(env, "\x00") + "\x00\x00"))
	owner := &ownedProcess{}
	defer func() {
		if err != nil {
			owner.close()
		}
	}()
	owner.job, err = windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, err
	}
	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err = windows.SetInformationJobObject(owner.job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits))); err != nil {
		return nil, err
	}
	owner.streams, err = prepareStreams(cmd)
	if err != nil {
		return nil, err
	}
	defer owner.streams.closeChildHandles()
	attributes, err := windows.NewProcThreadAttributeList(2)
	if err != nil {
		return nil, err
	}
	defer attributes.Delete()
	handles := append(append([]windows.Handle{}, owner.streams.handles[:]...), extra...)
	if err = attributes.Update(windows.PROC_THREAD_ATTRIBUTE_HANDLE_LIST, unsafe.Pointer(&handles[0]), uintptr(len(handles))*unsafe.Sizeof(handles[0])); err != nil {
		return nil, err
	}
	if err = attributes.Update(jobListAttribute, unsafe.Pointer(&owner.job), unsafe.Sizeof(owner.job)); err != nil {
		return nil, err
	}
	startup := windows.StartupInfoEx{}
	startup.Cb = uint32(unsafe.Sizeof(startup))
	startup.Flags = windows.STARTF_USESTDHANDLES
	startup.StdInput, startup.StdOutput, startup.StdErr = owner.streams.handles[0], owner.streams.handles[1], owner.streams.handles[2]
	startup.ProcThreadAttributeList = attributes.List()
	var process windows.ProcessInformation
	flags := uint32(windows.CREATE_UNICODE_ENVIRONMENT | windows.EXTENDED_STARTUPINFO_PRESENT | windows.CREATE_SUSPENDED | windows.CREATE_NEW_PROCESS_GROUP)
	if err = windows.CreateProcess(app, args, nil, nil, true, flags, &environment[0], dir, &startup.StartupInfo, &process); err != nil {
		return nil, err
	}
	owner.process, owner.thread, owner.pid = process.Process, process.Thread, process.ProcessId
	owner.streams.start()
	return owner, nil
}
