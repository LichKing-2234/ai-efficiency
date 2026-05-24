package hooks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestQueuePersistsAndDedupByEventID(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	q, err := NewWorkspaceQueue("ws-1")
	if err != nil {
		t.Fatalf("NewWorkspaceQueue: %v", err)
	}

	ev := HookEvent{Kind: "post-commit", WorkspaceID: "ws-1", CommitSHA: "deadbeef", EventID: "evt-1"}
	if err := q.Enqueue(ev); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	// Duplicate should be ignored.
	if err := q.Enqueue(ev); err != nil {
		t.Fatalf("Enqueue dup: %v", err)
	}

	items, err := q.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("items = %d, want 1", len(items))
	}

	// Reload should see the same item.
	q2, err := NewWorkspaceQueue("ws-1")
	if err != nil {
		t.Fatalf("NewWorkspaceQueue 2: %v", err)
	}
	items2, err := q2.List()
	if err != nil {
		t.Fatalf("List 2: %v", err)
	}
	if len(items2) != 1 {
		t.Fatalf("items2 = %d, want 1", len(items2))
	}

	// Ensure queue file exists on disk.
	if _, err := os.Stat(q.Path()); err != nil {
		t.Fatalf("expected queue file to exist: %v", err)
	}
}

func TestQueueRejectsMissingEventID(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	q, err := NewWorkspaceQueue("ws-1")
	if err != nil {
		t.Fatalf("NewWorkspaceQueue: %v", err)
	}

	// Task 5 contract: hook events must carry an explicit, stable event_id
	// before entering the queue (no queue-generated fallback IDs).
	ev := HookEvent{Kind: "post-commit", WorkspaceID: "ws-1", CommitSHA: "deadbeef"}
	if err := q.Enqueue(ev); err == nil {
		t.Fatalf("expected enqueue to fail due to missing event_id, got nil")
	}
}

func TestQueueReadsLargeAgentSnapshotPayload(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	q, err := NewWorkspaceQueue("ws-1")
	if err != nil {
		t.Fatalf("NewWorkspaceQueue: %v", err)
	}

	ev := HookEvent{
		Kind:        "post-commit",
		WorkspaceID: "ws-1",
		CommitSHA:   "deadbeef",
		EventID:     "evt-large",
		AgentSnapshot: map[string]any{
			"codex": map[string]any{
				"raw_payload": map[string]any{
					"blob": strings.Repeat("x", 9*1024*1024),
				},
			},
		},
	}
	if err := q.Enqueue(ev); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	items, err := q.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("items = %d, want 1", len(items))
	}
}

func TestLedgerAppendAndReadUsesWorkspaceStateWithoutRawPayloads(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	now := time.Date(2026, 5, 24, 10, 0, 0, 0, time.UTC)
	rec := LedgerRecord{
		Version:      1,
		Kind:         "post-commit",
		DedupeKey:    "dedupe-1",
		ServerURL:    "https://ae.example.com",
		AuthSubject:  "user:1",
		RepoConfigID: 123,
		RepoKey:      "repo-host.example.com/org/repo",
		WorkspaceID:  "ws-1",
		Status:       "uploaded",
		AttemptCount: 1,
		AttemptedAt:  now,
		UploadedAt:   &now,
	}

	if err := AppendLedger("ws-1", rec); err != nil {
		t.Fatalf("AppendLedger: %v", err)
	}
	records, err := ReadLedger("ws-1")
	if err != nil {
		t.Fatalf("ReadLedger: %v", err)
	}
	if len(records) != 1 || records[0].DedupeKey != "dedupe-1" || records[0].RepoConfigID != 123 {
		t.Fatalf("records = %+v, want one ledger record", records)
	}
	path, err := LedgerPath("ws-1")
	if err != nil {
		t.Fatalf("LedgerPath: %v", err)
	}
	if got, want := path, filepath.Join(home, ".ae-cli", "state", "attribution", "workspaces", "ws-1", "upload-ledger.jsonl"); got != want {
		t.Fatalf("LedgerPath() = %q, want %q", got, want)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile ledger: %v", err)
	}
	if strings.Contains(string(data), "raw_payload") || strings.Contains(string(data), "raw_source_path") {
		t.Fatalf("ledger should not contain raw payload or source paths: %s", string(data))
	}
}
