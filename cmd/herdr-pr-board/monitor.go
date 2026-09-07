package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/cdowell09/herdr-pr-board/internal/discovery"
	"github.com/cdowell09/herdr-pr-board/internal/monitor"
)

func runMonitor(source *monitor.Source, stderr io.Writer) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	err := source.Run(ctx, func(snapshot discovery.Snapshot) {
		for _, failure := range snapshot.Errors {
			fmt.Fprintln(stderr, "herdr-pr-board:", failure.Err)
		}
	})
	if err != nil {
		return fail(stderr, err)
	}
	return 0
}
