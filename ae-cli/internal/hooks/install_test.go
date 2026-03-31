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

func TestInstallSharedHooksPreservesLegacyChainAcrossReinstall(t *testing.T) {
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

	// First install captures legacy hook and chains it.
	if err := InstallSharedHooks(repo, "/bin/true"); err != nil {
		t.Fatalf("InstallSharedHooks(1): %v", err)
	}

	// Second install must not silently drop the legacy chain just because core.hooksPath
	// now points to the shared dir.
	if err := InstallSharedHooks(repo, "/bin/true"); err != nil {
		t.Fatalf("InstallSharedHooks(2): %v", err)
	}

	// Running the shared hook should still run the legacy script.
	gitCommon := git(t, repo, "rev-parse", "--git-common-dir")
	sharedHook := filepath.Join(repo, gitCommon, "ae-hooks", "post-commit")
	cmd := exec.Command(sharedHook)
	cmd.Dir = repo
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("running shared hook failed: %v\n%s", err, string(out))
	}

	if _, err := os.Stat(legacyRan); err != nil {
		t.Fatalf("expected legacy hook to run after reinstall and create %s: %v", legacyRan, err)
	}
}

func TestInstallSharedHooksDoesNotRunNonExecutableLegacyHook(t *testing.T) {
	repo := initRepoWithCommit(t)

	gitCommon := git(t, repo, "rev-parse", "--git-common-dir")
	legacyHook := filepath.Join(repo, gitCommon, "hooks", "post-commit")
	if err := os.MkdirAll(filepath.Dir(legacyHook), 0o755); err != nil {
		t.Fatalf("mkdir hooks dir: %v", err)
	}
	legacyRan := filepath.Join(repo, "legacy-should-not-run.txt")
	legacy := "#!/bin/sh\n" +
		"echo legacy >> " + shellQuote(legacyRan) + "\n"
	if err := os.WriteFile(legacyHook, []byte(legacy), 0o644); err != nil {
		t.Fatalf("write legacy hook: %v", err)
	}

	if err := InstallSharedHooks(repo, "/bin/true"); err != nil {
		t.Fatalf("InstallSharedHooks: %v", err)
	}

	sharedHook := filepath.Join(repo, gitCommon, "ae-hooks", "post-commit")
	cmd := exec.Command(sharedHook)
	cmd.Dir = repo
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("running shared hook failed: %v\n%s", err, string(out))
	}

	if _, err := os.Stat(legacyRan); !os.IsNotExist(err) {
		t.Fatalf("expected non-executable legacy hook not to run, stat err=%v", err)
	}
}

func TestInstallSharedHooksSeesWorktreeCoreHooksPath(t *testing.T) {
	repo := initRepoWithCommit(t)

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}

	git(t, repo, "config", "extensions.worktreeConfig", "true")
	legacyDir := filepath.Join(repo, "custom-hooks")
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatalf("mkdir custom-hooks: %v", err)
	}
	legacyRan := filepath.Join(repo, "legacy-worktree-ran.txt")
	legacy := "#!/bin/sh\n" +
		"echo legacy >> " + shellQuote(legacyRan) + "\n"
	if err := os.WriteFile(filepath.Join(legacyDir, "post-commit"), []byte(legacy), 0o755); err != nil {
		t.Fatalf("write legacy hook: %v", err)
	}
	git(t, repo, "config", "--worktree", "core.hooksPath", legacyDir)

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
		t.Fatalf("expected worktree hooksPath legacy hook to run: %v", err)
	}

	got := git(t, repo, "config", "--get", "core.hooksPath")
	expectedSharedDir := filepath.Join(repo, gitCommon, "ae-hooks")
	gotCanon, _ := canonicalPath(got)
	wantCanon, _ := canonicalPath(expectedSharedDir)
	if gotCanon != wantCanon {
		t.Fatalf("core.hooksPath = %q, want %q", gotCanon, wantCanon)
	}
}

func TestInstallSharedHooksPreservesPostRewriteInputForLegacyHook(t *testing.T) {
	repo := initRepoWithCommit(t)

	selfDir := t.TempDir()
	selfPath := filepath.Join(selfDir, "ae-cli")
	selfScript := "#!/bin/sh\ncat >/dev/null\n"
	if err := os.WriteFile(selfPath, []byte(selfScript), 0o755); err != nil {
		t.Fatalf("write fake ae-cli: %v", err)
	}

	gitCommon := git(t, repo, "rev-parse", "--git-common-dir")
	legacyHook := filepath.Join(repo, gitCommon, "hooks", "post-rewrite")
	if err := os.MkdirAll(filepath.Dir(legacyHook), 0o755); err != nil {
		t.Fatalf("mkdir hooks dir: %v", err)
	}
	legacySaw := filepath.Join(repo, "legacy-post-rewrite.txt")
	legacy := "#!/bin/sh\n" +
		"cat > " + shellQuote(legacySaw) + "\n"
	if err := os.WriteFile(legacyHook, []byte(legacy), 0o755); err != nil {
		t.Fatalf("write legacy hook: %v", err)
	}

	if err := InstallSharedHooks(repo, selfPath); err != nil {
		t.Fatalf("InstallSharedHooks: %v", err)
	}

	sharedHook := filepath.Join(repo, gitCommon, "ae-hooks", "post-rewrite")
	cmd := exec.Command(sharedHook, "amend")
	cmd.Dir = repo
	cmd.Stdin = strings.NewReader("oldsha newsha\n")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("running shared post-rewrite failed: %v\n%s", err, string(out))
	}

	data, err := os.ReadFile(legacySaw)
	if err != nil {
		t.Fatalf("read legacy output: %v", err)
	}
	if got := string(data); got != "oldsha newsha\n" {
		t.Fatalf("legacy stdin = %q, want %q", got, "oldsha newsha\n")
	}
}

func TestInstallSharedHooksPreservesDistinctLegacyHooksPerWorktree(t *testing.T) {
	repo := initRepoWithCommit(t)
	git(t, repo, "config", "extensions.worktreeConfig", "true")

	worktreeParent := t.TempDir()
	worktree := filepath.Join(worktreeParent, "linked")
	git(t, repo, "worktree", "add", "-b", "linked-test", worktree)

	legacyMainDir := filepath.Join(repo, "legacy-main")
	if err := os.MkdirAll(legacyMainDir, 0o755); err != nil {
		t.Fatalf("mkdir legacy-main: %v", err)
	}
	mainRan := filepath.Join(repo, "main-legacy.txt")
	mainHook := "#!/bin/sh\n" +
		"echo main >> " + shellQuote(mainRan) + "\n"
	if err := os.WriteFile(filepath.Join(legacyMainDir, "post-commit"), []byte(mainHook), 0o755); err != nil {
		t.Fatalf("write main legacy hook: %v", err)
	}

	legacyLinkedDir := filepath.Join(worktree, "legacy-linked")
	if err := os.MkdirAll(legacyLinkedDir, 0o755); err != nil {
		t.Fatalf("mkdir legacy-linked: %v", err)
	}
	linkedRan := filepath.Join(worktree, "linked-legacy.txt")
	linkedHook := "#!/bin/sh\n" +
		"echo linked >> " + shellQuote(linkedRan) + "\n"
	if err := os.WriteFile(filepath.Join(legacyLinkedDir, "post-commit"), []byte(linkedHook), 0o755); err != nil {
		t.Fatalf("write linked legacy hook: %v", err)
	}

	git(t, repo, "config", "--worktree", "core.hooksPath", legacyMainDir)
	git(t, worktree, "config", "--worktree", "core.hooksPath", legacyLinkedDir)

	if err := InstallSharedHooks(repo, "/bin/true"); err != nil {
		t.Fatalf("InstallSharedHooks(repo): %v", err)
	}
	if err := InstallSharedHooks(worktree, "/bin/true"); err != nil {
		t.Fatalf("InstallSharedHooks(worktree): %v", err)
	}

	if err := os.WriteFile(filepath.Join(repo, "main.txt"), []byte("main\n"), 0o644); err != nil {
		t.Fatalf("write main.txt: %v", err)
	}
	git(t, repo, "add", "main.txt")
	git(t, repo, "commit", "-m", "main")

	if err := os.WriteFile(filepath.Join(worktree, "linked.txt"), []byte("linked\n"), 0o644); err != nil {
		t.Fatalf("write linked.txt: %v", err)
	}
	git(t, worktree, "add", "linked.txt")
	git(t, worktree, "commit", "-m", "linked")

	if _, err := os.Stat(mainRan); err != nil {
		t.Fatalf("expected main worktree legacy hook to run: %v", err)
	}
	if _, err := os.Stat(linkedRan); err != nil {
		t.Fatalf("expected linked worktree legacy hook to run: %v", err)
	}
}

func TestInstallSharedHooksPreservesPostRewriteInputWithoutMktemp(t *testing.T) {
	repo := initRepoWithCommit(t)

	selfDir := t.TempDir()
	selfPath := filepath.Join(selfDir, "ae-cli")
	selfScript := "#!/bin/sh\ncat >/dev/null\n"
	if err := os.WriteFile(selfPath, []byte(selfScript), 0o755); err != nil {
		t.Fatalf("write fake ae-cli: %v", err)
	}

	gitCommon := git(t, repo, "rev-parse", "--git-common-dir")
	legacyHook := filepath.Join(repo, gitCommon, "hooks", "post-rewrite")
	if err := os.MkdirAll(filepath.Dir(legacyHook), 0o755); err != nil {
		t.Fatalf("mkdir hooks dir: %v", err)
	}
	legacySaw := filepath.Join(repo, "legacy-post-rewrite-no-mktemp.txt")
	legacy := "#!/bin/sh\n" +
		"cat > " + shellQuote(legacySaw) + "\n"
	if err := os.WriteFile(legacyHook, []byte(legacy), 0o755); err != nil {
		t.Fatalf("write legacy hook: %v", err)
	}

	if err := InstallSharedHooks(repo, selfPath); err != nil {
		t.Fatalf("InstallSharedHooks: %v", err)
	}

	binDir := t.TempDir()
	fakeMktemp := filepath.Join(binDir, "mktemp")
	if err := os.WriteFile(fakeMktemp, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatalf("write fake mktemp: %v", err)
	}

	sharedHook := filepath.Join(repo, gitCommon, "ae-hooks", "post-rewrite")
	cmd := exec.Command(sharedHook, "amend")
	cmd.Dir = repo
	cmd.Stdin = strings.NewReader("oldsha newsha\n")
	cmd.Env = append(os.Environ(), "PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("running shared post-rewrite failed: %v\n%s", err, string(out))
	}

	data, err := os.ReadFile(legacySaw)
	if err != nil {
		t.Fatalf("read legacy output: %v", err)
	}
	if got := string(data); got != "oldsha newsha\n" {
		t.Fatalf("legacy stdin = %q, want %q", got, "oldsha newsha\n")
	}
}
