package github

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/cdowell09/herdr-pr-board/internal/config"
)

func TestPublicationTransportDistinguishesRejectionFromUncertainty(t *testing.T) {
	for _, status := range []int{401, 403, 422, 429, 500, 503} {
		c := NewClient(func(_ context.Context, args ...string) ([]byte, error) {
			if !slices.Contains(args, "--include") || !slices.Contains(args, "POST") || !slices.Contains(args, "event=COMMENT") {
				t.Fatalf("args=%v", args)
			}
			return []byte(fmt.Sprintf("HTTP/1.1 %d Error\r\n\r\n{}", status)), errors.New("gh exited 1")
		}, config.GitHubConfig{})
		_, err := c.PublishReview(context.Background(), "https://github.com/acme/api/pull/7", "abc", "COMMENT", "findings")
		var rejected *PublicationRejected
		if errors.As(err, &rejected) != (status < 500) {
			t.Fatalf("HTTP%d err=%v", status, err)
		}
	}
}

func TestPublicationTransportReadsEveryReviewPage(t *testing.T) {
	c := NewClient(func(_ context.Context, args ...string) ([]byte, error) {
		if !slices.Contains(args, "--paginate") || !slices.Contains(args, "--slurp") {
			t.Fatalf("args=%v", args)
		}
		return []byte(`[[{"id":1}],[{"id":2}]]`), nil
	}, config.GitHubConfig{})
	reviews, err := c.ListPublishedReviews(context.Background(), "https://github.com/acme/api/pull/7")
	if err != nil || len(reviews) != 2 {
		t.Fatalf("%+v %v", reviews, err)
	}
}
