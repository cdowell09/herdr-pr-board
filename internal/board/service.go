package board

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/cdowell09/herdr-pr-board/internal/config"
	gh "github.com/cdowell09/herdr-pr-board/internal/github"
)

type ViewData struct {
	View      config.View
	PRs       []gh.PullRequest
	Err       error
	UpdatedAt time.Time
}

// Stale reports whether the view kept rows from an earlier refresh after a failed one.
func (v ViewData) Stale() bool {
	return v.Err != nil && len(v.PRs) > 0
}

// retainFrom keeps the previous view's rows and freshness after a failed refresh.
func (v *ViewData) retainFrom(prev ViewData) {
	if len(v.PRs) == 0 && len(prev.PRs) > 0 {
		v.PRs = prev.PRs
	}
	v.UpdatedAt = prev.UpdatedAt
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

// GitHub is the transport surface that board refresh policy depends on.
type GitHub interface {
	RateLimits(context.Context) (gh.RateLimits, error)
	SearchView(context.Context, config.View) ([]gh.PullRequest, error)
	EnrichCI(context.Context, []gh.PullRequest, gh.RateResource) (gh.RateResource, []string, error)
}

type Loader interface {
	RefreshAll(context.Context) Snapshot
	RefreshOne(context.Context, config.View) ViewSnapshot
}

type Service struct {
	cfg    config.Config
	client GitHub
}

func NewService(cfg config.Config, client GitHub) *Service {
	return &Service{cfg: cfg, client: client}
}

func (s *Service) RefreshAll(ctx context.Context) Snapshot {
	snapshot := Snapshot{Views: make([]ViewData, len(s.cfg.Views)), UpdatedAt: time.Now()}
	var budgetErr error
	snapshot.Rates, snapshot.Warning, budgetErr = s.searchBudget(ctx, s.cfg.SearchRequestCount())
	if budgetErr != nil {
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

	prs := uniquePRs(snapshot.Views)
	s.enrichCI(ctx, prs, &snapshot.Rates, &snapshot.Warning)
	applyCI(snapshot.Views, prs)
	return snapshot
}

func (s *Service) RefreshOne(ctx context.Context, view config.View) ViewSnapshot {
	result := ViewSnapshot{Data: ViewData{View: view}, UpdatedAt: time.Now()}
	var budgetErr error
	requests := view.SearchRequestCount(len(s.cfg.GitHub.Scopes), s.cfg.GitHub.LimitPerScope)
	result.Rates, result.Warning, budgetErr = s.searchBudget(ctx, requests)
	if budgetErr != nil {
		result.Data.Err = budgetErr
		return result
	}

	result.Data = s.searchView(ctx, view)
	s.enrichCI(ctx, result.Data.PRs, &result.Rates, &result.Warning)
	return result
}

// searchBudget loads the current rate limits and checks that requests
// Search calls fit. An unavailable rate limit is a warning, not a refusal:
// the refresh proceeds and the footer says the limits are unknown.
func (s *Service) searchBudget(ctx context.Context, requests int) (gh.RateLimits, string, error) {
	rates, err := s.client.RateLimits(ctx)
	if err != nil {
		return rates, "rate limits unavailable: " + err.Error(), nil
	}
	if !rates.Search.HasCapacity(requests) {
		return rates, "", searchCapacityError(rates.Search, requests)
	}
	return rates, "", nil
}

// enrichCI refreshes the displayed rate limits after the searches, then
// loads CI status into prs in place. Enrichment failures become warnings so
// the loaded rows stay visible, and the rates are refreshed again so the
// footer reflects the GraphQL points the failed attempt consumed.
func (s *Service) enrichCI(ctx context.Context, prs []gh.PullRequest, rates *gh.RateLimits, warning *string) {
	s.refreshRates(ctx, rates, warning)
	if len(prs) == 0 {
		return
	}
	graphRate, warnings, err := s.client.EnrichCI(ctx, prs, rates.GraphQL)
	if graphRate.Limit > 0 {
		rates.GraphQL = graphRate
	}
	for _, next := range warnings {
		*warning = appendWarning(*warning, next)
	}
	if err != nil {
		*warning = appendWarning(*warning, "CI refresh failed: "+err.Error())
		s.refreshRates(ctx, rates, warning)
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
	ciByURL := make(map[string]gh.CIState, len(prs))
	for _, pr := range prs {
		ciByURL[pr.URL] = pr.CI
	}
	for i := range views {
		for j := range views[i].PRs {
			if state, ok := ciByURL[views[i].PRs[j].URL]; ok {
				views[i].PRs[j].CI = state
			}
		}
	}
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

func searchCapacityError(rate gh.RateResource, requests int) error {
	return fmt.Errorf(
		"GitHub search rate limit has %d requests remaining but refresh requires %d; resets at %s",
		rate.Remaining,
		requests,
		rate.Reset.Local().Format("15:04"),
	)
}

// appendWarning joins footer warnings. It drops an empty or already listed
// warning, so one error shared by every view appears once.
func appendWarning(current, next string) string {
	if next == "" {
		return current
	}
	if current == "" {
		return next
	}
	if strings.Contains(current, next) {
		return current
	}
	return current + "; " + next
}
