package cli

import "io"

// LimitedWriter retains the first Remaining bytes and discards overflow.
// Each stream must have its own writer. Truncated is read after the process ends.
type LimitedWriter struct {
	Writer    io.Writer
	Remaining int64
	Truncated bool
}

func (w *LimitedWriter) Write(p []byte) (int, error) {
	original := len(p)
	if int64(len(p)) > w.Remaining {
		p = p[:w.Remaining]
		w.Truncated = true
	}
	n, err := w.Writer.Write(p)
	w.Remaining -= int64(n)
	if err != nil {
		return n, err
	}
	if n != len(p) {
		return n, io.ErrShortWrite
	}
	return original, nil
}
