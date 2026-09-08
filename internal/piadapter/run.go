package piadapter

import (
	"context"
	"os/exec"

	"github.com/cdowell09/herdr-pr-board/internal/agentadapter"
	"github.com/cdowell09/herdr-pr-board/internal/reviewercontract"
)

type Options struct{ Pi, Prompt, Skill string }

// Run prepares an isolated checkout and writes a validated local Pi result.
func Run(ctx context.Context, in reviewercontract.Input, opts Options) error {
	return agentadapter.Run(ctx, in, agentadapter.Options{Name: "pi", Binary: opts.Pi, Prompt: opts.Prompt, Skill: opts.Skill,
		Command: func(binary, skill, _, _ string) (*exec.Cmd, error) {
			args := []string{"--print", "--mode", "json", "--no-session", "--no-extensions", "--no-skills", "--no-context-files", "--no-approve"}
			if skill != "" {
				args = append(args, "--skill", skill)
			}
			return exec.Command(binary, args...), nil
		}, FinalText: finalText})
}
