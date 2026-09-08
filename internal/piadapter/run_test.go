package piadapter

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cdowell09/herdr-pr-board/internal/reviewercontract"
	"github.com/cdowell09/herdr-pr-board/internal/reviewmemory"
)

func TestRunUsesPinnedCheckoutAndValidatesPiResult(t *testing.T) {
	for _, mode := range []string{"complete", "oversized_diff", "empty_spec", "pi_error", "bad_result", "wrong_revision", "missing_issue", "changed_pr", "missing_skill", "omitted_identity", "omitted_version", "omitted_base_oid", "omitted_outcome"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			bin := filepath.Join(dir, "bin")
			if err := os.Mkdir(bin, 0700); err != nil {
				t.Fatal(err)
			}
			script := `#!/bin/sh
printf '%s\n' "$0 $*" >> "$TRACE"
case "$(basename "$0")" in
 gh)
  case "$1 $2" in
   'pr view') cat "$PR_DATA";;
   'issue view') echo unavailable >&2; exit 1;;
   'repo clone') mkdir -p "$4";;
  esac;;
 git)
  case "$1" in
   rev-parse) printf '%s\n' "$HEAD_OID";;
   --no-pager)
    if [ "$2" = diff ]; then
     if [ "$PI_TEST_MODE" = oversized_diff ]; then head -c 4194305 /dev/zero; else printf 'CAPTURED_DIFF'; fi
    else printf 'CAPTURED_LOG'; fi;;
   merge-base) printf '%s\n' "$BASE_OID";;
  esac;;
 pi)
  cat > "$PROMPT"
  cat "$PI_EVENTS";;
esac
`
			for _, name := range []string{"gh", "git", "pi"} {
				if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0700); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
			t.Setenv("TRACE", filepath.Join(dir, "trace"))
			t.Setenv("PI_TEST_MODE", mode)
			t.Setenv("PROMPT", filepath.Join(dir, "prompt"))
			in := reviewercontract.Input{Version: 1, Identity: reviewmemory.Identity{Repository: "owner/repo", Number: 42, HeadOID: strings.Repeat("a", 40), BaseRefName: "main"}, BaseOID: strings.Repeat("b", 40), ResultPath: filepath.Join(dir, "result.json")}
			t.Setenv("HEAD_OID", in.Identity.HeadOID)
			t.Setenv("BASE_OID", in.BaseOID)
			body := "Implement the requested widget. $(touch PWNED) `touch PWNED`"
			if mode == "empty_spec" {
				body = ""
			}
			pr := map[string]any{"body": body, "title": "spec", "headRefOid": in.Identity.HeadOID, "baseRefOid": in.BaseOID, "baseRefName": "main", "closingIssuesReferences": []any{}}
			if mode == "missing_issue" {
				pr["closingIssuesReferences"] = []any{map[string]string{"url": "https://github.com/owner/repo/issues/1"}}
			}
			if mode == "changed_pr" {
				pr["headRefOid"] = strings.Repeat("c", 40)
			}
			data, _ := json.Marshal(pr)
			prPath := filepath.Join(dir, "pr.json")
			os.WriteFile(prPath, data, 0600)
			t.Setenv("PR_DATA", prPath)
			events, err := os.ReadFile("testdata/completed.jsonl")
			if err != nil {
				t.Fatal(err)
			}
			if mode == "pi_error" {
				events = []byte(strings.ReplaceAll(string(events), `"stopReason":"stop"`, `"stopReason":"error"`))
			}
			if mode == "wrong_revision" {
				events = []byte(strings.ReplaceAll(string(events), in.Identity.HeadOID, strings.Repeat("c", 40)))
			}
			if mode == "bad_result" {
				events = []byte(strings.ReplaceAll(string(events), `\"completed\"`, `\"unknown\"`))
			}
			if strings.HasPrefix(mode, "omitted_") {
				var rows []map[string]any
				for _, line := range strings.Split(strings.TrimSpace(string(events)), "\n") {
					var event map[string]any
					json.Unmarshal([]byte(line), &event)
					rows = append(rows, event)
				}
				last := rows[len(rows)-1]["messages"].([]any)
				msg := last[len(last)-1].(map[string]any)
				part := msg["content"].([]any)[0].(map[string]any)
				var result map[string]any
				json.Unmarshal([]byte(part["text"].(string)), &result)
				delete(result, strings.TrimPrefix(mode, "omitted_"))
				raw, _ := json.Marshal(result)
				part["text"] = string(raw)
				events = nil
				for _, row := range rows {
					raw, _ := json.Marshal(row)
					events = append(events, raw...)
					events = append(events, '\n')
				}
			}
			eventPath := filepath.Join(dir, "events")
			os.WriteFile(eventPath, events, 0600)
			t.Setenv("PI_EVENTS", eventPath)
			skill := filepath.Join(dir, "SKILL.md")
			os.WriteFile(skill, []byte("Review both axes"), 0600)
			if mode == "missing_skill" {
				skill += ".missing"
			}
			err = Run(context.Background(), in, Options{Skill: skill})
			invalid := mode == "pi_error" || mode == "bad_result" || mode == "wrong_revision" || strings.HasPrefix(mode, "omitted_")
			if invalid {
				if err == nil {
					t.Fatal("accepted invalid result")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			data, err = os.ReadFile(in.ResultPath)
			if err != nil {
				t.Fatal(err)
			}
			var result reviewercontract.Result
			if err := reviewercontract.Decode(data, &result); err != nil {
				t.Fatal(err)
			}
			want := "completed"
			if mode != "complete" {
				want = "blocked"
			}
			if string(result.Outcome.Status) != want {
				t.Fatalf("outcome=%+v", result.Outcome)
			}
			trace, _ := os.ReadFile(filepath.Join(dir, "trace"))
			if want == "blocked" {
				if mode != "oversized_diff" && strings.Contains(string(trace), "repo clone") {
					t.Fatal("blocked run started checkout")
				}
				return
			}
			for _, arg := range []string{"fetch --no-tags origin " + in.Identity.HeadOID + " " + in.BaseOID, "checkout --detach " + in.Identity.HeadOID, "--no-pager diff --no-ext-diff --no-textconv " + in.BaseOID + "...HEAD", "--no-pager log --no-show-signature --format=fuller " + in.BaseOID + "..HEAD", "--print --mode json --no-session --no-extensions --no-skills --no-context-files --no-approve --skill"} {
				if !strings.Contains(string(trace), arg) {
					t.Fatalf("missing %q: %s", arg, trace)
				}
			}
			prompt, _ := os.ReadFile(filepath.Join(dir, "prompt"))
			if !strings.Contains(string(prompt), body) || !strings.Contains(string(prompt), "CAPTURED_DIFF") || !strings.Contains(string(prompt), "CAPTURED_LOG") || !strings.Contains(string(prompt), "Review both axes") {
				t.Fatal("PR body was not preserved as data")
			}
			if _, err := os.Stat(filepath.Join(dir, "PWNED")); !os.IsNotExist(err) {
				t.Fatal("shell text executed")
			}
		})
	}
}
