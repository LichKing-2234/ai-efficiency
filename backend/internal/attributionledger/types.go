package attributionledger

import "time"

const (
	CurrentSchemaVersion = 1
	MaxBucketBatchSize   = 100
)

type TokenQuality string

const (
	TokenQualityMeasured           TokenQuality = "measured"
	TokenQualityHistoricalAdvisory TokenQuality = "historical_advisory"
	TokenQualityInvalid            TokenQuality = "invalid"
)

type AllocationStatus string

const (
	AllocationStatusBoundAuto       AllocationStatus = "bound_auto"
	AllocationStatusBoundManual     AllocationStatus = "bound_manual"
	AllocationStatusUnbound         AllocationStatus = "unbound"
	AllocationStatusOverhead        AllocationStatus = "overhead"
	AllocationStatusOutOfScope      AllocationStatus = "out_of_scope"
	AllocationStatusMultiRepoShared AllocationStatus = "multi_repo_shared"
)

type CorrelationQuality string

const (
	CorrelationQualityExact    CorrelationQuality = "exact"
	CorrelationQualityAdvisory CorrelationQuality = "advisory"
	CorrelationQualityUnlinked CorrelationQuality = "unlinked"
)

type Tokens struct {
	FreshInput    int64 `json:"fresh_input_tokens"`
	CacheRead     int64 `json:"cache_read_tokens"`
	CacheWrite    int64 `json:"cache_write_tokens"`
	Output        int64 `json:"output_tokens"`
	Reasoning     int64 `json:"reasoning_tokens"`
	ProviderTotal int64 `json:"provider_total_tokens"`
	Processed     int64 `json:"processed_total_tokens"`
}

func (t Tokens) Add(other Tokens) Tokens {
	return Tokens{
		FreshInput:    t.FreshInput + other.FreshInput,
		CacheRead:     t.CacheRead + other.CacheRead,
		CacheWrite:    t.CacheWrite + other.CacheWrite,
		Output:        t.Output + other.Output,
		Reasoning:     t.Reasoning + other.Reasoning,
		ProviderTotal: t.ProviderTotal + other.ProviderTotal,
		Processed:     t.Processed + other.Processed,
	}
}

type SessionSlice struct {
	ConversationID string    `json:"conversation_id"`
	ObservedStart  time.Time `json:"observed_start_at"`
	ObservedEnd    time.Time `json:"observed_end_at"`
	TokenAtomCount int       `json:"token_atom_count"`
	AtomSetDigest  string    `json:"atom_set_digest"`
}

type CommitReference struct {
	RepoConfigID int    `json:"repo_config_id"`
	RepoKey      string `json:"repo_key,omitempty"`
	WorkspaceID  string `json:"workspace_id"`
	CommitSHA    string `json:"commit_sha"`
	Branch       string `json:"branch,omitempty"`
	Lineage      string `json:"lineage"`
}

type AllocationTarget struct {
	Status                  AllocationStatus  `json:"status"`
	RepoConfigID            int               `json:"repo_config_id,omitempty"`
	RepoKey                 string            `json:"repo_key,omitempty"`
	WorkspaceID             string            `json:"workspace_id,omitempty"`
	CommitSHA               string            `json:"commit_sha,omitempty"`
	Branch                  string            `json:"branch,omitempty"`
	Lineage                 string            `json:"lineage,omitempty"`
	AssociatedRepoConfigIDs []int             `json:"associated_repo_config_ids,omitempty"`
	InheritedCommits        []CommitReference `json:"inherited_commits,omitempty"`
}

type Allocation struct {
	Target AllocationTarget `json:"target"`
	Tokens Tokens           `json:"tokens"`
}

type AllocationRevision struct {
	RevisionID      string       `json:"revision_id"`
	Sequence        int          `json:"sequence"`
	Reason          string       `json:"reason"`
	EvidenceVersion string       `json:"evidence_version"`
	Allocations     []Allocation `json:"allocations"`
	RestatedAt      time.Time    `json:"restated_at"`
}

type UsageBucket struct {
	SchemaVersion        int                `json:"schema_version"`
	BucketID             string             `json:"bucket_id"`
	Tool                 string             `json:"tool"`
	Model                string             `json:"model,omitempty"`
	ChangeSetID          string             `json:"change_set_id,omitempty"`
	SessionSlices        []SessionSlice     `json:"session_slices"`
	ObservedStart        time.Time          `json:"observed_start_at"`
	ObservedEnd          time.Time          `json:"observed_end_at"`
	Tokens               Tokens             `json:"tokens"`
	RequestCount         int                `json:"request_count"`
	SourceEventCount     int                `json:"source_event_count"`
	SourceDigest         string             `json:"source_digest"`
	ExtractorVersion     string             `json:"extractor_version"`
	NormalizationVersion int                `json:"normalization_version"`
	TokenQuality         TokenQuality       `json:"token_quality"`
	CoverageGapCount     int                `json:"coverage_gap_count"`
	InitialRevision      AllocationRevision `json:"initial_revision"`
}

type BatchRequest struct {
	Buckets []UsageBucket `json:"buckets"`
}

type BatchResult struct {
	Accepted           int `json:"accepted"`
	CreatedBuckets     int `json:"created_buckets"`
	DuplicateBuckets   int `json:"duplicate_buckets"`
	CreatedRevisions   int `json:"created_revisions"`
	DuplicateRevisions int `json:"duplicate_revisions"`
}

type RevisionRequest struct {
	SchemaVersion int `json:"schema_version"`
	AllocationRevision
}

type InstallationCredentials struct {
	InstallationID   string `json:"installation_id"`
	ReporterToken    string `json:"reporter_token,omitempty"`
	OTLPToken        string `json:"otlp_token,omitempty"`
	Created          bool   `json:"created"`
	ReportingEnabled bool   `json:"reporting_enabled"`
	OTelEnabled      bool   `json:"otel_enabled"`
}

type InstallationPrincipal struct {
	DatabaseID     int
	InstallationID string
	UserID         int
}

type Report struct {
	From               time.Time      `json:"from"`
	To                 time.Time      `json:"to"`
	MeasuredTokens     int64          `json:"measured_tokens"`
	BoundTokens        int64          `json:"bound_tokens"`
	UnboundTokens      int64          `json:"unbound_tokens"`
	SharedTokens       int64          `json:"shared_tokens"`
	HistoricalAdvisory int64          `json:"historical_advisory_tokens"`
	AllocationRate     float64        `json:"allocation_rate"`
	CoverageGapCount   int            `json:"coverage_gap_count"`
	RequestIDCoverage  int            `json:"request_id_coverage_count"`
	BucketCount        int            `json:"bucket_count"`
	Repositories       []RepoReport   `json:"repositories"`
	Evidence           EvidenceReport `json:"evidence"`
	Buckets            []BucketReport `json:"buckets"`
}

type RepoReport struct {
	RepoConfigID    int            `json:"repo_config_id"`
	RepoKey         string         `json:"repo_key"`
	Name            string         `json:"name"`
	Tokens          int64          `json:"tokens"`
	ProcessedTokens int64          `json:"processed_tokens"`
	UnboundTokens   int64          `json:"unbound_tokens"`
	SharedTokens    int64          `json:"shared_tokens"`
	InheritedTokens int64          `json:"inherited_tokens"`
	Worktrees       []string       `json:"worktrees"`
	Branches        []string       `json:"branches"`
	Commits         []CommitReport `json:"commits"`
}

type CommitReport struct {
	CommitSHA               string     `json:"commit_sha"`
	Lineage                 string     `json:"lineage"`
	Tokens                  int64      `json:"tokens"`
	InheritedTokens         int64      `json:"inherited_tokens"`
	InheritedFromCommitSHAs []string   `json:"inherited_from_commit_shas"`
	PRs                     []PRReport `json:"prs"`
}

type PRReport struct {
	ID      int    `json:"id"`
	SCMPRID int    `json:"scm_pr_id"`
	Title   string `json:"title"`
	URL     string `json:"url"`
	Status  string `json:"status"`
}

type EvidenceReport struct {
	MeasuredBuckets            int `json:"measured_buckets"`
	HistoricalAdvisoryBuckets  int `json:"historical_advisory_buckets"`
	InvalidBuckets             int `json:"invalid_buckets"`
	ExactCorrelationBuckets    int `json:"exact_correlation_buckets"`
	AdvisoryCorrelationBuckets int `json:"advisory_correlation_buckets"`
	UnlinkedCorrelationBuckets int `json:"unlinked_correlation_buckets"`
}

type BucketReport struct {
	BucketID                 string             `json:"bucket_id"`
	Tool                     string             `json:"tool"`
	Model                    string             `json:"model"`
	ObservedStart            time.Time          `json:"observed_start_at"`
	ObservedEnd              time.Time          `json:"observed_end_at"`
	Tokens                   Tokens             `json:"tokens"`
	RequestCount             int                `json:"request_count"`
	TokenQuality             TokenQuality       `json:"token_quality"`
	CorrelationQuality       CorrelationQuality `json:"request_correlation_quality"`
	RequestIDCoverageCount   int                `json:"request_id_coverage_count"`
	CoverageGapCount         int                `json:"coverage_gap_count"`
	AllocationStatus         string             `json:"allocation_status"`
	AllocationRevision       int                `json:"allocation_revision"`
	AllocationRevisionReason string             `json:"allocation_revision_reason"`
}
