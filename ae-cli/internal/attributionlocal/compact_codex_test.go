package attributionlocal

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ai-efficiency/ae-cli/internal/client"
)

func TestParseCompactCodexFileUsesLastUsageAndOutputRepository(t *testing.T) {
	repoA := compactTestRepo(t, "repo-a")
	repoB := compactTestRepo(t, "repo-b")
	path := filepath.Join(t.TempDir(), "session.jsonl")
	rows := []any{
		map[string]any{"type": "session_meta", "payload": map[string]any{"id": "conversation-a", "cwd": repoA}},
		map[string]any{"type": "turn_context", "payload": map[string]any{"turn_id": "turn-a", "cwd": repoA, "model": "gpt-test"}},
		map[string]any{"type": "response_item", "payload": map[string]any{"type": "custom_tool_call", "name": "apply_patch", "input": "*** Begin Patch\n*** Update File: " + filepath.Join(repoB, "internal", "value.go") + "\n*** End Patch"}},
		compactTokenRow("2026-08-05T10:00:00Z", map[string]any{
			"input_tokens": 100, "cached_input_tokens": 40, "cache_write_input_tokens": 10, "output_tokens": 20,
			"reasoning_output_tokens": 5, "total_tokens": 120,
		}, map[string]any{"input_tokens": 9999, "output_tokens": 9999}),
	}
	writeJSONLines(t, path, rows)

	fromB, _, err := parseCompactCodexFile(context.Background(), path, repoB)
	if err != nil {
		t.Fatal(err)
	}
	if len(fromB) != 1 {
		t.Fatalf("repo B atoms = %d, want 1", len(fromB))
	}
	got := fromB[0]
	if got.Evidence != "direct" || got.FreshInput != 50 || got.CacheRead != 40 || got.CacheWrite != 10 || got.Output != 20 || got.Processed != 120 || got.ProviderTotal != 120 || got.Reasoning != 5 {
		t.Fatalf("atom = %+v", got)
	}
	fromA, _, err := parseCompactCodexFile(context.Background(), path, repoA)
	if err != nil {
		t.Fatal(err)
	}
	if len(fromA) != 0 {
		t.Fatalf("launch repository received output attributed to B: %+v", fromA)
	}
}

func TestScanCompactCodexAtomsIgnoresReadOnlyPathAsRepositoryEvidence(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	repoA := compactTestRepo(t, "repo-a")
	repoB := compactTestRepo(t, "repo-b")
	path := filepath.Join(home, ".codex", "sessions", "2026", "08", "05", "read-only.jsonl")
	writeJSONLines(t, path, []any{
		map[string]any{"type": "session_meta", "payload": map[string]any{"id": "conversation-read-only", "cwd": repoA}},
		map[string]any{"type": "turn_context", "payload": map[string]any{"turn_id": "turn-read-only", "cwd": repoA, "model": "gpt-test"}},
		map[string]any{"type": "response_item", "payload": map[string]any{
			"type": "function_call", "name": "read_file", "arguments": `{"path":"` + filepath.Join(repoB, "internal", "value.go") + `"}`,
		}},
		compactTokenRow("2026-08-05T10:00:00Z", map[string]any{"input_tokens": 10, "output_tokens": 2, "total_tokens": 12}, nil),
	})

	fromA, err := ScanCompactCodexAtoms(context.Background(), repoA)
	if err != nil {
		t.Fatal(err)
	}
	if len(fromA) != 1 || fromA[0].Evidence != "weak_cwd" {
		t.Fatalf("repo A atoms = %+v, want one cwd-only atom", fromA)
	}
	fromB, err := ScanCompactCodexAtoms(context.Background(), repoB)
	if err != nil {
		t.Fatal(err)
	}
	if len(fromB) != 0 {
		t.Fatalf("read-only path falsely produced direct evidence for repo B: %+v", fromB)
	}
}

func TestScanCompactCodexAtomsUsesExplicitWorkdirAsRepositoryEvidence(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	repoA := compactTestRepo(t, "repo-a")
	repoB := compactTestRepo(t, "repo-b")
	path := filepath.Join(home, ".codex", "sessions", "2026", "08", "05", "workdir.jsonl")
	writeJSONLines(t, path, []any{
		map[string]any{"type": "session_meta", "payload": map[string]any{"id": "conversation-workdir", "cwd": repoA}},
		map[string]any{"type": "turn_context", "payload": map[string]any{"turn_id": "turn-workdir", "cwd": repoA, "model": "gpt-test"}},
		map[string]any{"type": "response_item", "payload": map[string]any{
			"type": "function_call", "name": "exec_command", "arguments": `{"cmd":"git status","workdir":"` + repoB + `","metadata":{"path":"` + repoA + `"}}`,
		}},
		compactTokenRow("2026-08-05T10:00:00Z", map[string]any{"input_tokens": 10, "output_tokens": 2, "total_tokens": 12}, nil),
	})

	fromB, err := ScanCompactCodexAtoms(context.Background(), repoB)
	if err != nil {
		t.Fatal(err)
	}
	if len(fromB) != 1 || fromB[0].Evidence != "direct" {
		t.Fatalf("repo B atoms = %+v, want explicit workdir evidence", fromB)
	}
	fromA, err := ScanCompactCodexAtoms(context.Background(), repoA)
	if err != nil {
		t.Fatal(err)
	}
	if len(fromA) != 0 {
		t.Fatalf("turn cwd or nested path overrode explicit workdir: %+v", fromA)
	}
}

func TestScanCompactCodexAtomsUsesConfirmedPatchTargetAsRepositoryEvidence(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	repoA := compactTestRepo(t, "repo-a")
	repoB := compactTestRepo(t, "repo-b")
	path := filepath.Join(home, ".codex", "sessions", "2026", "08", "05", "patch.jsonl")
	writeJSONLines(t, path, []any{
		map[string]any{"type": "session_meta", "payload": map[string]any{"id": "conversation-patch", "cwd": repoA}},
		map[string]any{"type": "turn_context", "payload": map[string]any{"turn_id": "turn-patch", "cwd": repoA, "model": "gpt-test"}},
		map[string]any{"type": "response_item", "payload": map[string]any{
			"type": "custom_tool_call", "name": "apply_patch", "input": "*** Begin Patch\n*** Add File: " + filepath.Join(repoB, "feature.go") + "\n*** End Patch",
		}},
		compactTokenRow("2026-08-05T10:00:00Z", map[string]any{"input_tokens": 10, "output_tokens": 2, "total_tokens": 12}, nil),
	})

	fromB, err := ScanCompactCodexAtoms(context.Background(), repoB)
	if err != nil {
		t.Fatal(err)
	}
	if len(fromB) != 1 || fromB[0].Evidence != "direct" {
		t.Fatalf("repo B atoms = %+v, want confirmed patch-target evidence", fromB)
	}
	fromA, err := ScanCompactCodexAtoms(context.Background(), repoA)
	if err != nil {
		t.Fatal(err)
	}
	if len(fromA) != 0 {
		t.Fatalf("turn cwd overrode confirmed patch target: %+v", fromA)
	}
}

func TestInitializeCompactBaselineDoesNotPersistHistoricalAtoms(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	enabledAt := time.Date(2026, 8, 5, 10, 0, 0, 0, time.UTC)
	if err := InitializeCompactBaseline(context.Background(), enabledAt); err != nil {
		t.Fatal(err)
	}
	state, err := LoadCompactState()
	if err != nil {
		t.Fatal(err)
	}
	if !state.EnabledAt.Equal(enabledAt) || len(state.SeenAtoms) != 0 {
		t.Fatalf("baseline state = %+v", state)
	}
}

func TestScanCompactCodexAtomsSinceSkipsUnchangedHistoricalFiles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	repo := compactTestRepo(t, "repo")
	sessions := filepath.Join(home, ".codex", "sessions")
	if err := os.MkdirAll(sessions, 0o700); err != nil {
		t.Fatal(err)
	}
	enabledAt := time.Date(2026, 8, 5, 10, 0, 0, 0, time.UTC)
	oldPath := filepath.Join(sessions, "old.jsonl")
	writeCompactSessionFile(t, oldPath, repo, "conversation-old", "turn-old", "2026-08-05T10:01:00Z")
	if err := os.Chtimes(oldPath, enabledAt.Add(-time.Minute), enabledAt.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	newPath := filepath.Join(sessions, "new.jsonl")
	writeCompactSessionFile(t, newPath, repo, "conversation-new", "turn-new", "2026-08-05T10:02:00Z")
	if err := os.Chtimes(newPath, enabledAt.Add(time.Minute), enabledAt.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}

	atoms, err := scanCompactCodexAtomsSince(context.Background(), repo, enabledAt)
	if err != nil {
		t.Fatal(err)
	}
	if len(atoms) != 1 || atoms[0].ConversationID != "conversation-new" {
		t.Fatalf("atoms = %+v", atoms)
	}
}

func TestParseCompactCodexFileDistinguishesRealLinkedWorktrees(t *testing.T) {
	mainRepo := compactTestRepo(t, "repo")
	compactCommitFile(t, mainRepo, "base.txt", "base\n", "base")
	linkedRepo := filepath.Join(t.TempDir(), "linked")
	compactGit(t, mainRepo, "worktree", "add", "-q", "-b", "feature/linked", linkedRepo)
	path := filepath.Join(t.TempDir(), "session.jsonl")
	writeJSONLines(t, path, []any{
		map[string]any{"type": "session_meta", "payload": map[string]any{"id": "conversation-linked", "cwd": mainRepo}},
		map[string]any{"type": "turn_context", "payload": map[string]any{"turn_id": "turn-linked", "cwd": mainRepo, "model": "gpt-test"}},
		map[string]any{"type": "response_item", "payload": map[string]any{"type": "custom_tool_call", "name": "apply_patch", "input": "*** Begin Patch\n*** Update File: " + filepath.Join(linkedRepo, "feature.go") + "\n*** End Patch"}},
		compactTokenRow("2026-08-05T10:00:00Z", map[string]any{"input_tokens": 20, "cached_input_tokens": 5, "output_tokens": 5, "total_tokens": 25}, nil),
	})

	linkedAtoms, _, err := parseCompactCodexFile(context.Background(), path, linkedRepo)
	if err != nil {
		t.Fatal(err)
	}
	if len(linkedAtoms) != 1 || linkedAtoms[0].Evidence != "direct" {
		t.Fatalf("linked worktree atoms = %+v", linkedAtoms)
	}
	mainAtoms, _, err := parseCompactCodexFile(context.Background(), path, mainRepo)
	if err != nil {
		t.Fatal(err)
	}
	if len(mainAtoms) != 0 {
		t.Fatalf("main worktree received linked-worktree output: %+v", mainAtoms)
	}
}

func TestParseCompactCodexFileDoesNotPromoteCumulativeTotal(t *testing.T) {
	repo := compactTestRepo(t, "repo")
	path := filepath.Join(t.TempDir(), "session.jsonl")
	writeJSONLines(t, path, []any{
		map[string]any{"type": "session_meta", "payload": map[string]any{"id": "conversation-a", "cwd": repo}},
		compactTokenRow("2026-08-05T10:00:00Z", nil, map[string]any{"input_tokens": 900, "output_tokens": 100, "total_tokens": 1000}),
	})
	atoms, _, err := parseCompactCodexFile(context.Background(), path, repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(atoms) != 1 || atoms[0].Quality != "invalid" || atoms[0].Processed != 0 {
		t.Fatalf("atoms = %+v, want one zero-token coverage gap", atoms)
	}
}

func TestScanCompactCodexAtomsUsesSQLiteOnlyWhenJSONLHasNoMeasuredUsage(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	repo := compactTestRepo(t, "repo")
	sessionPath := filepath.Join(home, ".codex", "sessions", "2026", "08", "05", "session.jsonl")
	if err := os.MkdirAll(filepath.Dir(sessionPath), 0o700); err != nil {
		t.Fatal(err)
	}
	writeJSONLines(t, sessionPath, []any{
		map[string]any{"type": "session_meta", "payload": map[string]any{"id": "conversation-a", "cwd": repo}},
	})
	writeCompactSQLite(t, filepath.Join(home, ".codex", "logs_2.sqlite"), []string{
		`event.name="codex.sse_event" event.kind=response.completed input_token_count=12 output_token_count=5 cached_token_count=4 reasoning_token_count=2 event.timestamp=2026-08-05T10:00:00Z conversation.id=conversation-a response.id=response-a`,
	})

	atoms, err := ScanCompactCodexAtoms(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(atoms) != 1 || atoms[0].FreshInput != 8 || atoms[0].CacheRead != 4 || atoms[0].Output != 5 || atoms[0].Processed != 17 || atoms[0].Evidence != "weak_cwd" {
		t.Fatalf("fallback atoms = %+v", atoms)
	}

	writeJSONLines(t, sessionPath, []any{
		map[string]any{"type": "session_meta", "payload": map[string]any{"id": "conversation-a", "cwd": repo}},
		compactTokenRow("2026-08-05T10:01:00Z", map[string]any{"input_tokens": 30, "cached_input_tokens": 10, "output_tokens": 5, "total_tokens": 35}, nil),
	})
	atoms, err = ScanCompactCodexAtoms(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(atoms) != 1 || atoms[0].Processed != 35 || atoms[0].ID == compactSQLiteAtomID("conversation-a", "response-a") {
		t.Fatalf("JSONL and SQLite were combined or SQLite won unexpectedly: %+v", atoms)
	}
}

func TestScanCompactCodexAtomsDoesNotFallbackToSQLiteWhenMeasuredJSONLTargetsAnotherRepo(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	repoA := compactTestRepo(t, "repo-a")
	repoB := compactTestRepo(t, "repo-b")
	writeCompactSession(t, home, repoA, "conversation-cross-repo", "turn-cross-repo", "2026-08-05T10:00:00Z", []string{filepath.Join(repoB, "feature.go")})
	writeCompactSQLite(t, filepath.Join(home, ".codex", "logs_2.sqlite"), []string{
		`event.name="codex.sse_event" event.kind=response.completed input_token_count=100 output_token_count=20 cached_token_count=40 reasoning_token_count=5 event.timestamp=2026-08-05T10:00:00Z conversation.id=conversation-cross-repo response.id=response-cross-repo`,
	})

	fromA, err := ScanCompactCodexAtoms(context.Background(), repoA)
	if err != nil {
		t.Fatal(err)
	}
	if len(fromA) != 0 {
		t.Fatalf("launch repository received SQLite fallback despite measured JSONL in repo B: %+v", fromA)
	}
	fromB, err := ScanCompactCodexAtoms(context.Background(), repoB)
	if err != nil {
		t.Fatal(err)
	}
	if len(fromB) != 1 || fromB[0].Evidence != "direct" || fromB[0].ID == compactSQLiteAtomID("conversation-cross-repo", "response-cross-repo") {
		t.Fatalf("output repository atoms = %+v, want the single measured JSONL atom", fromB)
	}
}

func TestCompactSyncEngineConservesReplaysLateBindsAndRewrites(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	repo := compactTestRepo(t, "repo")
	enabledAt := time.Date(2026, 8, 5, 9, 0, 0, 0, time.UTC)
	writeCompactState(t, CompactState{Version: 2, EnabledAt: enabledAt, SeenAtoms: map[string]bool{}})
	writeCompactSession(t, home, repo, "conversation-a", "turn-a", "2026-08-05T10:00:00Z", []string{filepath.Join(repo, "main.go")})

	fake := &compactBackendFake{}
	engine := &CompactSyncEngine{Client: fake}
	base := CompactRunOptions{InstallationID: "installation-a", RepoRoot: repo, RepoConfigID: 11, RepoKey: "repo:a", WorkspaceID: "workspace-a", Cutoff: time.Date(2026, 8, 5, 10, 5, 0, 0, time.UTC)}
	if err := engine.Run(context.Background(), base); err != nil {
		t.Fatal(err)
	}
	if len(fake.buckets) != 1 || fake.buckets[0].InitialRevision.Allocations[0].Target.Status != "unbound" {
		t.Fatalf("initial buckets = %+v", fake.buckets)
	}
	assertBucketConservation(t, fake.buckets[0])
	if err := engine.Run(context.Background(), base); err != nil {
		t.Fatal(err)
	}
	if len(fake.buckets) != 1 {
		t.Fatalf("replay uploaded %d buckets, want 1", len(fake.buckets))
	}

	commitAt := time.Date(2026, 8, 5, 10, 10, 0, 0, time.UTC)
	commitRun := base
	commitRun.TriggerKind = "post-commit"
	commitRun.CommitSHA = "commit-a"
	commitRun.Branch = "feature/a"
	commitRun.Cutoff = commitAt
	if err := engine.Run(context.Background(), commitRun); err != nil {
		t.Fatal(err)
	}
	if len(fake.revisions) != 1 || fake.revisions[0].Allocations[0].Target.CommitSHA != "commit-a" {
		t.Fatalf("late-bind revisions = %+v", fake.revisions)
	}
	assertRevisionConservation(t, fake.buckets[0], fake.revisions[0])

	rewriteAt := commitAt.Add(time.Minute)
	if err := QueueCompactTrigger(context.Background(), CompactTrigger{
		Kind: "post-rewrite", RepoConfigID: 11, RepoKey: "repo:a", WorkspaceID: "workspace-a",
		OldCommitSHA: "commit-a", NewCommitSHA: "commit-b", RewriteType: "rebase", CapturedAt: rewriteAt,
	}); err != nil {
		t.Fatal(err)
	}
	base.Cutoff = rewriteAt
	if err := engine.Run(context.Background(), base); err != nil {
		t.Fatal(err)
	}
	if len(fake.revisions) != 2 || fake.revisions[1].Allocations[0].Target.CommitSHA != "commit-b" || fake.revisions[1].Allocations[0].Target.Lineage != "rebase" {
		t.Fatalf("rewrite revisions = %+v", fake.revisions)
	}
	assertRevisionConservation(t, fake.buckets[0], fake.revisions[1])

	squashAt := rewriteAt.Add(time.Minute)
	if err := QueueCompactTrigger(context.Background(), CompactTrigger{
		Kind: "post-rewrite", RepoConfigID: 11, RepoKey: "repo:a", WorkspaceID: "workspace-a",
		OldCommitSHA: "commit-b", NewCommitSHA: "commit-c", RewriteType: "squash", CapturedAt: squashAt,
	}); err != nil {
		t.Fatal(err)
	}
	base.Cutoff = squashAt
	if err := engine.Run(context.Background(), base); err != nil {
		t.Fatal(err)
	}
	if len(fake.revisions) != 3 || fake.revisions[2].Allocations[0].Target.CommitSHA != "commit-c" || fake.revisions[2].Allocations[0].Target.Lineage != "squash" {
		t.Fatalf("squash revisions = %+v", fake.revisions)
	}
	assertRevisionConservation(t, fake.buckets[0], fake.revisions[2])
}

func TestCompactSyncEngineRetainsCommitTriggerForLateJSONLVisibility(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	repo := compactTestRepo(t, "repo")
	commitSHA := compactCommitFile(t, repo, "main.go", "package main\n", "commit")
	writeCompactState(t, CompactState{Version: 2, EnabledAt: time.Date(2026, 8, 5, 9, 0, 0, 0, time.UTC), SeenAtoms: map[string]bool{}})
	commitAt := time.Date(2026, 8, 5, 10, 10, 0, 0, time.UTC)
	if err := QueueCompactTrigger(context.Background(), CompactTrigger{
		ID: "hook-event", Kind: "post-commit", RepoConfigID: 11, RepoKey: "repo:a", WorkspaceID: "workspace-a",
		CommitSHA: commitSHA, Branch: "feature/a", CapturedAt: commitAt,
	}); err != nil {
		t.Fatal(err)
	}
	fake := &compactBackendFake{}
	engine := &CompactSyncEngine{Client: fake}
	opts := CompactRunOptions{InstallationID: "installation-a", RepoRoot: repo, RepoConfigID: 11, RepoKey: "repo:a", WorkspaceID: "workspace-a", CommitSHA: commitSHA, Branch: "feature/a", TriggerKind: "post-commit", Cutoff: commitAt}
	if err := engine.Run(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	state, err := LoadCompactState()
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Triggers) != 1 {
		t.Fatalf("semantic duplicate commit triggers = %d, want 1", len(state.Triggers))
	}
	writeCompactSession(t, home, repo, "conversation-late-file", "turn-late-file", "2026-08-05T10:00:00Z", []string{filepath.Join(repo, "main.go")})
	opts.TriggerKind = "manual"
	if err := engine.Run(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	if len(fake.buckets) != 1 || fake.buckets[0].InitialRevision.Allocations[0].Target.CommitSHA != commitSHA {
		t.Fatalf("late-visible bucket = %+v", fake.buckets)
	}
}

func TestCompactSyncEngineUsesExactSubsecondCommitBoundary(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	repo := compactTestRepo(t, "repo")
	commitSHA := compactCommitFile(t, repo, "main.go", "package main\n", "commit")
	writeCompactState(t, CompactState{Version: 2, EnabledAt: time.Date(2026, 8, 5, 9, 0, 0, 0, time.UTC), SeenAtoms: map[string]bool{}})

	commitAt := time.Date(2026, 8, 5, 10, 10, 0, 500_000_000, time.UTC)
	if err := QueueCompactTrigger(context.Background(), CompactTrigger{
		ID: "subsecond-commit", Kind: "post-commit", RepoConfigID: 11, RepoKey: "repo:a", WorkspaceID: "workspace-a",
		CommitSHA: commitSHA, Branch: "feature/a", CapturedAt: commitAt,
	}); err != nil {
		t.Fatal(err)
	}
	patch := "*** Begin Patch\n*** Update File: " + filepath.Join(repo, "main.go") + "\n*** End Patch"
	writeJSONLines(t, filepath.Join(home, ".codex", "sessions", "2026", "08", "05", "subsecond.jsonl"), []any{
		map[string]any{"type": "session_meta", "payload": map[string]any{"id": "conversation-subsecond", "cwd": repo}},
		map[string]any{"type": "turn_context", "payload": map[string]any{"turn_id": "turn-before", "cwd": repo, "model": "gpt-test"}},
		map[string]any{"type": "response_item", "payload": map[string]any{"type": "custom_tool_call", "name": "apply_patch", "input": patch}},
		compactTokenRow("2026-08-05T10:10:00.100000000Z", map[string]any{"input_tokens": 10, "output_tokens": 2, "total_tokens": 12}, nil),
		map[string]any{"type": "turn_context", "payload": map[string]any{"turn_id": "turn-after", "cwd": repo, "model": "gpt-test"}},
		map[string]any{"type": "response_item", "payload": map[string]any{"type": "custom_tool_call", "name": "apply_patch", "input": patch}},
		compactTokenRow("2026-08-05T10:10:00.900000000Z", map[string]any{"input_tokens": 20, "output_tokens": 4, "total_tokens": 24}, nil),
	})

	fake := &compactBackendFake{}
	engine := &CompactSyncEngine{Client: fake}
	if err := engine.Run(context.Background(), CompactRunOptions{
		InstallationID: "installation-a", RepoRoot: repo, RepoConfigID: 11, RepoKey: "repo:a", WorkspaceID: "workspace-a", Cutoff: commitAt.Add(time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	if len(fake.buckets) != 2 {
		t.Fatalf("buckets = %+v, want one before-commit and one after-commit bucket", fake.buckets)
	}
	targets := map[string]client.AttributionTarget{}
	for _, bucket := range fake.buckets {
		targets[bucket.ChangeSetID] = bucket.InitialRevision.Allocations[0].Target
	}
	if got := targets["codex:conversation-subsecond:turn-before"]; got.Status != "bound_auto" || got.CommitSHA != commitSHA {
		t.Fatalf("before-commit target = %+v, want commit %s", got, commitSHA)
	}
	if got := targets["codex:conversation-subsecond:turn-after"]; got.Status != "unbound" || got.CommitSHA != "" {
		t.Fatalf("after-commit target = %+v, want unbound", got)
	}
}

func TestCompactSyncEngineRecordsCherryPickAsInheritedWithoutDuplicatingTokens(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	repo := compactTestRepo(t, "repo")
	baseSHA := compactCommitFile(t, repo, "base.txt", "base\n", "base")
	compactGit(t, repo, "checkout", "-q", "-b", "source")
	sourceSHA := compactCommitFile(t, repo, "feature.txt", "feature\n", "feature")
	compactGit(t, repo, "checkout", "-q", "-b", "target", baseSHA)
	compactCommitFile(t, repo, "target.txt", "target base\n", "target base")
	compactGit(t, repo, "cherry-pick", sourceSHA)
	targetSHA := compactGit(t, repo, "rev-parse", "HEAD")
	if targetSHA == sourceSHA {
		t.Fatal("test setup produced identical source and cherry-pick commits")
	}

	writeCompactState(t, CompactState{Version: 2, EnabledAt: time.Date(2026, 8, 5, 9, 0, 0, 0, time.UTC), SeenAtoms: map[string]bool{}})
	writeCompactSession(t, home, repo, "conversation-cherry", "turn-cherry", "2026-08-05T10:00:00Z", []string{filepath.Join(repo, "feature.txt")})
	fake := &compactBackendFake{}
	engine := &CompactSyncEngine{Client: fake}
	sourceAt := time.Date(2026, 8, 5, 10, 5, 0, 0, time.UTC)
	if err := engine.Run(context.Background(), CompactRunOptions{InstallationID: "installation-a", RepoRoot: repo, RepoConfigID: 11, RepoKey: "repo:a", WorkspaceID: "workspace-a", CommitSHA: sourceSHA, Branch: "source", TriggerKind: "post-commit", Cutoff: sourceAt}); err != nil {
		t.Fatal(err)
	}
	if len(fake.buckets) != 1 || fake.buckets[0].InitialRevision.Allocations[0].Target.CommitSHA != sourceSHA {
		t.Fatalf("source allocation = %+v", fake.buckets)
	}
	state, err := LoadCompactState()
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Closed) != 1 || state.Closed[0].CommitPatchID == "" {
		t.Fatalf("source compact state = %+v", state.Closed)
	}
	if targetPatchID := compactCommitPatchID(context.Background(), repo, targetSHA); targetPatchID != state.Closed[0].CommitPatchID {
		t.Fatalf("cherry-pick patch id = %q, want %q", targetPatchID, state.Closed[0].CommitPatchID)
	}
	cherryAt := sourceAt.Add(time.Minute)
	if err := QueueCompactTrigger(context.Background(), CompactTrigger{
		Kind: "post-commit", RepoConfigID: 11, RepoKey: "repo:a", WorkspaceID: "workspace-a",
		CommitSHA: targetSHA, Branch: "target", LineageKind: "cherry-pick", CapturedAt: cherryAt,
	}); err != nil {
		t.Fatal(err)
	}
	if err := engine.Run(context.Background(), CompactRunOptions{InstallationID: "installation-a", RepoRoot: repo, RepoConfigID: 11, RepoKey: "repo:a", WorkspaceID: "workspace-a", Cutoff: cherryAt}); err != nil {
		t.Fatal(err)
	}
	if len(fake.revisions) != 0 {
		t.Fatalf("cherry-pick without explicit source created lineage revisions: %+v", fake.revisions)
	}

	cherryWithSourceAt := cherryAt.Add(time.Second)
	if err := QueueCompactTrigger(context.Background(), CompactTrigger{
		Kind: "post-commit", RepoConfigID: 11, RepoKey: "repo:a", WorkspaceID: "workspace-a",
		CommitSHA: targetSHA, Branch: "target", LineageKind: "cherry-pick", SourceCommitSHA: sourceSHA, CapturedAt: cherryWithSourceAt,
	}); err != nil {
		t.Fatal(err)
	}
	state, err = LoadCompactState()
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Triggers) != 2 || state.Triggers[1].LineageKind != "cherry-pick" || state.Triggers[1].SourceCommitSHA != sourceSHA {
		t.Fatalf("cherry-pick triggers = %+v", state.Triggers)
	}
	if err := engine.Run(context.Background(), CompactRunOptions{InstallationID: "installation-a", RepoRoot: repo, RepoConfigID: 11, RepoKey: "repo:a", WorkspaceID: "workspace-a", Cutoff: cherryWithSourceAt}); err != nil {
		t.Fatal(err)
	}
	if len(fake.buckets) != 1 || len(fake.revisions) != 1 {
		t.Fatalf("buckets=%d revisions=%d, want one immutable bucket and one lineage revision", len(fake.buckets), len(fake.revisions))
	}
	target := fake.revisions[0].Allocations[0].Target
	if target.CommitSHA != sourceSHA || len(target.InheritedCommits) != 1 || target.InheritedCommits[0].CommitSHA != targetSHA || target.InheritedCommits[0].Lineage != "cherry-pick" {
		t.Fatalf("cherry-pick target = %+v", target)
	}
	assertRevisionConservation(t, fake.buckets[0], fake.revisions[0])
}

func TestCompactSyncEngineStoresOneSharedBucketAndAddsRepoAssociation(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	repoA := compactTestRepo(t, "repo-a")
	repoB := compactTestRepo(t, "repo-b")
	writeCompactState(t, CompactState{Version: 2, EnabledAt: time.Date(2026, 8, 5, 9, 0, 0, 0, time.UTC), SeenAtoms: map[string]bool{}})
	writeCompactSession(t, home, repoA, "conversation-shared", "turn-shared", "2026-08-05T10:00:00Z", []string{filepath.Join(repoA, "a.go"), filepath.Join(repoB, "b.go")})

	fake := &compactBackendFake{}
	engine := &CompactSyncEngine{Client: fake}
	cutoff := time.Date(2026, 8, 5, 10, 5, 0, 0, time.UTC)
	if err := engine.Run(context.Background(), CompactRunOptions{InstallationID: "installation-a", RepoRoot: repoA, RepoConfigID: 11, RepoKey: "repo:a", WorkspaceID: "workspace-a", Cutoff: cutoff}); err != nil {
		t.Fatal(err)
	}
	if err := engine.Run(context.Background(), CompactRunOptions{InstallationID: "installation-a", RepoRoot: repoB, RepoConfigID: 22, RepoKey: "repo:b", WorkspaceID: "workspace-b", Cutoff: cutoff}); err != nil {
		t.Fatal(err)
	}
	if len(fake.buckets) != 1 || len(fake.revisions) != 1 {
		t.Fatalf("buckets=%d revisions=%d, want one shared bucket and one association revision", len(fake.buckets), len(fake.revisions))
	}
	associated := fake.revisions[0].Allocations[0].Target.AssociatedRepoConfigIDs
	if len(associated) != 2 || associated[0] != 11 || associated[1] != 22 {
		t.Fatalf("associated repos = %v", associated)
	}
	assertRevisionConservation(t, fake.buckets[0], fake.revisions[0])
}

func TestCompactSyncEngineKeepsPendingBucketAcrossOfflineFailure(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	repo := compactTestRepo(t, "repo")
	writeCompactState(t, CompactState{Version: 2, EnabledAt: time.Date(2026, 8, 5, 9, 0, 0, 0, time.UTC), SeenAtoms: map[string]bool{}})
	writeCompactSession(t, home, repo, "conversation-a", "turn-a", "2026-08-05T10:00:00Z", []string{filepath.Join(repo, "main.go")})
	fake := &compactBackendFake{failBuckets: true}
	engine := &CompactSyncEngine{Client: fake}
	opts := CompactRunOptions{InstallationID: "installation-a", RepoRoot: repo, RepoConfigID: 11, RepoKey: "repo:a", WorkspaceID: "workspace-a", Cutoff: time.Date(2026, 8, 5, 10, 5, 0, 0, time.UTC)}
	if err := engine.Run(context.Background(), opts); err == nil {
		t.Fatal("offline upload unexpectedly succeeded")
	}
	state, err := LoadCompactState()
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Pending) != 1 || len(state.SeenAtoms) != 0 {
		t.Fatalf("offline state = %+v", state)
	}
	fake.failBuckets = false
	if err := engine.Run(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	if len(fake.buckets) != 1 {
		t.Fatalf("recovery uploaded %d buckets, want 1", len(fake.buckets))
	}
}

type compactBackendFake struct {
	buckets     []client.AttributionBucket
	revisions   []client.AttributionRevision
	failBuckets bool
}

func (f *compactBackendFake) SendAttributionBuckets(_ context.Context, buckets []client.AttributionBucket) error {
	if f.failBuckets {
		return os.ErrDeadlineExceeded
	}
	for _, bucket := range buckets {
		duplicate := false
		for _, existing := range f.buckets {
			if existing.BucketID == bucket.BucketID {
				duplicate = true
				break
			}
		}
		if !duplicate {
			f.buckets = append(f.buckets, bucket)
		}
	}
	return nil
}

func (f *compactBackendFake) SendAttributionRevision(_ context.Context, _ string, revision client.AttributionRevision) error {
	for _, existing := range f.revisions {
		if existing.RevisionID == revision.RevisionID {
			return nil
		}
	}
	f.revisions = append(f.revisions, revision)
	return nil
}

func compactTokenRow(timestamp string, last, total map[string]any) map[string]any {
	return map[string]any{
		"type": "event_msg", "timestamp": timestamp,
		"payload": map[string]any{"type": "token_count", "info": map[string]any{"last_token_usage": last, "total_token_usage": total}},
	}
}

func compactTestRepo(t *testing.T, name string) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "init", "-q", root)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	compactGit(t, root, "config", "user.email", "alice@example.com")
	compactGit(t, root, "config", "user.name", "alice")
	return root
}

func compactCommitFile(t *testing.T, repo, relativePath, content, message string) string {
	t.Helper()
	path := filepath.Join(repo, relativePath)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	compactGit(t, repo, "add", relativePath)
	compactGit(t, repo, "commit", "-q", "-m", message)
	return compactGit(t, repo, "rev-parse", "HEAD")
}

func compactGit(t *testing.T, repo string, args ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", repo}, args...)...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, output)
	}
	return strings.TrimSpace(string(output))
}

func writeJSONLines(t *testing.T, path string, rows []any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, row := range rows {
		payload, err := json.Marshal(row)
		if err != nil {
			t.Fatal(err)
		}
		lines = append(lines, string(payload))
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeCompactSQLite(t *testing.T, path string, lines []string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`CREATE TABLE logs (id INTEGER PRIMARY KEY AUTOINCREMENT, feedback_log_body TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	for _, line := range lines {
		if _, err := db.Exec(`INSERT INTO logs (feedback_log_body) VALUES (?)`, line); err != nil {
			t.Fatal(err)
		}
	}
}

func writeCompactState(t *testing.T, state CompactState) {
	t.Helper()
	if err := SaveJSON(CompactStatePath(), state); err != nil {
		t.Fatal(err)
	}
}

func writeCompactSession(t *testing.T, home, launchRepo, conversationID, turnID, timestamp string, outputPaths []string) {
	t.Helper()
	rows := []any{
		map[string]any{"type": "session_meta", "payload": map[string]any{"id": conversationID, "cwd": launchRepo}},
		map[string]any{"type": "turn_context", "payload": map[string]any{"turn_id": turnID, "cwd": launchRepo, "model": "gpt-test"}},
	}
	for _, outputPath := range outputPaths {
		rows = append(rows, map[string]any{"type": "response_item", "payload": map[string]any{"type": "custom_tool_call", "name": "apply_patch", "input": "*** Begin Patch\n*** Update File: " + outputPath + "\n*** End Patch"}})
	}
	rows = append(rows, compactTokenRow(timestamp, map[string]any{
		"input_tokens": 100, "cached_input_tokens": 40, "output_tokens": 20,
		"reasoning_output_tokens": 5, "total_tokens": 120,
	}, nil))
	writeJSONLines(t, filepath.Join(home, ".codex", "sessions", "2026", "08", "05", conversationID+".jsonl"), rows)
}

func writeCompactSessionFile(t *testing.T, path, repo, conversationID, turnID, timestamp string) {
	t.Helper()
	writeJSONLines(t, path, []any{
		map[string]any{"type": "session_meta", "payload": map[string]any{"id": conversationID, "cwd": repo}},
		map[string]any{"type": "turn_context", "payload": map[string]any{"turn_id": turnID, "cwd": repo, "model": "gpt-test"}},
		map[string]any{"type": "response_item", "payload": map[string]any{"type": "custom_tool_call", "name": "apply_patch", "input": "*** Begin Patch\n*** Update File: " + filepath.Join(repo, "main.go") + "\n*** End Patch"}},
		compactTokenRow(timestamp, map[string]any{"input_tokens": 10, "output_tokens": 2, "total_tokens": 12}, nil),
	})
}

func assertBucketConservation(t *testing.T, bucket client.AttributionBucket) {
	t.Helper()
	if len(bucket.InitialRevision.Allocations) != 1 || bucket.InitialRevision.Allocations[0].Tokens != bucket.Tokens {
		t.Fatalf("initial allocation does not conserve bucket: %+v", bucket)
	}
	if bucket.Tokens.Processed != bucket.Tokens.FreshInput+bucket.Tokens.CacheRead+bucket.Tokens.CacheWrite+bucket.Tokens.Output {
		t.Fatalf("processed token formula failed: %+v", bucket.Tokens)
	}
}

func assertRevisionConservation(t *testing.T, bucket client.AttributionBucket, revision client.AttributionRevision) {
	t.Helper()
	if len(revision.Allocations) != 1 || revision.Allocations[0].Tokens != bucket.Tokens {
		t.Fatalf("revision does not conserve bucket: bucket=%+v revision=%+v", bucket.Tokens, revision)
	}
}
