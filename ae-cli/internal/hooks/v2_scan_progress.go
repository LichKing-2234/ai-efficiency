package hooks

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ai-efficiency/ae-cli/internal/attributionlocal"
)

const v2ClaimScanProgressVersion = 2

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
