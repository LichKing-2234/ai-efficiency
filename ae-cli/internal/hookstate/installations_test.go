package hookstate

import (
	"testing"
	"time"
)

func TestInstallationsUseModeOnlyForGlobalIdentity(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	now := time.Date(2026, 5, 24, 10, 0, 0, 0, time.UTC)

	registry, err := LoadInstallations()
	if err != nil {
		t.Fatalf("LoadInstallations: %v", err)
	}
	registry.Upsert(InstallationRecord{Mode: "global", HooksPath: "/old", Enabled: true, TemplateVersion: 1, UpdatedAt: now})
	registry.Upsert(InstallationRecord{Mode: "global", HooksPath: "/new", Enabled: true, TemplateVersion: CurrentHookTemplateVersion, UpdatedAt: now.Add(time.Minute)})

	if len(registry.Records) != 1 {
		t.Fatalf("records = %d, want 1", len(registry.Records))
	}
	if registry.Records[0].HooksPath != "/new" || registry.Records[0].TemplateVersion != CurrentHookTemplateVersion {
		t.Fatalf("global record was not replaced: %+v", registry.Records[0])
	}
}

func TestInstallationsUseLocalAndWorktreeIdentities(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	now := time.Date(2026, 5, 24, 10, 0, 0, 0, time.UTC)
	registry, err := LoadInstallations()
	if err != nil {
		t.Fatalf("LoadInstallations: %v", err)
	}

	registry.Upsert(InstallationRecord{Mode: "local", GitCommonDir: "/repo/.git", ConfigScope: "local", HooksPath: "/repo/.git/ae-hooks", Enabled: true, UpdatedAt: now})
	registry.Upsert(InstallationRecord{Mode: "local", GitCommonDir: "/repo/.git", ConfigScope: "local", HooksPath: "/repo/.git/ae-hooks", Enabled: false, UpdatedAt: now.Add(time.Minute)})
	registry.Upsert(InstallationRecord{Mode: "worktree", GitDir: "/repo/.git/worktrees/wt-a", GitCommonDir: "/repo/.git", ConfigScope: "worktree", HooksPath: "/repo/.git/ae-hooks", Enabled: true, UpdatedAt: now})
	registry.Upsert(InstallationRecord{Mode: "worktree", GitDir: "/repo/.git/worktrees/wt-b", GitCommonDir: "/repo/.git", ConfigScope: "worktree", HooksPath: "/repo/.git/ae-hooks", Enabled: true, UpdatedAt: now})

	if len(registry.Records) != 3 {
		t.Fatalf("records = %d, want 3: %+v", len(registry.Records), registry.Records)
	}
	local, ok := registry.Find(InstallationRecord{Mode: "local", GitCommonDir: "/repo/.git", ConfigScope: "local", HooksPath: "/repo/.git/ae-hooks"})
	if !ok || local.Enabled {
		t.Fatalf("local record = %+v, %v; want disabled replacement", local, ok)
	}
	registry.Disable(InstallationRecord{Mode: "worktree", GitDir: "/repo/.git/worktrees/wt-a", ConfigScope: "worktree", HooksPath: "/repo/.git/ae-hooks"}, now.Add(2*time.Minute))
	wtA, ok := registry.Find(InstallationRecord{Mode: "worktree", GitDir: "/repo/.git/worktrees/wt-a", ConfigScope: "worktree", HooksPath: "/repo/.git/ae-hooks"})
	if !ok || wtA.Enabled {
		t.Fatalf("worktree A record = %+v, %v; want disabled", wtA, ok)
	}
	wtB, ok := registry.Find(InstallationRecord{Mode: "worktree", GitDir: "/repo/.git/worktrees/wt-b", ConfigScope: "worktree", HooksPath: "/repo/.git/ae-hooks"})
	if !ok || !wtB.Enabled {
		t.Fatalf("worktree B record = %+v, %v; want still enabled", wtB, ok)
	}
}
