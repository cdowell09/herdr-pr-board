package agentadapter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/cdowell09/herdr-pr-board/internal/cli"
	"github.com/cdowell09/herdr-pr-board/internal/reviewercontract"
)

func command(ctx context.Context, dir, binary string, args ...string) ([]byte, error) {
	cmd := exec.Command(binary, args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	output := &cli.LimitedWriter{Writer: &stdout, Remaining: 4 * 1024 * 1024}
	cmd.Stdout = output
	cmd.Stderr = &cli.LimitedWriter{Writer: &stderr, Remaining: 64 * 1024}
	if err := cli.RunProcess(ctx, cmd, time.Second); err != nil {
		return stdout.Bytes(), fmt.Errorf("%s: %w: %s", binary, err, strings.TrimSpace(stderr.String()))
	}
	if output.Truncated {
		return nil, fmt.Errorf("%s output exceeds 4 MiB", binary)
	}
	return stdout.Bytes(), nil
}

func fetchContext(ctx context.Context, work string, in reviewercontract.Input, requireSpec bool) ([]json.RawMessage, error) {
	var pr struct {
		Body                    string `json:"body"`
		Title                   string `json:"title"`
		HeadRefOID              string `json:"headRefOid"`
		BaseRefOID              string `json:"baseRefOid"`
		BaseRefName             string `json:"baseRefName"`
		ClosingIssuesReferences []struct {
			URL string `json:"url"`
		} `json:"closingIssuesReferences"`
	}
	data, err := command(ctx, work, "gh", "pr", "view", strconv.Itoa(in.Identity.Number), "--repo", in.Identity.Repository, "--json", "body,title,headRefOid,baseRefOid,baseRefName,closingIssuesReferences")
	if err != nil {
		return nil, fmt.Errorf("retrieve PR specification context: %w", err)
	}
	if err := json.Unmarshal(data, &pr); err != nil {
		return nil, fmt.Errorf("decode PR specification context: %w", err)
	}
	if pr.HeadRefOID != in.Identity.HeadOID || pr.BaseRefName != in.Identity.BaseRefName || pr.BaseRefOID != in.BaseOID {
		return nil, errors.New("PR revision changed before context retrieval; capture the current revision and retry")
	}
	contextData := []json.RawMessage{data}
	hasSpec := strings.TrimSpace(pr.Body) != ""
	for _, issue := range pr.ClosingIssuesReferences {
		// URLs come from GitHub metadata and are passed as one argument, never shell text.
		data, err := fetchIssue(ctx, work, issue.URL)
		if err != nil {
			if requireSpec {
				return nil, err
			}
			data, _ = json.Marshal(struct {
				URL   string `json:"url"`
				Error string `json:"error"`
			}{issue.URL, err.Error()})
		} else {
			hasSpec = true
		}
		contextData = append(contextData, data)
	}
	if requireSpec && !hasSpec {
		return nil, errors.New("PR body and linked issues contain no specification context")
	}
	return contextData, nil
}

func fetchIssue(ctx context.Context, work, url string) ([]byte, error) {
	if !strings.HasPrefix(url, "https://github.com/") {
		return nil, errors.New("unsupported linked specification URL")
	}
	data, err := command(ctx, work, "gh", "issue", "view", url, "--json", "title,body,url")
	if err != nil {
		return nil, fmt.Errorf("retrieve linked specification: %w", err)
	}
	var spec struct {
		Body string `json:"body"`
	}
	if err := json.Unmarshal(data, &spec); err != nil {
		return nil, fmt.Errorf("decode linked specification: %w", err)
	}
	if strings.TrimSpace(spec.Body) == "" {
		return nil, errors.New("linked specification has no body")
	}
	return data, nil
}
