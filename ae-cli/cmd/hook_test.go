package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ai-efficiency/ae-cli/internal/hooks"
	"github.com/ai-efficiency/ae-cli/internal/session"
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

func TestHookPostCommitCommandQueuesWhenUploaderUnsupported(t *testing.T) {
	repo := initRepoWithCommitForCmdTests(t)

	home := t.TempDir()
	t.Setenv("HOME", home)

	gitDir := runGitOutput(t, repo, "rev-parse", "--absolute-git-dir")
	gitCommonDir := runGitOutput(t, repo, "rev-parse", "--git-common-dir")
	workspaceID, err := session.DeriveWorkspaceID(repo, repo, gitDir, filepath.Join(repo, gitCommonDir))
	if err != nil {
		t.Fatalf("DeriveWorkspaceID: %v", err)
	}
	marker := &session.Marker{SessionID: "sess-1", WorkspaceID: workspaceID, RepoFullName: "github.com/acme/repo"}
	if err := session.WriteMarker(repo, marker); err != nil {
		t.Fatalf("WriteMarker: %v", err)
	}

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

	m, err := session.ReadMarker(repo)
	if err != nil {
		t.Fatalf("ReadMarker: %v", err)
	}
	q, err := hooks.NewWorkspaceQueue(m.WorkspaceID)
	if err != nil {
		t.Fatalf("NewWorkspaceQueue: %v", err)
	}
	items, err := q.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("queued items = %d, want 1", len(items))
	}
}
