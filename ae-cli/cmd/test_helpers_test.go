package cmd

import (
	"bytes"
	"os"
	"os/exec"
	"strings"
	"testing"
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
