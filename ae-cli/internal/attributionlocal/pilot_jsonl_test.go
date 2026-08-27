package attributionlocal

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writePilotJSONL writes one Pilot normalized event per line, matching the shape
// observed in ~/.loongsuite-pilot/logs/output/<agent>-<date>.jsonl.
func writePilotJSONL(t *testing.T, path string, events ...map[string]any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	for _, event := range events {
		line, err := json.Marshal(event)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write(append(line, '\n')); err != nil {
			t.Fatal(err)
		}
	}
}

func TestScanPilotClaimsBindsCodexPatchTurnToCommit(t *testing.T) {
	repo, commit := v2ClaimRepo(t, "feature.go", "package feature\n")
	dir := t.TempDir()
	writePilotJSONL(t, filepath.Join(dir, "codex-2026-08-27.jsonl"),
		map[string]any{
			"event.name": "tool.call", "gen_ai.agent.type": "codex",
			"gen_ai.session.id": "session-codex", "gen_ai.turn.id": "session-codex:t1",
			"gen_ai.tool.name": "exec", "gen_ai.tool.call.id": "call-1",
			"gen_ai.tool.call.arguments": "const patch = \"*** Begin Patch\\n*** Add File: feature.go\\n+package feature\\n*** End Patch\";\nconst result = await tools.apply_patch(patch);\ntext(result);",
		},
		map[string]any{
			"event.name": "tool.result", "gen_ai.agent.type": "codex",
			"gen_ai.turn.id": "session-codex:t1", "gen_ai.tool.call.id": "call-1",
			"tool.result.status": "success",
		},
		map[string]any{
			"event.name": "llm.response", "gen_ai.agent.type": "codex",
			"gen_ai.session.id": "session-codex", "gen_ai.turn.id": "session-codex:t1",
			"gen_ai.response.id": "resp-1", "gen_ai.turn.end": true,
			"gen_ai.usage.input_tokens": 100, "gen_ai.usage.output_tokens": 20,
			"gen_ai.usage.cache_read.input_tokens": 5, "gen_ai.usage.total_tokens": 125,
		},
	)

	result, err := ScanPilotClaims(context.Background(), PilotScanOptions{
		OutputDir: dir,
		V2ClaimScanOptions: V2ClaimScanOptions{
			RepoRoot: repo, CommitSHA: commit, RelayProviderID: 7, RepoConfigID: 8,
			WorkspaceID: "workspace-8", CheckpointEventID: "checkpoint-pilot-codex",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Claims) != 1 {
		t.Fatalf("claims = %d, want 1", len(result.Claims))
	}
	claim := result.Claims[0]
	if claim.GapReason != "" {
		t.Fatalf("gap reason = %q, want none", claim.GapReason)
	}
	if len(claim.Group.CommitAllocations) != 1 || claim.Group.CommitAllocations[0].CommitSHA != commit {
		t.Fatalf("commit allocations = %+v, want one bound to %s", claim.Group.CommitAllocations, commit)
	}
}

func TestScanPilotClaimsBindsClaudeWriteTurnToCommit(t *testing.T) {
	repo, commit := v2ClaimRepo(t, "feature.go", "package feature\n")
	dir := t.TempDir()
	writePilotJSONL(t, filepath.Join(dir, "claude-code-2026-08-27.jsonl"),
		map[string]any{
			"event.name": "tool.call", "gen_ai.agent.type": "claude-code",
			"gen_ai.session.id": "session-claude", "gen_ai.turn.id": "session-claude:t1",
			"gen_ai.tool.name": "Write", "gen_ai.tool.call.id": "toolu-1",
			"gen_ai.tool.call.arguments": `{"file_path": "feature.go", "content": "package feature\n"}`,
		},
		map[string]any{
			"event.name": "tool.result", "gen_ai.agent.type": "claude-code",
			"gen_ai.turn.id": "session-claude:t1", "gen_ai.tool.call.id": "toolu-1",
			"tool.result.status": "success",
		},
		map[string]any{
			"event.name": "llm.response", "gen_ai.agent.type": "claude-code",
			"gen_ai.session.id": "session-claude", "gen_ai.turn.id": "session-claude:t1",
			"gen_ai.response.id": "msg-1", "gen_ai.turn.end": true,
			"gen_ai.usage.input_tokens": 200, "gen_ai.usage.output_tokens": 30,
			"gen_ai.usage.total_tokens": 230,
		},
	)

	result, err := ScanPilotClaims(context.Background(), PilotScanOptions{
		OutputDir: dir,
		V2ClaimScanOptions: V2ClaimScanOptions{
			RepoRoot: repo, CommitSHA: commit, RelayProviderID: 7, RepoConfigID: 8,
			WorkspaceID: "workspace-8", CheckpointEventID: "checkpoint-pilot-claude",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Claims) != 1 {
		t.Fatalf("claims = %d, want 1", len(result.Claims))
	}
	claim := result.Claims[0]
	if claim.GapReason != "" {
		t.Fatalf("gap reason = %q, want none", claim.GapReason)
	}
	if len(claim.Group.CommitAllocations) != 1 || claim.Group.CommitAllocations[0].CommitSHA != commit {
		t.Fatalf("commit allocations = %+v, want one bound to %s", claim.Group.CommitAllocations, commit)
	}
}

// Kiro reports credit, never Token. It performs no structured mutation this POC
// can bind, so it must produce a credit-unit usage event and no claim.
func TestScanPilotClaimsRecordsKiroCreditUsage(t *testing.T) {
	repo, commit := v2ClaimRepo(t, "feature.go", "package feature\n")
	dir := t.TempDir()
	writePilotJSONL(t, filepath.Join(dir, "kiro-cli-2026-08-27.jsonl"),
		map[string]any{
			"event.name": "llm.response", "gen_ai.agent.type": "kiro-cli",
			"gen_ai.session.id": "session-kiro", "gen_ai.turn.id": "session-kiro:t1:r0",
			"gen_ai.response.id": "kiro-resp-1", "gen_ai.turn.end": true,
			"kiro.credit_cost": 0.07833677691542287, "kiro.token_source": "unavailable",
		},
	)

	result, err := ScanPilotClaims(context.Background(), PilotScanOptions{
		OutputDir: dir,
		V2ClaimScanOptions: V2ClaimScanOptions{
			RepoRoot: repo, CommitSHA: commit, RelayProviderID: 7, RepoConfigID: 8,
			WorkspaceID: "workspace-8", CheckpointEventID: "checkpoint-pilot-kiro",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Usage) != 1 {
		t.Fatalf("usage events = %d, want 1", len(result.Usage))
	}
	usage := result.Usage[0]
	if usage.UsageUnit != UsageUnitCredit {
		t.Fatalf("usage unit = %q, want %q", usage.UsageUnit, UsageUnitCredit)
	}
	if usage.CreditUsage != 0.07833677691542287 {
		t.Fatalf("credit usage = %v, want the exact value Pilot reported", usage.CreditUsage)
	}
	if usage.InputTokens != 0 || usage.OutputTokens != 0 {
		t.Fatalf("kiro token counts = %d/%d, want zero: Kiro has no Token source", usage.InputTokens, usage.OutputTokens)
	}
}

// Token usage must reach the usage surface for every agent, not only the ones
// that bind to a commit.
func TestScanPilotClaimsRecordsTokenUsageForCodexAndClaude(t *testing.T) {
	repo, commit := v2ClaimRepo(t, "feature.go", "package feature\n")
	dir := t.TempDir()
	writePilotJSONL(t, filepath.Join(dir, "codex-2026-08-27.jsonl"),
		map[string]any{
			"event.name": "llm.response", "gen_ai.agent.type": "codex",
			"gen_ai.session.id": "s", "gen_ai.turn.id": "s:t1", "gen_ai.response.id": "r1",
			"gen_ai.usage.input_tokens": 100, "gen_ai.usage.output_tokens": 20,
			"gen_ai.usage.cache_read.input_tokens": 5, "gen_ai.usage.reasoning_output_tokens": 7,
		},
	)

	result, err := ScanPilotClaims(context.Background(), PilotScanOptions{
		OutputDir: dir,
		V2ClaimScanOptions: V2ClaimScanOptions{
			RepoRoot: repo, CommitSHA: commit, RelayProviderID: 7, RepoConfigID: 8,
			WorkspaceID: "workspace-8", CheckpointEventID: "checkpoint-usage",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Usage) != 1 {
		t.Fatalf("usage events = %d, want 1", len(result.Usage))
	}
	usage := result.Usage[0]
	if usage.UsageUnit != UsageUnitToken {
		t.Fatalf("usage unit = %q, want %q", usage.UsageUnit, UsageUnitToken)
	}
	if usage.InputTokens != 100 || usage.OutputTokens != 20 || usage.CachedInputTokens != 5 || usage.ReasoningTokens != 7 {
		t.Fatalf("token components = %+v, want the values Pilot reported", usage)
	}
}

// A tool call whose wrapper we refuse must fail closed, exactly as the Codex
// session-file path does.
func TestScanPilotClaimsRefusesUnrecognizedWrapper(t *testing.T) {
	repo, commit := v2ClaimRepo(t, "feature.go", "package feature\n")
	dir := t.TempDir()
	writePilotJSONL(t, filepath.Join(dir, "codex-2026-08-27.jsonl"),
		map[string]any{
			"event.name": "tool.call", "gen_ai.agent.type": "codex",
			"gen_ai.session.id": "s", "gen_ai.turn.id": "s:t1",
			"gen_ai.tool.name": "exec", "gen_ai.tool.call.id": "c1",
			"gen_ai.tool.call.arguments": "const patch = \"*** Begin Patch\\n*** Add File: feature.go\\n+package feature\\n*** End Patch\";\nconst result = await tools.apply_patch(patch);\ntext(result.output);",
		},
		map[string]any{
			"event.name": "llm.response", "gen_ai.agent.type": "codex",
			"gen_ai.session.id": "s", "gen_ai.turn.id": "s:t1", "gen_ai.response.id": "r1",
			"gen_ai.usage.input_tokens": 1, "gen_ai.usage.output_tokens": 1,
		},
	)

	result, err := ScanPilotClaims(context.Background(), PilotScanOptions{
		OutputDir: dir,
		V2ClaimScanOptions: V2ClaimScanOptions{
			RepoRoot: repo, CommitSHA: commit, RelayProviderID: 7, RepoConfigID: 8,
			WorkspaceID: "workspace-8", CheckpointEventID: "checkpoint-refuse",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Claims) != 1 || result.Claims[0].GapReason != v2GapUnrecognizedPatchWrapper {
		t.Fatalf("claims = %+v, want one claim gapped as %q", result.Claims, v2GapUnrecognizedPatchWrapper)
	}
	if len(result.Claims[0].Group.CommitAllocations) != 0 {
		t.Fatalf("refused wrapper produced an allocation: %+v", result.Claims[0].Group.CommitAllocations)
	}
}

// A tool call that plainly carries a file mutation, from a tool this source does
// not yet extract, must be reported rather than dropped. Silence here is the
// same defect that let Codex wrapper drift go unnoticed: coverage gaps must be
// countable, not invisible.
func TestScanPilotClaimsReportsUnhandledMutationTool(t *testing.T) {
	repo, commit := v2ClaimRepo(t, "feature.go", "package feature\n")
	cases := map[string]map[string]any{
		// A tool this source has never seen, recognised only by arguments that
		// name a file and its new contents.
		"unknown tool": {
			"gen_ai.tool.name":           "mcp__files__write",
			"gen_ai.tool.call.arguments": `{"path": "feature.go", "content": "package other\n"}`,
		},
		// fs_write commands whose byte-exact newline handling this source does
		// not establish from a primary source are routed here on purpose.
		"kiro fs_write insert": {
			"gen_ai.tool.name":           "fs_write",
			"gen_ai.tool.call.arguments": `{"command": "insert", "path": "feature.go", "insert_line": 1, "new_str": "package other"}`,
		},
		"kiro fs_write append": {
			"gen_ai.tool.name":           "fs_write",
			"gen_ai.tool.call.arguments": `{"command": "append", "path": "feature.go", "new_str": "package other"}`,
		},
	}
	for name, toolAttrs := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			call := map[string]any{
				"event.name": "tool.call", "gen_ai.agent.type": "claude-code",
				"gen_ai.session.id": "s", "gen_ai.turn.id": "s:t1", "gen_ai.tool.call.id": "c1",
			}
			for key, value := range toolAttrs {
				call[key] = value
			}
			writePilotJSONL(t, filepath.Join(dir, "agent-2026-08-27.jsonl"), call,
				map[string]any{
					"event.name": "llm.response", "gen_ai.agent.type": "claude-code",
					"gen_ai.session.id": "s", "gen_ai.turn.id": "s:t1", "gen_ai.response.id": "r1",
					"gen_ai.usage.input_tokens": 10, "gen_ai.usage.output_tokens": 2,
				},
			)

			result, err := ScanPilotClaims(context.Background(), PilotScanOptions{
				OutputDir: dir,
				V2ClaimScanOptions: V2ClaimScanOptions{
					RepoRoot: repo, CommitSHA: commit, RelayProviderID: 7, RepoConfigID: 8,
					WorkspaceID: "workspace-8", CheckpointEventID: "checkpoint-unhandled",
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Claims) != 1 || result.Claims[0].GapReason != pilotGapUnhandledMutationTool {
				t.Fatalf("claims = %+v, want one claim gapped as %q", result.Claims, pilotGapUnhandledMutationTool)
			}
			if len(result.Claims[0].Group.CommitAllocations) != 0 {
				t.Fatalf("unhandled mutation tool produced an allocation: %+v", result.Claims[0].Group.CommitAllocations)
			}
		})
	}
}

// A tool that does not carry a file mutation must stay silent: counting every
// unhandled call would bury the signal under Bash.
func TestScanPilotClaimsIgnoresNonMutatingTools(t *testing.T) {
	repo, commit := v2ClaimRepo(t, "feature.go", "package feature\n")
	dir := t.TempDir()
	writePilotJSONL(t, filepath.Join(dir, "claude-code-2026-08-27.jsonl"),
		map[string]any{
			"event.name": "tool.call", "gen_ai.agent.type": "claude-code",
			"gen_ai.session.id": "s", "gen_ai.turn.id": "s:t1",
			"gen_ai.tool.name": "Bash", "gen_ai.tool.call.id": "c1",
			"gen_ai.tool.call.arguments": `{"command": "ls -la", "description": "list files"}`,
		},
		map[string]any{
			"event.name": "llm.response", "gen_ai.agent.type": "claude-code",
			"gen_ai.session.id": "s", "gen_ai.turn.id": "s:t1", "gen_ai.response.id": "r1",
			"gen_ai.usage.input_tokens": 10, "gen_ai.usage.output_tokens": 2,
		},
	)

	result, err := ScanPilotClaims(context.Background(), PilotScanOptions{
		OutputDir: dir,
		V2ClaimScanOptions: V2ClaimScanOptions{
			RepoRoot: repo, CommitSHA: commit, RelayProviderID: 7, RepoConfigID: 8,
			WorkspaceID: "workspace-8", CheckpointEventID: "checkpoint-bash",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Claims) != 0 {
		t.Fatalf("claims = %+v, want none for a non-mutating tool", result.Claims)
	}
	if len(result.Usage) != 1 {
		t.Fatalf("usage events = %d, want 1: the turn still consumed Token", len(result.Usage))
	}
}

// pilotReplayRepo builds a repo whose HEAD commit rewrites one file. Every
// replay-based extractor binds against this shape: the mutation must reproduce
// the commit's content starting from its parent's.
func pilotReplayRepo(t *testing.T, path, parent, current string) (string, string) {
	t.Helper()
	repo, _ := v2ClaimRepo(t, path, parent)
	if err := os.WriteFile(filepath.Join(repo, path), []byte(current), 0o600); err != nil {
		t.Fatal(err)
	}
	gitClaim(t, repo, "add", path)
	gitClaim(t, repo, "commit", "-m", "rewrite")
	return repo, strings.TrimSpace(gitClaim(t, repo, "rev-parse", "HEAD"))
}

// pilotToolTurn writes one Pilot turn: a tool call plus the llm.response that
// closes it, which is the minimum a claim needs.
func pilotToolTurn(t *testing.T, dir, agentType, toolName, arguments string) {
	t.Helper()
	writePilotJSONL(t, filepath.Join(dir, agentType+"-2026-08-27.jsonl"),
		map[string]any{
			"event.name": "tool.call", "gen_ai.agent.type": agentType,
			"gen_ai.session.id": "s", "gen_ai.turn.id": "s:t1",
			"gen_ai.tool.name": toolName, "gen_ai.tool.call.id": "c1",
			"gen_ai.tool.call.arguments": arguments,
		},
		map[string]any{
			"event.name": "llm.response", "gen_ai.agent.type": agentType,
			"gen_ai.session.id": "s", "gen_ai.turn.id": "s:t1", "gen_ai.response.id": "r1",
			"gen_ai.usage.input_tokens": 10, "gen_ai.usage.output_tokens": 2,
		},
	)
}

func scanPilotForTest(t *testing.T, dir, repo, commit string) PilotScanResult {
	t.Helper()
	result, err := ScanPilotClaims(context.Background(), PilotScanOptions{
		OutputDir: dir,
		V2ClaimScanOptions: V2ClaimScanOptions{
			RepoRoot: repo, CommitSHA: commit, RelayProviderID: 7, RepoConfigID: 8,
			WorkspaceID: "workspace-8", CheckpointEventID: "checkpoint-pilot",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func requireBoundPilotClaim(t *testing.T, result PilotScanResult, commit string) {
	t.Helper()
	if len(result.Claims) != 1 {
		t.Fatalf("claims = %d, want 1", len(result.Claims))
	}
	claim := result.Claims[0]
	if claim.GapReason != "" {
		t.Fatalf("gap reason = %q, want none", claim.GapReason)
	}
	if len(claim.Group.CommitAllocations) != 1 || claim.Group.CommitAllocations[0].CommitSHA != commit {
		t.Fatalf("commit allocations = %+v, want one bound to %s", claim.Group.CommitAllocations, commit)
	}
}

func requireRefusedPilotClaim(t *testing.T, result PilotScanResult, reason string) {
	t.Helper()
	if len(result.Claims) != 1 || result.Claims[0].GapReason != reason {
		t.Fatalf("claims = %+v, want one claim gapped as %q", result.Claims, reason)
	}
	if len(result.Claims[0].Group.CommitAllocations) != 0 {
		t.Fatalf("refused mutation produced an allocation: %+v", result.Claims[0].Group.CommitAllocations)
	}
}

// Claude Code Edit carries the replacement, not the result, so the post-state
// has to be replayed from the commit's parent before it can be bound.
func TestScanPilotClaimsBindsClaudeEditTurnToCommit(t *testing.T) {
	repo, commit := pilotReplayRepo(t, "feature.go", "package feature\n\nfunc A() {}\n", "package feature\n\nfunc B() {}\n")
	dir := t.TempDir()
	// Claude Code always reports file_path absolute, and Pilot serialises
	// replace_all as a string rather than a JSON boolean.
	arguments, err := json.Marshal(map[string]any{
		"file_path": filepath.Join(repo, "feature.go"), "old_string": "func A() {}",
		"new_string": "func B() {}", "replace_all": "False",
	})
	if err != nil {
		t.Fatal(err)
	}
	pilotToolTurn(t, dir, "claude-code", "Edit", string(arguments))
	requireBoundPilotClaim(t, scanPilotForTest(t, dir, repo, commit), commit)
}

// Pilot serialises replace_all as a string rather than a JSON boolean, so both
// forms of true must select every occurrence.
func TestScanPilotClaimsAppliesClaudeEditReplaceAll(t *testing.T) {
	for name, replaceAll := range map[string]any{"string": "True", "boolean": true} {
		t.Run(name, func(t *testing.T) {
			repo, commit := pilotReplayRepo(t, "notes.txt", "alpha\nbeta\nalpha\n", "gamma\nbeta\ngamma\n")
			dir := t.TempDir()
			arguments, err := json.Marshal(map[string]any{
				"file_path": "notes.txt", "old_string": "alpha", "new_string": "gamma", "replace_all": replaceAll,
			})
			if err != nil {
				t.Fatal(err)
			}
			pilotToolTurn(t, dir, "claude-code", "Edit", string(arguments))
			requireBoundPilotClaim(t, scanPilotForTest(t, dir, repo, commit), commit)
		})
	}
}

// Several Edits inside one turn have to chain: the second replays against the
// first one's result, not against the commit parent.
func TestScanPilotClaimsChainsClaudeEditsWithinATurn(t *testing.T) {
	repo, commit := pilotReplayRepo(t, "notes.txt", "one\n", "three\n")
	dir := t.TempDir()
	writePilotJSONL(t, filepath.Join(dir, "claude-code-2026-08-27.jsonl"),
		map[string]any{
			"event.name": "tool.call", "gen_ai.agent.type": "claude-code",
			"gen_ai.session.id": "s", "gen_ai.turn.id": "s:t1",
			"gen_ai.tool.name": "Edit", "gen_ai.tool.call.id": "c1",
			"gen_ai.tool.call.arguments": `{"file_path": "notes.txt", "old_string": "one", "new_string": "two", "replace_all": "False"}`,
		},
		map[string]any{
			"event.name": "tool.call", "gen_ai.agent.type": "claude-code",
			"gen_ai.session.id": "s", "gen_ai.turn.id": "s:t1",
			"gen_ai.tool.name": "Edit", "gen_ai.tool.call.id": "c2",
			"gen_ai.tool.call.arguments": `{"file_path": "notes.txt", "old_string": "two", "new_string": "three", "replace_all": "False"}`,
		},
		map[string]any{
			"event.name": "llm.response", "gen_ai.agent.type": "claude-code",
			"gen_ai.session.id": "s", "gen_ai.turn.id": "s:t1", "gen_ai.response.id": "r1",
			"gen_ai.usage.input_tokens": 10, "gen_ai.usage.output_tokens": 2,
		},
	)
	requireBoundPilotClaim(t, scanPilotForTest(t, dir, repo, commit), commit)
}

// An Edit this source cannot replay must fail closed and stay countable: it
// emits a mutation with no hash, which gaps the turn rather than silently
// allocating or silently disappearing.
func TestScanPilotClaimsRefusesUnreplayableClaudeEdit(t *testing.T) {
	cases := map[string]string{
		"old string absent":             `{"file_path": "notes.txt", "old_string": "missing", "new_string": "gamma", "replace_all": "False"}`,
		"ambiguous without replace all": `{"file_path": "notes.txt", "old_string": "alpha", "new_string": "gamma", "replace_all": "False"}`,
		"file absent from parent":       `{"file_path": "created.txt", "old_string": "alpha", "new_string": "gamma", "replace_all": "False"}`,
		"empty old string":              `{"file_path": "notes.txt", "old_string": "", "new_string": "gamma", "replace_all": "True"}`,
		"path escapes the repo":         `{"file_path": "../outside.txt", "old_string": "alpha", "new_string": "gamma"}`,
	}
	for name, arguments := range cases {
		t.Run(name, func(t *testing.T) {
			repo, commit := pilotReplayRepo(t, "notes.txt", "alpha\nbeta\nalpha\n", "gamma\nbeta\ngamma\n")
			dir := t.TempDir()
			pilotToolTurn(t, dir, "claude-code", "Edit", arguments)
			requireRefusedPilotClaim(t, scanPilotForTest(t, dir, repo, commit), "invalid_structured_mutation")
		})
	}
}

// Kiro CLI fs_write `create` carries the whole post-state, so it binds the same
// way Claude Code's Write does.
func TestScanPilotClaimsBindsKiroFsWriteCreateToCommit(t *testing.T) {
	repo, commit := v2ClaimRepo(t, "feature.go", "package feature\n")
	dir := t.TempDir()
	arguments, err := json.Marshal(map[string]any{
		"command": "create", "path": filepath.Join(repo, "feature.go"),
		"file_text": "package feature\n", "summary": "add the feature package",
	})
	if err != nil {
		t.Fatal(err)
	}
	pilotToolTurn(t, dir, "kiro-cli", "fs_write", string(arguments))
	requireBoundPilotClaim(t, scanPilotForTest(t, dir, repo, commit), commit)
}

// fs_write `str_replace` carries the replacement, so it replays like Edit — but
// its schema has no replace_all, so a non-unique old_str is always refused.
func TestScanPilotClaimsBindsKiroFsWriteStrReplaceToCommit(t *testing.T) {
	repo, commit := pilotReplayRepo(t, "feature.go", "package feature\n\nfunc A() {}\n", "package feature\n\nfunc B() {}\n")
	dir := t.TempDir()
	pilotToolTurn(t, dir, "kiro-cli", "fs_write",
		`{"command": "str_replace", "path": "feature.go", "old_str": "func A() {}", "new_str": "func B() {}", "summary": "rename"}`)
	requireBoundPilotClaim(t, scanPilotForTest(t, dir, repo, commit), commit)
}

func TestScanPilotClaimsRefusesUnreplayableKiroFsWrite(t *testing.T) {
	cases := map[string]string{
		"non unique old str":      `{"command": "str_replace", "path": "notes.txt", "old_str": "alpha", "new_str": "gamma"}`,
		"old str absent":          `{"command": "str_replace", "path": "notes.txt", "old_str": "missing", "new_str": "gamma"}`,
		"str replace on new file": `{"command": "str_replace", "path": "created.txt", "old_str": "alpha", "new_str": "gamma"}`,
	}
	for name, arguments := range cases {
		t.Run(name, func(t *testing.T) {
			repo, commit := pilotReplayRepo(t, "notes.txt", "alpha\nbeta\nalpha\n", "gamma\nbeta\ngamma\n")
			dir := t.TempDir()
			pilotToolTurn(t, dir, "kiro-cli", "fs_write", arguments)
			requireRefusedPilotClaim(t, scanPilotForTest(t, dir, repo, commit), "invalid_structured_mutation")
		})
	}
}
