package activity

import "time"

const MetricContractVersion = "activity-v1"

type Window struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
}

type DetailPageOptions struct {
	PRLimit      int
	PRCursor     string
	CommitLimit  int
	CommitCursor string
	BucketLimit  int
	BucketCursor string
}

type RepositoryPageOptions struct {
	MemberLimit  int
	MemberCursor string
	PRLimit      int
	PRCursor     string
	CommitLimit  int
	CommitCursor string
}

type PageOptions struct {
	Limit  int
	Cursor string
}

type CountMetric struct {
	Value      int  `json:"value"`
	LowerBound bool `json:"lower_bound"`
}

type Metrics struct {
	ParticipatingPRs   CountMetric `json:"participating_prs"`
	MergedPRs          CountMetric `json:"merged_prs"`
	ActiveRepositories int         `json:"active_repositories"`
	CommitCount        int         `json:"commit_count"`
	LatestActivity     *time.Time  `json:"latest_activity,omitempty"`
}

type QualitySummary struct {
	MeasuredBuckets         int `json:"measured_buckets"`
	UnboundBuckets          int `json:"unbound_buckets"`
	MultiRepoSharedBuckets  int `json:"multi_repo_shared_buckets"`
	InvalidTokenFacts       int `json:"invalid_token_facts"`
	HistoricalAdvisoryFacts int `json:"historical_advisory_facts"`
	CoverageGapCount        int `json:"coverage_gap_count"`
}

type SyncCoverage struct {
	Complete                    bool `json:"complete"`
	AffectedRepositories        int  `json:"affected_repositories"`
	UnsyncedRepositories        int  `json:"unsynced_repositories"`
	StaleRepositories           int  `json:"stale_repositories"`
	PartiallySyncedRepositories int  `json:"partially_synced_repositories"`
	FailedRepositories          int  `json:"failed_repositories"`
}

type PRReference struct {
	RepoConfigID int `json:"repo_config_id"`
	PRRecordID   int `json:"pr_record_id"`
	SCMPRID      int `json:"scm_pr_id"`
}

type CommitReference struct {
	RepoConfigID int    `json:"repo_config_id"`
	CommitSHA    string `json:"commit_sha"`
}

type PullRequest struct {
	RepoConfigID   int               `json:"repo_config_id"`
	RepoName       string            `json:"repo_name"`
	PRRecordID     int               `json:"pr_record_id"`
	SCMPRID        int               `json:"scm_pr_id"`
	Title          string            `json:"title"`
	URL            string            `json:"url"`
	Status         string            `json:"status"`
	MergedAt       *time.Time        `json:"merged_at,omitempty"`
	CycleTimeHours *float64          `json:"cycle_time_hours,omitempty"`
	Commits        []CommitReference `json:"commits"`
}

type Commit struct {
	RepoConfigID    int           `json:"repo_config_id"`
	RepoName        string        `json:"repo_name"`
	CommitSHA       string        `json:"commit_sha"`
	Branch          string        `json:"branch,omitempty"`
	LatestActivity  time.Time     `json:"latest_activity"`
	ProcessedTokens int64         `json:"processed_tokens"`
	PRs             []PRReference `json:"prs"`
}

type BucketSummary struct {
	BucketID         string    `json:"bucket_id"`
	ObservedEnd      time.Time `json:"observed_end_at"`
	ProcessedTokens  int64     `json:"processed_tokens"`
	AllocationStatus string    `json:"allocation_status"`
}

type Page[T any] struct {
	Items      []T    `json:"items"`
	NextCursor string `json:"next_cursor,omitempty"`
}

type MemberIdentity struct {
	UserID                    int      `json:"user_id"`
	DirectoryMemberExternalID string   `json:"directory_member_external_id,omitempty"`
	DisplayName               string   `json:"display_name"`
	Email                     string   `json:"email"`
	DepartmentExternalIDs     []string `json:"department_external_ids"`
}

type MemberActivity struct {
	ContractVersion string              `json:"contract_version"`
	Window          Window              `json:"window"`
	Member          MemberIdentity      `json:"member"`
	Available       bool                `json:"available"`
	Metrics         Metrics             `json:"metrics"`
	Quality         QualitySummary      `json:"quality"`
	SyncCoverage    SyncCoverage        `json:"sync_coverage"`
	PRs             Page[PullRequest]   `json:"prs"`
	Commits         Page[Commit]        `json:"commits"`
	Buckets         Page[BucketSummary] `json:"buckets"`
	BucketAccess    bool                `json:"bucket_access"`
}

type Team struct {
	ExternalID       string  `json:"external_id"`
	ParentExternalID *string `json:"parent_external_id,omitempty"`
	Name             string  `json:"name"`
	DisplayPath      string  `json:"display_path"`
	MemberCount      int     `json:"member_count"`
}

type ScopeResponse struct {
	ContractVersion string `json:"contract_version"`
	ScopeVersion    string `json:"scope_version"`
	CanViewTeams    bool   `json:"can_view_teams"`
	Admin           bool   `json:"admin"`
	Representative  bool   `json:"representative"`
	Teams           []Team `json:"teams"`
}

type MemberRow struct {
	Member    MemberIdentity `json:"member"`
	Available bool           `json:"available"`
	Metrics   Metrics        `json:"metrics"`
	Quality   QualitySummary `json:"quality"`
}

type TeamActivity struct {
	ContractVersion string          `json:"contract_version"`
	ScopeVersion    string          `json:"scope_version"`
	Window          Window          `json:"window"`
	Team            Team            `json:"team"`
	ActiveMembers   int             `json:"active_members"`
	Metrics         Metrics         `json:"metrics"`
	SyncCoverage    SyncCoverage    `json:"sync_coverage"`
	Members         Page[MemberRow] `json:"members"`
}

type MembersActivity struct {
	ContractVersion string          `json:"contract_version"`
	ScopeVersion    string          `json:"scope_version"`
	Window          Window          `json:"window"`
	Members         Page[MemberRow] `json:"members"`
}

type RepositoryIdentity struct {
	RepoConfigID int    `json:"repo_config_id"`
	Name         string `json:"name"`
}

type RepositoryActivity struct {
	ContractVersion      string             `json:"contract_version"`
	ScopeVersion         string             `json:"scope_version"`
	Window               Window             `json:"window"`
	Repository           RepositoryIdentity `json:"repository"`
	ParticipatingMembers int                `json:"participating_members"`
	Metrics              Metrics            `json:"metrics"`
	SyncCoverage         SyncCoverage       `json:"sync_coverage"`
	Members              Page[MemberRow]    `json:"members"`
	PRs                  Page[PullRequest]  `json:"prs"`
	Commits              Page[Commit]       `json:"commits"`
}

type TokenBreakdown struct {
	FreshInput    int64 `json:"fresh_input_tokens"`
	CacheRead     int64 `json:"cache_read_tokens"`
	CacheWrite    int64 `json:"cache_write_tokens"`
	Output        int64 `json:"output_tokens"`
	Reasoning     int64 `json:"reasoning_tokens"`
	ProviderTotal int64 `json:"provider_total_tokens"`
	Processed     int64 `json:"processed_total_tokens"`
}

type AllocationRevisionDetail struct {
	RevisionID      string           `json:"revision_id"`
	Sequence        int              `json:"sequence"`
	Reason          string           `json:"reason"`
	EvidenceVersion string           `json:"evidence_version"`
	RestatedAt      time.Time        `json:"restated_at"`
	Allocations     []map[string]any `json:"allocations"`
}

type RequestIDEvidence struct {
	RequestID     string    `json:"request_id"`
	ObservedAt    time.Time `json:"observed_at"`
	Transport     string    `json:"transport"`
	StatusCode    int       `json:"status_code,omitempty"`
	ErrorCategory string    `json:"error_category,omitempty"`
	Failed        bool      `json:"failed"`
}

type RequestIDDetail struct {
	State    string              `json:"state"`
	Count    int                 `json:"count"`
	Evidence []RequestIDEvidence `json:"evidence"`
}

type BucketDetail struct {
	ContractVersion      string                   `json:"contract_version"`
	BucketID             string                   `json:"bucket_id"`
	OwnerUserID          int                      `json:"owner_user_id"`
	Tool                 string                   `json:"tool"`
	Model                string                   `json:"model"`
	ObservedStart        time.Time                `json:"observed_start_at"`
	ObservedEnd          time.Time                `json:"observed_end_at"`
	Tokens               TokenBreakdown           `json:"tokens"`
	TokenQuality         string                   `json:"token_quality"`
	CoverageGapCount     int                      `json:"coverage_gap_count"`
	ExtractorVersion     string                   `json:"extractor_version"`
	NormalizationVersion int                      `json:"normalization_version"`
	CorrelationQuality   string                   `json:"correlation_quality"`
	Revision             AllocationRevisionDetail `json:"revision"`
	RequestIDs           RequestIDDetail          `json:"request_ids"`
}
