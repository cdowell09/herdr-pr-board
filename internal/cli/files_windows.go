package cli

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"syscall"

	"golang.org/x/sys/windows"
)

func DuplicateFile(file *os.File) (*os.File, error) {
	var handle windows.Handle
	err := windows.DuplicateHandle(windows.CurrentProcess(), windows.Handle(file.Fd()), windows.CurrentProcess(), &handle, 0, false, windows.DUPLICATE_SAME_ACCESS)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(handle), file.Name()), nil
}

func PassFile(cmd *exec.Cmd, file *os.File, name string) error {
	if cmd.SysProcAttr != nil && len(cmd.SysProcAttr.AdditionalInheritedHandles) != 0 {
		return fmt.Errorf("only one inherited file is supported")
	}
	if err := windows.SetHandleInformation(windows.Handle(file.Fd()), windows.HANDLE_FLAG_INHERIT, windows.HANDLE_FLAG_INHERIT); err != nil {
		return err
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.AdditionalInheritedHandles = append(cmd.SysProcAttr.AdditionalInheritedHandles, syscall.Handle(file.Fd()))
	if cmd.Env == nil {
		cmd.Env = os.Environ()
	}
	cmd.Env = append(cmd.Env, name+"="+strconv.FormatUint(uint64(file.Fd()), 10))
	return nil
}

func InheritedFile(name string) (*os.File, error) {
	value, present := os.LookupEnv(name)
	if !present {
		return nil, nil
	}
	handle, err := strconv.ParseUint(value, 10, 64)
	if err != nil || handle == 0 {
		return nil, fmt.Errorf("invalid %s handle", name)
	}
	var duplicate windows.Handle
	err = windows.DuplicateHandle(windows.CurrentProcess(), windows.Handle(handle), windows.CurrentProcess(), &duplicate, 0, false, windows.DUPLICATE_SAME_ACCESS)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(duplicate), name), nil
}

func TakeInheritedFile(name string) (*os.File, error) {
	value := os.Getenv(name)
	if value == "" {
		return nil, nil
	}
	handle, err := strconv.ParseUint(value, 10, 64)
	if err != nil || handle == 0 {
		return nil, fmt.Errorf("invalid %s handle", name)
	}
	if err := windows.SetHandleInformation(windows.Handle(handle), windows.HANDLE_FLAG_INHERIT, 0); err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(handle), name), nil
}
