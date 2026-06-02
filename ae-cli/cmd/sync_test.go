package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ai-efficiency/ae-cli/internal/attributionlocal"
	"github.com/ai-efficiency/ae-cli/internal/hooks"
)

func ptrTimeValue(t time.Time) *time.Time {
	return &t
}

func TestSyncStatusCommandIsRegistered(t *testing.T) {
	var found bool
	for _, c := range syncCmd.Commands() {
		if c.Name() == "status" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected sync status subcommand")
	}
}

func TestDoctorPrintsPendingSyncTask(t *testing.T) {
	repo := initRepoWithCommitForCmdTests(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeTestToken(t, home, "user:123")
	writePositiveEligibility(t, home, "github.com/acme/repo", 123)

	gitCtx, err := hooks.DetectGitContext(repo)
	if err != nil {
		t.Fatalf("DetectGitContext: %v", err)
	}
	task := hooks.SyncTask{
		WorkspaceID:     gitCtx.WorkspaceID,
		RepoRoot:        repo,
		ServerURL:       "https://ae.example.com",
		AuthSubject:     "user:123",
		RepoConfigID:    123,
		RepoKey:         gitCtx.RepoKey,
		Status:          hooks.SyncTaskStatusPending,
		LastRequestedAt: time.Date(2026, 5, 26, 9, 0, 0, 0, time.UTC),
		LastError:       "spawn failed",
		AttemptCount:    3,
	}
	if err := hooks.SaveSyncTask(task); err != nil {
		t.Fatalf("SaveSyncTask: %v", err)
	}

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	defer func() { _ = os.Chdir(wd) }()
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("Chdir(repo): %v", err)
	}

	buf := &bytes.Buffer{}
	doctorCmd.SetOut(buf)
	doctorCmd.SetErr(buf)
	if err := doctorCmd.RunE(doctorCmd, nil); err != nil {
		t.Fatalf("doctorCmd.RunE: %v", err)
	}
	output := buf.String()
	for _, want := range []string{"Sync Task: pending", "spawn failed", "attempt_count: 3"} {
		if !bytes.Contains(buf.Bytes(), []byte(want)) {
			t.Fatalf("doctor output missing %q:\n%s", want, output)
		}
	}
}

func TestDoctorRecoversCorruptSyncTask(t *testing.T) {
	repo := initRepoWithCommitForCmdTests(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeTestToken(t, home, "user:123")
	writePositiveEligibility(t, home, "github.com/acme/repo", 123)
	withWorkingDir(t, repo)

	gitCtx, err := hooks.DetectGitContext(repo)
	if err != nil {
		t.Fatalf("DetectGitContext: %v", err)
	}
	path, err := hooks.SyncTaskPath(gitCtx.WorkspaceID)
	if err != nil {
		t.Fatalf("SyncTaskPath: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte("{not-json"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	buf := &bytes.Buffer{}
	doctorCmd.SetOut(buf)
	doctorCmd.SetErr(buf)
	if err := doctorCmd.RunE(doctorCmd, nil); err != nil {
		t.Fatalf("doctorCmd.RunE: %v", err)
	}
	if !bytes.Contains(buf.Bytes(), []byte("corrupt sync task moved aside")) {
		t.Fatalf("doctor output missing corrupt recovery message:\n%s", buf.String())
	}
}

func TestSyncStatusPrintsRunningTask(t *testing.T) {
	repo := initRepoWithCommitForCmdTests(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeTestToken(t, home, "user:123")
	writePositiveEligibility(t, home, "github.com/acme/repo", 123)

	gitCtx, err := hooks.DetectGitContext(repo)
	if err != nil {
		t.Fatalf("DetectGitContext: %v", err)
	}
	now := time.Date(2026, 5, 26, 9, 30, 0, 0, time.UTC)
	task := hooks.SyncTask{
		WorkspaceID:     gitCtx.WorkspaceID,
		RepoRoot:        repo,
		ServerURL:       "https://ae.example.com",
		AuthSubject:     "user:123",
		RepoConfigID:    123,
		RepoKey:         gitCtx.RepoKey,
		Status:          hooks.SyncTaskStatusRunning,
		LastRequestedAt: now.Add(-5 * time.Minute),
		LastStartedAt:   &now,
		RunnerPID:       os.Getpid(),
		LeaseExpiresAt:  ptrTimeValue(now.Add(5 * time.Minute)),
	}
	if err := hooks.SaveSyncTask(task); err != nil {
		t.Fatalf("SaveSyncTask: %v", err)
	}

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	defer func() { _ = os.Chdir(wd) }()
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("Chdir(repo): %v", err)
	}

	buf := &bytes.Buffer{}
	syncStatusCmd.SetOut(buf)
	syncStatusCmd.SetErr(buf)
	if err := syncStatusCmd.RunE(syncStatusCmd, nil); err != nil {
		t.Fatalf("syncStatusCmd.RunE: %v", err)
	}
	output := buf.String()
	for _, want := range []string{"Sync Task: running", "runner_pid"} {
		if !bytes.Contains(buf.Bytes(), []byte(want)) {
			t.Fatalf("sync status output missing %q:\n%s", want, output)
		}
	}
}

func TestSyncStatusRecoversInactiveRunner(t *testing.T) {
	repo := initRepoWithCommitForCmdTests(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeTestToken(t, home, "user:123")
	writePositiveEligibility(t, home, "github.com/acme/repo", 123)

	gitCtx, err := hooks.DetectGitContext(repo)
	if err != nil {
		t.Fatalf("DetectGitContext: %v", err)
	}
	now := time.Now().UTC()
	task := hooks.SyncTask{
		WorkspaceID:     gitCtx.WorkspaceID,
		RepoRoot:        repo,
		ServerURL:       "https://ae.example.com",
		AuthSubject:     "user:123",
		RepoConfigID:    123,
		RepoKey:         gitCtx.RepoKey,
		Status:          hooks.SyncTaskStatusRunning,
		LastRequestedAt: now.Add(-5 * time.Minute),
		LastStartedAt:   &now,
		RunnerPID:       999999,
		LeaseExpiresAt:  ptrTimeValue(now.Add(5 * time.Minute)),
	}
	if err := hooks.SaveSyncTask(task); err != nil {
		t.Fatalf("SaveSyncTask: %v", err)
	}

	withWorkingDir(t, repo)
	buf := &bytes.Buffer{}
	syncStatusCmd.SetOut(buf)
	syncStatusCmd.SetErr(buf)
	if err := syncStatusCmd.RunE(syncStatusCmd, nil); err != nil {
		t.Fatalf("syncStatusCmd.RunE: %v", err)
	}
	output := buf.String()
	for _, want := range []string{"inactive runner recovered", "Sync Task: pending", "runner exited before updating sync task"} {
		if !bytes.Contains(buf.Bytes(), []byte(want)) {
			t.Fatalf("sync status output missing %q:\n%s", want, output)
		}
	}
}

func TestSyncStatusRecoversCorruptSyncTask(t *testing.T) {
	repo := initRepoWithCommitForCmdTests(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeTestToken(t, home, "user:123")
	writePositiveEligibility(t, home, "github.com/acme/repo", 123)
	withWorkingDir(t, repo)

	gitCtx, err := hooks.DetectGitContext(repo)
	if err != nil {
		t.Fatalf("DetectGitContext: %v", err)
	}
	path, err := hooks.SyncTaskPath(gitCtx.WorkspaceID)
	if err != nil {
		t.Fatalf("SyncTaskPath: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte("{not-json"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	buf := &bytes.Buffer{}
	syncStatusCmd.SetOut(buf)
	syncStatusCmd.SetErr(buf)
	if err := syncStatusCmd.RunE(syncStatusCmd, nil); err != nil {
		t.Fatalf("syncStatusCmd.RunE: %v", err)
	}
	if !bytes.Contains(buf.Bytes(), []byte("corrupt sync task moved aside")) {
		t.Fatalf("sync status output missing corrupt recovery message:\n%s", buf.String())
	}
}

func TestSyncStatusShowsUnresolvedAndDeadLetterCounts(t *testing.T) {
	repo := initRepoWithCommitForCmdTests(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeTestToken(t, home, "user:123")
	writePositiveEligibility(t, home, "github.com/acme/repo", 123)
	withWorkingDir(t, repo)

	gitCtx, err := hooks.DetectGitContext(repo)
	if err != nil {
		t.Fatalf("DetectGitContext: %v", err)
	}
	if err := hooks.EnqueueUnresolvedHookEvent(hooks.UnresolvedHookEvent{
		Kind:        "post-commit",
		RemoteURL:   "https://github.com/acme/repo.git",
		RepoKey:     "github.com/acme/repo",
		WorkspaceID: gitCtx.WorkspaceID,
		CommitSHA:   "abc123",
	}); err != nil {
		t.Fatalf("EnqueueUnresolvedHookEvent: %v", err)
	}
	workspaceDir := filepath.Join(attributionlocal.AttributionRootDir(), "workspaces", gitCtx.WorkspaceID)
	if err := os.MkdirAll(workspaceDir, 0o700); err != nil {
		t.Fatalf("mkdir workspace dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspaceDir, "dead-letter-tool-usage.jsonl"), []byte(`{"version":1}`+"\n"), 0o600); err != nil {
		t.Fatalf("write dead-letter: %v", err)
	}

	buf := &bytes.Buffer{}
	syncStatusCmd.SetOut(buf)
	syncStatusCmd.SetErr(buf)
	if err := syncStatusCmd.RunE(syncStatusCmd, nil); err != nil {
		t.Fatalf("sync status RunE: %v", err)
	}
	for _, want := range []string{"Unresolved Hook Events: 1", "Tool Usage Dead Letters: 1"} {
		if !bytes.Contains(buf.Bytes(), []byte(want)) {
			t.Fatalf("sync status output missing %q:\n%s", want, buf.String())
		}
	}
}

func TestSyncCommandReportsAlreadyRunningTask(t *testing.T) {
	repo := initRepoWithCommitForCmdTests(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeTestToken(t, home, "user:123")
	writePositiveEligibility(t, home, "github.com/acme/repo", 123)
	withWorkingDir(t, repo)

	oldCfg := cfg
	oldClient := apiClient
	cfg = nil
	apiClient = nil
	t.Cleanup(func() {
		cfg = oldCfg
		apiClient = oldClient
	})

	gitCtx, err := hooks.DetectGitContext(repo)
	if err != nil {
		t.Fatalf("DetectGitContext: %v", err)
	}
	now := time.Now().UTC()
	task := hooks.SyncTask{
		WorkspaceID:     gitCtx.WorkspaceID,
		RepoRoot:        repo,
		ServerURL:       "https://ae.example.com",
		AuthSubject:     "user:123",
		RepoConfigID:    123,
		RepoKey:         gitCtx.RepoKey,
		Status:          hooks.SyncTaskStatusRunning,
		LastRequestedAt: now,
		LastStartedAt:   &now,
		RunnerPID:       os.Getpid(),
		LeaseExpiresAt:  ptrTimeValue(now.Add(5 * time.Minute)),
	}
	if err := hooks.SaveSyncTask(task); err != nil {
		t.Fatalf("SaveSyncTask: %v", err)
	}

	buf := &bytes.Buffer{}
	syncCmd.SetOut(buf)
	syncCmd.SetErr(buf)
	if err := syncCmd.RunE(syncCmd, nil); err != nil {
		t.Fatalf("syncCmd.RunE: %v", err)
	}
	output := buf.String()
	if bytes.Contains([]byte(output), []byte("Synced local attribution data")) {
		t.Fatalf("sync output claimed completion while runner is active:\n%s", output)
	}
	if !bytes.Contains([]byte(output), []byte("Attribution sync already running")) {
		t.Fatalf("sync output missing active runner message:\n%s", output)
	}
}
