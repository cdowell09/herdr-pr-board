package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/cdowell09/herdr-pr-board/internal/config"
	"github.com/cdowell09/herdr-pr-board/internal/discovery"
	"github.com/cdowell09/herdr-pr-board/internal/dispatch"
	"github.com/cdowell09/herdr-pr-board/internal/monitor"
)

func runMonitor(source *monitor.Source, cfg config.Config, dispatcher *dispatch.Dispatcher, stderr io.Writer) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	err := dispatcher.Run(ctx, cfg, func(ctx context.Context, report func(discovery.Snapshot)) error {
		return source.Run(ctx, func(snapshot discovery.Snapshot) {
			report(snapshot)
			for _, failure := range snapshot.Errors {
				fmt.Fprintln(stderr, "herdr-pr-board:", failure.Err)
			}
		})
	}, func(event dispatch.Event) {
		if event.Error != "" {
			fmt.Fprintln(stderr, "automatic review:", event.Decision.URL, event.Error)
		} else if event.Run != nil {
			fmt.Fprintln(stderr, "automatic review:", event.Decision.URL, event.Run.Status)
		} else {
			fmt.Fprintln(stderr, "automatic review:", event.Decision.URL, event.Decision.Reason)
		}
	})
	if err != nil {
		return fail(stderr, err)
	}
	return 0
}
