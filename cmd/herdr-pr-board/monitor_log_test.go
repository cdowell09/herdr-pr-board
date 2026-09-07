package main

import (
	"bytes"
	"strings"
	"sync"
	"testing"

	"github.com/cdowell09/herdr-pr-board/internal/cli"
	"github.com/cdowell09/herdr-pr-board/internal/monitor"
)

func TestBackgroundMonitorLogCapsConcurrentDiscoveryAndDispatch(t *testing.T) {
	var output bytes.Buffer
	log := &monitorLog{writer: cli.LimitedWriter{Writer: &output, Remaining: monitor.BackgroundLogLimit}}
	var workers sync.WaitGroup
	for _, message := range []string{"discovery failure\n", "automatic review outcome\n"} {
		workers.Add(1)
		go func(message string) {
			defer workers.Done()
			p := []byte(strings.Repeat(message, 1000))
			for i := 0; i < 100; i++ {
				if n, err := log.Write(p); err != nil || n != len(p) {
					t.Errorf("write %d: %v", n, err)
				}
			}
		}(message)
	}
	workers.Wait()
	if output.Len() != monitor.BackgroundLogLimit {
		t.Fatalf("uncapped log: %d", output.Len())
	}
}
