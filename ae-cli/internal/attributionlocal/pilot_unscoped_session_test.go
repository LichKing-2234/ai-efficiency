package attributionlocal

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// codexPilotEvents is one Codex turn as Pilot records it when its tailer opened
// the session file at the end: the workspace attributes are simply absent,
// because the session_meta line carrying cwd had already been written.
func codexPilotEvents(sessionID string) []map[string]any {
	turn := sessionID + ":t1"
	return []map[string]any{
		{
			"event.name": "tool.call", "gen_ai.agent.type": "codex",
			"gen_ai.session.id": sessionID, "gen_ai.turn.id": turn,
			"gen_ai.tool.name": "exec", "gen_ai.tool.call.id": "call-1",
			"gen_ai.tool.call.arguments": "const patch = \"*** Begin Patch\\n*** Add File: feature.go\\n+package feature\\n*** End Patch\";\nconst result = await tools.apply_patch(patch);\ntext(result);",
		},
		{
			// Every real Pilot event carries the session id, tool results
			// included: 511 of 511 on the machine this was measured against.
			"event.name": "tool.result", "gen_ai.agent.type": "codex",
			"gen_ai.session.id": sessionID, "gen_ai.turn.id": turn, "gen_ai.tool.call.id": "call-1",
			"tool.result.status": "success",
		},
		{
			"event.name": "llm.response", "gen_ai.agent.type": "codex",
			"gen_ai.session.id": sessionID, "gen_ai.turn.id": turn,
			"gen_ai.response.id": "resp-1", "gen_ai.turn.end": true,
			"gen_ai.usage.input_tokens": 100, "gen_ai.usage.output_tokens": 20,
			"gen_ai.usage.cache_read.input_tokens": 5, "gen_ai.usage.total_tokens": 125,
		},
	}
}

// Pilot's Codex tailer starts at the end of an existing session file and never
// reads the session_meta line carrying cwd, so every Codex event arrives naming
// no workspace and the scope filter dropped all of them. Measured on one
// machine that was every Codex session, including one started three days after
// Pilot was installed, so the session identity is the only thing left to place
// them by — and Codex's own session files still carry the cwd.
func TestScanPilotClaimsPlacesUnscopedCodexBySessionIdentity(t *testing.T) {
	repo, commit := v2ClaimRepo(t, "feature.go", "package feature\n")
	dir := t.TempDir()
	writePilotJSONL(t, filepath.Join(dir, "codex-2026-08-31.jsonl"), codexPilotEvents("session-codex")...)

	options := PilotScanOptions{
		OutputDir: dir,
		V2ClaimScanOptions: V2ClaimScanOptions{
			RepoRoot: repo, CommitSHA: commit, RelayProviderID: 7, RepoConfigID: 8,
			WorkspaceID: "workspace-8", CheckpointEventID: "checkpoint-unscoped-codex",
		},
	}

	dropped, err := ScanPilotClaims(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if len(dropped.Claims) != 0 || len(dropped.Usage) != 0 || dropped.UnscopedRecords == 0 {
		t.Fatalf("without a session fallback = %d claims, %d usage, %d unscoped; want everything counted and dropped",
			len(dropped.Claims), len(dropped.Usage), dropped.UnscopedRecords)
	}

	options.WorkspaceSessionIDs = map[string]struct{}{"session-codex": {}}
	placed, err := ScanPilotClaims(context.Background(), options)
	if err != nil {
		t.Fatal(err)
	}
	if len(placed.Claims) != 1 || placed.UnscopedRecords != 0 {
		t.Fatalf("with the session fallback = %d claims, %d unscoped; want the turn placed",
			len(placed.Claims), placed.UnscopedRecords)
	}
	if claim := placed.Claims[0]; claim.GapReason != "" || len(claim.Group.CommitAllocations) != 1 || claim.Group.CommitAllocations[0].CommitSHA != commit {
		t.Fatalf("placed claim = gap %q allocations %+v, want it bound to %s", claim.GapReason, claim.Group.CommitAllocations, commit)
	}
	if len(placed.Usage) == 0 {
		t.Fatal("placed usage = 0, want the turn's consumption accounted for")
	}
}

// The fallback places only what it can prove. A session this machine cannot
// find stays unscoped rather than being assumed to belong to the repository
// being scanned, and an agent whose events name no workspace but whose sessions
// are not Codex's is not placed by a Codex session identity at all.
func TestScanPilotClaimsKeepsUnknownAndNonCodexSessionsUnscoped(t *testing.T) {
	repo, commit := v2ClaimRepo(t, "feature.go", "package feature\n")
	base := V2ClaimScanOptions{
		RepoRoot: repo, CommitSHA: commit, RelayProviderID: 7, RepoConfigID: 8,
		WorkspaceID: "workspace-8", CheckpointEventID: "checkpoint-unscoped-codex",
	}
	sessions := map[string]struct{}{"session-codex": {}}

	unknown := t.TempDir()
	writePilotJSONL(t, filepath.Join(unknown, "codex-2026-08-31.jsonl"), codexPilotEvents("session-elsewhere")...)
	result, err := ScanPilotClaims(context.Background(), PilotScanOptions{OutputDir: unknown, V2ClaimScanOptions: base, WorkspaceSessionIDs: sessions})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Claims) != 0 || result.UnscopedRecords == 0 {
		t.Fatalf("unknown session = %d claims, %d unscoped; want it left unscoped", len(result.Claims), result.UnscopedRecords)
	}

	other := t.TempDir()
	events := codexPilotEvents("session-codex")
	for _, event := range events {
		event["gen_ai.agent.type"] = "claude-code"
	}
	writePilotJSONL(t, filepath.Join(other, "claude-code-2026-08-31.jsonl"), events...)
	result, err = ScanPilotClaims(context.Background(), PilotScanOptions{OutputDir: other, V2ClaimScanOptions: base, WorkspaceSessionIDs: sessions})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Claims) != 0 || result.UnscopedRecords == 0 {
		t.Fatalf("non-Codex agent = %d claims, %d unscoped; want a Codex session identity not to place it",
			len(result.Claims), result.UnscopedRecords)
	}
}

// The session identities come from the cwd Codex records on its own session
// files, so only sessions opened in the scanned repository are returned.
func TestCodexWorkspaceSessionIDsReadsOnlyTheScannedRepository(t *testing.T) {
	repo, _ := v2ClaimRepo(t, "feature.go", "package feature\n")
	home := t.TempDir()
	sessions := filepath.Join(home, ".codex", "sessions", "2026", "08", "31")
	if err := os.MkdirAll(sessions, 0o700); err != nil {
		t.Fatal(err)
	}
	write := func(name, id, cwd string) {
		body := `{"type":"session_meta","payload":{"id":"` + id + `","cwd":"` + cwd + `"}}` + "\n"
		if err := os.WriteFile(filepath.Join(sessions, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("here.jsonl", "session-here", repo)
	write("elsewhere.jsonl", "session-elsewhere", t.TempDir())

	found := CodexWorkspaceSessionIDs(context.Background(), home, repo)
	if _, ok := found["session-here"]; !ok || len(found) != 1 {
		t.Fatalf("resolved sessions = %v, want only the one opened in the scanned repository", found)
	}
}
