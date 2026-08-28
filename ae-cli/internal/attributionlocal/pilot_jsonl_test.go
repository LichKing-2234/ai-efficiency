package attributionlocal

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/ai-efficiency/ae-cli/internal/client"
	"time"
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
			"workspace.current_root": repo,
			"gen_ai.session.id":      "session-codex", "gen_ai.turn.id": "session-codex:t1",
			"gen_ai.tool.name": "exec", "gen_ai.tool.call.id": "call-1",
			"gen_ai.tool.call.arguments": "const patch = \"*** Begin Patch\\n*** Add File: feature.go\\n+package feature\\n*** End Patch\";\nconst result = await tools.apply_patch(patch);\ntext(result);",
		},
		map[string]any{
			"event.name": "tool.result", "gen_ai.agent.type": "codex",
			"workspace.current_root": repo,
			"gen_ai.turn.id":         "session-codex:t1", "gen_ai.tool.call.id": "call-1",
			"tool.result.status": "success",
		},
		map[string]any{
			"event.name": "llm.response", "gen_ai.agent.type": "codex",
			"workspace.current_root": repo,
			"gen_ai.session.id":      "session-codex", "gen_ai.turn.id": "session-codex:t1",
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
			"workspace.current_root": repo,
			"gen_ai.session.id":      "session-claude", "gen_ai.turn.id": "session-claude:t1",
			"gen_ai.tool.name": "Write", "gen_ai.tool.call.id": "toolu-1",
			"gen_ai.tool.call.arguments": `{"file_path": "feature.go", "content": "package feature\n"}`,
		},
		map[string]any{
			"event.name": "tool.result", "gen_ai.agent.type": "claude-code",
			"workspace.current_root": repo,
			"gen_ai.turn.id":         "session-claude:t1", "gen_ai.tool.call.id": "toolu-1",
			"tool.result.status": "success",
		},
		map[string]any{
			"event.name": "llm.response", "gen_ai.agent.type": "claude-code",
			"workspace.current_root": repo,
			"gen_ai.session.id":      "session-claude", "gen_ai.turn.id": "session-claude:t1",
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
			"workspace.current_root": repo,
			"gen_ai.session.id":      "session-kiro", "gen_ai.turn.id": "session-kiro:t1:r0",
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
			"workspace.current_root": repo,
			"gen_ai.session.id":      "s", "gen_ai.turn.id": "s:t1", "gen_ai.response.id": "r1",
			"gen_ai.usage.input_tokens": 100, "gen_ai.usage.output_tokens": 20,
			"gen_ai.usage.cache_read.input_tokens": 5, "gen_ai.usage.cache_creation.input_tokens": 15,
			"gen_ai.usage.reasoning_output_tokens": 7,
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
	// Pilot reports 100 input with 5 read from cache and 15 written to it, so 80
	// input Token were actually re-read. The four components must not overlap:
	// passing Pilot's 100 straight through would count the 20 cached Token twice.
	if usage.InputTokens != 80 || usage.OutputTokens != 20 || usage.CachedInputTokens != 20 || usage.ReasoningTokens != 7 {
		t.Fatalf("token components = %+v, want input 80, output 20, cached 20, reasoning 7", usage)
	}
}

// Pilot's input total is normalized upstream. If a future version stops folding
// the cache into it, the subtraction must not turn consumption negative.
func TestScanPilotClaimsFloorsInputWhenCacheExceedsReportedTotal(t *testing.T) {
	repo, commit := v2ClaimRepo(t, "feature.go", "package feature\n")
	dir := t.TempDir()
	writePilotJSONL(t, filepath.Join(dir, "codex-2026-08-27.jsonl"),
		map[string]any{
			"event.name": "llm.response", "gen_ai.agent.type": "codex",
			"workspace.current_root": repo,
			"gen_ai.session.id":      "s", "gen_ai.turn.id": "s:t1", "gen_ai.response.id": "r1",
			"gen_ai.usage.input_tokens": 5, "gen_ai.usage.output_tokens": 20,
			"gen_ai.usage.cache_read.input_tokens": 40, "gen_ai.usage.cache_creation.input_tokens": 10,
		},
	)

	result, err := ScanPilotClaims(context.Background(), PilotScanOptions{
		OutputDir: dir,
		V2ClaimScanOptions: V2ClaimScanOptions{
			RepoRoot: repo, CommitSHA: commit, RepoConfigID: 8, WorkspaceID: "workspace-8",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Usage) != 1 {
		t.Fatalf("usage events = %d, want 1", len(result.Usage))
	}
	if got := result.Usage[0]; got.InputTokens != 0 || got.CachedInputTokens != 50 {
		t.Fatalf("token components = input %d, cached %d; want input floored to 0 and cached 50",
			got.InputTokens, got.CachedInputTokens)
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
			"workspace.current_root": repo,
			"gen_ai.session.id":      "s", "gen_ai.turn.id": "s:t1",
			"gen_ai.tool.name": "exec", "gen_ai.tool.call.id": "c1",
			"gen_ai.tool.call.arguments": "const patch = \"*** Begin Patch\\n*** Add File: feature.go\\n+package feature\\n*** End Patch\";\nconst result = await tools.apply_patch(patch);\ntext(result.output);",
		},
		map[string]any{
			"event.name": "llm.response", "gen_ai.agent.type": "codex",
			"workspace.current_root": repo,
			"gen_ai.session.id":      "s", "gen_ai.turn.id": "s:t1", "gen_ai.response.id": "r1",
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
				"workspace.current_root": repo,
				"gen_ai.session.id":      "s", "gen_ai.turn.id": "s:t1", "gen_ai.tool.call.id": "c1",
			}
			for key, value := range toolAttrs {
				call[key] = value
			}
			writePilotJSONL(t, filepath.Join(dir, "agent-2026-08-27.jsonl"), call,
				map[string]any{
					"event.name": "llm.response", "gen_ai.agent.type": "claude-code",
					"workspace.current_root": repo,
					"gen_ai.session.id":      "s", "gen_ai.turn.id": "s:t1", "gen_ai.response.id": "r1",
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
			"workspace.current_root": repo,
			"gen_ai.session.id":      "s", "gen_ai.turn.id": "s:t1",
			"gen_ai.tool.name": "Bash", "gen_ai.tool.call.id": "c1",
			"gen_ai.tool.call.arguments": `{"command": "ls -la", "description": "list files"}`,
		},
		map[string]any{
			"event.name": "llm.response", "gen_ai.agent.type": "claude-code",
			"workspace.current_root": repo,
			"gen_ai.session.id":      "s", "gen_ai.turn.id": "s:t1", "gen_ai.response.id": "r1",
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
var pilotTestObservedAt = time.Date(2026, 8, 27, 10, 7, 0, 0, time.UTC)

func pilotToolTurn(t *testing.T, dir, workspace, agentType, toolName, arguments string) {
	t.Helper()
	writePilotJSONL(t, filepath.Join(dir, agentType+"-2026-08-27.jsonl"),
		map[string]any{
			"event.name": "tool.call", "gen_ai.agent.type": agentType,
			"workspace.current_root": workspace,
			"gen_ai.session.id":      "s", "gen_ai.turn.id": "s:t1",
			"gen_ai.tool.name": toolName, "gen_ai.tool.call.id": "c1",
			"gen_ai.tool.call.arguments": arguments,
		},
		map[string]any{
			"event.name": "llm.response", "gen_ai.agent.type": agentType,
			"workspace.current_root": workspace,
			"gen_ai.session.id":      "s", "gen_ai.turn.id": "s:t1", "gen_ai.response.id": "r1",
			"gen_ai.usage.input_tokens": 10, "gen_ai.usage.output_tokens": 2,
			// Real Pilot output always carries an observation time, and a claim
			// cannot be priced without one.
			"time_unix_nano": pilotTestObservedAt.UnixNano(),
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
	pilotToolTurn(t, dir, repo, "claude-code", "Edit", string(arguments))
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
			pilotToolTurn(t, dir, repo, "claude-code", "Edit", string(arguments))
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
			"workspace.current_root": repo,
			"gen_ai.session.id":      "s", "gen_ai.turn.id": "s:t1",
			"gen_ai.tool.name": "Edit", "gen_ai.tool.call.id": "c1",
			"gen_ai.tool.call.arguments": `{"file_path": "notes.txt", "old_string": "one", "new_string": "two", "replace_all": "False"}`,
		},
		map[string]any{
			"event.name": "tool.call", "gen_ai.agent.type": "claude-code",
			"workspace.current_root": repo,
			"gen_ai.session.id":      "s", "gen_ai.turn.id": "s:t1",
			"gen_ai.tool.name": "Edit", "gen_ai.tool.call.id": "c2",
			"gen_ai.tool.call.arguments": `{"file_path": "notes.txt", "old_string": "two", "new_string": "three", "replace_all": "False"}`,
		},
		map[string]any{
			"event.name": "llm.response", "gen_ai.agent.type": "claude-code",
			"workspace.current_root": repo,
			"gen_ai.session.id":      "s", "gen_ai.turn.id": "s:t1", "gen_ai.response.id": "r1",
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
			pilotToolTurn(t, dir, repo, "claude-code", "Edit", arguments)
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
	pilotToolTurn(t, dir, repo, "kiro-cli", "fs_write", string(arguments))
	requireBoundPilotClaim(t, scanPilotForTest(t, dir, repo, commit), commit)
}

// fs_write `str_replace` carries the replacement, so it replays like Edit — but
// its schema has no replace_all, so a non-unique old_str is always refused.
func TestScanPilotClaimsBindsKiroFsWriteStrReplaceToCommit(t *testing.T) {
	repo, commit := pilotReplayRepo(t, "feature.go", "package feature\n\nfunc A() {}\n", "package feature\n\nfunc B() {}\n")
	dir := t.TempDir()
	pilotToolTurn(t, dir, repo, "kiro-cli", "fs_write",
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
			pilotToolTurn(t, dir, repo, "kiro-cli", "fs_write", arguments)
			requireRefusedPilotClaim(t, scanPilotForTest(t, dir, repo, commit), "invalid_structured_mutation")
		})
	}
}

// pilotResponseEvent builds one llm.response line. Usage keys are only written
// when non-zero, matching Pilot's habit of omitting what an agent did not report.
func pilotResponseEvent(workspace, agentType, sessionID, turnID, responseID string, input, output int64) map[string]any {
	event := map[string]any{
		"event.name": "llm.response", "gen_ai.agent.type": agentType,
		"workspace.current_root": workspace,
		"gen_ai.session.id":      sessionID, "gen_ai.turn.id": turnID,
		"gen_ai.usage.input_tokens": input, "gen_ai.usage.output_tokens": output,
	}
	if responseID != "" {
		event["gen_ai.response.id"] = responseID
	}
	return event
}

func pilotUsageByEventID(t *testing.T, result PilotScanResult) map[string]LocalToolUsageEvent {
	t.Helper()
	byID := map[string]LocalToolUsageEvent{}
	for _, usage := range result.Usage {
		if _, clash := byID[usage.ToolEventID]; clash {
			t.Fatalf("two usage events share tool event id %q: %+v", usage.ToolEventID, result.Usage)
		}
		byID[usage.ToolEventID] = usage
	}
	return byID
}

// Pilot's gen_ai.turn.id is a collector-derived counter over its own session id,
// not a native identifier. Resuming a Claude Code session makes Pilot replay the
// whole transcript under a fresh session and a restarted counter, so the same
// native response reappears under a second turn id carrying the same usage.
// Summing per turn inflated the total by every replayed response.
func TestScanPilotClaimsCountsReplayedResponseOnce(t *testing.T) {
	repo, commit := v2ClaimRepo(t, "feature.go", "package feature\n")
	dir := t.TempDir()
	// The original run.
	writePilotJSONL(t, filepath.Join(dir, "claude-code-2026-08-26.jsonl"),
		pilotResponseEvent(repo, "claude-code", "session-original", "session-original:t1", "msg_011replay", 343844, 952),
	)
	// The resumed run replays the same native response under a new session and a
	// restarted counter.
	writePilotJSONL(t, filepath.Join(dir, "claude-code-2026-08-27.jsonl"),
		pilotResponseEvent(repo, "claude-code", "session-resumed", "session-resumed:t1", "msg_011replay", 343844, 952),
	)

	result := scanPilotForTest(t, dir, repo, commit)
	if len(result.Usage) != 1 {
		t.Fatalf("usage events = %d, want 1: the replayed response is the same native response", len(result.Usage))
	}
	usage := result.Usage[0]
	if usage.InputTokens != 343844 || usage.OutputTokens != 952 {
		t.Fatalf("token components = %d/%d, want the response counted exactly once", usage.InputTokens, usage.OutputTokens)
	}
	if usage.ToolEventID != "msg_011replay" {
		t.Fatalf("tool event id = %q, want the native response id", usage.ToolEventID)
	}
	// The dedupe key must be the native response id so a later scan that only
	// sees the resumed file still reports the same event to the server.
	if usage.DedupeKey != "pilot:claude-code:response:msg_011replay" {
		t.Fatalf("dedupe key = %q, want it keyed on the native response id", usage.DedupeKey)
	}
	// Ownership goes to the earliest occurrence by (source file, line), which is
	// the original run rather than the replay.
	if usage.ToolSessionID != "session-original" || usage.RawSourceLocator != "turn:session-original:t1" {
		t.Fatalf("attribution = %q/%q, want the earliest occurrence's session and turn", usage.ToolSessionID, usage.RawSourceLocator)
	}
}

// A turn holds many responses — Codex four, Claude Code up to a few hundred.
// Deduplicating on the response id must not collapse the distinct responses a
// single turn made.
func TestScanPilotClaimsCountsEveryResponseInATurn(t *testing.T) {
	repo, commit := v2ClaimRepo(t, "feature.go", "package feature\n")
	dir := t.TempDir()
	writePilotJSONL(t, filepath.Join(dir, "codex-2026-08-26.jsonl"),
		pilotResponseEvent(repo, "codex", "session-codex", "session-codex:t1", "msg_0ba1", 100, 10),
		pilotResponseEvent(repo, "codex", "session-codex", "session-codex:t1", "rs_0ba1", 200, 20),
		pilotResponseEvent(repo, "codex", "session-codex", "session-codex:t1", "msg_0ba2", 300, 30),
	)
	// A replay of one of them under a second turn adds nothing.
	writePilotJSONL(t, filepath.Join(dir, "codex-2026-08-27.jsonl"),
		pilotResponseEvent(repo, "codex", "session-replay", "session-replay:t1", "rs_0ba1", 200, 20),
	)

	result := scanPilotForTest(t, dir, repo, commit)
	if len(result.Usage) != 3 {
		t.Fatalf("usage events = %d, want 3: one per distinct native response", len(result.Usage))
	}
	byID := pilotUsageByEventID(t, result)
	for id, want := range map[string]int64{"msg_0ba1": 100, "rs_0ba1": 200, "msg_0ba2": 300} {
		usage, ok := byID[id]
		if !ok {
			t.Fatalf("response %q produced no usage event: %+v", id, result.Usage)
		}
		if usage.InputTokens != want {
			t.Fatalf("response %q input tokens = %d, want %d", id, usage.InputTokens, want)
		}
	}
}

// A response Pilot reported without a native id cannot be deduplicated. It must
// still be counted — dropping it would understate real consumption — so it is
// counted once per physical event and reported as unidentified rather than
// silently folded into anything else.
func TestScanPilotClaimsCountsUnidentifiedResponseOnce(t *testing.T) {
	repo, commit := v2ClaimRepo(t, "feature.go", "package feature\n")
	dir := t.TempDir()
	writePilotJSONL(t, filepath.Join(dir, "claude-code-2026-08-27.jsonl"),
		pilotResponseEvent(repo, "claude-code", "s", "s:t1", "", 11, 1),
		pilotResponseEvent(repo, "claude-code", "s", "s:t1", "", 22, 2),
		pilotResponseEvent(repo, "claude-code", "s", "s:t1", "msg_011identified", 33, 3),
	)

	result := scanPilotForTest(t, dir, repo, commit)
	if result.UnidentifiedResponses != 2 {
		t.Fatalf("unidentified responses = %d, want 2 reported rather than silently accepted", result.UnidentifiedResponses)
	}
	if len(result.Usage) != 3 {
		t.Fatalf("usage events = %d, want 3: neither unidentified response may be dropped or merged", len(result.Usage))
	}
	var total int64
	for _, usage := range result.Usage {
		total += usage.InputTokens
	}
	if total != 66 {
		t.Fatalf("input tokens = %d, want 66: each event counted exactly once", total)
	}
	byID := pilotUsageByEventID(t, result)
	if usage, ok := byID["msg_011identified"]; !ok || usage.InputTokens != 33 {
		t.Fatalf("identified response was merged with an unidentified one: %+v", result.Usage)
	}
	// An unidentified event is named and keyed by its own location, in a
	// namespace that can never collide with a native response id.
	for _, usage := range result.Usage {
		if usage.ToolEventID == "msg_011identified" {
			continue
		}
		if !strings.HasPrefix(usage.ToolEventID, "line:claude-code-2026-08-27.jsonl#") {
			t.Fatalf("unidentified tool event id = %q, want the event's own location", usage.ToolEventID)
		}
		if usage.DedupeKey != "pilot:claude-code:event:"+usage.ToolEventID {
			t.Fatalf("unidentified dedupe key = %q, want it keyed on the event's own location", usage.DedupeKey)
		}
	}
}

// Kiro bills in credit rather than Token, and its turn id is collector-derived
// the same way, so replay inflates credit exactly as it inflates Token. Credit
// rides on the same llm.response event as the response id, so one identity
// deduplicates both.
func TestScanPilotClaimsCountsReplayedKiroCreditOnce(t *testing.T) {
	repo, commit := v2ClaimRepo(t, "feature.go", "package feature\n")
	dir := t.TempDir()
	credit := map[string]any{
		"event.name": "llm.response", "gen_ai.agent.type": "kiro-cli",
		"workspace.current_root": repo,
		"gen_ai.session.id":      "session-original", "gen_ai.turn.id": "session-original:t1:r0",
		"gen_ai.response.id": "3f1a5f0c-0b6d-4c2e-9a51-0d9d1a2b3c4d",
		"kiro.credit_cost":   0.07833677691542287, "kiro.token_source": "unavailable",
	}
	writePilotJSONL(t, filepath.Join(dir, "kiro-cli-2026-08-26.jsonl"), credit)
	replay := map[string]any{}
	for key, value := range credit {
		replay[key] = value
	}
	replay["gen_ai.session.id"] = "session-resumed"
	replay["gen_ai.turn.id"] = "session-resumed:t1:r0"
	writePilotJSONL(t, filepath.Join(dir, "kiro-cli-2026-08-27.jsonl"), replay)

	result := scanPilotForTest(t, dir, repo, commit)
	if len(result.Usage) != 1 {
		t.Fatalf("usage events = %d, want 1: replayed credit is the same native response", len(result.Usage))
	}
	usage := result.Usage[0]
	if usage.UsageUnit != UsageUnitCredit {
		t.Fatalf("usage unit = %q, want %q", usage.UsageUnit, UsageUnitCredit)
	}
	if usage.CreditUsage != 0.07833677691542287 {
		t.Fatalf("credit usage = %v, want the exact value Pilot reported, counted once", usage.CreditUsage)
	}
}

// The owning occurrence is chosen by (source file, line), never by the order the
// scan happened to read files in, so the same directory always attributes a
// replayed response to the same turn.
func TestScanPilotClaimsAttributesReplayedResponseDeterministically(t *testing.T) {
	repo, commit := v2ClaimRepo(t, "feature.go", "package feature\n")
	for range 5 {
		dir := t.TempDir()
		writePilotJSONL(t, filepath.Join(dir, "claude-code-2026-08-27.jsonl"),
			pilotResponseEvent(repo, "claude-code", "session-late", "session-late:t1", "msg_011replay", 5, 1),
		)
		writePilotJSONL(t, filepath.Join(dir, "claude-code-2026-08-26.jsonl"),
			pilotResponseEvent(repo, "claude-code", "session-early", "session-early:t1", "msg_011replay", 5, 1),
		)
		result := scanPilotForTest(t, dir, repo, commit)
		if len(result.Usage) != 1 || result.Usage[0].ToolSessionID != "session-early" {
			t.Fatalf("usage = %+v, want one event attributed to the earliest occurrence", result.Usage)
		}
	}
}

// The checkpoint-window binding the backend still uses for agents without a
// deterministic proof refuses to bind a usage event with no observation time
// (resolveCheckpointBinding returns early on a zero timestamp). Usage sourced
// from Pilot must therefore carry the time the consumption happened.
func TestScanPilotClaimsCarriesObservationTime(t *testing.T) {
	repo, commit := v2ClaimRepo(t, "feature.go", "package feature\n")
	dir := t.TempDir()
	writePilotJSONL(t, filepath.Join(dir, "claude-code-2026-08-27.jsonl"),
		map[string]any{
			"event.name": "llm.response", "gen_ai.agent.type": "claude-code",
			"workspace.current_root": repo,
			"gen_ai.session.id":      "s", "gen_ai.turn.id": "s:t1", "gen_ai.response.id": "msg-1",
			"gen_ai.usage.input_tokens": 10, "gen_ai.usage.output_tokens": 2,
			// The moment the response happened, and a much later moment at which
			// the collector saw it. Binding must use the former: on a replay the
			// observation time is "now" while the consumption is days old.
			"time_unix_nano":          "1787797232662000000",
			"observed_time_unix_nano": "1787797261745000000",
		},
	)

	result, err := ScanPilotClaims(context.Background(), PilotScanOptions{
		OutputDir: dir,
		V2ClaimScanOptions: V2ClaimScanOptions{
			RepoRoot: repo, CommitSHA: commit, RelayProviderID: 7, RepoConfigID: 8,
			WorkspaceID: "workspace-8", CheckpointEventID: "checkpoint-time",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Usage) != 1 {
		t.Fatalf("usage events = %d, want 1", len(result.Usage))
	}
	usage := result.Usage[0]
	if usage.ObservedEndAt.IsZero() || usage.ObservedStartAt.IsZero() {
		t.Fatalf("observation times = %v/%v, want both set", usage.ObservedStartAt, usage.ObservedEndAt)
	}
	wantUnixNano := int64(1787797232662000000)
	if got := usage.ObservedEndAt.UTC().UnixNano(); got != wantUnixNano {
		t.Fatalf("ObservedEndAt = %d, want the response time %d, not the collector's observation time", got, wantUnixNano)
	}
}

// A response Pilot reports with no usable time must not silently claim time
// zero, which the backend would read as "no observation" and refuse to bind.
func TestScanPilotClaimsLeavesObservationTimeZeroWhenPilotReportsNone(t *testing.T) {
	repo, commit := v2ClaimRepo(t, "feature.go", "package feature\n")
	dir := t.TempDir()
	writePilotJSONL(t, filepath.Join(dir, "claude-code-2026-08-27.jsonl"),
		map[string]any{
			"event.name": "llm.response", "gen_ai.agent.type": "claude-code",
			"workspace.current_root": repo,
			"gen_ai.session.id":      "s", "gen_ai.turn.id": "s:t1", "gen_ai.response.id": "msg-1",
			"gen_ai.usage.input_tokens": 10, "gen_ai.usage.output_tokens": 2,
		},
	)

	result, err := ScanPilotClaims(context.Background(), PilotScanOptions{
		OutputDir: dir,
		V2ClaimScanOptions: V2ClaimScanOptions{
			RepoRoot: repo, CommitSHA: commit, RelayProviderID: 7, RepoConfigID: 8,
			WorkspaceID: "workspace-8", CheckpointEventID: "checkpoint-notime",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Usage) != 1 {
		t.Fatalf("usage events = %d, want 1: consumption is still real without a timestamp", len(result.Usage))
	}
	if !result.Usage[0].ObservedEndAt.IsZero() {
		t.Fatalf("ObservedEndAt = %v, want zero: no time was reported and none may be invented", result.Usage[0].ObservedEndAt)
	}
}

// Pilot writes every workspace's activity into one file per agent. A scan is
// scoped to one repository, so events from another workspace must not be
// counted against it. On the machine this was measured, 41.6% of the Token in
// the local output belonged to workspaces other than the repository being
// scanned — including a parent directory of it.
func TestScanPilotClaimsExcludesOtherWorkspaces(t *testing.T) {
	repo, commit := v2ClaimRepo(t, "feature.go", "package feature\n")
	other := t.TempDir()
	dir := t.TempDir()
	writePilotJSONL(t, filepath.Join(dir, "claude-code-2026-08-27.jsonl"),
		map[string]any{
			"event.name": "llm.response", "gen_ai.agent.type": "claude-code",
			"gen_ai.session.id": "s", "gen_ai.turn.id": "s:t1", "gen_ai.response.id": "mine",
			"gen_ai.usage.input_tokens": 10, "gen_ai.usage.output_tokens": 2,
			"workspace.current_root": repo, "time_unix_nano": "1787797232662000000",
		},
		map[string]any{
			"event.name": "llm.response", "gen_ai.agent.type": "claude-code",
			"gen_ai.session.id": "o", "gen_ai.turn.id": "o:t1", "gen_ai.response.id": "theirs",
			"gen_ai.usage.input_tokens": 9999, "gen_ai.usage.output_tokens": 9999,
			"workspace.current_root": other, "time_unix_nano": "1787797232662000000",
		},
		// current_root is absent on some records; workspace.path carries it.
		map[string]any{
			"event.name": "llm.response", "gen_ai.agent.type": "claude-code",
			"gen_ai.session.id": "s", "gen_ai.turn.id": "s:t2", "gen_ai.response.id": "mine-2",
			"gen_ai.usage.input_tokens": 5, "gen_ai.usage.output_tokens": 1,
			"workspace.path": repo, "time_unix_nano": "1787797232662000000",
		},
	)

	result, err := ScanPilotClaims(context.Background(), PilotScanOptions{
		OutputDir: dir,
		V2ClaimScanOptions: V2ClaimScanOptions{
			RepoRoot: repo, CommitSHA: commit, RelayProviderID: 7, RepoConfigID: 8,
			WorkspaceID: "workspace-8", CheckpointEventID: "checkpoint-ws",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Usage) != 2 {
		t.Fatalf("usage events = %d, want 2: only this repository's activity", len(result.Usage))
	}
	var total int64
	for _, u := range result.Usage {
		total += u.InputTokens + u.OutputTokens
	}
	if total != 18 {
		t.Fatalf("token total = %d, want 18: another workspace's usage leaked in", total)
	}
}

// A record naming no workspace cannot be attributed to the repository being
// scanned. It is excluded rather than assumed, and counted so the exclusion is
// visible instead of silently shrinking the total.
func TestScanPilotClaimsCountsRecordsWithNoWorkspace(t *testing.T) {
	repo, commit := v2ClaimRepo(t, "feature.go", "package feature\n")
	dir := t.TempDir()
	writePilotJSONL(t, filepath.Join(dir, "claude-code-2026-08-27.jsonl"),
		map[string]any{
			"event.name": "llm.response", "gen_ai.agent.type": "claude-code",
			"gen_ai.session.id": "s", "gen_ai.turn.id": "s:t1", "gen_ai.response.id": "nowhere",
			"gen_ai.usage.input_tokens": 10, "gen_ai.usage.output_tokens": 2,
			"time_unix_nano": "1787797232662000000",
		},
	)

	result, err := ScanPilotClaims(context.Background(), PilotScanOptions{
		OutputDir: dir,
		V2ClaimScanOptions: V2ClaimScanOptions{
			RepoRoot: repo, CommitSHA: commit, RelayProviderID: 7, RepoConfigID: 8,
			WorkspaceID: "workspace-8", CheckpointEventID: "checkpoint-nows",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Usage) != 0 {
		t.Fatalf("usage events = %d, want 0: an unattributable record may not be counted here", len(result.Usage))
	}
	if result.UnscopedRecords != 1 {
		t.Fatalf("UnscopedRecords = %d, want 1", result.UnscopedRecords)
	}
}

// Every agent Pilot covers is priced from what Pilot observed. Only Codex
// routes through the relay at all, so pricing one agent from a source the other
// two structurally cannot have would leave one commit carrying two kinds of
// number.
func TestScanPilotClaimsPricesEveryAgentLocally(t *testing.T) {
	const patch = "*** Begin Patch\n*** Add File: feature.go\n+package feature\n*** End Patch"
	repo, commit := v2ClaimRepo(t, "feature.go", "package feature\n")
	dir := t.TempDir()
	pilotToolTurn(t, dir, repo, pilotAgentCodex, "apply_patch", patch)

	result := scanPilotForTest(t, dir, repo, commit)
	if len(result.Claims) != 1 {
		t.Fatalf("claims = %d, want 1", len(result.Claims))
	}
	group := result.Claims[0].Group
	if group.TokenSource != client.AttributionV2TokenSourceCodexLocal {
		t.Fatalf("token source = %q, want %q", group.TokenSource, client.AttributionV2TokenSourceCodexLocal)
	}
	if len(group.RequestIDs) != 0 {
		t.Fatalf("request ids = %v, want none: local pricing asks the relay nothing", group.RequestIDs)
	}
	if len(group.LocalUsage) != 1 {
		t.Fatalf("local usage = %+v, want one bucket", group.LocalUsage)
	}
	bucket := group.LocalUsage[0]
	if bucket.OutputTokens != 2 || bucket.RequestCount != 1 {
		t.Fatalf("bucket = %+v, want the turn's one response priced into it", bucket)
	}
	if !bucket.BucketStartUTC.Equal(bucket.BucketStartUTC.Truncate(15 * time.Minute)) {
		t.Fatalf("bucket start %s is not aligned to a quarter hour; the backend rejects that", bucket.BucketStartUTC)
	}
	if got := bucket.InputTokens + bucket.OutputTokens + bucket.CacheCreationTokens + bucket.CacheReadTokens; got != bucket.TotalTokens {
		t.Fatalf("total %d does not equal its components %d", bucket.TotalTokens, got)
	}
}

// Claude Code has no turn concept, so Pilot numbers the turns itself and a
// resumed session restarts the counter under a new session id. A claim named by
// session and turn would then arrive twice for one piece of work. Naming it by
// the commit and the evidence keeps one name.
func TestScanPilotClaimsNamesAGroupByItsEvidenceNotItsTurn(t *testing.T) {
	repo, parent := v2ClaimRepo(t, "feature.go", "package feature\n")
	_ = parent
	current := "package feature\n\nfunc Added() {}\n"
	if err := os.WriteFile(filepath.Join(repo, "feature.go"), []byte(current), 0o600); err != nil {
		t.Fatal(err)
	}
	gitClaim(t, repo, "add", "feature.go")
	gitClaim(t, repo, "commit", "-m", "extend")
	commit := strings.TrimSpace(gitClaim(t, repo, "rev-parse", "HEAD"))

	// The same edit, observed twice under different session and turn ids — what
	// a resume produces.
	groupIDs := map[string]struct{}{}
	for _, ids := range [][2]string{{"sess-a", "sess-a:t1"}, {"sess-b", "sess-b:t13"}} {
		dir := t.TempDir()
		writePilotJSONL(t, filepath.Join(dir, "claude-code-2026-08-27.jsonl"),
			map[string]any{
				"event.name": "tool.call", "gen_ai.agent.type": pilotAgentClaude,
				"workspace.current_root": repo,
				"gen_ai.session.id":      ids[0], "gen_ai.turn.id": ids[1],
				"gen_ai.tool.name": "Write", "gen_ai.tool.call.id": "c1",
				"gen_ai.tool.call.arguments": `{"file_path":"feature.go","content":` + strconvQuote(current) + `}`,
			},
			map[string]any{
				"event.name": "llm.response", "gen_ai.agent.type": pilotAgentClaude,
				"workspace.current_root": repo,
				"gen_ai.session.id":      ids[0], "gen_ai.turn.id": ids[1], "gen_ai.response.id": "msg_1",
				"gen_ai.usage.input_tokens": 10, "gen_ai.usage.output_tokens": 2,
				"time_unix_nano": pilotTestObservedAt.UnixNano(),
			},
		)
		result := scanPilotForTest(t, dir, repo, commit)
		if len(result.Claims) != 1 {
			t.Fatalf("session %s: claims = %d, want 1", ids[0], len(result.Claims))
		}
		if result.Claims[0].GapReason != "" {
			t.Fatalf("session %s: gap %q, want a proven claim", ids[0], result.Claims[0].GapReason)
		}
		groupIDs[result.Claims[0].Group.GroupID] = struct{}{}
	}
	if len(groupIDs) != 1 {
		t.Fatalf("group ids = %d distinct values, want 1: the same evidence for the same commit is one claim", len(groupIDs))
	}
}

// A turn that proves nothing has no evidence to be named by, and is not
// delivered either, so it keeps its observational name.
func TestScanPilotClaimsKeepsTheObservationalNameForAnUnprovenTurn(t *testing.T) {
	repo, commit := v2ClaimRepo(t, "feature.go", "package feature\n")
	dir := t.TempDir()
	pilotToolTurn(t, dir, repo, pilotAgentCodex, "apply_patch", "*** Begin Patch\n*** Add File: other.go\n+package other\n*** End Patch")

	result := scanPilotForTest(t, dir, repo, commit)
	if len(result.Claims) != 1 {
		t.Fatalf("claims = %d, want 1", len(result.Claims))
	}
	claim := result.Claims[0]
	if claim.GapReason == "" {
		t.Fatal("want a gap: the patch does not match the commit's content")
	}
	if claim.Group.GroupID != claimDigest("7", "s", "s:t1") {
		t.Fatalf("group id = %q, want the session/turn name for an unproven turn", claim.Group.GroupID)
	}
}

func strconvQuote(s string) string { return strconv.Quote(s) }
