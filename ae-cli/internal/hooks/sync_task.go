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
	Queued  int
	Running int
	Yielded int
	Failed  int
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
		case strings.TrimSpace(task.LastError) != "":
			summary.Failed++
		case task.HasActiveLease(now):
			summary.Running++
		case task.Status == SyncTaskStatusYielded:
			summary.Yielded++
		default:
			summary.Queued++
		}
	}
	return summary, nil
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
		task.Version = 1
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
		if current.RequestGeneration > passGeneration {
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
			current.V2Triggers = remaining
			completed := now.UTC()
			current.LastCompletedAt = &completed
			current.LastError = ""
			current.LastFailureStage = ""
			current.LastFailureReason = ""
			current.FirstFailureAt = nil
			current.RemainingTriggerCount = 0
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

func withMachineSyncRunLock(ctx context.Context, fn func() error) error {
	lockPath := filepath.Join(attributionlocal.AttributionRootDir(), "machine-sync.run.lock")
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o700); err != nil {
		return fmt.Errorf("create machine sync lock dir: %w", err)
	}
	for {
		file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_, _ = fmt.Fprintf(file, "%d\n", os.Getpid())
			_ = file.Close()
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
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(machineSyncLockPollInterval):
		}
	}
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
