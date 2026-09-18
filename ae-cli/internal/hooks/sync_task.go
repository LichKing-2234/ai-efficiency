package hooks

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ai-efficiency/ae-cli/internal/attributionlocal"
)

type SyncTaskStatus string
type SyncTaskFailureStage string

const (
	SyncTaskStatusPending SyncTaskStatus = "pending"
	SyncTaskStatusRunning SyncTaskStatus = "running"
	SyncTaskStatusYielded SyncTaskStatus = "yielded"

	SyncTaskFailureStageSync            SyncTaskFailureStage = "sync"
	SyncTaskFailureStageRunner          SyncTaskFailureStage = "runner"
	SyncTaskFailureStageLocalState      SyncTaskFailureStage = "local_state"
	SyncTaskFailureStageSourceDiscovery SyncTaskFailureStage = "source_discovery"
	SyncTaskFailureStageSourceScan      SyncTaskFailureStage = "source_scan"
	SyncTaskFailureStageBackendDelivery SyncTaskFailureStage = "backend_delivery"
	SyncTaskFailureStageAcknowledgement SyncTaskFailureStage = "acknowledgement"
)

var ErrSyncTaskAlreadyRunning = errors.New("sync task already running")

var syncTaskRunnerAlive = syncTaskProcessAlive
var machineSyncLockPollInterval = 25 * time.Millisecond

const (
	syncTaskVersion        = 3
	v2SyncTriggerRetention = 90 * 24 * time.Hour
	v2SyncTriggerExpiring  = 7 * 24 * time.Hour
)

type SyncTask struct {
	Version               int                  `json:"version"`
	WorkspaceID           string               `json:"workspace_id"`
	RepoRoot              string               `json:"repo_root"`
	ServerURL             string               `json:"server_url"`
	AuthSubject           string               `json:"auth_subject"`
	RepoConfigID          int                  `json:"repo_config_id"`
	RepoKey               string               `json:"repo_key"`
	TriggerKind           string               `json:"trigger_kind,omitempty"`
	TriggerEventID        string               `json:"trigger_event_id,omitempty"`
	TriggerCommitSHA      string               `json:"trigger_commit_sha,omitempty"`
	TriggerBranch         string               `json:"trigger_branch,omitempty"`
	Status                SyncTaskStatus       `json:"status"`
	LastRequestedAt       time.Time            `json:"last_requested_at"`
	LastStartedAt         *time.Time           `json:"last_started_at,omitempty"`
	LastCompletedAt       *time.Time           `json:"last_completed_at,omitempty"`
	LastError             string               `json:"last_error,omitempty"`
	LastFailureStage      SyncTaskFailureStage `json:"last_failure_stage,omitempty"`
	LastFailureReason     string               `json:"last_failure_reason,omitempty"`
	FirstFailureAt        *time.Time           `json:"first_failure_at,omitempty"`
	RemainingTriggerCount int                  `json:"remaining_trigger_count,omitempty"`
	AttemptCount          int                  `json:"attempt_count"`
	RunnerPID             int                  `json:"runner_pid,omitempty"`
	LeaseExpiresAt        *time.Time           `json:"lease_expires_at,omitempty"`
	LastSpawnAttemptAt    *time.Time           `json:"last_spawn_attempt_at,omitempty"`
	V2Triggers            []V2SyncTrigger      `json:"v2_triggers,omitempty"`
	RequestGeneration     int                  `json:"request_generation,omitempty"`
}

type MachineSyncTaskSummary struct {
	Queued      int
	Running     int
	Yielded     int
	Recoverable int
	Terminal    int
	Expiring    int
}

type SyncTaskMigrationBinding struct {
	ServerURL       string
	AuthSubject     string
	RelayProviderID int
}

type MachineSyncTaskMigrationSummary struct {
	Scanned  int
	Migrated int
	Deferred int
}

type syncTaskStageError struct {
	stage  SyncTaskFailureStage
	reason string
	err    error
}

func (e *syncTaskStageError) Error() string { return e.err.Error() }
func (e *syncTaskStageError) Unwrap() error { return e.err }

func syncTaskFailure(stage SyncTaskFailureStage, reason string, err error) error {
	if err == nil {
		return nil
	}
	return &syncTaskStageError{stage: stage, reason: reason, err: err}
}

type V2SyncTrigger struct {
	Kind            string    `json:"kind"`
	EventID         string    `json:"event_id"`
	ServerURL       string    `json:"server_url,omitempty"`
	AuthSubject     string    `json:"auth_subject,omitempty"`
	RepoConfigID    int       `json:"repo_config_id,omitempty"`
	RepoKey         string    `json:"repo_key,omitempty"`
	WorkspaceID     string    `json:"workspace_id,omitempty"`
	CommitSHA       string    `json:"commit_sha,omitempty"`
	Branch          string    `json:"branch,omitempty"`
	RewriteType     string    `json:"rewrite_type,omitempty"`
	OldCommitSHA    string    `json:"old_commit_sha,omitempty"`
	NewCommitSHA    string    `json:"new_commit_sha,omitempty"`
	CapturedAt      time.Time `json:"captured_at"`
	RelayProviderID int       `json:"relay_provider_id,omitempty"`
}

func SyncTaskPath(workspaceID string) (string, error) {
	if strings.TrimSpace(workspaceID) == "" {
		return "", fmt.Errorf("workspace_id is required")
	}
	return filepath.Join(attributionlocal.AttributionRootDir(), "workspaces", workspaceID, "sync-task.json"), nil
}

func syncTaskLockPath(workspaceID string) (string, error) {
	taskPath, err := SyncTaskPath(workspaceID)
	if err != nil {
		return "", err
	}
	return taskPath + ".lock", nil
}

func LoadSyncTask(workspaceID string) (*SyncTask, error) {
	path, err := SyncTaskPath(workspaceID)
	if err != nil {
		return nil, err
	}
	var task SyncTask
	if err := attributionlocal.LoadJSON(path, &task); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return &task, nil
}

func ListSyncTasks() ([]SyncTask, error) {
	root := filepath.Join(attributionlocal.AttributionRootDir(), "workspaces")
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("list sync task workspaces: %w", err)
	}
	tasks := make([]SyncTask, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		task, _, err := LoadSyncTaskRecovering(entry.Name())
		if err != nil {
			return nil, fmt.Errorf("load sync task %s: %w", entry.Name(), err)
		}
		if task != nil {
			tasks = append(tasks, *task)
		}
	}
	sort.Slice(tasks, func(i, j int) bool {
		if tasks[i].LastRequestedAt.Equal(tasks[j].LastRequestedAt) {
			return tasks[i].WorkspaceID < tasks[j].WorkspaceID
		}
		return tasks[i].LastRequestedAt.Before(tasks[j].LastRequestedAt)
	})
	return tasks, nil
}

func SummarizeMachineSyncTasks(now time.Time) (MachineSyncTaskSummary, error) {
	tasks, err := ListSyncTasks()
	if err != nil {
		return MachineSyncTaskSummary{}, err
	}
	var summary MachineSyncTaskSummary
	for index := range tasks {
		task := &tasks[index]
		switch {
		case task.HasActiveLease(now):
			summary.Running++
		case syncTaskRequiresRecovery(*task):
			summary.Recoverable++
		case task.Status == SyncTaskStatusYielded:
			summary.Yielded++
		default:
			summary.Queued++
		}
		if syncTaskExpiresSoon(*task, now) {
			summary.Expiring++
		}
	}
	state, err := attributionlocal.LoadV2ClaimState()
	if err != nil {
		return MachineSyncTaskSummary{}, fmt.Errorf("load v2 claim state for machine summary: %w", err)
	}
	summary.Terminal = attributionlocal.SummarizeV2ClaimDelivery(state).Conflict
	return summary, nil
}

func MigrateMachineSyncBacklog(binding SyncTaskMigrationBinding, now time.Time) (MachineSyncTaskMigrationSummary, error) {
	var summary MachineSyncTaskMigrationSummary
	if _, err := QuarantineSyntheticFixtureBacklog(binding, now); err != nil {
		return summary, fmt.Errorf("quarantine synthetic fixture backlog: %w", err)
	}
	if strings.TrimSpace(binding.ServerURL) == "" || strings.TrimSpace(binding.AuthSubject) == "" || binding.RelayProviderID <= 0 {
		return summary, nil
	}
	tasks, err := ListSyncTasks()
	if err != nil {
		return summary, err
	}
	for _, task := range tasks {
		if normalizeHookServerURL(task.ServerURL) != normalizeHookServerURL(binding.ServerURL) || strings.TrimSpace(task.AuthSubject) != strings.TrimSpace(binding.AuthSubject) {
			continue
		}
		summary.Scanned++
		changed, err := migrateSyncTask(task.WorkspaceID, binding.RelayProviderID, now)
		if err != nil {
			summary.Deferred++
			continue
		}
		if changed {
			summary.Migrated++
		}
	}
	return summary, nil
}

func migrateSyncTask(workspaceID string, relayProviderID int, now time.Time) (bool, error) {
	changed := false
	err := withSyncTaskLock(workspaceID, now, func() error {
		task, err := LoadSyncTask(workspaceID)
		if err != nil || task == nil {
			return err
		}
		if task.HasActiveLease(now) {
			return ErrSyncTaskAlreadyRunning
		}
		if task.Version > syncTaskVersion {
			return nil
		}
		triggers, err := upgradedSyncTaskTriggers(*task, relayProviderID)
		if err != nil {
			if diagnosticErr := markSyncTaskMigrationRecovery(task, now); diagnosticErr != nil {
				return fmt.Errorf("save legacy trigger migration diagnostic: %w", diagnosticErr)
			}
			return fmt.Errorf("deduplicate migrated triggers: %w", err)
		}
		if task.Version != syncTaskVersion || !sameV2SyncTriggerList(task.V2Triggers, triggers) {
			task.Version = syncTaskVersion
			task.V2Triggers = triggers
			changed = true
		}
		if task.RequestGeneration == 0 {
			task.RequestGeneration = 1
			changed = true
		}
		if task.Status == "" {
			task.Status = SyncTaskStatusPending
			changed = true
		}
		if strings.TrimSpace(task.LastError) != "" && task.LastFailureStage == "" {
			task.LastFailureStage = SyncTaskFailureStageLocalState
			task.LastFailureReason = "legacy attribution state requires recovery"
			if task.FirstFailureAt == nil {
				failedAt := task.LastRequestedAt.UTC()
				task.FirstFailureAt = &failedAt
			}
			task.RemainingTriggerCount = len(task.V2Triggers)
			changed = true
		}
		progressChanged, err := migrateV2ClaimScanProgress(workspaceID, now)
		if err != nil {
			return err
		}
		changed = changed || progressChanged
		if changed {
			if err := SaveSyncTask(*task); err != nil {
				return fmt.Errorf("save migrated sync task: %w", err)
			}
		}
		return nil
	})
	return changed, err
}

func markSyncTaskMigrationRecovery(task *SyncTask, now time.Time) error {
	if task == nil || task.LastFailureStage != "" || strings.TrimSpace(task.LastFailureReason) != "" {
		return nil
	}
	task.LastFailureStage = SyncTaskFailureStageLocalState
	task.LastFailureReason = "legacy trigger migration requires recovery"
	if task.FirstFailureAt == nil {
		failedAt := now.UTC()
		task.FirstFailureAt = &failedAt
	}
	task.RemainingTriggerCount = len(task.V2Triggers)
	return SaveSyncTask(*task)
}

func sameV2SyncTriggerList(left, right []V2SyncTrigger) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if !sameV2SyncTrigger(left[index], right[index]) || !left[index].CapturedAt.Equal(right[index].CapturedAt) {
			return false
		}
	}
	return true
}

func syncTaskExpiresSoon(task SyncTask, now time.Time) bool {
	cutoff := now.UTC().Add(-v2SyncTriggerRetention)
	warning := cutoff.Add(v2SyncTriggerExpiring)
	for _, trigger := range syncTaskDiagnosticTriggers(task) {
		if !trigger.CapturedAt.IsZero() && trigger.CapturedAt.After(cutoff) && !trigger.CapturedAt.After(warning) {
			return true
		}
	}
	return false
}

func syncTaskRequiresRecovery(task SyncTask) bool {
	if strings.TrimSpace(task.LastError) != "" {
		return true
	}
	for _, trigger := range syncTaskCommitTriggers(syncTaskDiagnosticTriggers(task)) {
		if trigger.RelayProviderID <= 0 {
			return true
		}
	}
	return false
}

func syncTaskDiagnosticTriggers(task SyncTask) []V2SyncTrigger {
	if len(task.V2Triggers) > 0 || strings.TrimSpace(task.TriggerEventID) == "" {
		return task.V2Triggers
	}
	return []V2SyncTrigger{{
		Kind: task.TriggerKind, EventID: task.TriggerEventID, CommitSHA: task.TriggerCommitSHA,
		Branch: task.TriggerBranch, CapturedAt: task.LastRequestedAt,
	}}
}

func LoadSyncTaskRecovering(workspaceID string) (*SyncTask, bool, error) {
	task, err := LoadSyncTask(workspaceID)
	if err == nil {
		return task, false, nil
	}
	if !isCorruptSyncTaskError(err) {
		return nil, false, err
	}
	if quarantineErr := quarantineCorruptSyncTask(workspaceID, time.Now().UTC()); quarantineErr != nil {
		return nil, false, quarantineErr
	}
	return nil, true, nil
}

func SaveSyncTask(task SyncTask) error {
	path, err := SyncTaskPath(task.WorkspaceID)
	if err != nil {
		return err
	}
	if task.Version == 0 {
		task.Version = syncTaskVersion
	}
	return attributionlocal.SaveJSON(path, task)
}

func DeleteSyncTask(workspaceID string) error {
	path, err := SyncTaskPath(workspaceID)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func pruneExpiredV2SyncTriggers(task *SyncTask, now time.Time, retention time.Duration) (bool, error) {
	if task == nil {
		return false, fmt.Errorf("task is nil")
	}
	deleted := false
	err := withSyncTaskLock(task.WorkspaceID, now, func() error {
		current, err := LoadSyncTask(task.WorkspaceID)
		if err != nil {
			return fmt.Errorf("load sync task for trigger cleanup: %w", err)
		}
		if current == nil {
			deleted = true
			return nil
		}
		cutoff := now.UTC().Add(-retention)
		kept := make([]V2SyncTrigger, 0, len(current.V2Triggers))
		for _, trigger := range current.V2Triggers {
			if trigger.CapturedAt.IsZero() || trigger.CapturedAt.After(cutoff) {
				kept = append(kept, trigger)
			}
		}
		if len(kept) == len(current.V2Triggers) {
			*task = *current
			return nil
		}
		if len(kept) == 0 {
			if err := DeleteV2ClaimScanProgress(current.WorkspaceID); err != nil {
				return fmt.Errorf("delete expired v2 scan progress: %w", err)
			}
			if err := DeleteSyncTask(current.WorkspaceID); err != nil {
				return fmt.Errorf("delete expired sync task: %w", err)
			}
			deleted = true
			return nil
		}
		current.V2Triggers = kept
		if current.FirstFailureAt != nil {
			current.RemainingTriggerCount = len(kept)
		}
		if err := SaveSyncTask(*current); err != nil {
			return fmt.Errorf("save pruned sync task: %w", err)
		}
		*task = *current
		return nil
	})
	return deleted, err
}

func (t *SyncTask) HasActiveLease(now time.Time) bool {
	return t != nil &&
		t.Status == SyncTaskStatusRunning &&
		t.RunnerPID != 0 &&
		t.LeaseExpiresAt != nil &&
		t.LeaseExpiresAt.After(now) &&
		syncTaskRunnerAlive(t.RunnerPID)
}

func RecoverInactiveSyncTaskRunner(workspaceID string, now time.Time) (*SyncTask, bool, error) {
	var out *SyncTask
	recovered := false
	err := withSyncTaskLock(workspaceID, now, func() error {
		current, _, err := LoadSyncTaskRecovering(workspaceID)
		if err != nil || current == nil {
			return err
		}
		out = current
		if current.Status != SyncTaskStatusRunning ||
			current.RunnerPID == 0 ||
			current.LeaseExpiresAt == nil ||
			!current.LeaseExpiresAt.After(now) ||
			syncTaskRunnerAlive(current.RunnerPID) {
			return nil
		}
		current.Status = SyncTaskStatusPending
		current.RunnerPID = 0
		current.LeaseExpiresAt = nil
		current.LastError = "runner exited before updating sync task"
		current.LastFailureStage = SyncTaskFailureStageRunner
		current.LastFailureReason = "runner exited before updating sync task"
		if current.FirstFailureAt == nil {
			failedAt := now.UTC()
			current.FirstFailureAt = &failedAt
		}
		current.RemainingTriggerCount = len(current.V2Triggers)
		if err := SaveSyncTask(*current); err != nil {
			return err
		}
		out = current
		recovered = true
		return nil
	})
	return out, recovered, err
}

func UpsertPendingSyncTask(next SyncTask) error {
	now := time.Now().UTC()
	return withSyncTaskLock(next.WorkspaceID, now, func() error {
		current, _, err := LoadSyncTaskRecovering(next.WorkspaceID)
		if err != nil {
			return err
		}
		if current != nil {
			if current.Version < syncTaskVersion {
				providerID := 0
				for _, trigger := range next.V2Triggers {
					if trigger.RelayProviderID > 0 {
						providerID = trigger.RelayProviderID
						break
					}
				}
				upgraded, upgradeErr := upgradedSyncTaskTriggers(*current, providerID)
				if upgradeErr != nil {
					return upgradeErr
				}
				current.Version = syncTaskVersion
				current.V2Triggers = upgraded
			}
			var mergeErr error
			next.V2Triggers, mergeErr = mergeV2SyncTriggers(current.V2Triggers, next.V2Triggers)
			if mergeErr != nil {
				current.LastError = mergeErr.Error()
				if err := SaveSyncTask(*current); err != nil {
					return err
				}
				return mergeErr
			}
			next.Version = current.Version
			next.RequestGeneration = current.RequestGeneration + 1
			next.AttemptCount = current.AttemptCount
			next.LastStartedAt = current.LastStartedAt
			next.LastCompletedAt = current.LastCompletedAt
			next.LastError = current.LastError
			next.LastFailureStage = current.LastFailureStage
			next.LastFailureReason = current.LastFailureReason
			next.FirstFailureAt = current.FirstFailureAt
			if current.FirstFailureAt != nil {
				next.RemainingTriggerCount = len(next.V2Triggers)
			}
			next.LastSpawnAttemptAt = current.LastSpawnAttemptAt
			if current.HasActiveLease(now) {
				next.RunnerPID = current.RunnerPID
				next.LeaseExpiresAt = current.LeaseExpiresAt
				next.Status = current.Status
			}
		}
		if next.RequestGeneration == 0 {
			next.RequestGeneration = 1
		}
		if next.Status == "" {
			next.Status = SyncTaskStatusPending
		}
		return SaveSyncTask(next)
	})
}

func upgradedSyncTaskTriggers(task SyncTask, relayProviderID int) ([]V2SyncTrigger, error) {
	triggers := append([]V2SyncTrigger(nil), task.V2Triggers...)
	if len(triggers) == 0 && strings.TrimSpace(task.TriggerEventID) != "" {
		triggers = append(triggers, V2SyncTrigger{
			Kind: task.TriggerKind, EventID: task.TriggerEventID, CommitSHA: task.TriggerCommitSHA,
			Branch: task.TriggerBranch, CapturedAt: task.LastRequestedAt,
		})
	}
	for index := range triggers {
		if triggers[index].RelayProviderID == 0 {
			triggers[index].RelayProviderID = relayProviderID
		}
		if strings.TrimSpace(triggers[index].ServerURL) == "" {
			triggers[index].ServerURL = task.ServerURL
		}
		if strings.TrimSpace(triggers[index].AuthSubject) == "" {
			triggers[index].AuthSubject = task.AuthSubject
		}
		if triggers[index].RepoConfigID == 0 {
			triggers[index].RepoConfigID = task.RepoConfigID
		}
		if strings.TrimSpace(triggers[index].RepoKey) == "" {
			triggers[index].RepoKey = task.RepoKey
		}
		if strings.TrimSpace(triggers[index].WorkspaceID) == "" {
			triggers[index].WorkspaceID = task.WorkspaceID
		}
	}
	return mergeV2SyncTriggers(nil, triggers)
}

func AppendV2SyncTrigger(workspaceID string, trigger V2SyncTrigger) error {
	now := time.Now().UTC()
	return withSyncTaskLock(workspaceID, now, func() error {
		current, _, err := LoadSyncTaskRecovering(workspaceID)
		if err != nil {
			return err
		}
		if current == nil {
			return fmt.Errorf("sync task does not exist")
		}
		merged, err := mergeV2SyncTriggers(current.V2Triggers, []V2SyncTrigger{trigger})
		if err != nil {
			current.LastError = err.Error()
			_ = SaveSyncTask(*current)
			return err
		}
		current.V2Triggers = merged
		current.RequestGeneration++
		if trigger.CapturedAt.After(current.LastRequestedAt) {
			current.LastRequestedAt = trigger.CapturedAt.UTC()
		}
		return SaveSyncTask(*current)
	})
}

func mergeV2SyncTriggers(existing, incoming []V2SyncTrigger) ([]V2SyncTrigger, error) {
	result := append([]V2SyncTrigger(nil), existing...)
	byID := make(map[string]V2SyncTrigger, len(result))
	for _, trigger := range result {
		byID[strings.TrimSpace(trigger.EventID)] = trigger
	}
	for _, trigger := range incoming {
		trigger.EventID = strings.TrimSpace(trigger.EventID)
		if trigger.EventID == "" {
			continue
		}
		if previous, ok := byID[trigger.EventID]; ok {
			if !sameV2SyncTrigger(previous, trigger) {
				return result, fmt.Errorf("v2 trigger %s has conflicting canonical payload", trigger.EventID)
			}
			continue
		}
		result = append(result, trigger)
		byID[trigger.EventID] = trigger
	}
	return result, nil
}

func sameV2SyncTrigger(left, right V2SyncTrigger) bool {
	return strings.TrimSpace(left.Kind) == strings.TrimSpace(right.Kind) &&
		strings.TrimSpace(left.EventID) == strings.TrimSpace(right.EventID) &&
		normalizeHookServerURL(left.ServerURL) == normalizeHookServerURL(right.ServerURL) &&
		strings.TrimSpace(left.AuthSubject) == strings.TrimSpace(right.AuthSubject) &&
		left.RepoConfigID == right.RepoConfigID &&
		strings.TrimSpace(left.RepoKey) == strings.TrimSpace(right.RepoKey) &&
		strings.TrimSpace(left.WorkspaceID) == strings.TrimSpace(right.WorkspaceID) &&
		strings.TrimSpace(left.CommitSHA) == strings.TrimSpace(right.CommitSHA) &&
		strings.TrimSpace(left.Branch) == strings.TrimSpace(right.Branch) &&
		strings.TrimSpace(left.RewriteType) == strings.TrimSpace(right.RewriteType) &&
		strings.TrimSpace(left.OldCommitSHA) == strings.TrimSpace(right.OldCommitSHA) &&
		strings.TrimSpace(left.NewCommitSHA) == strings.TrimSpace(right.NewCommitSHA) &&
		left.RelayProviderID == right.RelayProviderID
}

func TryClaimSyncTaskSpawn(workspaceID string, now time.Time, cooldown time.Duration) (bool, *SyncTask, error) {
	var out *SyncTask
	claimed := false
	err := withSyncTaskLock(workspaceID, now, func() error {
		current, _, err := LoadSyncTaskRecovering(workspaceID)
		if err != nil {
			return err
		}
		if current == nil {
			return nil
		}
		out = current
		if current.HasActiveLease(now) {
			return nil
		}
		if current.LastSpawnAttemptAt != nil && cooldown > 0 && now.Sub(current.LastSpawnAttemptAt.UTC()) < cooldown {
			return nil
		}
		spawnedAt := now.UTC()
		current.LastSpawnAttemptAt = &spawnedAt
		if err := SaveSyncTask(*current); err != nil {
			return err
		}
		out = current
		claimed = true
		return nil
	})
	return claimed, out, err
}

func isCorruptSyncTaskError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "unmarshal json:")
}

func quarantineCorruptSyncTask(workspaceID string, now time.Time) error {
	path, err := SyncTaskPath(workspaceID)
	if err != nil {
		return err
	}
	backup := fmt.Sprintf("%s.corrupt.%d", path, now.UTC().UnixNano())
	if err := os.Rename(path, backup); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("quarantine corrupt sync task: %w", err)
	}
	return nil
}

func TryAcquireSyncTaskLease(task *SyncTask, pid int, now time.Time, ttl time.Duration) (bool, error) {
	if task == nil {
		return false, fmt.Errorf("task is nil")
	}
	acquired := false
	err := withSyncTaskLock(task.WorkspaceID, now, func() error {
		latest := task
		current, err := LoadSyncTask(task.WorkspaceID)
		if err != nil {
			return err
		}
		if current != nil {
			latest = current
		}
		if latest.HasActiveLease(now) {
			*task = *latest
			return nil
		}
		expires := now.Add(ttl).UTC()
		started := now.UTC()
		latest.Status = SyncTaskStatusRunning
		latest.RunnerPID = pid
		latest.LastStartedAt = &started
		latest.LeaseExpiresAt = &expires
		if err := SaveSyncTask(*latest); err != nil {
			return err
		}
		*task = *latest
		acquired = true
		return nil
	})
	if errors.Is(err, ErrSyncTaskAlreadyRunning) {
		return false, nil
	}
	return acquired, err
}

func MarkSyncTaskFailure(task *SyncTask, now time.Time, err error) error {
	if task == nil {
		return fmt.Errorf("task is nil")
	}
	return withSyncTaskLock(task.WorkspaceID, now, func() error {
		latest := task
		current, loadErr := LoadSyncTask(task.WorkspaceID)
		if loadErr != nil {
			return loadErr
		}
		if current != nil {
			latest = current
		}
		if latest.RunnerPID != 0 && task.RunnerPID != 0 && latest.RunnerPID != task.RunnerPID && latest.HasActiveLease(now) {
			return nil
		}
		latest.Status = SyncTaskStatusPending
		latest.AttemptCount++
		latest.LastError = err.Error()
		latest.LastFailureStage = SyncTaskFailureStageSync
		latest.LastFailureReason = "attribution sync failed"
		var stageErr *syncTaskStageError
		if errors.As(err, &stageErr) {
			latest.LastFailureStage = stageErr.stage
			latest.LastFailureReason = stageErr.reason
		}
		if latest.FirstFailureAt == nil {
			failedAt := now.UTC()
			latest.FirstFailureAt = &failedAt
		}
		latest.RemainingTriggerCount = len(latest.V2Triggers)
		latest.RunnerPID = 0
		latest.LeaseExpiresAt = nil
		if saveErr := SaveSyncTask(*latest); saveErr != nil {
			return saveErr
		}
		*task = *latest
		return nil
	})
}

func MarkSyncTaskYielded(task *SyncTask, now time.Time) error {
	if task == nil {
		return fmt.Errorf("task is nil")
	}
	return withSyncTaskLock(task.WorkspaceID, now, func() error {
		latest := task
		current, err := LoadSyncTask(task.WorkspaceID)
		if err != nil {
			return err
		}
		if current != nil {
			latest = current
		}
		if latest.RunnerPID != 0 && task.RunnerPID != 0 && latest.RunnerPID != task.RunnerPID && latest.HasActiveLease(now) {
			return nil
		}
		latest.Status = SyncTaskStatusYielded
		latest.AttemptCount++
		latest.LastError = ""
		latest.LastFailureStage = ""
		latest.LastFailureReason = ""
		latest.FirstFailureAt = nil
		latest.RemainingTriggerCount = len(latest.V2Triggers)
		latest.RunnerPID = 0
		latest.LeaseExpiresAt = nil
		if err := SaveSyncTask(*latest); err != nil {
			return err
		}
		*task = *latest
		return nil
	})
}

func MarkSyncTaskSuccess(task *SyncTask, now time.Time) error {
	if task == nil {
		return fmt.Errorf("task is nil")
	}
	return withSyncTaskLock(task.WorkspaceID, now, func() error {
		latest := task
		current, err := LoadSyncTask(task.WorkspaceID)
		if err != nil {
			return err
		}
		if current != nil {
			latest = current
		}
		if latest.RunnerPID != 0 && task.RunnerPID != 0 && latest.RunnerPID != task.RunnerPID && latest.HasActiveLease(now) {
			return nil
		}
		completed := now.UTC()
		latest.Status = SyncTaskStatusPending
		latest.LastCompletedAt = &completed
		latest.LastError = ""
		latest.LastFailureStage = ""
		latest.LastFailureReason = ""
		latest.FirstFailureAt = nil
		latest.RemainingTriggerCount = 0
		latest.RunnerPID = 0
		latest.LeaseExpiresAt = nil
		latest.LastSpawnAttemptAt = nil
		if err := SaveSyncTask(*latest); err != nil {
			return err
		}
		*task = *latest
		return nil
	})
}

// CompleteSyncTaskPass atomically decides whether work arrived during a pass.
// It keeps the current lease for a successor pass or deletes an idle task.
func CompleteSyncTaskPass(task *SyncTask, passGeneration int, now time.Time) (bool, error) {
	if task == nil {
		return false, fmt.Errorf("task is nil")
	}
	idle := false
	err := withSyncTaskLock(task.WorkspaceID, now, func() error {
		current, err := LoadSyncTask(task.WorkspaceID)
		if err != nil || current == nil {
			return err
		}
		if current.RunnerPID != task.RunnerPID || current.RunnerPID == 0 {
			return ErrSyncTaskAlreadyRunning
		}
		processed := make(map[string]struct{}, len(task.V2Triggers))
		for _, trigger := range task.V2Triggers {
			processed[trigger.EventID] = struct{}{}
		}
		remaining := current.V2Triggers[:0]
		for _, trigger := range current.V2Triggers {
			if _, ok := processed[trigger.EventID]; !ok {
				remaining = append(remaining, trigger)
			}
		}
		if len(remaining) > 0 || current.RequestGeneration > passGeneration {
			current.V2Triggers = remaining
			completed := now.UTC()
			current.LastCompletedAt = &completed
			current.LastError = ""
			current.LastFailureStage = ""
			current.LastFailureReason = ""
			current.FirstFailureAt = nil
			current.RemainingTriggerCount = len(remaining)
			current.Status = SyncTaskStatusPending
			current.RunnerPID = 0
			current.LeaseExpiresAt = nil
			*task = *current
			return SaveSyncTask(*current)
		}
		path, err := SyncTaskPath(task.WorkspaceID)
		if err != nil {
			return err
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		idle = true
		return nil
	})
	return idle, err
}

func withSyncTaskLock(workspaceID string, now time.Time, fn func() error) error {
	lockPath, err := syncTaskLockPath(workspaceID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o700); err != nil {
		return fmt.Errorf("create sync task lock dir: %w", err)
	}
	const attempts = 20
	for attempt := 0; attempt < attempts; attempt++ {
		f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_, _ = fmt.Fprintf(f, "pid=%d\ncreated_at=%s\n", os.Getpid(), now.UTC().Format(time.RFC3339Nano))
			_ = f.Close()
			defer func() { _ = os.Remove(lockPath) }()
			return fn()
		}
		if !os.IsExist(err) {
			return fmt.Errorf("create sync task lock: %w", err)
		}
		if syncTaskLockIsStale(lockPath, now) {
			_ = os.Remove(lockPath)
			continue
		}
		time.Sleep(5 * time.Millisecond)
	}
	return ErrSyncTaskAlreadyRunning
}

func withMachineSyncRunLock(ctx context.Context, execCtx ExecutionContext, fn func() error) error {
	lockPath := filepath.Join(attributionlocal.AttributionRootDir(), "machine-sync.run.lock")
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o700); err != nil {
		return fmt.Errorf("create machine sync lock dir: %w", err)
	}
	waiting := false
	for {
		if err := ctx.Err(); err != nil {
			if waiting {
				_ = releaseMachineSyncWakeRequest(execCtx, os.Getpid())
			}
			return err
		}
		file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_, _ = fmt.Fprintf(file, "%d\n", os.Getpid())
			_ = file.Close()
			_ = os.Remove(filepath.Join(attributionlocal.AttributionRootDir(), "machine-sync.wake"))
			_ = os.Remove(machineSyncWakeRequestPath(execCtx))
			defer func() { _ = os.Remove(lockPath) }()
			return fn()
		}
		if !os.IsExist(err) {
			return fmt.Errorf("create machine sync lock: %w", err)
		}
		if machineSyncRunLockIsStale(lockPath) {
			_ = os.Remove(lockPath)
			continue
		}
		if !waiting {
			claimed, err := claimMachineSyncWakeRequest(execCtx)
			if err != nil {
				return err
			}
			if !claimed {
				return ErrSyncTaskAlreadyRunning
			}
			waiting = true
		}
		select {
		case <-ctx.Done():
			_ = releaseMachineSyncWakeRequest(execCtx, os.Getpid())
			return ctx.Err()
		case <-time.After(machineSyncLockPollInterval):
		}
	}
}

func machineSyncWakeRequestPath(execCtx ExecutionContext) string {
	scope := normalizeHookServerURL(execCtx.ServerURL) + "\x1f" + strings.TrimSpace(execCtx.AuthSubject)
	return filepath.Join(attributionlocal.AttributionRootDir(), "machine-sync-wakes", sha256Hex(scope)+".json")
}

func claimMachineSyncWakeRequest(execCtx ExecutionContext) (bool, error) {
	if normalizeHookServerURL(execCtx.ServerURL) == "" || strings.TrimSpace(execCtx.AuthSubject) == "" || strings.TrimSpace(execCtx.RepoRoot) == "" {
		return false, fmt.Errorf("machine sync wake requires server, owner, and repository")
	}
	path := machineSyncWakeRequestPath(execCtx)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return false, err
	}
	for range 2 {
		file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_, _ = fmt.Fprintf(file, "%d\n", os.Getpid())
			if err := file.Close(); err != nil {
				_ = os.Remove(path)
				return false, err
			}
			return true, nil
		}
		if !os.IsExist(err) {
			return false, err
		}
		payload, readErr := os.ReadFile(path)
		pid, parseErr := strconv.Atoi(strings.TrimSpace(string(payload)))
		if readErr == nil && parseErr == nil && pid > 0 {
			if syncTaskRunnerAlive(pid) {
				return false, nil
			}
			_ = os.Remove(path)
			continue
		}
		info, statErr := os.Stat(path)
		if statErr == nil && time.Since(info.ModTime()) <= machineSyncOwnerTimeout {
			return false, nil
		}
		if statErr != nil && !os.IsNotExist(statErr) {
			return false, statErr
		}
		_ = os.Remove(path)
	}
	return false, nil
}

func releaseMachineSyncWakeRequest(execCtx ExecutionContext, pid int) error {
	path := machineSyncWakeRequestPath(execCtx)
	payload, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	ownerPID, err := strconv.Atoi(strings.TrimSpace(string(payload)))
	if err != nil || ownerPID != pid {
		return nil
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func machineSyncRunLockIsStale(path string) bool {
	payload, err := os.ReadFile(path)
	if err == nil {
		pid, parseErr := strconv.Atoi(strings.TrimSpace(string(payload)))
		if parseErr == nil && pid > 0 {
			return !syncTaskRunnerAlive(pid)
		}
	}
	info, statErr := os.Stat(path)
	return statErr == nil && time.Since(info.ModTime()) > syncTaskLeaseTTL
}

func syncTaskLockIsStale(lockPath string, now time.Time) bool {
	info, err := os.Stat(lockPath)
	if err != nil {
		return false
	}
	return now.Sub(info.ModTime()) > 30*time.Second
}
