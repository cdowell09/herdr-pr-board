package dispatch

import (
	"context"
	"errors"
	"sync"

	"github.com/cdowell09/herdr-pr-board/internal/config"
	"github.com/cdowell09/herdr-pr-board/internal/discovery"
	"github.com/cdowell09/herdr-pr-board/internal/review"
	"github.com/cdowell09/herdr-pr-board/internal/reviewflow"
	"github.com/cdowell09/herdr-pr-board/internal/reviewmemory"
)

type StatusReader interface {
	ReviewStatus(reviewmemory.Identity) error
}

type Reviews interface {
	StatusReader
	Review(context.Context, review.Request, func(string)) (reviewmemory.Run, error)
}

type Event struct {
	Decision Decision          `json:"decision"`
	Run      *reviewmemory.Run `json:"run,omitempty"`
	Error    string            `json:"error,omitempty"`
}

type Dispatcher struct {
	configPath string
	reviews    Reviews
	publisher  reviewflow.Publisher
}

func New(configPath string, reviews Reviews, publisher reviewflow.Publisher) *Dispatcher {
	return &Dispatcher{configPath: configPath, reviews: reviews, publisher: publisher}
}

// Decisions uses the same eligibility gate as unattended dispatch.
func Decisions(candidates []Candidate, cfg config.Config, reviews StatusReader) []Decision {
	result := make([]Decision, 0, len(candidates))
	for _, candidate := range candidates {
		result = append(result, decision(candidate, cfg, reviews))
	}
	return result
}

func decision(candidate Candidate, cfg config.Config, reviews StatusReader) Decision {
	candidate.Selected = cfg.SelectsAutomaticView(candidate.Views)
	_, permissionErr := cfg.ResolveLaunch(candidate.PR.Repository, "", true)
	allowed := permissionErr == nil
	result := Evaluate(candidate, allowed, nil)
	if result.Eligible {
		if reviews == nil {
			return Evaluate(candidate, allowed, errors.New("review state is unavailable"))
		}
		result = Evaluate(candidate, allowed, reviews.ReviewStatus(result.Identity))
	}
	return result
}

// Run wraps a monitor source. New observations replace pending work, while
// launched reviews keep their captured revisions until completion or cancellation.
func (d *Dispatcher) Run(ctx context.Context, observedConfig config.Config, observe func(context.Context, func(discovery.Snapshot)) error, report func(Event)) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	updates := make(chan discovery.Snapshot, 1)
	sourceDone := make(chan error, 1)
	go func() {
		sourceDone <- observe(ctx, func(snapshot discovery.Snapshot) {
			select {
			case updates <- snapshot:
				return
			default:
			}
			select {
			case <-updates:
			default:
			}
			select {
			case updates <- snapshot:
			case <-ctx.Done():
			}
		})
	}()
	completed := make(chan Event, 8)
	var workers sync.WaitGroup
	defer func() { cancel(); workers.Wait() }()
	active := map[reviewmemory.Identity]bool{}
	attempted := map[reviewmemory.Identity]bool{}
	var latest *discovery.Snapshot
	emit := func(event Event) {
		if report != nil {
			report(event)
		}
	}
	for {
		// Prefer a newer queued observation before starting more pending work.
		select {
		case snapshot := <-updates:
			latest = &snapshot
			attempted = map[reviewmemory.Identity]bool{}
		default:
		}
		if latest != nil && ctx.Err() == nil {
			cfg, err := config.LoadExisting(d.configPath)
			if err != nil {
				emit(Event{Error: err.Error()})
			} else if !config.SameDiscovery(cfg, observedConfig) {
				emit(Event{Error: "monitor discovery configuration changed; restart the monitor"})
			} else {
				for _, candidate := range Candidates(*latest, cfg.Views) {
					if len(active) >= max(1, cfg.Review.MaxConcurrency) {
						break
					}
					id := Identity(candidate.PR)
					if active[id] || attempted[id] {
						continue
					}
					eligible := decision(candidate, cfg, d.reviews)
					emit(Event{Decision: eligible})
					if !eligible.Eligible {
						continue
					}
					attempted[id], active[id] = true, true
					workers.Add(1)
					go func(candidate Candidate, eligible Decision) {
						defer workers.Done()
						event := d.launch(ctx, candidate, eligible, observedConfig)
						select {
						case completed <- event:
						case <-ctx.Done():
						}
					}(candidate, eligible)
				}
			}
		}
		select {
		case <-ctx.Done():
			<-sourceDone
			return nil
		case err := <-sourceDone:
			return err
		case snapshot := <-updates:
			latest = &snapshot
			attempted = map[reviewmemory.Identity]bool{}
		case event := <-completed:
			delete(active, event.Decision.Identity)
			emit(event)
		}
	}
}

func (d *Dispatcher) launch(ctx context.Context, candidate Candidate, eligible Decision, observedConfig config.Config) Event {
	event := Event{Decision: eligible}
	id := eligible.Identity
	run, err := reviewflow.Run(ctx, d.reviews, d.publisher, review.Request{URL: candidate.PR.URL, Automatic: true, ExpectedRevision: &id, ObservedViews: candidate.Views, ObservedConfig: observedConfig}, nil)
	if run.ID != "" {
		event.Run = &run
	}
	if err != nil {
		event.Error = err.Error()
		if run.Status != reviewmemory.Completed {
			event.Decision.Eligible = false
			event.Decision.Reason = err.Error()
		}
	}
	return event
}
