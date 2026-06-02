package hooks

import (
	"testing"
	"time"
)

func TestUnresolvedQueuePersistsAndDedupesPostCommit(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	ev := UnresolvedHookEvent{
		Kind:           "post-commit",
		RemoteURL:      "https://github.com/acme/repo.git",
		RepoKey:        "github.com/acme/repo",
		WorkspaceID:    "ws-unresolved",
		ServerURL:      "https://ae.example.com",
		AuthSubject:    "user:123",
		CommitSHA:      "abc123",
		BranchSnapshot: "main",
		HeadSnapshot:   "abc123",
		CapturedAt:     time.Date(2026, 6, 2, 9, 0, 0, 0, time.UTC).Format(time.RFC3339),
	}
	if err := EnqueueUnresolvedHookEvent(ev); err != nil {
		t.Fatalf("EnqueueUnresolvedHookEvent first: %v", err)
	}
	if err := EnqueueUnresolvedHookEvent(ev); err != nil {
		t.Fatalf("EnqueueUnresolvedHookEvent duplicate: %v", err)
	}

	items, err := ListUnresolvedHookEvents()
	if err != nil {
		t.Fatalf("ListUnresolvedHookEvents: %v", err)
	}
	if len(items) != 1 || items[0].CommitSHA != "abc123" {
		t.Fatalf("items = %+v, want one unresolved commit", items)
	}
}

func TestUnresolvedQueuePersistsAndDedupesPostRewriteByPair(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	ev := UnresolvedHookEvent{
		Kind:         "post-rewrite",
		RemoteURL:    "https://github.com/acme/repo.git",
		RepoKey:      "github.com/acme/repo",
		WorkspaceID:  "ws-unresolved",
		ServerURL:    "https://ae.example.com",
		AuthSubject:  "user:123",
		RewriteType:  "amend",
		OldCommitSHA: "oldsha1",
		NewCommitSHA: "newsha1",
		CapturedAt:   time.Date(2026, 6, 2, 9, 0, 0, 0, time.UTC).Format(time.RFC3339),
	}
	if err := EnqueueUnresolvedHookEvent(ev); err != nil {
		t.Fatalf("EnqueueUnresolvedHookEvent first: %v", err)
	}
	if err := EnqueueUnresolvedHookEvent(ev); err != nil {
		t.Fatalf("EnqueueUnresolvedHookEvent duplicate: %v", err)
	}
	second := ev
	second.OldCommitSHA = "oldsha2"
	second.NewCommitSHA = "newsha2"
	if err := EnqueueUnresolvedHookEvent(second); err != nil {
		t.Fatalf("EnqueueUnresolvedHookEvent second pair: %v", err)
	}

	items, err := ListUnresolvedHookEvents()
	if err != nil {
		t.Fatalf("ListUnresolvedHookEvents: %v", err)
	}
	if len(items) != 2 || items[0].OldCommitSHA != "oldsha1" || items[1].OldCommitSHA != "oldsha2" {
		t.Fatalf("items = %+v, want distinct rewrite pairs only", items)
	}
}

func TestUnresolvedQueueLockedSaveDoesNotDropConcurrentEnqueue(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	first := UnresolvedHookEvent{
		Kind:        "post-commit",
		RemoteURL:   "https://github.com/acme/repo.git",
		RepoKey:     "github.com/acme/repo",
		WorkspaceID: "ws-unresolved",
		CommitSHA:   "first",
		CapturedAt:  time.Date(2026, 6, 2, 9, 0, 0, 0, time.UTC).Format(time.RFC3339),
	}
	second := first
	second.CommitSHA = "second"
	if err := EnqueueUnresolvedHookEvent(first); err != nil {
		t.Fatalf("EnqueueUnresolvedHookEvent(first): %v", err)
	}

	enqueueStarted := make(chan struct{})
	enqueueDone := make(chan error, 1)
	err := withUnresolvedQueueLock(func() error {
		items, err := listUnresolvedHookEventsUnlocked()
		if err != nil {
			return err
		}
		if len(items) != 1 || items[0].CommitSHA != "first" {
			t.Fatalf("locked list = %+v, want first unresolved event", items)
		}
		go func() {
			close(enqueueStarted)
			enqueueDone <- EnqueueUnresolvedHookEvent(second)
		}()
		<-enqueueStarted
		time.Sleep(50 * time.Millisecond)
		return saveUnresolvedHookEventsUnlocked(nil)
	})
	if err != nil {
		t.Fatalf("withUnresolvedQueueLock: %v", err)
	}
	if err := <-enqueueDone; err != nil {
		t.Fatalf("concurrent EnqueueUnresolvedHookEvent(second): %v", err)
	}

	items, err := ListUnresolvedHookEvents()
	if err != nil {
		t.Fatalf("ListUnresolvedHookEvents: %v", err)
	}
	if len(items) != 1 || items[0].CommitSHA != "second" {
		t.Fatalf("items after locked save = %+v, want only concurrent second event", items)
	}
}
