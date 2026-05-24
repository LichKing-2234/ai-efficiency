package cmd

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ai-efficiency/ae-cli/config"
	"github.com/ai-efficiency/ae-cli/internal/auth"
	"github.com/ai-efficiency/ae-cli/internal/client"
	"github.com/ai-efficiency/ae-cli/internal/hooks"
	"github.com/ai-efficiency/ae-cli/internal/hookstate"
)

func runGitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %s failed: %v stderr=%s", strings.Join(args, " "), err, stderr.String())
	}
	return strings.TrimSpace(string(out))
}

type blockingHookUploader struct{}

func (blockingHookUploader) UploadHookEvent(ctx context.Context, _ hooks.HookEvent) error {
	<-ctx.Done()
	return ctx.Err()
}

type recordingHookUploader struct {
	events []hooks.HookEvent
}

func (r *recordingHookUploader) UploadHookEvent(ctx context.Context, ev hooks.HookEvent) error {
	r.events = append(r.events, ev)
	return nil
}

func TestHookPostCommitCommandUsesBoundedContext(t *testing.T) {
	repo := initRepoWithCommitForCmdTests(t)

	home := t.TempDir()
	t.Setenv("HOME", home)
	writeTestToken(t, home, "user:123")
	writePositiveEligibility(t, home, "github.com/acme/repo", 123)

	origUploader := newHookUploader
	origTimeout := hookCommandTimeout
	newHookUploader = func() hooks.Uploader { return blockingHookUploader{} }
	hookCommandTimeout = 25 * time.Millisecond
	t.Cleanup(func() {
		newHookUploader = origUploader
		hookCommandTimeout = origTimeout
	})

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	defer func() { _ = os.Chdir(wd) }()
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("Chdir(repo): %v", err)
	}

	start := time.Now()
	if err := hookPostCommitCmd.RunE(hookPostCommitCmd, nil); err != nil {
		t.Fatalf("hook post-commit RunE: %v", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("hook post-commit elapsed = %s, want bounded return", elapsed)
	}
}

func TestHookCommandHasPostRewriteSubcommand(t *testing.T) {
	var found bool
	for _, c := range hookCmd.Commands() {
		if c.Name() == "post-rewrite" {
			found = true
			if !c.Hidden {
				t.Fatalf("expected hook post-rewrite to be hidden")
			}
		}
	}
	if !found {
		t.Fatalf("expected hidden subcommand 'ae-cli hook post-rewrite' to exist")
	}
}

func TestHookPostCommitSkipsUnknownRepoWithoutQueue(t *testing.T) {
	repo := initRepoWithCommitForCmdTests(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	u := &recordingHookUploader{}
	origUploader := newHookUploader
	newHookUploader = func() hooks.Uploader { return u }
	t.Cleanup(func() { newHookUploader = origUploader })

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	defer func() { _ = os.Chdir(wd) }()
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("Chdir(repo): %v", err)
	}

	if err := hookPostCommitCmd.RunE(hookPostCommitCmd, nil); err != nil {
		t.Fatalf("hook post-commit RunE: %v", err)
	}
	if len(u.events) != 0 {
		t.Fatalf("events = %+v, want none for unknown repo", u.events)
	}
	if _, err := os.Stat(filepath.Join(home, ".ae-cli", "state", "attribution")); !os.IsNotExist(err) {
		t.Fatalf("unexpected durable attribution state for unknown repo: %v", err)
	}
}

func TestHookPostCommitUsesResolvedRepoConfigID(t *testing.T) {
	repo := initRepoWithCommitForCmdTests(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeTestToken(t, home, "user:123")
	writePositiveEligibility(t, home, "github.com/acme/repo", 123)

	u := &recordingHookUploader{}
	origUploader := newHookUploader
	newHookUploader = func() hooks.Uploader { return u }
	t.Cleanup(func() { newHookUploader = origUploader })

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	defer func() { _ = os.Chdir(wd) }()
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("Chdir(repo): %v", err)
	}

	if err := hookPostCommitCmd.RunE(hookPostCommitCmd, nil); err != nil {
		t.Fatalf("hook post-commit RunE: %v", err)
	}
	if len(u.events) != 1 {
		t.Fatalf("events = %+v, want one event", u.events)
	}
	if u.events[0].RepoConfigID != 123 {
		t.Fatalf("repo_config_id = %d, want 123 in %+v", u.events[0].RepoConfigID, u.events[0])
	}
	if u.events[0].AuthSubject != "user:123" || u.events[0].RepoKey != "github.com/acme/repo" {
		t.Fatalf("event binding = %+v", u.events[0])
	}
}

func TestHookPostCommitResolvesCacheMissAndCachesPositive(t *testing.T) {
	repo := initRepoWithCommitForCmdTests(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	var resolveCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/repos/resolve-remote" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		resolveCalls++
		var req client.ResolveRepoRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.RemoteURL != "https://github.com/acme/repo.git" {
			t.Fatalf("remote_url = %q", req.RemoteURL)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": client.RepoEligibilityResponse{
				Eligible:     true,
				RepoConfigID: 456,
				RepoKey:      "github.com/acme/repo",
				FullName:     "acme/repo",
				CloneURL:     "https://github.com/acme/repo.git",
				Status:       "active",
			},
		})
	}))
	defer srv.Close()

	writeTestTokenForServer(t, home, srv.URL, "user:123")
	withHookAPIClient(t, srv.URL, "test-access-token")

	u := &recordingHookUploader{}
	origUploader := newHookUploader
	newHookUploader = func() hooks.Uploader { return u }
	t.Cleanup(func() { newHookUploader = origUploader })

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	defer func() { _ = os.Chdir(wd) }()
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("Chdir(repo): %v", err)
	}

	if err := hookPostCommitCmd.RunE(hookPostCommitCmd, nil); err != nil {
		t.Fatalf("hook post-commit RunE: %v", err)
	}
	if resolveCalls != 1 {
		t.Fatalf("resolve calls = %d, want 1", resolveCalls)
	}
	if len(u.events) != 1 || u.events[0].RepoConfigID != 456 {
		t.Fatalf("events = %+v, want one event with repo_config_id 456", u.events)
	}

	cache, err := hookstate.LoadEligibilityCache()
	if err != nil {
		t.Fatalf("LoadEligibilityCache: %v", err)
	}
	rec, ok := cache.Lookup(hookstate.Context{
		ServerURL:   srv.URL,
		AuthSubject: "user:123",
		RepoKey:     "github.com/acme/repo",
	}, time.Now(), true)
	if !ok || rec.RepoConfigID != 456 {
		t.Fatalf("cached record = %+v, ok=%t", rec, ok)
	}
}

func TestHookPostCommitAllowsImmediateResolveWithoutStableSubject(t *testing.T) {
	repo := initRepoWithCommitForCmdTests(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": client.RepoEligibilityResponse{
				Eligible:     true,
				RepoConfigID: 789,
				RepoKey:      "github.com/acme/repo",
				FullName:     "acme/repo",
				CloneURL:     "https://github.com/acme/repo.git",
				Status:       "active",
			},
		})
	}))
	defer srv.Close()

	if err := auth.WriteToken(filepath.Join(home, ".ae-cli", "token.json"), &auth.TokenFile{
		AccessToken:  "opaque-token",
		RefreshToken: "test-refresh-token",
		ExpiresAt:    time.Now().Add(time.Hour),
		ServerURL:    srv.URL,
	}); err != nil {
		t.Fatalf("WriteToken: %v", err)
	}
	withHookAPIClient(t, srv.URL, "opaque-token")

	u := &recordingHookUploader{}
	origUploader := newHookUploader
	newHookUploader = func() hooks.Uploader { return u }
	t.Cleanup(func() { newHookUploader = origUploader })

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	defer func() { _ = os.Chdir(wd) }()
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("Chdir(repo): %v", err)
	}

	if err := hookPostCommitCmd.RunE(hookPostCommitCmd, nil); err != nil {
		t.Fatalf("hook post-commit RunE: %v", err)
	}
	if len(u.events) != 1 {
		t.Fatalf("events = %+v, want one immediate upload", u.events)
	}
	if u.events[0].RepoConfigID != 789 || u.events[0].AuthSubject != "" {
		t.Fatalf("event binding = %+v", u.events[0])
	}

	cache, err := hookstate.LoadEligibilityCache()
	if err != nil {
		t.Fatalf("LoadEligibilityCache: %v", err)
	}
	if len(cache.Repos) != 0 || len(cache.Negative) != 0 {
		t.Fatalf("cache = %+v, want no durable eligibility entries without stable subject", cache)
	}
}

func writeTestToken(t *testing.T, home, authSubject string) {
	t.Helper()
	writeTestTokenForServer(t, home, "https://ae.example.com", authSubject)
}

func writeTestTokenForServer(t *testing.T, home, serverURL, authSubject string) {
	t.Helper()
	tokenPath := filepath.Join(home, ".ae-cli", "token.json")
	if err := auth.WriteToken(tokenPath, &auth.TokenFile{
		AccessToken:  "test-access-token",
		RefreshToken: "test-refresh-token",
		ExpiresAt:    time.Now().Add(time.Hour),
		ServerURL:    serverURL,
		AuthSubject:  authSubject,
	}); err != nil {
		t.Fatalf("WriteToken: %v", err)
	}
}

func writePositiveEligibility(t *testing.T, home, repoKey string, repoConfigID int) {
	t.Helper()
	t.Setenv("HOME", home)
	cache, err := hookstate.LoadEligibilityCache()
	if err != nil {
		t.Fatalf("LoadEligibilityCache: %v", err)
	}
	cache.PutPositive(hookstate.Context{
		ServerURL:   "https://ae.example.com",
		AuthSubject: "user:123",
		RepoKey:     repoKey,
	}, client.RepoEligibilityResponse{
		Eligible:     true,
		RepoConfigID: repoConfigID,
		RepoKey:      repoKey,
		FullName:     "acme/repo",
		CloneURL:     "https://github.com/acme/repo.git",
		Status:       "active",
	}, time.Now())
	if err := cache.Save(); err != nil {
		t.Fatalf("Save eligibility: %v", err)
	}
}

func withHookAPIClient(t *testing.T, serverURL, token string) {
	t.Helper()
	oldCfg := cfg
	oldClient := apiClient
	cfg = &config.Config{Server: config.ServerConfig{URL: serverURL, Token: token}}
	apiClient = client.New(serverURL, token)
	t.Cleanup(func() {
		cfg = oldCfg
		apiClient = oldClient
	})
}
