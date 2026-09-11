package attributionlocal

import (
	"context"
	"path/filepath"
	"testing"
)

// claudeTurnWritingPath is one Claude Code turn: the Write that names the file,
// the result, and the response that priced it. The workspace attributes are the
// caller's to set, because what they say is the point of these tests.
func claudeTurnWritingPath(t *testing.T, dir, turn, filePath, content string, workspace map[string]any) {
	t.Helper()
	with := func(base map[string]any) map[string]any {
		for key, value := range workspace {
			base[key] = value
		}
		return base
	}
	writePilotJSONL(t, filepath.Join(dir, "claude-code-2026-08-31.jsonl"),
		with(map[string]any{
			"event.name": "tool.call", "gen_ai.agent.type": "claude-code",
			"gen_ai.session.id": "session-claude", "gen_ai.turn.id": turn,
			"gen_ai.tool.name": "Write", "gen_ai.tool.call.id": "toolu-1",
			"gen_ai.tool.call.arguments": map[string]any{"file_path": filePath, "content": content},
		}),
		with(map[string]any{
			"event.name": "tool.result", "gen_ai.agent.type": "claude-code",
			"gen_ai.session.id": "session-claude", "gen_ai.turn.id": turn,
			"gen_ai.tool.call.id": "toolu-1", "tool.result.status": "success",
		}),
		with(map[string]any{
			"event.name": "llm.response", "gen_ai.agent.type": "claude-code",
			"gen_ai.session.id": "session-claude", "gen_ai.turn.id": turn,
			"gen_ai.response.id": "msg-1", "gen_ai.turn.end": true,
			"gen_ai.usage.input_tokens": 200, "gen_ai.usage.output_tokens": 30,
			"gen_ai.usage.total_tokens": 230,
		}),
	)
}

func scanOne(t *testing.T, dir, repo, commit string) PilotScanResult {
	t.Helper()
	result, err := ScanPilotClaims(context.Background(), PilotScanOptions{
		OutputDir: dir,
		V2ClaimScanOptions: V2ClaimScanOptions{
			RepoRoot: repo, CommitSHA: commit, RelayProviderID: 7, RepoConfigID: 8,
			WorkspaceID: "workspace-8", CheckpointEventID: "checkpoint-declared-path",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

// An agent session started above the repositories it edits carries no workspace
// this repository can be recognized from: measured on one machine, every
// structured edit named the session's parent directory, which resolves to no
// repository at all, and every commit those edits produced went unattributed.
//
// The path the edit itself declared settles it, and settles it for the whole
// turn: the response that carries the amount names no path of its own, so
// admitting the proof while dropping the price would leave a proven commit
// costing nothing.
func TestScanPilotClaimsTrustsTheDeclaredPathOverTheSessionDirectory(t *testing.T) {
	repo, commit := v2ClaimRepo(t, "feature.go", "package feature\n")
	dir := t.TempDir()
	claudeTurnWritingPath(t, dir, "session-claude:t1",
		filepath.Join(repo, "feature.go"), "package feature\n",
		// The parent of the repository, which is what a session started one
		// level up reports and what no repository can be resolved from.
		map[string]any{"workspace.path": filepath.Dir(repo)})

	result := scanOne(t, dir, repo, commit)
	if len(result.Claims) != 1 || result.Claims[0].GapReason != "" {
		t.Fatalf("claims = %+v, want the turn bound to the commit its edit produced", result.Claims)
	}
	if len(result.Usage) == 0 {
		t.Fatal("usage = 0, want the turn priced: the response has to come in with the proof")
	}
}

// The declared path outranks the workspace in the refusing direction too. A turn
// that edited another repository does not become this one's because the session
// happened to stand here.
func TestScanPilotClaimsRefusesATurnWhoseDeclaredPathIsElsewhere(t *testing.T) {
	repo, commit := v2ClaimRepo(t, "feature.go", "package feature\n")
	elsewhere := t.TempDir()
	dir := t.TempDir()
	claudeTurnWritingPath(t, dir, "session-claude:t1",
		filepath.Join(elsewhere, "feature.go"), "package feature\n",
		// The workspace says this repository; the edit says otherwise.
		map[string]any{"workspace.current_root": repo})

	result := scanOne(t, dir, repo, commit)
	if len(result.Claims) != 0 || len(result.Usage) != 0 {
		t.Fatalf("claims = %d usage = %d, want a turn that edited elsewhere refused whole",
			len(result.Claims), len(result.Usage))
	}
}

// A relative path is read against whichever repository is being scanned, so it
// resolves inside every one of them and proves membership in none. It must not
// settle the turn; the workspace attributes decide, as they did before.
func TestScanPilotClaimsDoesNotTreatARelativePathAsProof(t *testing.T) {
	repo, commit := v2ClaimRepo(t, "feature.go", "package feature\n")
	dir := t.TempDir()
	claudeTurnWritingPath(t, dir, "session-claude:t1", "feature.go", "package feature\n",
		map[string]any{"workspace.path": filepath.Dir(repo)})

	if result := scanOne(t, dir, repo, commit); len(result.Claims) != 0 {
		t.Fatalf("claims = %d, want a relative path to prove nothing and the unresolvable workspace to refuse it",
			len(result.Claims))
	}

	other := t.TempDir()
	claudeTurnWritingPath(t, other, "session-claude:t1", "feature.go", "package feature\n",
		map[string]any{"workspace.current_root": repo})
	if result := scanOne(t, other, repo, commit); len(result.Claims) != 1 {
		t.Fatalf("claims = %d, want the workspace still able to admit a turn the paths do not settle",
			len(result.Claims))
	}
}

// A turn that edited two repositories is admitted by both scans. That is what
// lets the pool bind one turn's amount to commits in either, counted once under
// the shared relation the ledger already has, rather than forcing a split
// nothing could justify. It is also the boundary worth stating: admitting the
// turn whole is what makes its amount reachable from a repository whose commit
// it proved, and the amount is not divided between them.
func TestScanPilotClaimsAdmitsATurnThatEditedTwoRepositories(t *testing.T) {
	first, firstCommit := v2ClaimRepo(t, "feature.go", "package feature\n")
	second, secondCommit := v2ClaimRepo(t, "feature.go", "package feature\n")
	dir := t.TempDir()

	turn := "session-claude:t1"
	write := func(path string) map[string]any {
		return map[string]any{
			"event.name": "tool.call", "gen_ai.agent.type": "claude-code",
			"gen_ai.session.id": "session-claude", "gen_ai.turn.id": turn,
			"gen_ai.tool.name": "Write", "gen_ai.tool.call.id": "toolu-" + filepath.Base(filepath.Dir(path)),
			"gen_ai.tool.call.arguments": map[string]any{"file_path": path, "content": "package feature\n"},
			"workspace.path":             filepath.Dir(first),
		}
	}
	writePilotJSONL(t, filepath.Join(dir, "claude-code-2026-08-31.jsonl"),
		write(filepath.Join(first, "feature.go")),
		write(filepath.Join(second, "feature.go")),
		map[string]any{
			"event.name": "llm.response", "gen_ai.agent.type": "claude-code",
			"gen_ai.session.id": "session-claude", "gen_ai.turn.id": turn,
			"gen_ai.response.id": "msg-1", "gen_ai.turn.end": true,
			"gen_ai.usage.input_tokens": 200, "gen_ai.usage.output_tokens": 30,
			"gen_ai.usage.total_tokens": 230,
			"workspace.path":            filepath.Dir(first),
		},
	)

	for _, repo := range []struct {
		root, commit, name string
	}{{first, firstCommit, "first"}, {second, secondCommit, "second"}} {
		result := scanOne(t, dir, repo.root, repo.commit)
		if len(result.Claims) != 1 || result.Claims[0].GapReason != "" {
			t.Fatalf("%s repository: claims = %+v, want the turn bound to the commit it proved there",
				repo.name, result.Claims)
		}
		if len(result.Usage) == 0 {
			t.Fatalf("%s repository: usage = 0, want the turn priced", repo.name)
		}
	}
}
