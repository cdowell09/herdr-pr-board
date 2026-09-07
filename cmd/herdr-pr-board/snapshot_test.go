package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// The executable exercises process arguments and stdout/stderr handling without a terminal.
func fakeSnapshotGH(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "calls")
	script := `#!/bin/sh
printf '%s\n' "$*" >> "$GH_TEST_LOG"
case "$1 $2" in
 "api rate_limit")
  if [ "$GH_TEST_MODE" = rates ]; then echo 'rate lookup failed' >&2; exit 1; fi
  printf '%s\n' '{"resources":{"search":{"limit":30,"remaining":30,"reset":4102444800},"graphql":{"limit":5000,"remaining":5000,"reset":4102444800}}}' ;;
 "search prs")
  case "$*" in
   *label:broken*) echo 'search unavailable' >&2; exit 1 ;;
  esac
  if [ "$GH_TEST_MODE" = failed_batch ]; then
   printf '%s\n' '[{"number":7,"url":"https://github.com/acme/api/pull/7","repository":{"nameWithOwner":"acme/api"}},{"number":8,"url":"https://github.com/acme/api/pull/8","repository":{"nameWithOwner":"acme/api"}}]'; exit
  fi
  if [ "$GH_TEST_MODE" = empty ]; then printf '[]\n'; exit; fi
  printf '%s\n' '[{"number":7,"title":"A change","url":"https://github.com/acme/api/pull/7","isDraft":false,"updatedAt":"2026-01-01T00:00:00Z","author":{"login":"ada"},"repository":{"nameWithOwner":"acme/api"}}]' ;;
 "api graphql")
  if [ "$GH_TEST_MODE" = failed_batch ]; then
   if [ -f "$GH_TEST_LOG.completed" ]; then echo 'second batch failed' >&2; exit 1; fi
   : > "$GH_TEST_LOG.completed"
   printf '%s\n' '{"data":{"rateLimit":{"limit":5000,"remaining":4993,"resetAt":"2100-01-01T00:00:00Z","cost":7},"p0":{"pullRequest":{"headRefOid":"head123","baseRefName":"main","baseRefOid":"base123","commits":{"nodes":[{"commit":{"statusCheckRollup":{"state":"SUCCESS"}}}]}}}}}'; exit
  fi
  if [ "$GH_TEST_MODE" = enrichment ]; then echo 'GraphQL unavailable' >&2; exit 1; fi
  if [ "$GH_TEST_MODE" = missing ]; then printf '%s\n' '{"data":{"p0":null}}'; exit; fi
  printf '%s\n' '{"data":{"rateLimit":{"limit":5000,"remaining":4999,"resetAt":"2100-01-01T00:00:00Z","cost":1},"p0":{"pullRequest":{"headRefOid":"head123","baseRefName":"main","baseRefOid":"base123","commits":{"nodes":[{"commit":{"statusCheckRollup":{"state":"SUCCESS"}}}]}}}}}' ;;
 *) echo "unexpected gh command: $*" >&2; exit 1 ;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
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
	check(pr, "repository number url title author draft state updated_at head_oid base_ref_name base_oid ci metadata_observed_at")
	for _, key := range []string{"state", "head_oid", "base_ref_name", "base_oid", "ci", "metadata_observed_at"} {
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
