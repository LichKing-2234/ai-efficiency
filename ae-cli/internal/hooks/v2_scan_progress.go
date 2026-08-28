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

const v2ClaimScanProgressVersion = 7

type V2ClaimScanProgress struct {
	Version            int                 `json:"version"`
	WorkspaceID        string              `json:"workspace_id"`
	ContextID          string              `json:"context_id"`
	SourceKeys         []string            `json:"source_keys"`
	CompletedUnits     []string            `json:"completed_units,omitempty"`
	SourceTurnKeys     map[string][]string `json:"source_turn_keys,omitempty"`
	SourceEvidenceKeys map[string]string   `json:"source_evidence_keys,omitempty"`
	UnprovenCommits    []V2UnprovenCommit  `json:"unproven_commits,omitempty"`
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

// V2UnprovenCommit is a commit whose evidence had not arrived when its own
// post-commit scan ran.
//
// For Claude Code the mutation reaches Pilot's output only when the turn ends,
// and a developer who edits and commits inside one turn commits first. The scan
// that runs then sees no evidence and, without this, the commit would never be
// looked at again: the commits a scan considers come from the triggers of the
// task that started it, and that task is finished.
//
// Keeping the commit here turns a permanent loss into a delay. It is retried on
// every later scan until it is proven or until its evidence has aged out.
type V2UnprovenCommit struct {
	CommitSHA         string    `json:"commit_sha"`
	CheckpointEventID string    `json:"checkpoint_event_id"`
	RepoRoot          string    `json:"repo_root"`
	RepoKey           string    `json:"repo_key,omitempty"`
	RepoConfigID      int       `json:"repo_config_id"`
	RelayProviderID   int       `json:"relay_provider_id"`
	WorkspaceID       string    `json:"workspace_id"`
	FirstSeenAt       time.Time `json:"first_seen_at"`
}

func (c V2UnprovenCommit) scanOptions() attributionlocal.V2ClaimScanOptions {
	return attributionlocal.V2ClaimScanOptions{
		RepoRoot:          c.RepoRoot,
		CommitSHA:         c.CommitSHA,
		RelayProviderID:   c.RelayProviderID,
		RepoConfigID:      c.RepoConfigID,
		RepoKey:           c.RepoKey,
		WorkspaceID:       c.WorkspaceID,
		CheckpointEventID: c.CheckpointEventID,
	}
}

func unprovenCommitKey(option attributionlocal.V2ClaimScanOptions) string {
	return strings.TrimSpace(option.CommitSHA) + "\x00" + strings.TrimSpace(option.CheckpointEventID)
}

// mergeUnprovenCommits records the commits a scan could not prove and forgets
// the ones it could.
//
// Commits whose evidence is older than the window a scan reads are dropped:
// retrying them would cost a scan every time and can no longer succeed.
func mergeUnprovenCommits(existing []V2UnprovenCommit, scanned []attributionlocal.V2ClaimScanOptions, proven map[string]struct{}, now time.Time) []V2UnprovenCommit {
	byKey := make(map[string]V2UnprovenCommit, len(existing)+len(scanned))
	order := make([]string, 0, len(existing)+len(scanned))
	remember := func(item V2UnprovenCommit) {
		key := unprovenCommitKey(item.scanOptions())
		if _, ok := byKey[key]; !ok {
			order = append(order, key)
		}
		byKey[key] = item
	}
	for _, item := range existing {
		remember(item)
	}
	for _, option := range scanned {
		key := unprovenCommitKey(option)
		if _, ok := byKey[key]; ok {
			continue
		}
		remember(V2UnprovenCommit{
			CommitSHA: strings.TrimSpace(option.CommitSHA), CheckpointEventID: strings.TrimSpace(option.CheckpointEventID),
			RepoRoot: option.RepoRoot, RepoKey: option.RepoKey, RepoConfigID: option.RepoConfigID,
			RelayProviderID: option.RelayProviderID, WorkspaceID: option.WorkspaceID, FirstSeenAt: now,
		})
	}

	out := make([]V2UnprovenCommit, 0, len(order))
	for _, key := range order {
		item := byKey[key]
		if _, ok := proven[key]; ok {
			continue
		}
		if strings.TrimSpace(item.CommitSHA) == "" || strings.TrimSpace(item.CheckpointEventID) == "" {
			continue
		}
		if !item.FirstSeenAt.IsZero() && now.Sub(item.FirstSeenAt) > v2ClaimSourceWindow {
			continue
		}
		out = append(out, item)
	}
	return out
}

// provenCommitKeys names the commits a batch of candidates actually proved. A
// candidate carrying a gap proved nothing, and a commit only claimed by such
// candidates stays pending.
func provenCommitKeys(candidates []attributionlocal.V2ClaimCandidate) map[string]struct{} {
	proven := map[string]struct{}{}
	for _, candidate := range candidates {
		if strings.TrimSpace(candidate.GapReason) != "" {
			continue
		}
		for _, allocation := range candidate.Group.CommitAllocations {
			if strings.TrimSpace(allocation.EvidenceDigest) == "" {
				continue
			}
			proven[strings.TrimSpace(allocation.CommitSHA)+"\x00"+strings.TrimSpace(allocation.CheckpointEventID)] = struct{}{}
		}
	}
	return proven
}
