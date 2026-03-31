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
