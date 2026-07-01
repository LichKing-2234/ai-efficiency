package hooks

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ai-efficiency/ae-cli/internal/client"
	"github.com/ai-efficiency/ae-cli/internal/hookstate"
)

type recordingBatchResolver struct {
	requests []client.HookEligibleRepoRequest
	resp     client.BatchHookEligibleResponse
}

func (r *recordingBatchResolver) BatchHookEligible(_ context.Context, repos []client.HookEligibleRepoRequest) (*client.BatchHookEligibleResponse, error) {
	r.requests = append(r.requests, repos...)
	return &r.resp, nil
}

type fakeRepoResolver struct {
	resp *client.RepoEligibilityResponse
}

func (f *fakeRepoResolver) ResolveRepoFromRemote(_ context.Context, _ client.ResolveRepoRequest) (*client.RepoEligibilityResponse, error) {
	return f.resp, nil
}

func git(t *testing.T, dir string, args ...string) string {
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

func initRepoWithCommit(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	git(t, dir, "init")
	git(t, dir, "config", "user.email", "t@example.com")
	git(t, dir, "config", "user.name", "t")
	git(t, dir, "remote", "add", "origin", "https://repo-host.example.com/org/repo.git")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "init")
	return dir
}

func TestDetectGitContextDerivesWorkspaceAndRepoKey(t *testing.T) {
	repo := initRepoWithCommit(t)

	ctx, err := DetectGitContext(repo)
	if err != nil {
		t.Fatalf("DetectGitContext: %v", err)
	}
	wantRepo, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatalf("EvalSymlinks(repo): %v", err)
	}
	if ctx.RepoRoot != wantRepo {
		t.Fatalf("RepoRoot = %q, want %q", ctx.RepoRoot, wantRepo)
	}
	if ctx.RemoteURL != "https://repo-host.example.com/org/repo.git" {
		t.Fatalf("RemoteURL = %q", ctx.RemoteURL)
	}
	if ctx.RepoKey != "repo-host.example.com/org/repo" {
		t.Fatalf("RepoKey = %q", ctx.RepoKey)
	}
	if strings.TrimSpace(ctx.WorkspaceID) == "" {
		t.Fatalf("WorkspaceID is empty")
	}
}

func TestRenderManagedHookScriptResolvesRuntimeBinary(t *testing.T) {
	script := RenderManagedHookScript("post-commit", "0.1.0")
	for _, want := range []string{"AE_CLI_BIN", "$HOME/.local/bin/ae-cli", "command -v ae-cli", "hook post-commit"} {
		if !strings.Contains(script, want) {
			t.Fatalf("script missing %q:\n%s", want, script)
		}
	}
	if strings.Contains(script, "/tmp/ae-cli") || strings.Contains(script, "ae-cli-local") || strings.Contains(script, "AE_CLI_HOOK_BIN") {
		t.Fatalf("script contains forbidden binary reference:\n%s", script)
	}
}

func TestRenderPostRewriteManagedScriptPreservesStdin(t *testing.T) {
	script := RenderManagedHookScript("post-rewrite", "0.1.0")
	for _, want := range []string{"cat >\"$tmp\"", "hook post-rewrite", "<\"$tmp\"", "rm -f \"$tmp\""} {
		if !strings.Contains(script, want) {
			t.Fatalf("post-rewrite script missing %q:\n%s", want, script)
		}
	}
}

func TestParseTemplateVersion(t *testing.T) {
	data := []byte("#!/bin/sh\n# ae-cli-managed-hook: template_version=2 generator_version=test\n")
	version, ok := ParseTemplateVersion(data)
	if !ok || version != hookstate.CurrentHookTemplateVersion {
		t.Fatalf("ParseTemplateVersion = %d, %v", version, ok)
	}
}

func TestGlobalManagedHooksPathUsesAeCliRoot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	got, err := GlobalManagedHooksPath()
	if err != nil {
		t.Fatalf("GlobalManagedHooksPath: %v", err)
	}
	if want := filepath.Join(home, ".ae-cli", "git-hooks"); got != want {
		t.Fatalf("GlobalManagedHooksPath() = %q, want %q", got, want)
	}
}

func TestEnableRepoForceDoesNotPreserveExistingHook(t *testing.T) {
	repo := initRepoWithCommit(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	defaultHook := filepath.Join(git(t, repo, "rev-parse", "--git-path", "hooks"), "post-commit")
	if !filepath.IsAbs(defaultHook) {
		defaultHook = filepath.Join(repo, defaultHook)
	}
	if err := os.MkdirAll(filepath.Dir(defaultHook), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(defaultHook, []byte("#!/bin/sh\necho legacy\n"), 0o755); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := EnableRepo(InstallOptions{CWD: repo, Force: true, GeneratorVersion: "test"}); err != nil {
		t.Fatalf("EnableRepo: %v", err)
	}
	hooksPath := git(t, repo, "config", "--get", "core.hooksPath")
	managed := filepath.Join(hooksPath, "post-commit")
	data, err := os.ReadFile(managed)
	if err != nil {
		t.Fatalf("read managed hook: %v", err)
	}
	if strings.Contains(string(data), "legacy") {
		t.Fatalf("managed hook should not chain legacy hook:\n%s", string(data))
	}
	registry, err := hookstate.LoadInstallations()
	if err != nil {
		t.Fatalf("LoadInstallations: %v", err)
	}
	rec, ok := registry.Find(hookstate.InstallationRecord{
		Mode:         "local",
		GitCommonDir: git(t, repo, "rev-parse", "--absolute-git-dir"),
		ConfigScope:  string(ConfigScopeLocal),
		HooksPath:    hooksPath,
	})
	if !ok || !rec.Enabled || rec.TemplateVersion != hookstate.CurrentHookTemplateVersion {
		t.Fatalf("installation record = %+v, %v", rec, ok)
	}
}

func TestEnableRepoRefusesExecutableDefaultHookWithoutForce(t *testing.T) {
	repo := initRepoWithCommit(t)
	t.Setenv("HOME", t.TempDir())
	defaultHook := filepath.Join(git(t, repo, "rev-parse", "--git-path", "hooks"), "post-commit")
	if !filepath.IsAbs(defaultHook) {
		defaultHook = filepath.Join(repo, defaultHook)
	}
	if err := os.MkdirAll(filepath.Dir(defaultHook), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(defaultHook, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := EnableRepo(InstallOptions{CWD: repo, Force: false, NonInteractive: true}); err == nil {
		t.Fatalf("expected refusal without force")
	}
}

func TestEnableRepoUnderAEGlobalIgnoresBypassedDefaultHooksWithoutForce(t *testing.T) {
	repo := initRepoWithCommit(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, ".gitconfig"))
	globalDir := filepath.Join(home, ".ae-cli", "git-hooks")
	git(t, repo, "config", "--global", "core.hooksPath", globalDir)
	defaultHook := filepath.Join(git(t, repo, "rev-parse", "--git-path", "hooks"), "post-commit")
	if !filepath.IsAbs(defaultHook) {
		defaultHook = filepath.Join(repo, defaultHook)
	}
	if err := os.MkdirAll(filepath.Dir(defaultHook), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(defaultHook, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write: %v", err)
	}

	if err := EnableRepo(InstallOptions{CWD: repo, Force: false, NonInteractive: true, GeneratorVersion: "test"}); err != nil {
		t.Fatalf("EnableRepo under AE global should be allowed without --force: %v", err)
	}
	if got := git(t, repo, "config", "--local", "--get", "core.hooksPath"); !strings.HasSuffix(got, ".git/ae-hooks") {
		t.Fatalf("local hooksPath = %q, want repo-local AE hook", got)
	}
}

func TestEnableGlobalForceDoesNotModifyRepoHooksPath(t *testing.T) {
	repo := initRepoWithCommit(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	git(t, repo, "config", "core.hooksPath", ".git/custom-hooks")

	if err := EnableGlobal(InstallOptions{CWD: repo, Force: true, GeneratorVersion: "test"}); err != nil {
		t.Fatalf("EnableGlobal: %v", err)
	}
	if got := git(t, repo, "config", "--local", "--get", "core.hooksPath"); got != ".git/custom-hooks" {
		t.Fatalf("local hooksPath = %q, want unchanged", got)
	}
	globalPath := git(t, repo, "config", "--global", "--get", "core.hooksPath")
	if want := filepath.Join(home, ".ae-cli", "git-hooks"); globalPath != want {
		t.Fatalf("global hooksPath = %q, want %q", globalPath, want)
	}
}

func TestDisableRepoOnlyRemovesAEManagedRepoPath(t *testing.T) {
	repo := initRepoWithCommit(t)
	t.Setenv("HOME", t.TempDir())
	if err := EnableRepo(InstallOptions{CWD: repo, Force: true, GeneratorVersion: "test"}); err != nil {
		t.Fatalf("EnableRepo: %v", err)
	}
	result, err := DisableRepoWithResult(repo)
	if err != nil {
		t.Fatalf("DisableRepoWithResult: %v", err)
	}
	if len(result.DisabledScopes) != 1 || result.DisabledScopes[0] != ConfigScopeLocal {
		t.Fatalf("DisabledScopes = %+v, want local", result.DisabledScopes)
	}
	if got := gitConfigOptional(t, repo, "--local", "--get", "core.hooksPath"); got != "" {
		t.Fatalf("local hooksPath = %q, want unset", got)
	}
}

func TestDisableRepoDisablesInstallationRecord(t *testing.T) {
	repo := initRepoWithCommit(t)
	t.Setenv("HOME", t.TempDir())
	if err := EnableRepo(InstallOptions{CWD: repo, Force: true, GeneratorVersion: "test"}); err != nil {
		t.Fatalf("EnableRepo: %v", err)
	}
	hooksPath := git(t, repo, "config", "--local", "--get", "core.hooksPath")

	if err := DisableRepo(repo); err != nil {
		t.Fatalf("DisableRepo: %v", err)
	}

	registry, err := hookstate.LoadInstallations()
	if err != nil {
		t.Fatalf("LoadInstallations: %v", err)
	}
	rec, ok := registry.Find(hookstate.InstallationRecord{
		Mode:         "local",
		GitCommonDir: git(t, repo, "rev-parse", "--absolute-git-dir"),
		ConfigScope:  string(ConfigScopeLocal),
		HooksPath:    hooksPath,
	})
	if !ok {
		t.Fatalf("missing installation record")
	}
	if rec.Enabled {
		t.Fatalf("record still enabled after DisableRepo: %+v", rec)
	}
}

func TestDisableRepoReconcilesAlreadyAbsentEnabledRecord(t *testing.T) {
	repo := initRepoWithCommit(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	gitCtx, err := DetectGitContext(repo)
	if err != nil {
		t.Fatalf("DetectGitContext: %v", err)
	}
	managed, err := RepoManagedHooksPath(gitCtx.GitCommonDir)
	if err != nil {
		t.Fatalf("RepoManagedHooksPath: %v", err)
	}
	registry, err := hookstate.LoadInstallations()
	if err != nil {
		t.Fatalf("LoadInstallations: %v", err)
	}
	registry.Upsert(hookstate.InstallationRecord{
		Mode:            "local",
		GitDir:          gitCtx.GitDir,
		GitCommonDir:    gitCtx.GitCommonDir,
		ConfigScope:     string(ConfigScopeLocal),
		HooksPath:       managed,
		Enabled:         true,
		TemplateVersion: hookstate.CurrentHookTemplateVersion,
		UpdatedAt:       time.Now(),
	})
	if err := registry.Save(); err != nil {
		t.Fatalf("Save registry: %v", err)
	}

	if err := DisableRepo(repo); err != nil {
		t.Fatalf("DisableRepo: %v", err)
	}
	registry, err = hookstate.LoadInstallations()
	if err != nil {
		t.Fatalf("LoadInstallations after disable: %v", err)
	}
	rec, ok := registry.Find(hookstate.InstallationRecord{
		Mode:         "local",
		GitCommonDir: gitCtx.GitCommonDir,
		ConfigScope:  string(ConfigScopeLocal),
		HooksPath:    managed,
	})
	if !ok || rec.Enabled {
		t.Fatalf("record = %+v, ok=%v, want disabled existing record", rec, ok)
	}
}

func TestDisableRepoRemovesExposedLowerPrecedenceAERepoLayer(t *testing.T) {
	repo := initRepoWithCommit(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	gitCtx, err := DetectGitContext(repo)
	if err != nil {
		t.Fatalf("DetectGitContext: %v", err)
	}
	managed, err := RepoManagedHooksPath(gitCtx.GitCommonDir)
	if err != nil {
		t.Fatalf("RepoManagedHooksPath: %v", err)
	}
	git(t, repo, "config", "extensions.worktreeConfig", "true")
	git(t, repo, "config", "--local", "core.hooksPath", managed)
	git(t, repo, "config", "--worktree", "core.hooksPath", managed)

	if err := DisableRepo(repo); err != nil {
		t.Fatalf("DisableRepo: %v", err)
	}
	if got := gitConfigOptional(t, repo, "--worktree", "--get", "core.hooksPath"); got != "" {
		t.Fatalf("worktree hooksPath = %q, want unset", got)
	}
	if got := gitConfigOptional(t, repo, "--local", "--get", "core.hooksPath"); got != "" {
		t.Fatalf("local hooksPath = %q, want lower-precedence AE layer unset too", got)
	}
	status, err := InspectEffectiveHookConfig(repo, gitCtx)
	if err != nil {
		t.Fatalf("InspectEffectiveHookConfig: %v", err)
	}
	if status.Mode == HookModeAERepo {
		t.Fatalf("effective mode = %s, want AE repo disabled", status.Mode)
	}
}

func TestRefreshManagedInstallationsRewritesActiveGlobalFromGitConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, ".gitconfig"))
	globalDir := filepath.Join(home, ".ae-cli", "git-hooks")
	git(t, home, "config", "--global", "core.hooksPath", globalDir)

	if err := RefreshManagedInstallations("test-version", io.Discard); err != nil {
		t.Fatalf("RefreshManagedInstallations: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(globalDir, "post-commit"))
	if err != nil {
		t.Fatalf("read hook: %v", err)
	}
	if !strings.Contains(string(data), "template_version=2") {
		t.Fatalf("stale script: %s", data)
	}
}

func TestRefreshManagedInstallationsSkipsDisabledRepoRecords(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	disabledPath := filepath.Join(home, "repo", ".git", "ae-hooks")
	registry, err := hookstate.LoadInstallations()
	if err != nil {
		t.Fatalf("LoadInstallations: %v", err)
	}
	registry.Upsert(hookstate.InstallationRecord{
		Mode:            "repo",
		GitCommonDir:    filepath.Join(home, "repo", ".git"),
		ConfigScope:     "local",
		HooksPath:       disabledPath,
		Enabled:         false,
		TemplateVersion: 1,
		UpdatedAt:       time.Now(),
	})
	if err := registry.Save(); err != nil {
		t.Fatalf("Save registry: %v", err)
	}
	if err := RefreshManagedInstallations("test-version", io.Discard); err != nil {
		t.Fatalf("RefreshManagedInstallations: %v", err)
	}
	if _, err := os.Stat(filepath.Join(disabledPath, "post-commit")); !os.IsNotExist(err) {
		t.Fatalf("disabled repo hook should not be rewritten, stat err=%v", err)
	}
}

func TestRefreshManagedInstallationsSkipsDeletedRepoLocalRecords(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	deletedCommonDir := filepath.Join(home, "deleted-repo", ".git")
	deletedHooksPath := filepath.Join(deletedCommonDir, "ae-hooks")
	registry, err := hookstate.LoadInstallations()
	if err != nil {
		t.Fatalf("LoadInstallations: %v", err)
	}
	registry.Upsert(hookstate.InstallationRecord{
		Mode:            "local",
		GitCommonDir:    deletedCommonDir,
		ConfigScope:     "local",
		HooksPath:       deletedHooksPath,
		Enabled:         true,
		TemplateVersion: 1,
		UpdatedAt:       time.Now(),
	})
	if err := registry.Save(); err != nil {
		t.Fatalf("Save registry: %v", err)
	}

	var out bytes.Buffer
	if err := RefreshManagedInstallations("test-version", &out); err != nil {
		t.Fatalf("RefreshManagedInstallations: %v", err)
	}
	if _, err := os.Stat(filepath.Join(deletedHooksPath, "post-commit")); !os.IsNotExist(err) {
		t.Fatalf("deleted repo-local hook should not be recreated, stat err=%v", err)
	}
	if !strings.Contains(out.String(), "skipped") {
		t.Fatalf("output = %q, want skipped diagnostic", out.String())
	}
}

func TestStatusForRepoReportsAERepoTemplate(t *testing.T) {
	repo := initRepoWithCommit(t)
	t.Setenv("HOME", t.TempDir())
	if err := EnableRepo(InstallOptions{CWD: repo, Force: true, GeneratorVersion: "test"}); err != nil {
		t.Fatalf("EnableRepo: %v", err)
	}
	status, err := StatusForRepo(StatusOptions{CWD: repo})
	if err != nil {
		t.Fatalf("StatusForRepo: %v", err)
	}
	if !status.RepoEnabled || status.EffectiveMode != HookModeAERepo || status.TemplateVersion != hookstate.CurrentHookTemplateVersion || status.TemplateStale {
		t.Fatalf("status = %+v", status)
	}
}

func TestStatusForRepoReportsCurrentEligibilityAndObservedRepo(t *testing.T) {
	repo := initRepoWithCommit(t)
	t.Setenv("HOME", t.TempDir())
	gitCtx, err := DetectGitContext(repo)
	if err != nil {
		t.Fatalf("DetectGitContext: %v", err)
	}
	binding := hookstate.Context{
		ServerURL:   "https://ae.example.com/",
		AuthSubject: "user:1",
		RepoKey:     gitCtx.RepoKey,
	}
	now := time.Now().Add(-time.Minute).UTC()
	cache, err := hookstate.LoadEligibilityCache()
	if err != nil {
		t.Fatalf("LoadEligibilityCache: %v", err)
	}
	cache.PutPositive(binding, client.RepoEligibilityResponse{
		Eligible:     true,
		RepoConfigID: 123,
		RepoKey:      gitCtx.RepoKey,
		Status:       "active",
	}, now)
	if err := cache.Save(); err != nil {
		t.Fatalf("Save cache: %v", err)
	}
	observed, err := hookstate.LoadObservedRepos()
	if err != nil {
		t.Fatalf("LoadObservedRepos: %v", err)
	}
	observed.Observe(binding, gitCtx.RemoteURL, now)
	if err := observed.Save(); err != nil {
		t.Fatalf("Save observed: %v", err)
	}

	status, err := StatusForRepo(StatusOptions{CWD: repo, Binding: binding})
	if err != nil {
		t.Fatalf("StatusForRepo: %v", err)
	}
	if status.EligibilityCache != "eligible repo_config_id=123" {
		t.Fatalf("EligibilityCache = %q", status.EligibilityCache)
	}
	if status.ObservedRepo != "bound" {
		t.Fatalf("ObservedRepo = %q", status.ObservedRepo)
	}
}

func TestRefreshCurrentWritesObservedRepoBinding(t *testing.T) {
	repo := initRepoWithCommit(t)
	t.Setenv("HOME", t.TempDir())
	gitCtx, err := DetectGitContext(repo)
	if err != nil {
		t.Fatalf("DetectGitContext: %v", err)
	}
	resolver := &fakeRepoResolver{resp: &client.RepoEligibilityResponse{
		Eligible:     true,
		RepoConfigID: 123,
		RepoKey:      gitCtx.RepoKey,
		Status:       "active",
	}}
	binding := hookstate.Context{ServerURL: "https://ae.example.com", AuthSubject: "user:123", RepoKey: gitCtx.RepoKey}

	if err := RefreshCurrent(context.Background(), resolver, repo, binding); err != nil {
		t.Fatalf("RefreshCurrent: %v", err)
	}
	observed, err := hookstate.LoadObservedRepos()
	if err != nil {
		t.Fatalf("LoadObservedRepos: %v", err)
	}
	matches := observed.Matching(binding)
	if len(matches) == 0 {
		t.Fatalf("expected observed repo binding")
	}
	if matches[0].ServerURL != "https://ae.example.com" || matches[0].AuthSubject != "user:123" || matches[0].RepoKey != gitCtx.RepoKey {
		t.Fatalf("observed = %+v, want current bound repo", matches[0])
	}
}

func TestRefreshObservedCallsBatchAndUpdatesCache(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	binding := hookstate.Context{ServerURL: "https://ae.example.com", AuthSubject: "user:123", RepoKey: "github.com/acme/repo"}
	observed, err := hookstate.LoadObservedRepos()
	if err != nil {
		t.Fatalf("LoadObservedRepos: %v", err)
	}
	now := time.Now()
	observed.Observe(binding, "https://github.com/acme/repo.git", now)
	if err := observed.Save(); err != nil {
		t.Fatalf("Save observed: %v", err)
	}
	resolver := &recordingBatchResolver{resp: client.BatchHookEligibleResponse{
		Repos: []client.RepoEligibilityResponse{{
			Eligible:     true,
			RepoConfigID: 123,
			RepoKey:      "github.com/acme/repo",
			Status:       "active",
		}},
		Version: client.RepoEligibilityVersion,
	}}

	if err := RefreshObserved(context.Background(), resolver, hookstate.Context{ServerURL: "https://ae.example.com", AuthSubject: "user:123"}); err != nil {
		t.Fatalf("RefreshObserved: %v", err)
	}
	if len(resolver.requests) != 1 || resolver.requests[0].RepoKey != "github.com/acme/repo" {
		t.Fatalf("requests = %+v, want observed repo", resolver.requests)
	}
	cache, err := hookstate.LoadEligibilityCache()
	if err != nil {
		t.Fatalf("LoadEligibilityCache: %v", err)
	}
	rec, ok := cache.Lookup(binding, time.Now(), true)
	if !ok || rec.RepoConfigID != 123 {
		t.Fatalf("cache lookup = %+v, %v, want positive repo_config_id 123", rec, ok)
	}
}

func TestStatusForRepoWithUploadsSummarizesQueueAndLedger(t *testing.T) {
	repo := initRepoWithCommit(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	gitCtx, err := DetectGitContext(repo)
	if err != nil {
		t.Fatalf("DetectGitContext: %v", err)
	}
	queue, err := NewWorkspaceQueue(gitCtx.WorkspaceID)
	if err != nil {
		t.Fatalf("NewWorkspaceQueue: %v", err)
	}
	if err := queue.Enqueue(HookEvent{
		Kind:         "post-commit",
		EventID:      "pending-1",
		ServerURL:    "https://ae.example.com",
		AuthSubject:  "user:1",
		RepoConfigID: 123,
		RepoKey:      gitCtx.RepoKey,
		WorkspaceID:  gitCtx.WorkspaceID,
		CommitSHA:    "abc123",
	}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	uploadedAt := time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC)
	if err := AppendLedger(gitCtx.WorkspaceID, LedgerRecord{
		Kind:         "checkpoint",
		DedupeKey:    "uploaded-1",
		ServerURL:    "https://ae.example.com",
		AuthSubject:  "user:1",
		RepoConfigID: 123,
		RepoKey:      gitCtx.RepoKey,
		WorkspaceID:  gitCtx.WorkspaceID,
		Status:       "uploaded",
		AttemptCount: 1,
		AttemptedAt:  uploadedAt,
		UploadedAt:   &uploadedAt,
	}); err != nil {
		t.Fatalf("AppendLedger uploaded: %v", err)
	}
	failedAt := uploadedAt.Add(time.Minute)
	if err := AppendLedger(gitCtx.WorkspaceID, LedgerRecord{
		Kind:         "tool_usage",
		DedupeKey:    "failed-1",
		ServerURL:    "https://ae.example.com",
		AuthSubject:  "user:1",
		RepoConfigID: 123,
		RepoKey:      gitCtx.RepoKey,
		WorkspaceID:  gitCtx.WorkspaceID,
		Status:       "failed",
		AttemptCount: 1,
		AttemptedAt:  failedAt,
		LastError:    "backend unavailable",
	}); err != nil {
		t.Fatalf("AppendLedger failed: %v", err)
	}
	if err := AppendLedger(gitCtx.WorkspaceID, LedgerRecord{
		Kind:         "checkpoint",
		DedupeKey:    "deferred-1",
		ServerURL:    "https://ae.example.com",
		AuthSubject:  "user:1",
		RepoConfigID: 123,
		RepoKey:      gitCtx.RepoKey,
		WorkspaceID:  gitCtx.WorkspaceID,
		Status:       "deferred",
		AttemptCount: 1,
		AttemptedAt:  failedAt.Add(time.Minute),
		LastError:    "context mismatch",
	}); err != nil {
		t.Fatalf("AppendLedger deferred: %v", err)
	}

	status, err := StatusForRepo(StatusOptions{CWD: repo, Uploads: true})
	if err != nil {
		t.Fatalf("StatusForRepo: %v", err)
	}
	if len(status.UploadGroups) != 1 {
		t.Fatalf("UploadGroups = %+v, want one group", status.UploadGroups)
	}
	group := status.UploadGroups[0]
	if group.ServerURL != "https://ae.example.com" || group.AuthSubject != "user:1" || group.RepoConfigID != 123 || group.RepoKey != gitCtx.RepoKey || group.WorkspaceID != gitCtx.WorkspaceID {
		t.Fatalf("group binding = %+v, want current binding", group)
	}
	if group.PendingCount != 1 || group.UploadedCount != 1 || group.FailedCount != 1 || group.DeferredCount != 1 || group.LastError != "context mismatch" {
		t.Fatalf("group counts/error = %+v, want pending/uploaded/failed/deferred summary", group)
	}
	if group.LastSuccessfulUpload == nil || !group.LastSuccessfulUpload.Equal(uploadedAt) {
		t.Fatalf("LastSuccessfulUpload = %v, want %v", group.LastSuccessfulUpload, uploadedAt)
	}
}

func gitConfigOptional(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"config"}, args...)...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
