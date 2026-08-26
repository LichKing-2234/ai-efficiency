package hooks

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ai-efficiency/ae-cli/internal/attributionlocal"
)

const syntheticFixtureRepoKey = "repo-host.example.com/org/repo"

type SyntheticFixtureQuarantineSummary struct {
	Version          int       `json:"version"`
	Workspaces       int       `json:"workspaces"`
	UnresolvedEvents int       `json:"unresolved_events"`
	MigratedAt       time.Time `json:"migrated_at"`
}

type syntheticFixtureMigrationJournal struct {
	Version     int       `json:"version"`
	ServerURL   string    `json:"server_url"`
	AuthSubject string    `json:"auth_subject"`
	StartedAt   time.Time `json:"started_at"`
}

func syntheticFixtureQuarantineRoot() string {
	return filepath.Join(attributionlocal.AttributionRootDir(), "quarantine", "synthetic-git-fixtures")
}

func syntheticFixtureQuarantineSummaryPath() string {
	return filepath.Join(syntheticFixtureQuarantineRoot(), "summary.json")
}

func LoadSyntheticFixtureQuarantineSummary() (SyntheticFixtureQuarantineSummary, error) {
	var summary SyntheticFixtureQuarantineSummary
	err := attributionlocal.LoadJSON(syntheticFixtureQuarantineSummaryPath(), &summary)
	if os.IsNotExist(err) {
		return SyntheticFixtureQuarantineSummary{}, nil
	}
	return summary, err
}

func QuarantineSyntheticFixtureBacklog(binding SyncTaskMigrationBinding, now time.Time) (SyntheticFixtureQuarantineSummary, error) {
	if normalizeHookServerURL(binding.ServerURL) == "" || strings.TrimSpace(binding.AuthSubject) == "" {
		return LoadSyntheticFixtureQuarantineSummary()
	}
	root := syntheticFixtureQuarantineRoot()
	var summary SyntheticFixtureQuarantineSummary
	owned, err := withMachineSyncMaintenanceOwnership(func() error {
		return withFileLock(filepath.Join(root, ".migration.lock"), func() error {
			previous, err := LoadSyntheticFixtureQuarantineSummary()
			if err != nil {
				return err
			}
			journalPath := filepath.Join(root, "migration.json")
			if err := attributionlocal.SaveJSON(journalPath, syntheticFixtureMigrationJournal{
				Version: 1, ServerURL: normalizeHookServerURL(binding.ServerURL), AuthSubject: strings.TrimSpace(binding.AuthSubject), StartedAt: now.UTC(),
			}); err != nil {
				return err
			}
			if err := quarantineSyntheticWorkspaces(root, binding); err != nil {
				return err
			}
			if err := quarantineSyntheticUnresolvedEvents(root, binding); err != nil {
				return err
			}
			workspaces, err := countDirectories(filepath.Join(root, "workspaces"))
			if err != nil {
				return err
			}
			unresolved, err := countSyntheticUnresolvedAudit(filepath.Join(root, "unresolved-hooks.jsonl"))
			if err != nil {
				return err
			}
			summary = SyntheticFixtureQuarantineSummary{
				Version: 1, Workspaces: workspaces, UnresolvedEvents: unresolved, MigratedAt: previous.MigratedAt,
			}
			if summary.MigratedAt.IsZero() && (workspaces > 0 || unresolved > 0) {
				summary.MigratedAt = now.UTC()
			}
			if workspaces > 0 || unresolved > 0 {
				if err := attributionlocal.SaveJSON(syntheticFixtureQuarantineSummaryPath(), summary); err != nil {
					return err
				}
			}
			if err := os.Remove(journalPath); err != nil && !os.IsNotExist(err) {
				return err
			}
			return nil
		})
	})
	if err != nil {
		return summary, err
	}
	if !owned {
		return LoadSyntheticFixtureQuarantineSummary()
	}
	return summary, err
}

func withMachineSyncMaintenanceOwnership(fn func() error) (bool, error) {
	lockPath := filepath.Join(attributionlocal.AttributionRootDir(), "machine-sync.run.lock")
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o700); err != nil {
		return false, err
	}
	for {
		payload, err := os.ReadFile(lockPath)
		if err == nil {
			pid, parseErr := strconv.Atoi(strings.TrimSpace(string(payload)))
			if parseErr == nil && pid == os.Getpid() {
				return true, fn()
			}
			if !machineSyncRunLockIsStale(lockPath) {
				return false, nil
			}
			_ = os.Remove(lockPath)
		} else if !os.IsNotExist(err) {
			return false, err
		}
		file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if os.IsExist(err) {
			continue
		}
		if err != nil {
			return false, err
		}
		_, _ = fmt.Fprintf(file, "%d\n", os.Getpid())
		_ = file.Close()
		defer func() { _ = os.Remove(lockPath) }()
		return true, fn()
	}
}

func quarantineSyntheticWorkspaces(root string, binding SyncTaskMigrationBinding) error {
	activeRoot := filepath.Join(attributionlocal.AttributionRootDir(), "workspaces")
	entries, err := os.ReadDir(activeRoot)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("list attribution workspaces for quarantine: %w", err)
	}
	quarantineRoot := filepath.Join(root, "workspaces")
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		task, err := LoadSyncTask(entry.Name())
		if err != nil {
			if isCorruptSyncTaskError(err) {
				continue
			}
			return fmt.Errorf("load workspace %s for quarantine: %w", entry.Name(), err)
		}
		if task == nil || strings.TrimSpace(task.RepoKey) != syntheticFixtureRepoKey ||
			normalizeHookServerURL(task.ServerURL) != normalizeHookServerURL(binding.ServerURL) ||
			strings.TrimSpace(task.AuthSubject) != strings.TrimSpace(binding.AuthSubject) {
			continue
		}
		if err := os.MkdirAll(quarantineRoot, 0o700); err != nil {
			return fmt.Errorf("create synthetic workspace quarantine: %w", err)
		}
		source := filepath.Join(activeRoot, entry.Name())
		target := filepath.Join(quarantineRoot, entry.Name())
		if _, err := os.Stat(target); err == nil {
			return fmt.Errorf("synthetic workspace quarantine already exists: %s", entry.Name())
		} else if !os.IsNotExist(err) {
			return err
		}
		if err := os.Rename(source, target); err != nil {
			return fmt.Errorf("quarantine synthetic workspace %s: %w", entry.Name(), err)
		}
	}
	return nil
}

func quarantineSyntheticUnresolvedEvents(root string, binding SyncTaskMigrationBinding) error {
	return withUnresolvedQueueLock(func() error {
		activePath := unresolvedQueuePath()
		payload, err := os.ReadFile(activePath)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read unresolved hook queue for quarantine: %w", err)
		}
		auditPath := filepath.Join(root, "unresolved-hooks.jsonl")
		audit, err := os.ReadFile(auditPath)
		if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("read synthetic unresolved audit: %w", err)
		}
		auditKeys := unresolvedEventKeys(audit)
		var kept bytes.Buffer
		for _, line := range splitJSONLLines(payload) {
			trimmed := bytes.TrimSpace(line)
			var event UnresolvedHookEvent
			if len(trimmed) == 0 || json.Unmarshal(trimmed, &event) != nil || strings.TrimSpace(event.RepoKey) != syntheticFixtureRepoKey ||
				normalizeHookServerURL(event.ServerURL) != normalizeHookServerURL(binding.ServerURL) ||
				strings.TrimSpace(event.AuthSubject) != strings.TrimSpace(binding.AuthSubject) {
				_, _ = kept.Write(line)
				continue
			}
			key := unresolvedDedupeKey(event)
			if _, exists := auditKeys[key]; !exists {
				audit = append(audit, line...)
				auditKeys[key] = struct{}{}
			}
		}
		if len(audit) > 0 {
			if err := writeAtomicState(auditPath, audit); err != nil {
				return err
			}
		}
		if kept.Len() == 0 {
			if err := os.Remove(activePath); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("remove quarantined unresolved queue: %w", err)
			}
			return nil
		}
		return writeAtomicState(activePath, kept.Bytes())
	})
}

func splitJSONLLines(payload []byte) [][]byte {
	if len(payload) == 0 {
		return nil
	}
	lines := bytes.SplitAfter(payload, []byte("\n"))
	if len(lines[len(lines)-1]) == 0 {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func unresolvedEventKeys(payload []byte) map[string]struct{} {
	keys := make(map[string]struct{})
	for _, line := range splitJSONLLines(payload) {
		var event UnresolvedHookEvent
		if json.Unmarshal(bytes.TrimSpace(line), &event) == nil {
			keys[unresolvedDedupeKey(event)] = struct{}{}
		}
	}
	return keys
}

func writeAtomicState(path string, payload []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := fmt.Sprintf("%s.%d.tmp", path, os.Getpid())
	if err := os.WriteFile(tmp, payload, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func countDirectories(root string) (int, error) {
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	count := 0
	for _, entry := range entries {
		if entry.IsDir() {
			count++
		}
	}
	return count, nil
}

func countSyntheticUnresolvedAudit(path string) (int, error) {
	payload, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return len(unresolvedEventKeys(payload)), nil
}
