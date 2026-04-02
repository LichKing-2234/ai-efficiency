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

	"github.com/ai-efficiency/ae-cli/internal/collector"
	"github.com/ai-efficiency/ae-cli/internal/proxy"
	"github.com/ai-efficiency/ae-cli/internal/session"
)

type fakeUploader struct {
	err    error
	events []HookEvent
}

func (f *fakeUploader) UploadHookEvent(ctx context.Context, ev HookEvent) error {
	f.events = append(f.events, ev)
	return f.err
}

func startRealProxy(t *testing.T, sessionID string) (listenAddr, authToken string) {
	t.Helper()

	cfg := proxy.RuntimeConfig{
		SessionID:  sessionID,
		ListenAddr: "127.0.0.1:0",
		AuthToken:  "proxy-token",
	}
	result, err := proxy.Spawn(cfg)
	if err != nil {
		t.Fatalf("proxy.Spawn: %v", err)
	}
	t.Cleanup(func() {
		if err := proxy.Stop(proxy.StopRequest{
			PID:        result.PID,
			ListenAddr: result.ListenAddr,
			AuthToken:  cfg.AuthToken,
			ConfigPath: result.ConfigPath,
		}); err != nil {
			t.Fatalf("proxy.Stop: %v", err)
		}
	})

	return result.ListenAddr, cfg.AuthToken
}

func writeRuntimeWithProxy(t *testing.T, sessionID, listenAddr, authToken string) {
	t.Helper()
	if err := session.WriteRuntimeBundle(&session.RuntimeBundle{
		SessionID: sessionID,
		Proxy: &session.ProxyRuntime{
			ListenAddr: listenAddr,
			AuthToken:  authToken,
		},
	}); err != nil {
		t.Fatalf("WriteRuntimeBundle: %v", err)
	}
}

func writeMarker(t *testing.T, repo, sessionID string) {
	t.Helper()
	if err := session.WriteMarker(repo, &session.Marker{SessionID: sessionID, RepoFullName: "origin"}); err != nil {
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

func TestPostCommitSendsEventToLocalProxyBeforeQueueFallback(t *testing.T) {
	repo := initRepoWithCommit2(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	listenAddr, authToken := startRealProxy(t, "sess-1")
	writeRuntimeWithProxy(t, "sess-1", listenAddr, authToken)
	writeMarker(t, repo, "sess-1")

	h := NewHandler(nil)
	if err := h.PostCommit(context.Background(), repo); err != nil {
		t.Fatalf("PostCommit: %v", err)
	}

	q, err := NewLocalQueue("sess-1")
	if err != nil {
		t.Fatalf("NewLocalQueue: %v", err)
	}
	items, err := q.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("queued items = %d, want 0 (expected local proxy ingress to accept event)", len(items))
	}
}

func TestPostCommitQueuesEventWhenUploadFails(t *testing.T) {
	repo := initRepoWithCommit2(t)

	home := t.TempDir()
	t.Setenv("HOME", home)

	marker := &session.Marker{SessionID: "sess-1", RepoFullName: "origin", Branch: "main", HeadSHA: git2(t, repo, "rev-parse", "HEAD")}
	if err := session.WriteMarker(repo, marker); err != nil {
		t.Fatalf("WriteMarker: %v", err)
	}

	u := &fakeUploader{err: errors.New("upload failed")}
	h := NewHandler(u)

	if err := h.PostCommit(context.Background(), repo); err != nil {
		t.Fatalf("PostCommit should be fail-open, got: %v", err)
	}

	q, err := NewLocalQueue("sess-1")
	if err != nil {
		t.Fatalf("NewLocalQueue: %v", err)
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
}

func TestFlushReplaysQueuedEvents(t *testing.T) {
	repo := initRepoWithCommit2(t)

	home := t.TempDir()
	t.Setenv("HOME", home)

	marker := &session.Marker{SessionID: "sess-1", RepoFullName: "origin", Branch: "main", HeadSHA: git2(t, repo, "rev-parse", "HEAD")}
	if err := session.WriteMarker(repo, marker); err != nil {
		t.Fatalf("WriteMarker: %v", err)
	}

	q, err := NewLocalQueue("sess-1")
	if err != nil {
		t.Fatalf("NewLocalQueue: %v", err)
	}
	// Seed queue with two events.
	for i := 0; i < 2; i++ {
		sha := "c" + string(rune('0'+i))
		eid, err := CheckpointEventID("ws-1", sha)
		if err != nil {
			t.Fatalf("CheckpointEventID: %v", err)
		}
		ev := HookEvent{Kind: "post-commit", SessionID: "sess-1", CommitSHA: sha, EventID: eid}
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
	marker := &session.Marker{SessionID: "sess-1", RepoFullName: repoHint}
	if err := session.WriteMarker(repo, marker); err != nil {
		t.Fatalf("WriteMarker: %v", err)
	}

	u := &fakeUploader{err: errors.New("upload failed")}
	h := NewHandler(u)

	stdin := strings.NewReader("oldsha1 newsha1\n")
	if err := h.PostRewrite(context.Background(), repo, "amend", stdin); err != nil {
		t.Fatalf("PostRewrite should be fail-open, got: %v", err)
	}

	q, err := NewLocalQueue("sess-1")
	if err != nil {
		t.Fatalf("NewLocalQueue: %v", err)
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

func TestPostCommitSetsEventIDBeforeUpload(t *testing.T) {
	repo := initRepoWithCommit2(t)

	home := t.TempDir()
	t.Setenv("HOME", home)

	marker := &session.Marker{SessionID: "sess-1", RepoFullName: "origin", Branch: "main", HeadSHA: git2(t, repo, "rev-parse", "HEAD")}
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
	if got := strings.TrimSpace(u.events[0].EventID); got == "" {
		t.Fatalf("uploaded event_id is empty; expected handler to set event_id before upload")
	}
}

func TestPostCommitUsesRepoScopedEventID(t *testing.T) {
	repo := initRepoWithCommit2(t)

	home := t.TempDir()
	t.Setenv("HOME", home)

	repoHint := "github.com/acme/repo"
	marker := &session.Marker{SessionID: "sess-1", RepoFullName: repoHint}
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

func TestPostRewriteUsesRepoScopedEventID(t *testing.T) {
	repo := initRepoWithCommit2(t)

	home := t.TempDir()
	t.Setenv("HOME", home)

	repoHint := "github.com/acme/repo"
	marker := &session.Marker{SessionID: "sess-1", RepoFullName: repoHint}
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

	marker := &session.Marker{SessionID: "sess-collector", RepoFullName: "github.com/acme/repo"}
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
	if u.events[0].AgentSnapshot == nil {
		t.Fatalf("uploaded agent snapshot is nil")
	}
	codexSnapshot, _ := u.events[0].AgentSnapshot["codex"].(map[string]any)
	if got := codexSnapshot["source_session_id"]; got != "codex-sess-1" {
		t.Fatalf("codex source_session_id = %v, want codex-sess-1", got)
	}

	cacheFile := filepath.Join(session.RuntimeCollectorsDir("sess-collector"), "latest.json")
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

	marker := &session.Marker{SessionID: "sess-collector", RepoFullName: "github.com/acme/repo"}
	if err := session.WriteMarker(repo, marker); err != nil {
		t.Fatalf("WriteMarker: %v", err)
	}

	u := &fakeUploader{err: errors.New("upload failed")}
	h := NewHandler(u)
	if err := h.PostCommit(context.Background(), repo); err != nil {
		t.Fatalf("PostCommit should fail-open, got: %v", err)
	}

	q, err := NewLocalQueue("sess-collector")
	if err != nil {
		t.Fatalf("NewLocalQueue: %v", err)
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

	marker := &session.Marker{SessionID: "sess-cache", RepoFullName: "github.com/acme/repo"}
	if err := session.WriteMarker(repo, marker); err != nil {
		t.Fatalf("WriteMarker: %v", err)
	}
	original := &collector.Snapshot{
		Codex: &collector.CodexSnapshot{SourceSessionID: "codex-prev", TotalTokens: 999},
	}
	if err := collector.WriteCache("sess-cache", original); err != nil {
		t.Fatalf("WriteCache: %v", err)
	}

	u := &fakeUploader{}
	h := NewHandler(u)
	if err := h.PostCommit(context.Background(), repo); err != nil {
		t.Fatalf("PostCommit: %v", err)
	}

	cacheFile := filepath.Join(session.RuntimeCollectorsDir("sess-cache"), "latest.json")
	data, err := os.ReadFile(cacheFile)
	if err != nil {
		t.Fatalf("read cache file: %v", err)
	}
	if !strings.Contains(string(data), "\"source_session_id\": \"codex-prev\"") {
		t.Fatalf("cache file was unexpectedly overwritten: %s", string(data))
	}
}
