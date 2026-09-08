package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	gh "github.com/cdowell09/herdr-pr-board/internal/github"
	"github.com/cdowell09/herdr-pr-board/internal/publication"
	"github.com/cdowell09/herdr-pr-board/internal/review"
	"github.com/cdowell09/herdr-pr-board/internal/reviewmemory"
)

type completionGitHub struct {
	pr      gh.PullRequest
	posts   int
	postErr error
}

func (g *completionGitHub) CaptureRevision(context.Context, string) (gh.PullRequest, error) {
	return g.pr, nil
}
func (g *completionGitHub) PublicationActor(context.Context) (string, error) { return "fixture", nil }
func (g *completionGitHub) ListPublishedReviews(context.Context, string) ([]gh.PublishedReview, error) {
	return nil, nil
}
func (g *completionGitHub) PublishReview(_ context.Context, url, head, event, body string) (gh.PublishedReview, error) {
	g.posts++
	if g.postErr != nil {
		return gh.PublishedReview{}, g.postErr
	}
	result := gh.PublishedReview{ID: int64(g.posts), URL: fmt.Sprintf("%s#pullrequestreview-%d", url, g.posts), CommitID: head, State: "COMMENTED", Body: body}
	result.User.Login = "fixture"
	if event != "COMMENT" {
		return gh.PublishedReview{}, fmt.Errorf("unexpected action %s", event)
	}
	return result, nil
}

func TestManualCLIReviewAndRerunApplySavedPostingChoice(t *testing.T) {
	for _, mode := range []string{"comment", "local", "post error"} {
		t.Run(mode, func(t *testing.T) {
			state := t.TempDir()
			t.Setenv("HERDR_PLUGIN_STATE_DIR", state)
			t.Setenv("COMMAND_REVIEW_MODE", "valid")
			command, _ := json.Marshal([]string{os.Args[0], "-test.run=^TestCommandReviewerProcess$", "--", "argument with spaces"})
			action := "comment"
			if mode == "local" {
				action = ""
			}
			path := writeConfig(t, validConfigTOML+fmt.Sprintf("\n[[reviewers]]\nid='fixture'\ncommand=%s\n[[repositories]]\nname='acme/repo'\nreviewer='fixture'\nauto_launch=false\npublish_actions=['comment']\nauto_publish=%q\n", command, action))
			api := &completionGitHub{pr: gh.PullRequest{Repository: "acme/repo", Number: 7, URL: "https://github.com/acme/repo/pull/7", Title: "Review $(title) as data", State: gh.PROpen, HeadOID: strings.Repeat("a", 40), BaseOID: strings.Repeat("b", 40), BaseRefName: "main"}}
			if mode == "post error" {
				api.postErr = errors.New("fixture response lost")
			}
			reviews, err := review.New(state, path, api)
			if err != nil {
				t.Fatal(err)
			}
			defer reviews.Wait()
			publisher, err := publication.New(state, path, api)
			if err != nil {
				t.Fatal(err)
			}
			defer publisher.Wait()
			var ids []string
			for _, rerun := range []bool{false, true} {
				var stdout, stderr bytes.Buffer
				code := printReview(options{review: api.pr.URL, rerun: rerun}, reviews, publisher, &stdout, &stderr)
				wantCode := 0
				if mode == "post error" {
					wantCode = 1
				}
				if code != wantCode {
					t.Fatalf("exit%d: %s", code, &stderr)
				}
				var result struct {
					Version int              `json:"version"`
					Run     reviewmemory.Run `json:"run"`
				}
				var fields map[string]json.RawMessage
				if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
					t.Fatal(err)
				}
				if err := json.Unmarshal(stdout.Bytes(), &fields); err != nil || len(fields) != 2 || fields["version"] == nil || fields["run"] == nil {
					t.Fatalf("CLI wire changed: %s", &stdout)
				}
				if result.Version != 1 || result.Run.Status != reviewmemory.Completed || result.Run.ID == "" {
					t.Fatalf("lost completion: %+v", result)
				}
				if mode == "post error" && !strings.Contains(stderr.String(), "review completed; publication failed") {
					t.Fatalf("ambiguous failure: %s", &stderr)
				}
				ids = append(ids, result.Run.ID)
			}
			if ids[0] == ids[1] {
				t.Fatal("rerun did not produce its own completed run")
			}
			wantPosts := 2
			if mode == "local" {
				wantPosts = 0
			}
			if api.posts != wantPosts {
				t.Fatalf("automatic posts=%d want%d", api.posts, wantPosts)
			}
		})
	}
}
