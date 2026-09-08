package review

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/cdowell09/herdr-pr-board/internal/cli"
	"github.com/cdowell09/herdr-pr-board/internal/config"
	gh "github.com/cdowell09/herdr-pr-board/internal/github"
	"github.com/cdowell09/herdr-pr-board/internal/localstate"
	"github.com/cdowell09/herdr-pr-board/internal/reviewercontract"
	"github.com/cdowell09/herdr-pr-board/internal/reviewmemory"
)

const maxResultBytes = 4 * 1024 * 1024

func (s *Service) execute(ctx context.Context, claim *reviewmemory.Claim, pr gh.PullRequest, reviewer config.Reviewer, request Request) (reviewmemory.Outcome, error) {
	dir := s.RunDirectory(claim.ID())
	var stderr *os.File
	failed := func(err error) (reviewmemory.Outcome, error) {
		message := err.Error()
		if stderr != nil {
			data := make([]byte, 4096)
			if n, _ := stderr.ReadAt(data, 0); n > 0 {
				message += ": " + strings.TrimSpace(string(data[:n]))
			}
		}
		message += "; diagnostics: " + dir
		return reviewmemory.Outcome{Status: reviewmemory.Failed, Message: message}, err
	}
	if err := os.Mkdir(dir, 0700); err != nil {
		return failed(err)
	}
	in := reviewercontract.Input{Version: reviewercontract.Version, Identity: reviewmemory.Identity{Repository: pr.Repository, Number: pr.Number, HeadOID: pr.HeadOID, BaseRefName: pr.BaseRefName}, BaseOID: pr.BaseOID, PRURL: pr.URL, Title: pr.Title, ResultPath: filepath.Join(dir, "result.json")}
	if err := in.Validate(); err != nil {
		return failed(err)
	}
	data, err := json.Marshal(in)
	if err != nil {
		return failed(err)
	}
	if err := localstate.AtomicWrite(filepath.Join(dir, "input.json"), data); err != nil {
		return failed(err)
	}
	stdout, err := os.OpenFile(filepath.Join(dir, "stdout.log"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return failed(err)
	}
	defer stdout.Close()
	stderr, err = os.OpenFile(filepath.Join(dir, "stderr.log"), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		return failed(err)
	}
	defer stderr.Close()
	fd, err := claim.LockFile()
	if err != nil {
		return failed(err)
	}
	defer fd.Close()
	currentConfig, err := config.LoadExisting(s.configPath)
	if err != nil {
		return failed(err)
	}
	if err := request.validateAutomatic(pr, currentConfig); err != nil {
		return failed(err)
	}
	currentReviewer, err := currentConfig.ResolveLaunch(pr.Repository, request.Reviewer, request.Automatic)
	if err != nil {
		return failed(err)
	}
	if currentReviewer.ID != reviewer.ID || !slices.Equal(currentReviewer.Command, reviewer.Command) {
		return failed(fmt.Errorf("reviewer configuration changed before launch; retry the review"))
	}
	cmd := exec.Command(reviewer.Command[0], reviewer.Command[1:]...)
	cmd.Stdin = bytes.NewReader(data)
	cmd.Stdout = &cli.LimitedWriter{Writer: stdout, Remaining: 1024 * 1024}
	cmd.Stderr = &cli.LimitedWriter{Writer: stderr, Remaining: 1024 * 1024}
	if err := cli.PassFile(cmd, fd, "HERDR_REVIEW_CLAIM_FD"); err != nil {
		return failed(err)
	}
	if err := cli.RunProcess(ctx, cmd, 3*time.Second); err != nil {
		return failed(fmt.Errorf("reviewer execution: %w", err))
	}
	resultData, err := readResult(in.ResultPath)
	if err != nil {
		return failed(err)
	}
	var result reviewercontract.Result
	if err := reviewercontract.Decode(resultData, &result); err != nil {
		return failed(fmt.Errorf("invalid reviewer result: %w", err))
	}
	if err := result.Validate(in); err != nil {
		return failed(err)
	}
	return result.Outcome, nil
}

func readResult(path string) ([]byte, error) {
	f, err := localstate.OpenRegular(path, false)
	if err != nil {
		return nil, fmt.Errorf("read reviewer result: %w", err)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxResultBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxResultBytes || strings.TrimSpace(string(data)) == "" {
		return nil, fmt.Errorf("reviewer result must contain JSON within 4 MiB")
	}
	return data, nil
}
