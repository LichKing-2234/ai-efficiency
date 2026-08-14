package attributionlocal

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ai-efficiency/ae-cli/internal/client"
)

func TestMergeV2ClaimStateFreezesProviderAndAppendsLateRequests(t *testing.T) {
	now := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	state := &V2ClaimState{Claims: []V2ClaimCandidate{{
		LocalKey: "thread-turn", UpdatedAt: now.Add(-time.Hour),
		FirstSeenAt: now.Add(-time.Hour), Group: client.AttributionV2ClaimGroup{GroupID: "group-provider-7", RelayProviderID: 7, RequestIDs: []string{"req-1"}, CommitAllocations: []client.AttributionV2CommitAllocation{{Sequence: 1, CheckpointEventID: "checkpoint-1"}}},
	}}}
	MergeV2ClaimState(state, []V2ClaimCandidate{{
		LocalKey:    "thread-turn",
		FirstSeenAt: now.Add(-time.Hour), Group: client.AttributionV2ClaimGroup{GroupID: "group-provider-10", RelayProviderID: 10, RequestIDs: []string{"req-2"}, CommitAllocations: []client.AttributionV2CommitAllocation{{Sequence: 1, CheckpointEventID: "checkpoint-2"}}},
	}}, now)
	if len(state.Claims) != 1 || state.Claims[0].Group.GroupID != "group-provider-7" || state.Claims[0].Group.RelayProviderID != 7 || strings.Join(state.Claims[0].Group.RequestIDs, ",") != "req-1,req-2" || len(state.Claims[0].Group.CommitAllocations) != 2 || state.Claims[0].Group.CommitAllocations[1].Sequence != 2 {
		t.Fatalf("merged state = %+v", state)
	}
}

func TestMergeV2ClaimStateDoesNotRenewExpiredSource(t *testing.T) {
	now := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	expired := V2ClaimCandidate{LocalKey: "old", FirstSeenAt: now.Add(-91 * 24 * time.Hour), UpdatedAt: now.Add(-time.Hour)}
	state := &V2ClaimState{Claims: []V2ClaimCandidate{expired}}
	MergeV2ClaimState(state, []V2ClaimCandidate{expired}, now)
	if len(state.Claims) != 0 {
		t.Fatalf("expired claim was renewed: %+v", state.Claims)
	}
}

func TestMergeV2ClaimStatePromotesGapWithoutLosingRequests(t *testing.T) {
	now := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	state := &V2ClaimState{Claims: []V2ClaimCandidate{{
		LocalKey: "turn", FirstSeenAt: now.Add(-time.Hour), GapReason: "commit_content_mismatch",
		Group: client.AttributionV2ClaimGroup{GroupID: "group", RelayProviderID: 7, RequestIDs: []string{"req-old"}},
	}}}
	calibration := &client.AttributionV2Calibration{Digest: "calibration", TotalTokens: 12}
	MergeV2ClaimState(state, []V2ClaimCandidate{{
		LocalKey: "turn", FirstSeenAt: now.Add(-time.Hour), Group: client.AttributionV2ClaimGroup{
			GroupID: "group", RelayProviderID: 7, EvidenceDigest: "evidence", RequestIDs: []string{"req-new"}, Calibration: calibration,
			CommitAllocations: []client.AttributionV2CommitAllocation{{Sequence: 1, CheckpointEventID: "checkpoint-1", EvidenceDigest: "evidence"}},
		},
	}}, now)
	got := state.Claims[0]
	if got.GapReason != "" || strings.Join(got.Group.RequestIDs, ",") != "req-new,req-old" || got.Group.Calibration == nil || len(got.Group.CommitAllocations) != 1 {
		t.Fatalf("promoted claim = %+v", got)
	}
}

func TestMergeV2ClaimStateFailsClosedOnLateMixedTokenSource(t *testing.T) {
	now := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	usage := client.AttributionV2LocalUsageBucket{RequestedModel: "gpt-test", BucketStartUTC: now, InputTokens: 10, OutputTokens: 2, TotalTokens: 12, RequestCount: 1}
	state := &V2ClaimState{Claims: []V2ClaimCandidate{{
		LocalKey: "turn", FirstSeenAt: now.Add(-time.Hour), GroupAcknowledged: true,
		Group: client.AttributionV2ClaimGroup{GroupID: "group", TokenSource: client.AttributionV2TokenSourceCodexLocal, LocalUsage: []client.AttributionV2LocalUsageBucket{usage}},
	}}}
	MergeV2ClaimState(state, []V2ClaimCandidate{{
		LocalKey: "turn", FirstSeenAt: now.Add(-time.Hour), GapReason: "mixed_token_sources",
		Group: client.AttributionV2ClaimGroup{GroupID: "group", TokenSource: client.AttributionV2TokenSourceRelayOfficial, RequestIDs: []string{"client:req"}},
	}}, now)
	got := state.Claims[0]
	if got.GapReason != "mixed_token_sources" || len(got.Group.RequestIDs) != 0 || len(UploadableV2ClaimGroups(state.Claims)) != 0 {
		t.Fatalf("late mixed source did not fail closed: %+v", got)
	}
}

func TestScanCodexV2ClaimsFromHomeUsesRecentActiveAndArchivedSources(t *testing.T) {
	repo, commit := v2ClaimRepo(t, "feature.go", "package feature\n")
	home := t.TempDir()
	active := filepath.Join(home, ".codex", "sessions", "active.jsonl")
	archived := filepath.Join(home, ".codex", "archived_sessions", "archived.jsonl")
	old := filepath.Join(home, ".codex", "sessions", "old.jsonl")
	for path, turn := range map[string]string{active: "active", archived: "archived", old: "old"} {
		writeV2JSONL(t, path,
			map[string]any{"type": "session_meta", "payload": map[string]any{"id": "thread-" + turn}},
			map[string]any{"type": "turn_context", "payload": map[string]any{"turn_id": "turn-" + turn}},
			map[string]any{"type": "response_item", "payload": map[string]any{"type": "custom_tool_call", "name": "apply_patch", "input": "*** Begin Patch\n*** Add File: feature.go\n+package feature\n*** End Patch"}},
		)
	}
	oldTime := time.Now().UTC().Add(-91 * 24 * time.Hour)
	if err := os.Chtimes(old, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	writeV2RequestLog(t, home, map[string]string{
		"thread-active":   "turn-active",
		"thread-archived": "turn-archived",
		"thread-old":      "turn-old",
	})

	claims, err := ScanCodexV2ClaimsFromHome(context.Background(), home, V2ClaimScanOptions{
		RepoRoot: repo, CommitSHA: commit, RelayProviderID: 7, RepoConfigID: 8, WorkspaceID: "workspace-8", CheckpointEventID: "checkpoint-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(claims) != 2 {
		t.Fatalf("claims = %+v, want only recent active and archived sources", claims)
	}
}

func TestScanCodexV2ClaimsFromHomeBatchReadsSourceOnceForMultipleCommits(t *testing.T) {
	repo := t.TempDir()
	for _, args := range [][]string{{"init"}, {"config", "user.email", "alice@example.com"}, {"config", "user.name", "Alice"}} {
		gitClaim(t, repo, args...)
	}
	if err := os.WriteFile(filepath.Join(repo, "a.go"), []byte("package feature\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitClaim(t, repo, "add", "a.go")
	gitClaim(t, repo, "commit", "-m", "add a")
	commitA := strings.TrimSpace(gitClaim(t, repo, "rev-parse", "HEAD"))
	if err := os.WriteFile(filepath.Join(repo, "b.go"), []byte("package feature\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitClaim(t, repo, "add", "b.go")
	gitClaim(t, repo, "commit", "-m", "add b")
	commitB := strings.TrimSpace(gitClaim(t, repo, "rev-parse", "HEAD"))

	home := t.TempDir()
	writeV2JSONL(t, filepath.Join(home, ".codex", "sessions", "session.jsonl"),
		map[string]any{"type": "session_meta", "payload": map[string]any{"id": "thread-batch"}},
		map[string]any{"type": "turn_context", "payload": map[string]any{"turn_id": "turn-a"}},
		map[string]any{"type": "response_item", "payload": map[string]any{"type": "custom_tool_call", "name": "apply_patch", "input": "*** Begin Patch\n*** Add File: a.go\n+package feature\n*** End Patch"}},
		map[string]any{"type": "turn_context", "payload": map[string]any{"turn_id": "turn-b"}},
		map[string]any{"type": "response_item", "payload": map[string]any{"type": "custom_tool_call", "name": "apply_patch", "input": "*** Begin Patch\n*** Add File: b.go\n+package feature\n*** End Patch"}},
	)
	writeV2RequestLog(t, home, map[string]string{"thread-batch/req-a": "turn-a", "thread-batch/req-b": "turn-b"})

	var reads int32
	originalObserver := codexV2SourceReadObserver
	codexV2SourceReadObserver = func(string) { atomic.AddInt32(&reads, 1) }
	t.Cleanup(func() { codexV2SourceReadObserver = originalObserver })
	base := V2ClaimScanOptions{RepoRoot: repo, RelayProviderID: 7, RepoConfigID: 8, RepoKey: "example.com/org/repo", WorkspaceID: "workspace-8"}
	first := base
	first.CommitSHA, first.CheckpointEventID = commitA, "checkpoint-a"
	second := base
	second.CommitSHA, second.CheckpointEventID = commitB, "checkpoint-b"
	candidates, err := ScanCodexV2ClaimsFromHomeBatch(context.Background(), home, []V2ClaimScanOptions{first, second})
	if err != nil {
		t.Fatal(err)
	}
	state := &V2ClaimState{}
	MergeV2ClaimState(state, candidates, time.Now().UTC())
	if reads != 1 {
		t.Fatalf("source reads = %d, want 1", reads)
	}
	if groups := UploadableV2ClaimGroups(state.Claims); len(groups) != 2 {
		t.Fatalf("uploadable groups = %+v, want two commit claims", groups)
	}
}

func TestCodexV2ClaimScanSourceEvidenceKeyChangesOnlyForRelevantLateRequest(t *testing.T) {
	home := t.TempDir()
	writeV2RequestLog(t, home, map[string]string{"thread-late/req-first": "turn-late"})
	first, err := PrepareCodexV2ClaimScan(context.Background(), home, time.Now().UTC().Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	turnKeys := []string{claimDigest("thread-late", "turn-late")}
	firstKey := first.SourceEvidenceKey(turnKeys)
	db, err := sql.Open("sqlite", filepath.Join(home, ".codex", "logs_2.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	body := `turn{thread.id=thread-late turn.id=turn-late}: Request completed method=POST api.path="responses" status=200 OK headers={"x-client-request-id":"req-second"}`
	if _, err := db.Exec(`INSERT INTO logs(id, ts, ts_nanos, thread_id, target, feedback_log_body) VALUES(2, ?, 0, ?, ?, ?)`, time.Now().UTC().Unix(), "thread-late", codexResponsesHTTPClientTarget, body); err != nil {
		t.Fatal(err)
	}
	second, err := PrepareCodexV2ClaimScan(context.Background(), home, time.Now().UTC().Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	secondKey := second.SourceEvidenceKey(turnKeys)
	if firstKey == secondKey {
		t.Fatal("evidence key did not change after a late Request")
	}
	if strings.Contains(secondKey, "req-") {
		t.Fatalf("evidence key exposed Request identity: %q", secondKey)
	}
	unrelatedBody := `turn{thread.id=thread-other turn.id=turn-other}: Request completed method=POST api.path="responses" status=200 OK headers={"x-client-request-id":"req-unrelated"}`
	if _, err := db.Exec(`INSERT INTO logs(id, ts, ts_nanos, thread_id, target, feedback_log_body) VALUES(3, ?, 0, ?, ?, ?)`, time.Now().UTC().Unix(), "thread-other", codexResponsesHTTPClientTarget, unrelatedBody); err != nil {
		t.Fatal(err)
	}
	third, err := PrepareCodexV2ClaimScan(context.Background(), home, time.Now().UTC().Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if third.SourceEvidenceKey(turnKeys) != secondKey {
		t.Fatal("unrelated late Request invalidated a completed source")
	}
}

func TestMergeV2ClaimTurnKeysPreservesOlderTriggerTurns(t *testing.T) {
	older := []string{claimDigest("thread-old", "turn-old")}
	newer := []string{claimDigest("thread-new", "turn-new")}
	merged := MergeV2ClaimTurnKeys(older, newer)
	if len(merged) != 2 || !slices.Contains(merged, older[0]) || !slices.Contains(merged, newer[0]) {
		t.Fatalf("merged turn keys = %v, want old and new trigger turns", merged)
	}
	if len(MergeV2ClaimTurnKeys(merged, older)) != 2 {
		t.Fatal("replayed trigger duplicated a source turn key")
	}
}

func TestCodexV2ClaimScanCancellationDoesNotReturnPartialMultiTriggerResults(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".codex", "sessions", "large.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	var body strings.Builder
	for index := 0; index < 50_000; index++ {
		body.WriteString("{\"type\":\"event_msg\",\"payload\":{\"type\":\"noise\"}}\n")
	}
	if err := os.WriteFile(path, []byte(body.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	scan, err := PrepareCodexV2ClaimScan(context.Background(), home, time.Now().UTC().Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	originalObserver := codexV2SourceReadObserver
	codexV2SourceReadObserver = func(string) { cancel() }
	t.Cleanup(func() { codexV2SourceReadObserver = originalObserver })
	options := []V2ClaimScanOptions{{CommitSHA: "commit-a", CheckpointEventID: "event-a"}, {CommitSHA: "commit-b", CheckpointEventID: "event-b"}}
	candidates, err := scan.ScanSource(ctx, scan.SourceKeys()[0], options)
	if !errors.Is(err, context.Canceled) || len(candidates) != 0 {
		t.Fatalf("cancelled scan = candidates %+v, err %v; want no partial results and context.Canceled", candidates, err)
	}
}

func TestScanCodexV2ClaimsFromLargeHomeSkipsExpiredContentsWithinBudget(t *testing.T) {
	repo, commit := v2ClaimRepo(t, "feature.go", "package feature\n")
	home := t.TempDir()
	oldTime := time.Now().UTC().Add(-91 * 24 * time.Hour)
	for index := 0; index < 2268; index++ {
		path := filepath.Join(home, ".codex", "sessions", "old", fmt.Sprintf("session-%04d.jsonl", index))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("{expired source contents}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, oldTime, oldTime); err != nil {
			t.Fatal(err)
		}
	}
	recent := filepath.Join(home, ".codex", "sessions", "recent.jsonl")
	writeV2JSONL(t, recent,
		map[string]any{"type": "session_meta", "payload": map[string]any{"id": "thread-recent"}},
		map[string]any{"type": "turn_context", "payload": map[string]any{"turn_id": "turn-recent"}},
		map[string]any{"type": "response_item", "payload": map[string]any{"type": "custom_tool_call", "name": "apply_patch", "input": "*** Begin Patch\n*** Add File: feature.go\n+package feature\n*** End Patch"}},
	)
	file, err := os.OpenFile(recent, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(strings.Repeat("{\"type\":\"event_msg\",\"payload\":{\"type\":\"noise\"}}\n", 50_000)); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	writeV2RequestLog(t, home, map[string]string{"thread-recent": "turn-recent"})
	var reads int32
	originalObserver := codexV2SourceReadObserver
	codexV2SourceReadObserver = func(string) { atomic.AddInt32(&reads, 1) }
	t.Cleanup(func() { codexV2SourceReadObserver = originalObserver })

	started := time.Now()
	claims, err := ScanCodexV2ClaimsFromHome(context.Background(), home, V2ClaimScanOptions{
		RepoRoot: repo, CommitSHA: commit, RelayProviderID: 7, RepoConfigID: 8, WorkspaceID: "workspace-8", CheckpointEventID: "checkpoint-recent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("large-home scan elapsed = %s, budget = 5s", elapsed)
	}
	if reads != 1 || len(UploadableV2ClaimGroups(claims)) != 1 {
		t.Fatalf("source reads/claims = %d/%+v, want one recent source", reads, claims)
	}
}

func TestScanCodexV2ClaimsReturnsSourceError(t *testing.T) {
	_, err := ScanCodexV2Claims(context.Background(), []string{t.TempDir()}, V2ClaimScanOptions{})
	if err == nil || !strings.Contains(err.Error(), "scan Codex v2 source") {
		t.Fatalf("source error = %v", err)
	}
}

func TestScanCodexV2ClaimsMultiRequestStableArchiveRecoveryAndPrivacy(t *testing.T) {
	repo, commit := v2ClaimRepo(t, "feature.go", "package feature\n")
	sessions := filepath.Join(t.TempDir(), "sessions")
	active := filepath.Join(sessions, "active.jsonl")
	writeV2JSONL(t, active,
		map[string]any{"type": "session_meta", "payload": map[string]any{"id": "thread-1"}},
		map[string]any{"type": "turn_context", "payload": map[string]any{"turn_id": "turn-1"}},
		map[string]any{"type": "response_item", "payload": map[string]any{"type": "custom_tool_call", "name": "apply_patch", "input": "*** Begin Patch\n*** Add File: feature.go\n+package feature\n*** End Patch"}},
		map[string]any{"type": "event_msg", "payload": map[string]any{"type": "transport", "message": `headers={"x-client-request-id":"client:req-2"}`}},
		map[string]any{"type": "event_msg", "payload": map[string]any{"type": "transport", "message": `headers={"x-client-request-id":"req-1"}`}},
		map[string]any{"type": "event_msg", "payload": map[string]any{"type": "token_count", "info": map[string]any{"last_token_usage": map[string]any{"input_tokens": 10, "output_tokens": 2}}}},
	)
	opts := V2ClaimScanOptions{RepoRoot: repo, CommitSHA: commit, RelayProviderID: 7, RepoConfigID: 8, WorkspaceID: "workspace-8", CheckpointEventID: "checkpoint-9"}
	first, err := scanV2ClaimsForTest([]string{active}, opts, "thread-1", "req-1", "req-2")
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 1 || first[0].GapReason != "" || strings.Join(first[0].Group.RequestIDs, ",") != "req-1,req-2" {
		t.Fatalf("claims = %+v", first)
	}
	groupID := first[0].Group.GroupID

	archived := filepath.Join(sessions, "archived", "moved.jsonl")
	if err := os.MkdirAll(filepath.Dir(archived), 0o755); err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(active)
	if err != nil {
		t.Fatal(err)
	}
	payload = append(payload, []byte("{\"type\":\"event_msg\",\"payload\":{\"type\":\"transport\",\"message\":\"x-client-request-id: client:req-3\"}}\n")...)
	if err := os.WriteFile(archived, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	late, err := scanV2ClaimsForTest([]string{archived}, opts, "thread-1", "req-1", "req-2", "req-3")
	if err != nil {
		t.Fatal(err)
	}
	if len(late) != 1 || late[0].Group.GroupID != groupID || strings.Join(late[0].Group.RequestIDs, ",") != "req-1,req-2,req-3" {
		t.Fatalf("late archived claim = %+v", late)
	}

	upload, err := json.Marshal(UploadableV2ClaimGroups(late))
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{repo, archived, "package feature", "apply_patch"} {
		if strings.Contains(string(upload), forbidden) {
			t.Fatalf("upload contains private source %q: %s", forbidden, upload)
		}
	}
}

func TestScanCodexV2ClaimsUsesHTTPClientRequestIdentityAndTurn(t *testing.T) {
	repo, commit := v2ClaimRepo(t, "feature.go", "package feature\n")
	home := t.TempDir()
	alias := filepath.Join(t.TempDir(), "repo-alias")
	if err := os.Symlink(repo, alias); err != nil {
		t.Fatal(err)
	}
	session := filepath.Join(home, ".codex", "sessions", "session.jsonl")
	writeV2JSONL(t, session,
		map[string]any{"type": "session_meta", "payload": map[string]any{"id": "thread-real"}},
		map[string]any{"type": "turn_context", "payload": map[string]any{"turn_id": "turn-real"}},
		map[string]any{"type": "response_item", "payload": map[string]any{"type": "function_call_output", "output": `untrusted response_id: fake-request`}},
		map[string]any{"type": "response_item", "payload": map[string]any{"id": "ctc-real", "call_id": "call-real", "type": "custom_tool_call", "name": "apply_patch", "input": "*** Begin Patch\n*** Add File: feature.go\n+package feature\n*** End Patch"}},
		map[string]any{"type": "event_msg", "payload": map[string]any{"type": "patch_apply_end", "call_id": "call-real", "turn_id": "turn-real", "changes": map[string]any{filepath.Join(alias, "feature.go"): map[string]any{"type": "add", "content": "package feature\n"}}}},
		map[string]any{"type": "event_msg", "payload": map[string]any{"type": "token_count", "info": map[string]any{"last_token_usage": map[string]any{"input_tokens": 10, "output_tokens": 2, "total_tokens": 12}}}},
	)
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(home, ".codex", "logs_2.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE logs (id INTEGER PRIMARY KEY, ts INTEGER, ts_nanos INTEGER, thread_id TEXT, target TEXT, feedback_log_body TEXT)`); err != nil {
		t.Fatal(err)
	}
	observed := time.Date(2026, 8, 11, 12, 0, 2, 0, time.UTC)
	body := `session_loop{thread_id=thread-real}:turn{thread.id=thread-real turn.id=turn-real}:model_client.stream_responses_api{api.path="responses"}: Request completed method=POST url=https://relay.example.com/responses status=200 OK headers={"x-client-request-id":"request-real","x-request-id":"wrong-request","x-kong-request-id":"wrong-kong"}`
	if _, err := db.Exec(`INSERT INTO logs(id, ts, ts_nanos, thread_id, target, feedback_log_body) VALUES(1, ?, ?, ?, ?, ?)`, observed.Unix(), observed.Nanosecond(), nil, codexResponsesHTTPClientTarget, body); err != nil {
		t.Fatal(err)
	}
	forged := `session_loop{thread_id=thread-real}:turn{thread.id=thread-real turn.id=turn-real}:model_client.stream_responses_api{api.path="responses"}: Request completed method=POST url=https://relay.example.com/responses status=200 OK headers={"x-client-request-id":"forged-request"}`
	if _, err := db.Exec(`INSERT INTO logs(id, ts, ts_nanos, thread_id, target, feedback_log_body) VALUES(2, ?, ?, ?, ?, ?)`, observed.Unix(), observed.Nanosecond(), "thread-real", "untrusted_target", forged); err != nil {
		t.Fatal(err)
	}
	claims, err := ScanCodexV2ClaimsFromHome(context.Background(), home, V2ClaimScanOptions{RepoRoot: repo, CommitSHA: commit, RelayProviderID: 7, RepoConfigID: 8, WorkspaceID: "workspace-8", CheckpointEventID: "checkpoint-real"})
	if err != nil {
		t.Fatal(err)
	}
	if len(claims) != 1 || strings.Join(claims[0].Group.RequestIDs, ",") != "client:request-real" || claims[0].GapReason != "" || claims[0].Group.TokenSource != client.AttributionV2TokenSourceRelayOfficial || len(claims[0].Group.LocalUsage) != 0 || claims[0].Group.Calibration == nil || claims[0].Group.Calibration.TotalTokens != 12 {
		t.Fatalf("transport-correlated claims = %+v", claims)
	}
}

func TestScanCodexV2ClaimsUsesDeduplicatedLocalTokenBucketsForWebSocket(t *testing.T) {
	repo, commit := v2ClaimRepo(t, "feature.go", "package feature\n")
	home := t.TempDir()
	writeV2JSONL(t, filepath.Join(home, ".codex", "sessions", "session.jsonl"),
		map[string]any{"type": "session_meta", "payload": map[string]any{"id": "thread-websocket"}},
		map[string]any{"type": "turn_context", "timestamp": "2026-08-13T12:13:00Z", "payload": map[string]any{"turn_id": "turn-websocket", "model": "gpt-test"}},
		map[string]any{"type": "response_item", "payload": map[string]any{"type": "custom_tool_call", "name": "apply_patch", "input": "*** Begin Patch\n*** Add File: feature.go\n+package feature\n*** End Patch"}},
		map[string]any{"type": "event_msg", "timestamp": "2026-08-13T12:14:00Z", "payload": map[string]any{"type": "token_count", "info": map[string]any{
			"last_token_usage":  map[string]any{"input_tokens": 100, "cached_input_tokens": 40, "cache_write_input_tokens": 10, "output_tokens": 20, "total_tokens": 120},
			"total_token_usage": map[string]any{"input_tokens": 100, "cached_input_tokens": 40, "cache_write_input_tokens": 10, "output_tokens": 20, "total_tokens": 120},
		}}},
		map[string]any{"type": "event_msg", "timestamp": "2026-08-13T12:16:00Z", "payload": map[string]any{"type": "token_count", "info": map[string]any{
			"last_token_usage":  map[string]any{"input_tokens": 30, "cached_input_tokens": 10, "output_tokens": 5, "total_tokens": 35},
			"total_token_usage": map[string]any{"input_tokens": 130, "cached_input_tokens": 50, "cache_write_input_tokens": 10, "output_tokens": 25, "total_tokens": 155},
		}}},
		map[string]any{"type": "event_msg", "timestamp": "2026-08-13T12:17:00Z", "payload": map[string]any{"type": "token_count", "info": map[string]any{
			"last_token_usage":  map[string]any{"input_tokens": 20, "output_tokens": 3, "total_tokens": 23},
			"total_token_usage": map[string]any{"input_tokens": 150, "cached_input_tokens": 50, "cache_write_input_tokens": 10, "output_tokens": 28, "total_tokens": 178},
		}}},
		map[string]any{"type": "event_msg", "timestamp": "2026-08-13T12:17:01Z", "payload": map[string]any{"type": "token_count", "info": map[string]any{
			"last_token_usage":  map[string]any{"input_tokens": 20, "output_tokens": 3, "total_tokens": 23},
			"total_token_usage": map[string]any{"input_tokens": 150, "cached_input_tokens": 50, "cache_write_input_tokens": 10, "output_tokens": 28, "total_tokens": 178},
		}}},
	)
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o700); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(home, ".codex", "logs_2.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE logs (id INTEGER PRIMARY KEY, ts INTEGER, ts_nanos INTEGER, thread_id TEXT, target TEXT, feedback_log_body TEXT)`); err != nil {
		t.Fatal(err)
	}
	rows := []struct{ target, body string }{
		{codexResponsesWebSocketEventTarget, `session_loop{thread_id=thread-websocket}:turn{thread.id=thread-websocket turn.id=turn-websocket}:session_task.run:run_turn:run_sampling_request{turn_id=turn-websocket}:stream_request:model_client.stream_responses_websocket{transport="responses_websocket" api.path="responses" websocket.warmup=false}:responses_websocket.stream_request{transport="responses_websocket" api.path="responses"}: unhandled responses event: response.in_progress`},
		{codexResponsesWebSocketCompletionTarget, `session_loop{thread_id=thread-websocket}:turn{thread.id=thread-websocket turn.id=turn-websocket}:session_task.run:run_turn: post sampling token usage turn_id=turn-websocket total_usage_tokens=178`},
	}
	for index, row := range rows {
		if _, err := db.Exec(`INSERT INTO logs(id, ts, ts_nanos, thread_id, target, feedback_log_body) VALUES(?, ?, 0, ?, ?, ?)`, index+1, time.Now().UTC().Unix(), "thread-websocket", row.target, row.body); err != nil {
			t.Fatal(err)
		}
	}
	claims, err := ScanCodexV2ClaimsFromHome(context.Background(), home, V2ClaimScanOptions{
		RepoRoot: repo, CommitSHA: commit, RelayProviderID: 7, RepoConfigID: 8,
		WorkspaceID: "workspace-8", CheckpointEventID: "checkpoint-websocket",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(claims) != 1 || claims[0].GapReason != "" || claims[0].Group.TokenSource != client.AttributionV2TokenSourceCodexLocal || len(claims[0].Group.RequestIDs) != 0 || claims[0].Group.Calibration != nil || len(claims[0].Group.LocalUsage) != 2 {
		t.Fatalf("WebSocket local claim = %+v", claims)
	}
	first, second := claims[0].Group.LocalUsage[0], claims[0].Group.LocalUsage[1]
	if first.InputTokens != 50 || first.CacheReadTokens != 40 || first.CacheCreationTokens != 10 || first.OutputTokens != 20 || first.TotalTokens != 120 || first.RequestCount != 1 ||
		second.InputTokens != 40 || second.CacheReadTokens != 10 || second.OutputTokens != 8 || second.TotalTokens != 58 || second.RequestCount != 2 {
		t.Fatalf("local usage buckets = %+v", claims[0].Group.LocalUsage)
	}
	if _, err := db.Exec(`DELETE FROM logs`); err != nil {
		t.Fatal(err)
	}
	legacyBody := `session_loop{thread_id=thread-websocket}:turn{thread.id=thread-websocket turn.id=turn-websocket}:stream_request:model_client.stream_responses_websocket{transport="responses_websocket" api.path="responses"}: websocket event: {"type":"response.completed","response":{"id":"resp-not-uploaded"}}`
	if _, err := db.Exec(`INSERT INTO logs(id, ts, ts_nanos, thread_id, target, feedback_log_body) VALUES(1, ?, 0, ?, ?, ?)`, time.Now().UTC().Unix(), "thread-websocket", codexResponsesWebSocketEventTarget, legacyBody); err != nil {
		t.Fatal(err)
	}
	legacyClaims, err := ScanCodexV2ClaimsFromHome(context.Background(), home, V2ClaimScanOptions{
		RepoRoot: repo, CommitSHA: commit, RelayProviderID: 7, RepoConfigID: 8,
		WorkspaceID: "workspace-8", CheckpointEventID: "checkpoint-websocket",
	})
	if err != nil || len(legacyClaims) != 1 || legacyClaims[0].GapReason != "" || legacyClaims[0].Group.TokenSource != client.AttributionV2TokenSourceCodexLocal {
		t.Fatalf("legacy WebSocket claim = %+v, err=%v", legacyClaims, err)
	}
	encoded, err := json.Marshal(legacyClaims[0].Group)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "resp-not-uploaded") {
		t.Fatalf("WebSocket response identity leaked into claim: %s", encoded)
	}
}

func TestParseCodexV2WebSocketTurnEvidenceRequiresCompletedResponse(t *testing.T) {
	prefix := `turn{thread.id=thread-websocket turn.id=turn-websocket}:model_client.stream_responses_websocket{transport="responses_websocket"}: websocket event: `
	for name, testCase := range map[string]struct {
		body string
		want bool
	}{
		"completed":    {body: prefix + `{"type":"response.completed","response":{"id":"resp-placeholder"}}`, want: true},
		"not complete": {body: prefix + `{"type":"response.created","response":{"id":"resp-placeholder"}}`},
		"missing id":   {body: prefix + `{"type":"response.completed","response":{}}`},
	} {
		t.Run(name, func(t *testing.T) {
			thread, turn, ok := parseCodexV2WebSocketTurnEvidence("", testCase.body)
			if ok != testCase.want || ok && (thread != "thread-websocket" || turn != "turn-websocket") {
				t.Fatalf("parse result = (%q, %q, %t), want valid=%t", thread, turn, ok, testCase.want)
			}
		})
	}
}

func TestLoadCodexV2RequestEvidenceRequiresLiteralWebSocketSpans(t *testing.T) {
	for name, rows := range map[string][]struct{ target, body string }{
		"transport": {
			{codexResponsesWebSocketEventTarget, `turn{thread.id=thread-websocket turn.id=turn-websocket}:modelXclient.streamYresponsesZwebsocket{transport="responses_websocket" websocket.warmup=false}: unhandled responses event: response.in_progress`},
			{codexResponsesWebSocketCompletionTarget, `turn{thread.id=thread-websocket turn.id=turn-websocket}:session_task.run:run_turn: post sampling token usage turn_id=turn-websocket total_usage_tokens=10`},
		},
		"completion": {
			{codexResponsesWebSocketEventTarget, `turn{thread.id=thread-websocket turn.id=turn-websocket}:model_client.stream_responses_websocket{transport="responses_websocket" websocket.warmup=false}: unhandled responses event: response.in_progress`},
			{codexResponsesWebSocketCompletionTarget, `turn{thread.id=thread-websocket turn.id=turn-websocket}:sessionXtask.run:runXturn: post sampling token usage turn_id=turn-websocket total_usage_tokens=10`},
		},
		"warmup": {
			{codexResponsesWebSocketEventTarget, `turn{thread.id=thread-websocket turn.id=turn-websocket}:model_client.stream_responses_websocket{transport="responses_websocket" websocket.warmup=true}: unhandled responses event: response.in_progress`},
			{codexResponsesWebSocketCompletionTarget, `turn{thread.id=thread-websocket turn.id=turn-websocket}:session_task.run:run_turn: post sampling token usage turn_id=turn-websocket total_usage_tokens=10`},
		},
	} {
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o755); err != nil {
				t.Fatal(err)
			}
			db, err := sql.Open("sqlite", filepath.Join(home, ".codex", "logs_2.sqlite"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if _, err := db.Exec(`CREATE TABLE logs (id INTEGER PRIMARY KEY, ts INTEGER, ts_nanos INTEGER, thread_id TEXT, target TEXT, feedback_log_body TEXT)`); err != nil {
				t.Fatal(err)
			}
			for index, row := range rows {
				if _, err := db.Exec(`INSERT INTO logs(id, ts, ts_nanos, thread_id, target, feedback_log_body) VALUES(?, ?, 0, ?, ?, ?)`, index+1, time.Now().UTC().Unix(), "thread-websocket", row.target, row.body); err != nil {
					t.Fatal(err)
				}
			}
			evidence, err := loadCodexV2RequestEvidence(context.Background(), home, time.Time{})
			if err != nil {
				t.Fatal(err)
			}
			if len(evidence) != 0 {
				t.Fatalf("near-match WebSocket span was trusted: %+v", evidence)
			}
		})
	}
}

func TestScanCodexV2ClaimsResetsLocalTokenBaselineForEachTurn(t *testing.T) {
	repo, commit := v2ClaimRepo(t, "feature.go", "package feature\n")
	path := filepath.Join(t.TempDir(), "session.jsonl")
	writeV2JSONL(t, path,
		map[string]any{"type": "session_meta", "payload": map[string]any{"id": "thread-websocket"}},
		map[string]any{"type": "turn_context", "timestamp": "2026-08-13T12:13:00Z", "payload": map[string]any{"turn_id": "turn-first", "model": "gpt-test"}},
		map[string]any{"type": "response_item", "payload": map[string]any{"type": "custom_tool_call", "name": "apply_patch", "input": "*** Begin Patch\n*** Add File: feature.go\n+package feature\n*** End Patch"}},
		map[string]any{"type": "event_msg", "timestamp": "2026-08-13T12:14:00Z", "payload": map[string]any{"type": "token_count", "info": map[string]any{
			"last_token_usage": map[string]any{"input_tokens": 100, "output_tokens": 20, "total_tokens": 120}, "total_token_usage": map[string]any{"input_tokens": 100, "output_tokens": 20, "total_tokens": 120},
		}}},
		map[string]any{"type": "turn_context", "timestamp": "2026-08-13T12:16:00Z", "payload": map[string]any{"turn_id": "turn-second", "model": "gpt-test"}},
		map[string]any{"type": "response_item", "payload": map[string]any{"type": "custom_tool_call", "name": "apply_patch", "input": "*** Begin Patch\n*** Add File: feature.go\n+package feature\n*** End Patch"}},
		map[string]any{"type": "event_msg", "timestamp": "2026-08-13T12:17:00Z", "payload": map[string]any{"type": "token_count", "info": map[string]any{
			"last_token_usage": map[string]any{"input_tokens": 10, "output_tokens": 2, "total_tokens": 12}, "total_token_usage": map[string]any{"input_tokens": 10, "output_tokens": 2, "total_tokens": 12},
		}}},
	)
	claims, err := scanCodexV2ClaimsWithEvidence(context.Background(), []string{path}, V2ClaimScanOptions{
		RepoRoot: repo, CommitSHA: commit, RelayProviderID: 7, RepoConfigID: 8, WorkspaceID: "workspace-8", CheckpointEventID: "checkpoint-websocket",
	}, []v2RequestEvidence{
		{threadID: "thread-websocket", turnID: "turn-first", webSocket: true},
		{threadID: "thread-websocket", turnID: "turn-second", webSocket: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(claims) != 2 {
		t.Fatalf("WebSocket claims = %+v, want two turns", claims)
	}
	wantTotal := map[string]int64{"turn-first": 120, "turn-second": 12}
	for _, claim := range claims {
		if claim.GapReason != "" || claim.Group.TokenSource != client.AttributionV2TokenSourceCodexLocal || len(claim.Group.LocalUsage) != 1 {
			t.Fatalf("WebSocket turn %q = %+v", claim.Group.TurnID, claim)
		}
		if got := claim.Group.LocalUsage[0].TotalTokens; got != wantTotal[claim.Group.TurnID] {
			t.Fatalf("WebSocket turn %q total = %d, want %d", claim.Group.TurnID, got, wantTotal[claim.Group.TurnID])
		}
	}
}

func TestScanCodexV2ClaimsContinuesSessionTokenBaselineAcrossTurns(t *testing.T) {
	repo, commit := v2ClaimRepo(t, "feature.go", "package feature\n")
	path := filepath.Join(t.TempDir(), "session.jsonl")
	writeV2JSONL(t, path,
		map[string]any{"type": "session_meta", "payload": map[string]any{"id": "thread-websocket"}},
		map[string]any{"type": "turn_context", "timestamp": "2026-08-13T12:13:00Z", "payload": map[string]any{"turn_id": "turn-first", "model": "gpt-test"}},
		map[string]any{"type": "response_item", "payload": map[string]any{"type": "custom_tool_call", "name": "apply_patch", "input": "*** Begin Patch\n*** Add File: feature.go\n+package feature\n*** End Patch"}},
		map[string]any{"type": "event_msg", "timestamp": "2026-08-13T12:14:00Z", "payload": map[string]any{"type": "token_count", "info": map[string]any{
			"last_token_usage": map[string]any{"input_tokens": 100, "output_tokens": 20, "total_tokens": 120}, "total_token_usage": map[string]any{"input_tokens": 100, "output_tokens": 20, "total_tokens": 120},
		}}},
		map[string]any{"type": "turn_context", "timestamp": "2026-08-13T12:16:00Z", "payload": map[string]any{"turn_id": "turn-second", "model": "gpt-test"}},
		map[string]any{"type": "response_item", "payload": map[string]any{"type": "custom_tool_call", "name": "apply_patch", "input": "*** Begin Patch\n*** Add File: feature.go\n+package feature\n*** End Patch"}},
		map[string]any{"type": "event_msg", "timestamp": "2026-08-13T12:17:00Z", "payload": map[string]any{"type": "token_count", "info": map[string]any{
			"last_token_usage": map[string]any{"input_tokens": 10, "output_tokens": 2, "total_tokens": 12}, "total_token_usage": map[string]any{"input_tokens": 110, "output_tokens": 22, "total_tokens": 132},
		}}},
	)
	claims, err := scanCodexV2ClaimsWithEvidence(context.Background(), []string{path}, V2ClaimScanOptions{
		RepoRoot: repo, CommitSHA: commit, RelayProviderID: 7, RepoConfigID: 8, WorkspaceID: "workspace-8", CheckpointEventID: "checkpoint-websocket",
	}, []v2RequestEvidence{
		{threadID: "thread-websocket", turnID: "turn-first", webSocket: true},
		{threadID: "thread-websocket", turnID: "turn-second", webSocket: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(claims) != 2 {
		t.Fatalf("WebSocket claims = %+v, want two turns", claims)
	}
	wantTotal := map[string]int64{"turn-first": 120, "turn-second": 12}
	for _, claim := range claims {
		if claim.GapReason != "" || claim.Group.TokenSource != client.AttributionV2TokenSourceCodexLocal || len(claim.Group.LocalUsage) != 1 {
			t.Fatalf("WebSocket turn %q = %+v", claim.Group.TurnID, claim)
		}
		if got := claim.Group.LocalUsage[0].TotalTokens; got != wantTotal[claim.Group.TurnID] {
			t.Fatalf("WebSocket turn %q total = %d, want %d", claim.Group.TurnID, got, wantTotal[claim.Group.TurnID])
		}
	}
}

func TestMergeV2ClaimStateRecoversFailedWebSocketScan(t *testing.T) {
	repo, commit := v2ClaimRepo(t, "feature.go", "package feature\n")
	path := filepath.Join(t.TempDir(), "session.jsonl")
	rows := []map[string]any{
		{"type": "session_meta", "payload": map[string]any{"id": "thread-websocket"}},
		{"type": "turn_context", "timestamp": "2026-08-13T12:13:00Z", "payload": map[string]any{"turn_id": "turn-websocket", "model": "gpt-test"}},
		{"type": "response_item", "payload": map[string]any{"type": "custom_tool_call", "name": "apply_patch", "input": "*** Begin Patch\n*** Add File: feature.go\n+package feature\n*** End Patch"}},
	}
	invalid := append(slices.Clone(rows), map[string]any{"type": "event_msg", "timestamp": "2026-08-13T12:14:00Z", "payload": map[string]any{"type": "token_count", "info": map[string]any{
		"last_token_usage": map[string]any{"input_tokens": 10, "output_tokens": 2, "total_tokens": 12},
	}}})
	writeV2JSONL(t, path, invalid...)
	opts := V2ClaimScanOptions{RepoRoot: repo, CommitSHA: commit, RelayProviderID: 7, RepoConfigID: 8, WorkspaceID: "workspace-8", CheckpointEventID: "checkpoint-websocket"}
	evidence := []v2RequestEvidence{{threadID: "thread-websocket", turnID: "turn-websocket", webSocket: true}}
	first, err := scanCodexV2ClaimsWithEvidence(context.Background(), []string{path}, opts, evidence)
	if err != nil {
		t.Fatal(err)
	}
	state := &V2ClaimState{}
	now := time.Date(2026, 8, 13, 13, 0, 0, 0, time.UTC)
	MergeV2ClaimState(state, first, now)
	if len(state.Claims) != 1 || state.Claims[0].GapReason != "invalid_local_usage" || state.Claims[0].Group.TokenSource != client.AttributionV2TokenSourceCodexLocal {
		t.Fatalf("initial failed WebSocket scan = %+v", state.Claims)
	}

	valid := append(slices.Clone(rows), map[string]any{"type": "event_msg", "timestamp": "2026-08-13T12:14:00Z", "payload": map[string]any{"type": "token_count", "info": map[string]any{
		"last_token_usage": map[string]any{"input_tokens": 10, "output_tokens": 2, "total_tokens": 12}, "total_token_usage": map[string]any{"input_tokens": 10, "output_tokens": 2, "total_tokens": 12},
	}}})
	writeV2JSONL(t, path, valid...)
	second, err := scanCodexV2ClaimsWithEvidence(context.Background(), []string{path}, opts, evidence)
	if err != nil {
		t.Fatal(err)
	}
	MergeV2ClaimState(state, second, now.Add(time.Minute))
	if len(state.Claims) != 1 || state.Claims[0].GapReason != "" || state.Claims[0].Group.TokenSource != client.AttributionV2TokenSourceCodexLocal || len(UploadableV2ClaimGroups(state.Claims)) != 1 {
		t.Fatalf("recovered WebSocket scan = %+v", state.Claims)
	}
}

func TestScanCodexV2ClaimsRejectsInvalidWebSocketCumulativeUsage(t *testing.T) {
	repo, commit := v2ClaimRepo(t, "feature.go", "package feature\n")
	last := map[string]any{"input_tokens": 10, "output_tokens": 2, "total_tokens": 12}
	for name, info := range map[string]map[string]any{
		"missing cumulative":    {"last_token_usage": last},
		"malformed number":      {"last_token_usage": map[string]any{"input_tokens": 10, "cached_input_tokens": "junk", "output_tokens": 2, "total_tokens": 12}, "total_token_usage": map[string]any{"input_tokens": 10, "cached_input_tokens": "junk", "output_tokens": 2, "total_tokens": 12}},
		"fractional number":     {"last_token_usage": map[string]any{"input_tokens": 10.5, "output_tokens": 2, "total_tokens": 12}, "total_token_usage": map[string]any{"input_tokens": 10.5, "output_tokens": 2, "total_tokens": 12}},
		"mismatched cumulative": {"last_token_usage": last, "total_token_usage": map[string]any{"input_tokens": 11, "output_tokens": 2, "total_tokens": 13}},
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "session.jsonl")
			writeV2JSONL(t, path,
				map[string]any{"type": "session_meta", "payload": map[string]any{"id": "thread-websocket"}},
				map[string]any{"type": "turn_context", "timestamp": "2026-08-13T12:13:00Z", "payload": map[string]any{"turn_id": "turn-websocket", "model": "gpt-test"}},
				map[string]any{"type": "response_item", "payload": map[string]any{"type": "custom_tool_call", "name": "apply_patch", "input": "*** Begin Patch\n*** Add File: feature.go\n+package feature\n*** End Patch"}},
				map[string]any{"type": "event_msg", "timestamp": "2026-08-13T12:14:00Z", "payload": map[string]any{"type": "token_count", "info": info}},
			)
			claims, err := scanCodexV2ClaimsWithEvidence(context.Background(), []string{path}, V2ClaimScanOptions{
				RepoRoot: repo, CommitSHA: commit, RelayProviderID: 7, RepoConfigID: 8, WorkspaceID: "workspace-8", CheckpointEventID: "checkpoint-websocket",
			}, []v2RequestEvidence{{threadID: "thread-websocket", turnID: "turn-websocket", webSocket: true}})
			if err != nil {
				t.Fatal(err)
			}
			if len(claims) != 1 || claims[0].GapReason != "invalid_local_usage" || len(UploadableV2ClaimGroups(claims)) != 0 {
				t.Fatalf("invalid WebSocket cumulative usage claim = %+v", claims)
			}
		})
	}
}

func TestScanCodexV2ClaimsRejectsChangedIncrementWithRepeatedCumulativeSnapshot(t *testing.T) {
	repo, commit := v2ClaimRepo(t, "feature.go", "package feature\n")
	path := filepath.Join(t.TempDir(), "session.jsonl")
	writeV2JSONL(t, path,
		map[string]any{"type": "session_meta", "payload": map[string]any{"id": "thread-websocket"}},
		map[string]any{"type": "turn_context", "timestamp": "2026-08-13T12:13:00Z", "payload": map[string]any{"turn_id": "turn-websocket", "model": "gpt-test"}},
		map[string]any{"type": "response_item", "payload": map[string]any{"type": "custom_tool_call", "name": "apply_patch", "input": "*** Begin Patch\n*** Add File: feature.go\n+package feature\n*** End Patch"}},
		map[string]any{"type": "event_msg", "timestamp": "2026-08-13T12:14:00Z", "payload": map[string]any{"type": "token_count", "info": map[string]any{
			"last_token_usage": map[string]any{"input_tokens": 10, "output_tokens": 2, "total_tokens": 12}, "total_token_usage": map[string]any{"input_tokens": 10, "output_tokens": 2, "total_tokens": 12},
		}}},
		map[string]any{"type": "event_msg", "timestamp": "2026-08-13T12:14:01Z", "payload": map[string]any{"type": "token_count", "info": map[string]any{
			"last_token_usage": map[string]any{"input_tokens": 100, "output_tokens": 50, "total_tokens": 150}, "total_token_usage": map[string]any{"input_tokens": 10, "output_tokens": 2, "total_tokens": 12},
		}}},
	)
	claims, err := scanCodexV2ClaimsWithEvidence(context.Background(), []string{path}, V2ClaimScanOptions{
		RepoRoot: repo, CommitSHA: commit, RelayProviderID: 7, RepoConfigID: 8, WorkspaceID: "workspace-8", CheckpointEventID: "checkpoint-websocket",
	}, []v2RequestEvidence{{threadID: "thread-websocket", turnID: "turn-websocket", webSocket: true}})
	if err != nil {
		t.Fatal(err)
	}
	if len(claims) != 1 || claims[0].GapReason != "invalid_local_usage" || len(UploadableV2ClaimGroups(claims)) != 0 {
		t.Fatalf("changed increment with repeated cumulative snapshot = %+v", claims)
	}
}

func TestScanCodexV2ClaimsRejectsTokenRowWithTrailingJSON(t *testing.T) {
	repo, commit := v2ClaimRepo(t, "feature.go", "package feature\n")
	path := filepath.Join(t.TempDir(), "session.jsonl")
	writeV2JSONL(t, path,
		map[string]any{"type": "session_meta", "payload": map[string]any{"id": "thread-websocket"}},
		map[string]any{"type": "turn_context", "timestamp": "2026-08-13T12:13:00Z", "payload": map[string]any{"turn_id": "turn-websocket", "model": "gpt-test"}},
		map[string]any{"type": "response_item", "payload": map[string]any{"type": "custom_tool_call", "name": "apply_patch", "input": "*** Begin Patch\n*** Add File: feature.go\n+package feature\n*** End Patch"}},
	)
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	malformed := `{"type":"event_msg","timestamp":"2026-08-13T12:14:00Z","payload":{"type":"token_count","info":{"last_token_usage":{"input_tokens":10,"output_tokens":2,"total_tokens":12},"total_token_usage":{"input_tokens":10,"output_tokens":2,"total_tokens":12}}}} trailing` + "\n"
	if _, err := file.WriteString(malformed); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	claims, err := scanCodexV2ClaimsWithEvidence(context.Background(), []string{path}, V2ClaimScanOptions{
		RepoRoot: repo, CommitSHA: commit, RelayProviderID: 7, RepoConfigID: 8, WorkspaceID: "workspace-8", CheckpointEventID: "checkpoint-websocket",
	}, []v2RequestEvidence{{threadID: "thread-websocket", turnID: "turn-websocket", webSocket: true}})
	if err != nil {
		t.Fatal(err)
	}
	if len(claims) != 1 || claims[0].GapReason != "missing_local_usage" || len(UploadableV2ClaimGroups(claims)) != 0 {
		t.Fatalf("trailing JSON token row = %+v", claims)
	}
}

func TestScanCodexV2ClaimsRejectsMixedHTTPAndWebSocketTurn(t *testing.T) {
	repo, commit := v2ClaimRepo(t, "feature.go", "package feature\n")
	path := filepath.Join(t.TempDir(), "session.jsonl")
	writeV2JSONL(t, path,
		map[string]any{"type": "session_meta", "payload": map[string]any{"id": "thread-mixed"}},
		map[string]any{"type": "turn_context", "timestamp": "2026-08-13T12:13:00Z", "payload": map[string]any{"turn_id": "turn-mixed", "model": "gpt-test"}},
		map[string]any{"type": "response_item", "payload": map[string]any{"type": "custom_tool_call", "name": "apply_patch", "input": "*** Begin Patch\n*** Add File: feature.go\n+package feature\n*** End Patch"}},
		map[string]any{"type": "event_msg", "timestamp": "2026-08-13T12:14:00Z", "payload": map[string]any{"type": "token_count", "info": map[string]any{
			"last_token_usage": map[string]any{"input_tokens": 10, "output_tokens": 2, "total_tokens": 12}, "total_token_usage": map[string]any{"input_tokens": 10, "output_tokens": 2, "total_tokens": 12},
		}}},
	)
	evidence := []v2RequestEvidence{
		{threadID: "thread-mixed", turnID: "turn-mixed", requestID: "client:req-http"},
		{threadID: "thread-mixed", turnID: "turn-mixed", webSocket: true},
	}
	claims, err := scanCodexV2ClaimsWithEvidence(context.Background(), []string{path}, V2ClaimScanOptions{
		RepoRoot: repo, CommitSHA: commit, RelayProviderID: 7, RepoConfigID: 8, WorkspaceID: "workspace-8", CheckpointEventID: "checkpoint-mixed",
	}, evidence)
	if err != nil {
		t.Fatal(err)
	}
	if len(claims) != 1 || claims[0].GapReason != "mixed_token_sources" || len(UploadableV2ClaimGroups(claims)) != 0 {
		t.Fatalf("mixed transport claim = %+v", claims)
	}
}

func TestScanCodexV2ClaimsAcceptsExecWrappedApplyPatch(t *testing.T) {
	repo, commit := v2ClaimRepo(t, "feature.go", "package feature\n")
	home := t.TempDir()
	writeV2JSONL(t, filepath.Join(home, ".codex", "sessions", "session.jsonl"),
		map[string]any{"type": "session_meta", "payload": map[string]any{"id": "thread-wrapped"}},
		map[string]any{"type": "turn_context", "payload": map[string]any{"turn_id": "turn-wrapped"}},
		map[string]any{"type": "response_item", "payload": map[string]any{
			"type": "custom_tool_call", "name": "exec",
			"input": "const patch = \"*** Begin Patch\\n*** Add File: feature.go\\n+package feature\\n*** End Patch\";\ntext(await tools.apply_patch(patch));",
		}},
	)
	writeV2RequestLog(t, home, map[string]string{"thread-wrapped/request-wrapped": "turn-wrapped"})

	claims, err := ScanCodexV2ClaimsFromHome(context.Background(), home, V2ClaimScanOptions{
		RepoRoot: repo, CommitSHA: commit, RelayProviderID: 7, RepoConfigID: 8,
		WorkspaceID: "workspace-8", CheckpointEventID: "checkpoint-wrapped",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(claims) != 1 || claims[0].GapReason != "" || len(UploadableV2ClaimGroups(claims)) != 1 {
		t.Fatalf("wrapped apply_patch claim = %+v, want one uploadable deterministic claim", claims)
	}
}

func TestScanCodexV2ClaimsRejectsAmbiguousExecWrappedPatches(t *testing.T) {
	patch := `"*** Begin Patch\n*** Add File: feature.go\n+package feature\n*** End Patch"`
	cases := map[string]string{
		"comment marker":        "// tools.apply_patch(patch)\nconst patch = " + patch + ";",
		"unrelated variable":    "const patch = " + patch + ";\ntext(await tools.apply_patch(other));",
		"malformed call":        "const patch = " + patch + ";\ntools.apply_patch(patch",
		"multiple calls":        "const patch = " + patch + ";\ntext(await tools.apply_patch(patch));\ntext(await tools.apply_patch(patch));",
		"patch outside binding": "const other = " + patch + ";\ntext(await tools.apply_patch(patch));",
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			repo, commit := v2ClaimRepo(t, "feature.go", "package feature\n")
			home := t.TempDir()
			writeV2JSONL(t, filepath.Join(home, ".codex", "sessions", "session.jsonl"),
				map[string]any{"type": "session_meta", "payload": map[string]any{"id": "thread-ambiguous"}},
				map[string]any{"type": "turn_context", "payload": map[string]any{"turn_id": "turn-ambiguous"}},
				map[string]any{"type": "response_item", "payload": map[string]any{"type": "custom_tool_call", "name": "exec", "input": input}},
			)
			writeV2RequestLog(t, home, map[string]string{"thread-ambiguous/request-ambiguous": "turn-ambiguous"})
			claims, err := ScanCodexV2ClaimsFromHome(context.Background(), home, V2ClaimScanOptions{
				RepoRoot: repo, CommitSHA: commit, RelayProviderID: 7, RepoConfigID: 8,
				WorkspaceID: "workspace-8", CheckpointEventID: "checkpoint-ambiguous",
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(claims) != 1 || claims[0].GapReason != "missing_structured_mutation" || len(UploadableV2ClaimGroups(claims)) != 0 {
				t.Fatalf("ambiguous wrapped patch was accepted: %+v", claims)
			}
		})
	}
}

func TestScanCodexV2ClaimsRejectsWrongHeadersTargetsAndUnmatchedTurns(t *testing.T) {
	repo, commit := v2ClaimRepo(t, "feature.go", "package feature\n")
	home := t.TempDir()
	session := filepath.Join(home, ".codex", "sessions", "session.jsonl")
	writeV2JSONL(t, session,
		map[string]any{"type": "session_meta", "payload": map[string]any{"id": "thread-real"}},
		map[string]any{"type": "turn_context", "payload": map[string]any{"turn_id": "turn-real"}},
		map[string]any{"type": "response_item", "payload": map[string]any{"id": "ctc-real", "type": "custom_tool_call", "name": "apply_patch", "input": "*** Begin Patch\n*** Add File: feature.go\n+package feature\n*** End Patch"}},
	)
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(home, ".codex", "logs_2.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE logs (id INTEGER PRIMARY KEY, ts INTEGER, ts_nanos INTEGER, thread_id TEXT, target TEXT, feedback_log_body TEXT)`); err != nil {
		t.Fatal(err)
	}
	rows := []struct{ target, body string }{
		{codexResponsesHTTPClientTarget, `turn{thread.id=thread-real turn.id=turn-real}: Request completed method=POST api.path="responses" status=200 OK headers={"x-request-id":"wrong-request","x-kong-request-id":"wrong-kong"}`},
		{codexResponsesHTTPClientTarget, `turn{thread.id=thread-real turn.id=turn-other}: Request completed method=POST api.path="responses" status=200 OK headers={"x-client-request-id":"wrong-turn"}`},
		{codexResponsesHTTPClientTarget, `turn{thread.id=thread-real turn.id=turn-real}: Request completed method=POST api.path="responses" status=503 Service Unavailable headers={"x-client-request-id":"failed-request"}`},
		{"codex_api::sse::responses", `turn{thread.id=thread-real turn.id=turn-real}: Request completed method=POST api.path="responses" status=200 OK headers={"x-client-request-id":"wrong-target"}`},
		{codexResponsesWebSocketEventTarget, `turn{thread.id=thread-real turn.id=turn-real}:model_client.stream_responses_websocket{transport="responses_websocket" websocket.warmup=false}: unhandled responses event: response.in_progress`},
		{codexResponsesWebSocketCompletionTarget, `turn{thread.id=thread-real turn.id=turn-other}:session_task.run:run_turn: post sampling token usage turn_id=turn-other total_usage_tokens=10`},
		{codexResponsesWebSocketEventTarget, `turn{thread.id=thread-real turn.id=turn-real}:modelXclient.streamYresponsesZwebsocket{transport="responses_websocket" websocket.warmup=false}: unhandled responses event: response.in_progress`},
		{codexResponsesWebSocketCompletionTarget, `turn{thread.id=thread-real turn.id=turn-real}:sessionXtask.run:runXturn: post sampling token usage turn_id=turn-real total_usage_tokens=10`},
	}
	for index, row := range rows {
		if _, err := db.Exec(`INSERT INTO logs(id, ts, ts_nanos, target, feedback_log_body) VALUES(?, ?, 0, ?, ?)`, index+1, time.Now().Unix(), row.target, row.body); err != nil {
			t.Fatal(err)
		}
	}
	claims, err := ScanCodexV2ClaimsFromHome(context.Background(), home, V2ClaimScanOptions{RepoRoot: repo, CommitSHA: commit, RelayProviderID: 7})
	if err != nil {
		t.Fatal(err)
	}
	if len(claims) != 1 || claims[0].GapReason != "missing_request_id" || len(claims[0].Group.RequestIDs) != 0 {
		t.Fatalf("untrusted identity was accepted: %+v", claims)
	}
}

func TestLoadCodexV2RequestEvidenceRejectsAmbiguousRequestIdentity(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(home, ".codex", "logs_2.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE logs (id INTEGER PRIMARY KEY, ts INTEGER, ts_nanos INTEGER, thread_id TEXT, target TEXT, feedback_log_body TEXT)`); err != nil {
		t.Fatal(err)
	}
	for index, turn := range []string{"turn-a", "turn-b"} {
		body := `turn{thread.id=thread-real turn.id=` + turn + `}: Request completed method=POST api.path="responses" status=200 OK headers={"x-client-request-id":"same-request"}`
		if _, err := db.Exec(`INSERT INTO logs(id, ts, ts_nanos, target, feedback_log_body) VALUES(?, 1, ?, ?, ?)`, index+1, index, codexResponsesHTTPClientTarget, body); err != nil {
			t.Fatal(err)
		}
	}
	evidence, err := loadCodexV2RequestEvidence(context.Background(), home, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(evidence) != 0 {
		t.Fatalf("ambiguous evidence = %+v", evidence)
	}
}

func TestNormalizeV2RequestIDKeepsOneClientPrefix(t *testing.T) {
	for input, want := range map[string]string{"request": "client:request", " client:request ": "client:request", "client:": ""} {
		if got := normalizeV2RequestID(input); got != want {
			t.Fatalf("normalize %q = %q, want %q", input, got, want)
		}
	}
}

func TestScanCodexV2ClaimsFailsClosedOnCommitMismatchAndProviderSwitch(t *testing.T) {
	repo, commit := v2ClaimRepo(t, "feature.go", "different\n")
	path := filepath.Join(t.TempDir(), "session.jsonl")
	writeV2JSONL(t, path,
		map[string]any{"type": "session_meta", "payload": map[string]any{"id": "thread-1"}},
		map[string]any{"type": "turn_context", "payload": map[string]any{"turn_id": "turn-1"}},
		map[string]any{"type": "response_item", "payload": map[string]any{"type": "custom_tool_call", "name": "apply_patch", "input": "*** Begin Patch\n*** Add File: feature.go\n+package feature\n*** End Patch"}},
		map[string]any{"type": "event_msg", "payload": map[string]any{"type": "transport", "message": `x-client-request-id: req-1`}},
	)
	base := V2ClaimScanOptions{RepoRoot: repo, CommitSHA: commit, RelayProviderID: 7, RepoConfigID: 8, WorkspaceID: "workspace-8", CheckpointEventID: "checkpoint-9"}
	claims, err := scanV2ClaimsForTest([]string{path}, base, "thread-1", "req-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(claims) != 1 || claims[0].GapReason != "commit_content_mismatch" || len(UploadableV2ClaimGroups(claims)) != 0 {
		t.Fatalf("mismatch claims = %+v", claims)
	}
	base.RelayProviderID = 10
	switched, err := scanV2ClaimsForTest([]string{path}, base, "thread-1", "req-1")
	if err != nil {
		t.Fatal(err)
	}
	if switched[0].Group.GroupID == claims[0].Group.GroupID || claims[0].Group.RelayProviderID != 7 {
		t.Fatalf("provider switch rewrote identity: old=%+v new=%+v", claims[0], switched[0])
	}
}

func TestScanCodexV2ClaimsReplaysUpdatePatchAgainstCommitParent(t *testing.T) {
	repo := t.TempDir()
	gitClaim(t, repo, "init")
	gitClaim(t, repo, "config", "user.email", "alice@example.com")
	gitClaim(t, repo, "config", "user.name", "Alice")
	path := filepath.Join(repo, "feature.go")
	if err := os.WriteFile(path, []byte("package feature\n\nconst Value = 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitClaim(t, repo, "add", "feature.go")
	gitClaim(t, repo, "commit", "-m", "parent")
	if err := os.WriteFile(path, []byte("package feature\n\nconst Value = 2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitClaim(t, repo, "add", "feature.go")
	gitClaim(t, repo, "commit", "-m", "update")
	commit := strings.TrimSpace(gitClaim(t, repo, "rev-parse", "HEAD"))
	session := filepath.Join(t.TempDir(), "session.jsonl")
	writeV2JSONL(t, session,
		map[string]any{"type": "session_meta", "payload": map[string]any{"id": "thread-update"}},
		map[string]any{"type": "turn_context", "payload": map[string]any{"turn_id": "turn-update"}},
		map[string]any{"type": "response_item", "payload": map[string]any{"type": "custom_tool_call", "name": "apply_patch", "input": "*** Begin Patch\n*** Update File: feature.go\n@@\n package feature\n \n-const Value = 1\n+const Value = 2\n*** End Patch"}},
		map[string]any{"type": "event_msg", "payload": map[string]any{"type": "transport", "message": `x-client-request-id: req-update`}},
	)
	claims, err := scanV2ClaimsForTest([]string{session}, V2ClaimScanOptions{RepoRoot: repo, CommitSHA: commit, RelayProviderID: 7, RepoConfigID: 8, WorkspaceID: "workspace-8", CheckpointEventID: "checkpoint-update"}, "thread-update", "req-update")
	if err != nil {
		t.Fatal(err)
	}
	if len(claims) != 1 || claims[0].GapReason != "" || len(UploadableV2ClaimGroups(claims)) != 1 {
		t.Fatalf("update claims = %+v", claims)
	}
}

func TestScanCodexV2ClaimsDoesNotBindAddPatchToLaterCommit(t *testing.T) {
	repo, _ := v2ClaimRepo(t, "feature.go", "package feature\n")
	if err := os.WriteFile(filepath.Join(repo, "other.go"), []byte("package feature\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitClaim(t, repo, "add", "other.go")
	gitClaim(t, repo, "commit", "-m", "later")
	commit := strings.TrimSpace(gitClaim(t, repo, "rev-parse", "HEAD"))
	session := filepath.Join(t.TempDir(), "session.jsonl")
	writeV2JSONL(t, session,
		map[string]any{"type": "session_meta", "payload": map[string]any{"id": "thread-add"}},
		map[string]any{"type": "turn_context", "payload": map[string]any{"turn_id": "turn-add"}},
		map[string]any{"type": "response_item", "payload": map[string]any{"type": "custom_tool_call", "name": "apply_patch", "input": "*** Begin Patch\n*** Add File: feature.go\n+package feature\n*** End Patch"}},
		map[string]any{"type": "event_msg", "payload": map[string]any{"type": "transport", "message": `x-client-request-id: req-add`}},
	)
	claims, err := scanV2ClaimsForTest([]string{session}, V2ClaimScanOptions{RepoRoot: repo, CommitSHA: commit, RelayProviderID: 7, RepoConfigID: 8, WorkspaceID: "workspace-8", CheckpointEventID: "checkpoint-later"}, "thread-add", "req-add")
	if err != nil {
		t.Fatal(err)
	}
	if len(claims) != 1 || claims[0].GapReason != "commit_content_mismatch" {
		t.Fatalf("later commit claims = %+v", claims)
	}
}

func TestScanCodexV2ClaimsBuildsMultiCommitAllocationSequence(t *testing.T) {
	repo := t.TempDir()
	for _, args := range [][]string{{"init"}, {"config", "user.email", "alice@example.com"}, {"config", "user.name", "Alice"}} {
		gitClaim(t, repo, args...)
	}
	if err := os.WriteFile(filepath.Join(repo, "a.go"), []byte("package feature\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitClaim(t, repo, "add", "a.go")
	gitClaim(t, repo, "commit", "-m", "add a")
	commitA := strings.TrimSpace(gitClaim(t, repo, "rev-parse", "HEAD"))
	if err := os.WriteFile(filepath.Join(repo, "b.go"), []byte("package feature\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitClaim(t, repo, "add", "b.go")
	gitClaim(t, repo, "commit", "-m", "add b")
	commitB := strings.TrimSpace(gitClaim(t, repo, "rev-parse", "HEAD"))

	session := filepath.Join(t.TempDir(), "session.jsonl")
	writeV2JSONL(t, session,
		map[string]any{"type": "session_meta", "payload": map[string]any{"id": "thread-multi"}},
		map[string]any{"type": "turn_context", "payload": map[string]any{"turn_id": "turn-multi"}},
		map[string]any{"type": "response_item", "payload": map[string]any{"type": "custom_tool_call", "name": "apply_patch", "input": "*** Begin Patch\n*** Add File: a.go\n+package feature\n*** End Patch"}},
		map[string]any{"type": "response_item", "payload": map[string]any{"type": "custom_tool_call", "name": "apply_patch", "input": "*** Begin Patch\n*** Add File: b.go\n+package feature\n*** End Patch"}},
	)
	base := V2ClaimScanOptions{RepoRoot: repo, RelayProviderID: 7, RepoConfigID: 8, RepoKey: "example.com/org/repo", WorkspaceID: "workspace-8"}
	base.CommitSHA, base.CheckpointEventID = commitA, "checkpoint-a"
	first, err := scanV2ClaimsForTest([]string{session}, base, "thread-multi", "req-multi")
	if err != nil || len(first) != 1 || first[0].GapReason != "" {
		t.Fatalf("first allocation = %+v, err = %v", first, err)
	}
	base.CommitSHA, base.CheckpointEventID = commitB, "checkpoint-b"
	second, err := scanV2ClaimsForTest([]string{session}, base, "thread-multi", "req-multi")
	if err != nil || len(second) != 1 || second[0].GapReason != "" {
		t.Fatalf("second allocation = %+v, err = %v", second, err)
	}
	state := &V2ClaimState{}
	MergeV2ClaimState(state, first, time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC))
	MergeV2ClaimState(state, second, time.Date(2026, 8, 11, 12, 1, 0, 0, time.UTC))
	allocations := state.Claims[0].Group.CommitAllocations
	if len(allocations) != 2 || allocations[0].CommitSHA != commitA || allocations[1].CommitSHA != commitB || allocations[1].Sequence != 2 || allocations[0].EvidenceDigest == allocations[1].EvidenceDigest {
		t.Fatalf("allocation sequence = %+v", allocations)
	}
}

func TestScanCodexV2ClaimsBuildsSequentialSameFileUpdateAllocations(t *testing.T) {
	repo := t.TempDir()
	for _, args := range [][]string{{"init"}, {"config", "user.email", "alice@example.com"}, {"config", "user.name", "Alice"}} {
		gitClaim(t, repo, args...)
	}
	feature := filepath.Join(repo, "feature.go")
	for index, value := range []string{"0", "1", "2"} {
		if err := os.WriteFile(feature, []byte("package feature\n\nconst Value = "+value+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		gitClaim(t, repo, "add", "feature.go")
		gitClaim(t, repo, "commit", "-m", "value "+value)
		if index == 0 {
			continue
		}
	}
	commitB := strings.TrimSpace(gitClaim(t, repo, "rev-parse", "HEAD"))
	commitA := strings.TrimSpace(gitClaim(t, repo, "rev-parse", "HEAD^"))
	session := filepath.Join(t.TempDir(), "session.jsonl")
	baseRows := []map[string]any{
		{"type": "session_meta", "payload": map[string]any{"id": "thread-update-sequence"}},
		{"type": "turn_context", "payload": map[string]any{"turn_id": "turn-update-sequence"}},
		{"type": "response_item", "payload": map[string]any{"type": "custom_tool_call", "name": "apply_patch", "input": "*** Begin Patch\n*** Update File: feature.go\n@@\n-const Value = 0\n+const Value = 1\n*** End Patch"}},
	}
	writeV2JSONL(t, session, baseRows...)
	opts := V2ClaimScanOptions{RepoRoot: repo, RelayProviderID: 7, RepoConfigID: 8, WorkspaceID: "workspace-8", CommitSHA: commitA, CheckpointEventID: "checkpoint-a"}
	first, err := scanV2ClaimsForTest([]string{session}, opts, "thread-update-sequence", "req-update-sequence")
	if err != nil || first[0].GapReason != "" {
		t.Fatalf("first update allocation = %+v, err = %v", first, err)
	}
	writeV2JSONL(t, session, append(baseRows,
		map[string]any{"type": "response_item", "payload": map[string]any{"type": "custom_tool_call", "name": "apply_patch", "input": "*** Begin Patch\n*** Update File: feature.go\n@@\n-const Value = 1\n+const Value = 2\n*** End Patch"}},
	)...)
	opts.CommitSHA, opts.CheckpointEventID = commitB, "checkpoint-b"
	second, err := scanV2ClaimsForTest([]string{session}, opts, "thread-update-sequence", "req-update-sequence")
	if err != nil || second[0].GapReason != "" {
		t.Fatalf("second update allocation = %+v, err = %v", second, err)
	}
	state := &V2ClaimState{}
	MergeV2ClaimState(state, first, time.Now().UTC())
	MergeV2ClaimState(state, second, time.Now().UTC())
	if allocations := state.Claims[0].Group.CommitAllocations; len(allocations) != 2 || allocations[0].CommitSHA != commitA || allocations[1].CommitSHA != commitB {
		t.Fatalf("same-file allocation sequence = %+v", allocations)
	}
}

func TestScanCodexV2ClaimsReplaysOrderedUpdatesWithinOneCommit(t *testing.T) {
	repo := t.TempDir()
	for _, args := range [][]string{{"init"}, {"config", "user.email", "alice@example.com"}, {"config", "user.name", "Alice"}} {
		gitClaim(t, repo, args...)
	}
	feature := filepath.Join(repo, "feature.go")
	if err := os.WriteFile(feature, []byte("package feature\n\nconst Value = 0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitClaim(t, repo, "add", "feature.go")
	gitClaim(t, repo, "commit", "-m", "parent")
	if err := os.WriteFile(feature, []byte("package feature\n\nconst Value = 2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitClaim(t, repo, "add", "feature.go")
	gitClaim(t, repo, "commit", "-m", "two patches")
	commit := strings.TrimSpace(gitClaim(t, repo, "rev-parse", "HEAD"))
	session := filepath.Join(t.TempDir(), "session.jsonl")
	writeV2JSONL(t, session,
		map[string]any{"type": "session_meta", "payload": map[string]any{"id": "thread-ordered"}},
		map[string]any{"type": "turn_context", "payload": map[string]any{"turn_id": "turn-ordered"}},
		map[string]any{"type": "response_item", "payload": map[string]any{"type": "custom_tool_call", "name": "apply_patch", "input": "*** Begin Patch\n*** Update File: feature.go\n@@\n-const Value = 0\n+const Value = 1\n*** End Patch"}},
		map[string]any{"type": "response_item", "payload": map[string]any{"type": "custom_tool_call", "name": "apply_patch", "input": "*** Begin Patch\n*** Update File: feature.go\n@@\n-const Value = 1\n+const Value = 2\n*** End Patch"}},
	)
	claims, err := scanV2ClaimsForTest([]string{session}, V2ClaimScanOptions{
		RepoRoot: repo, CommitSHA: commit, RelayProviderID: 7, RepoConfigID: 8, WorkspaceID: "workspace-8", CheckpointEventID: "checkpoint-ordered",
	}, "thread-ordered", "req-ordered")
	if err != nil || len(claims) != 1 || claims[0].GapReason != "" || len(claims[0].Group.CommitAllocations) != 1 {
		t.Fatalf("ordered update claim = %+v, err = %v", claims, err)
	}
}

func v2ClaimRepo(t *testing.T, path, content string) (string, string) {
	t.Helper()
	repo := t.TempDir()
	for _, args := range [][]string{{"init"}, {"config", "user.email", "alice@example.com"}, {"config", "user.name", "Alice"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, output)
		}
	}
	if err := os.WriteFile(filepath.Join(repo, path), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", path}, {"commit", "-m", "test"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, output)
		}
	}
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = repo
	output, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	return repo, strings.TrimSpace(string(output))
}

func gitClaim(t *testing.T, repo string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = repo
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
	return string(output)
}

func writeV2JSONL(t *testing.T, path string, rows ...map[string]any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	var body strings.Builder
	base := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	for index, row := range rows {
		if _, ok := row["timestamp"]; !ok {
			row["timestamp"] = base.Add(time.Duration(index) * time.Second).Format(time.RFC3339Nano)
		}
		encoded, err := json.Marshal(row)
		if err != nil {
			t.Fatal(err)
		}
		body.Write(encoded)
		body.WriteByte('\n')
	}
	if err := os.WriteFile(path, []byte(body.String()), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeV2RequestLog(t *testing.T, home string, turns map[string]string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o700); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(home, ".codex", "logs_2.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`CREATE TABLE logs (id INTEGER PRIMARY KEY, ts INTEGER, ts_nanos INTEGER, thread_id TEXT, target TEXT, feedback_log_body TEXT)`); err != nil {
		t.Fatal(err)
	}
	index := 0
	for identity, turnID := range turns {
		index++
		parts := strings.SplitN(identity, "/", 2)
		threadID := parts[0]
		requestID := "request-" + strings.TrimPrefix(threadID, "thread-")
		if len(parts) == 2 {
			requestID = parts[1]
		}
		body := `turn{thread.id=` + threadID + ` turn.id=` + turnID + `}: Request completed method=POST api.path="responses" status=200 OK headers={"x-client-request-id":"` + requestID + `"}`
		if _, err := db.Exec(`INSERT INTO logs(id, ts, ts_nanos, thread_id, target, feedback_log_body) VALUES(?, ?, 0, ?, ?, ?)`, index, time.Now().UTC().Unix(), threadID, codexResponsesHTTPClientTarget, body); err != nil {
			t.Fatal(err)
		}
	}
}

func scanV2ClaimsForTest(paths []string, opts V2ClaimScanOptions, threadID string, requestIDs ...string) ([]V2ClaimCandidate, error) {
	evidence := make([]v2RequestEvidence, 0, len(requestIDs))
	for _, requestID := range requestIDs {
		evidence = append(evidence, v2RequestEvidence{
			threadID: threadID, turnID: strings.Replace(threadID, "thread-", "turn-", 1), requestID: requestID,
		})
	}
	return scanCodexV2ClaimsWithEvidence(context.Background(), paths, opts, evidence)
}
