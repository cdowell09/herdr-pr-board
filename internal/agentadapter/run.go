// Package agentadapter runs isolated reviews through agent-specific CLI adapters.
package agentadapter

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/cdowell09/herdr-pr-board/internal/cli"
	"github.com/cdowell09/herdr-pr-board/internal/localstate"
	"github.com/cdowell09/herdr-pr-board/internal/reviewercontract"
	"github.com/cdowell09/herdr-pr-board/internal/reviewinstructions"
	"github.com/cdowell09/herdr-pr-board/internal/reviewmemory"
)

type Options struct {
	Name, Binary, Prompt, Skill string
	CancelSignal                syscall.Signal
	Command                     func(binary, skill, work, checkout string) (*exec.Cmd, error)
	FinalText                   func([]byte) ([]byte, error)
}

// Run prepares an isolated checkout and writes a validated local result.
// The caller owns cancellation and the run directory containing ResultPath.
func Run(ctx context.Context, in reviewercontract.Input, opts Options) error {
	if err := in.Validate(); err != nil {
		return err
	}
	if opts.Binary == "" {
		opts.Binary = opts.Name
	}
	dir := filepath.Dir(in.ResultPath)
	work, err := os.MkdirTemp(dir, opts.Name+"-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)
	result := reviewercontract.Result{Version: reviewercontract.Version, Identity: in.Identity, BaseOID: in.BaseOID}
	blocked := func(message string) error {
		result.Outcome = reviewmemory.Outcome{Status: reviewmemory.Blocked, Message: message, Findings: []reviewmemory.Finding{}}
		return writeResult(in, result)
	}
	if strings.ContainsRune(opts.Binary, filepath.Separator) {
		opts.Binary, err = filepath.Abs(opts.Binary)
		if err != nil {
			return err
		}
	}
	files := reviewinstructions.Files{Prompt: opts.Prompt, Skill: opts.Skill}
	if selected := os.Getenv(reviewinstructions.Environment); selected != "" {
		if err := reviewercontract.Decode([]byte(selected), &files); err != nil {
			return blocked("Cannot decode configured review instructions: " + err.Error())
		}
	}
	instructions, err := files.Load("")
	if err != nil {
		return blocked("Cannot load review instructions: " + err.Error())
	}
	opts.Skill = instructions.Files.Skill
	contextData, err := fetchContext(ctx, work, in, !instructions.CustomPrompt)
	if err != nil {
		return blocked(err.Error())
	}
	checkout := filepath.Join(work, "checkout")
	if _, err := command(ctx, work, "gh", "repo", "clone", in.Identity.Repository, checkout, "--", "--no-checkout", "--config", "core.hooksPath="+os.DevNull, "--template="); err != nil {
		return err
	}
	if _, err := command(ctx, checkout, "git", "fetch", "--no-tags", "origin", in.Identity.HeadOID, in.BaseOID); err != nil {
		return err
	}
	if _, err := command(ctx, checkout, "git", "-c", "core.hooksPath="+os.DevNull, "checkout", "--detach", in.Identity.HeadOID); err != nil {
		return err
	}
	head, err := command(ctx, checkout, "git", "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(head)) != in.Identity.HeadOID {
		return fmt.Errorf("checkout HEAD does not match captured revision")
	}
	if _, err := command(ctx, checkout, "git", "merge-base", in.BaseOID, "HEAD"); err != nil {
		return blocked("Captured base and head have no available comparison: " + err.Error())
	}
	diff, err := command(ctx, checkout, "git", "--no-pager", "diff", "--no-ext-diff", "--no-textconv", in.BaseOID+"...HEAD")
	if err != nil {
		return blocked("Cannot retrieve captured diff: " + err.Error())
	}
	log, err := command(ctx, checkout, "git", "--no-pager", "log", "--no-show-signature", "--format=fuller", in.BaseOID+"..HEAD")
	if err != nil {
		return blocked("Cannot retrieve captured log: " + err.Error())
	}
	return runAgent(ctx, in, opts, work, checkout, prompt(in, contextData, instructions, diff, log))
}

func runAgent(ctx context.Context, in reviewercontract.Input, opts Options, work, checkout, prompt string) error {
	dir := filepath.Dir(in.ResultPath)
	events, err := os.OpenFile(filepath.Join(dir, opts.Name+"-events.jsonl"), os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer events.Close()
	diagnostics, err := os.OpenFile(filepath.Join(dir, opts.Name+"-stderr.log"), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer diagnostics.Close()
	cmd, err := opts.Command(opts.Binary, opts.Skill, work, checkout)
	if err != nil {
		return err
	}
	if err := cli.ResolveNodeShim(cmd); err != nil {
		return err
	}
	claim, err := inheritedClaim()
	if err != nil {
		return err
	}
	if claim != nil {
		defer claim.Close()
		if err := cli.PassFile(cmd, claim, "HERDR_REVIEW_CLAIM_FD"); err != nil {
			return err
		}
	}
	cmd.Dir = checkout
	cmd.Stdin = strings.NewReader(prompt)
	eventOutput := &cli.LimitedWriter{Writer: events, Remaining: 32 * 1024 * 1024}
	cmd.Stdout = eventOutput
	cmd.Stderr = &cli.LimitedWriter{Writer: diagnostics, Remaining: 1024 * 1024}
	if err := cli.RunProcessWithSignal(ctx, cmd, time.Second, opts.CancelSignal); err != nil {
		return fmt.Errorf("%s failed (see %s-stderr.log): %w", opts.Name, opts.Name, err)
	}
	if eventOutput.Truncated {
		return fmt.Errorf("%s event stream exceeds 32 MiB", opts.Name)
	}
	if _, err := events.Seek(0, io.SeekStart); err != nil {
		return err
	}
	data, err := io.ReadAll(io.LimitReader(events, 32*1024*1024+1))
	if err != nil {
		return err
	}
	if len(data) > 32*1024*1024 {
		return fmt.Errorf("%s event stream exceeds 32 MiB", opts.Name)
	}
	final, err := opts.FinalText(data)
	if err != nil {
		return err
	}
	var finalResult reviewercontract.Result
	if err := reviewercontract.Decode(final, &finalResult); err != nil {
		return fmt.Errorf("invalid %s review result: %w", opts.Name, err)
	}
	return writeResult(in, finalResult)
}

func writeResult(in reviewercontract.Input, result reviewercontract.Result) error {
	if err := result.Validate(in); err != nil {
		return err
	}
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	return localstate.AtomicWrite(in.ResultPath, append(data, '\n'))
}
