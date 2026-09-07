package main

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/cdowell09/herdr-pr-board/internal/config"
	"github.com/cdowell09/herdr-pr-board/internal/discovery"
	"github.com/cdowell09/herdr-pr-board/internal/dispatch"
)

func printEligibility(cfg config.Config, source discovery.Loader, reviews dispatch.Reviews, stdout, stderr io.Writer) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, discovery.RefreshAllTimeout)
	defer cancel()
	snapshot := source.RefreshAll(ctx)
	decisions := dispatch.Decisions(dispatch.Candidates(snapshot, cfg.Views, cfg.Review.AutoViews), cfg, reviews)
	if err := json.NewEncoder(stdout).Encode(struct {
		Version    int                 `json:"version"`
		ObservedAt time.Time           `json:"observed_at"`
		Decisions  []dispatch.Decision `json:"decisions"`
	}{1, snapshot.FinishedAt, decisions}); err != nil {
		return fail(stderr, err)
	}
	for _, failure := range snapshot.Errors {
		fail(stderr, failure.Err)
	}
	if len(snapshot.Errors) > 0 {
		return 1
	}
	return 0
}
