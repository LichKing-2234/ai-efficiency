package hooks

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ai-efficiency/ae-cli/internal/hookstate"
)

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
	if err := DisableRepo(repo); err != nil {
		t.Fatalf("DisableRepo: %v", err)
	}
	if got := gitConfigOptional(t, repo, "--local", "--get", "core.hooksPath"); got != "" {
		t.Fatalf("local hooksPath = %q, want unset", got)
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
	if group.PendingCount != 1 || group.UploadedCount != 1 || group.FailedCount != 1 || group.LastError != "backend unavailable" {
		t.Fatalf("group counts/error = %+v, want pending/uploaded/failed summary", group)
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
