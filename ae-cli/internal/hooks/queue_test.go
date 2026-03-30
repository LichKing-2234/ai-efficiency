package hooks

import (
	"os"
	"testing"
)

func TestQueuePersistsAndDedupByEventID(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	q, err := NewLocalQueue("sess-1")
	if err != nil {
		t.Fatalf("NewLocalQueue: %v", err)
	}

	ev := HookEvent{Kind: "post-commit", SessionID: "sess-1", CommitSHA: "deadbeef", EventID: "evt-1"}
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
	q2, err := NewLocalQueue("sess-1")
	if err != nil {
		t.Fatalf("NewLocalQueue 2: %v", err)
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
