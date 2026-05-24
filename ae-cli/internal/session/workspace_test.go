package session

import (
	"github.com/google/uuid"
	"os"
	"path/filepath"
	"testing"
)

func TestDeriveWorkspaceIDUsesCanonicalGitContext(t *testing.T) {
	tmp := t.TempDir()

	real := filepath.Join(tmp, "real")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatalf("mkdir real: %v", err)
	}

	realRepo := filepath.Join(real, "repo")
	realWork := filepath.Join(real, "work")
	realGitDir := filepath.Join(real, "gitdir")
	realCommon := filepath.Join(real, "commondir")
	for _, p := range []string{realRepo, realWork, realGitDir, realCommon} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", p, err)
		}
	}

	link := filepath.Join(tmp, "link")
	if err := os.Symlink(real, link); err != nil {
		// Some platforms/filesystems may not support symlinks without extra privileges.
		t.Skipf("symlink not supported: %v", err)
	}

	repoRoot := filepath.Join(link, "repo", "..", "repo")
	workspaceRoot := filepath.Join(link, "work", "..", "work")
	gitDir := filepath.Join(link, "gitdir")
	gitCommonDir := filepath.Join(link, "commondir")

	got, err := deriveWorkspaceID(repoRoot, workspaceRoot, gitDir, gitCommonDir)
	if err != nil {
		t.Fatalf("deriveWorkspaceID: %v", err)
	}

	cRepo, err := filepath.EvalSymlinks(repoRoot)
	if err != nil {
		t.Fatalf("EvalSymlinks(repoRoot): %v", err)
	}
	cWork, err := filepath.EvalSymlinks(workspaceRoot)
	if err != nil {
		t.Fatalf("EvalSymlinks(workspaceRoot): %v", err)
	}
	cGitDir, err := filepath.EvalSymlinks(gitDir)
	if err != nil {
		t.Fatalf("EvalSymlinks(gitDir): %v", err)
	}
	cCommon, err := filepath.EvalSymlinks(gitCommonDir)
	if err != nil {
		t.Fatalf("EvalSymlinks(gitCommonDir): %v", err)
	}

	cRepo, _ = filepath.Abs(filepath.Clean(cRepo))
	cWork, _ = filepath.Abs(filepath.Clean(cWork))
	cGitDir, _ = filepath.Abs(filepath.Clean(cGitDir))
	cCommon, _ = filepath.Abs(filepath.Clean(cCommon))

	ns := uuid.NewSHA1(uuid.NameSpaceDNS, []byte("ae-workspace"))
	name := cRepo + "\x1f" + cWork + "\x1f" + cGitDir + "\x1f" + cCommon
	want := uuid.NewSHA1(ns, []byte(name)).String()

	if got != want {
		t.Fatalf("workspace_id = %q, want %q", got, want)
	}
}
