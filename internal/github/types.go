package github

import "time"

type CIState string

const (
	CIUnknown CIState = "UNKNOWN"
	CINone    CIState = "NONE"
	CISuccess CIState = "SUCCESS"
	CIPending CIState = "PENDING"
	CIFailure CIState = "FAILURE"
	CIError   CIState = "ERROR"
)

type PullRequest struct {
	Repository string
	Number     int
	Title      string
	URL        string
	Author     string
	Draft      bool
	UpdatedAt  time.Time
	CI         CIState
}

type RateResource struct {
	Limit     int       `json:"limit"`
	Remaining int       `json:"remaining"`
	Reset     time.Time `json:"-"`
	// Cost is the points the last GraphQL query consumed, as reported by
	// GitHub. Zero means unknown, which budgeting treats as one point.
	Cost int `json:"-"`
}

// CostPerQuery returns the points to reserve for one more query like the last.
func (r RateResource) CostPerQuery() int {
	return max(1, r.Cost)
}

func (r RateResource) HasCapacity(required int) bool {
	return r.Limit == 0 || r.Remaining >= required || !time.Now().Before(r.Reset)
}

type RateLimits struct {
	Search  RateResource
	GraphQL RateResource
}
