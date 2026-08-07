package board

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/cdowell09/herdr-pr-board/internal/config"
	gh "github.com/cdowell09/herdr-pr-board/internal/github"
)

type ViewData struct {
	View config.View
	PRs  []gh.PullRequest
	Err  error
}

type Snapshot struct {
	Views     []ViewData
	Rates     gh.RateLimits
	Warning   string
	UpdatedAt time.Time
}

type ViewSnapshot struct {
	Data      ViewData
	Rates     gh.RateLimits
	Warning   string
	UpdatedAt time.Time
}

type Loader interface {
	RefreshAll(context.Context) Snapshot
	RefreshOne(context.Context, config.View) ViewSnapshot
}

type Service struct {
	cfg    config.Config
	client *gh.Client
}

func NewService(cfg config.Config, client *gh.Client) *Service {
	return &Service{cfg: cfg, client: client}
}

func (s *Service) RefreshAll(ctx context.Context) Snapshot {
	snapshot := Snapshot{Views: make([]ViewData, len(s.cfg.Views)), UpdatedAt: time.Now()}
	rates, rateErr := s.client.RateLimits(ctx)
	snapshot.Rates = rates
	if rateErr != nil {
		snapshot.Warning = "rate limits unavailable: " + rateErr.Error()
	}
	requests := s.cfg.SearchRequestCount()
	if rateErr == nil && !searchCapacityAvailable(rates.Search, requests) {
		err := searchCapacityError(rates.Search, requests)
		for i, view := range s.cfg.Views {
			snapshot.Views[i] = ViewData{View: view, Err: err}
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

	s.refreshRates(ctx, &snapshot.Rates, &snapshot.Warning)
	unique := make(map[string]gh.PullRequest)
	for _, view := range snapshot.Views {
		for _, pr := range view.PRs {
			unique[pr.URL] = pr
		}
	}
	prs := make([]gh.PullRequest, 0, len(unique))
	for _, pr := range unique {
		prs = append(prs, pr)
	}
	if len(prs) > 0 && graphQLCapacityAvailable(snapshot.Rates.GraphQL) {
		graphRate, err := s.client.EnrichCI(ctx, prs)
		if err != nil {
			snapshot.Warning = appendWarning(snapshot.Warning, err.Error())
		} else if graphRate.Limit > 0 {
			snapshot.Rates.GraphQL = graphRate
		}
	} else if len(prs) > 0 {
		snapshot.Warning = appendWarning(snapshot.Warning, "GraphQL rate limit exhausted; CI status is stale")
	}

	ciByURL := make(map[string]gh.CIState, len(prs))
	for _, pr := range prs {
		ciByURL[pr.URL] = pr.CI
	}
	for i := range snapshot.Views {
		for j := range snapshot.Views[i].PRs {
			if state, ok := ciByURL[snapshot.Views[i].PRs[j].URL]; ok {
				snapshot.Views[i].PRs[j].CI = state
			}
		}
	}
	return snapshot
}

func (s *Service) RefreshOne(ctx context.Context, view config.View) ViewSnapshot {
	result := ViewSnapshot{Data: ViewData{View: view}, UpdatedAt: time.Now()}
	rates, rateErr := s.client.RateLimits(ctx)
	result.Rates = rates
	if rateErr != nil {
		result.Warning = "rate limits unavailable: " + rateErr.Error()
	}
	requests := view.SearchRequestCount(len(s.cfg.GitHub.Scopes))
	if rateErr == nil && !searchCapacityAvailable(rates.Search, requests) {
		result.Data.Err = searchCapacityError(rates.Search, requests)
		return result
	}

	result.Data = s.searchView(ctx, view)
	s.refreshRates(ctx, &result.Rates, &result.Warning)
	if result.Data.Err != nil || len(result.Data.PRs) == 0 {
		return result
	}
	if !graphQLCapacityAvailable(result.Rates.GraphQL) {
		result.Warning = appendWarning(result.Warning, "GraphQL rate limit exhausted; CI status is stale")
		return result
	}
	graphRate, err := s.client.EnrichCI(ctx, result.Data.PRs)
	if err != nil {
		result.Warning = appendWarning(result.Warning, "PRs loaded; CI unavailable: "+err.Error())
	} else if graphRate.Limit > 0 {
		result.Rates.GraphQL = graphRate
	}
	return result
}

func (s *Service) searchView(ctx context.Context, view config.View) ViewData {
	prs, err := s.client.SearchView(ctx, view)
	return ViewData{View: view, PRs: prs, Err: err}
}

func (s *Service) refreshRates(ctx context.Context, rates *gh.RateLimits, warning *string) {
	latest, err := s.client.RateLimits(ctx)
	if err != nil {
		*warning = appendWarning(*warning, "updated rate limits unavailable: "+err.Error())
		return
	}
	*rates = latest
}

func searchCapacityAvailable(rate gh.RateResource, requests int) bool {
	return rate.Limit == 0 || rate.Remaining >= requests || !time.Now().Before(rate.Reset)
}

func searchCapacityError(rate gh.RateResource, requests int) error {
	return fmt.Errorf(
		"GitHub search rate limit has %d requests remaining but refresh requires %d; resets at %s",
		rate.Remaining,
		requests,
		rate.Reset.Local().Format("15:04"),
	)
}

func graphQLCapacityAvailable(rate gh.RateResource) bool {
	return rate.Limit == 0 || rate.Remaining > 0 || !time.Now().Before(rate.Reset)
}

func appendWarning(current, next string) string {
	if current == "" {
		return next
	}
	return current + "; " + next
}
