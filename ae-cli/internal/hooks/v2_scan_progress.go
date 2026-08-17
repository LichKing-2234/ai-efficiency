package hooks

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ai-efficiency/ae-cli/internal/attributionlocal"
)

const v2ClaimScanProgressVersion = 3

type V2ClaimScanProgress struct {
	Version            int                 `json:"version"`
	WorkspaceID        string              `json:"workspace_id"`
	ContextID          string              `json:"context_id"`
	SourceKeys         []string            `json:"source_keys"`
	CompletedUnits     []string            `json:"completed_units,omitempty"`
	SourceTurnKeys     map[string][]string `json:"source_turn_keys,omitempty"`
	SourceEvidenceKeys map[string]string   `json:"source_evidence_keys,omitempty"`
	StartedAt          time.Time           `json:"started_at"`
	Complete           bool                `json:"complete,omitempty"`
}

func V2ClaimScanProgressPath(workspaceID string) (string, error) {
	if strings.TrimSpace(workspaceID) == "" {
		return "", fmt.Errorf("workspace_id is required")
	}
	return filepath.Join(attributionlocal.AttributionRootDir(), "workspaces", workspaceID, "v2-claim-scan.json"), nil
}

func LoadV2ClaimScanProgress(workspaceID string) (*V2ClaimScanProgress, error) {
	path, err := V2ClaimScanProgressPath(workspaceID)
	if err != nil {
		return nil, fmt.Errorf("resolve v2 claim scan progress path: %w", err)
	}
	var progress V2ClaimScanProgress
	if err := attributionlocal.LoadJSON(path, &progress); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("load v2 claim scan progress: %w", err)
	}
	return &progress, nil
}

func SaveV2ClaimScanProgress(progress *V2ClaimScanProgress) error {
	if progress == nil {
		return fmt.Errorf("v2 claim scan progress is nil")
	}
	path, err := V2ClaimScanProgressPath(progress.WorkspaceID)
	if err != nil {
		return fmt.Errorf("resolve v2 claim scan progress path: %w", err)
	}
	if progress.Version == 0 {
		progress.Version = v2ClaimScanProgressVersion
	}
	if err := attributionlocal.SaveJSON(path, progress); err != nil {
		return fmt.Errorf("save v2 claim scan progress: %w", err)
	}
	return nil
}

func DeleteV2ClaimScanProgress(workspaceID string) error {
	path, err := V2ClaimScanProgressPath(workspaceID)
	if err != nil {
		return fmt.Errorf("resolve v2 claim scan progress path: %w", err)
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete v2 claim scan progress: %w", err)
	}
	return nil
}

func migrateV2ClaimScanProgress(workspaceID string, now time.Time) (bool, error) {
	progress, err := LoadV2ClaimScanProgress(workspaceID)
	if err != nil || progress == nil {
		return false, err
	}
	if progress.Version > v2ClaimScanProgressVersion {
		return false, nil
	}
	if progress.Version < v2ClaimScanProgressVersion {
		progress.Version = v2ClaimScanProgressVersion
		progress.SourceKeys = nil
		progress.CompletedUnits = nil
		progress.SourceTurnKeys = map[string][]string{}
		progress.SourceEvidenceKeys = map[string]string{}
		progress.StartedAt = now.UTC()
		progress.Complete = false
		if err := SaveV2ClaimScanProgress(progress); err != nil {
			return false, fmt.Errorf("rebuild stale v2 claim scan progress: %w", err)
		}
		return true, nil
	}
	changed := deduplicateStrings(&progress.SourceKeys)
	if deduplicateStrings(&progress.CompletedUnits) {
		changed = true
	}
	for sourceKey, turnKeys := range progress.SourceTurnKeys {
		if deduplicateStrings(&turnKeys) {
			progress.SourceTurnKeys[sourceKey] = turnKeys
			changed = true
		}
	}
	if changed {
		if err := SaveV2ClaimScanProgress(progress); err != nil {
			return false, fmt.Errorf("save deduplicated v2 claim scan progress: %w", err)
		}
	}
	return changed, nil
}

func deduplicateStrings(values *[]string) bool {
	if values == nil || len(*values) < 2 {
		return false
	}
	original := append([]string(nil), (*values)...)
	sort.Strings(*values)
	kept := (*values)[:0]
	for _, value := range *values {
		if len(kept) == 0 || kept[len(kept)-1] != value {
			kept = append(kept, value)
		}
	}
	*values = kept
	return len(original) != len(kept) || !equalStrings(original, kept)
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
