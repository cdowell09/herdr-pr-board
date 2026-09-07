package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/cdowell09/herdr-pr-board/internal/reviewmemory"
)

// ParsePRURL accepts the GitHub PR identity supported by the review adapters.
func ParsePRURL(value string) (string, int, error) {
	u, err := url.Parse(value)
	if err != nil || u.Scheme != "https" || u.Host != "github.com" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", 0, errors.New("use a GitHub PR URL: https://github.com/owner/repository/pull/number")
	}
	parts := strings.Split(strings.TrimSuffix(u.Path, "/"), "/")
	if len(parts) != 5 || parts[3] != "pull" {
		return "", 0, errors.New("invalid GitHub PR URL")
	}
	number, err := strconv.Atoi(parts[4])
	repo := strings.ToLower(parts[1] + "/" + parts[2])
	if err != nil || number <= 0 || reviewmemory.ValidateRepository(repo) != nil {
		return "", 0, errors.New("invalid GitHub PR identity")
	}
	return repo, number, nil
}

// CaptureRevision bypasses discovery and CI caches before a manual review launch.
func (c *Client) CaptureRevision(ctx context.Context, prURL string) (PullRequest, error) {
	repo, number, err := ParsePRURL(prURL)
	if err != nil {
		return PullRequest{}, err
	}
	data, err := c.runner(ctx, "pr", "view", strconv.Itoa(number), "--repo", repo, "--json", "number,title,url,isDraft,state,headRefOid,baseRefName,baseRefOid")
	if err != nil {
		return PullRequest{}, err
	}
	var row struct {
		Number int    `json:"number"`
		Title  string `json:"title"`
		URL    string `json:"url"`
		Draft  bool   `json:"isDraft"`
		State  string `json:"state"`
		Head   string `json:"headRefOid"`
		Base   string `json:"baseRefOid"`
		Target string `json:"baseRefName"`
	}
	if err := json.Unmarshal(data, &row); err != nil {
		return PullRequest{}, fmt.Errorf("decode PR revision: %w", err)
	}
	actualRepo, actualNumber, err := ParsePRURL(row.URL)
	if err != nil || actualRepo != repo || actualNumber != number || row.Number != number {
		return PullRequest{}, errors.New("GitHub returned a different PR identity")
	}
	if row.State != "OPEN" {
		return PullRequest{}, errors.New("PR must be open for review")
	}
	id := reviewmemory.Identity{Repository: repo, Number: number, HeadOID: row.Head, BaseRefName: row.Target}
	if err := reviewmemory.ValidateRevision(id, row.Base); err != nil {
		return PullRequest{}, err
	}
	return PullRequest{Repository: repo, Number: number, URL: row.URL, Title: row.Title, Draft: row.Draft, HeadOID: row.Head, BaseOID: row.Base, BaseRefName: row.Target, MetadataObservedAt: time.Now().UTC()}, nil
}
