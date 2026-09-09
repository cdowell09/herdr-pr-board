package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/cdowell09/herdr-pr-board/internal/antigravityadapter"
	"github.com/cdowell09/herdr-pr-board/internal/claudeadapter"
	"github.com/cdowell09/herdr-pr-board/internal/codexadapter"
	"github.com/cdowell09/herdr-pr-board/internal/copilotadapter"
	"github.com/cdowell09/herdr-pr-board/internal/cursoradapter"
	gh "github.com/cdowell09/herdr-pr-board/internal/github"
	"github.com/cdowell09/herdr-pr-board/internal/grokadapter"
	"github.com/cdowell09/herdr-pr-board/internal/hermesadapter"
	"github.com/cdowell09/herdr-pr-board/internal/kimiadapter"
	"github.com/cdowell09/herdr-pr-board/internal/localstate"
	"github.com/cdowell09/herdr-pr-board/internal/mastraadapter"
	"github.com/cdowell09/herdr-pr-board/internal/ompadapter"
	"github.com/cdowell09/herdr-pr-board/internal/piadapter"
	"github.com/cdowell09/herdr-pr-board/internal/qoderadapter"
	"github.com/cdowell09/herdr-pr-board/internal/qwenadapter"
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
		err = piadapter.Run(ctx, input, piadapter.Options{Pi: o.executable, Prompt: o.prompt, Skill: o.skill})
	case "codex":
		err = codexadapter.Run(ctx, input, codexadapter.Options{Codex: o.executable, Prompt: o.prompt, Skill: o.skill})
	case "claude":
		err = claudeadapter.Run(ctx, input, claudeadapter.Options{Claude: o.executable, Prompt: o.prompt, Skill: o.skill})
	case "kimi":
		err = kimiadapter.Run(ctx, input, kimiadapter.Options{Kimi: o.executable, Prompt: o.prompt, Skill: o.skill})
	case "qwen":
		err = qwenadapter.Run(ctx, input, qwenadapter.Options{Qwen: o.executable, Prompt: o.prompt, Skill: o.skill})
	case "omp":
		err = ompadapter.Run(ctx, input, ompadapter.Options{OMP: o.executable, Prompt: o.prompt, Skill: o.skill})
	case "qodercli":
		err = qoderadapter.Run(ctx, input, qoderadapter.Options{Qoder: o.executable, Prompt: o.prompt, Skill: o.skill})
	case "copilot":
		err = copilotadapter.Run(ctx, input, copilotadapter.Options{Copilot: o.executable, Prompt: o.prompt, Skill: o.skill})
	case "mastracode":
		err = mastraadapter.Run(ctx, input, mastraadapter.Options{MastraCode: o.executable, Prompt: o.prompt, Skill: o.skill})
	case "hermes":
		err = hermesadapter.Run(ctx, input, hermesadapter.Options{Hermes: o.executable, Prompt: o.prompt, Skill: o.skill})
	case "cursor":
		err = cursoradapter.Run(ctx, input, cursoradapter.Options{Cursor: o.executable, Prompt: o.prompt, Skill: o.skill})
	case "antigravity":
		err = antigravityadapter.Run(ctx, input, antigravityadapter.Options{Antigravity: o.executable, Prompt: o.prompt, Skill: o.skill})
	case "grok":
		err = grokadapter.Run(ctx, input, grokadapter.Options{Grok: o.executable, Prompt: o.prompt, Skill: o.skill})
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
