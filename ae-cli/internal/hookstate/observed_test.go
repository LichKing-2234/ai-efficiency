package hookstate

import (
	"testing"
	"time"
)

func TestObservedReposTrackStableContextWithoutChangingFirstObserved(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	first := time.Date(2026, 5, 24, 10, 0, 0, 0, time.UTC)
	second := first.Add(time.Hour)
	ctx := Context{ServerURL: "https://AE.example.com/", AuthSubject: "user:1", RepoKey: "repo-host.example.com/org/repo"}

	observed, err := LoadObservedRepos()
	if err != nil {
		t.Fatalf("LoadObservedRepos: %v", err)
	}
	observed.Observe(ctx, "git@repo-host.example.com:org/repo.git", first)
	observed.Observe(ctx, "https://repo-host.example.com/org/repo.git", second)

	matches := observed.Matching(ctx)
	if len(matches) != 1 {
		t.Fatalf("matches = %d, want 1", len(matches))
	}
	if !matches[0].FirstObservedAt.Equal(first) || !matches[0].LastObservedAt.Equal(second) {
		t.Fatalf("observed times = %s/%s, want %s/%s", matches[0].FirstObservedAt, matches[0].LastObservedAt, first, second)
	}
	if matches[0].ServerURL != "https://ae.example.com" || matches[0].AuthSubject != "user:1" {
		t.Fatalf("context not normalized in record: %+v", matches[0])
	}
}

func TestObservedReposTrackUnboundByRepoKey(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	now := time.Date(2026, 5, 24, 10, 0, 0, 0, time.UTC)
	ctx := Context{RepoKey: "repo-host.example.com/org/repo"}

	observed, err := LoadObservedRepos()
	if err != nil {
		t.Fatalf("LoadObservedRepos: %v", err)
	}
	observed.Observe(ctx, "https://repo-host.example.com/org/repo.git", now)

	matches := observed.Matching(ctx)
	if len(matches) != 1 {
		t.Fatalf("matches = %d, want 1", len(matches))
	}
	if matches[0].ServerURL != "" || matches[0].AuthSubject != "" {
		t.Fatalf("unbound record should not carry server/account context: %+v", matches[0])
	}
}
