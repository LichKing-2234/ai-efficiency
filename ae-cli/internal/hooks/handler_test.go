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

type recordingBackendHookClient struct {
	checkpoints []client.CommitCheckpointRequest
	toolUsage   []client.ToolUsageEventRequest
	order       []string
}

func (r *recordingBackendHookClient) EnsureRepoFromRemote(ctx context.Context, remoteURL, branch string) (*client.RepoEnsureResponse, error) {
	r.order = append(r.order, "ensure_repo")
	return &client.RepoEnsureResponse{
		ID:            1,
		RepoKey:       "github.com/acme/repo",
		FullName:      remoteURL,
		DefaultBranch: branch,
		BindingState:  "unbound",
	}, nil
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

func writeMarker(t *testing.T, repo, sessionID string) {
	t.Helper()
	gitDir := git2(t, repo, "rev-parse", "--absolute-git-dir")
	gitCommonDir := git2(t, repo, "rev-parse", "--git-common-dir")
	workspaceID, err := session.DeriveWorkspaceID(repo, repo, gitDir, filepath.Join(repo, gitCommonDir))
	if err != nil {
		t.Fatalf("DeriveWorkspaceID: %v", err)
	}
	if err := session.WriteMarker(repo, &session.Marker{SessionID: sessionID, WorkspaceID: workspaceID, RepoFullName: "origin"}); err != nil {
		t.Fatalf("WriteMarker: %v", err)
	}
}

func git2(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("git %s failed: %v\nstderr=%s", strings.Join(args, " "), err, stderr.String())
	}
	return strings.TrimSpace(stdout.String())
}

func initRepoWithCommit2(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	git2(t, dir, "init")
	git2(t, dir, "config", "user.email", "t@example.com")
	git2(t, dir, "config", "user.name", "t")
	git2(t, dir, "remote", "add", "origin", "https://github.com/acme/repo.git")
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

func TestPostCommitBootstrapsMarkerFromEnv(t *testing.T) {
	repo := initRepoWithCommit2(t)

	home := t.TempDir()
	t.Setenv("HOME", home)

	// Marker doesn't exist yet.
	_ = os.RemoveAll(filepath.Join(repo, ".ae"))

	t.Setenv("AE_SESSION_ID", "sess-env-1")
	t.Setenv("AE_RUNTIME_REF", "rt-1")
	t.Setenv("AE_RELAY_API_KEY_ID", "42")
	t.Setenv("AE_PROVIDER_NAME", "relay")

	u := &fakeUploader{}
	h := NewHandler(u)

	if err := h.PostCommit(context.Background(), repo); err != nil {
		t.Fatalf("PostCommit: %v", err)
	}

	m, err := session.ReadMarker(repo)
	if err != nil {
		t.Fatalf("ReadMarker: %v", err)
	}
	if got, want := m.SessionID, "sess-env-1"; got != want {
		t.Fatalf("marker.session_id = %q, want %q", got, want)
	}

	// Env bootstrap should also ensure /.ae is ignored.
	gitCommon := git2(t, repo, "rev-parse", "--git-common-dir")
	excludePath := filepath.Join(repo, gitCommon, "info", "exclude")
	b, err := os.ReadFile(excludePath)
	if err != nil {
		t.Fatalf("read exclude: %v", err)
	}
	if !strings.Contains(string(b), "/.ae/") {
		t.Fatalf("exclude missing /.ae/ pattern, got:\n%s", string(b))
	}
}

func TestPostCommitQueuesEventWhenUploadFails(t *testing.T) {
	repo := initRepoWithCommit2(t)

	home := t.TempDir()
	t.Setenv("HOME", home)

	writeMarker(t, repo, "sess-1")

	u := &fakeUploader{err: errors.New("upload failed")}
	h := NewHandler(u)

	if err := h.PostCommit(context.Background(), repo); err != nil {
		t.Fatalf("PostCommit should be fail-open, got: %v", err)
	}

	marker, err := session.ReadMarker(repo)
	if err != nil {
		t.Fatalf("ReadMarker: %v", err)
	}
	q, err := NewWorkspaceQueue(marker.WorkspaceID)
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
	if items[0].Event.SessionID != "sess-1" {
		t.Fatalf("queued session_id = %q, want %q", items[0].Event.SessionID, "sess-1")
	}
	if strings.TrimSpace(items[0].Event.WorkspaceID) == "" {
		t.Fatalf("queued workspace_id is empty")
	}
}

func TestPostCommitFlushesQueuedEventsBeforeUploadingCurrentCheckpoint(t *testing.T) {
	repo := initRepoWithCommit2(t)

	home := t.TempDir()
	t.Setenv("HOME", home)

	writeMarker(t, repo, "sess-1")

	marker, err := session.ReadMarker(repo)
	if err != nil {
		t.Fatalf("ReadMarker: %v", err)
	}
	q, err := NewWorkspaceQueue(marker.WorkspaceID)
	if err != nil {
		t.Fatalf("NewWorkspaceQueue: %v", err)
	}

	oldEventID, err := CheckpointEventID("github.com/acme/repo", "old-sha")
	if err != nil {
		t.Fatalf("CheckpointEventID: %v", err)
	}
	if err := q.Enqueue(HookEvent{
		Kind:         "post-commit",
		EventID:      oldEventID,
		SessionID:    "sess-1",
		WorkspaceID:  marker.WorkspaceID,
		RepoFullName: "github.com/acme/repo",
		CommitSHA:    "old-sha",
	}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	u := &fakeUploader{}
	h := NewHandler(u)
	if err := h.PostCommit(context.Background(), repo); err != nil {
		t.Fatalf("PostCommit: %v", err)
	}

	if len(u.events) != 2 {
		b, _ := json.Marshal(u.events)
		t.Fatalf("uploaded events = %d, want 2; events=%s", len(u.events), string(b))
	}
	if got := u.events[0].CommitSHA; got != "old-sha" {
		t.Fatalf("first uploaded commit = %q, want old queued commit", got)
	}
	head := git2(t, repo, "rev-parse", "HEAD")
	if got := u.events[1].CommitSHA; got != head {
		t.Fatalf("second uploaded commit = %q, want current head %q", got, head)
	}

	items, err := q.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("queued items after post-commit = %d, want 0", len(items))
	}
}

func TestFlushReplaysQueuedEvents(t *testing.T) {
	repo := initRepoWithCommit2(t)

	home := t.TempDir()
	t.Setenv("HOME", home)

	writeMarker(t, repo, "sess-1")

	marker, err := session.ReadMarker(repo)
	if err != nil {
		t.Fatalf("ReadMarker: %v", err)
	}
	q, err := NewWorkspaceQueue(marker.WorkspaceID)
	if err != nil {
		t.Fatalf("NewWorkspaceQueue: %v", err)
	}
	// Seed queue with two events.
	for i := 0; i < 2; i++ {
		sha := "c" + string(rune('0'+i))
		eid, err := CheckpointEventID("ws-1", sha)
		if err != nil {
			t.Fatalf("CheckpointEventID: %v", err)
		}
		ev := HookEvent{Kind: "post-commit", SessionID: "sess-1", WorkspaceID: marker.WorkspaceID, CommitSHA: sha, EventID: eid}
		if err := q.Enqueue(ev); err != nil {
			t.Fatalf("Enqueue: %v", err)
		}
	}

	u := &fakeUploader{}
	h := NewHandler(u)
	if err := h.Flush(context.Background(), repo); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	if len(u.events) != 2 {
		b, _ := json.Marshal(u.events)
		t.Fatalf("uploaded events = %d, want 2; events=%s", len(u.events), string(b))
	}
	items, err := q.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("items after flush = %d, want 0", len(items))
	}
}

func TestPostRewriteQueuesEventsWhenUploadFails(t *testing.T) {
	repo := initRepoWithCommit2(t)

	home := t.TempDir()
	t.Setenv("HOME", home)

	repoHint := "github.com/acme/repo"
	writeMarker(t, repo, "sess-1")
	marker, err := session.ReadMarker(repo)
	if err != nil {
		t.Fatalf("ReadMarker: %v", err)
	}
	marker.RepoFullName = repoHint
	if err := session.WriteMarker(repo, marker); err != nil {
		t.Fatalf("WriteMarker: %v", err)
	}

	u := &fakeUploader{err: errors.New("upload failed")}
	h := NewHandler(u)

	stdin := strings.NewReader("oldsha1 newsha1\n")
	if err := h.PostRewrite(context.Background(), repo, "amend", stdin); err != nil {
		t.Fatalf("PostRewrite should be fail-open, got: %v", err)
	}

	marker, err = session.ReadMarker(repo)
	if err != nil {
		t.Fatalf("ReadMarker: %v", err)
	}
	q, err := NewWorkspaceQueue(marker.WorkspaceID)
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
	if ev.Kind != "post-rewrite" {
		t.Fatalf("queued kind = %q, want %q", ev.Kind, "post-rewrite")
	}
	if ev.RewriteType != "amend" || ev.OldCommitSHA != "oldsha1" || ev.NewCommitSHA != "newsha1" {
		b, _ := json.Marshal(ev)
		t.Fatalf("queued rewrite fields mismatch: %s", string(b))
	}
	wantID, err := RewriteEventID(repoHint, "oldsha1", "newsha1", "amend")
	if err != nil {
		t.Fatalf("RewriteEventID: %v", err)
	}
	if ev.EventID != wantID {
		t.Fatalf("queued event_id = %q, want %q", ev.EventID, wantID)
	}
}

func TestPostCommitWithoutMarkerUploadsUnboundCheckpoint(t *testing.T) {
	repo := initRepoWithCommit2(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	_ = runAttributionSync(context.Background(), repo, nil)
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
	if strings.TrimSpace(ev.WorkspaceID) == "" {
		t.Fatalf("workspace_id is empty")
	}
	if strings.TrimSpace(ev.SessionID) != "" {
		t.Fatalf("session_id = %q, want empty", ev.SessionID)
	}
}

func TestPostCommitSetsEventIDBeforeUpload(t *testing.T) {
	repo := initRepoWithCommit2(t)

	home := t.TempDir()
	t.Setenv("HOME", home)

	writeMarker(t, repo, "sess-1")

	u := &fakeUploader{}
	h := NewHandler(u)
	if err := h.PostCommit(context.Background(), repo); err != nil {
		t.Fatalf("PostCommit: %v", err)
	}

	if len(u.events) != 1 {
		t.Fatalf("uploaded events = %d, want 1", len(u.events))
	}
	if got := strings.TrimSpace(u.events[0].EventID); got == "" {
		t.Fatalf("uploaded event_id is empty; expected handler to set event_id before upload")
	}
}

func TestPostCommitUsesRepoScopedEventID(t *testing.T) {
	repo := initRepoWithCommit2(t)

	home := t.TempDir()
	t.Setenv("HOME", home)

	repoHint := "github.com/acme/repo"
	writeMarker(t, repo, "sess-1")
	marker, err := session.ReadMarker(repo)
	if err != nil {
		t.Fatalf("ReadMarker: %v", err)
	}
	marker.RepoFullName = repoHint
	if err := session.WriteMarker(repo, marker); err != nil {
		t.Fatalf("WriteMarker: %v", err)
	}

	u := &fakeUploader{}
	h := NewHandler(u)
	if err := h.PostCommit(context.Background(), repo); err != nil {
		t.Fatalf("PostCommit: %v", err)
	}

	if len(u.events) != 1 {
		t.Fatalf("uploaded events = %d, want 1", len(u.events))
	}
	head := git2(t, repo, "rev-parse", "HEAD")
	wantID, err := CheckpointEventID(repoHint, head)
	if err != nil {
		t.Fatalf("CheckpointEventID: %v", err)
	}
	if got := u.events[0].EventID; got != wantID {
		t.Fatalf("uploaded event_id = %q, want %q", got, wantID)
	}
}

func TestPostCommit_TriggersAttributionSync(t *testing.T) {
	repo := initRepoWithCommit2(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	writeMarker(t, repo, "sess-1")

	calls := 0
	old := runAttributionSync
	runAttributionSync = func(ctx context.Context, cwd string, syncClient attributionlocal.BackendClient) error {
		calls++
		return nil
	}
	t.Cleanup(func() { runAttributionSync = old })

	h := NewHandler(&fakeUploader{})
	if err := h.PostCommit(context.Background(), repo); err != nil {
		t.Fatalf("PostCommit: %v", err)
	}
	if calls != 1 {
		t.Fatalf("sync calls = %d, want 1", calls)
	}
}

func TestPostCommit_TriggersAttributionSyncBeforeUpload(t *testing.T) {
	repo := initRepoWithCommit2(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	writeMarker(t, repo, "sess-1")

	synced := false
	old := runAttributionSync
	runAttributionSync = func(ctx context.Context, cwd string, syncClient attributionlocal.BackendClient) error {
		synced = true
		return nil
	}
	t.Cleanup(func() { runAttributionSync = old })

	u := &fakeUploader{
		onCall: func() {
			if !synced {
				t.Fatal("expected attribution sync before upload")
			}
		},
	}
	h := NewHandler(u)
	if err := h.PostCommit(context.Background(), repo); err != nil {
		t.Fatalf("PostCommit: %v", err)
	}
}

func TestPostCommit_WithBackendUploaderUploadsCheckpointBeforeToolUsageSync(t *testing.T) {
	repo := initRepoWithCommit2(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	workspaceRoot := git2(t, repo, "rev-parse", "--show-toplevel")
	codexDir := filepath.Join(home, ".codex", "sessions", "2026", "05", "19")
	if err := os.MkdirAll(codexDir, 0o700); err != nil {
		t.Fatalf("mkdir codex dir: %v", err)
	}
	codexPath := filepath.Join(codexDir, "sess-hook-sync.jsonl")
	codexBody := `{"timestamp":"2026-05-19T07:05:07Z","type":"session_meta","payload":{"id":"codex-hook-sync-1","cwd":"` + workspaceRoot + `"}}
{"timestamp":"2026-05-19T07:05:08Z","type":"event_msg","payload":{"type":"token_count","response_id":"resp-hook-sync-1","info":{"last_token_usage":{"input_tokens":12,"cached_input_tokens":4,"output_tokens":5,"reasoning_output_tokens":2,"total_tokens":21}}}}`
	if err := os.WriteFile(codexPath, []byte(codexBody), 0o600); err != nil {
		t.Fatalf("write codex jsonl: %v", err)
	}
	writeMarker(t, repo, "sess-1")

	clientStub := &recordingBackendHookClient{}
	h := NewHandler(NewBackendUploader(clientStub))
	if err := h.PostCommit(context.Background(), repo); err != nil {
		t.Fatalf("PostCommit: %v", err)
	}

	if len(clientStub.checkpoints) != 1 {
		t.Fatalf("checkpoint uploads = %d, want 1", len(clientStub.checkpoints))
	}
	if len(clientStub.toolUsage) == 0 {
		t.Fatal("expected tool usage uploads during post-commit sync")
	}
	if len(clientStub.order) < 3 {
		t.Fatalf("upload order = %v, want ensure_repo then checkpoint then tool usage", clientStub.order)
	}
	if clientStub.order[0] != "ensure_repo" || clientStub.order[1] != "checkpoint" || clientStub.order[2] != "tool_usage" {
		t.Fatalf("upload order = %v, want ensure_repo before checkpoint before tool usage", clientStub.order)
	}
}

func TestPostRewriteUsesRepoScopedEventID(t *testing.T) {
	repo := initRepoWithCommit2(t)

	home := t.TempDir()
	t.Setenv("HOME", home)

	repoHint := "github.com/acme/repo"
	writeMarker(t, repo, "sess-1")
	marker, err := session.ReadMarker(repo)
	if err != nil {
		t.Fatalf("ReadMarker: %v", err)
	}
	marker.RepoFullName = repoHint
	if err := session.WriteMarker(repo, marker); err != nil {
		t.Fatalf("WriteMarker: %v", err)
	}

	u := &fakeUploader{}
	h := NewHandler(u)
	if err := h.PostRewrite(context.Background(), repo, "amend", strings.NewReader("oldsha1 newsha1\n")); err != nil {
		t.Fatalf("PostRewrite: %v", err)
	}

	if len(u.events) != 1 {
		t.Fatalf("uploaded events = %d, want 1", len(u.events))
	}
	wantID, err := RewriteEventID(repoHint, "oldsha1", "newsha1", "amend")
	if err != nil {
		t.Fatalf("RewriteEventID: %v", err)
	}
	if got := u.events[0].EventID; got != wantID {
		t.Fatalf("uploaded event_id = %q, want %q", got, wantID)
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

func TestPostCommitAttachesCollectorSnapshotAndWritesCache(t *testing.T) {
	repo := initRepoWithCommit2(t)

	home := t.TempDir()
	t.Setenv("HOME", home)

	workspaceRoot := git2(t, repo, "rev-parse", "--show-toplevel")
	codex, claude, kiro := writeCollectorFixtures(t, workspaceRoot)
	t.Setenv("AE_CODEX_SESSION_FILES", codex)
	t.Setenv("AE_CLAUDE_SESSION_FILES", claude)
	t.Setenv("AE_KIRO_SESSION_FILES", kiro)

	writeMarker(t, repo, "sess-collector")

	u := &fakeUploader{}
	h := NewHandler(u)
	if err := h.PostCommit(context.Background(), repo); err != nil {
		t.Fatalf("PostCommit: %v", err)
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

	workspaceID := u.events[0].WorkspaceID
	cacheFile := filepath.Join(attributionlocal.AttributionRootDir(), "workspaces", workspaceID, "collectors", "latest.json")
	data, err := os.ReadFile(cacheFile)
	if err != nil {
		t.Fatalf("read cache file: %v", err)
	}
	if !strings.Contains(string(data), "\"conversation_id\": \"conv-kiro-1\"") {
		t.Fatalf("cache file missing kiro snapshot: %s", string(data))
	}
}

func TestPostCommitQueuesCollectorSnapshotWhenUploadFails(t *testing.T) {
	repo := initRepoWithCommit2(t)

	home := t.TempDir()
	t.Setenv("HOME", home)

	workspaceRoot := git2(t, repo, "rev-parse", "--show-toplevel")
	codex, claude, kiro := writeCollectorFixtures(t, workspaceRoot)
	t.Setenv("AE_CODEX_SESSION_FILES", codex)
	t.Setenv("AE_CLAUDE_SESSION_FILES", claude)
	t.Setenv("AE_KIRO_SESSION_FILES", kiro)

	writeMarker(t, repo, "sess-collector")

	u := &fakeUploader{err: errors.New("upload failed")}
	h := NewHandler(u)
	if err := h.PostCommit(context.Background(), repo); err != nil {
		t.Fatalf("PostCommit should fail-open, got: %v", err)
	}

	marker, err := session.ReadMarker(repo)
	if err != nil {
		t.Fatalf("ReadMarker: %v", err)
	}
	q, err := NewWorkspaceQueue(marker.WorkspaceID)
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
	if items[0].Event.AgentSnapshot == nil {
		t.Fatalf("queued agent snapshot is nil")
	}
	claudeSnapshot, _ := items[0].Event.AgentSnapshot["claude"].(map[string]any)
	if got := claudeSnapshot["cached_input_tokens"]; got != float64(90) {
		t.Fatalf("claude cached_input_tokens = %v, want 90", got)
	}
}

func TestPostCommitPreservesCollectorCacheWhenNoSnapshot(t *testing.T) {
	repo := initRepoWithCommit2(t)

	home := t.TempDir()
	t.Setenv("HOME", home)

	writeMarker(t, repo, "sess-cache")
	original := &collector.Snapshot{
		Codex: &collector.CodexSnapshot{SourceSessionID: "codex-prev", TotalTokens: 999},
	}
	marker, err := session.ReadMarker(repo)
	if err != nil {
		t.Fatalf("ReadMarker: %v", err)
	}
	if err := collector.WriteWorkspaceCache(marker.WorkspaceID, original); err != nil {
		t.Fatalf("WriteWorkspaceCache: %v", err)
	}

	u := &fakeUploader{}
	h := NewHandler(u)
	if err := h.PostCommit(context.Background(), repo); err != nil {
		t.Fatalf("PostCommit: %v", err)
	}

	marker, err = session.ReadMarker(repo)
	if err != nil {
		t.Fatalf("ReadMarker: %v", err)
	}
	cacheFile := filepath.Join(attributionlocal.AttributionRootDir(), "workspaces", marker.WorkspaceID, "collectors", "latest.json")
	data, err := os.ReadFile(cacheFile)
	if err != nil {
		t.Fatalf("read cache file: %v", err)
	}
	if !strings.Contains(string(data), "\"source_session_id\": \"codex-prev\"") {
		t.Fatalf("cache file was unexpectedly overwritten: %s", string(data))
	}
}
