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

type PRState string

const (
	PROpen   PRState = "OPEN"
	PRClosed PRState = "CLOSED"
	PRMerged PRState = "MERGED"
)

type SubmittedReview struct {
	ID          int64
	HeadOID     string
	State       string
	SubmittedAt time.Time
}

type ReviewObservation struct {
	Actor      string
	Reviews    []SubmittedReview
	Complete   bool
	ObservedAt time.Time
}

type PullRequest struct {
	Repository         string
	Number             int
	Title              string
	URL                string
	Author             string
	State              PRState
	Draft              bool
	UpdatedAt          time.Time
	CI                 CIState
	HeadOID            string
	BaseRefName        string
	BaseOID            string
	ViewerReviews      *ReviewObservation
	MetadataObservedAt time.Time
}

// CopyEnrichment copies the GraphQL observation without replacing Search data.
func (pr *PullRequest) CopyEnrichment(from PullRequest) {
	pr.ViewerReviews = nil
	if from.ViewerReviews != nil {
		observation := *from.ViewerReviews
		observation.Reviews = append([]SubmittedReview(nil), observation.Reviews...)
		pr.ViewerReviews = &observation
	}
	pr.CI = from.CI
	pr.HeadOID = from.HeadOID
	pr.BaseRefName = from.BaseRefName
	pr.BaseOID = from.BaseOID
	pr.MetadataObservedAt = from.MetadataObservedAt
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
