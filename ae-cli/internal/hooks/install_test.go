package hooks

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
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
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "init")
	return dir
}

func TestInstallSharedHooksChainsExistingLegacyHook(t *testing.T) {
	repo := initRepoWithCommit(t)

	gitDir := git(t, repo, "rev-parse", "--absolute-git-dir")
	legacyHook := filepath.Join(gitDir, "hooks", "post-commit")
	if err := os.MkdirAll(filepath.Dir(legacyHook), 0o755); err != nil {
		t.Fatalf("mkdir hooks dir: %v", err)
	}
	legacyRan := filepath.Join(repo, "legacy-ran.txt")
	legacy := "#!/bin/sh\n" +
		"echo legacy >> " + shellQuote(legacyRan) + "\n"
	if err := os.WriteFile(legacyHook, []byte(legacy), 0o755); err != nil {
		t.Fatalf("write legacy hook: %v", err)
	}

	if err := InstallSharedHooks(repo, "/bin/true"); err != nil {
		t.Fatalf("InstallSharedHooks: %v", err)
	}

	gitCommon := git(t, repo, "rev-parse", "--git-common-dir")
	sharedHook := filepath.Join(repo, gitCommon, "ae-hooks", "post-commit")
	cmd := exec.Command(sharedHook)
	cmd.Dir = repo
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("running shared hook failed: %v\n%s", err, string(out))
	}

	if _, err := os.Stat(legacyRan); err != nil {
		t.Fatalf("expected legacy hook to run and create %s: %v", legacyRan, err)
	}
}

func TestInstallSharedHooksRejectsRecursiveLegacyPath(t *testing.T) {
	repo := initRepoWithCommit(t)

	gitCommon := git(t, repo, "rev-parse", "--git-common-dir")
	recursive := filepath.Join(repo, gitCommon, "ae-hooks", "legacy")
	if err := os.MkdirAll(recursive, 0o755); err != nil {
		t.Fatalf("mkdir recursive: %v", err)
	}
	git(t, repo, "config", "core.hooksPath", recursive)

	if err := InstallSharedHooks(repo, "/bin/true"); err == nil {
		t.Fatalf("expected error for recursive legacy path, got nil")
	}
}
