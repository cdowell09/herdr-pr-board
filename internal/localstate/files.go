// Package localstate provides local process locks and atomic state replacement.
package localstate

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var ErrLocked = errors.New("state lock is held")

func Dir() (string, error) {
	dir := os.Getenv("HERDR_PLUGIN_STATE_DIR")
	if strings.TrimSpace(dir) == "" || !filepath.IsAbs(dir) {
		return "", errors.New("HERDR_PLUGIN_STATE_DIR must be an absolute path")
	}
	return dir, nil
}

func Lock(ctx context.Context, path string) (*os.File, error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		f, err := TryLock(path)
		if !errors.Is(err, ErrLocked) {
			return f, err
		}
		timer := time.NewTimer(20 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

// ReadFile permits concurrent atomic replacement while retaining one complete
// file version. It follows links, including user configuration links.
func ReadFile(path string) ([]byte, error) {
	file, err := openRead(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return io.ReadAll(file)
}

func AtomicWrite(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".state-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return replaceFile(f.Name(), path)
}

func regularFile(file *os.File, write bool) (*os.File, error) {
	info, err := file.Stat()
	if err == nil && !info.Mode().IsRegular() {
		err = errors.New("state file must be a regular file")
	}
	if err == nil && write {
		err = file.Truncate(0)
	}
	if err == nil && write {
		err = file.Chmod(0600)
	}
	if err != nil {
		file.Close()
		return nil, err
	}
	return file, nil
}
