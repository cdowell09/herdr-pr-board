package cli

import (
	"bytes"
	"testing"
)

func TestLimitedWriterRetainsPrefixAndConsumesOverflow(t *testing.T) {
	var output bytes.Buffer
	writer := LimitedWriter{Writer: &output, Remaining: 3}
	for _, text := range []string{"ab", "cdef", "gh"} {
		if n, err := writer.Write([]byte(text)); err != nil || n != len(text) {
			t.Fatalf("write=%d,%v", n, err)
		}
	}
	if output.String() != "abc" || !writer.Truncated || writer.Remaining != 0 {
		t.Fatalf("output=%q writer=%+v", output.String(), writer)
	}
}
