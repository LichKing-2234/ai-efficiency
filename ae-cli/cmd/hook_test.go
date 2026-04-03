package cmd

import (
	"os"
	"testing"

	"github.com/ai-efficiency/ae-cli/internal/hooks"
	"github.com/ai-efficiency/ae-cli/internal/session"
)

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

func TestHookCommandHasSessionEventSubcommand(t *testing.T) {
	var found bool
	for _, c := range hookCmd.Commands() {
		if c.Name() == "session-event" {
			found = true
			if !c.Hidden {
				t.Fatalf("expected hook session-event to be hidden")
			}
		}
	}
	if !found {
		t.Fatalf("expected hidden subcommand 'ae-cli hook session-event' to exist")
	}
}

func TestHookPostCommitCommandQueuesWhenUploaderUnsupported(t *testing.T) {
	repo := initRepoWithCommitForCmdTests(t)

	home := t.TempDir()
	t.Setenv("HOME", home)

	marker := &session.Marker{SessionID: "sess-1", RepoFullName: "github.com/acme/repo"}
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

	q, err := hooks.NewLocalQueue("sess-1")
	if err != nil {
		t.Fatalf("NewLocalQueue: %v", err)
	}
	items, err := q.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("queued items = %d, want 1", len(items))
	}
}
