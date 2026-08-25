package cmd

import (
	"bytes"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/ai-efficiency/ae-cli/internal/hooks"
)

func initRepoWithCommitForCmdTests(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init", "--template=/dev/null")
	runGit(t, dir, "config", "user.email", "t@example.com")
	runGit(t, dir, "config", "user.name", "t")
	if err := os.MkdirAll(dir+"/.git/test-hooks-empty", 0o755); err != nil {
		t.Fatalf("mkdir test hooks dir: %v", err)
	}
	runGit(t, dir, "config", "core.hooksPath", dir+"/.git/test-hooks-empty")
	runGit(t, dir, "remote", "add", "origin", "https://github.com/acme/repo.git")
	if err := os.WriteFile(dir+"/a.txt", []byte("a\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "init")
	return dir
}

func TestCmdGitFixtureDoesNotRunInheritedGlobalHook(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GIT_CONFIG_GLOBAL", home+"/.gitconfig")
	capture := home + "/unexpected-post-commit"
	t.Setenv("AE_TEST_GLOBAL_HOOK_CAPTURE", capture)
	fakeCLI := home + "/ae-cli"
	if err := os.WriteFile(fakeCLI, []byte("#!/bin/sh\n: >\"$AE_TEST_GLOBAL_HOOK_CAPTURE\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AE_CLI_BIN", fakeCLI)
	hooksDir := home + "/global-hooks"
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hooksDir+"/post-commit", []byte(hooks.RenderManagedHookScript("post-commit", "fixture-isolation")), 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, home, "config", "--global", "core.hooksPath", hooksDir)

	initRepoWithCommitForCmdTests(t)

	if _, err := os.Stat(capture); !os.IsNotExist(err) {
		t.Fatalf("command fixture ran inherited global hook: %v", err)
	}
	if _, err := os.Stat(home + "/.ae-cli/state"); !os.IsNotExist(err) {
		t.Fatalf("command fixture wrote outer AE state: %v", err)
	}
}

func runGit(t *testing.T, dir string, args ...string) {
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
}
