package hooks

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/ai-efficiency/ae-cli/internal/attributionlocal"
)

func ptrTime(t time.Time) *time.Time {
	return &t
}

func TestUpsertPendingSyncTaskCoalescesRequests(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	workspaceID := "ws-1"
	first := SyncTask{
		WorkspaceID:     workspaceID,
		RepoRoot:        "/tmp/repo",
		ServerURL:       "https://ae.example.com",
		AuthSubject:     "user:1",
		RepoConfigID:    2,
		RepoKey:         "github.com/acme/repo",
		Status:          SyncTaskStatusPending,
		LastRequestedAt: time.Date(2026, 5, 26, 8, 0, 0, 0, time.UTC),
	}
	second := first
	second.LastRequestedAt = first.LastRequestedAt.Add(2 * time.Minute)

	if err := SaveSyncTask(first); err != nil {
		t.Fatalf("SaveSyncTask(first): %v", err)
	}
	if err := UpsertPendingSyncTask(second); err != nil {
		t.Fatalf("UpsertPendingSyncTask(second): %v", err)
	}

	got, err := LoadSyncTask(workspaceID)
	if err != nil {
		t.Fatalf("LoadSyncTask: %v", err)
	}
	if got == nil || got.LastRequestedAt != second.LastRequestedAt {
		t.Fatalf("task=%+v, want last_requested_at=%s", got, second.LastRequestedAt)
	}

	if gotPath, wantPath := filepath.Join(attributionlocal.AttributionRootDir(), "workspaces", workspaceID, "sync-task.json"), filepath.Join(home, ".ae-cli", "state", "attribution", "workspaces", workspaceID, "sync-task.json"); gotPath != wantPath {
		t.Fatalf("sync task path = %q, want %q", gotPath, wantPath)
	}
}

func TestAcquireSyncTaskLeaseRejectsActiveRunnerAndAllowsExpiredLease(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	now := time.Now().UTC()
	task := &SyncTask{
		WorkspaceID:     "ws-1",
		Status:          SyncTaskStatusRunning,
		RunnerPID:       1234,
		LeaseExpiresAt:  ptrTime(now.Add(5 * time.Minute)),
		LastRequestedAt: now,
	}
	if ok, err := TryAcquireSyncTaskLease(task, 9999, now, 30*time.Second); err != nil || ok {
		t.Fatalf("TryAcquireSyncTaskLease(active) = %t, %v, want false, nil", ok, err)
	}

	task.LeaseExpiresAt = ptrTime(now.Add(-1 * time.Minute))
	ok, err := TryAcquireSyncTaskLease(task, 9999, now, 30*time.Second)
	if err != nil || !ok {
		t.Fatalf("TryAcquireSyncTaskLease(expired) = %t, %v, want true, nil", ok, err)
	}
	if task.RunnerPID != 9999 {
		t.Fatalf("RunnerPID = %d, want 9999", task.RunnerPID)
	}
	if task.Status != SyncTaskStatusRunning {
		t.Fatalf("Status = %q, want %q", task.Status, SyncTaskStatusRunning)
	}
	if task.LeaseExpiresAt == nil || !task.LeaseExpiresAt.After(now) {
		t.Fatalf("LeaseExpiresAt = %v, want future time", task.LeaseExpiresAt)
	}
}
