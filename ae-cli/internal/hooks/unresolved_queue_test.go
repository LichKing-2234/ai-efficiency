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
