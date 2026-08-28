package attributionlocal

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPrepareLocalV2ClaimScanReportsNothingWithoutPilot(t *testing.T) {
	scan, err := PrepareLocalV2ClaimScan(filepath.Join(t.TempDir(), "absent"), time.Time{})
	if err != nil {
		t.Fatalf("want a missing Pilot install to be ordinary, got %v", err)
	}
	if keys := scan.SourceKeys(); len(keys) != 0 {
		t.Fatalf("source keys = %v, want none", keys)
	}
}

// The key has to move when the output moves, or a commit made after new agent
// activity would be skipped as already scanned.
func TestPrepareLocalV2ClaimScanKeyTracksTheOutput(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "codex-2026-08-27.jsonl")
	if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	first, err := PrepareLocalV2ClaimScan(dir, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.SourceKeys()) != 1 {
		t.Fatalf("source keys = %v, want exactly one: the whole directory is one source", first.SourceKeys())
	}

	if err := os.WriteFile(path, []byte("{}\n{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(time.Minute)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}
	second, err := PrepareLocalV2ClaimScan(dir, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if first.SourceKeys()[0] == second.SourceKeys()[0] {
		t.Fatal("want the source key to change once Pilot wrote more")
	}
}

// Output last written before the cutoff describes commits claimed long ago.
func TestPrepareLocalV2ClaimScanLeavesOutStaleOutput(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "codex-2026-01-01.jsonl")
	if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-200 * 24 * time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	scan, err := PrepareLocalV2ClaimScan(dir, time.Now().Add(-90*24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if keys := scan.SourceKeys(); len(keys) != 0 {
		t.Fatalf("source keys = %v, want none for output older than the window", keys)
	}
}

func TestPilotV2ClaimScanProvesACommitThroughTheSourceInterface(t *testing.T) {
	const patch = "*** Begin Patch\n*** Add File: feature.go\n+package feature\n*** End Patch"
	repo, commit := v2ClaimRepo(t, "feature.go", "package feature\n")
	dir := t.TempDir()
	pilotToolTurn(t, dir, repo, pilotAgentCodex, "apply_patch", patch)

	scan, err := PrepareLocalV2ClaimScan(dir, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	keys := scan.SourceKeys()
	if len(keys) != 1 {
		t.Fatalf("source keys = %v, want one", keys)
	}
	candidates, err := scan.ScanSource(context.Background(), keys[0], []V2ClaimScanOptions{{
		RepoRoot: repo, CommitSHA: commit, RelayProviderID: 7, RepoConfigID: 8,
		WorkspaceID: "workspace-8", CheckpointEventID: "checkpoint-1",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 {
		t.Fatalf("candidates = %d, want 1", len(candidates))
	}
	claim := candidates[0]
	if claim.GapReason != "" {
		t.Fatalf("gap = %q, want a proven claim", claim.GapReason)
	}
	if len(claim.Group.CommitAllocations) != 1 || claim.Group.CommitAllocations[0].CommitSHA != commit {
		t.Fatalf("allocations = %+v, want the commit asked about", claim.Group.CommitAllocations)
	}
	if len(UploadableV2ClaimGroups([]V2ClaimCandidate{claim})) != 1 {
		t.Fatal("want the claim to be deliverable without any relay request id")
	}
}

// A source key that does not name this scan must return nothing rather than
// silently scanning something else.
func TestPilotV2ClaimScanIgnoresAForeignSourceKey(t *testing.T) {
	repo, commit := v2ClaimRepo(t, "feature.go", "package feature\n")
	dir := t.TempDir()
	pilotToolTurn(t, dir, repo, pilotAgentCodex, "apply_patch", "*** Begin Patch\n*** Add File: feature.go\n+package feature\n*** End Patch")
	scan, err := PrepareLocalV2ClaimScan(dir, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	got, err := scan.ScanSource(context.Background(), "not-this-source", []V2ClaimScanOptions{{RepoRoot: repo, CommitSHA: commit}})
	if err != nil || len(got) != 0 {
		t.Fatalf("candidates = %d, err = %v; want nothing for a foreign key", len(got), err)
	}
}
