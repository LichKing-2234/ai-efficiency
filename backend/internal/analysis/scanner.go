package analysis

import (
	"context"

	"github.com/ai-efficiency/backend/internal/analysis/rules"
)

// ScanResult is the output of a scan.
type ScanResult struct {
	Score       int                    `json:"score"`
	Dimensions  []rules.DimensionScore `json:"dimensions"`
	Suggestions []rules.Suggestion     `json:"suggestions"`
	ScanType    string                 `json:"scan_type"`
	CommitSHA   string                 `json:"commit_sha,omitempty"`
}

// Scanner defines the interface for repo analysis scanners.
type Scanner interface {
	Scan(ctx context.Context, repoPath string) (*ScanResult, error)
}
