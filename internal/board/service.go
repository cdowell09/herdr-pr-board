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
	View      config.View
	PRs       []gh.PullRequest
	Err       error
	UpdatedAt time.Time
}

type Snapshot struct {
	Views     []ViewData
	Rates     gh.RateLimits
	Warning   string
	UpdatedAt time.Time
}

type Loader interface {
	RefreshAll(context.Context) Snapshot
	RefreshOne(context.Context, config.View) ViewData
}

type Service struct {
	cfg    config.Config
	client *gh.Client
}

func NewService(cfg config.Config, client *gh.Client) *Service {
	return &Service{cfg: cfg, client: client}
}

func (s *Service) RefreshAll(ctx context.Context) Snapshot {
	now := time.Now()
	snapshot := Snapshot{Views: make([]ViewData, len(s.cfg.Views)), UpdatedAt: now}
	rates, rateErr := s.client.RateLimits(ctx)
	snapshot.Rates = rates
	if rateErr != nil {
		snapshot.Warning = "rate limits unavailable: " + rateErr.Error()
	}
	if rates.Search.Limit > 0 && rates.Search.Remaining == 0 {
		for i, view := range s.cfg.Views {
			snapshot.Views[i] = ViewData{View: view, Err: fmt.Errorf("GitHub search rate limit exhausted until %s", rates.Search.Reset.Local().Format("15:04"))}
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
	if rates.GraphQL.Limit == 0 || rates.GraphQL.Remaining > 0 {
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

func (s *Service) RefreshOne(ctx context.Context, view config.View) ViewData {
	data := s.searchView(ctx, view)
	if data.Err != nil || len(data.PRs) == 0 {
		return data
	}
	if _, ciErr := s.client.EnrichCI(ctx, data.PRs); ciErr != nil {
		data.Err = fmt.Errorf("PRs loaded; CI unavailable: %w", ciErr)
	}
	return data
}

func (s *Service) searchView(ctx context.Context, view config.View) ViewData {
	prs, err := s.client.SearchView(ctx, view)
	return ViewData{View: view, PRs: prs, Err: err, UpdatedAt: time.Now()}
}

func appendWarning(current, next string) string {
	if current == "" {
		return next
	}
	return current + "; " + next
}
