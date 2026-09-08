//go:build !windows

package localstate

import (
	"errors"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
)

// TryLock returns an ownership descriptor. Close it to release ownership.
// Do not unlock explicitly: an inherited descriptor can still own the lock.
func TryLock(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, unix.EWOULDBLOCK) {
			return nil, ErrLocked
		}
		return nil, err
	}
	return f, nil
}

func replaceFile(source, target string) error {
	if err := os.Rename(source, target); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(target))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

// OpenRegular rejects links and special files before reading or truncating.
func OpenRegular(path string, write bool) (*os.File, error) {
	flags := unix.O_RDONLY | unix.O_NONBLOCK | unix.O_NOFOLLOW | unix.O_CLOEXEC
	if write {
		flags = unix.O_RDWR | unix.O_CREAT | unix.O_NONBLOCK | unix.O_NOFOLLOW | unix.O_CLOEXEC
	}
	fd, err := unix.Open(path, flags, 0600)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	return regularFile(f, write)
}
