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
	"sync/atomic"
	"testing"
	"time"

	"github.com/ai-efficiency/ae-cli/config"
	"github.com/ai-efficiency/ae-cli/internal/auth"
	"github.com/ai-efficiency/ae-cli/internal/client"
	"github.com/ai-efficiency/ae-cli/internal/hooks"
	"github.com/ai-efficiency/ae-cli/internal/hookstate"
	"github.com/ai-efficiency/ae-cli/internal/reporting"
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

func TestHookPostCommitCommandUsesBoundedContextAndPersistsV2Trigger(t *testing.T) {
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
	gitCtx, err := hooks.DetectGitContext(repo)
	if err != nil {
		t.Fatal(err)
	}
	task, err := hooks.LoadSyncTask(gitCtx.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if task == nil || len(task.V2Triggers) != 1 || task.V2Triggers[0].Kind != "post-commit" {
		t.Fatalf("bounded hook v2 task = %+v", task)
	}
}

func TestHookBackgroundSyncRunsWithoutHookTimeout(t *testing.T) {
	repo := initRepoWithCommitForCmdTests(t)

	home := t.TempDir()
	t.Setenv("HOME", home)
	writeTestToken(t, home, "user:123")
	writePositiveEligibility(t, home, "github.com/acme/repo", 123)

	origRun := runBackgroundSyncTask
	var ctxErr error
	runBackgroundSyncTask = func(ctx context.Context, execCtx hooks.ExecutionContext, uploader hooks.Uploader) error {
		ctxErr = ctx.Err()
		if execCtx.RepoConfigID != 123 {
			t.Fatalf("repo_config_id = %d, want 123", execCtx.RepoConfigID)
		}
		return nil
	}
	t.Cleanup(func() { runBackgroundSyncTask = origRun })

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	defer func() { _ = os.Chdir(wd) }()
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("Chdir(repo): %v", err)
	}

	if err := hookBackgroundSyncCmd.RunE(hookBackgroundSyncCmd, nil); err != nil {
		t.Fatalf("hook background-sync RunE: %v", err)
	}
	if ctxErr != nil {
		t.Fatalf("background sync context err = %v, want nil", ctxErr)
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

func TestHookResolveUsesReporterTokenWithoutOAuthLoginState(t *testing.T) {
	repo := initRepoWithCommitForCmdTests(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/attribution/repos/resolve-remote" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer reporter-token" {
			t.Fatalf("authorization = %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": client.RepoEligibilityResponse{
			Eligible: true, RepoConfigID: 123, RepoKey: "github.com/acme/repo",
		}})
	}))
	defer server.Close()
	if err := reporting.Save("", &reporting.Config{
		Version: 1, InstallationID: "11111111-1111-4111-8111-111111111111",
		ServerURL: server.URL, AuthSubject: "user:123", ReporterToken: "reporter-token", ReportingEnabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	originalAPIClient := apiClient
	apiClient = nil
	t.Cleanup(func() { apiClient = originalAPIClient })

	gitCtx, err := hooks.DetectGitContext(repo)
	if err != nil {
		t.Fatal(err)
	}
	execCtx, ok := resolveHookExecutionContext(context.Background(), gitCtx)
	if !ok || execCtx.RepoConfigID != 123 || execCtx.AuthSubject != "user:123" {
		t.Fatalf("execution context = %+v, ok=%t", execCtx, ok)
	}
}

func TestHookPostCommitUsesExpiredPositiveEligibilityWhenRefreshTimesOut(t *testing.T) {
	repo := initRepoWithCommitForCmdTests(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	var resolveCalls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/repos/resolve-remote" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		atomic.AddInt32(&resolveCalls, 1)
		time.Sleep(200 * time.Millisecond)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": client.RepoEligibilityResponse{
				Eligible:     true,
				RepoConfigID: 999,
				RepoKey:      "github.com/acme/repo",
			},
		})
	}))
	defer srv.Close()

	writeTestTokenForServer(t, home, srv.URL, "user:123")
	writePositiveEligibilityForServerAt(t, home, srv.URL, "user:123", "github.com/acme/repo", 321, time.Now().Add(-25*time.Hour))
	withHookAPIClient(t, srv.URL, "test-access-token")

	u := &recordingHookUploader{}
	origUploader := newHookUploader
	newHookUploader = func() hooks.Uploader { return u }
	origResolveTimeout := hookEligibilityResolveTimeout
	hookEligibilityResolveTimeout = 25 * time.Millisecond
	t.Cleanup(func() {
		newHookUploader = origUploader
		hookEligibilityResolveTimeout = origResolveTimeout
	})

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
	if got := atomic.LoadInt32(&resolveCalls); got != 1 {
		t.Fatalf("resolve calls = %d, want 1 refresh attempt before stale fallback", got)
	}
	if len(u.events) != 1 {
		t.Fatalf("events = %+v, want one event from stale positive eligibility", u.events)
	}
	if u.events[0].RepoConfigID != 321 {
		t.Fatalf("repo_config_id = %d, want stale repo_config_id 321 in %+v", u.events[0].RepoConfigID, u.events[0])
	}
	if u.events[0].AuthSubject != "user:123" || u.events[0].RepoKey != "github.com/acme/repo" {
		t.Fatalf("event binding = %+v", u.events[0])
	}
}

func TestHookPostCommitQueuesUnresolvedWhenInitialResolveTimesOut(t *testing.T) {
	repo := initRepoWithCommitForCmdTests(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusGatewayTimeout)
	}))
	defer srv.Close()
	writeTestTokenForServer(t, home, srv.URL, "user:123")
	withHookAPIClient(t, srv.URL, "test-access-token")

	origResolveTimeout := hookEligibilityResolveTimeout
	hookEligibilityResolveTimeout = 25 * time.Millisecond
	t.Cleanup(func() { hookEligibilityResolveTimeout = origResolveTimeout })

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
	items, err := hooks.ListUnresolvedHookEvents()
	if err != nil {
		t.Fatalf("ListUnresolvedHookEvents: %v", err)
	}
	if len(items) != 1 || items[0].RemoteURL != "https://github.com/acme/repo.git" || items[0].CommitSHA == "" {
		t.Fatalf("unresolved items = %+v, want unresolved post-commit", items)
	}
	capturedAt, err := time.Parse(time.RFC3339Nano, items[0].CapturedAt)
	if err != nil {
		t.Fatalf("CapturedAt = %q, want RFC3339Nano: %v", items[0].CapturedAt, err)
	}
	if !strings.Contains(items[0].CapturedAt, ".") || capturedAt.Nanosecond() == 0 {
		t.Fatalf("CapturedAt = %q, want preserved subsecond precision", items[0].CapturedAt)
	}
}

func TestHookPostRewriteQueuesUnresolvedWhenInitialResolveTimesOut(t *testing.T) {
	repo := initRepoWithCommitForCmdTests(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusGatewayTimeout)
	}))
	defer srv.Close()
	writeTestTokenForServer(t, home, srv.URL, "user:123")
	withHookAPIClient(t, srv.URL, "test-access-token")

	origResolveTimeout := hookEligibilityResolveTimeout
	hookEligibilityResolveTimeout = 25 * time.Millisecond
	t.Cleanup(func() { hookEligibilityResolveTimeout = origResolveTimeout })

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	if _, err := w.WriteString("oldsha1 newsha1\n"); err != nil {
		t.Fatalf("write stdin: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close stdin writer: %v", err)
	}
	origStdin := os.Stdin
	os.Stdin = r
	t.Cleanup(func() {
		os.Stdin = origStdin
		_ = r.Close()
	})

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	defer func() { _ = os.Chdir(wd) }()
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("Chdir(repo): %v", err)
	}

	if err := hookPostRewriteCmd.RunE(hookPostRewriteCmd, []string{"amend"}); err != nil {
		t.Fatalf("hook post-rewrite RunE: %v", err)
	}
	items, err := hooks.ListUnresolvedHookEvents()
	if err != nil {
		t.Fatalf("ListUnresolvedHookEvents: %v", err)
	}
	if len(items) != 1 || items[0].Kind != "post-rewrite" || items[0].RemoteURL != "https://github.com/acme/repo.git" || items[0].OldCommitSHA != "oldsha1" || items[0].NewCommitSHA != "newsha1" || items[0].RewriteType != "amend" {
		t.Fatalf("unresolved items = %+v, want unresolved post-rewrite", items)
	}
	capturedAt, err := time.Parse(time.RFC3339Nano, items[0].CapturedAt)
	if err != nil {
		t.Fatalf("CapturedAt = %q, want RFC3339Nano: %v", items[0].CapturedAt, err)
	}
	if !strings.Contains(items[0].CapturedAt, ".") || capturedAt.Nanosecond() == 0 {
		t.Fatalf("CapturedAt = %q, want preserved subsecond precision", items[0].CapturedAt)
	}
}

func TestHookPostCommitDoesNotQueueUnresolvedWhenRepoIsExplicitlyIneligible(t *testing.T) {
	repo := initRepoWithCommitForCmdTests(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": client.RepoEligibilityResponse{
				Eligible: false,
				RepoKey:  "github.com/acme/repo",
				Reason:   "not_found",
			},
		})
	}))
	defer srv.Close()
	writeTestTokenForServer(t, home, srv.URL, "user:123")
	withHookAPIClient(t, srv.URL, "test-access-token")

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
	items, err := hooks.ListUnresolvedHookEvents()
	if err != nil {
		t.Fatalf("ListUnresolvedHookEvents: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("unresolved items = %+v, want none for explicitly ineligible repo", items)
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

func TestHookPostCommitDefaultResolveTimeoutCoversHealthySlowResolve(t *testing.T) {
	repo := initRepoWithCommitForCmdTests(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/repos/resolve-remote" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		time.Sleep(1200 * time.Millisecond)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": client.RepoEligibilityResponse{
				Eligible:     true,
				RepoConfigID: 654,
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
	if len(u.events) != 1 || u.events[0].RepoConfigID != 654 {
		t.Fatalf("events = %+v, want one event with repo_config_id 654", u.events)
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
	writePositiveEligibilityForServerAt(t, home, "https://ae.example.com", "user:123", repoKey, repoConfigID, time.Now())
}

func writePositiveEligibilityForServerAt(t *testing.T, home, serverURL, authSubject, repoKey string, repoConfigID int, resolvedAt time.Time) {
	t.Helper()
	t.Setenv("HOME", home)
	cache, err := hookstate.LoadEligibilityCache()
	if err != nil {
		t.Fatalf("LoadEligibilityCache: %v", err)
	}
	cache.PutPositive(hookstate.Context{
		ServerURL:   serverURL,
		AuthSubject: authSubject,
		RepoKey:     repoKey,
	}, client.RepoEligibilityResponse{
		Eligible:     true,
		RepoConfigID: repoConfigID,
		RepoKey:      repoKey,
		FullName:     "acme/repo",
		CloneURL:     "https://github.com/acme/repo.git",
		Status:       "active",
	}, resolvedAt)
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
