package hooks

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ai-efficiency/ae-cli/internal/attributionlocal"
)

func TestMachineMigrationQuarantinesOnlyExactSyntheticFixtureBacklog(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	now := time.Date(2026, 8, 25, 10, 0, 0, 0, time.UTC)
	base := SyncTask{
		ServerURL: "https://ae.example.com", AuthSubject: "user:1", RepoConfigID: 23,
		Status: SyncTaskStatusPending, LastRequestedAt: now, LastError: "repository unavailable",
		V2Triggers: []V2SyncTrigger{{Kind: "post-commit", EventID: "event-1", CommitSHA: "abc123", CapturedAt: now, RelayProviderID: 17}},
	}
	synthetic := base
	synthetic.WorkspaceID = "synthetic-workspace"
	synthetic.RepoRoot = filepath.Join(home, "deleted-TestFixture", "001")
	synthetic.RepoKey = "repo-host.example.com/org/repo"
	legitimate := base
	legitimate.WorkspaceID = "legitimate-workspace"
	legitimate.RepoRoot = filepath.Join(home, "deleted-real-worktree")
	legitimate.RepoKey = "github.com/acme/repo"
	otherOwner := synthetic
	otherOwner.WorkspaceID = "synthetic-other-owner"
	otherOwner.AuthSubject = "user:2"
	otherServer := synthetic
	otherServer.WorkspaceID = "synthetic-other-server"
	otherServer.ServerURL = "https://other-ae.example.com"
	for _, task := range []SyncTask{synthetic, legitimate, otherOwner, otherServer} {
		if err := SaveSyncTask(task); err != nil {
			t.Fatal(err)
		}
	}
	syntheticProgress := filepath.Join(attributionlocal.AttributionRootDir(), "workspaces", synthetic.WorkspaceID, "v2-claim-scan.json")
	if err := attributionlocal.SaveJSON(syntheticProgress, map[string]any{"version": 1}); err != nil {
		t.Fatal(err)
	}
	for _, event := range []UnresolvedHookEvent{
		{Kind: "post-commit", RemoteURL: "https://repo-host.example.com/org/repo.git", RepoKey: synthetic.RepoKey, WorkspaceID: synthetic.WorkspaceID, ServerURL: synthetic.ServerURL, AuthSubject: synthetic.AuthSubject, CommitSHA: "abc123", CapturedAt: now.Format(time.RFC3339)},
		{Kind: "post-commit", RemoteURL: "https://github.com/acme/repo.git", RepoKey: legitimate.RepoKey, WorkspaceID: legitimate.WorkspaceID, ServerURL: legitimate.ServerURL, AuthSubject: legitimate.AuthSubject, CommitSHA: "def456", CapturedAt: now.Format(time.RFC3339)},
		{Kind: "post-commit", RemoteURL: "https://repo-host.example.com/org/repo.git", RepoKey: otherOwner.RepoKey, WorkspaceID: otherOwner.WorkspaceID, ServerURL: otherOwner.ServerURL, AuthSubject: otherOwner.AuthSubject, CommitSHA: "owner456", CapturedAt: now.Format(time.RFC3339)},
		{Kind: "post-commit", RemoteURL: "https://repo-host.example.com/org/repo.git", RepoKey: otherServer.RepoKey, WorkspaceID: otherServer.WorkspaceID, ServerURL: otherServer.ServerURL, AuthSubject: otherServer.AuthSubject, CommitSHA: "server456", CapturedAt: now.Format(time.RFC3339)},
	} {
		if err := EnqueueUnresolvedHookEvent(event); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := MigrateMachineSyncBacklog(SyncTaskMigrationBinding{
		ServerURL: base.ServerURL, AuthSubject: base.AuthSubject, RelayProviderID: 17,
	}, now); err != nil {
		t.Fatal(err)
	}

	if task, err := LoadSyncTask(synthetic.WorkspaceID); err != nil || task != nil {
		t.Fatalf("synthetic active task = %+v, %v, want quarantined", task, err)
	}
	if task, err := LoadSyncTask(legitimate.WorkspaceID); err != nil || task == nil {
		t.Fatalf("legitimate active task = %+v, %v, want retained", task, err)
	}
	for _, retainedID := range []string{otherOwner.WorkspaceID, otherServer.WorkspaceID} {
		if task, err := LoadSyncTask(retainedID); err != nil || task == nil {
			t.Fatalf("other binding task %s = %+v, %v, want retained", retainedID, task, err)
		}
	}
	items, err := ListUnresolvedHookEvents()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 || items[0].RepoKey != legitimate.RepoKey || items[1].AuthSubject != otherOwner.AuthSubject || items[2].ServerURL != otherServer.ServerURL {
		t.Fatalf("active unresolved events = %+v, want non-matching bindings retained", items)
	}
	summary, err := LoadSyntheticFixtureQuarantineSummary()
	if err != nil {
		t.Fatal(err)
	}
	if summary.Workspaces != 1 || summary.UnresolvedEvents != 1 || !summary.MigratedAt.Equal(now) {
		t.Fatalf("quarantine summary = %+v", summary)
	}
	quarantinedProgress := filepath.Join(attributionlocal.AttributionRootDir(), "quarantine", "synthetic-git-fixtures", "workspaces", synthetic.WorkspaceID, "v2-claim-scan.json")
	if _, err := os.Stat(quarantinedProgress); err != nil {
		t.Fatalf("quarantined workspace detail: %v", err)
	}
	if _, err := os.Stat(filepath.Join(syntheticFixtureQuarantineRoot(), "migration.json")); !os.IsNotExist(err) {
		t.Fatalf("completed migration journal still exists: %v", err)
	}
}

func TestSyntheticFixtureQuarantineDefersToLiveMachineOwner(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	now := time.Date(2026, 8, 25, 10, 15, 0, 0, time.UTC)
	binding := SyncTaskMigrationBinding{ServerURL: "https://ae.example.com", AuthSubject: "user:1", RelayProviderID: 17}
	task := SyncTask{
		WorkspaceID: "synthetic-live-owner", RepoRoot: "/deleted/test-fixture", ServerURL: binding.ServerURL, AuthSubject: binding.AuthSubject,
		RepoConfigID: 23, RepoKey: syntheticFixtureRepoKey, Status: SyncTaskStatusPending, LastRequestedAt: now,
	}
	if err := SaveSyncTask(task); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(attributionlocal.AttributionRootDir(), "machine-sync.run.lock")
	if err := os.WriteFile(lockPath, []byte("4242\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	originalAlive := syncTaskRunnerAlive
	syncTaskRunnerAlive = func(pid int) bool { return pid == 4242 }
	t.Cleanup(func() { syncTaskRunnerAlive = originalAlive })

	summary, err := QuarantineSyntheticFixtureBacklog(binding, now)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Workspaces != 0 {
		t.Fatalf("quarantine ran under another live owner: %+v", summary)
	}
	if retained, err := LoadSyncTask(task.WorkspaceID); err != nil || retained == nil {
		t.Fatalf("live-owner task = %+v, %v, want retained", retained, err)
	}
	if err := os.Remove(lockPath); err != nil {
		t.Fatal(err)
	}
	summary, err = QuarantineSyntheticFixtureBacklog(binding, now)
	if err != nil || summary.Workspaces != 1 {
		t.Fatalf("quarantine after owner release = %+v, %v", summary, err)
	}
}

func TestSyntheticFixtureQuarantinePreservesOtherUnresolvedBytesAndIsIdempotent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	now := time.Date(2026, 8, 25, 10, 30, 0, 0, time.UTC)
	path := unresolvedQueuePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	legitimate := []byte("  {\"kind\":\"post-commit\",\"remote_url\":\"https://github.com/acme/repo.git\",\"repo_key\":\"github.com/acme/repo\",\"workspace_id\":\"real\",\"server_url\":\"https://ae.example.com\",\"auth_subject\":\"user:1\",\"commit_sha\":\"def456\"}  \n")
	corrupt := []byte("{not-json}\n")
	synthetic := []byte("{\"kind\":\"post-commit\",\"remote_url\":\"https://repo-host.example.com/org/repo.git\",\"repo_key\":\"repo-host.example.com/org/repo\",\"workspace_id\":\"fixture\",\"server_url\":\"https://ae.example.com\",\"auth_subject\":\"user:1\",\"commit_sha\":\"abc123\"}\n")
	original := append(append(append([]byte{}, legitimate...), corrupt...), synthetic...)
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}

	binding := SyncTaskMigrationBinding{ServerURL: "https://ae.example.com", AuthSubject: "user:1", RelayProviderID: 17}
	first, err := QuarantineSyntheticFixtureBacklog(binding, now)
	if err != nil {
		t.Fatal(err)
	}
	second, err := QuarantineSyntheticFixtureBacklog(binding, now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if first != second || first.UnresolvedEvents != 1 || !first.MigratedAt.Equal(now) {
		t.Fatalf("idempotent summaries = first %+v second %+v", first, second)
	}
	active, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	wantActive := append(append([]byte{}, legitimate...), corrupt...)
	if string(active) != string(wantActive) {
		t.Fatalf("active unresolved bytes = %q, want %q", active, wantActive)
	}
	audit, err := os.ReadFile(filepath.Join(syntheticFixtureQuarantineRoot(), "unresolved-hooks.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if string(audit) != string(synthetic) {
		t.Fatalf("synthetic audit = %q, want one event %q", audit, synthetic)
	}
}
