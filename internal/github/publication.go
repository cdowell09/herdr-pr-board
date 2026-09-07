package github

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type PublishedReview struct {
	ID       int64  `json:"id"`
	URL      string `json:"html_url"`
	Body     string `json:"body"`
	CommitID string `json:"commit_id"`
	State    string `json:"state"`
	User     struct {
		Login string `json:"login"`
	} `json:"user"`
}

// PublicationActor deliberately bypasses the discovery login cache.
func (c *Client) PublicationActor(ctx context.Context) (string, error) {
	data, err := c.runner(ctx, "api", "user", "--jq", ".login")
	if err != nil {
		return "", err
	}
	login := strings.TrimSpace(string(data))
	if login == "" || strings.ContainsAny(login, " \t\r\n") {
		return "", errors.New("GitHub returned an invalid publication actor")
	}
	return login, nil
}

func reviewEndpoint(prURL string) (string, error) {
	repo, number, err := ParsePRURL(prURL)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("repos/%s/pulls/%d/reviews", repo, number), nil
}

func (c *Client) ListPublishedReviews(ctx context.Context, prURL string) ([]PublishedReview, error) {
	endpoint, err := reviewEndpoint(prURL)
	if err != nil {
		return nil, err
	}
	data, err := c.runner(ctx, "api", endpoint+"?per_page=100", "--paginate", "--slurp")
	if err != nil {
		return nil, err
	}
	var pages [][]PublishedReview
	if err := json.Unmarshal(data, &pages); err != nil {
		return nil, fmt.Errorf("decode published reviews: %w", err)
	}
	if pages == nil {
		return nil, errors.New("GitHub returned unavailable review pages")
	}
	var reviews []PublishedReview
	for _, page := range pages {
		reviews = append(reviews, page...)
	}
	return reviews, nil
}

func (c *Client) PublishReview(ctx context.Context, prURL, headOID, event, body string) (PublishedReview, error) {
	endpoint, err := reviewEndpoint(prURL)
	if err != nil {
		return PublishedReview{}, err
	}
	if event != "COMMENT" && event != "APPROVE" && event != "REQUEST_CHANGES" {
		return PublishedReview{}, errors.New("invalid publication event")
	}
	data, runErr := c.runner(ctx, "api", endpoint, "--method", "POST", "--include", "-f", "commit_id="+headOID, "-f", "event="+event, "-f", "body="+body)
	response, err := http.ReadResponse(bufio.NewReader(bytes.NewReader(data)), nil)
	if err != nil {
		if runErr != nil {
			return PublishedReview{}, runErr
		}
		return PublishedReview{}, fmt.Errorf("decode publication response: %w", err)
	}
	defer response.Body.Close()
	payload, readErr := io.ReadAll(response.Body)
	switch response.StatusCode {
	case 400, 401, 403, 404, 405, 409, 410, 415, 422, 429:
		return PublishedReview{}, &PublicationRejected{Status: response.StatusCode, Message: strings.TrimSpace(string(payload))}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return PublishedReview{}, fmt.Errorf("publication returned HTTP %d", response.StatusCode)
	}
	if readErr != nil {
		return PublishedReview{}, readErr
	}
	var result PublishedReview
	if err := json.Unmarshal(payload, &result); err != nil {
		return result, fmt.Errorf("decode created review: %w", err)
	}
	return result, nil
}

// PublicationRejected identifies an HTTP rejection that did not create a review.
// Network failures and server errors cannot establish that fact.
type PublicationRejected struct {
	Status  int
	Message string
}

func (e *PublicationRejected) Error() string {
	return fmt.Sprintf("GitHub rejected publication (HTTP %d): %s", e.Status, e.Message)
}
