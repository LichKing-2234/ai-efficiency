package attributionlocal

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ai-efficiency/ae-cli/internal/client"
)

const v2ClaimSchemaVersion = 2

type V2ClaimScanOptions struct {
	RepoRoot          string
	CommitSHA         string
	RelayProviderID   int
	RepoConfigID      int
	RepoKey           string
	WorkspaceID       string
	CheckpointEventID string
}

// V2ClaimCandidate retains source/mutation detail locally. Group is the only
// value eligible for upload and contains digests rather than paths or content.
type V2ClaimCandidate struct {
	LocalKey    string                         `json:"local_key"`
	Group       client.AttributionV2ClaimGroup `json:"group"`
	Source      string                         `json:"source,omitempty"`
	GapReason   string                         `json:"gap_reason,omitempty"`
	FirstSeenAt time.Time                      `json:"first_seen_at"`
	UpdatedAt   time.Time                      `json:"updated_at"`
}

type V2ClaimState struct {
	Version int                `json:"version"`
	Claims  []V2ClaimCandidate `json:"claims"`
}

type V2ClaimBackendClient interface {
	SendAttributionV2Claims(context.Context, []client.AttributionV2ClaimGroup) (*client.AttributionV2ClaimBatchResult, error)
}

func ScanCodexV2ClaimsFromHome(ctx context.Context, homeDir string, opts V2ClaimScanOptions) ([]V2ClaimCandidate, error) {
	if strings.TrimSpace(homeDir) == "" {
		var err error
		homeDir, err = os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("resolve home: %w", err)
		}
	}
	evidence, err := loadCodexV2RequestEvidence(ctx, homeDir)
	if err != nil {
		return nil, err
	}
	return scanCodexV2ClaimsWithEvidence(ctx, findCodexJSONLFiles("", homeDir), opts, evidence)
}

func V2ClaimStatePath() string {
	return filepath.Join(AttributionRootDir(), "claims-v2", "state.json")
}

func LoadV2ClaimState() (*V2ClaimState, error) {
	var state V2ClaimState
	if err := LoadJSON(V2ClaimStatePath(), &state); err != nil {
		if os.IsNotExist(err) {
			return &V2ClaimState{Version: 1, Claims: []V2ClaimCandidate{}}, nil
		}
		return nil, err
	}
	if state.Version == 0 {
		state.Version = 1
	}
	return &state, nil
}

func SaveV2ClaimState(state *V2ClaimState) error {
	if state == nil {
		return fmt.Errorf("v2 claim state is nil")
	}
	state.Version = 1
	return SaveJSON(V2ClaimStatePath(), state)
}

// MergeV2ClaimState freezes the first provider/group selected for a turn.
// Later scans may append Requests or improve evidence, but never re-home it.
func MergeV2ClaimState(state *V2ClaimState, scanned []V2ClaimCandidate, now time.Time) {
	if state == nil {
		return
	}
	cutoff := now.UTC().Add(-90 * 24 * time.Hour)
	byKey := map[string]int{}
	kept := make([]V2ClaimCandidate, 0, len(state.Claims)+len(scanned))
	for _, existing := range state.Claims {
		if !existing.FirstSeenAt.IsZero() && existing.FirstSeenAt.Before(cutoff) {
			continue
		}
		kept = append(kept, existing)
		byKey[existing.LocalKey] = len(kept) - 1
	}
	for _, candidate := range scanned {
		if candidate.FirstSeenAt.IsZero() {
			candidate.FirstSeenAt = now.UTC()
		}
		if candidate.FirstSeenAt.Before(cutoff) {
			continue
		}
		candidate.UpdatedAt = now.UTC()
		index, found := byKey[candidate.LocalKey]
		if !found {
			kept = append(kept, candidate)
			byKey[candidate.LocalKey] = len(kept) - 1
			continue
		}
		existing := &kept[index]
		existing.Group.RequestIDs = uniqueSorted(append(existing.Group.RequestIDs, candidate.Group.RequestIDs...))
		existing.Group.CommitAllocations = mergeV2Allocations(existing.Group.CommitAllocations, candidate.Group.CommitAllocations)
		existing.Group.EvidenceDigest = v2AllocationEvidenceDigest(existing.Group.CommitAllocations)
		if existing.Group.Calibration == nil && candidate.Group.Calibration != nil {
			existing.Group.Calibration = candidate.Group.Calibration
		}
		existing.UpdatedAt = candidate.UpdatedAt
		if existing.GapReason != "" && candidate.GapReason == "" {
			existing.GapReason = ""
			existing.Group.EvidenceDigest = candidate.Group.EvidenceDigest
			existing.Group.Calibration = candidate.Group.Calibration
		}
	}
	state.Claims = kept
}

type v2Mutation struct {
	path string
	hash string
	kind string
}

type v2Turn struct {
	threadID    string
	turnID      string
	requests    map[string]struct{}
	mutations   []v2Mutation
	calibration client.AttributionV2Calibration
	startedAt   time.Time
}

type v2RequestEvidence struct {
	threadID   string
	requestID  string
	observedAt time.Time
}

func ScanCodexV2Claims(ctx context.Context, paths []string, opts V2ClaimScanOptions) ([]V2ClaimCandidate, error) {
	return scanCodexV2ClaimsWithEvidence(ctx, paths, opts, nil)
}

func scanCodexV2ClaimsWithEvidence(ctx context.Context, paths []string, opts V2ClaimScanOptions, requestEvidence []v2RequestEvidence) ([]V2ClaimCandidate, error) {
	merged := map[string]*V2ClaimCandidate{}
	for _, path := range paths {
		candidates, err := parseCodexV2ClaimFile(ctx, path, opts, requestEvidence)
		if err != nil {
			return nil, fmt.Errorf("scan Codex v2 source: %w", err)
		}
		for _, candidate := range candidates {
			existing := merged[candidate.Group.GroupID]
			if existing == nil {
				copy := candidate
				merged[candidate.Group.GroupID] = &copy
				continue
			}
			existing.Group.RequestIDs = uniqueSorted(append(existing.Group.RequestIDs, candidate.Group.RequestIDs...))
			if existing.GapReason != "" && candidate.GapReason == "" {
				requests := existing.Group.RequestIDs
				existing.GapReason = ""
				existing.Group = candidate.Group
				existing.Group.RequestIDs = requests
			}
		}
	}
	result := make([]V2ClaimCandidate, 0, len(merged))
	for _, candidate := range merged {
		result = append(result, *candidate)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Group.GroupID < result[j].Group.GroupID })
	return result, nil
}

func UploadableV2ClaimGroups(candidates []V2ClaimCandidate) []client.AttributionV2ClaimGroup {
	groups := make([]client.AttributionV2ClaimGroup, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.GapReason == "" && len(candidate.Group.RequestIDs) > 0 {
			groups = append(groups, candidate.Group)
		}
	}
	return groups
}

func parseCodexV2ClaimFile(ctx context.Context, path string, opts V2ClaimScanOptions, requestEvidence []v2RequestEvidence) ([]V2ClaimCandidate, error) {
	var sessionID, threadID string
	var sessionEnd time.Time
	turns := map[string]*v2Turn{}
	var current *v2Turn
	err := forEachCodexJSONLLine(ctx, path, func(_ int, raw []byte) error {
		var row struct {
			Type      string `json:"type"`
			Timestamp string `json:"timestamp"`
			Payload   struct {
				ID        string `json:"id"`
				ThreadID  string `json:"thread_id"`
				TurnID    string `json:"turn_id"`
				Type      string `json:"type"`
				Name      string `json:"name"`
				Input     string `json:"input"`
				Arguments string `json:"arguments"`
				Info      any    `json:"info"`
				Changes   map[string]struct {
					Type          string `json:"type"`
					SHA256        string `json:"sha256"`
					ContentSHA256 string `json:"content_sha256"`
				} `json:"changes"`
			} `json:"payload"`
		}
		if err := json.Unmarshal(raw, &row); err != nil {
			return nil
		}
		observedAt := parseObservedAt(row.Timestamp)
		if observedAt.After(sessionEnd) {
			sessionEnd = observedAt
		}
		switch strings.TrimSpace(row.Type) {
		case "session_meta":
			sessionID = strings.TrimSpace(row.Payload.ID)
			threadID = firstNonEmptyCompact(strings.TrimSpace(row.Payload.ThreadID), sessionID)
		case "turn_context":
			turnID := strings.TrimSpace(row.Payload.TurnID)
			if turnID == "" {
				current = nil
				return nil
			}
			current = turns[turnID]
			if current == nil {
				current = &v2Turn{threadID: firstNonEmptyCompact(strings.TrimSpace(row.Payload.ThreadID), threadID, sessionID), turnID: turnID, requests: map[string]struct{}{}, startedAt: observedAt}
				turns[turnID] = current
			}
		case "response_item":
			if current != nil && compactIsPatchTool(row.Payload.Name) {
				current.mutations = append(current.mutations, v2PatchMutations(ctx, row.Payload.Input+"\n"+row.Payload.Arguments, opts.RepoRoot, opts.CommitSHA)...)
			}
		case "event_msg":
			if current != nil && strings.TrimSpace(row.Payload.Type) == "patch_apply_end" {
				for path, change := range row.Payload.Changes {
					hash := firstNonEmptyCompact(strings.TrimSpace(change.ContentSHA256), strings.TrimSpace(change.SHA256))
					if hash != "" {
						current.mutations = append(current.mutations, v2Mutation{path: canonicalClaimPath(opts.RepoRoot, path), hash: strings.ToLower(hash), kind: strings.TrimSpace(change.Type)})
					}
				}
			}
			if current != nil && strings.TrimSpace(row.Payload.Type) == "token_count" && row.Payload.Info != nil {
				addV2Calibration(&current.calibration, row.Payload.Info)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	orderedTurns := make([]*v2Turn, 0, len(turns))
	for _, turn := range turns {
		orderedTurns = append(orderedTurns, turn)
	}
	sort.Slice(orderedTurns, func(i, j int) bool { return orderedTurns[i].startedAt.Before(orderedTurns[j].startedAt) })
	for index, turn := range orderedTurns {
		end := sessionEnd
		if index+1 < len(orderedTurns) {
			end = orderedTurns[index+1].startedAt
		}
		for _, evidence := range requestEvidence {
			if evidence.threadID == turn.threadID && !evidence.observedAt.Before(turn.startedAt) && (end.IsZero() || evidence.observedAt.Before(end)) {
				turn.requests[normalizeV2RequestID(evidence.requestID)] = struct{}{}
			}
		}
	}
	result := make([]V2ClaimCandidate, 0, len(orderedTurns))
	for _, turn := range orderedTurns {
		requests := make([]string, 0, len(turn.requests))
		for requestID := range turn.requests {
			requests = append(requests, requestID)
		}
		requests = uniqueSorted(requests)
		groupID := claimDigest(fmt.Sprintf("%d", opts.RelayProviderID), sessionID, turn.turnID)
		candidate := V2ClaimCandidate{LocalKey: claimDigest(sessionID, turn.turnID), Source: path, FirstSeenAt: turn.startedAt, Group: client.AttributionV2ClaimGroup{
			SchemaVersion: v2ClaimSchemaVersion, GroupID: groupID, RelayProviderID: opts.RelayProviderID,
			ThreadID: turn.threadID, TurnID: turn.turnID, RequestIDs: requests,
		}}
		if len(requests) == 0 {
			candidate.GapReason = "missing_request_id"
		} else if len(turn.mutations) == 0 {
			candidate.GapReason = "missing_structured_mutation"
		} else if !verifyV2Mutations(ctx, opts.RepoRoot, opts.CommitSHA, turn.mutations) {
			candidate.GapReason = "commit_content_mismatch"
		} else {
			evidenceDigest := v2MutationDigest(turn.mutations)
			candidate.Group.EvidenceDigest = evidenceDigest
			candidate.Group.CommitAllocations = []client.AttributionV2CommitAllocation{{
				Sequence: 1, RepoConfigID: opts.RepoConfigID, RepoKey: strings.TrimSpace(opts.RepoKey), WorkspaceID: strings.TrimSpace(opts.WorkspaceID),
				CheckpointEventID: opts.CheckpointEventID, CommitSHA: opts.CommitSHA, EvidenceDigest: evidenceDigest,
			}}
			if turn.calibration.TotalTokens > 0 {
				turn.calibration.Digest = v2CalibrationDigest(turn.calibration)
				calibration := turn.calibration
				candidate.Group.Calibration = &calibration
			}
		}
		result = append(result, candidate)
	}
	return result, nil
}

func v2PatchMutations(ctx context.Context, patch, repoRoot, commitSHA string) []v2Mutation {
	lines := strings.Split(strings.ReplaceAll(patch, "\r\n", "\n"), "\n")
	var mutations []v2Mutation
	for i := 0; i < len(lines); i++ {
		header := lines[i]
		kind := ""
		prefix := ""
		for candidate, value := range map[string]string{"*** Add File: ": "add", "*** Update File: ": "update", "*** Delete File: ": "delete"} {
			if strings.HasPrefix(header, candidate) {
				prefix, kind = candidate, value
				break
			}
		}
		if kind == "" {
			continue
		}
		path := canonicalClaimPath(repoRoot, strings.TrimSpace(strings.TrimPrefix(header, prefix)))
		var block []string
		for i++; i < len(lines) && !strings.HasPrefix(lines[i], "*** "); i++ {
			block = append(block, lines[i])
		}
		i--
		if path == "" {
			mutations = append(mutations, v2Mutation{kind: kind})
			continue
		}
		switch kind {
		case "add":
			content := make([]string, 0, len(block))
			for _, line := range block {
				if strings.HasPrefix(line, "+") {
					content = append(content, strings.TrimPrefix(line, "+"))
				}
			}
			mutations = append(mutations, v2Mutation{path: path, hash: claimDigest(strings.Join(content, "\n") + "\n"), kind: kind})
		case "delete":
			if _, err := gitShowClaimFile(ctx, repoRoot, strings.TrimSpace(commitSHA)+"^", path); err == nil {
				mutations = append(mutations, v2Mutation{path: path, hash: claimDigest("deleted"), kind: kind})
			}
		case "update":
			parent, err := gitShowClaimFile(ctx, repoRoot, strings.TrimSpace(commitSHA)+"^", path)
			if err != nil {
				mutations = append(mutations, v2Mutation{path: path, kind: kind})
				continue
			}
			expected, ok := applyV2PatchBlock(string(parent), block)
			if ok {
				mutations = append(mutations, v2Mutation{path: path, hash: claimDigest(expected), kind: kind})
			} else {
				mutations = append(mutations, v2Mutation{path: path, kind: kind})
			}
		}
	}
	return mutations
}

func verifyV2Mutations(ctx context.Context, repoRoot, commitSHA string, mutations []v2Mutation) bool {
	if strings.TrimSpace(repoRoot) == "" || strings.TrimSpace(commitSHA) == "" {
		return false
	}
	for _, mutation := range mutations {
		if mutation.path == "" || mutation.hash == "" {
			return false
		}
		if mutation.kind == "delete" {
			if _, err := gitShowClaimFile(ctx, repoRoot, strings.TrimSpace(commitSHA), mutation.path); err == nil {
				return false
			}
			continue
		}
		if mutation.kind == "add" {
			if _, err := gitShowClaimFile(ctx, repoRoot, strings.TrimSpace(commitSHA)+"^", mutation.path); err == nil {
				return false
			}
		}
		content, err := gitShowClaimFile(ctx, repoRoot, strings.TrimSpace(commitSHA), mutation.path)
		if err != nil || claimDigest(string(content)) != strings.ToLower(mutation.hash) {
			return false
		}
	}
	return true
}

func canonicalClaimPath(repoRoot, path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if filepath.IsAbs(path) {
		relative, err := filepath.Rel(repoRoot, path)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return ""
		}
		path = relative
	}
	path = filepath.Clean(path)
	if path == "." || path == ".." || strings.HasPrefix(path, ".."+string(filepath.Separator)) {
		return ""
	}
	return filepath.ToSlash(path)
}

func v2MutationDigest(mutations []v2Mutation) string {
	items := make([]string, 0, len(mutations))
	for _, mutation := range mutations {
		items = append(items, mutation.kind+"\x00"+mutation.path+"\x00"+mutation.hash)
	}
	sort.Strings(items)
	return claimDigest(items...)
}

func gitShowClaimFile(ctx context.Context, repoRoot, revision, path string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", "show", revision+":"+path)
	cmd.Dir = repoRoot
	return cmd.Output()
}

func applyV2PatchBlock(parent string, block []string) (string, bool) {
	trailingNewline := strings.HasSuffix(parent, "\n")
	current := strings.Split(strings.TrimSuffix(parent, "\n"), "\n")
	var hunks [][]string
	for _, line := range block {
		if strings.HasPrefix(line, "@@") {
			hunks = append(hunks, nil)
			continue
		}
		if len(hunks) == 0 {
			hunks = append(hunks, nil)
		}
		hunks[len(hunks)-1] = append(hunks[len(hunks)-1], line)
	}
	for _, hunk := range hunks {
		var oldLines, newLines []string
		for _, line := range hunk {
			switch {
			case strings.HasPrefix(line, "+"):
				newLines = append(newLines, line[1:])
			case strings.HasPrefix(line, "-"):
				oldLines = append(oldLines, line[1:])
			default:
				contextLine := strings.TrimPrefix(line, " ")
				oldLines = append(oldLines, contextLine)
				newLines = append(newLines, contextLine)
			}
		}
		if len(oldLines) == 0 {
			return "", false
		}
		match := -1
		for start := 0; start+len(oldLines) <= len(current); start++ {
			if equalClaimLines(current[start:start+len(oldLines)], oldLines) {
				if match != -1 {
					return "", false
				}
				match = start
			}
		}
		if match == -1 {
			return "", false
		}
		next := make([]string, 0, len(current)-len(oldLines)+len(newLines))
		next = append(next, current[:match]...)
		next = append(next, newLines...)
		next = append(next, current[match+len(oldLines):]...)
		current = next
	}
	result := strings.Join(current, "\n")
	if trailingNewline {
		result += "\n"
	}
	return result, true
}

func equalClaimLines(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func loadCodexV2RequestEvidence(ctx context.Context, homeDir string) ([]v2RequestEvidence, error) {
	paths := findCodexSQLiteFiles(homeDir)
	if len(paths) == 0 {
		return nil, nil
	}
	db, err := sql.Open("sqlite", codexSQLiteReadOnlyDSN(paths[0]))
	if err != nil {
		return nil, fmt.Errorf("open Codex request log: %w", err)
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, `
		SELECT ts, ts_nanos, thread_id, feedback_log_body
		FROM logs
		WHERE target = ?
		  AND feedback_log_body LIKE '%Request completed method=POST%'
		  AND feedback_log_body LIKE '%api.path="responses"%'
		  AND feedback_log_body LIKE '%"x-client-request-id"%'
		ORDER BY ts, ts_nanos, id`, codexFailedRequestTarget)
	if err != nil {
		return nil, fmt.Errorf("query Codex request log: %w", err)
	}
	defer rows.Close()
	var result []v2RequestEvidence
	for rows.Next() {
		var ts, nanos int64
		var thread sql.NullString
		var body string
		if err := rows.Scan(&ts, &nanos, &thread, &body); err != nil {
			return nil, fmt.Errorf("scan Codex request log: %w", err)
		}
		requestID := normalizeV2RequestID(firstSubmatch(reFailHdrClientReqID, body))
		threadID := strings.TrimSpace(thread.String)
		if requestID != "" && threadID != "" {
			result = append(result, v2RequestEvidence{threadID: threadID, requestID: requestID, observedAt: time.Unix(ts, nanos).UTC()})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate Codex request log: %w", err)
	}
	return result, nil
}

func addV2Calibration(calibration *client.AttributionV2Calibration, raw any) {
	info, _ := raw.(map[string]any)
	selected, _ := info["last_token_usage"].(map[string]any)
	if len(selected) == 0 {
		selected, _ = info["total_token_usage"].(map[string]any)
	}
	if len(selected) == 0 {
		return
	}
	calibration.InputTokens += asInt64(selected["input_tokens"])
	calibration.OutputTokens += asInt64(selected["output_tokens"])
	calibration.CacheReadTokens += asInt64(selected["cached_input_tokens"])
	calibration.CacheCreationTokens += asInt64(selected["cache_write_input_tokens"])
	total := asInt64(selected["total_tokens"])
	if total == 0 {
		total = asInt64(selected["input_tokens"]) + asInt64(selected["output_tokens"])
	}
	calibration.TotalTokens += total
}

func v2CalibrationDigest(calibration client.AttributionV2Calibration) string {
	return claimDigest(
		fmt.Sprintf("%d", calibration.InputTokens), fmt.Sprintf("%d", calibration.OutputTokens),
		fmt.Sprintf("%d", calibration.CacheCreationTokens), fmt.Sprintf("%d", calibration.CacheReadTokens),
		fmt.Sprintf("%d", calibration.TotalTokens),
	)
}

func mergeV2Allocations(existing, incoming []client.AttributionV2CommitAllocation) []client.AttributionV2CommitAllocation {
	result := append([]client.AttributionV2CommitAllocation(nil), existing...)
	seen := map[string]struct{}{}
	for _, allocation := range result {
		seen[allocation.CheckpointEventID] = struct{}{}
	}
	for _, allocation := range incoming {
		if _, ok := seen[allocation.CheckpointEventID]; ok {
			continue
		}
		allocation.Sequence = len(result) + 1
		result = append(result, allocation)
		seen[allocation.CheckpointEventID] = struct{}{}
	}
	return result
}

func v2AllocationEvidenceDigest(allocations []client.AttributionV2CommitAllocation) string {
	parts := make([]string, 0, len(allocations))
	for _, allocation := range allocations {
		parts = append(parts, fmt.Sprintf("%d", allocation.Sequence)+"\x00"+allocation.EvidenceDigest)
	}
	return claimDigest(parts...)
}

func normalizeV2RequestID(value string) string {
	value = strings.TrimSpace(value)
	return strings.TrimSpace(strings.TrimPrefix(value, "client:"))
}

func uniqueSorted(values []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func claimDigest(values ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(values, "\x00")))
	return hex.EncodeToString(sum[:])
}
