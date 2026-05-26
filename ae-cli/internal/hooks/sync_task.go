package hooks

import (
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

func UpsertPendingSyncTask(next SyncTask) error {
	current, err := LoadSyncTask(next.WorkspaceID)
	if err != nil {
		return err
	}
	if current != nil {
		next.Version = current.Version
		next.AttemptCount = current.AttemptCount
		next.LastStartedAt = current.LastStartedAt
		next.LastCompletedAt = current.LastCompletedAt
		next.LastError = current.LastError
		next.RunnerPID = current.RunnerPID
		next.LeaseExpiresAt = current.LeaseExpiresAt
	}
	next.Status = SyncTaskStatusPending
	return SaveSyncTask(next)
}

func TryAcquireSyncTaskLease(task *SyncTask, pid int, now time.Time, ttl time.Duration) (bool, error) {
	if task == nil {
		return false, fmt.Errorf("task is nil")
	}
	if task.LeaseExpiresAt != nil && task.LeaseExpiresAt.After(now) && task.Status == SyncTaskStatusRunning {
		return false, nil
	}
	expires := now.Add(ttl).UTC()
	started := now.UTC()
	task.Status = SyncTaskStatusRunning
	task.RunnerPID = pid
	task.LastStartedAt = &started
	task.LeaseExpiresAt = &expires
	return true, SaveSyncTask(*task)
}

func MarkSyncTaskFailure(task *SyncTask, now time.Time, err error) error {
	if task == nil {
		return fmt.Errorf("task is nil")
	}
	task.Status = SyncTaskStatusPending
	task.AttemptCount++
	task.LastError = err.Error()
	task.RunnerPID = 0
	task.LeaseExpiresAt = nil
	return SaveSyncTask(*task)
}

func MarkSyncTaskSuccess(task *SyncTask, now time.Time) error {
	if task == nil {
		return fmt.Errorf("task is nil")
	}
	completed := now.UTC()
	task.Status = SyncTaskStatusPending
	task.LastCompletedAt = &completed
	task.LastError = ""
	task.RunnerPID = 0
	task.LeaseExpiresAt = nil
	return SaveSyncTask(*task)
}
