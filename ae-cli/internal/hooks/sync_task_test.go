package hooks

import (
	"context"
	"errors"
	"fmt"
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

func TestMigrateMachineSyncBacklogUpgradesLegacyTasksIdempotently(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	now := time.Date(2026, 8, 17, 8, 0, 0, 0, time.UTC)
	task := SyncTask{
		Version: 1, WorkspaceID: "workspace-legacy", RepoRoot: "/gone",
		ServerURL: "https://ae.example.com/", AuthSubject: "user:1",
		RepoConfigID: 23, RepoKey: "github.com/acme/repo",
		TriggerKind: "post-commit", TriggerEventID: "event-legacy",
		TriggerCommitSHA: strings.Repeat("a", 40), LastRequestedAt: now.Add(-time.Hour),
		LastError: "legacy raw error",
	}
	if err := attributionlocal.SaveJSON(mustSyncTaskPath(t, task.WorkspaceID), task); err != nil {
		t.Fatal(err)
	}
	if err := SaveV2ClaimScanProgress(&V2ClaimScanProgress{
		Version: 2, WorkspaceID: task.WorkspaceID, ContextID: "legacy-context",
		SourceKeys: []string{"source-a"}, CompletedUnits: []string{"unit-a"},
		StartedAt: now.Add(-time.Hour), Complete: true,
	}); err != nil {
		t.Fatal(err)
	}
	binding := SyncTaskMigrationBinding{ServerURL: "https://ae.example.com", AuthSubject: "user:1", RelayProviderID: 17}
	first, err := MigrateMachineSyncBacklog(binding, now)
	if err != nil {
		t.Fatal(err)
	}
	if first.Scanned != 1 || first.Migrated != 1 || first.Deferred != 0 {
		t.Fatalf("first migration = %+v", first)
	}
	got, err := LoadSyncTask(task.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != syncTaskVersion || got.RequestGeneration != 1 || got.Status != SyncTaskStatusPending || len(got.V2Triggers) != 1 || got.V2Triggers[0].RelayProviderID != 17 {
		t.Fatalf("migrated task = %+v", got)
	}
	if got.LastFailureStage != SyncTaskFailureStageLocalState || got.LastFailureReason != "legacy attribution state requires recovery" || got.FirstFailureAt == nil || got.RemainingTriggerCount != 1 {
		t.Fatalf("migrated diagnostics = %+v", got)
	}
	progress, err := LoadV2ClaimScanProgress(task.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if progress.Version != v2ClaimScanProgressVersion || len(progress.SourceKeys) != 0 || len(progress.CompletedUnits) != 0 || progress.Complete {
		t.Fatalf("rebuilt progress = %+v", progress)
	}
	second, err := MigrateMachineSyncBacklog(binding, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if second.Scanned != 1 || second.Migrated != 0 || second.Deferred != 0 {
		t.Fatalf("second migration = %+v", second)
	}
}

func TestMigrateMachineSyncBacklogPreservesFailureStagesAndDistinctIdentities(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	now := time.Date(2026, 8, 17, 8, 0, 0, 0, time.UTC)
	for index, stage := range []SyncTaskFailureStage{
		SyncTaskFailureStageSourceScan, SyncTaskFailureStageLocalState, SyncTaskFailureStageAcknowledgement,
	} {
		workspaceID := fmt.Sprintf("workspace-stage-%d", index)
		task := SyncTask{
			Version: 1, WorkspaceID: workspaceID, ServerURL: "https://ae.example.com", AuthSubject: "user:1",
			RepoConfigID: 23, RepoKey: "github.com/acme/repo", Status: SyncTaskStatusPending, LastRequestedAt: now,
			LastError: "legacy failure", LastFailureStage: stage, LastFailureReason: "existing safe reason",
			V2Triggers: []V2SyncTrigger{
				{Kind: "post-commit", EventID: "event-a", CommitSHA: strings.Repeat("a", 40), CapturedAt: now},
				{Kind: "post-commit", EventID: "event-a", CommitSHA: strings.Repeat("a", 40), CapturedAt: now},
				{Kind: "post-commit", EventID: "event-b", CommitSHA: strings.Repeat("b", 40), CapturedAt: now, RelayProviderID: 29},
			},
		}
		if err := attributionlocal.SaveJSON(mustSyncTaskPath(t, workspaceID), task); err != nil {
			t.Fatal(err)
		}
		if index == 0 {
			if err := SaveV2ClaimScanProgress(&V2ClaimScanProgress{
				Version: v2ClaimScanProgressVersion, WorkspaceID: workspaceID, SourceKeys: []string{"source-b", "source-a", "source-a"},
				CompletedUnits: []string{"unit-b", "unit-a", "unit-a"}, SourceTurnKeys: map[string][]string{"source-a": {"turn-b", "turn-a", "turn-a"}},
				StartedAt: now.Add(-time.Hour),
			}); err != nil {
				t.Fatal(err)
			}
		}
	}

	summary, err := MigrateMachineSyncBacklog(SyncTaskMigrationBinding{ServerURL: "https://ae.example.com", AuthSubject: "user:1", RelayProviderID: 17}, now)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Scanned != 3 || summary.Migrated != 3 || summary.Deferred != 0 {
		t.Fatalf("migration summary = %+v", summary)
	}
	for index, stage := range []SyncTaskFailureStage{
		SyncTaskFailureStageSourceScan, SyncTaskFailureStageLocalState, SyncTaskFailureStageAcknowledgement,
	} {
		task, err := LoadSyncTask(fmt.Sprintf("workspace-stage-%d", index))
		if err != nil {
			t.Fatal(err)
		}
		if task.LastFailureStage != stage || task.LastFailureReason != "existing safe reason" {
			t.Fatalf("failure diagnostics changed: %+v", task)
		}
		if len(task.V2Triggers) != 2 || task.V2Triggers[0].EventID != "event-a" || task.V2Triggers[0].RelayProviderID != 17 || task.V2Triggers[1].EventID != "event-b" || task.V2Triggers[1].RelayProviderID != 29 {
			t.Fatalf("migrated identities = %+v", task.V2Triggers)
		}
	}
	progress, err := LoadV2ClaimScanProgress("workspace-stage-0")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(progress.SourceKeys, ",") != "source-a,source-b" || strings.Join(progress.CompletedUnits, ",") != "unit-a,unit-b" || strings.Join(progress.SourceTurnKeys["source-a"], ",") != "turn-a,turn-b" {
		t.Fatalf("deduplicated progress = %+v", progress)
	}
}

func TestMigrateMachineSyncBacklogLeavesOtherOwnersAndFutureVersionsUnchanged(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	now := time.Now().UTC()
	tasks := []SyncTask{
		{Version: 1, WorkspaceID: "other-owner", ServerURL: "https://ae.example.com", AuthSubject: "user:2", RepoConfigID: 1, RepoKey: "github.com/acme/repo", LastRequestedAt: now, V2Triggers: []V2SyncTrigger{{Kind: "post-commit", EventID: "event-other", CommitSHA: strings.Repeat("a", 40), CapturedAt: now}}},
		{Version: syncTaskVersion + 1, WorkspaceID: "future", ServerURL: "https://ae.example.com", AuthSubject: "user:1", RepoConfigID: 1, RepoKey: "github.com/acme/repo", LastRequestedAt: now, V2Triggers: []V2SyncTrigger{{Kind: "post-commit", EventID: "event-future", CommitSHA: strings.Repeat("b", 40), CapturedAt: now}}},
	}
	for _, task := range tasks {
		if err := attributionlocal.SaveJSON(mustSyncTaskPath(t, task.WorkspaceID), task); err != nil {
			t.Fatal(err)
		}
	}
	summary, err := MigrateMachineSyncBacklog(SyncTaskMigrationBinding{ServerURL: "https://ae.example.com", AuthSubject: "user:1", RelayProviderID: 17}, now)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Scanned != 1 || summary.Migrated != 0 {
		t.Fatalf("migration summary = %+v", summary)
	}
	for _, task := range tasks {
		got, err := LoadSyncTask(task.WorkspaceID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Version != task.Version || got.V2Triggers[0].RelayProviderID != 0 {
			t.Fatalf("task changed = %+v", got)
		}
	}
}

func TestMigrateMachineSyncBacklogDefersActiveRunner(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	now := time.Now().UTC()
	stubSyncTaskRunnerAlive(t, func(pid int) bool { return pid == 42 })
	task := SyncTask{
		Version: 1, WorkspaceID: "active-runner", ServerURL: "https://ae.example.com", AuthSubject: "user:1",
		RepoConfigID: 1, RepoKey: "github.com/acme/repo", Status: SyncTaskStatusRunning, LastRequestedAt: now,
		RunnerPID: 42, LeaseExpiresAt: ptrTime(now.Add(time.Minute)), LastError: "legacy failure",
		V2Triggers: []V2SyncTrigger{{Kind: "post-commit", EventID: "event-active", CommitSHA: strings.Repeat("a", 40), CapturedAt: now}},
	}
	if err := attributionlocal.SaveJSON(mustSyncTaskPath(t, task.WorkspaceID), task); err != nil {
		t.Fatal(err)
	}
	summary, err := MigrateMachineSyncBacklog(SyncTaskMigrationBinding{ServerURL: task.ServerURL, AuthSubject: task.AuthSubject, RelayProviderID: 17}, now)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Scanned != 1 || summary.Migrated != 0 || summary.Deferred != 1 {
		t.Fatalf("migration summary = %+v", summary)
	}
	got, err := LoadSyncTask(task.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != 1 || got.V2Triggers[0].RelayProviderID != 0 {
		t.Fatalf("active task changed = %+v", got)
	}
	machine, err := SummarizeMachineSyncTasks(now)
	if err != nil {
		t.Fatal(err)
	}
	if machine.Running != 1 || machine.Recoverable != 0 {
		t.Fatalf("machine summary = %+v", machine)
	}
}

func TestMigrateMachineSyncBacklogContinuesAcrossLargeBacklogFailures(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	now := time.Date(2026, 8, 17, 8, 0, 0, 0, time.UTC)
	const taskCount = 300
	for index := 0; index < taskCount; index++ {
		workspaceID := fmt.Sprintf("workspace-%03d", index)
		task := SyncTask{
			Version: 1, WorkspaceID: workspaceID, RepoRoot: filepath.Join(t.TempDir(), "deleted"),
			ServerURL: "https://ae.example.com", AuthSubject: "user:1", RepoConfigID: 23, RepoKey: "github.com/acme/repo",
			Status: SyncTaskStatusPending, LastRequestedAt: now.Add(-time.Duration(index) * time.Minute),
			V2Triggers: []V2SyncTrigger{{Kind: "post-commit", EventID: "event-" + workspaceID, CommitSHA: strings.Repeat("a", 40), CapturedAt: now.Add(-time.Duration(index) * time.Minute)}},
		}
		if err := attributionlocal.SaveJSON(mustSyncTaskPath(t, workspaceID), task); err != nil {
			t.Fatal(err)
		}
		if err := SaveV2ClaimScanProgress(&V2ClaimScanProgress{
			Version: 2, WorkspaceID: workspaceID, SourceKeys: []string{"source", "source"},
			CompletedUnits: []string{"unit", "unit"}, StartedAt: now.Add(-time.Hour),
		}); err != nil {
			t.Fatal(err)
		}
	}
	lockedWorkspace := "workspace-150"
	lockPath, err := syncTaskLockPath(lockedWorkspace)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lockPath, []byte("busy"), 0o600); err != nil {
		t.Fatal(err)
	}
	conflictingWorkspace := "workspace-200"
	conflicting, err := LoadSyncTask(conflictingWorkspace)
	if err != nil {
		t.Fatal(err)
	}
	conflicting.V2Triggers = append(conflicting.V2Triggers, V2SyncTrigger{
		Kind: "post-commit", EventID: conflicting.V2Triggers[0].EventID,
		CommitSHA: strings.Repeat("b", 40), CapturedAt: now,
	})
	if err := attributionlocal.SaveJSON(mustSyncTaskPath(t, conflictingWorkspace), conflicting); err != nil {
		t.Fatal(err)
	}

	binding := SyncTaskMigrationBinding{ServerURL: "https://ae.example.com", AuthSubject: "user:1", RelayProviderID: 17}
	first, err := MigrateMachineSyncBacklog(binding, now)
	if err != nil {
		t.Fatal(err)
	}
	if first.Scanned != taskCount || first.Migrated != taskCount-2 || first.Deferred != 2 {
		t.Fatalf("first migration = %+v", first)
	}
	if err := os.Remove(lockPath); err != nil {
		t.Fatal(err)
	}
	second, err := MigrateMachineSyncBacklog(binding, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if second.Scanned != taskCount || second.Migrated != 1 || second.Deferred != 1 {
		t.Fatalf("second migration = %+v", second)
	}
	conflicting, err = LoadSyncTask(conflictingWorkspace)
	if err != nil {
		t.Fatal(err)
	}
	if conflicting.Version != 1 || len(conflicting.V2Triggers) != 2 || conflicting.LastFailureStage != SyncTaskFailureStageLocalState || conflicting.LastFailureReason != "legacy trigger migration requires recovery" {
		t.Fatalf("conflicting task diagnostics = %+v", conflicting)
	}
	for index := 0; index < taskCount; index++ {
		workspaceID := fmt.Sprintf("workspace-%03d", index)
		if workspaceID == conflictingWorkspace {
			continue
		}
		task, err := LoadSyncTask(workspaceID)
		if err != nil {
			t.Fatal(err)
		}
		progress, err := LoadV2ClaimScanProgress(workspaceID)
		if err != nil {
			t.Fatal(err)
		}
		if task.Version != syncTaskVersion || task.V2Triggers[0].RelayProviderID != 17 || progress.Version != v2ClaimScanProgressVersion || len(progress.CompletedUnits) != 0 {
			t.Fatalf("workspace %s was not migrated: task=%+v progress=%+v", workspaceID, task, progress)
		}
	}
}

func TestSummarizeMachineSyncTasksReportsRecoveryTerminalAndExpiry(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	now := time.Date(2026, 8, 17, 8, 0, 0, 0, time.UTC)
	for _, task := range []SyncTask{
		{WorkspaceID: "queued", Status: SyncTaskStatusPending, LastRequestedAt: now},
		{WorkspaceID: "recoverable", Status: SyncTaskStatusPending, LastRequestedAt: now, V2Triggers: []V2SyncTrigger{{Kind: "post-commit", EventID: "event-recovery", CommitSHA: strings.Repeat("a", 40), CapturedAt: now.Add(-89 * 24 * time.Hour)}}},
		{Version: 1, WorkspaceID: "legacy", Status: SyncTaskStatusPending, LastRequestedAt: now.Add(-89 * 24 * time.Hour), TriggerKind: "post-commit", TriggerEventID: "event-legacy", TriggerCommitSHA: strings.Repeat("b", 40)},
	} {
		if err := SaveSyncTask(task); err != nil {
			t.Fatal(err)
		}
	}
	if err := attributionlocal.SaveV2ClaimState(&attributionlocal.V2ClaimState{Version: 1, Claims: []attributionlocal.V2ClaimCandidate{{DeliveryStatus: attributionlocal.V2DeliveryConflict}}}); err != nil {
		t.Fatal(err)
	}
	summary, err := SummarizeMachineSyncTasks(now)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Queued != 1 || summary.Recoverable != 2 || summary.Terminal != 1 || summary.Expiring != 2 {
		t.Fatalf("machine summary = %+v", summary)
	}
}

func mustSyncTaskPath(t *testing.T, workspaceID string) string {
	t.Helper()
	path, err := SyncTaskPath(workspaceID)
	if err != nil {
		t.Fatal(err)
	}
	return path
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

func TestMarkSyncTaskFailureRecordsSafeStageAndExactRemainingTriggers(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	task := &SyncTask{
		WorkspaceID: "ws-safe-failure", Status: SyncTaskStatusRunning, RunnerPID: 1111,
		LastRequestedAt: now, LeaseExpiresAt: ptrTime(now.Add(time.Minute)),
		V2Triggers: []V2SyncTrigger{{EventID: "event-a"}, {EventID: "event-b"}},
	}
	if err := SaveSyncTask(*task); err != nil {
		t.Fatal(err)
	}
	if err := MarkSyncTaskFailure(task, now, syncTaskFailure(SyncTaskFailureStageBackendDelivery, "backend claim delivery failed", errors.New("backend rejected client:raw-request"))); err != nil {
		t.Fatal(err)
	}
	if task.LastFailureStage != "backend_delivery" || task.LastFailureReason != "backend claim delivery failed" || task.RemainingTriggerCount != 2 || task.FirstFailureAt == nil || !task.FirstFailureAt.Equal(now) {
		t.Fatalf("failure diagnostics = %+v", task)
	}
	firstFailure := *task.FirstFailureAt
	if err := MarkSyncTaskFailure(task, now.Add(time.Minute), syncTaskFailure(SyncTaskFailureStageSourceScan, "local Codex evidence scan failed", errors.New("retry failed"))); err != nil {
		t.Fatal(err)
	}
	if task.FirstFailureAt == nil || !task.FirstFailureAt.Equal(firstFailure) {
		t.Fatalf("first_failure_at changed = %v, want %v", task.FirstFailureAt, firstFailure)
	}
}

func TestV2ClaimScanProgressPersistsCompletedSourceUnits(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	progress := &V2ClaimScanProgress{
		Version: v2ClaimScanProgressVersion, WorkspaceID: "ws-progress", ContextID: "context-1",
		SourceKeys: []string{"source-a", "source-b"}, CompletedUnits: []string{"source-a"},
		StartedAt: time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC),
	}
	if err := SaveV2ClaimScanProgress(progress); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadV2ClaimScanProgress("ws-progress")
	if err != nil {
		t.Fatal(err)
	}
	if loaded == nil || loaded.ContextID != "context-1" || len(loaded.CompletedUnits) != 1 || loaded.Complete {
		t.Fatalf("loaded progress = %+v", loaded)
	}
	loaded.CompletedUnits = append(loaded.CompletedUnits, "source-b")
	loaded.Complete = true
	if err := SaveV2ClaimScanProgress(loaded); err != nil {
		t.Fatal(err)
	}
	reloaded, err := LoadV2ClaimScanProgress("ws-progress")
	if err != nil || reloaded == nil || !reloaded.Complete || len(reloaded.CompletedUnits) != 2 {
		t.Fatalf("reloaded progress = %+v, err=%v", reloaded, err)
	}
}

func TestV2ClaimScanContextIDIgnoresTriggerCommit(t *testing.T) {
	option := attributionlocal.V2ClaimScanOptions{RepoRoot: "/tmp/repo", RepoConfigID: 1, RepoKey: "example.com/org/repo", WorkspaceID: "workspace"}
	option.CommitSHA = "commit-a"
	option.CheckpointEventID = "event-a"
	first, err := v2ClaimScanContextID(option)
	if err != nil {
		t.Fatal(err)
	}
	option.CommitSHA = "commit-b"
	option.CheckpointEventID = "event-b"
	second, err := v2ClaimScanContextID(option)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("new commit trigger reset the shared scan context")
	}
}

func TestRunV2ClaimSyncRescansOldProgressVersion(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(home, ".codex", "sessions", "session.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	execCtx := ExecutionContext{WorkspaceID: "ws-old-progress", RepoRoot: t.TempDir(), RepoConfigID: 9, RepoKey: "repo-host.example.com/org/repo"}
	now := time.Now().UTC()
	task := &SyncTask{WorkspaceID: execCtx.WorkspaceID, V2Triggers: []V2SyncTrigger{{Kind: "post-commit", EventID: "event-a", CommitSHA: "commit-a", CapturedAt: now}}}
	option := attributionlocal.V2ClaimScanOptions{
		RepoRoot: execCtx.RepoRoot, CommitSHA: "commit-a", RelayProviderID: 7,
		RepoConfigID: execCtx.RepoConfigID, RepoKey: execCtx.RepoKey, WorkspaceID: execCtx.WorkspaceID,
		CheckpointEventID: "event-a",
	}
	scan, err := attributionlocal.PrepareCodexV2ClaimScan(context.Background(), "", now.Add(-90*24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	sourceKeys := scan.SourceKeys()
	if len(sourceKeys) != 1 {
		t.Fatalf("source keys = %v, want one", sourceKeys)
	}
	contextID, err := v2ClaimScanContextID(option)
	if err != nil {
		t.Fatal(err)
	}
	unitID, err := v2ClaimScanUnitID(sourceKeys[0], option)
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveV2ClaimScanProgress(&V2ClaimScanProgress{
		Version: v2ClaimScanProgressVersion - 1, WorkspaceID: execCtx.WorkspaceID, ContextID: contextID,
		SourceKeys: sourceKeys, CompletedUnits: []string{unitID}, StartedAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	original := scanCodexV2ClaimSource
	calls := 0
	scanCodexV2ClaimSource = func(scan *attributionlocal.CodexV2ClaimScan, ctx context.Context, sourceKey string, options []attributionlocal.V2ClaimScanOptions) ([]attributionlocal.V2ClaimCandidate, error) {
		calls++
		return nil, nil
	}
	t.Cleanup(func() { scanCodexV2ClaimSource = original })
	uploader := countingV2Uploader{fakeUploader: &fakeUploader{}, client: &countingV2ClaimClient{}}
	protocol := client.AttributionProtocol{LedgerEpoch: client.AttributionLedgerEpochShadowV2, V1WritePolicy: client.AttributionV1WritePolicyAccept}
	if err := runV2ClaimSync(context.Background(), uploader, execCtx, task, protocol); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("source scans = %d, want old progress version to rescan once", calls)
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

	firstFailureAt := time.Date(2026, 5, 26, 7, 59, 0, 0, time.UTC)
	workspaceID := "ws-1"
	first := SyncTask{
		WorkspaceID:           workspaceID,
		RepoRoot:              "/tmp/repo",
		ServerURL:             "https://ae.example.com",
		AuthSubject:           "user:1",
		RepoConfigID:          2,
		RepoKey:               "github.com/acme/repo",
		Status:                SyncTaskStatusPending,
		LastRequestedAt:       time.Date(2026, 5, 26, 8, 0, 0, 0, time.UTC),
		LastError:             "backend claim delivery failed",
		LastFailureStage:      "backend_delivery",
		LastFailureReason:     "backend claim delivery failed",
		FirstFailureAt:        &firstFailureAt,
		RemainingTriggerCount: 2,
		V2Triggers: []V2SyncTrigger{
			{Kind: "post-commit", EventID: "event-a", CommitSHA: "commit-a"},
			{Kind: "post-commit", EventID: "event-b", CommitSHA: "commit-b"},
		},
	}
	second := first
	second.LastRequestedAt = first.LastRequestedAt.Add(2 * time.Minute)
	second.V2Triggers = []V2SyncTrigger{{Kind: "post-commit", EventID: "event-c", CommitSHA: "commit-c"}}

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
	if got.LastFailureStage != first.LastFailureStage || got.LastFailureReason != first.LastFailureReason || got.FirstFailureAt == nil || !got.FirstFailureAt.Equal(firstFailureAt) || got.RemainingTriggerCount != 3 {
		t.Fatalf("task diagnostics=%+v, want inherited diagnostics from prior failure", got)
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

func TestUpsertPendingSyncTaskUpgradesVersion2TriggerBeforeExactReplay(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	now := time.Now().UTC()
	legacy := SyncTask{
		Version: 2, WorkspaceID: "ws-version-2-replay", RepoRoot: "/repo", ServerURL: "https://ae.example.com", AuthSubject: "user:1",
		RepoConfigID: 23, RepoKey: "github.com/acme/repo", Status: SyncTaskStatusPending, LastRequestedAt: now,
		V2Triggers: []V2SyncTrigger{{
			Kind: "post-commit", EventID: "event-version-2", CommitSHA: strings.Repeat("a", 40), CapturedAt: now, RelayProviderID: 17,
		}},
	}
	if err := SaveSyncTask(legacy); err != nil {
		t.Fatal(err)
	}
	replay := legacy
	replay.Version = syncTaskVersion
	replay.V2Triggers[0].ServerURL = legacy.ServerURL
	replay.V2Triggers[0].AuthSubject = legacy.AuthSubject
	replay.V2Triggers[0].RepoConfigID = legacy.RepoConfigID
	replay.V2Triggers[0].RepoKey = legacy.RepoKey
	replay.V2Triggers[0].WorkspaceID = legacy.WorkspaceID
	if err := UpsertPendingSyncTask(replay); err != nil {
		t.Fatalf("exact version-2 replay: %v", err)
	}
	got, err := LoadSyncTask(legacy.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Version != syncTaskVersion || len(got.V2Triggers) != 1 {
		t.Fatalf("upgraded replay task = %+v", got)
	}
	trigger := got.V2Triggers[0]
	if trigger.ServerURL != legacy.ServerURL || trigger.AuthSubject != legacy.AuthSubject || trigger.RepoConfigID != legacy.RepoConfigID || trigger.RepoKey != legacy.RepoKey || trigger.WorkspaceID != legacy.WorkspaceID {
		t.Fatalf("upgraded replay trigger = %+v", trigger)
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

func TestRunPendingSyncTaskDrainsOtherMachineWorkspace(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	stubSyncTaskRunnerAlive(t, func(pid int) bool { return pid == os.Getpid() })
	now := time.Now().UTC()
	tasks := []SyncTask{
		{WorkspaceID: "ws-machine-a", RepoRoot: t.TempDir(), ServerURL: "https://ae.example.com", AuthSubject: "user:1", RepoConfigID: 2, RepoKey: "github.com/acme/repo-a", Status: SyncTaskStatusPending, LastRequestedAt: now},
		{WorkspaceID: "ws-machine-b", RepoRoot: t.TempDir(), ServerURL: "https://ae.example.com", AuthSubject: "user:1", RepoConfigID: 3, RepoKey: "github.com/acme/repo-b", Status: SyncTaskStatusPending, LastRequestedAt: now.Add(time.Second)},
	}
	for _, task := range tasks {
		if err := UpsertPendingSyncTask(task); err != nil {
			t.Fatal(err)
		}
	}
	original := runAttributionSync
	seen := map[string]int{}
	runAttributionSync = func(_ context.Context, opts attributionlocal.RunOptions, _ attributionlocal.BackendClient) error {
		seen[opts.WorkspaceID]++
		return nil
	}
	t.Cleanup(func() { runAttributionSync = original })
	execCtx := ExecutionContext{ServerURL: tasks[0].ServerURL, AuthSubject: tasks[0].AuthSubject, RepoConfigID: tasks[0].RepoConfigID, RepoKey: tasks[0].RepoKey, WorkspaceID: tasks[0].WorkspaceID, RepoRoot: tasks[0].RepoRoot, DurableReplay: true}
	if err := RunPendingSyncTask(context.Background(), execCtx, syncCapableFakeUploader{fakeUploader: &fakeUploader{}}); err != nil {
		t.Fatal(err)
	}
	if seen["ws-machine-a"] != 1 || seen["ws-machine-b"] != 1 {
		t.Fatalf("machine workspace passes = %+v, want each workspace once", seen)
	}
	for _, task := range tasks {
		if got, err := LoadSyncTask(task.WorkspaceID); err != nil || got != nil {
			t.Fatalf("completed task %s = %+v, %v, want deleted", task.WorkspaceID, got, err)
		}
	}
}

func TestRunPendingSyncTaskSerializesMachineWorkspaces(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	stubSyncTaskRunnerAlive(t, func(pid int) bool { return pid == os.Getpid() })
	now := time.Now().UTC()
	tasks := []SyncTask{
		{WorkspaceID: "ws-serial-a", RepoRoot: t.TempDir(), ServerURL: "https://ae.example.com", AuthSubject: "user:1", RepoConfigID: 2, RepoKey: "github.com/acme/repo-a", Status: SyncTaskStatusPending, LastRequestedAt: now},
		{WorkspaceID: "ws-serial-b", RepoRoot: t.TempDir(), ServerURL: "https://ae.example.com", AuthSubject: "user:1", RepoConfigID: 3, RepoKey: "github.com/acme/repo-b", Status: SyncTaskStatusPending, LastRequestedAt: now.Add(time.Second)},
	}
	for _, task := range tasks {
		if err := UpsertPendingSyncTask(task); err != nil {
			t.Fatal(err)
		}
	}
	original := runAttributionSync
	var active int32
	var maximum int32
	runAttributionSync = func(_ context.Context, _ attributionlocal.RunOptions, _ attributionlocal.BackendClient) error {
		current := atomic.AddInt32(&active, 1)
		for {
			observed := atomic.LoadInt32(&maximum)
			if current <= observed || atomic.CompareAndSwapInt32(&maximum, observed, current) {
				break
			}
		}
		time.Sleep(30 * time.Millisecond)
		atomic.AddInt32(&active, -1)
		return nil
	}
	t.Cleanup(func() { runAttributionSync = original })
	start := make(chan struct{})
	errs := make(chan error, len(tasks))
	var wg sync.WaitGroup
	for _, task := range tasks {
		task := task
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			execCtx := ExecutionContext{ServerURL: task.ServerURL, AuthSubject: task.AuthSubject, RepoConfigID: task.RepoConfigID, RepoKey: task.RepoKey, WorkspaceID: task.WorkspaceID, RepoRoot: task.RepoRoot, DurableReplay: true}
			errs <- RunPendingSyncTask(context.Background(), execCtx, syncCapableFakeUploader{fakeUploader: &fakeUploader{}})
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil && !errors.Is(err, ErrSyncTaskAlreadyRunning) {
			t.Fatal(err)
		}
	}
	if maximum != 1 {
		t.Fatalf("concurrent machine sync passes = %d, want one owner", maximum)
	}
	for _, task := range tasks {
		if got, err := LoadSyncTask(task.WorkspaceID); err != nil || got != nil {
			t.Fatalf("serialized task %s = %+v, %v, want deleted", task.WorkspaceID, got, err)
		}
	}
}

func TestRunPendingSyncTaskWaiterDrainsTaskAfterOwnerFailure(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	stubSyncTaskRunnerAlive(t, func(pid int) bool { return pid == os.Getpid() })
	now := time.Now().UTC()
	ownerTask := SyncTask{WorkspaceID: "ws-owner-failure", RepoRoot: t.TempDir(), ServerURL: "https://ae.example.com", AuthSubject: "user:1", RepoConfigID: 2, RepoKey: "github.com/acme/repo-a", Status: SyncTaskStatusPending, LastRequestedAt: now}
	waiterTask := SyncTask{WorkspaceID: "ws-waiter-successor", RepoRoot: t.TempDir(), ServerURL: ownerTask.ServerURL, AuthSubject: ownerTask.AuthSubject, RepoConfigID: 3, RepoKey: "github.com/acme/repo-b", Status: SyncTaskStatusPending, LastRequestedAt: now.Add(time.Second)}
	if err := UpsertPendingSyncTask(ownerTask); err != nil {
		t.Fatal(err)
	}

	ownerStarted := make(chan struct{})
	releaseOwner := make(chan struct{})
	original := runAttributionSync
	runAttributionSync = func(_ context.Context, opts attributionlocal.RunOptions, _ attributionlocal.BackendClient) error {
		if opts.WorkspaceID == ownerTask.WorkspaceID {
			select {
			case <-ownerStarted:
			default:
				close(ownerStarted)
			}
			<-releaseOwner
			return errors.New("owner sync failed")
		}
		return nil
	}
	t.Cleanup(func() { runAttributionSync = original })

	uploader := syncCapableFakeUploader{fakeUploader: &fakeUploader{}}
	ownerCtx := ExecutionContext{ServerURL: ownerTask.ServerURL, AuthSubject: ownerTask.AuthSubject, RepoConfigID: ownerTask.RepoConfigID, RepoKey: ownerTask.RepoKey, WorkspaceID: ownerTask.WorkspaceID, RepoRoot: ownerTask.RepoRoot, DurableReplay: true}
	waiterCtx := ExecutionContext{ServerURL: waiterTask.ServerURL, AuthSubject: waiterTask.AuthSubject, RepoConfigID: waiterTask.RepoConfigID, RepoKey: waiterTask.RepoKey, WorkspaceID: waiterTask.WorkspaceID, RepoRoot: waiterTask.RepoRoot, DurableReplay: true}
	ownerDone := make(chan error, 1)
	go func() { ownerDone <- RunPendingSyncTask(context.Background(), ownerCtx, uploader) }()
	<-ownerStarted
	if err := UpsertPendingSyncTask(waiterTask); err != nil {
		t.Fatal(err)
	}
	waiterDone := make(chan error, 1)
	go func() { waiterDone <- RunPendingSyncTask(context.Background(), waiterCtx, uploader) }()
	time.Sleep(50 * time.Millisecond)
	close(releaseOwner)

	for name, done := range map[string]<-chan error{"owner": ownerDone, "waiter": waiterDone} {
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatalf("%s runner did not finish", name)
		}
	}
	if got, err := LoadSyncTask(waiterTask.WorkspaceID); err != nil || got != nil {
		t.Fatalf("waiter task = %+v, %v, want drained without another event", got, err)
	}
}

func TestRunPendingSyncTaskCoalescesConcurrentMachineWakeups(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	stubSyncTaskRunnerAlive(t, func(pid int) bool { return pid == os.Getpid() })
	now := time.Now().UTC()
	task := SyncTask{
		WorkspaceID: "ws-machine-owner", RepoRoot: t.TempDir(), ServerURL: "https://ae.example.com", AuthSubject: "user:1",
		RepoConfigID: 2, RepoKey: "github.com/acme/repo", Status: SyncTaskStatusPending, LastRequestedAt: now,
	}
	if err := UpsertPendingSyncTask(task); err != nil {
		t.Fatal(err)
	}

	ownerStarted := make(chan struct{})
	releaseOwner := make(chan struct{})
	original := runAttributionSync
	runAttributionSync = func(context.Context, attributionlocal.RunOptions, attributionlocal.BackendClient) error {
		select {
		case <-ownerStarted:
		default:
			close(ownerStarted)
		}
		<-releaseOwner
		return nil
	}
	t.Cleanup(func() { runAttributionSync = original })

	execCtx := ExecutionContext{
		ServerURL: task.ServerURL, AuthSubject: task.AuthSubject, RepoConfigID: task.RepoConfigID,
		RepoKey: task.RepoKey, WorkspaceID: task.WorkspaceID, RepoRoot: task.RepoRoot, DurableReplay: true,
	}
	ownerDone := make(chan error, 1)
	go func() {
		ownerDone <- RunPendingSyncTask(context.Background(), execCtx, syncCapableFakeUploader{fakeUploader: &fakeUploader{}})
	}()
	<-ownerStarted

	started := time.Now()
	errs := make(chan error, 100)
	var wg sync.WaitGroup
	for range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			errs <- RunPendingSyncTask(ctx, execCtx, syncCapableFakeUploader{fakeUploader: &fakeUploader{}})
		}()
	}
	wg.Wait()
	close(errs)
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("coalesced wakeups waited %s for the active owner", elapsed)
	}
	alreadyRunning := 0
	boundedWaiter := 0
	for err := range errs {
		switch {
		case errors.Is(err, ErrSyncTaskAlreadyRunning):
			alreadyRunning++
		case errors.Is(err, context.DeadlineExceeded):
			boundedWaiter++
		default:
			t.Fatalf("concurrent wakeup error = %v", err)
		}
	}
	if alreadyRunning != 99 || boundedWaiter != 1 {
		t.Fatalf("coalesced wakeups: already_running=%d bounded_waiter=%d, want 99/1", alreadyRunning, boundedWaiter)
	}

	close(releaseOwner)
	if err := <-ownerDone; err != nil {
		t.Fatal(err)
	}
}

func TestRunPendingSyncTaskDifferentOwnerWakeUsesScopedWaiter(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	stubSyncTaskRunnerAlive(t, func(pid int) bool { return pid == os.Getpid() })
	now := time.Now().UTC()
	ownerTask := SyncTask{
		WorkspaceID: "ws-owner-a", RepoRoot: t.TempDir(), ServerURL: "https://ae-a.example.com", AuthSubject: "user:a",
		RepoConfigID: 1, RepoKey: "github.com/acme/repo-a", Status: SyncTaskStatusPending, LastRequestedAt: now,
	}
	otherTask := SyncTask{
		WorkspaceID: "ws-owner-b", RepoRoot: t.TempDir(), ServerURL: "https://ae-b.example.com", AuthSubject: "user:b",
		RepoConfigID: 2, RepoKey: "github.com/acme/repo-b", Status: SyncTaskStatusPending, LastRequestedAt: now,
	}
	if err := UpsertPendingSyncTask(ownerTask); err != nil {
		t.Fatal(err)
	}
	ownerStarted := make(chan struct{})
	releaseOwner := make(chan struct{})
	originalRun := runAttributionSync
	runAttributionSync = func(context.Context, attributionlocal.RunOptions, attributionlocal.BackendClient) error {
		select {
		case <-ownerStarted:
		default:
			close(ownerStarted)
		}
		<-releaseOwner
		return nil
	}
	t.Cleanup(func() {
		runAttributionSync = originalRun
	})
	uploader := syncCapableFakeUploader{fakeUploader: &fakeUploader{}}
	ownerCtx := executionContextFromSyncTask(ownerTask)
	otherCtx := executionContextFromSyncTask(otherTask)
	ownerDone := make(chan error, 1)
	go func() { ownerDone <- RunPendingSyncTask(context.Background(), ownerCtx, uploader) }()
	<-ownerStarted
	if err := UpsertPendingSyncTask(otherTask); err != nil {
		t.Fatal(err)
	}
	otherDone := make(chan error, 1)
	go func() { otherDone <- RunPendingSyncTask(context.Background(), otherCtx, uploader) }()
	select {
	case err := <-otherDone:
		t.Fatalf("different owner returned before lock handoff: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(releaseOwner)
	if err := <-ownerDone; err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-otherDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("different reporting owner waiter did not acquire the released lock")
	}
	if task, err := LoadSyncTask(otherTask.WorkspaceID); err != nil || task != nil {
		t.Fatalf("different owner task = %+v, %v, want drained", task, err)
	}
}

func TestRunDetachedPendingSyncTaskSpawnsSuccessorAtOwnerDeadline(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	task := SyncTask{
		WorkspaceID: "ws-owner-deadline", RepoRoot: t.TempDir(), ServerURL: "https://ae.example.com", AuthSubject: "user:1",
		RepoConfigID: 2, RepoKey: "github.com/acme/repo", Status: SyncTaskStatusPending, LastRequestedAt: time.Now().UTC(),
	}
	if err := UpsertPendingSyncTask(task); err != nil {
		t.Fatal(err)
	}

	originalRun := runAttributionSync
	runAttributionSync = func(ctx context.Context, _ attributionlocal.RunOptions, _ attributionlocal.BackendClient) error {
		<-ctx.Done()
		return ctx.Err()
	}
	originalTimeout := machineSyncOwnerTimeout
	machineSyncOwnerTimeout = 20 * time.Millisecond
	originalSpawn := spawnBackgroundSyncRunner
	spawned := make(chan string, 1)
	spawnBackgroundSyncRunner = func(repoRoot string) error {
		spawned <- repoRoot
		return nil
	}
	t.Cleanup(func() {
		runAttributionSync = originalRun
		machineSyncOwnerTimeout = originalTimeout
		spawnBackgroundSyncRunner = originalSpawn
	})

	execCtx := ExecutionContext{
		ServerURL: task.ServerURL, AuthSubject: task.AuthSubject, RepoConfigID: task.RepoConfigID,
		RepoKey: task.RepoKey, WorkspaceID: task.WorkspaceID, RepoRoot: task.RepoRoot, DurableReplay: true,
	}
	if err := RunDetachedPendingSyncTask(execCtx, syncCapableFakeUploader{fakeUploader: &fakeUploader{}}); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-spawned:
		if got != task.RepoRoot {
			t.Fatalf("successor repo root = %q, want %q", got, task.RepoRoot)
		}
	default:
		t.Fatal("owner deadline did not start a successor")
	}
	retained, err := LoadSyncTask(task.WorkspaceID)
	if err != nil || retained == nil || retained.Status != SyncTaskStatusYielded {
		t.Fatalf("deadline task = %+v, %v, want yielded", retained, err)
	}
}

func TestRunPendingSyncTaskRecoversDeadMachineOwner(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	stubSyncTaskRunnerAlive(t, func(pid int) bool { return pid == os.Getpid() })
	task := SyncTask{WorkspaceID: "ws-dead-owner", RepoRoot: t.TempDir(), ServerURL: "https://ae.example.com", AuthSubject: "user:1", RepoConfigID: 2, RepoKey: "github.com/acme/repo", Status: SyncTaskStatusPending, LastRequestedAt: time.Now().UTC()}
	if err := UpsertPendingSyncTask(task); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(attributionlocal.AttributionRootDir(), "machine-sync.run.lock")
	if err := os.WriteFile(lockPath, []byte("2147483647\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	original := runAttributionSync
	runAttributionSync = func(context.Context, attributionlocal.RunOptions, attributionlocal.BackendClient) error { return nil }
	t.Cleanup(func() { runAttributionSync = original })
	execCtx := ExecutionContext{ServerURL: task.ServerURL, AuthSubject: task.AuthSubject, RepoConfigID: task.RepoConfigID, RepoKey: task.RepoKey, WorkspaceID: task.WorkspaceID, RepoRoot: task.RepoRoot, DurableReplay: true}
	if err := RunPendingSyncTask(context.Background(), execCtx, syncCapableFakeUploader{fakeUploader: &fakeUploader{}}); err != nil {
		t.Fatal(err)
	}
	if got, err := LoadSyncTask(task.WorkspaceID); err != nil || got != nil {
		t.Fatalf("dead-owner task = %+v, %v, want drained", got, err)
	}
}

func TestRunPendingSyncTaskContinuesAfterBoundedYield(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	stubSyncTaskRunnerAlive(t, func(pid int) bool { return pid == os.Getpid() })
	originalTimeout := syncTaskRunTimeout
	syncTaskRunTimeout = 10 * time.Millisecond
	t.Cleanup(func() { syncTaskRunTimeout = originalTimeout })
	task := SyncTask{WorkspaceID: "ws-yield", RepoRoot: t.TempDir(), ServerURL: "https://ae.example.com", AuthSubject: "user:1", RepoConfigID: 2, RepoKey: "github.com/acme/repo", Status: SyncTaskStatusPending, LastRequestedAt: time.Now().UTC()}
	if err := UpsertPendingSyncTask(task); err != nil {
		t.Fatal(err)
	}
	original := runAttributionSync
	calls := 0
	runAttributionSync = func(ctx context.Context, _ attributionlocal.RunOptions, _ attributionlocal.BackendClient) error {
		calls++
		if calls == 1 {
			<-ctx.Done()
			return ctx.Err()
		}
		return nil
	}
	t.Cleanup(func() { runAttributionSync = original })
	execCtx := ExecutionContext{ServerURL: task.ServerURL, AuthSubject: task.AuthSubject, RepoConfigID: task.RepoConfigID, RepoKey: task.RepoKey, WorkspaceID: task.WorkspaceID, RepoRoot: task.RepoRoot, DurableReplay: true}
	if err := RunPendingSyncTask(context.Background(), execCtx, syncCapableFakeUploader{fakeUploader: &fakeUploader{}}); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("bounded sync passes = %d, want automatic successor", calls)
	}
	if got, err := LoadSyncTask(task.WorkspaceID); err != nil || got != nil {
		t.Fatalf("completed yielded task = %+v, %v, want deleted", got, err)
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

type countingV2ClaimClient struct{ calls int }

func (c *countingV2ClaimClient) SendAttributionV2Claims(context.Context, []client.AttributionV2ClaimGroup) (*client.AttributionV2ClaimBatchResult, error) {
	c.calls++
	return nil, nil
}

type acknowledgingV2ClaimClient struct {
	calls  int
	groups []client.AttributionV2ClaimGroup
}

func (c *acknowledgingV2ClaimClient) SendAttributionV2Claims(_ context.Context, groups []client.AttributionV2ClaimGroup) (*client.AttributionV2ClaimBatchResult, error) {
	c.calls++
	c.groups = append(c.groups, groups...)
	results := make([]client.AttributionV2ClaimResult, 0, len(groups))
	for _, group := range groups {
		requests := make([]client.AttributionV2ItemStatus, 0, len(group.RequestIDs))
		for _, requestID := range group.RequestIDs {
			requests = append(requests, client.AttributionV2ItemStatus{ID: requestID, Status: "persisted"})
		}
		results = append(results, client.AttributionV2ClaimResult{Group: client.AttributionV2ItemStatus{ID: group.GroupID, Status: "persisted"}, Requests: requests})
	}
	return &client.AttributionV2ClaimBatchResult{LedgerEpoch: client.AttributionLedgerEpochShadowV2, V1WritePolicy: client.AttributionV1WritePolicyAccept, Results: results}, nil
}

type acknowledgingV2Uploader struct {
	*fakeUploader
	client *acknowledgingV2ClaimClient
}

func (u acknowledgingV2Uploader) V2ClaimClient() attributionlocal.V2ClaimBackendClient {
	return u.client
}

func (u acknowledgingV2Uploader) RelayProviderID() int { return 7 }

type recoveringV2Uploader struct {
	acknowledgingV2Uploader
	providerID int
}

func (u recoveringV2Uploader) AttributionProtocol() client.AttributionProtocol {
	return client.AttributionProtocol{LedgerEpoch: client.AttributionLedgerEpochShadowV2, V1WritePolicy: client.AttributionV1WritePolicyAccept}
}

func (u recoveringV2Uploader) RelayProviderID() int { return u.providerID }

func TestRunPendingSyncTaskMigratesAndRecoversDeletedLegacyWorktree(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	stubSyncTaskRunnerAlive(t, func(pid int) bool { return pid == os.Getpid() })
	repo := initRepoWithCommit2(t)
	temporaryRoot := filepath.Join(t.TempDir(), "temporary-worktree")
	git2(t, repo, "worktree", "add", "-b", "temporary-recovery", temporaryRoot)
	if err := os.WriteFile(filepath.Join(temporaryRoot, "recovered.txt"), []byte("recovered\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git2(t, temporaryRoot, "add", "recovered.txt")
	git2(t, temporaryRoot, "commit", "-m", "test: retained trigger")
	commitSHA := git2(t, temporaryRoot, "rev-parse", "HEAD")
	temporaryContext, err := DetectGitContext(temporaryRoot)
	if err != nil {
		t.Fatal(err)
	}
	currentContext, err := DetectGitContext(repo)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	eventID, err := CheckpointEventID("repo_config_id:23\x1fuser:1\x1f"+temporaryContext.WorkspaceID, commitSHA)
	if err != nil {
		t.Fatal(err)
	}
	task := SyncTask{
		Version:     1,
		WorkspaceID: temporaryContext.WorkspaceID, RepoRoot: temporaryRoot, ServerURL: "https://ae.example.com", AuthSubject: "user:1",
		RepoConfigID: 23, RepoKey: temporaryContext.RepoKey, Status: SyncTaskStatusPending, LastRequestedAt: now,
		V2Triggers: []V2SyncTrigger{{Kind: "post-commit", EventID: eventID, CommitSHA: commitSHA, CapturedAt: now}},
	}
	if err := UpsertPendingSyncTask(task); err != nil {
		t.Fatal(err)
	}
	sourcePath := filepath.Join(home, ".codex", "sessions", "recovery.jsonl")
	if err := os.MkdirAll(filepath.Dir(sourcePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sourcePath, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git2(t, repo, "worktree", "remove", temporaryRoot)
	if _, err := os.Stat(temporaryRoot); !os.IsNotExist(err) {
		t.Fatalf("temporary worktree still exists: %v", err)
	}

	originalScan := scanCodexV2ClaimSource
	scanCodexV2ClaimSource = func(_ *attributionlocal.CodexV2ClaimScan, _ context.Context, sourceKey string, options []attributionlocal.V2ClaimScanOptions) ([]attributionlocal.V2ClaimCandidate, error) {
		if len(options) != 1 {
			return nil, fmt.Errorf("scan options = %d, want 1", len(options))
		}
		option := options[0]
		if option.RepoRoot != repo || option.RepoConfigID != 23 || option.RepoKey != temporaryContext.RepoKey || option.WorkspaceID != temporaryContext.WorkspaceID || option.CheckpointEventID != eventID || option.CommitSHA != commitSHA || option.RelayProviderID != 99 {
			return nil, fmt.Errorf("recovered option = %+v", option)
		}
		return []attributionlocal.V2ClaimCandidate{{
			LocalKey: sourceKey, FirstSeenAt: now,
			Group: client.AttributionV2ClaimGroup{
				SchemaVersion: 2, GroupID: "group-recovery", RelayProviderID: option.RelayProviderID,
				TokenSource: client.AttributionV2TokenSourceRelayOfficial, RequestIDs: []string{"request-synthetic"}, EvidenceDigest: "evidence-recovery",
				CommitAllocations: []client.AttributionV2CommitAllocation{{Sequence: 1, RepoConfigID: option.RepoConfigID, RepoKey: option.RepoKey, WorkspaceID: option.WorkspaceID, CheckpointEventID: option.CheckpointEventID, CommitSHA: option.CommitSHA, EvidenceDigest: "evidence-recovery"}},
			},
		}}, nil
	}
	t.Cleanup(func() { scanCodexV2ClaimSource = originalScan })
	backend := &acknowledgingV2ClaimClient{}
	uploader := recoveringV2Uploader{acknowledgingV2Uploader: acknowledgingV2Uploader{fakeUploader: &fakeUploader{}, client: backend}, providerID: 99}
	seed := ExecutionContext{
		ServerURL: task.ServerURL, AuthSubject: task.AuthSubject, RepoConfigID: task.RepoConfigID, RepoKey: task.RepoKey,
		WorkspaceID: currentContext.WorkspaceID, RepoRoot: repo, DurableReplay: true,
	}
	originalSpawn := spawnBackgroundSyncRunner
	spawned := 0
	spawnBackgroundSyncRunner = func(root string) error {
		if root != repo {
			return fmt.Errorf("spawn root = %q, want %q", root, repo)
		}
		spawned++
		return nil
	}
	t.Cleanup(func() { spawnBackgroundSyncRunner = originalSpawn })
	if err := NewHandler(uploader).PrePushResolved(seed); err != nil {
		t.Fatal(err)
	}
	if spawned != 1 {
		t.Fatalf("pre-push recovery runners = %d, want 1", spawned)
	}
	if err := RunPendingSyncTask(context.Background(), seed, uploader); err != nil {
		t.Fatal(err)
	}
	if backend.calls != 1 || len(backend.groups) != 1 || len(backend.groups[0].CommitAllocations) != 1 {
		t.Fatalf("accepted recovery groups = %+v, calls=%d", backend.groups, backend.calls)
	}
	if backend.groups[0].RelayProviderID != 99 || backend.groups[0].CommitAllocations[0].WorkspaceID != temporaryContext.WorkspaceID {
		t.Fatalf("recovered identities = %+v", backend.groups[0])
	}
	if remaining, err := LoadSyncTask(temporaryContext.WorkspaceID); err != nil || remaining != nil {
		t.Fatalf("recovered task = %+v, %v, want drained", remaining, err)
	}
}

func TestRunPendingSyncTaskRetainsUnreachableRecoveryCommit(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	stubSyncTaskRunnerAlive(t, func(pid int) bool { return pid == os.Getpid() })
	repo := initRepoWithCommit2(t)
	gitCtx, err := DetectGitContext(repo)
	if err != nil {
		t.Fatal(err)
	}
	unreachableCommit := strings.Repeat("f", 40)
	unreachableEvent, err := CheckpointEventID(eventIDRepoHint(ExecutionContext{RepoConfigID: 23, AuthSubject: "user:1", WorkspaceID: gitCtx.WorkspaceID}), unreachableCommit)
	if err != nil {
		t.Fatal(err)
	}
	task := SyncTask{
		WorkspaceID: gitCtx.WorkspaceID, RepoRoot: repo, ServerURL: "https://ae.example.com", AuthSubject: "user:1",
		RepoConfigID: 23, RepoKey: gitCtx.RepoKey, Status: SyncTaskStatusPending, LastRequestedAt: time.Now().UTC(),
		V2Triggers: []V2SyncTrigger{{Kind: "post-commit", EventID: unreachableEvent, CommitSHA: unreachableCommit, CapturedAt: time.Now().UTC(), RelayProviderID: 17}},
	}
	if err := UpsertPendingSyncTask(task); err != nil {
		t.Fatal(err)
	}
	seed := ExecutionContext{
		ServerURL: task.ServerURL, AuthSubject: task.AuthSubject, RepoConfigID: task.RepoConfigID, RepoKey: task.RepoKey,
		WorkspaceID: gitCtx.WorkspaceID, RepoRoot: repo, DurableReplay: true,
	}
	if err := RunPendingSyncTask(context.Background(), seed, syncCapableFakeUploader{fakeUploader: &fakeUploader{}}); err == nil {
		t.Fatal("unreachable recovery error = nil")
	}
	retained, err := LoadSyncTask(task.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if retained == nil || retained.LastFailureStage != SyncTaskFailureStageLocalState || retained.LastFailureReason != "commit unavailable in recovery checkout" || len(retained.V2Triggers) != 1 {
		t.Fatalf("retained unreachable task = %+v", retained)
	}
}

func TestRunPendingSyncTaskProcessesReachableTriggerAndRetainsUnreachableSibling(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	stubSyncTaskRunnerAlive(t, func(pid int) bool { return pid == os.Getpid() })
	repo := initRepoWithCommit2(t)
	gitCtx, err := DetectGitContext(repo)
	if err != nil {
		t.Fatal(err)
	}
	reachableCommit := git2(t, repo, "rev-parse", "HEAD")
	unreachableCommit := strings.Repeat("f", 40)
	contextHint := eventIDRepoHint(ExecutionContext{RepoConfigID: 23, AuthSubject: "user:1", WorkspaceID: gitCtx.WorkspaceID})
	reachableEvent, err := CheckpointEventID(contextHint, reachableCommit)
	if err != nil {
		t.Fatal(err)
	}
	unreachableEvent, err := CheckpointEventID(contextHint, unreachableCommit)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	task := SyncTask{
		WorkspaceID: gitCtx.WorkspaceID, RepoRoot: repo, ServerURL: "https://ae.example.com", AuthSubject: "user:1",
		RepoConfigID: 23, RepoKey: gitCtx.RepoKey, Status: SyncTaskStatusPending, LastRequestedAt: now,
		V2Triggers: []V2SyncTrigger{
			{Kind: "post-commit", EventID: reachableEvent, CommitSHA: reachableCommit, CapturedAt: now, RelayProviderID: 17},
			{Kind: "post-commit", EventID: unreachableEvent, CommitSHA: unreachableCommit, CapturedAt: now, RelayProviderID: 17},
		},
	}
	if err := UpsertPendingSyncTask(task); err != nil {
		t.Fatal(err)
	}
	sourcePath := filepath.Join(home, ".codex", "sessions", "mixed-recovery.jsonl")
	if err := os.MkdirAll(filepath.Dir(sourcePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sourcePath, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	originalScan := scanCodexV2ClaimSource
	scanCodexV2ClaimSource = func(_ *attributionlocal.CodexV2ClaimScan, _ context.Context, sourceKey string, options []attributionlocal.V2ClaimScanOptions) ([]attributionlocal.V2ClaimCandidate, error) {
		if len(options) != 1 || options[0].CommitSHA != reachableCommit || options[0].CheckpointEventID != reachableEvent {
			return nil, fmt.Errorf("mixed recovery options = %+v, want reachable trigger only", options)
		}
		return []attributionlocal.V2ClaimCandidate{{
			LocalKey: sourceKey, FirstSeenAt: now,
			Group: client.AttributionV2ClaimGroup{
				SchemaVersion: 2, GroupID: "group-mixed-recovery", RelayProviderID: 17,
				TokenSource: client.AttributionV2TokenSourceRelayOfficial, RequestIDs: []string{"request-synthetic"}, EvidenceDigest: "evidence-mixed-recovery",
				CommitAllocations: []client.AttributionV2CommitAllocation{{
					Sequence: 1, RepoConfigID: 23, RepoKey: gitCtx.RepoKey, WorkspaceID: gitCtx.WorkspaceID,
					CheckpointEventID: reachableEvent, CommitSHA: reachableCommit, EvidenceDigest: "evidence-mixed-recovery",
				}},
			},
		}}, nil
	}
	t.Cleanup(func() { scanCodexV2ClaimSource = originalScan })
	backend := &acknowledgingV2ClaimClient{}
	uploader := recoveringV2Uploader{acknowledgingV2Uploader: acknowledgingV2Uploader{fakeUploader: &fakeUploader{}, client: backend}, providerID: 17}
	seed := ExecutionContext{
		ServerURL: task.ServerURL, AuthSubject: task.AuthSubject, RepoConfigID: task.RepoConfigID, RepoKey: task.RepoKey,
		WorkspaceID: gitCtx.WorkspaceID, RepoRoot: repo, DurableReplay: true,
	}
	if err := RunPendingSyncTask(context.Background(), seed, uploader); err == nil {
		t.Fatal("mixed recovery error = nil, want retained sibling diagnostic")
	}
	if backend.calls != 1 || len(backend.groups) != 1 || backend.groups[0].CommitAllocations[0].CommitSHA != reachableCommit {
		t.Fatalf("mixed recovery backend groups = %+v, calls=%d", backend.groups, backend.calls)
	}
	retained, err := LoadSyncTask(task.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if retained == nil || len(retained.V2Triggers) != 1 || retained.V2Triggers[0].CommitSHA != unreachableCommit || retained.LastFailureReason != "commit unavailable in recovery checkout" {
		t.Fatalf("mixed recovery retained task = %+v", retained)
	}
}

func TestExecutionContextForSyncTaskRejectsMismatchedCheckpointRecovery(t *testing.T) {
	repo := initRepoWithCommit2(t)
	seedGit, err := DetectGitContext(repo)
	if err != nil {
		t.Fatal(err)
	}
	task := SyncTask{
		WorkspaceID: "removed-workspace", RepoRoot: filepath.Join(t.TempDir(), "gone"), ServerURL: "https://ae.example.com", AuthSubject: "user:1",
		RepoConfigID: 23, RepoKey: seedGit.RepoKey,
		V2Triggers: []V2SyncTrigger{{Kind: "post-commit", EventID: "mismatched-event", CommitSHA: git2(t, repo, "rev-parse", "HEAD"), RelayProviderID: 17}},
	}
	seed := ExecutionContext{RepoConfigID: 23, RepoKey: seedGit.RepoKey, WorkspaceID: seedGit.WorkspaceID, RepoRoot: repo}
	_, err = executionContextForSyncTask(task, seed)
	var stageErr *syncTaskStageError
	if !errors.As(err, &stageErr) || stageErr.stage != SyncTaskFailureStageLocalState || stageErr.reason != "checkpoint identity does not match" {
		t.Fatalf("mismatched-checkpoint recovery error = %v", err)
	}
}

func TestPruneExpiredV2SyncTriggersUsesNinetyDayBoundary(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	now := time.Date(2026, 8, 17, 8, 0, 0, 0, time.UTC)
	firstFailure := now.Add(-time.Hour)
	task := SyncTask{
		WorkspaceID: "workspace-prune", Status: SyncTaskStatusPending, FirstFailureAt: &firstFailure, RemainingTriggerCount: 2,
		V2Triggers: []V2SyncTrigger{
			{Kind: "post-commit", EventID: "event-expired", CapturedAt: now.Add(-v2SyncTriggerRetention)},
			{Kind: "post-commit", EventID: "event-kept", CapturedAt: now.Add(-v2SyncTriggerRetention).Add(time.Nanosecond)},
		},
	}
	if err := SaveSyncTask(task); err != nil {
		t.Fatal(err)
	}
	deleted, err := pruneExpiredV2SyncTriggers(&task, now, v2SyncTriggerRetention)
	if err != nil {
		t.Fatal(err)
	}
	if deleted || len(task.V2Triggers) != 1 || task.V2Triggers[0].EventID != "event-kept" || task.RemainingTriggerCount != 1 {
		t.Fatalf("pruned task = %+v, deleted=%t", task, deleted)
	}

	task.V2Triggers[0].CapturedAt = now.Add(-v2SyncTriggerRetention)
	if err := SaveSyncTask(task); err != nil {
		t.Fatal(err)
	}
	if err := SaveV2ClaimScanProgress(&V2ClaimScanProgress{WorkspaceID: task.WorkspaceID}); err != nil {
		t.Fatal(err)
	}
	deleted, err = pruneExpiredV2SyncTriggers(&task, now, v2SyncTriggerRetention)
	if err != nil {
		t.Fatal(err)
	}
	remaining, loadErr := LoadSyncTask(task.WorkspaceID)
	progress, progressErr := LoadV2ClaimScanProgress(task.WorkspaceID)
	if !deleted || loadErr != nil || remaining != nil || progressErr != nil || progress != nil {
		t.Fatalf("expired task=%+v loadErr=%v progress=%+v progressErr=%v deleted=%t", remaining, loadErr, progress, progressErr, deleted)
	}
}

func TestExecutionContextForSyncTaskRetainsOnlyMismatchedTriggerRepositoryIdentity(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo := initRepoWithCommit2(t)
	gitCtx, err := DetectGitContext(repo)
	if err != nil {
		t.Fatal(err)
	}
	firstCommit := git2(t, repo, "rev-parse", "HEAD")
	if err := os.WriteFile(filepath.Join(repo, "second.txt"), []byte("second\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git2(t, repo, "add", "second.txt")
	git2(t, repo, "commit", "-m", "second")
	secondCommit := git2(t, repo, "rev-parse", "HEAD")
	task := SyncTask{
		WorkspaceID: gitCtx.WorkspaceID, RepoRoot: repo, ServerURL: "https://ae.example.com", AuthSubject: "user:1",
		RepoConfigID: 23, RepoKey: gitCtx.RepoKey, Status: SyncTaskStatusPending, LastRequestedAt: time.Now().UTC(),
	}
	hint := eventIDRepoHint(executionContextFromSyncTask(task))
	firstEvent, err := CheckpointEventID(hint, firstCommit)
	if err != nil {
		t.Fatal(err)
	}
	secondEvent, err := CheckpointEventID(hint, secondCommit)
	if err != nil {
		t.Fatal(err)
	}
	base := V2SyncTrigger{
		Kind: "post-commit", ServerURL: task.ServerURL, AuthSubject: task.AuthSubject, RepoConfigID: task.RepoConfigID,
		RepoKey: task.RepoKey, WorkspaceID: task.WorkspaceID, CapturedAt: time.Now().UTC(), RelayProviderID: 17,
	}
	first := base
	first.EventID, first.CommitSHA = firstEvent, firstCommit
	second := base
	second.EventID, second.CommitSHA, second.RepoKey = secondEvent, secondCommit, "github.com/acme/other"
	task.V2Triggers = []V2SyncTrigger{first, second}

	resolved, err := executionContextForSyncTask(task, executionContextFromSyncTask(task))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := resolved.runnableV2TriggerIDs[firstEvent]; !ok || len(resolved.runnableV2TriggerIDs) != 1 {
		t.Fatalf("runnable trigger IDs = %+v, want first only", resolved.runnableV2TriggerIDs)
	}
	var stageErr *syncTaskStageError
	if !errors.As(resolved.retainedV2TriggerError, &stageErr) || stageErr.reason != "repository checkout unavailable" {
		t.Fatalf("retained trigger error = %v", resolved.retainedV2TriggerError)
	}
}

func TestExecutionContextForSyncTaskRejectsCrossRepositoryRecovery(t *testing.T) {
	repo := initRepoWithCommit2(t)
	git2(t, repo, "remote", "set-url", "origin", "https://github.com/acme/other.git")
	seedGit, err := DetectGitContext(repo)
	if err != nil {
		t.Fatal(err)
	}
	task := SyncTask{
		WorkspaceID: "removed-workspace", RepoRoot: filepath.Join(t.TempDir(), "gone"), RepoConfigID: 23, RepoKey: "github.com/acme/repo",
		V2Triggers: []V2SyncTrigger{{Kind: "post-commit", EventID: "event-cross-repo", CommitSHA: git2(t, repo, "rev-parse", "HEAD"), RelayProviderID: 17}},
	}
	seed := ExecutionContext{RepoConfigID: 23, RepoKey: seedGit.RepoKey, WorkspaceID: seedGit.WorkspaceID, RepoRoot: repo}
	_, err = executionContextForSyncTask(task, seed)
	var stageErr *syncTaskStageError
	if !errors.As(err, &stageErr) || stageErr.stage != SyncTaskFailureStageLocalState || stageErr.reason != "repository checkout unavailable" {
		t.Fatalf("cross-repository recovery error = %v", err)
	}
}

func TestExecutionContextForSyncTaskRequiresFrozenProviderForRecovery(t *testing.T) {
	repo := initRepoWithCommit2(t)
	seedGit, err := DetectGitContext(repo)
	if err != nil {
		t.Fatal(err)
	}
	task := SyncTask{
		WorkspaceID: "removed-workspace", RepoRoot: filepath.Join(t.TempDir(), "gone"), RepoConfigID: 23, RepoKey: seedGit.RepoKey,
		V2Triggers: []V2SyncTrigger{{Kind: "post-commit", EventID: "event-no-provider", CommitSHA: git2(t, repo, "rev-parse", "HEAD")}},
	}
	seed := ExecutionContext{RepoConfigID: 23, RepoKey: seedGit.RepoKey, WorkspaceID: seedGit.WorkspaceID, RepoRoot: repo}
	_, err = executionContextForSyncTask(task, seed)
	var stageErr *syncTaskStageError
	if !errors.As(err, &stageErr) || stageErr.stage != SyncTaskFailureStageLocalState || stageErr.reason != "provider identity unavailable for recovery" {
		t.Fatalf("missing-provider recovery error = %v", err)
	}
}

type countingV2Uploader struct {
	*fakeUploader
	client *countingV2ClaimClient
}

func (u countingV2Uploader) V2ClaimClient() attributionlocal.V2ClaimBackendClient { return u.client }
func (u countingV2Uploader) RelayProviderID() int                                 { return 7 }

type conflictV2ClaimClient struct{}

func (conflictV2ClaimClient) SendAttributionV2Claims(_ context.Context, groups []client.AttributionV2ClaimGroup) (*client.AttributionV2ClaimBatchResult, error) {
	results := make([]client.AttributionV2ClaimResult, 0, len(groups))
	for _, group := range groups {
		results = append(results, client.AttributionV2ClaimResult{Group: client.AttributionV2ItemStatus{ID: group.GroupID, Status: "conflict", Error: "claim conflict"}})
	}
	return &client.AttributionV2ClaimBatchResult{LedgerEpoch: "shadow_v2", V1WritePolicy: "accept", Results: results}, nil
}

type conflictV2Uploader struct{ *fakeUploader }

func (u conflictV2Uploader) V2ClaimClient() attributionlocal.V2ClaimBackendClient {
	return conflictV2ClaimClient{}
}
func (u conflictV2Uploader) RelayProviderID() int { return 7 }

func TestRunV2ClaimSyncDoesNotUploadManualCommitWithoutCodexEvidence(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	backend := &countingV2ClaimClient{}
	task := &SyncTask{WorkspaceID: "ws-manual", V2Triggers: []V2SyncTrigger{{
		Kind: "post-commit", EventID: "event-manual", CommitSHA: "commit-manual", CapturedAt: time.Now().UTC(),
	}}}
	err := runV2ClaimSync(context.Background(), countingV2Uploader{fakeUploader: &fakeUploader{}, client: backend}, ExecutionContext{
		WorkspaceID: "ws-manual", RepoRoot: t.TempDir(), RepoConfigID: 9, RepoKey: "repo-host.example.com/org/repo",
	}, task, client.AttributionProtocol{LedgerEpoch: client.AttributionLedgerEpochShadowV2, V1WritePolicy: client.AttributionV1WritePolicyAccept})
	if err != nil {
		t.Fatal(err)
	}
	if backend.calls != 0 {
		t.Fatalf("claim uploads = %d, want 0", backend.calls)
	}
	progress, err := LoadV2ClaimScanProgress("ws-manual")
	if err != nil || progress != nil {
		t.Fatalf("completed scan progress = %+v, err=%v, want removed", progress, err)
	}
}

func TestRunV2ClaimSyncQuarantinesTerminalConflictWithoutBlockingTrigger(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	now := time.Now().UTC()
	if err := attributionlocal.SaveV2ClaimState(&attributionlocal.V2ClaimState{
		Version: 1, Claims: []attributionlocal.V2ClaimCandidate{{
			LocalKey: "local-conflict", FirstSeenAt: now, DeliveryStatus: attributionlocal.V2DeliveryConflict,
			LastDeliveryError: "checkpoint allocation conflict",
			Group:             client.AttributionV2ClaimGroup{GroupID: "group-conflict", RelayProviderID: 7, RequestIDs: []string{"req-conflict"}},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	backend := &countingV2ClaimClient{}
	execCtx := ExecutionContext{WorkspaceID: "ws-conflict", RepoRoot: t.TempDir(), RepoConfigID: 9, RepoKey: "repo-host.example.com/org/repo"}
	task := &SyncTask{WorkspaceID: execCtx.WorkspaceID, V2Triggers: []V2SyncTrigger{{
		Kind: "post-commit", EventID: "event-conflict", CommitSHA: "commit-conflict", CapturedAt: now,
	}}}
	protocol := client.AttributionProtocol{LedgerEpoch: client.AttributionLedgerEpochShadowV2, V1WritePolicy: client.AttributionV1WritePolicyAccept}
	if err := runV2ClaimSync(context.Background(), countingV2Uploader{fakeUploader: &fakeUploader{}, client: backend}, execCtx, task, protocol); err != nil {
		t.Fatal(err)
	}
	if backend.calls != 0 {
		t.Fatalf("terminal conflict uploads = %d, want 0", backend.calls)
	}
	if progress, err := LoadV2ClaimScanProgress(execCtx.WorkspaceID); err != nil || progress != nil {
		t.Fatalf("completed conflict scan progress = %+v, err=%v, want removed", progress, err)
	}
}

func TestRunV2ClaimSyncFinishesTriggerWhenBackendReturnsTerminalConflict(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	now := time.Now().UTC()
	if err := attributionlocal.SaveV2ClaimState(&attributionlocal.V2ClaimState{
		Version: 1, Claims: []attributionlocal.V2ClaimCandidate{{
			LocalKey: "local-conflict", FirstSeenAt: now,
			Group: client.AttributionV2ClaimGroup{GroupID: "group-conflict", RelayProviderID: 7, RequestIDs: []string{"req-conflict"}},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	execCtx := ExecutionContext{WorkspaceID: "ws-new-conflict", RepoRoot: t.TempDir(), RepoConfigID: 9, RepoKey: "repo-host.example.com/org/repo"}
	task := &SyncTask{WorkspaceID: execCtx.WorkspaceID, V2Triggers: []V2SyncTrigger{{
		Kind: "post-commit", EventID: "event-conflict", CommitSHA: "commit-conflict", CapturedAt: now,
	}}}
	protocol := client.AttributionProtocol{LedgerEpoch: client.AttributionLedgerEpochShadowV2, V1WritePolicy: client.AttributionV1WritePolicyAccept}
	if err := runV2ClaimSync(context.Background(), conflictV2Uploader{fakeUploader: &fakeUploader{}}, execCtx, task, protocol); err != nil {
		t.Fatal(err)
	}
	state, err := attributionlocal.LoadV2ClaimState()
	if err != nil || len(state.Claims) != 1 || state.Claims[0].DeliveryStatus != attributionlocal.V2DeliveryConflict || len(state.Claims[0].Group.RequestIDs) != 1 {
		t.Fatalf("quarantined conflict state = %+v, err=%v", state, err)
	}
	if progress, err := LoadV2ClaimScanProgress(execCtx.WorkspaceID); err != nil || progress != nil {
		t.Fatalf("completed conflict scan progress = %+v, err=%v, want removed", progress, err)
	}
}

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
	err := runV2ClaimSync(context.Background(), uploader, ExecutionContext{WorkspaceID: "ws-1"}, &SyncTask{}, client.AttributionProtocol{LedgerEpoch: client.AttributionLedgerEpochShadowV2, V1WritePolicy: client.AttributionV1WritePolicyAccept})
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

func TestRunV2ClaimSyncAddsOnlyNewTriggerUnitsAfterBackendFailure(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, name := range []string{"one.jsonl", "two.jsonl"} {
		path := filepath.Join(home, ".codex", "sessions", name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now().UTC()
	group := client.AttributionV2ClaimGroup{GroupID: "group-progress", RelayProviderID: 7, RequestIDs: []string{"req-progress"}}
	if err := attributionlocal.SaveV2ClaimState(&attributionlocal.V2ClaimState{
		Version: 1, Claims: []attributionlocal.V2ClaimCandidate{{LocalKey: "local-progress", Group: group, FirstSeenAt: now}},
	}); err != nil {
		t.Fatal(err)
	}
	uploader := failingV2Uploader{fakeUploader: &fakeUploader{}, client: failingV2ClaimClient{err: errors.New("backend unavailable")}}
	execCtx := ExecutionContext{WorkspaceID: "ws-progress-retry", RepoRoot: t.TempDir(), RepoConfigID: 9, RepoKey: "repo-host.example.com/org/repo"}
	task := &SyncTask{WorkspaceID: execCtx.WorkspaceID, V2Triggers: []V2SyncTrigger{{Kind: "post-commit", EventID: "event-a", CommitSHA: "commit-a", CapturedAt: now}}}
	protocol := client.AttributionProtocol{LedgerEpoch: client.AttributionLedgerEpochShadowV2, V1WritePolicy: client.AttributionV1WritePolicyAccept}
	if err := runV2ClaimSync(context.Background(), uploader, execCtx, task, protocol); err == nil {
		t.Fatal("first backend failure = nil")
	}
	first, err := LoadV2ClaimScanProgress(execCtx.WorkspaceID)
	if err != nil || first == nil || len(first.CompletedUnits) != 2 {
		t.Fatalf("first progress = %+v, err=%v", first, err)
	}
	task.V2Triggers = append(task.V2Triggers, V2SyncTrigger{Kind: "post-commit", EventID: "event-b", CommitSHA: "commit-b", CapturedAt: now.Add(time.Minute)})
	if err := runV2ClaimSync(context.Background(), uploader, execCtx, task, protocol); err == nil {
		t.Fatal("second backend failure = nil")
	}
	second, err := LoadV2ClaimScanProgress(execCtx.WorkspaceID)
	if err != nil || second == nil || len(second.CompletedUnits) != 4 {
		t.Fatalf("second progress = %+v, err=%v, want one new unit per source", second, err)
	}
}

func TestRunV2ClaimSyncDeliversCompletedSourceBeforeLaterSourceStops(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, name := range []string{"one.jsonl", "two.jsonl"} {
		path := filepath.Join(home, ".codex", "sessions", name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now().UTC()
	execCtx := ExecutionContext{WorkspaceID: "ws-incremental-delivery", RepoRoot: t.TempDir(), RepoConfigID: 9, RepoKey: "repo-host.example.com/org/repo"}
	task := &SyncTask{WorkspaceID: execCtx.WorkspaceID, V2Triggers: []V2SyncTrigger{{Kind: "post-commit", EventID: "event-a", CommitSHA: "commit-a", CapturedAt: now}}}
	protocol := client.AttributionProtocol{LedgerEpoch: client.AttributionLedgerEpochShadowV2, V1WritePolicy: client.AttributionV1WritePolicyAccept}
	backend := &acknowledgingV2ClaimClient{}
	original := scanCodexV2ClaimSource
	calls := 0
	scanCodexV2ClaimSource = func(_ *attributionlocal.CodexV2ClaimScan, _ context.Context, sourceKey string, _ []attributionlocal.V2ClaimScanOptions) ([]attributionlocal.V2ClaimCandidate, error) {
		calls++
		if calls == 2 {
			return nil, context.Canceled
		}
		return []attributionlocal.V2ClaimCandidate{{
			LocalKey: sourceKey, FirstSeenAt: now,
			Group: client.AttributionV2ClaimGroup{SchemaVersion: 2, GroupID: "group-incremental", RelayProviderID: 7, TokenSource: client.AttributionV2TokenSourceRelayOfficial, RequestIDs: []string{"request-synthetic"}, EvidenceDigest: "evidence-synthetic", CommitAllocations: []client.AttributionV2CommitAllocation{{Sequence: 1, RepoConfigID: 9, WorkspaceID: execCtx.WorkspaceID, CheckpointEventID: "event-a", CommitSHA: "commit-a", EvidenceDigest: "evidence-synthetic"}}},
		}}, nil
	}
	t.Cleanup(func() { scanCodexV2ClaimSource = original })
	incrementalUploader := acknowledgingV2Uploader{fakeUploader: &fakeUploader{}, client: backend}
	if err := runV2ClaimSync(context.Background(), incrementalUploader, execCtx, task, protocol); !errors.Is(err, context.Canceled) {
		t.Fatalf("interrupted incremental run = %v, want context.Canceled", err)
	}
	if backend.calls != 1 {
		t.Fatalf("incremental backend calls = %d, want first completed source delivered", backend.calls)
	}
}

func TestRunV2ClaimSyncResumesRemainingUnitsAfterSourceInterruption(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, name := range []string{"one.jsonl", "two.jsonl"} {
		path := filepath.Join(home, ".codex", "sessions", name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	execCtx := ExecutionContext{WorkspaceID: "ws-interrupted-scan", RepoRoot: t.TempDir(), RepoConfigID: 9, RepoKey: "repo-host.example.com/org/repo"}
	now := time.Now().UTC()
	task := &SyncTask{WorkspaceID: execCtx.WorkspaceID, V2Triggers: []V2SyncTrigger{
		{Kind: "post-commit", EventID: "event-a", CommitSHA: "commit-a", CapturedAt: now},
		{Kind: "post-commit", EventID: "event-b", CommitSHA: "commit-b", CapturedAt: now},
	}}
	protocol := client.AttributionProtocol{LedgerEpoch: client.AttributionLedgerEpochShadowV2, V1WritePolicy: client.AttributionV1WritePolicyAccept}
	backend := &countingV2ClaimClient{}
	uploader := countingV2Uploader{fakeUploader: &fakeUploader{}, client: backend}
	original := scanCodexV2ClaimSource
	calls := 0
	interrupt := true
	scanCodexV2ClaimSource = func(scan *attributionlocal.CodexV2ClaimScan, ctx context.Context, sourceKey string, options []attributionlocal.V2ClaimScanOptions) ([]attributionlocal.V2ClaimCandidate, error) {
		calls++
		if interrupt && calls == 2 {
			return nil, context.Canceled
		}
		return scan.ScanSource(ctx, sourceKey, options)
	}
	t.Cleanup(func() { scanCodexV2ClaimSource = original })

	if err := runV2ClaimSync(context.Background(), uploader, execCtx, task, protocol); !errors.Is(err, context.Canceled) {
		t.Fatalf("first interrupted run = %v, want context.Canceled", err)
	}
	first, err := LoadV2ClaimScanProgress(execCtx.WorkspaceID)
	if err != nil || first == nil || len(first.CompletedUnits) != 2 {
		t.Fatalf("interrupted progress = %+v, err=%v, want first source x two triggers", first, err)
	}
	interrupt = false
	if err := runV2ClaimSync(context.Background(), uploader, execCtx, task, protocol); err != nil {
		t.Fatal(err)
	}
	if calls != 3 {
		t.Fatalf("source calls across interruption and resume = %d, want two initial calls plus only one remaining source", calls)
	}
	if progress, err := LoadV2ClaimScanProgress(execCtx.WorkspaceID); err != nil || progress != nil {
		t.Fatalf("completed progress = %+v, err=%v, want removed", progress, err)
	}
}

func TestRunV2ClaimSyncDrainsLargeHomeAcrossSmallBudgets(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	sessions := filepath.Join(home, ".codex", "sessions")
	if err := os.MkdirAll(sessions, 0o700); err != nil {
		t.Fatal(err)
	}
	const sourceCount = 1800
	const triggerCount = 83
	for index := 0; index < sourceCount; index++ {
		path := filepath.Join(sessions, fmt.Sprintf("source-%04d.jsonl", index))
		if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now().UTC()
	execCtx := ExecutionContext{WorkspaceID: "ws-large-home", RepoRoot: t.TempDir(), RepoConfigID: 9, RepoKey: "repo-host.example.com/org/repo"}
	task := &SyncTask{WorkspaceID: execCtx.WorkspaceID, V2Triggers: make([]V2SyncTrigger, 0, triggerCount)}
	for index := 0; index < triggerCount; index++ {
		task.V2Triggers = append(task.V2Triggers, V2SyncTrigger{Kind: "post-commit", EventID: fmt.Sprintf("event-%02d", index), CommitSHA: fmt.Sprintf("commit-%02d", index), CapturedAt: now})
	}
	protocol := client.AttributionProtocol{LedgerEpoch: client.AttributionLedgerEpochShadowV2, V1WritePolicy: client.AttributionV1WritePolicyAccept}
	uploader := countingV2Uploader{fakeUploader: &fakeUploader{}, client: &countingV2ClaimClient{}}
	originalScan := scanCodexV2ClaimSource
	originalBatchSize := v2ClaimProgressBatchSize
	v2ClaimProgressBatchSize = 100
	budget := 0
	scanned := 0
	scanCodexV2ClaimSource = func(_ *attributionlocal.CodexV2ClaimScan, _ context.Context, _ string, _ []attributionlocal.V2ClaimScanOptions) ([]attributionlocal.V2ClaimCandidate, error) {
		if budget == 0 {
			return nil, context.DeadlineExceeded
		}
		budget--
		scanned++
		return nil, nil
	}
	t.Cleanup(func() {
		scanCodexV2ClaimSource = originalScan
		v2ClaimProgressBatchSize = originalBatchSize
	})

	completed := 0
	for attempt := 1; ; attempt++ {
		budget = 300
		err := runV2ClaimSync(context.Background(), uploader, execCtx, task, protocol)
		if err == nil {
			if attempt != 6 {
				t.Fatalf("large-home attempts = %d, want six bounded passes", attempt)
			}
			break
		}
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("large-home pass %d = %v", attempt, err)
		}
		progress, loadErr := LoadV2ClaimScanProgress(execCtx.WorkspaceID)
		if loadErr != nil || progress == nil {
			t.Fatalf("large-home progress after pass %d = %+v, %v", attempt, progress, loadErr)
		}
		if len(progress.CompletedUnits) <= completed {
			t.Fatalf("large-home progress stalled at %d units after pass %d", len(progress.CompletedUnits), attempt)
		}
		completed = len(progress.CompletedUnits)
	}
	if progress, err := LoadV2ClaimScanProgress(execCtx.WorkspaceID); err != nil || progress != nil {
		t.Fatalf("large-home final progress = %+v, %v, want removed after %d units", progress, err, sourceCount*triggerCount)
	}
	if scanned != sourceCount {
		t.Fatalf("large-home scanned sources = %d, want %d (%d completed units)", scanned, sourceCount, sourceCount*triggerCount)
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
