package github

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/cdowell09/herdr-pr-board/internal/config"
)

func TestCaptureRevisionUsesFreshPRMetadata(t *testing.T) {
	calls := 0
	runner := func(_ context.Context, args ...string) ([]byte, error) {
		calls++
		if strings.Join(args, " ") != "pr view 7 --repo acme/repo --json number,title,url,isDraft,state,headRefOid,baseRefName,baseRefOid" {
			t.Fatalf("args=%v", args)
		}
		return []byte(fmt.Sprintf(`{"number":7,"title":"Text $(data)","url":"https://github.com/acme/repo/pull/7","state":"OPEN","headRefOid":%q,"baseRefOid":%q,"baseRefName":"main"}`, strings.Repeat("a", 40), strings.Repeat("b", 40))), nil
	}
	client := NewClient(runner, config.GitHubConfig{})
	for range 2 {
		pr, err := client.CaptureRevision(context.Background(), "https://github.com/ACME/repo/pull/7")
		if err != nil || pr.Repository != "acme/repo" || pr.MetadataObservedAt.IsZero() {
			t.Fatalf("%+v %v", pr, err)
		}
	}
	if calls != 2 {
		t.Fatalf("capture reused cache: %d", calls)
	}
}

func TestCaptureRejectsUnavailableOrMismatchedRevision(t *testing.T) {
	for _, data := range []string{`{}`, `null`, `{"number":8,"url":"https://github.com/acme/repo/pull/8","state":"OPEN"}`, `{"number":7,"url":"https://github.com/acme/repo/pull/7","state":"CLOSED"}`} {
		client := NewClient(func(context.Context, ...string) ([]byte, error) { return []byte(data), nil }, config.GitHubConfig{})
		if _, err := client.CaptureRevision(context.Background(), "https://github.com/acme/repo/pull/7"); err == nil {
			t.Fatalf("accepted %s", data)
		}
	}
	for _, url := range []string{"--help", "https://evil.example/acme/repo/pull/7", "https://github.com/acme/repo/pull/0", "https://github.com/acme/repo/pull/7?x=y"} {
		if _, _, err := ParsePRURL(url); err == nil {
			t.Fatalf("accepted %s", url)
		}
	}
}
