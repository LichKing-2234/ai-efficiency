package cmd

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/ai-efficiency/ae-cli/internal/hooks"
	"github.com/ai-efficiency/ae-cli/internal/session"
)

type recordingUploader struct {
	events []hooks.HookEvent
}

func (r *recordingUploader) UploadHookEvent(_ context.Context, ev hooks.HookEvent) error {
	r.events = append(r.events, ev)
	return nil
}

func TestFlushCommandDrainsQueuedHookEvents(t *testing.T) {
	repo := initRepoWithCommitForCmdTests(t)

	home := t.TempDir()
	t.Setenv("HOME", home)

	// Seed a bound session marker so flush can find the session queue.
	marker := &session.Marker{SessionID: "sess-1"}
	if err := session.WriteMarker(repo, marker); err != nil {
		t.Fatalf("WriteMarker: %v", err)
	}

	q, err := hooks.NewLocalQueue("sess-1")
	if err != nil {
		t.Fatalf("NewLocalQueue: %v", err)
	}
	if err := q.Enqueue(hooks.HookEvent{Kind: "post-commit", SessionID: "sess-1", CommitSHA: "deadbeef", EventID: "evt-1"}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	defer func() { _ = os.Chdir(wd) }()
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("Chdir(repo): %v", err)
	}

	// Run the hidden command implementation directly.
	if err := flushCmd.RunE(flushCmd, nil); err != nil {
		t.Fatalf("flush RunE: %v", err)
	}

	items, err := q.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("queue items = %d, want 1 when uploader is unsupported", len(items))
	}

	// Ensure it is safe to call multiple times.
	if err := flushCmd.RunE(flushCmd, nil); err != nil {
		t.Fatalf("flush RunE (2): %v", err)
	}
}

func TestFlushCommandFindsQueuedEventsWithoutMarker(t *testing.T) {
	repo := initRepoWithCommitForCmdTests(t)

	home := t.TempDir()
	t.Setenv("HOME", home)

	q, err := hooks.NewLocalQueue("sess-orphan")
	if err != nil {
		t.Fatalf("NewLocalQueue: %v", err)
	}
	if err := q.Enqueue(hooks.HookEvent{Kind: "post-commit", SessionID: "sess-orphan", CommitSHA: "deadbeef", EventID: "evt-orphan"}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	uploader := &recordingUploader{}
	prevFactory := newHookUploader
	newHookUploader = func() hooks.Uploader { return uploader }
	defer func() { newHookUploader = prevFactory }()

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	defer func() { _ = os.Chdir(wd) }()
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("Chdir(repo): %v", err)
	}

	if err := flushCmd.RunE(flushCmd, nil); err != nil {
		t.Fatalf("flush RunE: %v", err)
	}

	items, err := q.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("queue items = %d, want 0 after flush replay", len(items))
	}
	if len(uploader.events) != 1 {
		t.Fatalf("uploaded events = %d, want 1", len(uploader.events))
	}
}

func initRepoWithCommitForCmdTests(t *testing.T) string {
	t.Helper()
	// Reuse the hooks test helper without importing hooks test package internals.
	// Minimal local git repo init.
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "t@example.com")
	runGit(t, dir, "config", "user.name", "t")
	if err := os.WriteFile(dir+"/a.txt", []byte("a\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "init")
	return dir
}

func runGit(t *testing.T, dir string, args ...string) {
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
}
