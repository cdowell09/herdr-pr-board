package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/cdowell09/herdr-pr-board/internal/config"
	"github.com/cdowell09/herdr-pr-board/internal/discovery"
	gh "github.com/cdowell09/herdr-pr-board/internal/github"
)

// These types define the version-one wire contract independently of discovery and UI types.
type snapshotJSON struct {
	SchemaVersion  int         `json:"schema_version"`
	StartedAt      time.Time   `json:"started_at"`
	FinishedAt     time.Time   `json:"finished_at"`
	LimitPerSearch int         `json:"limit_per_search"`
	Views          []viewJSON  `json:"views"`
	Rates          ratesJSON   `json:"rates"`
	Errors         []errorJSON `json:"errors"`
}

type viewJSON struct {
	ID              string     `json:"id"`
	Title           string     `json:"title"`
	Query           string     `json:"query"`
	Scope           string     `json:"scope"`
	Scopes          []string   `json:"scopes"`
	ObservedAt      *time.Time `json:"observed_at"`
	SearchSucceeded bool       `json:"search_succeeded"`
	Completeness    string     `json:"completeness"`
	PRs             []prJSON   `json:"prs"`
}

type prJSON struct {
	Repository         string     `json:"repository"`
	Number             int        `json:"number"`
	URL                string     `json:"url"`
	Title              string     `json:"title"`
	Author             string     `json:"author"`
	State              *string    `json:"state"`
	Draft              bool       `json:"draft"`
	UpdatedAt          *time.Time `json:"updated_at"`
	HeadOID            *string    `json:"head_oid"`
	BaseRefName        *string    `json:"base_ref_name"`
	BaseOID            *string    `json:"base_oid"`
	CI                 *string    `json:"ci"`
	MetadataObservedAt *time.Time `json:"metadata_observed_at"`
}

type ratesJSON struct {
	Search  *rateJSON `json:"search"`
	GraphQL *rateJSON `json:"graphql"`
}

type rateJSON struct {
	Limit     int        `json:"limit"`
	Remaining int        `json:"remaining"`
	ResetAt   *time.Time `json:"reset_at"`
	Cost      *int       `json:"cost"`
}

type errorJSON struct {
	Stage   string  `json:"stage"`
	ViewID  *string `json:"view_id"`
	Message string  `json:"message"`
}

func printSnapshot(cfg config.Config, service discovery.Loader, stdout, stderr io.Writer) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, discovery.RefreshAllTimeout)
	defer cancel()
	snapshot := service.RefreshAll(ctx)
	document := snapshotJSON{
		SchemaVersion: 1, StartedAt: snapshot.StartedAt, FinishedAt: snapshot.FinishedAt,
		LimitPerSearch: cfg.GitHub.LimitPerScope, Views: make([]viewJSON, 0, len(snapshot.Views)),
		Rates:  ratesJSON{Search: wireRate(snapshot.Rates.Search), GraphQL: wireRate(snapshot.Rates.GraphQL)},
		Errors: make([]errorJSON, 0, len(snapshot.Errors)),
	}
	for _, data := range snapshot.Views {
		view := viewJSON{ID: data.View.ID, Title: data.View.Title, Query: data.View.Query,
			Scope: string(data.View.Scope), Scopes: []string{}, ObservedAt: wireTime(data.ObservedAt),
			SearchSucceeded: data.Err == nil, Completeness: "unknown", PRs: make([]prJSON, 0, len(data.PRs))}
		if data.View.Scope == config.ScopeConfigured {
			view.Scopes = cfg.GitHub.Scopes
		}
		for _, pr := range data.PRs {
			ci := string(pr.CI)
			if pr.CI == gh.CIUnknown {
				ci = ""
			}
			view.PRs = append(view.PRs, prJSON{Repository: pr.Repository, Number: pr.Number, URL: pr.URL,
				Title: pr.Title, Author: pr.Author, Draft: pr.Draft, State: wireString(string(pr.State)), UpdatedAt: wireTime(pr.UpdatedAt),
				HeadOID: wireString(pr.HeadOID), BaseRefName: wireString(pr.BaseRefName), BaseOID: wireString(pr.BaseOID),
				CI: wireString(ci), MetadataObservedAt: wireTime(pr.MetadataObservedAt)})
		}
		document.Views = append(document.Views, view)
	}
	for _, failure := range snapshot.Errors {
		document.Errors = append(document.Errors, errorJSON{Stage: failure.Stage, ViewID: wireString(failure.ViewID), Message: failure.Err.Error()})
		fmt.Fprintln(stderr, "herdr-pr-board:", failure.Err)
	}
	if err := json.NewEncoder(stdout).Encode(document); err != nil {
		return fail(stderr, err)
	}
	if len(document.Errors) > 0 {
		return 1
	}
	return 0
}

func wireTime(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	value = value.UTC()
	return &value
}

func wireString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func wireRate(rate gh.RateResource) *rateJSON {
	if rate.Limit == 0 {
		return nil
	}
	result := &rateJSON{Limit: rate.Limit, Remaining: rate.Remaining, ResetAt: wireTime(rate.Reset)}
	if rate.Cost > 0 {
		result.Cost = &rate.Cost
	}
	return result
}
