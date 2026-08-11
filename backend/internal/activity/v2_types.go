package activity

import (
	"context"
	"time"
)

const V2MetricContractVersion = "activity-v2"

type V2ScopeKind string

const (
	V2ScopePersonal V2ScopeKind = "personal"
	V2ScopeMember   V2ScopeKind = "member"
	V2ScopeTeam     V2ScopeKind = "team"
)

type V2Query struct {
	Scope      V2ScopeKind
	SubjectID  int
	TeamID     string
	FromDate   string
	ToDate     string
	Timezone   string
	RepoID     int
	PRRecordID int
}

type V2PageQuery struct {
	V2Query
	Search string
	Sort   string
	Cursor string
}

type V2Coverage struct {
	Complete   bool `json:"complete"`
	LowerBound bool `json:"lower_bound"`
}

type V2Ratio struct {
	State           string     `json:"state"`
	CommittedTokens int64      `json:"committed_tokens"`
	TotalTokens     *int64     `json:"total_tokens,omitempty"`
	Percent         *float64   `json:"percent,omitempty"`
	AsOf            *time.Time `json:"as_of,omitempty"`
}

type V2TrendPoint struct {
	Date           string `json:"date"`
	DirectTokens   int64  `json:"direct_tokens"`
	SharedTokens   int64  `json:"shared_tokens"`
	InvolvedTokens int64  `json:"involved_tokens"`
}

type V2Readiness struct {
	State           string     `json:"state"`
	FirstAcceptedAt *time.Time `json:"first_accepted_at,omitempty"`
}

type V2Overview struct {
	ContractVersion string         `json:"contract_version"`
	ScopeVersion    string         `json:"scope_version"`
	FromDate        string         `json:"from"`
	ToDate          string         `json:"to"`
	Timezone        string         `json:"timezone"`
	CommittedTokens int64          `json:"committed_tokens"`
	Coverage        V2Coverage     `json:"claim_coverage"`
	SCMCoverage     SyncCoverage   `json:"scm_coverage"`
	Ratio           V2Ratio        `json:"ratio"`
	Trend           []V2TrendPoint `json:"trend"`
	Readiness       V2Readiness    `json:"readiness"`
}

type V2RepositoryRow struct {
	RepoConfigID int      `json:"repo_config_id"`
	Name         string   `json:"name"`
	DirectTokens int64    `json:"direct_tokens"`
	DirectShare  *float64 `json:"direct_share,omitempty"`
	SharedTokens int64    `json:"shared_tokens"`
}

type V2PullRequestRow struct {
	PRRecordID     int    `json:"pr_record_id"`
	RepoConfigID   int    `json:"repo_config_id"`
	RepositoryName string `json:"repository_name"`
	SCMPRID        int    `json:"scm_pr_id"`
	Title          string `json:"title"`
	URL            string `json:"url"`
	Status         string `json:"status"`
	InvolvedTokens int64  `json:"involved_tokens"`
	OverlapState   string `json:"overlap_state"`
}

type V2Page[T any] struct {
	Items       []T           `json:"items"`
	NextCursor  string        `json:"next_cursor,omitempty"`
	SCMCoverage *SyncCoverage `json:"scm_coverage,omitempty"`
}

type V2DenominatorRequest struct {
	ActorUserID   int
	Scope         V2ScopeKind
	SubjectUserID int
	TeamID        string
	FromDate      string
	ToDate        string
	Timezone      string
	ScopeVersion  string
}

type V2Denominator struct {
	TotalTokens int64
	AsOf        time.Time
	FreshUntil  time.Time
	Fresh       bool
	Complete    bool
}

type V2MemberDenominatorCacheKey struct {
	ActorUserID     int    `json:"actor_user_id"`
	SubjectUserID   int    `json:"subject_user_id"`
	ScopeVersion    string `json:"scope_version"`
	ProviderID      int    `json:"provider_id"`
	ProviderVersion int64  `json:"provider_version"`
	BindingVersion  int64  `json:"binding_version"`
	FromDate        string `json:"from"`
	ToDate          string `json:"to"`
	Timezone        string `json:"timezone"`
}

type V2DenominatorReader interface {
	ResolveDenominator(context.Context, V2DenominatorRequest) (V2Denominator, error)
}
