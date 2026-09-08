package monitor

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/cdowell09/herdr-pr-board/internal/config"
	"github.com/cdowell09/herdr-pr-board/internal/discovery"
	gh "github.com/cdowell09/herdr-pr-board/internal/github"
	"github.com/cdowell09/herdr-pr-board/internal/localstate"
)

// Disk records contain observations only. Retaining older rows belongs to the board.
type record struct {
	Version               int
	Config                configIdentity
	StartedAt, FinishedAt time.Time
	Rates                 storedRates
	CapacityError         string
	Views                 []storedView
	Errors                []storedError
}

type configIdentity struct {
	GitHub config.GitHubConfig
	Views  []config.View
}

func identity(cfg config.Config) configIdentity { return configIdentity{cfg.GitHub, cfg.Views} }

type storedRate struct {
	Limit, Remaining, Cost int
	Reset                  time.Time
}

func storeRate(r gh.RateResource) storedRate {
	return storedRate{r.Limit, r.Remaining, r.Cost, r.Reset}
}
func (r storedRate) rate() gh.RateResource {
	return gh.RateResource{Limit: r.Limit, Remaining: r.Remaining, Cost: r.Cost, Reset: r.Reset}
}

type storedRates struct{ Search, GraphQL storedRate }

type storedView struct {
	View                  config.View
	PRs                   []gh.PullRequest
	UpdatedAt, ObservedAt time.Time
	Error                 string
}

type storedError struct{ Stage, ViewID, Message string }

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func storedFailure(message string) error {
	if message == "" {
		return nil
	}
	return errors.New(message)
}

func (s *Source) write(snapshot discovery.Snapshot) error {
	r := record{Version: 1, Config: identity(s.cfg), StartedAt: snapshot.StartedAt, FinishedAt: snapshot.FinishedAt, Rates: storedRates{storeRate(snapshot.Rates.Search), storeRate(snapshot.Rates.GraphQL)}, CapacityError: errorText(snapshot.CapacityErr)}
	for _, v := range snapshot.Views {
		r.Views = append(r.Views, storedView{View: v.View, PRs: v.PRs, UpdatedAt: v.UpdatedAt, ObservedAt: v.ObservedAt, Error: errorText(v.Err)})
	}
	for _, e := range snapshot.Errors {
		r.Errors = append(r.Errors, storedError{e.Stage, e.ViewID, errorText(e.Err)})
	}
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	return localstate.AtomicWrite(filepath.Join(s.dir, "monitor-snapshot.json"), b)
}

func (s *Source) read() (discovery.Snapshot, error) {
	var r record
	b, err := localstate.ReadFile(filepath.Join(s.dir, "monitor-snapshot.json"))
	if err != nil {
		return discovery.Snapshot{}, err
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return discovery.Snapshot{}, fmt.Errorf("read monitor snapshot: %w", err)
	}
	if r.Version != 1 {
		return discovery.Snapshot{}, fmt.Errorf("unsupported monitor snapshot version %d; restart the monitor", r.Version)
	}
	if !config.SameDiscovery(config.Config{GitHub: r.Config.GitHub, Views: r.Config.Views}, s.cfg) {
		return discovery.Snapshot{}, errors.New("monitor configuration differs; restart the monitor with this configuration")
	}
	if r.FinishedAt.IsZero() || len(r.Views) != len(s.cfg.Views) {
		return discovery.Snapshot{}, errors.New("monitor snapshot is incomplete")
	}
	snapshot := discovery.Snapshot{StartedAt: r.StartedAt, FinishedAt: r.FinishedAt, Rates: gh.RateLimits{Search: r.Rates.Search.rate(), GraphQL: r.Rates.GraphQL.rate()}, CapacityErr: storedFailure(r.CapacityError)}
	for i, v := range r.Views {
		if v.View != s.cfg.Views[i] {
			return discovery.Snapshot{}, errors.New("monitor snapshot view differs from configuration")
		}
		snapshot.Views = append(snapshot.Views, discovery.ViewData{View: v.View, PRs: v.PRs, UpdatedAt: v.UpdatedAt, ObservedAt: v.ObservedAt, Err: storedFailure(v.Error)})
	}
	for _, e := range r.Errors {
		snapshot.Errors = append(snapshot.Errors, discovery.RetrievalError{Stage: e.Stage, ViewID: e.ViewID, Err: errors.New(e.Message)})
	}
	return snapshot, nil
}
