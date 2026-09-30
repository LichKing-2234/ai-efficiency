package attributionlocal

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// The Pilot source replaces the per-agent sources for usage. Commit binding must
// not move with it: the same turn, replayed against the same commit, has to
// produce the same proof whichever source reported it. This pins that, so a
// change to either reader that shifts the evidence digest fails here rather than
// silently re-attributing history.
func TestPilotAndCodexSessionSourceAgreeOnCommitEvidence(t *testing.T) {
	const patch = "*** Begin Patch\n*** Add File: feature.go\n+package feature\n*** End Patch"
	opts := V2ClaimScanOptions{
		RelayProviderID: 7, RepoConfigID: 8,
		WorkspaceID: "workspace-8", CheckpointEventID: "checkpoint-equivalence",
	}

	legacyRepo, legacyCommit := v2ClaimRepo(t, "feature.go", "package feature\n")
	home := t.TempDir()
	writeV2JSONL(t, filepath.Join(home, ".codex", "sessions", "session.jsonl"),
		map[string]any{"type": "session_meta", "payload": map[string]any{"id": "thread-1"}},
		map[string]any{"type": "turn_context", "payload": map[string]any{"turn_id": "turn-1"}},
		map[string]any{"type": "response_item", "payload": map[string]any{"type": "custom_tool_call", "name": "apply_patch", "input": patch}},
	)
	writeV2RequestRows(t, home, v2RequestLogRow{
		threadID: "thread-1", turnID: "turn-1", requestID: "req-1",
		observedAt: time.Now().UTC().Add(-time.Hour),
	})
	legacyOpts := opts
	legacyOpts.RepoRoot = legacyRepo
	legacyOpts.CommitSHA = legacyCommit
	legacyClaims, err := ScanCodexV2ClaimsFromHome(context.Background(), home, legacyOpts)
	if err != nil {
		t.Fatalf("scan Codex session source: %v", err)
	}

	pilotRepo, pilotCommit := v2ClaimRepo(t, "feature.go", "package feature\n")
	pilotDir := t.TempDir()
	pilotToolTurn(t, pilotDir, pilotRepo, pilotAgentCodex, "apply_patch", patch)
	pilotOpts := opts
	pilotOpts.RepoRoot = pilotRepo
	pilotOpts.CommitSHA = pilotCommit
	pilotResult, err := ScanPilotClaims(context.Background(), PilotScanOptions{
		V2ClaimScanOptions: pilotOpts,
		OutputDir:          pilotDir,
	})
	if err != nil {
		t.Fatalf("scan Pilot source: %v", err)
	}

	if len(legacyClaims) != 1 || len(pilotResult.Claims) != 1 {
		t.Fatalf("claims = %d legacy, %d pilot; want 1 each", len(legacyClaims), len(pilotResult.Claims))
	}
	legacy, pilot := legacyClaims[0].Group, pilotResult.Claims[0].Group
	if legacy.EvidenceDigest == "" {
		t.Fatal("legacy evidence digest is empty; the comparison would be vacuous")
	}
	if legacy.EvidenceDigest != pilot.EvidenceDigest {
		t.Fatalf("evidence digest diverged:\n  codex session source = %s\n  pilot source         = %s",
			legacy.EvidenceDigest, pilot.EvidenceDigest)
	}
	if len(legacy.CommitAllocations) != len(pilot.CommitAllocations) {
		t.Fatalf("commit allocations = %d legacy, %d pilot", len(legacy.CommitAllocations), len(pilot.CommitAllocations))
	}
	for idx := range legacy.CommitAllocations {
		if legacy.CommitAllocations[idx].EvidenceDigest != pilot.CommitAllocations[idx].EvidenceDigest {
			t.Fatalf("allocation %d evidence digest diverged: %s vs %s", idx,
				legacy.CommitAllocations[idx].EvidenceDigest, pilot.CommitAllocations[idx].EvidenceDigest)
		}
	}
	if legacyClaims[0].GapReason != "" || pilotResult.Claims[0].GapReason != "" {
		t.Fatalf("gap reasons = %q legacy, %q pilot; want neither source to report a gap",
			legacyClaims[0].GapReason, pilotResult.Claims[0].GapReason)
	}

	// The one thing that does not carry across. The Codex session source links a
	// claim to the relay request it came from, and refuses to deliver a claim
	// that has no such link. Pilot's output carries no relay request id, so its
	// claims have nothing to link and nothing to refuse on. Only the usage
	// surface reads Pilot today, so this does not reach delivery — but it has to
	// be settled before the claim surface follows.
	if len(legacy.RequestIDs) == 0 {
		t.Fatal("legacy claim carried no request id; the contrast below would be vacuous")
	}
	if len(pilot.RequestIDs) != 0 {
		t.Fatalf("pilot claim carried request ids %v; update this test and the delivery gate together", pilot.RequestIDs)
	}
}
