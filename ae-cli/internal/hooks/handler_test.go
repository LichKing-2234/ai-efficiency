package hooks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ai-efficiency/ae-cli/internal/attributionlocal"
	"github.com/ai-efficiency/ae-cli/internal/client"
	"github.com/ai-efficiency/ae-cli/internal/collector"
	"github.com/ai-efficiency/ae-cli/internal/session"
)

type fakeUploader struct {
	err    error
	events []HookEvent
	onCall func()
}

func (f *fakeUploader) UploadHookEvent(ctx context.Context, ev HookEvent) error {
	if f.onCall != nil {
		f.onCall()
	}
	f.events = append(f.events, ev)
	return f.err
}

type noopToolUsageClient struct{}

func (noopToolUsageClient) SendToolUsageEvent(ctx context.Context, req client.ToolUsageEventRequest) error {
	return nil
}

type syncCapableFakeUploader struct {
	*fakeUploader
}

func (s syncCapableFakeUploader) ToolUsageClient() attributionlocal.BackendClient {
	return noopToolUsageClient{}
}

type recordingBackendHookClient struct {
	checkpoints []client.CommitCheckpointRequest
	toolUsage   []client.ToolUsageEventRequest
	order       []string
}

func (r *recordingBackendHookClient) SendCommitCheckpoint(ctx context.Context, req client.CommitCheckpointRequest) error {
	r.order = append(r.order, "checkpoint")
	r.checkpoints = append(r.checkpoints, req)
	return nil
}

func (r *recordingBackendHookClient) SendCommitRewrite(ctx context.Context, req client.CommitRewriteRequest) error {
	return nil
}

func (r *recordingBackendHookClient) SendToolUsageEvent(ctx context.Context, req client.ToolUsageEventRequest) error {
	r.order = append(r.order, "tool_usage")
	r.toolUsage = append(r.toolUsage, req)
	return nil
}

func git2(t *testing.T, dir string, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("git %s failed: %v\nstdout=%s\nstderr=%s", strings.Join(args, " "), err, stdout.String(), stderr.String())
	}
	return strings.TrimSpace(stdout.String())
}

func initRepoWithCommit2(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	git2(t, dir, "init", "--template=/dev/null")
	git2(t, dir, "config", "user.email", "alice@example.com")
	git2(t, dir, "config", "user.name", "alice")
	git2(t, dir, "config", "core.hooksPath", filepath.Join(dir, ".git", "test-hooks-empty"))
	git2(t, dir, "remote", "add", "origin", "https://github.com/acme/repo.git")
	if err := os.MkdirAll(filepath.Join(dir, ".git", "test-hooks-empty"), 0o755); err != nil {
		t.Fatalf("mkdir test hooks dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	git2(t, dir, "add", ".")
	git2(t, dir, "commit", "-m", "init")
	return dir
}

func writeCollectorFixtures(t *testing.T, workspaceRoot string) (string, string, string) {
	t.Helper()

	dir := t.TempDir()
	codex := filepath.Join(dir, "codex.jsonl")
	claude := filepath.Join(dir, "claude.jsonl")
	kiro := filepath.Join(dir, "kiro.json")

	codexBody := `{"timestamp":"2026-03-27T09:00:00Z","type":"session_meta","payload":{"id":"codex-sess-1","cwd":"` + workspaceRoot + `"}}
{"timestamp":"2026-03-27T09:05:00Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":1200,"cached_input_tokens":300,"output_tokens":250,"reasoning_output_tokens":80,"total_tokens":1450}}}}`
	if err := os.WriteFile(codex, []byte(codexBody), 0o600); err != nil {
		t.Fatalf("write codex fixture: %v", err)
	}

	claudeBody := `{"type":"assistant","cwd":"` + workspaceRoot + `","sessionId":"claude-sess-1","message":{"usage":{"input_tokens":500,"output_tokens":120,"cache_creation_input_tokens":40,"cache_read_input_tokens":25}}}
{"type":"assistant","cwd":"` + workspaceRoot + `","sessionId":"claude-sess-1","message":{"usage":{"input_tokens":600,"output_tokens":140,"cache_creation_input_tokens":10,"cache_read_input_tokens":15}}}`
	if err := os.WriteFile(claude, []byte(claudeBody), 0o600); err != nil {
		t.Fatalf("write claude fixture: %v", err)
	}

	kiroBody := `{"session_id":"kiro-sess-1","cwd":"` + workspaceRoot + `","session_state":{"rts_model_state":{"conversation_id":"conv-kiro-1","context_usage_percentage":47.5}}}`
	if err := os.WriteFile(kiro, []byte(kiroBody), 0o600); err != nil {
		t.Fatalf("write kiro fixture: %v", err)
	}

	return codex, claude, kiro
}

func resolvedContextForRepo(t *testing.T, repo string) ExecutionContext {
	t.Helper()
	repoRoot := git2(t, repo, "rev-parse", "--show-toplevel")
	gitDir := git2(t, repo, "rev-parse", "--absolute-git-dir")
	gitCommon := git2(t, repo, "rev-parse", "--git-common-dir")
	if !filepath.IsAbs(gitCommon) {
		gitCommon = filepath.Join(repoRoot, gitCommon)
	}
	workspaceID, err := session.DeriveWorkspaceID(repoRoot, repoRoot, gitDir, gitCommon)
	if err != nil {
		t.Fatalf("DeriveWorkspaceID: %v", err)
	}
	return ExecutionContext{
		ServerURL:     "https://ae.example.com",
		AuthSubject:   "user:123",
		RepoConfigID:  123,
		RepoKey:       "github.com/acme/repo",
		RepoFullName:  "acme/repo",
		WorkspaceID:   workspaceID,
		RepoRoot:      repoRoot,
		Branch:        "main",
		DurableReplay: true,
	}
}

func TestPostCommitWrapperUsesGitContextAndDoesNotCreateMarker(t *testing.T) {
	repo := initRepoWithCommit2(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("AE_SESSION_ID", "legacy-session")

	u := &fakeUploader{}
	h := NewHandler(u)
	if err := h.PostCommit(context.Background(), repo); err != nil {
		t.Fatalf("PostCommit: %v", err)
	}

	if len(u.events) != 1 {
		t.Fatalf("uploaded events = %d, want 1", len(u.events))
	}
	ev := u.events[0]
	if ev.BindingSource != "unbound" {
		t.Fatalf("binding_source = %q, want unbound", ev.BindingSource)
	}
	if ev.SessionID != "" {
		t.Fatalf("session_id = %q, want empty", ev.SessionID)
	}
	if strings.TrimSpace(ev.WorkspaceID) == "" {
		t.Fatalf("workspace_id is empty")
	}
	if _, err := os.Stat(filepath.Join(repo, "."+"ae")); !os.IsNotExist(err) {
		t.Fatalf("legacy marker directory should not be created, stat err=%v", err)
	}
}

func TestPostCommitResolvedQueuesOnlyWithStableBinding(t *testing.T) {
	repo := initRepoWithCommit2(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	origSpawn := spawnBackgroundSyncRunner
	spawnBackgroundSyncRunner = func(repoRoot string) error { return nil }
	t.Cleanup(func() { spawnBackgroundSyncRunner = origSpawn })

	execCtx := resolvedContextForRepo(t, repo)
	u := &fakeUploader{err: errors.New("upload failed")}
	h := NewHandler(u)
	if err := h.PostCommitResolved(context.Background(), execCtx); err != nil {
		t.Fatalf("PostCommitResolved should fail-open, got: %v", err)
	}

	q, err := NewWorkspaceQueue(execCtx.WorkspaceID)
	if err != nil {
		t.Fatalf("NewWorkspaceQueue: %v", err)
	}
	items, err := q.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("queued items = %d, want 1", len(items))
	}
	ev := items[0].Event
	if ev.ServerURL != execCtx.ServerURL || ev.AuthSubject != execCtx.AuthSubject || ev.RepoConfigID != execCtx.RepoConfigID || ev.RepoKey != execCtx.RepoKey {
		t.Fatalf("queued event missing binding context: %+v", ev)
	}

	unstable := execCtx
	unstable.AuthSubject = ""
	unstable.WorkspaceID = execCtx.WorkspaceID + "-unstable"
	if err := h.PostCommitResolved(context.Background(), unstable); err != nil {
		t.Fatalf("PostCommitResolved unstable: %v", err)
	}
	q2, err := NewWorkspaceQueue(unstable.WorkspaceID)
	if err != nil {
		t.Fatalf("NewWorkspaceQueue unstable: %v", err)
	}
	items, err = q2.List()
	if err != nil {
		t.Fatalf("List unstable: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("unstable queued items = %d, want 0", len(items))
	}
}

func TestPostCommitResolvedReportsQueueFailure(t *testing.T) {
	repo := initRepoWithCommit2(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	execCtx := resolvedContextForRepo(t, repo)

	workspaceDir := filepath.Join(attributionlocal.AttributionRootDir(), "workspaces", execCtx.WorkspaceID)
	if err := os.MkdirAll(workspaceDir, 0o700); err != nil {
		t.Fatalf("mkdir workspace dir: %v", err)
	}
	queuePath, err := workspaceQueuePath(execCtx.WorkspaceID)
	if err != nil {
		t.Fatalf("workspaceQueuePath: %v", err)
	}
	if err := os.MkdirAll(queuePath, 0o700); err != nil {
		t.Fatalf("mkdir queue path as directory: %v", err)
	}

	var stderr bytes.Buffer
	oldStderr := hookStderr
	hookStderr = &stderr
	t.Cleanup(func() { hookStderr = oldStderr })

	u := &fakeUploader{err: errors.New("upload failed")}
	if err := NewHandler(u).PostCommitResolved(context.Background(), execCtx); err != nil {
		t.Fatalf("PostCommitResolved: %v", err)
	}
	if !strings.Contains(stderr.String(), "failed to queue checkpoint event") {
		t.Fatalf("stderr = %q, want queue failure warning", stderr.String())
	}
}

func TestFlushUnresolvedResolvedUploadsMatchingEventAndRemovesIt(t *testing.T) {
	repo := initRepoWithCommit2(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	execCtx := resolvedContextForRepo(t, repo)

	if err := EnqueueUnresolvedHookEvent(UnresolvedHookEvent{
		Kind:           "post-commit",
		RemoteURL:      "https://github.com/acme/repo.git",
		RepoKey:        execCtx.RepoKey,
		WorkspaceID:    execCtx.WorkspaceID,
		ServerURL:      execCtx.ServerURL,
		AuthSubject:    execCtx.AuthSubject,
		CommitSHA:      "abc123",
		BranchSnapshot: "main",
		HeadSnapshot:   "abc123",
		CapturedAt:     "2026-06-02T09:00:00Z",
	}); err != nil {
		t.Fatalf("EnqueueUnresolvedHookEvent: %v", err)
	}

	u := &fakeUploader{}
	if err := NewHandler(u).FlushUnresolvedResolved(context.Background(), execCtx); err != nil {
		t.Fatalf("FlushUnresolvedResolved: %v", err)
	}
	if len(u.events) != 1 || u.events[0].CommitSHA != "abc123" || u.events[0].RepoConfigID != execCtx.RepoConfigID {
		t.Fatalf("uploaded events = %+v, want resolved checkpoint upload", u.events)
	}
	items, err := ListUnresolvedHookEvents()
	if err != nil {
		t.Fatalf("ListUnresolvedHookEvents: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("remaining unresolved items = %+v, want none", items)
	}
}

func TestFlushUnresolvedResolvedKeepsEventWithoutStableStoredBinding(t *testing.T) {
	repo := initRepoWithCommit2(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	execCtx := resolvedContextForRepo(t, repo)

	if err := EnqueueUnresolvedHookEvent(UnresolvedHookEvent{
		Kind:           "post-commit",
		RemoteURL:      "https://github.com/acme/repo.git",
		RepoKey:        execCtx.RepoKey,
		WorkspaceID:    execCtx.WorkspaceID,
		CommitSHA:      "abc123",
		BranchSnapshot: "main",
		HeadSnapshot:   "abc123",
		CapturedAt:     "2026-06-02T09:00:00Z",
	}); err != nil {
		t.Fatalf("EnqueueUnresolvedHookEvent: %v", err)
	}

	u := &fakeUploader{}
	if err := NewHandler(u).FlushUnresolvedResolved(context.Background(), execCtx); err != nil {
		t.Fatalf("FlushUnresolvedResolved: %v", err)
	}
	if len(u.events) != 0 {
		t.Fatalf("uploaded events = %+v, want none for unresolved event without stored server/auth binding", u.events)
	}
	items, err := ListUnresolvedHookEvents()
	if err != nil {
		t.Fatalf("ListUnresolvedHookEvents: %v", err)
	}
	if len(items) != 1 || items[0].CommitSHA != "abc123" {
		t.Fatalf("remaining unresolved items = %+v, want original event preserved", items)
	}
}

func TestFlushUnresolvedResolvedUploadsMatchingRewriteEventAndRemovesIt(t *testing.T) {
	repo := initRepoWithCommit2(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	execCtx := resolvedContextForRepo(t, repo)

	if err := EnqueueUnresolvedHookEvent(UnresolvedHookEvent{
		Kind:         "post-rewrite",
		RemoteURL:    "https://github.com/acme/repo.git",
		RepoKey:      execCtx.RepoKey,
		WorkspaceID:  execCtx.WorkspaceID,
		ServerURL:    execCtx.ServerURL,
		AuthSubject:  execCtx.AuthSubject,
		RewriteType:  "amend",
		OldCommitSHA: "oldsha1",
		NewCommitSHA: "newsha1",
		CapturedAt:   "2026-06-02T09:00:00Z",
	}); err != nil {
		t.Fatalf("EnqueueUnresolvedHookEvent: %v", err)
	}

	u := &fakeUploader{}
	if err := NewHandler(u).FlushUnresolvedResolved(context.Background(), execCtx); err != nil {
		t.Fatalf("FlushUnresolvedResolved: %v", err)
	}
	if len(u.events) != 1 {
		t.Fatalf("uploaded events = %+v, want resolved rewrite upload", u.events)
	}
	ev := u.events[0]
	if ev.Kind != "post-rewrite" || ev.RewriteType != "amend" || ev.OldCommitSHA != "oldsha1" || ev.NewCommitSHA != "newsha1" || ev.RepoConfigID != execCtx.RepoConfigID {
		t.Fatalf("uploaded rewrite event = %+v, want resolved rewrite context", ev)
	}
	wantID, err := RewriteEventID("repo_config_id:123", "oldsha1", "newsha1", "amend")
	if err != nil {
		t.Fatalf("RewriteEventID: %v", err)
	}
	if ev.EventID != wantID {
		t.Fatalf("event_id = %q, want %q", ev.EventID, wantID)
	}
	items, err := ListUnresolvedHookEvents()
	if err != nil {
		t.Fatalf("ListUnresolvedHookEvents: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("remaining unresolved items = %+v, want none", items)
	}
}

func TestPostCommitResolvedLeavesQueuedEventsForAsyncRunner(t *testing.T) {
	repo := initRepoWithCommit2(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	execCtx := resolvedContextForRepo(t, repo)
	origSpawn := spawnBackgroundSyncRunner
	spawnBackgroundSyncRunner = func(repoRoot string) error { return nil }
	t.Cleanup(func() { spawnBackgroundSyncRunner = origSpawn })

	q, err := NewWorkspaceQueue(execCtx.WorkspaceID)
	if err != nil {
		t.Fatalf("NewWorkspaceQueue: %v", err)
	}
	oldEventID, err := CheckpointEventID("repo_config_id:123", "old-sha")
	if err != nil {
		t.Fatalf("CheckpointEventID: %v", err)
	}
	if err := q.Enqueue(HookEvent{
		Kind:         "post-commit",
		EventID:      oldEventID,
		WorkspaceID:  execCtx.WorkspaceID,
		ServerURL:    execCtx.ServerURL,
		AuthSubject:  execCtx.AuthSubject,
		RepoConfigID: execCtx.RepoConfigID,
		RepoKey:      execCtx.RepoKey,
		RepoFullName: "acme/repo",
		CommitSHA:    "old-sha",
	}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	u := &fakeUploader{}
	h := NewHandler(u)
	if err := h.PostCommitResolved(context.Background(), execCtx); err != nil {
		t.Fatalf("PostCommitResolved: %v", err)
	}

	if len(u.events) != 1 {
		b, _ := json.Marshal(u.events)
		t.Fatalf("uploaded events = %d, want 1; events=%s", len(u.events), string(b))
	}
	head := git2(t, repo, "rev-parse", "HEAD")
	if got := u.events[0].CommitSHA; got != head {
		t.Fatalf("uploaded commit = %q, want current head %q", got, head)
	}
	items, err := q.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 1 || items[0].Event.CommitSHA != "old-sha" {
		t.Fatalf("queued items after post-commit = %+v, want old queued event preserved", items)
	}
}

func TestFlushResolvedSkipsContextMismatchAndWritesLedger(t *testing.T) {
	repo := initRepoWithCommit2(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	execCtx := resolvedContextForRepo(t, repo)

	q, err := NewWorkspaceQueue(execCtx.WorkspaceID)
	if err != nil {
		t.Fatalf("NewWorkspaceQueue: %v", err)
	}
	if err := q.Enqueue(HookEvent{
		Kind:         "post-commit",
		EventID:      "evt-mismatch",
		WorkspaceID:  execCtx.WorkspaceID,
		ServerURL:    "https://other.example.com",
		AuthSubject:  execCtx.AuthSubject,
		RepoConfigID: execCtx.RepoConfigID,
		RepoKey:      execCtx.RepoKey,
		CommitSHA:    "old-sha",
	}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	h := NewHandler(&fakeUploader{})
	if err := h.FlushResolved(context.Background(), execCtx); err != nil {
		t.Fatalf("FlushResolved: %v", err)
	}
	items, err := q.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 1 || items[0].Event.EventID != "evt-mismatch" {
		t.Fatalf("items after mismatch defer = %+v, want mismatched event preserved", items)
	}
	records, err := ReadLedger(execCtx.WorkspaceID)
	if err != nil {
		t.Fatalf("ReadLedger: %v", err)
	}
	if len(records) != 1 || records[0].Status != "deferred" || records[0].DedupeKey != "evt-mismatch" {
		t.Fatalf("ledger records = %+v, want deferred mismatch", records)
	}
}

func TestFlushResolvedDoesNotDropConcurrentEnqueueDuringUpload(t *testing.T) {
	repo := initRepoWithCommit2(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	execCtx := resolvedContextForRepo(t, repo)

	q, err := NewWorkspaceQueue(execCtx.WorkspaceID)
	if err != nil {
		t.Fatalf("NewWorkspaceQueue: %v", err)
	}
	if err := q.Enqueue(HookEvent{
		Kind:         "post-commit",
		EventID:      "evt-first",
		WorkspaceID:  execCtx.WorkspaceID,
		ServerURL:    execCtx.ServerURL,
		AuthSubject:  execCtx.AuthSubject,
		RepoConfigID: execCtx.RepoConfigID,
		RepoKey:      execCtx.RepoKey,
		CommitSHA:    "first",
	}); err != nil {
		t.Fatalf("Enqueue(first): %v", err)
	}

	var enqueueErr error
	u := &fakeUploader{onCall: func() {
		enqueueErr = q.Enqueue(HookEvent{
			Kind:         "post-commit",
			EventID:      "evt-second",
			WorkspaceID:  execCtx.WorkspaceID,
			ServerURL:    execCtx.ServerURL,
			AuthSubject:  execCtx.AuthSubject,
			RepoConfigID: execCtx.RepoConfigID,
			RepoKey:      execCtx.RepoKey,
			CommitSHA:    "second",
		})
	}}
	if err := NewHandler(u).FlushResolved(context.Background(), execCtx); err != nil {
		t.Fatalf("FlushResolved: %v", err)
	}
	if enqueueErr != nil {
		t.Fatalf("concurrent enqueue during upload: %v", enqueueErr)
	}
	items, err := q.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 1 || items[0].Event.EventID != "evt-second" {
		t.Fatalf("queue after flush = %+v, want concurrent second event preserved", items)
	}
}

func TestPostRewriteResolvedQueuesEventsWhenUploadFails(t *testing.T) {
	repo := initRepoWithCommit2(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	execCtx := resolvedContextForRepo(t, repo)

	u := &fakeUploader{err: errors.New("upload failed")}
	h := NewHandler(u)
	stdin := strings.NewReader("oldsha1 newsha1\n")
	if err := h.PostRewriteResolved(context.Background(), execCtx, "amend", stdin); err != nil {
		t.Fatalf("PostRewriteResolved should be fail-open, got: %v", err)
	}

	q, err := NewWorkspaceQueue(execCtx.WorkspaceID)
	if err != nil {
		t.Fatalf("NewWorkspaceQueue: %v", err)
	}
	items, err := q.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("queued items = %d, want 1", len(items))
	}
	ev := items[0].Event
	if ev.Kind != "post-rewrite" || ev.RewriteType != "amend" || ev.OldCommitSHA != "oldsha1" || ev.NewCommitSHA != "newsha1" {
		b, _ := json.Marshal(ev)
		t.Fatalf("queued rewrite fields mismatch: %s", string(b))
	}
	wantID, err := RewriteEventID("repo_config_id:123", "oldsha1", "newsha1", "amend")
	if err != nil {
		t.Fatalf("RewriteEventID: %v", err)
	}
	if ev.EventID != wantID {
		t.Fatalf("queued event_id = %q, want %q", ev.EventID, wantID)
	}
}

func TestPostRewriteResolvedCreatesPendingSyncTaskWhenUploadFails(t *testing.T) {
	repo := initRepoWithCommit2(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	execCtx := resolvedContextForRepo(t, repo)

	origSpawn := spawnBackgroundSyncRunner
	spawned := false
	spawnBackgroundSyncRunner = func(repoRoot string) error {
		spawned = true
		return nil
	}
	t.Cleanup(func() { spawnBackgroundSyncRunner = origSpawn })

	u := syncCapableFakeUploader{fakeUploader: &fakeUploader{err: errors.New("rewrite upload failed")}}
	if err := NewHandler(u).PostRewriteResolved(context.Background(), execCtx, "amend", strings.NewReader("oldsha1 newsha1\n")); err != nil {
		t.Fatalf("PostRewriteResolved: %v", err)
	}
	task, err := LoadSyncTask(execCtx.WorkspaceID)
	if err != nil {
		t.Fatalf("LoadSyncTask: %v", err)
	}
	if task == nil || task.Status != SyncTaskStatusPending {
		t.Fatalf("task = %+v, want pending task", task)
	}
	if !spawned {
		t.Fatalf("background sync runner was not spawned")
	}
}

func TestPostCommitSetsRepoConfigScopedEventID(t *testing.T) {
	repo := initRepoWithCommit2(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	origSpawn := spawnBackgroundSyncRunner
	spawnBackgroundSyncRunner = func(repoRoot string) error { return nil }
	t.Cleanup(func() { spawnBackgroundSyncRunner = origSpawn })
	execCtx := resolvedContextForRepo(t, repo)

	u := &fakeUploader{}
	h := NewHandler(u)
	if err := h.PostCommitResolved(context.Background(), execCtx); err != nil {
		t.Fatalf("PostCommitResolved: %v", err)
	}

	if len(u.events) != 1 {
		t.Fatalf("uploaded events = %d, want 1", len(u.events))
	}
	head := git2(t, repo, "rev-parse", "HEAD")
	wantID, err := CheckpointEventID("repo_config_id:123", head)
	if err != nil {
		t.Fatalf("CheckpointEventID: %v", err)
	}
	if got := u.events[0].EventID; got != wantID {
		t.Fatalf("uploaded event_id = %q, want %q", got, wantID)
	}
}

func TestPostCommitWithBackendUploaderCreatesPendingTaskAfterCheckpointUpload(t *testing.T) {
	repo := initRepoWithCommit2(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	execCtx := resolvedContextForRepo(t, repo)

	origSpawn := spawnBackgroundSyncRunner
	spawnBackgroundSyncRunner = func(repoRoot string) error { return nil }
	t.Cleanup(func() { spawnBackgroundSyncRunner = origSpawn })

	clientStub := &recordingBackendHookClient{}
	h := NewHandler(NewBackendUploader(clientStub))
	if err := h.PostCommitResolved(context.Background(), execCtx); err != nil {
		t.Fatalf("PostCommitResolved: %v", err)
	}

	if len(clientStub.checkpoints) != 1 {
		t.Fatalf("checkpoint uploads = %d, want 1", len(clientStub.checkpoints))
	}
	if clientStub.checkpoints[0].RepoConfigID != 123 {
		t.Fatalf("checkpoint repo_config_id = %d, want 123", clientStub.checkpoints[0].RepoConfigID)
	}
	if len(clientStub.toolUsage) != 0 {
		t.Fatalf("tool usage uploads = %d, want 0 during post-commit fast path", len(clientStub.toolUsage))
	}
	if len(clientStub.order) != 1 || clientStub.order[0] != "checkpoint" {
		t.Fatalf("upload order = %v, want only checkpoint on post-commit fast path", clientStub.order)
	}
	task, err := LoadSyncTask(execCtx.WorkspaceID)
	if err != nil {
		t.Fatalf("LoadSyncTask: %v", err)
	}
	if task == nil || task.Status != SyncTaskStatusPending {
		t.Fatalf("task = %+v, want pending sync task", task)
	}
}

func TestPostCommitTriggersBackgroundRunnerAfterUpload(t *testing.T) {
	repo := initRepoWithCommit2(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	execCtx := resolvedContextForRepo(t, repo)

	spawned := false
	origSpawn := spawnBackgroundSyncRunner
	spawnBackgroundSyncRunner = func(repoRoot string) error {
		spawned = true
		if repoRoot != execCtx.RepoRoot {
			t.Fatalf("repoRoot = %q, want %q", repoRoot, execCtx.RepoRoot)
		}
		return nil
	}
	t.Cleanup(func() { spawnBackgroundSyncRunner = origSpawn })

	u := &fakeUploader{
		onCall: func() {
			if spawned {
				t.Fatal("expected checkpoint upload before background runner trigger")
			}
		},
	}
	h := NewHandler(syncCapableFakeUploader{fakeUploader: u})
	if err := h.PostCommitResolved(context.Background(), execCtx); err != nil {
		t.Fatalf("PostCommitResolved: %v", err)
	}
	if !spawned {
		t.Fatal("expected background runner trigger")
	}
}

func TestPostCommitThrottlesBackgroundRunnerSpawnAttempts(t *testing.T) {
	repo := initRepoWithCommit2(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	execCtx := resolvedContextForRepo(t, repo)

	var spawnCount int
	origSpawn := spawnBackgroundSyncRunner
	spawnBackgroundSyncRunner = func(repoRoot string) error {
		spawnCount++
		return nil
	}
	t.Cleanup(func() { spawnBackgroundSyncRunner = origSpawn })

	h := NewHandler(syncCapableFakeUploader{fakeUploader: &fakeUploader{}})
	if err := h.PostCommitResolved(context.Background(), execCtx); err != nil {
		t.Fatalf("first PostCommitResolved: %v", err)
	}
	if err := h.PostCommitResolved(context.Background(), execCtx); err != nil {
		t.Fatalf("second PostCommitResolved: %v", err)
	}
	if spawnCount != 1 {
		t.Fatalf("spawn count = %d, want 1", spawnCount)
	}
}

func TestPostCommitDoesNotTriggerRunnerWhenLeaseIsActive(t *testing.T) {
	repo := initRepoWithCommit2(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	stubSyncTaskRunnerAlive(t, func(pid int) bool { return pid == 2222 })
	execCtx := resolvedContextForRepo(t, repo)

	now := time.Now().UTC()
	if err := SaveSyncTask(SyncTask{
		WorkspaceID:     execCtx.WorkspaceID,
		RepoRoot:        execCtx.RepoRoot,
		ServerURL:       execCtx.ServerURL,
		AuthSubject:     execCtx.AuthSubject,
		RepoConfigID:    execCtx.RepoConfigID,
		RepoKey:         execCtx.RepoKey,
		Status:          SyncTaskStatusRunning,
		LastRequestedAt: now.Add(-1 * time.Minute),
		LastStartedAt:   ptrTime(now),
		RunnerPID:       2222,
		LeaseExpiresAt:  ptrTime(now.Add(5 * time.Minute)),
	}); err != nil {
		t.Fatalf("SaveSyncTask: %v", err)
	}

	spawned := false
	origSpawn := spawnBackgroundSyncRunner
	spawnBackgroundSyncRunner = func(repoRoot string) error {
		spawned = true
		return nil
	}
	t.Cleanup(func() { spawnBackgroundSyncRunner = origSpawn })

	h := NewHandler(syncCapableFakeUploader{fakeUploader: &fakeUploader{}})
	if err := h.PostCommitResolved(context.Background(), execCtx); err != nil {
		t.Fatalf("PostCommitResolved: %v", err)
	}
	if spawned {
		t.Fatal("expected active lease to suppress background runner trigger")
	}
}

func TestPostCommitWarnsWhenBackgroundRunnerSpawnFails(t *testing.T) {
	repo := initRepoWithCommit2(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	execCtx := resolvedContextForRepo(t, repo)

	origSpawn := spawnBackgroundSyncRunner
	spawnBackgroundSyncRunner = func(repoRoot string) error {
		return errors.New("spawn failed")
	}
	t.Cleanup(func() { spawnBackgroundSyncRunner = origSpawn })

	var stderr bytes.Buffer
	oldStderr := hookStderr
	hookStderr = &stderr
	t.Cleanup(func() { hookStderr = oldStderr })

	h := NewHandler(syncCapableFakeUploader{fakeUploader: &fakeUploader{}})
	if err := h.PostCommitResolved(context.Background(), execCtx); err != nil {
		t.Fatalf("PostCommitResolved: %v", err)
	}
	if !strings.Contains(stderr.String(), "ae-cli: attribution sync pending for this repo; run 'ae-cli doctor' for details") {
		t.Fatalf("stderr = %q, want backlog warning", stderr.String())
	}
}

func TestPostCommitFailsOpenOutsideGitRepo(t *testing.T) {
	h := NewHandler(&fakeUploader{})
	if err := h.PostCommit(context.Background(), t.TempDir()); err != nil {
		t.Fatalf("PostCommit outside git repo should fail-open, got: %v", err)
	}
}

func TestPostRewriteFailsOpenOutsideGitRepo(t *testing.T) {
	h := NewHandler(&fakeUploader{})
	if err := h.PostRewrite(context.Background(), t.TempDir(), "amend", strings.NewReader("oldsha1 newsha1\n")); err != nil {
		t.Fatalf("PostRewrite outside git repo should fail-open, got: %v", err)
	}
}

func TestPostCommitAttachesCollectorSnapshotAndWritesWorkspaceCache(t *testing.T) {
	repo := initRepoWithCommit2(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	execCtx := resolvedContextForRepo(t, repo)
	origSpawn := spawnBackgroundSyncRunner
	spawnBackgroundSyncRunner = func(repoRoot string) error { return nil }
	t.Cleanup(func() { spawnBackgroundSyncRunner = origSpawn })

	workspaceRoot := git2(t, repo, "rev-parse", "--show-toplevel")
	codex, claude, kiro := writeCollectorFixtures(t, workspaceRoot)
	t.Setenv("AE_CODEX_SESSION_FILES", codex)
	t.Setenv("AE_CLAUDE_SESSION_FILES", claude)
	t.Setenv("AE_KIRO_SESSION_FILES", kiro)

	u := &fakeUploader{}
	h := NewHandler(u)
	if err := h.PostCommitResolved(context.Background(), execCtx); err != nil {
		t.Fatalf("PostCommitResolved: %v", err)
	}

	if len(u.events) != 1 {
		t.Fatalf("uploaded events = %d, want 1", len(u.events))
	}
	if u.events[0].AgentSnapshot == nil {
		t.Fatalf("uploaded agent snapshot is nil")
	}
	codexSnapshot, _ := u.events[0].AgentSnapshot["codex"].(map[string]any)
	if got := codexSnapshot["source_session_id"]; got != "codex-sess-1" {
		t.Fatalf("codex source_session_id = %v, want codex-sess-1", got)
	}

	cacheFile := filepath.Join(attributionlocal.AttributionRootDir(), "workspaces", execCtx.WorkspaceID, "collectors", "latest.json")
	data, err := os.ReadFile(cacheFile)
	if err != nil {
		t.Fatalf("read cache file: %v", err)
	}
	if !strings.Contains(string(data), "\"conversation_id\": \"conv-kiro-1\"") {
		t.Fatalf("cache file missing kiro snapshot: %s", string(data))
	}
}

func TestPostCommitPreservesCollectorCacheWhenNoSnapshot(t *testing.T) {
	repo := initRepoWithCommit2(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	execCtx := resolvedContextForRepo(t, repo)
	origSpawn := spawnBackgroundSyncRunner
	spawnBackgroundSyncRunner = func(repoRoot string) error { return nil }
	t.Cleanup(func() { spawnBackgroundSyncRunner = origSpawn })

	original := &collector.Snapshot{
		Codex: &collector.CodexSnapshot{SourceSessionID: "codex-prev", TotalTokens: 999},
	}
	if err := collector.WriteWorkspaceCache(execCtx.WorkspaceID, original); err != nil {
		t.Fatalf("WriteWorkspaceCache: %v", err)
	}

	u := &fakeUploader{}
	h := NewHandler(u)
	if err := h.PostCommitResolved(context.Background(), execCtx); err != nil {
		t.Fatalf("PostCommitResolved: %v", err)
	}

	cacheFile := filepath.Join(attributionlocal.AttributionRootDir(), "workspaces", execCtx.WorkspaceID, "collectors", "latest.json")
	data, err := os.ReadFile(cacheFile)
	if err != nil {
		t.Fatalf("read cache file: %v", err)
	}
	if !strings.Contains(string(data), "\"source_session_id\": \"codex-prev\"") {
		t.Fatalf("cache file was unexpectedly overwritten: %s", string(data))
	}
}
