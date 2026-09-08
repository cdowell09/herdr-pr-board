//go:build !windows

package cli

import (
	"fmt"
	"os"
	"os/exec"

	"golang.org/x/sys/unix"
)

func DuplicateFile(file *os.File) (*os.File, error) {
	fd, err := unix.FcntlInt(file.Fd(), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), file.Name()), nil
}

// PassFile exports one explicit inherited descriptor. The caller keeps file
// open through Start and closes it after the child inherits ownership.
func PassFile(cmd *exec.Cmd, file *os.File, name string) error {
	if len(cmd.ExtraFiles) != 0 {
		return fmt.Errorf("only one inherited file is supported")
	}
	cmd.ExtraFiles = []*os.File{file}
	if cmd.Env == nil {
		cmd.Env = os.Environ()
	}
	cmd.Env = append(cmd.Env, name+"=3")
	return nil
}

func InheritedFile(name string) (*os.File, error) {
	value, present := os.LookupEnv(name)
	if !present {
		return nil, nil
	}
	if value != "3" {
		return nil, fmt.Errorf("%s must be 3", name)
	}
	fd, err := unix.FcntlInt(3, unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), name), nil
}

func TakeInheritedFile(name string) (*os.File, error) {
	value := os.Getenv(name)
	if value == "" {
		return nil, nil
	}
	if value != "3" {
		return nil, fmt.Errorf("invalid %s descriptor", name)
	}
	unix.CloseOnExec(3)
	return os.NewFile(3, name), nil
}
