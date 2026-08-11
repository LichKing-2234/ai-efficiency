package hooks

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ai-efficiency/ae-cli/internal/attributionlocal"
	"github.com/ai-efficiency/ae-cli/internal/client"
)

func ptrTime(t time.Time) *time.Time {
	return &t
}

func stubSyncTaskRunnerAlive(t *testing.T, alive func(int) bool) {
	t.Helper()
	orig := syncTaskRunnerAlive
	syncTaskRunnerAlive = alive
	t.Cleanup(func() { syncTaskRunnerAlive = orig })
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
	stubSyncTaskRunnerAlive(t, func(int) bool { return true })

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
	stubSyncTaskRunnerAlive(t, func(pid int) bool { return pid == 2222 })

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

func TestUpsertPendingSyncTaskRejectsSameEventIDDifferentPayload(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	now := time.Now().UTC()
	first := SyncTask{WorkspaceID: "ws-conflict", Status: SyncTaskStatusPending, LastRequestedAt: now, V2Triggers: []V2SyncTrigger{{Kind: "post-commit", EventID: "event-1", CommitSHA: "aaa", CapturedAt: now}}}
	if err := UpsertPendingSyncTask(first); err != nil {
		t.Fatal(err)
	}
	second := first
	second.V2Triggers = []V2SyncTrigger{{Kind: "post-commit", EventID: "event-1", CommitSHA: "bbb", CapturedAt: now.Add(time.Second)}}
	if err := UpsertPendingSyncTask(second); err == nil || !strings.Contains(err.Error(), "conflicting canonical payload") {
		t.Fatalf("conflicting trigger error = %v", err)
	}
	got, _ := LoadSyncTask(first.WorkspaceID)
	if got == nil || got.V2Triggers[0].CommitSHA != "aaa" || !strings.Contains(got.LastError, "conflicting canonical payload") {
		t.Fatalf("task after conflict = %+v", got)
	}
}

func TestRunPendingSyncTaskDrainsWorkArrivingDuringRun(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	stubSyncTaskRunnerAlive(t, func(pid int) bool { return pid == os.Getpid() })
	now := time.Now().UTC()
	task := SyncTask{WorkspaceID: "ws-successor", RepoRoot: t.TempDir(), ServerURL: "https://ae.example.com", AuthSubject: "user:1", RepoConfigID: 2, RepoKey: "github.com/acme/repo", Status: SyncTaskStatusPending, LastRequestedAt: now}
	if err := UpsertPendingSyncTask(task); err != nil {
		t.Fatal(err)
	}
	original := runAttributionSync
	var calls int
	runAttributionSync = func(context.Context, attributionlocal.RunOptions, attributionlocal.BackendClient) error {
		calls++
		if calls == 1 {
			next := task
			next.LastRequestedAt = now.Add(time.Second)
			return UpsertPendingSyncTask(next)
		}
		return nil
	}
	t.Cleanup(func() { runAttributionSync = original })
	execCtx := ExecutionContext{ServerURL: task.ServerURL, AuthSubject: task.AuthSubject, RepoConfigID: task.RepoConfigID, RepoKey: task.RepoKey, WorkspaceID: task.WorkspaceID, RepoRoot: task.RepoRoot, DurableReplay: true}
	if err := RunPendingSyncTask(context.Background(), execCtx, syncCapableFakeUploader{fakeUploader: &fakeUploader{}}); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("sync passes = %d, want successor pass", calls)
	}
	if got, err := LoadSyncTask(task.WorkspaceID); err != nil || got != nil {
		t.Fatalf("completed task = %+v, %v, want deleted", got, err)
	}
}

type failingV2ClaimClient struct{ err error }

func (f failingV2ClaimClient) SendAttributionV2Claims(context.Context, []client.AttributionV2ClaimGroup) (*client.AttributionV2ClaimBatchResult, error) {
	return nil, f.err
}

type failingV2Uploader struct {
	*fakeUploader
	client failingV2ClaimClient
}

func (f failingV2Uploader) V2ClaimClient() attributionlocal.V2ClaimBackendClient { return f.client }
func (f failingV2Uploader) RelayProviderID() int                                 { return 7 }

func TestRunV2ClaimSyncPreservesLocalStateOnResponseLoss(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	now := time.Now().UTC()
	group := client.AttributionV2ClaimGroup{GroupID: "group-1", RelayProviderID: 7, RequestIDs: []string{"req-1"}}
	if err := attributionlocal.SaveV2ClaimState(&attributionlocal.V2ClaimState{
		Version: 1, Claims: []attributionlocal.V2ClaimCandidate{{LocalKey: "local-1", Group: group, FirstSeenAt: now}},
	}); err != nil {
		t.Fatal(err)
	}
	uploader := failingV2Uploader{fakeUploader: &fakeUploader{}, client: failingV2ClaimClient{err: errors.New("response lost")}}
	err := runV2ClaimSync(context.Background(), uploader, ExecutionContext{WorkspaceID: "ws-1"}, &SyncTask{})
	if err == nil || !strings.Contains(err.Error(), "response lost") {
		t.Fatalf("runV2ClaimSync error = %v", err)
	}
	state, err := attributionlocal.LoadV2ClaimState()
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Claims) != 1 || len(state.Claims[0].Group.RequestIDs) != 1 || state.Claims[0].Group.RequestIDs[0] != "req-1" {
		t.Fatalf("response loss mutated local state: %+v", state.Claims)
	}
}

func TestAcquireSyncTaskLeaseRejectsActiveRunnerAndAllowsExpiredLease(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	stubSyncTaskRunnerAlive(t, func(pid int) bool { return pid == 1234 })

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

func TestRecoverInactiveSyncTaskRunnerClearsDeadLease(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	stubSyncTaskRunnerAlive(t, func(int) bool { return false })

	now := time.Now().UTC()
	task := SyncTask{
		WorkspaceID:     "ws-dead-runner",
		Status:          SyncTaskStatusRunning,
		RunnerPID:       1234,
		LeaseExpiresAt:  ptrTime(now.Add(5 * time.Minute)),
		LastRequestedAt: now,
	}
	if err := SaveSyncTask(task); err != nil {
		t.Fatalf("SaveSyncTask: %v", err)
	}

	got, recovered, err := RecoverInactiveSyncTaskRunner(task.WorkspaceID, now)
	if err != nil {
		t.Fatalf("RecoverInactiveSyncTaskRunner: %v", err)
	}
	if !recovered {
		t.Fatal("recovered = false, want true")
	}
	if got == nil || got.Status != SyncTaskStatusPending || got.RunnerPID != 0 || got.LeaseExpiresAt != nil {
		t.Fatalf("task = %+v, want pending without lease", got)
	}
	if got.LastError != "runner exited before updating sync task" {
		t.Fatalf("LastError = %q, want runner recovery error", got.LastError)
	}
}
