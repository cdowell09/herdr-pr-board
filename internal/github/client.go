package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cdowell09/herdr-pr-board/internal/config"
)

type Runner interface {
	Run(context.Context, ...string) ([]byte, error)
}

type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "gh", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			message = err.Error()
		}
		return nil, errors.New(message)
	}
	return output, nil
}

type ciCacheEntry struct {
	state     CIState
	expiresAt time.Time
}

type Client struct {
	runner Runner
	cfg    config.GitHubConfig

	loginOnce sync.Once
	login     string
	loginErr  error

	ciMu    sync.Mutex
	ciCache map[string]ciCacheEntry
	ciTTL   time.Duration
}

func NewClient(runner Runner, cfg config.GitHubConfig) *Client {
	if runner == nil {
		runner = ExecRunner{}
	}
	return &Client{
		runner:  runner,
		cfg:     cfg,
		ciCache: make(map[string]ciCacheEntry),
		ciTTL:   2 * time.Minute,
	}
}

type searchRow struct {
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	URL       string    `json:"url"`
	IsDraft   bool      `json:"isDraft"`
	UpdatedAt time.Time `json:"updatedAt"`
	Author    struct {
		Login string `json:"login"`
	} `json:"author"`
	Repository struct {
		NameWithOwner string `json:"nameWithOwner"`
		FullName      string `json:"fullName"`
		Name          string `json:"name"`
	} `json:"repository"`
}

func (c *Client) SearchView(ctx context.Context, view config.View) ([]PullRequest, error) {
	queries := []string{strings.TrimSpace(view.Query)}
	if view.Scope == config.ScopeConfigured {
		queries = queries[:0]
		for _, scope := range c.cfg.Scopes {
			resolved, err := c.resolveScope(ctx, scope)
			if err != nil {
				return nil, err
			}
			queries = append(queries, strings.TrimSpace(view.Query)+" "+resolved)
		}
	}

	byURL := make(map[string]PullRequest)
	for _, query := range queries {
		rows, err := c.search(ctx, query)
		if err != nil {
			return nil, fmt.Errorf("search %q: %w", view.Title, err)
		}
		for _, pr := range rows {
			byURL[pr.URL] = pr
		}
	}
	result := make([]PullRequest, 0, len(byURL))
	for _, pr := range byURL {
		result = append(result, pr)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].UpdatedAt.After(result[j].UpdatedAt)
	})
	return result, nil
}

func (c *Client) search(ctx context.Context, query string) ([]PullRequest, error) {
	terms, err := splitQuery(query)
	if err != nil {
		return nil, err
	}
	args := []string{
		"search", "prs",
		"--limit", strconv.Itoa(c.cfg.LimitPerScope),
		"--sort", "updated", "--order", "desc",
		"--json", "number,title,url,author,isDraft,updatedAt,repository",
		"--",
	}
	args = append(args, terms...)
	output, err := c.runner.Run(ctx, args...)
	if err != nil {
		return nil, err
	}
	var rows []searchRow
	if err := json.Unmarshal(output, &rows); err != nil {
		return nil, fmt.Errorf("decode gh search output: %w", err)
	}
	result := make([]PullRequest, 0, len(rows))
	for _, row := range rows {
		repository := row.Repository.NameWithOwner
		if repository == "" {
			repository = row.Repository.FullName
		}
		if repository == "" {
			repository = row.Repository.Name
		}
		result = append(result, PullRequest{
			Repository: repository,
			Number:     row.Number,
			Title:      row.Title,
			URL:        row.URL,
			Author:     row.Author.Login,
			Draft:      row.IsDraft,
			UpdatedAt:  row.UpdatedAt,
			CI:         CIUnknown,
		})
	}
	return result, nil
}

func (c *Client) resolveScope(ctx context.Context, scope string) (string, error) {
	if !strings.Contains(scope, "@me") {
		return scope, nil
	}
	c.loginOnce.Do(func() {
		output, err := c.runner.Run(ctx, "api", "user", "--jq", ".login")
		if err != nil {
			c.loginErr = fmt.Errorf("resolve @me: %w", err)
			return
		}
		c.login = strings.TrimSpace(string(output))
		if c.login == "" {
			c.loginErr = errors.New("resolve @me: gh returned an empty login")
		}
	})
	if c.loginErr != nil {
		return "", c.loginErr
	}
	return strings.ReplaceAll(scope, "@me", c.login), nil
}

func graphQLCapacityAvailable(rate RateResource, requests int) bool {
	return rate.Limit == 0 || rate.Remaining >= requests || !time.Now().Before(rate.Reset)
}

func (c *Client) EnrichCI(ctx context.Context, prs []PullRequest, budget RateResource) (RateResource, error) {
	if len(prs) == 0 {
		return budget, nil
	}

	now := time.Now()
	pending := make([]PullRequest, 0, len(prs))
	pendingIndexes := make([]int, 0, len(prs))
	c.ciMu.Lock()
	for i := range prs {
		entry, cached := c.ciCache[prs[i].URL]
		if cached && now.Before(entry.expiresAt) {
			prs[i].CI = entry.state
			continue
		}
		pending = append(pending, prs[i])
		pendingIndexes = append(pendingIndexes, i)
	}
	c.ciMu.Unlock()

	batchSize := c.cfg.CIBatchSize
	latest := budget
	for start := 0; start < len(pending); start += batchSize {
		remainingBatches := (len(pending) - start + batchSize - 1) / batchSize
		if !graphQLCapacityAvailable(latest, remainingBatches) {
			return latest, fmt.Errorf(
				"GraphQL rate limit has %d points remaining but CI refresh needs at least %d; CI status is stale",
				latest.Remaining,
				remainingBatches,
			)
		}
		end := min(start+batchSize, len(pending))
		rate, err := c.enrichBatch(ctx, pending[start:end])
		if err != nil {
			return latest, err
		}
		latest = rate
	}

	c.ciMu.Lock()
	defer c.ciMu.Unlock()
	for i, index := range pendingIndexes {
		prs[index].CI = pending[i].CI
		if pending[i].CI != CIUnknown {
			c.ciCache[pending[i].URL] = ciCacheEntry{state: pending[i].CI, expiresAt: now.Add(c.ciTTL)}
		}
	}
	return latest, nil
}

func (c *Client) enrichBatch(ctx context.Context, prs []PullRequest) (RateResource, error) {
	var query strings.Builder
	query.WriteString("query { rateLimit { limit remaining resetAt cost } ")
	for i, pr := range prs {
		owner, name, ok := strings.Cut(pr.Repository, "/")
		if !ok || owner == "" || name == "" {
			prs[i].CI = CIUnknown
			continue
		}
		fmt.Fprintf(&query, "p%d: repository(owner: %s, name: %s) { pullRequest(number: %d) { commits(last: 1) { nodes { commit { statusCheckRollup { state } } } } } } ", i, strconv.Quote(owner), strconv.Quote(name), pr.Number)
	}
	query.WriteString("}")

	output, err := c.runner.Run(ctx, "api", "graphql", "-f", "query="+query.String())
	if err != nil {
		return RateResource{}, fmt.Errorf("load CI checks: %w", err)
	}
	var response struct {
		Data   map[string]json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(output, &response); err != nil {
		return RateResource{}, fmt.Errorf("decode CI response: %w", err)
	}
	if len(response.Errors) > 0 && len(response.Data) == 0 {
		return RateResource{}, fmt.Errorf("load CI checks: %s", response.Errors[0].Message)
	}

	rate := decodeGraphQLRate(response.Data["rateLimit"])
	for i := range prs {
		raw, exists := response.Data[fmt.Sprintf("p%d", i)]
		if !exists || string(raw) == "null" {
			prs[i].CI = CIUnknown
			continue
		}
		prs[i].CI = decodeCI(raw)
	}
	return rate, nil
}

func decodeCI(raw json.RawMessage) CIState {
	var node struct {
		PullRequest *struct {
			Commits struct {
				Nodes []struct {
					Commit struct {
						Rollup *struct {
							State string `json:"state"`
						} `json:"statusCheckRollup"`
					} `json:"commit"`
				} `json:"nodes"`
			} `json:"commits"`
		} `json:"pullRequest"`
	}
	if err := json.Unmarshal(raw, &node); err != nil || node.PullRequest == nil || len(node.PullRequest.Commits.Nodes) == 0 {
		return CIUnknown
	}
	rollup := node.PullRequest.Commits.Nodes[0].Commit.Rollup
	if rollup == nil {
		return CINone
	}
	switch CIState(rollup.State) {
	case CISuccess, CIPending, CIFailure, CIError:
		return CIState(rollup.State)
	case "EXPECTED":
		return CIPending
	default:
		return CIUnknown
	}
}

func decodeGraphQLRate(raw json.RawMessage) RateResource {
	var value struct {
		Limit     int       `json:"limit"`
		Remaining int       `json:"remaining"`
		ResetAt   time.Time `json:"resetAt"`
	}
	if len(raw) == 0 || json.Unmarshal(raw, &value) != nil {
		return RateResource{}
	}
	return RateResource{Limit: value.Limit, Remaining: value.Remaining, Reset: value.ResetAt}
}

func (c *Client) RateLimits(ctx context.Context) (RateLimits, error) {
	output, err := c.runner.Run(ctx, "api", "rate_limit")
	if err != nil {
		return RateLimits{}, err
	}
	var response struct {
		Resources struct {
			Search struct {
				Limit     int   `json:"limit"`
				Remaining int   `json:"remaining"`
				Reset     int64 `json:"reset"`
			} `json:"search"`
			GraphQL struct {
				Limit     int   `json:"limit"`
				Remaining int   `json:"remaining"`
				Reset     int64 `json:"reset"`
			} `json:"graphql"`
		} `json:"resources"`
	}
	if err := json.Unmarshal(output, &response); err != nil {
		return RateLimits{}, fmt.Errorf("decode rate limits: %w", err)
	}
	return RateLimits{
		Search: RateResource{
			Limit: response.Resources.Search.Limit, Remaining: response.Resources.Search.Remaining,
			Reset: time.Unix(response.Resources.Search.Reset, 0),
		},
		GraphQL: RateResource{
			Limit: response.Resources.GraphQL.Limit, Remaining: response.Resources.GraphQL.Remaining,
			Reset: time.Unix(response.Resources.GraphQL.Reset, 0),
		},
	}, nil
}
