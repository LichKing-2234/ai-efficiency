package hooks

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ai-efficiency/ae-cli/internal/attributionlocal"
)

type SyncTaskStatus string

const (
	SyncTaskStatusPending SyncTaskStatus = "pending"
	SyncTaskStatusRunning SyncTaskStatus = "running"
)

var ErrSyncTaskAlreadyRunning = errors.New("sync task already running")

type SyncTask struct {
	Version         int            `json:"version"`
	WorkspaceID     string         `json:"workspace_id"`
	RepoRoot        string         `json:"repo_root"`
	ServerURL       string         `json:"server_url"`
	AuthSubject     string         `json:"auth_subject"`
	RepoConfigID    int            `json:"repo_config_id"`
	RepoKey         string         `json:"repo_key"`
	Status          SyncTaskStatus `json:"status"`
	LastRequestedAt time.Time      `json:"last_requested_at"`
	LastStartedAt   *time.Time     `json:"last_started_at,omitempty"`
	LastCompletedAt *time.Time     `json:"last_completed_at,omitempty"`
	LastError       string         `json:"last_error,omitempty"`
	AttemptCount    int            `json:"attempt_count"`
	RunnerPID       int            `json:"runner_pid,omitempty"`
	LeaseExpiresAt  *time.Time     `json:"lease_expires_at,omitempty"`
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
		t.LeaseExpiresAt.After(now)
}

func UpsertPendingSyncTask(next SyncTask) error {
	now := time.Now().UTC()
	return withSyncTaskLock(next.WorkspaceID, now, func() error {
		current, _, err := LoadSyncTaskRecovering(next.WorkspaceID)
		if err != nil {
			return err
		}
		if current != nil {
			next.Version = current.Version
			next.AttemptCount = current.AttemptCount
			next.LastStartedAt = current.LastStartedAt
			next.LastCompletedAt = current.LastCompletedAt
			next.LastError = current.LastError
			if current.HasActiveLease(now) {
				next.RunnerPID = current.RunnerPID
				next.LeaseExpiresAt = current.LeaseExpiresAt
				next.Status = current.Status
			}
		}
		if next.Status == "" {
			next.Status = SyncTaskStatusPending
		}
		return SaveSyncTask(next)
	})
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
		latest.RunnerPID = 0
		latest.LeaseExpiresAt = nil
		if saveErr := SaveSyncTask(*latest); saveErr != nil {
			return saveErr
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
		latest.RunnerPID = 0
		latest.LeaseExpiresAt = nil
		if err := SaveSyncTask(*latest); err != nil {
			return err
		}
		*task = *latest
		return nil
	})
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

func syncTaskLockIsStale(lockPath string, now time.Time) bool {
	info, err := os.Stat(lockPath)
	if err != nil {
		return false
	}
	return now.Sub(info.ModTime()) > 30*time.Second
}
