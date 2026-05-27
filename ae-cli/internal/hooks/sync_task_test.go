package hooks

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ai-efficiency/ae-cli/internal/attributionlocal"
)

func ptrTime(t time.Time) *time.Time {
	return &t
}

func TestUpsertPendingSyncTaskRecoversCorruptTaskFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	path, err := SyncTaskPath("ws-corrupt")
	if err != nil {
		t.Fatalf("SyncTaskPath: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte("{not-json"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	next := SyncTask{
		WorkspaceID:     "ws-corrupt",
		RepoRoot:        "/tmp/repo",
		ServerURL:       "https://ae.example.com",
		AuthSubject:     "user:1",
		RepoConfigID:    2,
		RepoKey:         "github.com/acme/repo",
		Status:          SyncTaskStatusPending,
		LastRequestedAt: time.Now().UTC(),
	}
	if err := UpsertPendingSyncTask(next); err != nil {
		t.Fatalf("UpsertPendingSyncTask: %v", err)
	}
	got, err := LoadSyncTask("ws-corrupt")
	if err != nil {
		t.Fatalf("LoadSyncTask: %v", err)
	}
	if got == nil || got.RepoConfigID != 2 {
		t.Fatalf("task = %+v, want recreated pending task", got)
	}

	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	var foundBackup bool
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "sync-task.json.corrupt.") {
			foundBackup = true
			break
		}
	}
	if !foundBackup {
		t.Fatalf("corrupt sync task backup not found in %s", filepath.Dir(path))
	}
}

func TestAcquireSyncTaskLeaseAllowsOnlyOneConcurrentRunner(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	now := time.Now().UTC()
	task := SyncTask{
		WorkspaceID:     "ws-concurrent",
		RepoRoot:        "/tmp/repo",
		Status:          SyncTaskStatusPending,
		LastRequestedAt: now,
	}
	if err := SaveSyncTask(task); err != nil {
		t.Fatalf("SaveSyncTask: %v", err)
	}

	const runners = 8
	loaded := make([]*SyncTask, runners)
	for i := range loaded {
		task, err := LoadSyncTask("ws-concurrent")
		if err != nil {
			t.Fatalf("LoadSyncTask(%d): %v", i, err)
		}
		loaded[i] = task
	}

	var acquired int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range loaded {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			ok, err := TryAcquireSyncTaskLease(loaded[i], 1000+i, now, time.Minute)
			if err != nil {
				t.Errorf("TryAcquireSyncTaskLease(%d): %v", i, err)
				return
			}
			if ok {
				atomic.AddInt32(&acquired, 1)
			}
		}(i)
	}
	close(start)
	wg.Wait()

	if acquired != 1 {
		t.Fatalf("acquired leases = %d, want 1", acquired)
	}
}

func TestMarkSyncTaskFailurePreservesNewerRequestForSameRunner(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	started := time.Now().UTC()
	stale := SyncTask{
		WorkspaceID:     "ws-failure",
		Status:          SyncTaskStatusRunning,
		LastRequestedAt: started,
		RunnerPID:       1111,
		LeaseExpiresAt:  ptrTime(started.Add(time.Minute)),
	}
	current := stale
	current.LastRequestedAt = started.Add(10 * time.Second)
	if err := SaveSyncTask(current); err != nil {
		t.Fatalf("SaveSyncTask: %v", err)
	}

	if err := MarkSyncTaskFailure(&stale, started.Add(20*time.Second), errors.New("upload failed")); err != nil {
		t.Fatalf("MarkSyncTaskFailure: %v", err)
	}
	got, err := LoadSyncTask("ws-failure")
	if err != nil {
		t.Fatalf("LoadSyncTask: %v", err)
	}
	if got == nil || !got.LastRequestedAt.Equal(current.LastRequestedAt) {
		t.Fatalf("LastRequestedAt = %v, want newer request %v", got, current.LastRequestedAt)
	}
	if got.LastError != "upload failed" || got.RunnerPID != 0 || got.LeaseExpiresAt != nil {
		t.Fatalf("task after failure = %+v, want pending failure without lease", got)
	}
}

func TestMarkSyncTaskFailureDoesNotClearDifferentActiveRunner(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	now := time.Now().UTC()
	stale := SyncTask{
		WorkspaceID:     "ws-different-runner",
		Status:          SyncTaskStatusRunning,
		LastRequestedAt: now,
		RunnerPID:       1111,
		LeaseExpiresAt:  ptrTime(now.Add(time.Minute)),
	}
	current := stale
	current.RunnerPID = 2222
	if err := SaveSyncTask(current); err != nil {
		t.Fatalf("SaveSyncTask: %v", err)
	}

	if err := MarkSyncTaskFailure(&stale, now.Add(10*time.Second), errors.New("old runner failed")); err != nil {
		t.Fatalf("MarkSyncTaskFailure: %v", err)
	}
	got, err := LoadSyncTask("ws-different-runner")
	if err != nil {
		t.Fatalf("LoadSyncTask: %v", err)
	}
	if got == nil || got.RunnerPID != 2222 || got.Status != SyncTaskStatusRunning || got.LastError != "" {
		t.Fatalf("task after stale failure = %+v, want active runner preserved", got)
	}
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
