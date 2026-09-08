package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	gh "github.com/cdowell09/herdr-pr-board/internal/github"
	"github.com/cdowell09/herdr-pr-board/internal/testutil"
)

func TestMain(m *testing.M) {
	if strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe") == "gh" {
		response, err := snapshotGHResponse(os.Args[1:])
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if os.Getenv("GH_TEST_ELIGIBLE") == "1" {
			response = strings.NewReplacer("head123", strings.Repeat("a", 40), "base123", strings.Repeat("b", 40), `"isDraft":false`, `"isDraft":false,"state":"open"`).Replace(response)
		}
		fmt.Fprintln(os.Stdout, response)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func snapshotGHResponse(args []string) (string, error) {
	if metadata := os.Getenv("GH_REVIEW_METADATA"); metadata != "" {
		return metadata, nil
	}
	log, err := os.OpenFile(os.Getenv("GH_TEST_LOG"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return "", err
	}
	_, err = fmt.Fprintln(log, strings.Join(args, " "))
	if err := errors.Join(err, log.Close()); err != nil {
		return "", err
	}
	if len(args) < 2 {
		return "", errors.New("missing gh command")
	}
	mode := os.Getenv("GH_TEST_MODE")
	switch args[0] + " " + args[1] {
	case "api user":
		return "ada", nil
	case "api rate_limit":
		if mode == "rates" {
			return "", errors.New("rate lookup failed")
		}
		return `{"resources":{"search":{"limit":30,"remaining":30,"reset":4102444800},"graphql":{"limit":5000,"remaining":5000,"reset":4102444800}}}`, nil
	case "search prs":
		if slices.Contains(args, "label:broken") {
			return "", errors.New("search unavailable")
		}
		if mode == "failed_batch" {
			return `[{"number":7,"url":"https://github.com/acme/api/pull/7","repository":{"nameWithOwner":"acme/api"}},{"number":8,"url":"https://github.com/acme/api/pull/8","repository":{"nameWithOwner":"acme/api"}}]`, nil
		}
		if mode == "empty" {
			return "[]", nil
		}
		return `[{"number":7,"title":"A change","url":"https://github.com/acme/api/pull/7","isDraft":false,"updatedAt":"2026-01-01T00:00:00Z","author":{"login":"ada"},"repository":{"nameWithOwner":"acme/api"}}]`, nil
	case "api graphql":
		if mode == "enrichment" {
			return "", errors.New("GraphQL unavailable")
		}
		if mode == "missing" {
			return `{"data":{"p0":null}}`, nil
		}
		cost := 1
		if mode == "failed_batch" {
			marker := os.Getenv("GH_TEST_LOG") + ".completed"
			if _, err := os.Stat(marker); err == nil {
				return "", errors.New("second batch failed")
			}
			if err := os.WriteFile(marker, nil, 0600); err != nil {
				return "", err
			}
			cost = 7
		}
		return fmt.Sprintf(`{"data":{"rateLimit":{"limit":5000,"remaining":%d,"resetAt":"2100-01-01T00:00:00Z","cost":%d},"p0":{"pullRequest":{"reviews":{"nodes":[],"pageInfo":{"hasNextPage":false}},"headRefOid":"head123","baseRefName":"main","baseRefOid":"base123","commits":{"nodes":[{"commit":{"statusCheckRollup":{"state":"SUCCESS"}}}]}}}}}`, 5000-cost, cost), nil
	}
	return "", fmt.Errorf("unexpected gh command: %v", args)
}

// The executable exercises process arguments and stdout/stderr handling without a terminal.
func fakeSnapshotGH(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "calls")
	testutil.Executable(t, dir, "gh")
	t.Setenv("GORACE", os.Getenv("GORACE")+" atexit_sleep_ms=0")
	t.Setenv("GH_REVIEW_METADATA", "")
	t.Setenv("GH_TEST_ELIGIBLE", "")
	t.Setenv("PATH", dir)
	t.Setenv("GH_TEST_LOG", log)
	t.Setenv("GH_TEST_MODE", "")
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	return log
}

const secondViewTOML = `
[[views]]
id = "mine"
title = "Mine"
query = "is:open author:@me"
scope = "global"
`

func decodeSnapshot(t *testing.T, data []byte) snapshotJSON {
	t.Helper()
	var result snapshotJSON
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		t.Fatalf("decode snapshot: %v; output=%s", err, data)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		t.Fatalf("extra stdout after JSON: %v", err)
	}
	return result
}

func TestRunJSONAllViewsAndSelectedView(t *testing.T) {
	log := fakeSnapshotGH(t)
	path := writeConfig(t, validConfigTOML+secondViewTOML)
	var firstObserved time.Time
	for _, selected := range []string{"", "mine"} {
		if err := os.WriteFile(log, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		var stdout, stderr bytes.Buffer
		args := []string{"--json", "--config", path}
		if selected != "" {
			args = append(args, "--view", selected)
		}
		before := time.Now()
		if code := run(args, &stdout, &stderr); code != 0 {
			t.Fatalf("code=%d stderr=%s", code, &stderr)
		}
		if stderr.Len() != 0 {
			t.Fatalf("stderr=%s", &stderr)
		}
		doc := decodeSnapshot(t, stdout.Bytes())
		want := 2
		if selected != "" {
			want = 1
		}
		if doc.SchemaVersion != 1 || len(doc.Views) != want || doc.LimitPerSearch != 100 || len(doc.Errors) != 0 {
			t.Fatalf("document=%+v", doc)
		}
		if doc.StartedAt.Before(before) || doc.FinishedAt.Before(doc.StartedAt) {
			t.Fatalf("scan times=%+v", doc)
		}
		if doc.Rates.Search == nil || doc.Rates.GraphQL == nil || doc.Rates.GraphQL.Cost == nil || *doc.Rates.GraphQL.Cost != 1 {
			t.Fatalf("rates=%+v", doc.Rates)
		}
		for _, view := range doc.Views {
			if selected != "" && view.ID != selected {
				t.Fatalf("view=%s", view.ID)
			}
			if !view.SearchSucceeded || view.Completeness != "unknown" || view.ObservedAt == nil || view.ObservedAt.Before(doc.StartedAt) || len(view.PRs) != 1 {
				t.Fatalf("view=%+v", view)
			}
			pr := view.PRs[0]
			if pr.HeadOID == nil || *pr.HeadOID != "head123" || pr.BaseRefName == nil || *pr.BaseRefName != "main" || pr.BaseOID == nil || *pr.BaseOID != "base123" || pr.CI == nil || *pr.CI != "SUCCESS" || pr.MetadataObservedAt == nil {
				t.Fatalf("pr=%+v", pr)
			}
			if pr.ViewerReviews == nil || !pr.ViewerReviews.Complete || pr.ViewerReviews.Actor != "ada" || pr.ViewerReviews.ObservedAt == nil || pr.ViewerReviews.Reviews == nil {
				t.Fatalf("viewer reviews=%+v", pr.ViewerReviews)
			}
			if pr.MetadataObservedAt.Before(*view.ObservedAt) || pr.MetadataObservedAt.After(doc.FinishedAt) {
				t.Fatalf("metadata time=%s", pr.MetadataObservedAt)
			}
			if selected != "" && !pr.MetadataObservedAt.After(firstObserved) {
				t.Fatal("second command reused earlier metadata")
			}
			if selected == "" {
				firstObserved = *pr.MetadataObservedAt
			}
		}
		calls, err := os.ReadFile(log)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Count(string(calls), "search prs") != want || strings.Count(string(calls), "api graphql") != 1 {
			t.Fatalf("calls=%s", calls)
		}
		if !strings.Contains(string(calls), "--limit 100 --sort updated --order desc --json number,title,url,author,isDraft,updatedAt,repository,state -- is:open") {
			t.Fatalf("search arguments=%s", calls)
		}
		if selected != "" && strings.Contains(string(calls), "org:acme") {
			t.Fatalf("selected global view used configured scope: %s", calls)
		}
	}
}

func TestRunJSONEmptyAndPartialFailures(t *testing.T) {
	for _, mode := range []string{"empty", "enrichment", "missing", "rates", "search"} {
		t.Run(mode, func(t *testing.T) {
			fakeSnapshotGH(t)
			t.Setenv("GH_TEST_MODE", mode)
			content := validConfigTOML + secondViewTOML
			if mode == "search" {
				content = strings.Replace(content, "is:open author:@me", "is:open label:broken", 1)
			}
			path := writeConfig(t, content)
			var stdout, stderr bytes.Buffer
			code := run([]string{"--json", "--config", path}, &stdout, &stderr)
			wantCode := 1
			if mode == "empty" {
				wantCode = 0
			}
			if code != wantCode {
				t.Fatalf("code=%d want=%d stderr=%s", code, wantCode, &stderr)
			}
			doc := decodeSnapshot(t, stdout.Bytes())
			if mode == "enrichment" {
				assertSnapshotWireKeys(t, stdout.Bytes())
			}
			if len(doc.Views) != 2 {
				t.Fatalf("views=%+v", doc.Views)
			}
			if mode == "empty" {
				if stderr.Len() != 0 || len(doc.Errors) != 0 || len(doc.Views[0].PRs) != 0 || doc.Views[0].ObservedAt == nil {
					t.Fatalf("empty=%+v stderr=%s", doc, &stderr)
				}
				if !bytes.Contains(stdout.Bytes(), []byte(`"prs":[]`)) {
					t.Fatalf("empty rows must be array: %s", &stdout)
				}
				return
			}
			if stderr.Len() == 0 || len(doc.Errors) == 0 || len(doc.Views[0].PRs) != 1 {
				t.Fatalf("partial=%+v stderr=%s", doc, &stderr)
			}
			if mode == "enrichment" || mode == "missing" {
				pr := doc.Views[0].PRs[0]
				if pr.HeadOID != nil || pr.BaseRefName != nil || pr.BaseOID != nil || pr.CI != nil || pr.MetadataObservedAt != nil {
					t.Fatalf("unavailable metadata=%+v", pr)
				}
			}
			if mode == "rates" && doc.Rates.Search != nil {
				t.Fatalf("unknown search rate=%+v", doc.Rates.Search)
			}
			if mode == "search" && (doc.Views[1].SearchSucceeded || doc.Views[1].ObservedAt != nil || len(doc.Views[1].PRs) != 0 || doc.Errors[0].ViewID == nil || *doc.Errors[0].ViewID != "mine") {
				t.Fatalf("failed view=%+v errors=%+v", doc.Views[1], doc.Errors)
			}
		})
	}
}

func TestRunJSONInvalidUsageDoesNotCallGH(t *testing.T) {
	log := fakeSnapshotGH(t)
	path := writeConfig(t, validConfigTOML)
	for _, extra := range [][]string{{"--json", "--validate"}, {"--view", "all"}, {"--json", "--view", ""}, {"--json", "--view", "missing"}, {"--json", "extra"}, {"extra"}} {
		var stdout, stderr bytes.Buffer
		args := append([]string{"--config", path}, extra...)
		if code := run(args, &stdout, &stderr); code != 2 || stdout.Len() != 0 || stderr.Len() == 0 {
			t.Fatalf("args=%v code=%d stdout=%s stderr=%s", args, code, &stdout, &stderr)
		}
	}
	if _, err := os.Stat(log); !os.IsNotExist(err) {
		t.Fatalf("invalid usage called gh: %v", err)
	}
}

// Literal keys pin the public contract independently of production struct tags.
func assertSnapshotWireKeys(t *testing.T, data []byte) {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	check := func(value map[string]any, want string) {
		t.Helper()
		got := make([]string, 0, len(value))
		for key := range value {
			got = append(got, key)
		}
		slices.Sort(got)
		expected := strings.Fields(want)
		slices.Sort(expected)
		if !slices.Equal(got, expected) {
			t.Fatalf("wire keys=%v want=%v", got, expected)
		}
	}
	check(doc, "schema_version started_at finished_at limit_per_search views rates errors")
	view := doc["views"].([]any)[0].(map[string]any)
	check(view, "id title query scope scopes observed_at search_succeeded completeness prs")
	pr := view["prs"].([]any)[0].(map[string]any)
	check(pr, "repository number url title author draft state updated_at head_oid base_ref_name base_oid ci metadata_observed_at viewer_reviews")
	for _, key := range []string{"state", "head_oid", "base_ref_name", "base_oid", "ci", "metadata_observed_at", "viewer_reviews"} {
		if pr[key] != nil {
			t.Fatalf("%s=%v want null", key, pr[key])
		}
	}
	rates := doc["rates"].(map[string]any)
	check(rates, "search graphql")
	check(rates["search"].(map[string]any), "limit remaining reset_at cost")
	check(doc["errors"].([]any)[0].(map[string]any), "stage view_id message")
}

func TestRunJSONPreservesGraphQLCostAfterFailedBatch(t *testing.T) {
	fakeSnapshotGH(t)
	t.Setenv("GH_TEST_MODE", "failed_batch")
	path := writeConfig(t, strings.Replace(validConfigTOML, "[github]", "[github]\nci_batch_size = 1", 1))
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--json", "--config", path}, &stdout, &stderr); code != 1 {
		t.Fatalf("code=%d stderr=%s", code, &stderr)
	}
	doc := decodeSnapshot(t, stdout.Bytes())
	rate := doc.Rates.GraphQL
	if rate == nil || rate.Cost == nil || *rate.Cost != 7 || rate.Remaining != 5000 {
		t.Fatalf("GraphQL rate=%+v", rate)
	}
	if len(doc.Views[0].PRs) != 2 || len(doc.Errors) == 0 {
		t.Fatalf("partial snapshot=%+v", doc)
	}
}

func TestViewerReviewWireContract(t *testing.T) {
	observed := time.Date(2026, 1, 2, 3, 4, 5, 0, time.FixedZone("offset", 3600))
	observation := &gh.ReviewObservation{Actor: "alice", Complete: false, ObservedAt: observed, Reviews: []gh.SubmittedReview{{ID: 9007199254740993, State: "DISMISSED", SubmittedAt: observed}, {ID: 42, HeadOID: "head", State: "APPROVED", SubmittedAt: observed}}}
	data, err := json.Marshal(wireViewerReviews(observation))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"actor":"alice","complete":false,"observed_at":"2026-01-02T02:04:05Z","reviews":[{"id":9007199254740993,"head_oid":null,"state":"DISMISSED","submitted_at":"2026-01-02T02:04:05Z"},{"id":42,"head_oid":"head","state":"APPROVED","submitted_at":"2026-01-02T02:04:05Z"}]}`
	if string(data) != want {
		t.Fatalf("wire review metadata=%s, want %s", data, want)
	}
	for _, test := range []struct {
		value *gh.ReviewObservation
		want  string
	}{
		{nil, "null"},
		{&gh.ReviewObservation{Actor: "alice", Complete: true}, `{"actor":"alice","complete":true,"observed_at":null,"reviews":[]}`},
	} {
		data, err := json.Marshal(wireViewerReviews(test.value))
		if err != nil || string(data) != test.want {
			t.Fatalf("wire=%s, want %s, err=%v", data, test.want, err)
		}
	}
}
