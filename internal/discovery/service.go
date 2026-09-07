package discovery

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/cdowell09/herdr-pr-board/internal/config"
	gh "github.com/cdowell09/herdr-pr-board/internal/github"
)

const (
	RefreshAllTimeout = 90 * time.Second
	RefreshOneTimeout = 60 * time.Second
)

// ViewData records a search observation. UpdatedAt advances only after successful searches.
// ObservedAt also records partial results from failed searches.
type ViewData struct {
	View       config.View
	PRs        []gh.PullRequest
	Err        error
	UpdatedAt  time.Time
	ObservedAt time.Time
}

type Snapshot struct {
	Views       []ViewData
	Rates       gh.RateLimits
	StartedAt   time.Time
	CapacityErr error
	Errors      []RetrievalError
	FinishedAt  time.Time
}

type ViewSnapshot struct {
	Data       ViewData
	Rates      gh.RateLimits
	StartedAt  time.Time
	Errors     []RetrievalError
	FinishedAt time.Time
}

// GitHub is the transport surface that discovery policy depends on.
type GitHub interface {
	Reconfigured(config.GitHubConfig) *gh.Client
	RateLimits(context.Context) (gh.RateLimits, error)
	SearchView(context.Context, config.View) ([]gh.PullRequest, error)
	EnrichCI(context.Context, []gh.PullRequest, gh.RateResource) (gh.RateResource, []string, error)
}

type Loader interface {
	RefreshAll(context.Context) Snapshot
	RefreshOne(context.Context, config.View) ViewSnapshot
	Reconfigured(config.Config) Loader
}

type Service struct {
	cfg    config.Config
	client GitHub
}

func NewService(cfg config.Config, client GitHub) *Service {
	return &Service{cfg: cfg, client: client}
}

func (s *Service) Reconfigured(cfg config.Config) Loader {
	return NewService(cfg, s.client.Reconfigured(cfg.GitHub))
}

func (s *Service) RefreshAll(ctx context.Context) (snapshot Snapshot) {
	defer func() { snapshot.FinishedAt = time.Now() }()
	snapshot = Snapshot{Views: make([]ViewData, len(s.cfg.Views)), StartedAt: time.Now()}
	var budgetErr error
	snapshot.Rates, budgetErr = s.searchBudget(ctx, s.cfg.SearchRequestCount(), &snapshot.Errors)
	if budgetErr != nil {
		snapshot.CapacityErr = budgetErr
		snapshot.Errors = append(snapshot.Errors, RetrievalError{Stage: "search_budget", Err: budgetErr})
		for i, view := range s.cfg.Views {
			snapshot.Views[i] = ViewData{View: view, Err: budgetErr}
		}
		return snapshot
	}

	type job struct {
		index int
		view  config.View
	}
	jobs := make(chan job)
	var wg sync.WaitGroup
	workerCount := min(s.cfg.GitHub.MaxConcurrency, len(s.cfg.Views))
	for range workerCount {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for current := range jobs {
				snapshot.Views[current.index] = s.searchView(ctx, current.view)
			}
		}()
	}
	for i, view := range s.cfg.Views {
		jobs <- job{index: i, view: view}
	}
	close(jobs)
	wg.Wait()

	for _, view := range snapshot.Views {
		if view.Err != nil {
			snapshot.Errors = append(snapshot.Errors, RetrievalError{Stage: "search", ViewID: view.View.ID, Err: view.Err})
		}
	}
	prs := uniquePRs(snapshot.Views)
	s.enrichCI(ctx, prs, &snapshot.Rates, &snapshot.Errors)
	applyCI(snapshot.Views, prs)
	return snapshot
}

func (s *Service) RefreshOne(ctx context.Context, view config.View) (result ViewSnapshot) {
	defer func() { result.FinishedAt = time.Now() }()
	result = ViewSnapshot{Data: ViewData{View: view}, StartedAt: time.Now()}
	var budgetErr error
	requests := view.SearchRequestCount(len(s.cfg.GitHub.Scopes), s.cfg.GitHub.LimitPerScope)
	result.Rates, budgetErr = s.searchBudget(ctx, requests, &result.Errors)
	if budgetErr != nil {
		result.Data.Err = budgetErr
		result.Errors = append(result.Errors, RetrievalError{Stage: "search_budget", ViewID: view.ID, Err: budgetErr})
		return result
	}

	result.Data = s.searchView(ctx, view)
	if result.Data.Err != nil {
		result.Errors = append(result.Errors, RetrievalError{Stage: "search", ViewID: view.ID, Err: result.Data.Err})
	}
	s.enrichCI(ctx, result.Data.PRs, &result.Rates, &result.Errors)
	return result
}

// searchBudget loads the current rate limits and checks that requests
// Search calls fit. An unavailable rate limit is recorded as a retrieval failure;
// it does not prevent the search.
func (s *Service) searchBudget(ctx context.Context, requests int, failures *[]RetrievalError) (gh.RateLimits, error) {
	rates, err := s.client.RateLimits(ctx)
	if err != nil {
		*failures = append(*failures, RetrievalError{Stage: "rates", Err: err})
		return rates, nil
	}
	if !rates.Search.HasCapacity(requests) {
		return rates, searchCapacityError(rates.Search, requests)
	}
	return rates, nil
}

// enrichCI refreshes the displayed rate limits after the searches, then
// loads CI status into prs in place. Enrichment failures preserve loaded rows.
// Rates refresh again after failures to include the consumed GraphQL points.
func (s *Service) enrichCI(ctx context.Context, prs []gh.PullRequest, rates *gh.RateLimits, failures *[]RetrievalError) {
	s.refreshRates(ctx, rates, failures)
	if len(prs) == 0 {
		return
	}
	graphRate, warnings, err := s.client.EnrichCI(ctx, prs, rates.GraphQL)
	if graphRate.Limit > 0 {
		if graphRate.Cost == 0 {
			graphRate.Cost = rates.GraphQL.Cost
		}
		rates.GraphQL = graphRate
	}
	for _, next := range warnings {
		*failures = append(*failures, RetrievalError{Stage: "enrichment", Err: fmt.Errorf("%s", next)})
	}
	if err != nil {
		*failures = append(*failures, RetrievalError{Stage: "enrichment", Err: err})
		s.refreshRates(ctx, rates, failures)
	}
}

// uniquePRs collects each PR once across views so CI is loaded once per PR.
func uniquePRs(views []ViewData) []gh.PullRequest {
	unique := make(map[string]gh.PullRequest)
	for _, view := range views {
		for _, pr := range view.PRs {
			unique[pr.URL] = pr
		}
	}
	prs := make([]gh.PullRequest, 0, len(unique))
	for _, pr := range unique {
		prs = append(prs, pr)
	}
	return prs
}

// applyCI copies enriched CI states back into every view that lists the PR.
func applyCI(views []ViewData, prs []gh.PullRequest) {
	ciByURL := make(map[string]gh.PullRequest, len(prs))
	for _, pr := range prs {
		ciByURL[pr.URL] = pr
	}
	for i := range views {
		for j := range views[i].PRs {
			if state, ok := ciByURL[views[i].PRs[j].URL]; ok {
				views[i].PRs[j].CopyEnrichment(state)
			}
		}
	}
}

func (s *Service) searchView(ctx context.Context, view config.View) ViewData {
	prs, err := s.client.SearchView(ctx, view)
	data := ViewData{View: view, PRs: prs, Err: err}
	if err == nil || len(prs) > 0 {
		data.ObservedAt = time.Now()
	}
	if err == nil {
		data.UpdatedAt = data.ObservedAt
	}
	return data
}

func (s *Service) refreshRates(ctx context.Context, rates *gh.RateLimits, failures *[]RetrievalError) {
	latest, err := s.client.RateLimits(ctx)
	if err != nil {
		*failures = append(*failures, RetrievalError{Stage: "rates", Err: err})
		return
	}
	// REST rate limits omit the cost reported by the last GraphQL query.
	if latest.GraphQL.Cost == 0 {
		latest.GraphQL.Cost = rates.GraphQL.Cost
	}
	*rates = latest
}

func searchCapacityError(rate gh.RateResource, requests int) error {
	return fmt.Errorf(
		"GitHub search rate limit has %d requests remaining but refresh requires %d; resets at %s",
		rate.Remaining,
		requests,
		rate.Reset.Local().Format("15:04"),
	)
}

// RetrievalError identifies the failed discovery operation.
type RetrievalError struct {
	Stage  string
	ViewID string
	Err    error
}
