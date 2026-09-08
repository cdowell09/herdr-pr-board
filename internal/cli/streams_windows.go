package cli

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"reflect"
	"time"

	"golang.org/x/sys/windows"
)

type processStreams struct {
	handles     [3]windows.Handle
	childFiles  []*os.File
	parentFiles []*os.File
	copies      []func() error
	done        chan error
}

func prepareStreams(cmd *exec.Cmd) (_ *processStreams, err error) {
	streams := &processStreams{}
	defer func() {
		if err != nil {
			streams.closeChildHandles()
			streams.close()
		}
	}()
	input, err := streams.input(cmd.Stdin)
	if err != nil {
		return nil, err
	}
	output, err := streams.output(cmd.Stdout)
	if err != nil {
		return nil, err
	}
	diagnostic := output
	if cmd.Stdout == nil || !reflect.TypeOf(cmd.Stdout).Comparable() || cmd.Stdout != cmd.Stderr {
		diagnostic, err = streams.output(cmd.Stderr)
		if err != nil {
			return nil, err
		}
	}
	for i, file := range []*os.File{input, output, diagnostic} {
		if err = windows.DuplicateHandle(windows.CurrentProcess(), windows.Handle(file.Fd()), windows.CurrentProcess(), &streams.handles[i], 0, true, windows.DUPLICATE_SAME_ACCESS); err != nil {
			return nil, err
		}
	}
	return streams, nil
}

func (s *processStreams) input(reader io.Reader) (*os.File, error) {
	if file, ok := reader.(*os.File); ok {
		return file, nil
	}
	if reader == nil {
		file, err := os.Open(os.DevNull)
		if err == nil {
			s.childFiles = append(s.childFiles, file)
		}
		return file, err
	}
	read, write, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	s.childFiles = append(s.childFiles, read)
	s.parentFiles = append(s.parentFiles, write)
	s.copies = append(s.copies, func() error {
		_, err := io.Copy(write, reader)
		write.Close()
		if errors.Is(err, windows.ERROR_BROKEN_PIPE) || errors.Is(err, windows.ERROR_NO_DATA) {
			return nil
		}
		return err
	})
	return read, nil
}

func (s *processStreams) output(writer io.Writer) (*os.File, error) {
	if file, ok := writer.(*os.File); ok {
		return file, nil
	}
	if writer == nil {
		file, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
		if err == nil {
			s.childFiles = append(s.childFiles, file)
		}
		return file, err
	}
	read, write, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	s.childFiles = append(s.childFiles, write)
	s.parentFiles = append(s.parentFiles, read)
	s.copies = append(s.copies, func() error { defer read.Close(); _, err := io.Copy(writer, read); return err })
	return write, nil
}

func (s *processStreams) start() {
	s.done = make(chan error, len(s.copies))
	for _, copy := range s.copies {
		go func() { s.done <- copy() }()
	}
}

func (s *processStreams) wait() error {
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	var result error
	for remaining := len(s.copies); remaining > 0; remaining-- {
		select {
		case err := <-s.done:
			result = errors.Join(result, err)
		case <-timer.C:
			s.close()
			for ; remaining > 0; remaining-- {
				<-s.done
			}
			return errors.Join(result, exec.ErrWaitDelay)
		}
	}

	return result
}

func (s *processStreams) closeChildHandles() {
	for i, handle := range s.handles {
		if handle != 0 {
			windows.CloseHandle(handle)
			s.handles[i] = 0
		}
	}
	for _, file := range s.childFiles {
		file.Close()
	}
	s.childFiles = nil
}
func (s *processStreams) close() {
	for _, file := range s.parentFiles {
		file.Close()
	}
}
