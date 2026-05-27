# ae-cli Post-Commit Async Attribution Sync Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `ae-cli` post-commit reporting durable and fast by moving full tool-usage sync out of the hook timeout into a workspace-level async runner with clear pending/error visibility.

**Architecture:** Keep checkpoint upload on the hook fast path, but replace inline `SyncEngine.Run(...)` with a workspace-scoped sync task plus opportunistic detached runner. Reuse the existing workspace hook queue, attribution `scan-state.json`, and `spool.json`; the new layer only manages when to run sync, how to prevent duplicate runners, and how to surface backlog/error state to users.

**Tech Stack:** Go CLI with Cobra commands, git hook helpers in `ae-cli/internal/hooks`, attribution scanner/uploader in `ae-cli/internal/attributionlocal`, git-based tests with `go test`, markdown architecture/spec docs.

**Status:** Complete with active follow-up. PR created: https://github.com/LichKing-2234/ai-efficiency/pull/59. Follow-ups on 2026-05-27 fixed linked-worktree Codex session matching after real local verification found checkpoint uploads without matching `/events` rows, bounded tool-usage upload / runner execution after a live manual sync exposed a stuck HTTPS connection, replayed already-spooled usage before cold scans after local `~/.codex/sessions` volume blocked backlog upload, preserved newest-first replay for newly scanned spool entries, compacted managed spool files, and added compatible batch ingest to reduce backlog upload round trips.

---

## File Map

- Create: `ae-cli/internal/hooks/sync_task.go`
  - Define workspace-level async sync task state, lease acquisition/release, status transitions, and disk paths under `~/.ae-cli/state/attribution/workspaces/<workspace_id>/`.
- Create: `ae-cli/internal/hooks/sync_task_test.go`
  - Cover coalescing, lease expiry, runner acquisition, and success/failure transitions.
- Create: `ae-cli/internal/hooks/background_runner.go`
  - Spawn detached `ae-cli hook background-sync` processes and expose a small runner API used by hook commands.
- Modify: `ae-cli/internal/client/client.go`
  - Bound managed tool-usage upload attempts and keep transient retry behavior from turning a stuck backend connection into a long-lived runner.
- Modify: `ae-cli/internal/hooks/handler.go`
  - Remove inline full sync from `PostCommitResolved`.
  - Upsert pending sync task after checkpoint handling.
  - Attempt background trigger when no active lease exists.
- Modify: `ae-cli/cmd/hook.go`
  - Add a hidden `background-sync` subcommand that runs outside the 10 second hook timeout.
  - Wire the task runner into `hook attribution-sync` and `hook post-commit`.
- Modify: `ae-cli/cmd/doctor.go`
  - Print workspace sync task status, timestamps, attempt count, and last error.
- Modify: `ae-cli/cmd/sync.go`
  - Make `ae-cli sync` consume pending workspace sync tasks and show the same task state through `sync status`.
- Modify: `ae-cli/cmd/hook_test.go`
  - Cover the new hook command path, detached runner trigger decisions, and bounded post-commit execution.
- Modify: `ae-cli/internal/hooks/handler_test.go`
  - Cover pending task creation, runner trigger skip when lease is active, and backlog warning conditions.
- Modify: `ae-cli/cmd/sync_test.go`
  - Cover `sync` / `sync status` behavior when tasks are pending, running, or failed.
- Modify: `docs/architecture.md`
  - Update the current-state architecture so hook fast path only does checkpoint + pending sync task, while full artifact scanning happens in the async runner.
- Reference: `docs/superpowers/specs/2026-05-26-ae-cli-post-commit-async-attribution-sync-design.md`
  - Source of truth for this plan’s contracts and scope.

---

### Task 1: Add Workspace Sync Task State and Lease Management

**Files:**
- Create: `ae-cli/internal/hooks/sync_task.go`
- Create: `ae-cli/internal/hooks/sync_task_test.go`

- [x] **Step 1: Write failing task-state tests**

Add tests to `ae-cli/internal/hooks/sync_task_test.go` for:

```go
func TestUpsertPendingSyncTaskCoalescesRequests(t *testing.T) {
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
}

func TestAcquireSyncTaskLeaseRejectsActiveRunnerAndAllowsExpiredLease(t *testing.T) {
	task := &SyncTask{
		WorkspaceID:     "ws-1",
		Status:          SyncTaskStatusRunning,
		RunnerPID:       1234,
		LeaseExpiresAt:  ptrTime(time.Now().Add(5 * time.Minute).UTC()),
		LastRequestedAt: time.Now().UTC(),
	}
	if ok, err := TryAcquireSyncTaskLease(task, 9999, time.Now().UTC(), 30*time.Second); err != nil || ok {
		t.Fatalf("TryAcquireSyncTaskLease(active) = %t, %v, want false, nil", ok, err)
	}

	task.LeaseExpiresAt = ptrTime(time.Now().Add(-1 * time.Minute).UTC())
	ok, err := TryAcquireSyncTaskLease(task, 9999, time.Now().UTC(), 30*time.Second)
	if err != nil || !ok {
		t.Fatalf("TryAcquireSyncTaskLease(expired) = %t, %v, want true, nil", ok, err)
	}
}
```

- [x] **Step 2: Run the new tests and verify RED**

Run:

```bash
cd ae-cli && go test ./internal/hooks -run 'TestUpsertPendingSyncTask|TestAcquireSyncTaskLease' -count=1
```

Expected: compile failures because `SyncTask`, `SaveSyncTask`, `UpsertPendingSyncTask`, `LoadSyncTask`, and `TryAcquireSyncTaskLease` do not exist yet.

- [x] **Step 3: Implement minimal sync-task model**

Create `ae-cli/internal/hooks/sync_task.go` with:

```go
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
	task.Status = SyncTaskStatusRunning
	task.RunnerPID = pid
	task.LastStartedAt = &now
	task.LeaseExpiresAt = &expires
	return true, SaveSyncTask(*task)
}

func MarkSyncTaskFailure(task *SyncTask, now time.Time, err error) error {
	task.Status = SyncTaskStatusPending
	task.AttemptCount++
	task.LastError = err.Error()
	task.RunnerPID = 0
	task.LeaseExpiresAt = nil
	return SaveSyncTask(*task)
}

func MarkSyncTaskSuccess(task *SyncTask, now time.Time) error {
	task.Status = SyncTaskStatusPending
	task.LastCompletedAt = &now
	task.LastError = ""
	task.RunnerPID = 0
	task.LeaseExpiresAt = nil
	return SaveSyncTask(*task)
}
```

- [x] **Step 4: Run the task-state tests and verify GREEN**

Run:

```bash
cd ae-cli && go test ./internal/hooks -run 'TestUpsertPendingSyncTask|TestAcquireSyncTaskLease' -count=1
```

Expected: PASS.

- [x] **Step 5: Commit the task-state foundation**

Run:

```bash
git add ae-cli/internal/hooks/sync_task.go ae-cli/internal/hooks/sync_task_test.go
git commit -m "feat(ae-cli): add async sync task state"
```

---

### Task 2: Move Post-Commit Usage Sync to a Detached Background Runner

**Files:**
- Create: `ae-cli/internal/hooks/background_runner.go`
- Modify: `ae-cli/internal/hooks/handler.go`
- Modify: `ae-cli/cmd/hook.go`
- Modify: `ae-cli/cmd/hook_test.go`
- Modify: `ae-cli/internal/hooks/handler_test.go`

- [x] **Step 1: Write failing hook/runner tests**

Add tests covering:

```go
func TestPostCommitResolvedCreatesPendingTaskInsteadOfInlineSync(t *testing.T) {
	repo := initRepoWithCommit2(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	execCtx := resolvedContextForRepo(t, repo)

	var syncCalled bool
	origRun := runAttributionSync
	runAttributionSync = func(ctx context.Context, opts attributionlocal.RunOptions, client attributionlocal.BackendClient) error {
		syncCalled = true
		return nil
	}
	t.Cleanup(func() { runAttributionSync = origRun })

	var spawned bool
	origSpawn := spawnBackgroundSyncRunner
	spawnBackgroundSyncRunner = func(repoRoot string) error {
		spawned = true
		return nil
	}
	t.Cleanup(func() { spawnBackgroundSyncRunner = origSpawn })

	h := NewHandler(syncCapableFakeUploader{&fakeUploader{}})
	if err := h.PostCommitResolved(context.Background(), execCtx); err != nil {
		t.Fatalf("PostCommitResolved: %v", err)
	}

	task, err := LoadSyncTask(execCtx.WorkspaceID)
	if err != nil || task == nil {
		t.Fatalf("LoadSyncTask = %+v, %v, want pending task", task, err)
	}
	if syncCalled {
		t.Fatal("expected inline attribution sync to be removed from post-commit path")
	}
	if !spawned {
		t.Fatal("expected detached runner trigger")
	}
}

func TestHookBackgroundSyncRunsWithoutHookTimeout(t *testing.T) {
	repo := initRepoWithCommitForCmdTests(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeTestToken(t, home, "user:123")
	writePositiveEligibility(t, home, "github.com/acme/repo", 123)

	origRun := hooks.RunPendingSyncTask
	var ctxErr error
	hooks.RunPendingSyncTask = func(ctx context.Context, execCtx hooks.ExecutionContext, backend attributionlocal.BackendClient) error {
		ctxErr = ctx.Err()
		return nil
	}
	t.Cleanup(func() { hooks.RunPendingSyncTask = origRun })

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	defer func() { _ = os.Chdir(wd) }()
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("Chdir(repo): %v", err)
	}

	if err := hookBackgroundSyncCmd.RunE(hookBackgroundSyncCmd, nil); err != nil {
		t.Fatalf("hook background-sync RunE: %v", err)
	}
	if ctxErr != nil {
		t.Fatalf("background sync context err = %v, want nil", ctxErr)
	}
}
```

- [x] **Step 2: Run the hook tests and verify RED**

Run:

```bash
cd ae-cli && go test ./cmd ./internal/hooks -run 'TestPostCommitResolvedCreatesPendingTaskInsteadOfInlineSync|TestHookBackgroundSyncRunsWithoutHookTimeout' -count=1
```

Expected: fail because `spawnBackgroundSyncRunner` and `hook background-sync` do not exist, and `PostCommitResolved` still calls inline sync.

- [x] **Step 3: Implement detached runner path**

Create `ae-cli/internal/hooks/background_runner.go`:

```go
var spawnBackgroundSyncRunner = func(repoRoot string) error {
	aeCLI, err := resolveManagedHookBinary()
	if err != nil {
		return err
	}
	devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	cmd := exec.Command(aeCLI, "hook", "background-sync")
	cmd.Dir = repoRoot
	cmd.Stdout = devNull
	cmd.Stderr = devNull
	cmd.Stdin = nil
	cmd.SysProcAttr = detachedProcessAttrs()
	return cmd.Start()
}
```

Modify `ae-cli/internal/hooks/handler.go` so `PostCommitResolved`:

```go
	if err := h.uploader.UploadHookEvent(ctx, ev); err != nil {
		_ = enqueueForReplay(execCtx, ev)
	}
	task := SyncTask{
		WorkspaceID:     workspaceID,
		RepoRoot:        repoRoot,
		ServerURL:       execCtx.ServerURL,
		AuthSubject:     execCtx.AuthSubject,
		RepoConfigID:    execCtx.RepoConfigID,
		RepoKey:         execCtx.RepoKey,
		Status:          SyncTaskStatusPending,
		LastRequestedAt: time.Now().UTC(),
	}
	_ = UpsertPendingSyncTask(task)
	if shouldStartSyncRunner(task) {
		_ = spawnBackgroundSyncRunner(repoRoot)
	}
	return nil
```

Modify `ae-cli/cmd/hook.go` to add a new hidden command:

```go
var hookBackgroundSyncCmd = &cobra.Command{
	Use:    "background-sync",
	Short:  "Run async attribution sync outside hook timeout (hidden)",
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, _ := os.Getwd()
		gitCtx, err := hooks.DetectGitContext(cwd)
		if err != nil {
			return nil
		}
		execCtx, ok := resolveHookExecutionContext(context.Background(), gitCtx)
		if !ok {
			return nil
		}
		return hooks.RunPendingSyncTask(context.Background(), execCtx, apiClient)
	},
}
```

- [x] **Step 4: Run the hook tests and verify GREEN**

Run:

```bash
cd ae-cli && go test ./cmd ./internal/hooks -run 'TestPostCommitResolvedCreatesPendingTaskInsteadOfInlineSync|TestHookBackgroundSyncRunsWithoutHookTimeout' -count=1
```

Expected: PASS.

- [x] **Step 5: Commit the detached runner change**

Run:

```bash
git add ae-cli/internal/hooks/background_runner.go ae-cli/internal/hooks/handler.go ae-cli/cmd/hook.go ae-cli/cmd/hook_test.go ae-cli/internal/hooks/handler_test.go
git commit -m "feat(ae-cli): move hook sync to async runner"
```

---

### Task 3: Surface Pending Sync and Errors in doctor, sync status, and Hook Warnings

**Files:**
- Modify: `ae-cli/cmd/doctor.go`
- Modify: `ae-cli/cmd/sync.go`
- Modify: `ae-cli/cmd/sync_test.go`
- Modify: `ae-cli/cmd/hook_test.go`
- Modify: `ae-cli/internal/hooks/handler.go`

- [x] **Step 1: Write failing visibility tests**

Add tests for:

```go
func TestDoctorPrintsPendingSyncTask(t *testing.T) {
	task := SyncTask{
		WorkspaceID:     "ws-1",
		Status:          SyncTaskStatusPending,
		LastRequestedAt: time.Date(2026, 5, 26, 9, 0, 0, 0, time.UTC),
		LastError:       "spawn failed",
		AttemptCount:    3,
	}
	if err := SaveSyncTask(task); err != nil {
		t.Fatalf("SaveSyncTask: %v", err)
	}
	buf := &bytes.Buffer{}
	doctorCmd.SetOut(buf)
	doctorCmd.SetErr(buf)
	if err := doctorCmd.RunE(doctorCmd, nil); err != nil {
		t.Fatalf("doctorCmd.RunE: %v", err)
	}
	output := buf.String()
	for _, want := range []string{"Sync Task: pending", "spawn failed", "attempt_count: 3"} {
		if !strings.Contains(output, want) {
			t.Fatalf("doctor output missing %q:\n%s", want, output)
		}
	}
}

func TestSyncStatusPrintsRunningTask(t *testing.T) {
	now := time.Date(2026, 5, 26, 9, 30, 0, 0, time.UTC)
	task := SyncTask{
		WorkspaceID:     "ws-1",
		Status:          SyncTaskStatusRunning,
		LastRequestedAt: now.Add(-5 * time.Minute),
		LastStartedAt:   &now,
		RunnerPID:       4321,
	}
	if err := SaveSyncTask(task); err != nil {
		t.Fatalf("SaveSyncTask: %v", err)
	}
	buf := &bytes.Buffer{}
	syncStatusCmd.SetOut(buf)
	syncStatusCmd.SetErr(buf)
	if err := syncStatusCmd.RunE(syncStatusCmd, nil); err != nil {
		t.Fatalf("syncStatusCmd.RunE: %v", err)
	}
	output := buf.String()
	for _, want := range []string{"Sync Task: running", "runner_pid: 4321"} {
		if !strings.Contains(output, want) {
			t.Fatalf("sync status output missing %q:\n%s", want, output)
		}
	}
}
```

- [x] **Step 2: Run the visibility tests and verify RED**

Run:

```bash
cd ae-cli && go test ./cmd -run 'TestDoctorPrintsPendingSyncTask|TestSyncStatusPrintsRunningTask' -count=1
```

Expected: fail because `doctor` and `sync status` do not print any sync-task state yet.

- [x] **Step 3: Implement CLI status printing and warning conditions**

Add a helper used by both commands:

```go
func printSyncTaskStatus(out io.Writer, task *hooks.SyncTask) {
	if task == nil {
		fmt.Fprintln(out, "Sync Task: none")
		return
	}
	fmt.Fprintf(out, "Sync Task: %s\n", task.Status)
	fmt.Fprintf(out, "  last_requested_at: %s\n", task.LastRequestedAt.UTC().Format(time.RFC3339))
	if task.LastStartedAt != nil {
		fmt.Fprintf(out, "  last_started_at: %s\n", task.LastStartedAt.UTC().Format(time.RFC3339))
	}
	if task.LastCompletedAt != nil {
		fmt.Fprintf(out, "  last_completed_at: %s\n", task.LastCompletedAt.UTC().Format(time.RFC3339))
	}
	fmt.Fprintf(out, "  attempt_count: %d\n", task.AttemptCount)
	if strings.TrimSpace(task.LastError) != "" {
		fmt.Fprintf(out, "  last_error: %s\n", task.LastError)
	}
}
```

Update `doctor.go` and `sync.go` to load the current workspace task and print it.

Update `PostCommitResolved` warning logic:

```go
	if strings.TrimSpace(task.LastError) != "" || spawnErr != nil {
		fmt.Fprintln(os.Stderr, "ae-cli: attribution sync pending for this repo; run 'ae-cli doctor' for details")
	}
```

- [x] **Step 4: Run the visibility tests and verify GREEN**

Run:

```bash
cd ae-cli && go test ./cmd -run 'TestDoctorPrintsPendingSyncTask|TestSyncStatusPrintsRunningTask' -count=1
```

Expected: PASS.

- [x] **Step 5: Commit the CLI visibility work**

Run:

```bash
git add ae-cli/cmd/doctor.go ae-cli/cmd/sync.go ae-cli/cmd/sync_test.go ae-cli/cmd/hook_test.go ae-cli/internal/hooks/handler.go
git commit -m "fix(ae-cli): expose async sync backlog status"
```

---

### Task 4: Update Architecture Docs, Run Full Verification, and Prepare the PR

**Files:**
- Modify: `docs/architecture.md`
- Modify: `docs/superpowers/plans/2026-05-26-ae-cli-post-commit-async-attribution-sync.md`

- [x] **Step 1: Update architecture doc to match the new current state**

In `docs/architecture.md`, add or update the current-state runtime description to say:

```md
- `post-commit` uploads or queues commit checkpoints on the hook fast path.
- The hook also marks the workspace as needing attribution sync and opportunistically starts an async runner.
- Full `Codex` / `Claude` / `Kiro` artifact scanning and `tool_usage_events` upload happen in the async runner or later manual `ae-cli sync`, not inside the hook timeout.
```

- [x] **Step 2: Mark completed plan steps as you finish them**

As each task above lands, update this plan file in the same commit wave:

```md
**Status:** In progress. Completed tasks: Task 1, Task 2. Remaining: Task 3, Task 4 verification, PR creation.
```

And flip each finished checkbox from `- [ ]` to `- [x]`.

- [x] **Step 3: Run focused ae-cli tests**

Run:

```bash
cd ae-cli && go test ./cmd ./internal/hooks ./internal/attributionlocal -count=1
```

Expected: PASS.

- [x] **Step 4: Run repo-level default ae-cli tests**

Run:

```bash
cd ae-cli && go test ./... -count=1
```

Expected: PASS.

- [x] **Step 5: Request code review before PR**

Use the `superpowers:requesting-code-review` workflow against the implementation range:

```bash
BASE_SHA=$(git merge-base HEAD origin/main)
HEAD_SHA=$(git rev-parse HEAD)
```

Reviewer context:

```text
DESCRIPTION: Async post-commit attribution sync with workspace tasks, detached runner, and backlog visibility
PLAN_OR_REQUIREMENTS: docs/superpowers/specs/2026-05-26-ae-cli-post-commit-async-attribution-sync-design.md and this implementation plan
BASE_SHA: $BASE_SHA
HEAD_SHA: $HEAD_SHA
```

- [x] **Step 6: Fix review findings and re-run verification**

After applying any valid review feedback, rerun:

```bash
cd ae-cli && go test ./cmd ./internal/hooks ./internal/attributionlocal -count=1
cd ae-cli && go test ./... -count=1
```

Expected: PASS on both commands.

- [x] **Step 7: Push branch and create PR**

Run:

```bash
git push -u origin codex/async-post-commit-attribution-sync
gh pr create --title "fix(ae-cli): move post-commit attribution sync to async runner" --body "$(cat <<'EOF'
## Summary
- move post-commit usage sync out of the hook timeout into a workspace async runner
- add durable workspace sync task state, lease handling, and backlog visibility
- update architecture docs to match the new async sync contract

## Test Plan
- [x] cd ae-cli && go test ./cmd ./internal/hooks ./internal/attributionlocal -count=1
- [x] cd ae-cli && go test ./... -count=1
EOF
)"
```

---

### Task 5: Follow-up Linked Worktree Codex Session Matching

**Files:**
- Modify: `ae-cli/internal/attributionlocal/codex_jsonl.go`
- Modify: `ae-cli/internal/attributionlocal/workspace_path.go`
- Modify: `ae-cli/internal/attributionlocal/scanner.go`
- Modify: `ae-cli/internal/attributionlocal/scanner_test.go`
- Modify: `docs/architecture.md`
- Modify: `docs/superpowers/specs/2026-05-13-sessionless-local-tool-attribution-design.md`
- Modify: `docs/superpowers/specs/2026-05-26-ae-cli-post-commit-async-attribution-sync-design.md`

- [x] **Step 1: Diagnose real missing `/events` rows after linked-worktree commits**

Verified:

```text
~/.local/bin/ae-cli was built after the earlier commits, so those commits used the old installed binary.
The current Codex JSONL session_meta.cwd was /Users/admin/ai-efficiency while commits happened under /Users/admin/ai-efficiency/.worktrees/async-post-commit-attribution-sync.
The backend /api/v1/events window for 2026-05-27 returned total=0, while the local checkpoint upload ledger showed only a checkpoint upload.
```

- [x] **Step 2: Add linked-worktree Codex session matching**

Codex session matching now accepts exact workspace paths and paths that resolve to the same Git common dir. The uploaded event still uses the current hook workspace ID, preserving checkpoint binding for the linked worktree.

- [x] **Step 3: Add regression coverage**

Added `TestScanner_MatchesCodexSessionFromLinkedWorktreeCommonDir`, covering a real linked worktree where `session_meta.cwd` points at the main checkout and sync runs from the linked worktree.

- [x] **Step 4: Update current contract docs**

Updated the sessionless attribution spec, async sync spec, and architecture overview to document Codex linked-worktree matching.

- [x] **Step 5: Run verification**

Run:

```bash
cd ae-cli && go test ./internal/attributionlocal -run 'TestScanner_MatchesCodexSessionFromLinkedWorktreeCommonDir|TestScanner_UsesCodexSQLiteBeforeJSONLFallback|TestParseCodexJSONL_MatchesCanonicalEquivalentWorkspacePath' -count=1
cd ae-cli && go test ./... -count=1
```

Expected: PASS.

---

### Task 6: Follow-up Stale Runner Lease Recovery

**Files:**
- Modify: `ae-cli/internal/hooks/sync_task.go`
- Modify: `ae-cli/internal/hooks/sync_task_test.go`
- Modify: `ae-cli/cmd/doctor.go`
- Modify: `ae-cli/cmd/sync.go`
- Modify: `ae-cli/cmd/sync_test.go`
- Modify: `docs/architecture.md`
- Modify: `docs/superpowers/specs/2026-05-26-ae-cli-post-commit-async-attribution-sync-design.md`

- [x] **Step 1: Diagnose stale running task after real hook-triggered commit**

The real post-commit runner process exited, but `sync-task.json` still showed `status=running`, `runner_pid=26704`, and a one-hour `lease_expires_at`. `ps -p 26704` showed no live process, so manual sync would incorrectly report an active runner until lease expiry.

- [x] **Step 2: Treat a lease as active only while the runner process is alive**

`SyncTask.HasActiveLease` now checks `runner_pid` liveness in addition to status and lease expiry. Dead runner recovery clears `runner_pid` and `lease_expires_at`, returns the task to pending, and records `last_error`.

- [x] **Step 3: Surface recovery through diagnostics**

`ae-cli doctor` and `ae-cli sync status` now print `Sync Task: inactive runner recovered` when they repair a stale running task.

- [x] **Step 4: Add regression coverage**

Added tests for dead lease recovery, active runner preservation, sync status recovery, and active runner reporting with a live PID.

- [x] **Step 5: Run verification**

Run:

```bash
cd ae-cli && go test ./internal/hooks -run 'TestRecoverInactiveSyncTaskRunnerClearsDeadLease|TestAcquireSyncTaskLeaseRejectsActiveRunnerAndAllowsExpiredLease|TestAcquireSyncTaskLeaseAllowsOnlyOneConcurrentRunner|TestMarkSyncTaskFailureDoesNotClearDifferentActiveRunner' -count=1
cd ae-cli && go test ./cmd -run 'TestSyncStatusPrintsRunningTask|TestSyncStatusRecoversInactiveRunner|TestSyncCommandReportsActiveRunnerWithoutRunningSync' -count=1
```

Expected: PASS.

---

### Task 7: Follow-up Codex Websocket SQLite Usage Parsing

**Files:**
- Modify: `ae-cli/internal/attributionlocal/codex_sqlite.go`
- Modify: `ae-cli/internal/attributionlocal/codex_sqlite_test.go`
- Modify: `docs/architecture.md`
- Modify: `docs/superpowers/specs/2026-05-13-sessionless-local-tool-attribution-design.md`

- [x] **Step 1: Diagnose zero events after successful foreground sync**

After linked-worktree matching was fixed, `ae-cli sync` completed and advanced the Codex sqlite watermark, but `/api/v1/events` for 2026-05-27 still returned `total=0`. Inspecting `~/.codex/logs_2.sqlite` showed current Codex rows use websocket JSON payloads: `websocket event: {"type":"response.completed","response":{"id":"...","usage":{...}}}`. The existing parser only recognized old text fields like `response.id=` and `input_token_count=`.

- [x] **Step 2: Add websocket response.completed parser support**

`parseCodexCompletedLine` now accepts websocket JSON payloads, extracting response id, input/output/cached/reasoning tokens, and timestamp while using `conversation.id` or `thread_id` as the tool session id.

- [x] **Step 3: Add regression coverage**

Added `TestParseCodexSQLite_ExtractsWebsocketResponseCompletedUsage`.

- [x] **Step 4: Update current contract docs**

Updated the sessionless attribution spec and architecture overview to document both old text counters and new websocket JSON payloads.

---

### Task 8: Follow-up Transient Tool-Usage Upload Retries

**Files:**
- Modify: `ae-cli/internal/client/client.go`
- Modify: `ae-cli/internal/client/client_test.go`
- Modify: `docs/architecture.md`
- Modify: `docs/superpowers/specs/2026-05-26-ae-cli-post-commit-async-attribution-sync-design.md`

- [x] **Step 1: Diagnose retryable backend upload failures**

After websocket parsing produced events, foreground `ae-cli sync` failed on `/api/v1/tool-usage-events` with a transient `502`. Posting the first failed spooled event manually returned `201`, and retrying sync advanced the spool before hitting another `502`.

- [x] **Step 2: Add short retry for transient upload statuses**

`SendToolUsageEvent` now retries transient 429/502/503/504 responses before returning an error and leaving the remaining events in spool.

- [x] **Step 3: Add regression coverage**

Added client tests for retrying a 502 followed by success and for not retrying validation errors.

- [x] **Step 4: Update current contract docs**

Updated the async sync spec and architecture overview to document transient upload retry behavior.

---

### Task 9: Follow-up Runtime Boundaries for Stuck Uploads

**Files:**
- Modify: `ae-cli/internal/client/client.go`
- Modify: `ae-cli/internal/client/client_test.go`
- Modify: `ae-cli/internal/hooks/background_runner.go`
- Modify: `docs/architecture.md`
- Modify: `docs/superpowers/specs/2026-05-26-ae-cli-post-commit-async-attribution-sync-design.md`

- [x] **Step 1: Diagnose long-running manual sync**

A live `ae-cli sync` used one runner PID and no meaningful CPU, but stayed connected to the backend HTTPS endpoint for several minutes. This was not a process fan-out problem; it showed the upload path needed explicit per-request and runner-level runtime bounds.

- [x] **Step 2: Bound upload attempts and runner execution**

`SendToolUsageEvent` now applies a short timeout to each managed tool-usage upload attempt, and `RunPendingSyncTask` wraps the whole runner in a total timeout. Timeout failures keep the existing pending/spool recovery semantics.

- [x] **Step 3: Add regression coverage**

Added a client test that verifies a slow tool-usage upload attempt returns quickly instead of waiting indefinitely.

- [x] **Step 4: Run verification**

Run:

```bash
cd ae-cli && go test ./internal/client ./internal/hooks ./cmd -count=1
cd ae-cli && go test ./... -count=1
```

Expected: PASS.

---

### Task 10: Follow-up Preserve Pending State When New Events Spool

**Files:**
- Modify: `ae-cli/internal/attributionlocal/sync.go`
- Modify: `ae-cli/internal/attributionlocal/sync_test.go`
- Modify: `docs/architecture.md`
- Modify: `docs/superpowers/specs/2026-05-26-ae-cli-post-commit-async-attribution-sync-design.md`

- [x] **Step 1: Diagnose false success with remaining spool**

After a live sync uploaded additional events, `/events` advanced but `spool.json` still contained remaining tool-usage events while `sync-task.json` had been deleted. The cause was `SyncEngine.Run`: when a new scanned event upload failed, it spooled the remaining events and saved scan state, but returned nil to the runner.

- [x] **Step 2: Return upload failure after spooling**

`SyncEngine.Run` and the legacy path now persist spool and scan state, then return the upload error. The runner therefore marks the sync task failed/pending instead of deleting it as successful.

- [x] **Step 3: Add regression coverage**

Updated the existing new-event upload failure test to require an error while still verifying that remaining events are spooled and scan state is saved.

- [x] **Step 4: Run verification**

Run:

```bash
cd ae-cli && go test ./internal/attributionlocal ./internal/client ./internal/hooks ./cmd -count=1
cd ae-cli && go test ./... -count=1
```

Expected: PASS.

---

### Task 11: Follow-up Prioritize Fresh Usage Ahead of Backlog

**Files:**
- Modify: `ae-cli/internal/attributionlocal/sync.go`
- Modify: `ae-cli/internal/attributionlocal/sync_test.go`
- Modify: `docs/architecture.md`
- Modify: `docs/superpowers/specs/2026-05-26-ae-cli-post-commit-async-attribution-sync-design.md`

- [x] **Step 1: Diagnose fresh commit blocked behind historical spool**

A new empty commit `44a57cd` refreshed `sync-task.json`, and the scanner wrote post-commit Codex usage into `spool.json`, but `/events` for the post-commit window still returned zero because replay processed old backlog first and the runner hit its timeout before reaching the fresh events.

- [x] **Step 2: Make durable sync newest-first**

Durable sync now writes current scan results into spool before replaying, then sorts spooled events by `observed_end_at` descending during replay. Fresh usage is attempted before historical backlog while preserving spool recovery semantics.

- [x] **Step 3: Add regression coverage**

Added tests that replay uploads newest spooled events before older failing backlog, and that `Run` uploads the current scan before an older backlog failure.

- [x] **Step 4: Run verification**

Run:

```bash
cd ae-cli && go test ./internal/attributionlocal -count=1
cd ae-cli && go test ./... -count=1
```

Expected: PASS.

---

### Task 12: Follow-up Batch Upload Historical Tool-Usage Backlog

**Files:**
- Modify: `backend/internal/toolusage/service.go`
- Modify: `backend/internal/handler/tool_usage.go`
- Modify: `backend/internal/handler/router.go`
- Modify: `backend/internal/handler/tool_usage_test.go`
- Modify: `ae-cli/internal/client/client.go`
- Modify: `ae-cli/internal/client/client_test.go`
- Modify: `ae-cli/internal/attributionlocal/sync.go`
- Modify: `ae-cli/internal/attributionlocal/sync_test.go`
- Modify: `docs/architecture.md`
- Modify: `docs/superpowers/specs/2026-05-26-ae-cli-post-commit-async-attribution-sync-design.md`

- [x] **Step 1: Diagnose remaining backlog timeout**

Live verification showed fresh post-commit events were now visible in `/events`, but `spool.json` still had a historical backlog and `sync-task.json` retained `last_error=context deadline exceeded`. The remaining bottleneck was upload throughput: replay still spent one HTTPS request per tool-usage event.

- [x] **Step 2: Add compatible batch ingest**

Added `POST /api/v1/tool-usage-events/batch` while keeping the existing single-event endpoint. The backend accepts bounded batches, resolves scope once per workspace/repo context, prechecks duplicate `dedupe_key` values, reuses checkpoint lookup per group, and keeps duplicate events idempotent.

- [x] **Step 3: Use batch replay from ae-cli**

`SyncEngine` now detects clients that support `SendToolUsageEvents` and uploads replay candidates in bounded chunks, preserving newest-first order. The CLI client falls back to the single-event endpoint when a backend does not support batch ingest or when a batch validation failure needs isolation.

- [x] **Step 4: Add targeted regression coverage**

Added client coverage for batch upload and fallback, sync replay coverage for batch-capable clients, and backend handler coverage for batch create plus duplicate idempotency.

- [x] **Step 5: Run full verification**

Run:

```bash
cd ae-cli && go test ./... -count=1
cd backend && AE_TEST_POSTGRES_DSN='postgres://postgres:postgres@127.0.0.1:15432/postgres?sslmode=disable' go test ./... -count=1
```

Expected: PASS.
