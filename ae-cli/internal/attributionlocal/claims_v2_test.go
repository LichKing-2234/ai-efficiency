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

func TestScanCodexV2ClaimsUsesTransportLogNotArbitraryJSONLText(t *testing.T) {
	repo, commit := v2ClaimRepo(t, "feature.go", "package feature\n")
	home := t.TempDir()
	session := filepath.Join(home, ".codex", "sessions", "session.jsonl")
	writeV2JSONL(t, session,
		map[string]any{"type": "session_meta", "payload": map[string]any{"id": "thread-real"}},
		map[string]any{"type": "turn_context", "payload": map[string]any{"turn_id": "turn-real"}},
		map[string]any{"type": "response_item", "payload": map[string]any{"type": "function_call_output", "output": `untrusted x-client-request-id: fake-request`}},
		map[string]any{"type": "response_item", "payload": map[string]any{"type": "custom_tool_call", "name": "apply_patch", "input": "*** Begin Patch\n*** Add File: feature.go\n+package feature\n*** End Patch"}},
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
	body := `model_client.stream_responses_api{api.path="responses"}: Request completed method=POST headers={"x-client-request-id": "client:real-request"}`
	if _, err := db.Exec(`INSERT INTO logs(id, ts, ts_nanos, thread_id, target, feedback_log_body) VALUES(1, ?, ?, ?, ?, ?)`, observed.Unix(), observed.Nanosecond(), "thread-real", codexFailedRequestTarget, body); err != nil {
		t.Fatal(err)
	}
	claims, err := ScanCodexV2ClaimsFromHome(context.Background(), home, V2ClaimScanOptions{RepoRoot: repo, CommitSHA: commit, RelayProviderID: 7, RepoConfigID: 8, WorkspaceID: "workspace-8", CheckpointEventID: "checkpoint-real"})
	if err != nil {
		t.Fatal(err)
	}
	if len(claims) != 1 || strings.Join(claims[0].Group.RequestIDs, ",") != "real-request" || claims[0].GapReason != "" || claims[0].Group.Calibration == nil || claims[0].Group.Calibration.TotalTokens != 12 {
		t.Fatalf("transport-correlated claims = %+v", claims)
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

func scanV2ClaimsForTest(paths []string, opts V2ClaimScanOptions, threadID string, requestIDs ...string) ([]V2ClaimCandidate, error) {
	base := time.Date(2026, 8, 11, 12, 0, 2, 0, time.UTC)
	evidence := make([]v2RequestEvidence, 0, len(requestIDs))
	for index, requestID := range requestIDs {
		evidence = append(evidence, v2RequestEvidence{threadID: threadID, requestID: requestID, observedAt: base.Add(time.Duration(index) * time.Millisecond)})
	}
	return scanCodexV2ClaimsWithEvidence(context.Background(), paths, opts, evidence)
}
