package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cdowell09/herdr-pr-board/internal/cli"
	"github.com/cdowell09/herdr-pr-board/internal/config"
)

// Runner executes gh. See cli.Runner for the stdout and error contract.
type Runner = cli.Runner

// TokenVars lists the environment variables gh reads before its stored
// keyring login, in the order gh checks them. A value in either variable
// overrides a valid keyring login.
var TokenVars = []string{"GH_TOKEN", "GITHUB_TOKEN"}

type ciCacheEntry struct {
	pr        PullRequest
	expiresAt time.Time
}

type Client struct {
	runner     Runner
	baseRunner Runner
	tokenVars  []string
	cfg        config.GitHubConfig

	loginMu sync.Mutex
	login   string

	ciMu    sync.Mutex
	ciCache map[string]ciCacheEntry
	ciTTL   time.Duration

	searchSem chan struct{}
}

func NewClient(runner Runner, cfg config.GitHubConfig) *Client {
	if runner == nil {
		runner = cli.Command("gh")
	}
	capacity := cfg.MaxConcurrency
	if capacity < 1 {
		capacity = 1
	}
	return &Client{
		runner:     withAuthHint(runner, nil),
		baseRunner: runner,
		cfg:        cfg,
		ciCache:    make(map[string]ciCacheEntry),
		ciTTL:      2 * time.Minute,
		searchSem:  make(chan struct{}, capacity),
	}
}

// Reconfigured returns a client with the same base command runner and new
// settings. It carries over the recorded token variables and wraps the base
// runner with the auth hint exactly once; it never wraps the already-wrapped
// runner, which would grow a new no-op layer on every reconfiguration.
func (c *Client) Reconfigured(cfg config.GitHubConfig) *Client {
	next := NewClient(c.baseRunner, cfg)
	next.SetTokenVars(c.tokenVars)
	return next
}

// SetTokenVars records which of the variables in TokenVars are set in the
// process environment. The github package does not read the environment
// itself; the caller checks TokenVars with os.Getenv and passes the names
// that are set. When a gh command fails with an authentication error, the
// client replaces the raw gh error with a short message naming the fix, and
// appends a hint naming the variable when a recorded variable is set.
func (c *Client) SetTokenVars(set []string) {
	c.tokenVars = append([]string(nil), set...)
	c.runner = withAuthHint(c.baseRunner, c.tokenVars)
}

// authFailedMessage replaces the raw gh error text on an authentication
// failure. The raw text can embed an HTTP response body with escaped line
// breaks; this message names the fix instead.
const authFailedMessage = "GitHub authentication failed. Run: gh auth login"

// withAuthHint wraps runner so an authentication failure replaces the raw gh
// error, which can embed an escaped HTTP response body, with a short message
// naming the fix. It appends a hint naming the overriding environment
// variable when one is set. It passes stdout through unchanged and only
// replaces the error.
func withAuthHint(runner Runner, tokenVars []string) Runner {
	return func(ctx context.Context, args ...string) ([]byte, error) {
		output, err := runner(ctx, args...)
		if err != nil && isAuthError(err) {
			err = authError(tokenVars)
		}
		return output, err
	}
}

// authError builds the replacement authentication error, adding the
// environment token hint when a token variable overrides the gh keyring
// login.
func authError(tokenVars []string) error {
	if len(tokenVars) == 0 {
		return errors.New(authFailedMessage)
	}
	return fmt.Errorf("%s; %s", authFailedMessage, tokenHintSentence(tokenVars))
}

// isAuthError reports whether err looks like a gh authentication failure.
// This covers a stored login rejected by GitHub ("bad credentials", an HTTP
// 401) and gh finding no login at all, keyring or environment token ("to get
// started with github cli").
func isAuthError(err error) bool {
	lower := strings.ToLower(err.Error())
	return strings.Contains(lower, "bad credentials") ||
		strings.Contains(lower, "http 401") ||
		strings.Contains(lower, "to get started with github cli")
}

// tokenHintSentence names the environment variables that override the gh
// keyring login, with grammar adjusted for one name versus more than one.
func tokenHintSentence(tokenVars []string) string {
	if len(tokenVars) == 1 {
		return fmt.Sprintf("%s is set and overrides the gh keyring login; unset it or replace it with a valid token", tokenVars[0])
	}
	names := strings.Join(tokenVars, " and ")
	return fmt.Sprintf("%s are set and override the gh keyring login; unset them or replace them with valid tokens", names)
}

type searchRow struct {
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	URL       string    `json:"url"`
	State     string    `json:"state"`
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

func parsePRState(value string) PRState {
	state := PRState(strings.ToUpper(value))
	switch state {
	case PROpen, PRClosed, PRMerged:
		return state
	default:
		return ""
	}
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

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make([][]PullRequest, len(queries))
	var errMu sync.Mutex
	var firstErr error
	var wg sync.WaitGroup
	for i, query := range queries {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rows, err := c.search(ctx, query)
			if err != nil {
				errMu.Lock()
				shouldCancel := firstErr == nil
				if shouldCancel {
					firstErr = err
				}
				errMu.Unlock()
				if shouldCancel {
					cancel()
				}
				return
			}
			results[i] = rows
		}()
	}
	wg.Wait()

	byURL := make(map[string]PullRequest)
	for _, rows := range results {
		for _, pr := range rows {
			if existing, ok := byURL[pr.URL]; !ok || pr.UpdatedAt.After(existing.UpdatedAt) {
				byURL[pr.URL] = pr
			}
		}
	}
	result := make([]PullRequest, 0, len(byURL))
	for _, pr := range byURL {
		result = append(result, pr)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].UpdatedAt.After(result[j].UpdatedAt)
	})
	if firstErr != nil {
		return result, fmt.Errorf("search %q: %w", view.Title, firstErr)
	}
	return result, nil
}

func (c *Client) search(ctx context.Context, query string) ([]PullRequest, error) {
	select {
	case c.searchSem <- struct{}{}:
		defer func() { <-c.searchSem }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	terms, err := splitQuery(query)
	if err != nil {
		return nil, err
	}
	args := []string{
		"search", "prs",
		"--limit", strconv.Itoa(c.cfg.LimitPerScope),
		"--sort", "updated", "--order", "desc",
		"--json", "number,title,url,author,isDraft,updatedAt,repository,state",
		"--",
	}
	args = append(args, terms...)
	output, err := c.runner(ctx, args...)
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
			State:      parsePRState(row.State),
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
	login, err := c.currentLogin(ctx)
	if err != nil {
		return "", err
	}
	return strings.ReplaceAll(scope, "@me", login), nil
}

// currentLogin returns the authenticated user's login. It caches the value
// only after a successful lookup, so a transient failure such as a context
// timeout is retried on the next refresh instead of poisoning the session.
func (c *Client) currentLogin(ctx context.Context) (string, error) {
	c.loginMu.Lock()
	defer c.loginMu.Unlock()
	if c.login != "" {
		return c.login, nil
	}
	output, err := c.runner(ctx, "api", "user", "--jq", ".login")
	if err != nil {
		return "", fmt.Errorf("resolve @me: %w", err)
	}
	login := strings.TrimSpace(string(output))
	if login == "" {
		return "", errors.New("resolve @me: gh returned an empty login")
	}
	c.login = login
	return login, nil
}

func (c *Client) EnrichCI(ctx context.Context, prs []PullRequest, budget RateResource) (RateResource, []string, error) {
	if len(prs) == 0 {
		return budget, nil, nil
	}

	now := time.Now()
	pending := make([]PullRequest, 0, len(prs))
	pendingIndexes := make([]int, 0, len(prs))
	c.ciMu.Lock()
	for i := range prs {
		entry, cached := c.ciCache[prs[i].URL]
		if cached && now.Before(entry.expiresAt) {
			prs[i].CopyEnrichment(entry.pr)
			continue
		}
		prs[i].CopyEnrichment(PullRequest{CI: CIUnknown})
		pending = append(pending, prs[i])
		pendingIndexes = append(pendingIndexes, i)
	}
	c.ciMu.Unlock()

	batchSize := c.cfg.CIBatchSize
	latest := budget
	var warnings []string
	actor := ""
	if len(pending) > 0 {
		var err error
		actor, err = c.currentLogin(ctx)
		if err != nil {
			warnings = append(warnings, "load viewer reviews: "+err.Error())
		}
	}
	applyBatch := func(batch []PullRequest, indexes []int) {
		for i, index := range indexes {
			prs[index].CopyEnrichment(batch[i])
		}
		c.ciMu.Lock()
		for url, entry := range c.ciCache {
			if !now.Before(entry.expiresAt) {
				delete(c.ciCache, url)
			}
		}
		for _, pr := range batch {
			if pr.ViewerReviews != nil && pr.ViewerReviews.Complete && pr.CI != CIUnknown && pr.HeadOID != "" && pr.BaseRefName != "" && pr.BaseOID != "" && !pr.MetadataObservedAt.IsZero() {
				c.ciCache[pr.URL] = ciCacheEntry{pr: pr, expiresAt: pr.MetadataObservedAt.Add(c.ciTTL)}
			}
		}
		c.ciMu.Unlock()
	}
	for start := 0; start < len(pending); start += batchSize {
		remainingBatches := (len(pending) - start + batchSize - 1) / batchSize
		required := remainingBatches * latest.CostPerQuery()
		if !latest.HasCapacity(required) {
			return latest, warnings, fmt.Errorf(
				"GraphQL rate limit has %d points remaining but CI refresh needs at least %d; CI status is stale",
				latest.Remaining,
				required,
			)
		}
		end := min(start+batchSize, len(pending))
		rate, batchWarnings, err := c.enrichBatch(ctx, pending[start:end], actor, latest)
		warnings = append(warnings, batchWarnings...)
		if rate.Limit > 0 {
			latest = rate
		}
		applyBatch(pending[start:end], pendingIndexes[start:end])
		if err != nil {
			return latest, warnings, err
		}
	}
	return latest, warnings, nil
}

func (c *Client) enrichBatch(ctx context.Context, prs []PullRequest, actor string, budget RateResource) (RateResource, []string, error) {
	var query strings.Builder
	query.WriteString("query { rateLimit { limit remaining resetAt cost } ")
	for i, pr := range prs {
		owner, name, ok := strings.Cut(pr.Repository, "/")
		if !ok || owner == "" || name == "" {
			prs[i].CI = CIUnknown
			continue
		}
		fmt.Fprintf(&query, "p%d: repository(owner: %s, name: %s) { pullRequest(number: %d) { %s headRefOid baseRefName baseRefOid commits(last: 1) { nodes { commit { statusCheckRollup { state } } } } } } ", i, strconv.Quote(owner), strconv.Quote(name), pr.Number, reviewSelection(actor, ""))
	}
	query.WriteString("}")

	output, runErr := c.runner(ctx, "api", "graphql", "-f", "query="+query.String())
	var response graphQLResponse
	decodeErr := json.Unmarshal(output, &response)
	if runErr != nil && (decodeErr != nil || len(response.Data) == 0) {
		// gh exits non-zero when the response carries errors but still prints
		// the body. Without usable data the exit error is all there is.
		return RateResource{}, nil, fmt.Errorf("load CI checks: %w", runErr)
	}
	if decodeErr != nil {
		return RateResource{}, nil, fmt.Errorf("decode CI response: %w", decodeErr)
	}
	var warnings []string
	switch {
	case len(response.Errors) > 0:
		if len(response.Data) == 0 {
			return RateResource{}, nil, fmt.Errorf("load CI checks: %s", response.Errors[0].Message)
		}
		for _, graphErr := range response.Errors {
			warnings = append(warnings, "load CI checks: "+graphErr.Message)
		}
	case runErr != nil:
		warnings = append(warnings, "load CI checks: "+runErr.Error())
	}

	rate := decodeGraphQLRate(response.Data["rateLimit"])
	observedAt := time.Now()
	for i := range prs {
		raw, exists := response.Data[fmt.Sprintf("p%d", i)]
		if !exists || string(raw) == "null" {
			prs[i].CI = CIUnknown
			if len(response.Errors) == 0 {
				warnings = append(warnings, fmt.Sprintf("load PR metadata: %s#%d is unavailable", prs[i].Repository, prs[i].Number))
			}
			continue
		}
		prs[i].CopyEnrichment(decodeEnrichment(raw, observedAt))
		if len(response.Errors) == 0 && (prs[i].HeadOID == "" || prs[i].BaseRefName == "" || prs[i].BaseOID == "" || prs[i].CI == CIUnknown) {
			warnings = append(warnings, fmt.Sprintf("load PR metadata: %s#%d has unavailable revision or CI data", prs[i].Repository, prs[i].Number))
		}
		for _, graphErr := range response.Errors {
			if len(graphErr.Path) > 0 && graphErr.Path[0] == fmt.Sprintf("p%d", i) {
				for _, field := range graphErr.Path {
					if field == "commits" {
						prs[i].CI = CIUnknown
					}
				}
			}
		}
	}
	if rate.Limit > 0 {
		budget = rate
	}
	budget, reviewWarnings, err := c.enrichReviews(ctx, prs, actor, response, budget, observedAt)
	warnings = append(warnings, reviewWarnings...)
	return budget, warnings, err
}

// graphQLResponse is the envelope gh api graphql prints. A response can carry
// both data and errors when some aliased repositories resolve and others do not.
type graphQLResponse struct {
	Data   map[string]json.RawMessage `json:"data"`
	Errors []struct {
		Message string `json:"message"`
		Path    []any  `json:"path"`
	} `json:"errors"`
}

func decodeEnrichment(raw json.RawMessage, observedAt time.Time) PullRequest {
	var node struct {
		PullRequest *struct {
			HeadOID     string `json:"headRefOid"`
			BaseRefName string `json:"baseRefName"`
			BaseOID     string `json:"baseRefOid"`
			Commits     struct {
				Nodes []struct {
					Commit struct {
						Rollup json.RawMessage `json:"statusCheckRollup"`
					} `json:"commit"`
				} `json:"nodes"`
			} `json:"commits"`
		} `json:"pullRequest"`
	}
	if err := json.Unmarshal(raw, &node); err != nil || node.PullRequest == nil {
		return PullRequest{CI: CIUnknown}
	}
	pr := PullRequest{CI: CIUnknown, HeadOID: node.PullRequest.HeadOID, BaseRefName: node.PullRequest.BaseRefName, BaseOID: node.PullRequest.BaseOID, MetadataObservedAt: observedAt}
	if len(node.PullRequest.Commits.Nodes) == 0 {
		return pr
	}
	rawRollup := node.PullRequest.Commits.Nodes[0].Commit.Rollup
	if string(rawRollup) == "null" {
		pr.CI = CINone
		return pr
	}
	var rollup struct {
		State string `json:"state"`
	}
	if json.Unmarshal(rawRollup, &rollup) != nil {
		return pr
	}
	switch CIState(rollup.State) {
	case CISuccess, CIPending, CIFailure, CIError:
		pr.CI = CIState(rollup.State)
	case "EXPECTED":
		pr.CI = CIPending
	}
	return pr
}

func decodeGraphQLRate(raw json.RawMessage) RateResource {
	var value struct {
		Limit     int       `json:"limit"`
		Remaining int       `json:"remaining"`
		ResetAt   time.Time `json:"resetAt"`
		Cost      int       `json:"cost"`
	}
	if len(raw) == 0 || json.Unmarshal(raw, &value) != nil {
		return RateResource{}
	}
	return RateResource{Limit: value.Limit, Remaining: value.Remaining, Reset: value.ResetAt, Cost: value.Cost}
}

func (c *Client) RateLimits(ctx context.Context) (RateLimits, error) {
	output, err := c.runner(ctx, "api", "rate_limit")
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
