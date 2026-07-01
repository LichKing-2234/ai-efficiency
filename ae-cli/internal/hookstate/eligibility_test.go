package hookstate

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/ai-efficiency/ae-cli/internal/client"
)

func TestEligibilityPositiveLookupRequiresContextCredentialAndRepoID(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	now := time.Date(2026, 5, 24, 10, 0, 0, 0, time.UTC)
	ctx := Context{ServerURL: "https://AE.example.com/", AuthSubject: "user:1", RepoKey: "repo-host.example.com/org/repo"}

	cache, err := LoadEligibilityCache()
	if err != nil {
		t.Fatalf("LoadEligibilityCache: %v", err)
	}
	cache.PutPositive(ctx, client.RepoEligibilityResponse{
		Eligible:     true,
		RepoConfigID: 123,
		RepoKey:      "repo-host.example.com/org/repo",
		FullName:     "org/repo",
		CloneURL:     "https://repo-host.example.com/org/repo.git",
		Status:       "active",
		BindingState: "bound",
	}, now)

	got, ok := cache.Lookup(ctx, now.Add(time.Minute), true)
	if !ok || !got.Eligible || got.RepoConfigID != 123 {
		t.Fatalf("positive lookup = %+v, %v; want repo_config_id 123 hit", got, ok)
	}
	if got.ServerURL != "https://ae.example.com" {
		t.Fatalf("stored server = %q, want normalized server", got.ServerURL)
	}
	if _, ok := cache.Lookup(ctx, now.Add(time.Minute), false); ok {
		t.Fatalf("lookup should miss without usable credentials")
	}
	if _, ok := cache.Lookup(Context{ServerURL: ctx.ServerURL, AuthSubject: "user:2", RepoKey: ctx.RepoKey}, now.Add(time.Minute), true); ok {
		t.Fatalf("lookup should miss for a different auth subject")
	}
	if _, ok := cache.Lookup(ctx, now.Add(25*time.Hour), true); ok {
		t.Fatalf("lookup should miss after positive TTL")
	}
	if got, ok := cache.LookupStalePositive(ctx, true); !ok || got.RepoConfigID != 123 {
		t.Fatalf("stale positive lookup = %+v, %v; want repo_config_id 123 hit", got, ok)
	}
	if _, ok := cache.LookupStalePositive(ctx, false); ok {
		t.Fatalf("stale positive lookup should miss without usable credentials")
	}

	cache.PutPositive(ctx, client.RepoEligibilityResponse{Eligible: true, RepoKey: ctx.RepoKey}, now)
	if _, ok := cache.Lookup(ctx, now.Add(time.Minute), true); ok {
		t.Fatalf("positive lookup should miss when repo_config_id is zero")
	}
}

func TestEligibilityNegativeLookupExpiresQuickly(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	now := time.Date(2026, 5, 24, 10, 0, 0, 0, time.UTC)
	ctx := Context{ServerURL: "https://ae.example.com", AuthSubject: "user:1", RepoKey: "repo-host.example.com/org/missing"}

	cache, err := LoadEligibilityCache()
	if err != nil {
		t.Fatalf("LoadEligibilityCache: %v", err)
	}
	cache.PutNegative(ctx, "https://repo-host.example.com/org/missing.git", "not_found", now)

	got, ok := cache.Lookup(ctx, now.Add(4*time.Minute), true)
	if !ok || got.Eligible || got.Reason != "not_found" {
		t.Fatalf("negative lookup = %+v, %v; want not_found hit", got, ok)
	}
	if _, ok := cache.Lookup(ctx, now.Add(6*time.Minute), true); ok {
		t.Fatalf("negative lookup should miss after negative TTL")
	}
}

func TestEligibilitySaveAndLoadUsesHooksStatePath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	now := time.Date(2026, 5, 24, 10, 0, 0, 0, time.UTC)
	ctx := Context{ServerURL: "https://ae.example.com", AuthSubject: "user:1", RepoKey: "repo-host.example.com/org/repo"}

	cache, err := LoadEligibilityCache()
	if err != nil {
		t.Fatalf("LoadEligibilityCache: %v", err)
	}
	cache.PutPositive(ctx, client.RepoEligibilityResponse{
		Eligible:     true,
		RepoConfigID: 123,
		RepoKey:      ctx.RepoKey,
	}, now)
	if err := cache.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if got, want := EligibilityPath(), filepath.Join(home, ".ae-cli", "state", "hooks", "repos.json"); got != want {
		t.Fatalf("EligibilityPath() = %q, want %q", got, want)
	}
	reloaded, err := LoadEligibilityCache()
	if err != nil {
		t.Fatalf("LoadEligibilityCache reload: %v", err)
	}
	if got, ok := reloaded.Lookup(ctx, now.Add(time.Minute), true); !ok || got.RepoConfigID != 123 {
		t.Fatalf("reloaded lookup = %+v, %v; want hit", got, ok)
	}
}
