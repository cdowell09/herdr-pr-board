package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/cdowell09/herdr-pr-board/internal/claudeadapter"
	"github.com/cdowell09/herdr-pr-board/internal/codexadapter"
	gh "github.com/cdowell09/herdr-pr-board/internal/github"
	"github.com/cdowell09/herdr-pr-board/internal/localstate"
	"github.com/cdowell09/herdr-pr-board/internal/piadapter"
	"github.com/cdowell09/herdr-pr-board/internal/review"
	"github.com/cdowell09/herdr-pr-board/internal/reviewercontract"
	"github.com/cdowell09/herdr-pr-board/internal/reviewflow"
	"github.com/cdowell09/herdr-pr-board/internal/reviewmemory"
)

func runAdapter(o adapterOptions, stdin io.Reader, stderr io.Writer) int {
	data, err := io.ReadAll(io.LimitReader(stdin, 4*1024*1024+1))
	if err != nil {
		return fail(stderr, err)
	}
	if len(data) > 4*1024*1024 {
		return fail(stderr, fmt.Errorf("reviewer input exceeds 4 MiB"))
	}
	var input reviewercontract.Input
	if err := reviewercontract.Decode(data, &input); err != nil {
		return fail(stderr, err)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	switch o.name {
	case "pi":
		err = piadapter.Run(ctx, input, piadapter.Options{Pi: o.executable, Skill: o.skill})
	case "codex":
		err = codexadapter.Run(ctx, input, codexadapter.Options{Codex: o.executable, Skill: o.skill})
	case "claude":
		err = claudeadapter.Run(ctx, input, claudeadapter.Options{Claude: o.executable, Skill: o.skill})
	default:
		err = fmt.Errorf("unknown review adapter %q", o.name)
	}
	if err != nil {
		return fail(stderr, err)
	}
	return 0
}

func printReviewHistory(prURL string, stdout, stderr io.Writer) int {
	repo, number, err := gh.ParsePRURL(prURL)
	if err != nil {
		return fail(stderr, err)
	}
	dir, err := localstate.Dir()
	if err != nil {
		return fail(stderr, err)
	}
	store, err := reviewmemory.Open(dir)
	if err != nil {
		return fail(stderr, err)
	}
	runs, err := store.PRHistory(repo, number)
	if err != nil {
		return fail(stderr, err)
	}
	if err := json.NewEncoder(stdout).Encode(struct {
		Version int                `json:"version"`
		Runs    []reviewmemory.Run `json:"runs"`
	}{1, runs}); err != nil {
		return fail(stderr, err)
	}
	return 0
}

func printReview(o options, service reviewflow.Reviewer, publisher reviewflow.Publisher, stdout, stderr io.Writer) int {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	run, err := reviewflow.Run(ctx, service, publisher, review.Request{URL: o.review, Reviewer: o.reviewer, Rerun: o.rerun}, func(state string) { fmt.Fprintln(stderr, "review:", state) })
	if run.ID != "" {
		if writeErr := json.NewEncoder(stdout).Encode(struct {
			Version int              `json:"version"`
			Run     reviewmemory.Run `json:"run"`
		}{1, run}); writeErr != nil {
			return fail(stderr, writeErr)
		}
	}
	if err != nil {
		return fail(stderr, err)
	}
	if run.Status != reviewmemory.Completed {
		return 1
	}
	return 0
}
