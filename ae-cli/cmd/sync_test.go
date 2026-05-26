package cmd

import (
	"bytes"
	"os"
	"testing"
	"time"

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
		RunnerPID:       4321,
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
	for _, want := range []string{"Sync Task: running", "runner_pid: 4321"} {
		if !bytes.Contains(buf.Bytes(), []byte(want)) {
			t.Fatalf("sync status output missing %q:\n%s", want, output)
		}
	}
}
