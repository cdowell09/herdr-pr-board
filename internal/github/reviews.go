package github

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

func reviewSelection(actor, cursor string) string {
	if actor == "" {
		return ""
	}
	after := ""
	if cursor != "" {
		after = ", after: " + strconv.Quote(cursor)
	}
	return fmt.Sprintf("reviews(first: 100, author: %s%s) { nodes { fullDatabaseId state submittedAt commit { oid } } pageInfo { hasNextPage endCursor } }", strconv.Quote(actor), after)
}

// enrichReviews follows each connection until GitHub confirms its last page.
// A failed page retains submitted reviews but never establishes their absence.
func (c *Client) enrichReviews(ctx context.Context, prs []PullRequest, actor string, response graphQLResponse, budget RateResource, observedAt time.Time) (RateResource, []string, error) {
	if actor == "" {
		return budget, nil, nil
	}
	cursors := make(map[int]string)
	seen := make(map[int]map[string]bool)
	var warnings []string
	for i := range prs {
		cursors[i] = ""
		seen[i] = make(map[string]bool)
	}
	for len(cursors) > 0 {
		for i := range cursors {
			alias := fmt.Sprintf("p%d", i)
			failed := false
			for _, e := range response.Errors {
				if len(e.Path) == 0 || (e.Path[0] == alias && (len(e.Path) < 3 || e.Path[2] == "reviews")) {
					failed = true
				}
			}
			page, next, err := decodeReviewPage(response.Data[alias])
			if err != nil || failed {
				if len(response.Errors) == 0 {
					warnings = append(warnings, fmt.Sprintf("load viewer reviews: %s#%d has incomplete review data", prs[i].Repository, prs[i].Number))
				}
				delete(cursors, i)
				continue
			}
			if prs[i].ViewerReviews == nil {
				prs[i].ViewerReviews = &ReviewObservation{Actor: actor, ObservedAt: observedAt, Reviews: []SubmittedReview{}}
			}
			observation := prs[i].ViewerReviews
			observation.Reviews = append(observation.Reviews, page...)
			if next == "" {
				observation.Complete = true
				delete(cursors, i)
			} else if seen[i][next] {
				warnings = append(warnings, fmt.Sprintf("load viewer reviews: %s#%d has a repeated review cursor", prs[i].Repository, prs[i].Number))
				delete(cursors, i)
			} else {
				seen[i][next] = true
				cursors[i] = next
			}
		}
		if len(cursors) == 0 {
			break
		}
		if !budget.HasCapacity(budget.CostPerQuery()) {
			return budget, warnings, fmt.Errorf("GraphQL rate limit has %d points remaining but review pagination needs at least %d; viewer reviews are incomplete", budget.Remaining, budget.CostPerQuery())
		}
		var query strings.Builder
		query.WriteString("query { rateLimit { limit remaining resetAt cost } ")
		for i, pr := range prs {
			cursor, pending := cursors[i]
			if !pending {
				continue
			}
			owner, name, _ := strings.Cut(pr.Repository, "/")
			fmt.Fprintf(&query, "p%d: repository(owner: %s, name: %s) { pullRequest(number: %d) { %s } } ", i, strconv.Quote(owner), strconv.Quote(name), pr.Number, reviewSelection(actor, cursor))
		}
		query.WriteString("}")
		output, runErr := c.runner(ctx, "api", "graphql", "-f", "query="+query.String())
		response = graphQLResponse{}
		if err := json.Unmarshal(output, &response); err != nil {
			if runErr != nil {
				err = runErr
			}
			warnings = append(warnings, "load viewer reviews: "+err.Error())
			break
		}
		if rate := decodeGraphQLRate(response.Data["rateLimit"]); rate.Limit > 0 {
			budget = rate
		}
		for _, graphErr := range response.Errors {
			warnings = append(warnings, "load viewer reviews: "+graphErr.Message)
		}
		if runErr != nil && len(response.Errors) == 0 {
			warnings = append(warnings, "load viewer reviews: "+runErr.Error())
			break
		}
	}
	return budget, warnings, nil
}

func decodeReviewPage(raw json.RawMessage) ([]SubmittedReview, string, error) {
	var node struct {
		PullRequest *struct {
			Reviews *struct {
				Nodes []*struct {
					ID          json.Number `json:"fullDatabaseId"`
					State       string      `json:"state"`
					SubmittedAt *time.Time  `json:"submittedAt"`
					Commit      *struct {
						OID string `json:"oid"`
					} `json:"commit"`
				} `json:"nodes"`
				PageInfo *struct {
					HasNextPage *bool  `json:"hasNextPage"`
					EndCursor   string `json:"endCursor"`
				} `json:"pageInfo"`
			} `json:"reviews"`
		} `json:"pullRequest"`
	}
	if err := json.Unmarshal(raw, &node); err != nil {
		return nil, "", err
	}
	if node.PullRequest == nil || node.PullRequest.Reviews == nil {
		return nil, "", fmt.Errorf("reviews unavailable")
	}
	connection := node.PullRequest.Reviews
	if connection.Nodes == nil || connection.PageInfo == nil || connection.PageInfo.HasNextPage == nil {
		return nil, "", fmt.Errorf("review page unavailable")
	}
	reviews := make([]SubmittedReview, 0, len(connection.Nodes))
	for _, review := range connection.Nodes {
		if review == nil {
			return nil, "", fmt.Errorf("review unavailable")
		}
		if review.State == "PENDING" {
			continue
		}
		switch review.State {
		case "COMMENTED", "APPROVED", "CHANGES_REQUESTED", "DISMISSED":
		default:
			return nil, "", fmt.Errorf("review state unavailable")
		}
		id, err := review.ID.Int64()
		if err != nil || id <= 0 || review.SubmittedAt == nil || review.SubmittedAt.IsZero() {
			return nil, "", fmt.Errorf("submitted review unavailable")
		}
		head := ""
		if review.Commit != nil {
			head = review.Commit.OID
		}
		reviews = append(reviews, SubmittedReview{ID: id, HeadOID: head, State: review.State, SubmittedAt: *review.SubmittedAt})
	}
	if *connection.PageInfo.HasNextPage {
		if connection.PageInfo.EndCursor == "" {
			return nil, "", fmt.Errorf("review cursor unavailable")
		}
		return reviews, connection.PageInfo.EndCursor, nil
	}
	return reviews, "", nil
}
