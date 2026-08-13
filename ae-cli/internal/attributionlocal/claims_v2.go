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
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ai-efficiency/ae-cli/internal/client"
)

const v2ClaimSchemaVersion = 2
const codexResponsesHTTPClientTarget = "codex_http_client::client"
const v2LocalEvidenceWindow = 90 * 24 * time.Hour

var codexV2SourceReadObserver = func(string) {}

var (
	v2ThreadIDPattern          = regexp.MustCompile(`thread\.id=([^ }]+)`)
	v2TurnIDPattern            = regexp.MustCompile(`turn\.id=([^ }]+)`)
	v2SuccessfulResponseStatus = regexp.MustCompile(` status=2[0-9]{2} `)
	v2WrappedPatchPattern      = regexp.MustCompile(`(?s)^\s*const\s+patch\s*=\s*("(?:\\.|[^"\\])*")\s*;\s*text\s*\(\s*await\s+tools\.apply_patch\s*\(\s*patch\s*\)\s*\)\s*;\s*$`)
)

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
	LocalKey                      string                         `json:"local_key"`
	Group                         client.AttributionV2ClaimGroup `json:"group"`
	Source                        string                         `json:"source,omitempty"`
	GapReason                     string                         `json:"gap_reason,omitempty"`
	DeliveryStatus                string                         `json:"delivery_status,omitempty"`
	LastDeliveryError             string                         `json:"last_delivery_error,omitempty"`
	GroupAcknowledged             bool                           `json:"group_acknowledged,omitempty"`
	AcknowledgedRequestDigests    []string                       `json:"acknowledged_request_digests,omitempty"`
	AcknowledgedCalibrationDigest string                         `json:"acknowledged_calibration_digest,omitempty"`
	FirstSeenAt                   time.Time                      `json:"first_seen_at"`
	UpdatedAt                     time.Time                      `json:"updated_at"`
}

type V2ClaimState struct {
	Version int                `json:"version"`
	Claims  []V2ClaimCandidate `json:"claims"`
}

type V2ClaimBackendClient interface {
	SendAttributionV2Claims(context.Context, []client.AttributionV2ClaimGroup) (*client.AttributionV2ClaimBatchResult, error)
}

type codexV2ClaimSource struct {
	key  string
	path string
}

// CodexV2ClaimScan holds one bounded source discovery and Request-evidence
// query that can be reused across every commit trigger in a runner pass.
type CodexV2ClaimScan struct {
	sources  []codexV2ClaimSource
	evidence []v2RequestEvidence
}

// SourceEvidenceKey changes only when trusted Request evidence for one
// source's digest-only turn keys changes.
func (s *CodexV2ClaimScan) SourceEvidenceKey(turnKeys []string) string {
	if s == nil {
		return ""
	}
	wanted := make(map[string]struct{}, len(turnKeys))
	for _, key := range turnKeys {
		wanted[key] = struct{}{}
	}
	values := make([]string, 0, len(s.evidence))
	for _, evidence := range s.evidence {
		if _, ok := wanted[claimDigest(evidence.threadID, evidence.turnID)]; ok {
			values = append(values, claimDigest(evidence.requestID, strings.Join(evidence.transportIDs, "\x00")))
		}
	}
	sort.Strings(values)
	return claimDigest(values...)
}

// V2ClaimTurnKeys returns privacy-safe identities for the turns observed while
// scanning one source. Raw thread and turn identifiers are not persisted.
func V2ClaimTurnKeys(candidates []V2ClaimCandidate) []string {
	keys := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		keys = append(keys, claimDigest(candidate.Group.ThreadID, candidate.Group.TurnID))
	}
	return uniqueSorted(keys)
}

func MergeV2ClaimTurnKeys(existing, scanned []string) []string {
	return uniqueSorted(append(append([]string(nil), existing...), scanned...))
}

func PrepareCodexV2ClaimScan(ctx context.Context, homeDir string, cutoff time.Time) (*CodexV2ClaimScan, error) {
	if strings.TrimSpace(homeDir) == "" {
		var err error
		homeDir, err = os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("resolve home: %w", err)
		}
	}
	if cutoff.IsZero() {
		cutoff = time.Now().UTC().Add(-v2LocalEvidenceWindow)
	}
	evidence, err := loadCodexV2RequestEvidence(ctx, homeDir, cutoff)
	if err != nil {
		return nil, fmt.Errorf("load Codex v2 request evidence: %w", err)
	}
	paths := findCodexV2JSONLFiles(homeDir, cutoff)
	sources := make([]codexV2ClaimSource, 0, len(paths))
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		sources = append(sources, codexV2ClaimSource{
			key:  claimDigest(filepath.Clean(path), fmt.Sprintf("%d", info.ModTime().UnixNano()), fmt.Sprintf("%d", info.Size())),
			path: path,
		})
	}
	return &CodexV2ClaimScan{sources: sources, evidence: evidence}, nil
}

func (s *CodexV2ClaimScan) SourceKeys() []string {
	if s == nil {
		return nil
	}
	keys := make([]string, 0, len(s.sources))
	for _, source := range s.sources {
		keys = append(keys, source.key)
	}
	return keys
}

func (s *CodexV2ClaimScan) ScanSource(ctx context.Context, sourceKey string, options []V2ClaimScanOptions) ([]V2ClaimCandidate, error) {
	if s == nil || len(options) == 0 {
		return nil, nil
	}
	for _, source := range s.sources {
		if source.key != sourceKey {
			continue
		}
		codexV2SourceReadObserver(source.key)
		candidates, err := parseCodexV2ClaimFileBatch(ctx, source.path, options, s.evidence)
		if err != nil {
			return nil, fmt.Errorf("scan Codex v2 source: %w", err)
		}
		return candidates, nil
	}
	return nil, nil
}

func ScanCodexV2ClaimsFromHome(ctx context.Context, homeDir string, opts V2ClaimScanOptions) ([]V2ClaimCandidate, error) {
	candidates, err := ScanCodexV2ClaimsFromHomeBatch(ctx, homeDir, []V2ClaimScanOptions{opts})
	if err != nil {
		return nil, fmt.Errorf("scan Codex v2 claims from home: %w", err)
	}
	return mergeV2ScannedCandidates(candidates), nil
}

func ScanCodexV2ClaimsFromHomeBatch(ctx context.Context, homeDir string, options []V2ClaimScanOptions) ([]V2ClaimCandidate, error) {
	scan, err := PrepareCodexV2ClaimScan(ctx, homeDir, time.Time{})
	if err != nil {
		return nil, fmt.Errorf("prepare Codex v2 claim scan: %w", err)
	}
	var candidates []V2ClaimCandidate
	for _, sourceKey := range scan.SourceKeys() {
		scanned, err := scan.ScanSource(ctx, sourceKey, options)
		if err != nil {
			return nil, fmt.Errorf("scan Codex v2 claim source: %w", err)
		}
		candidates = append(candidates, scanned...)
	}
	return candidates, nil
}

func findCodexV2JSONLFiles(homeDir string, cutoff time.Time) []string {
	paths := findCodexJSONLFiles("", homeDir)
	kept := paths[:0]
	for _, path := range paths {
		info, err := os.Stat(path)
		if err == nil && !info.ModTime().Before(cutoff) {
			kept = append(kept, path)
		}
	}
	return kept
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
		candidate.Group.RequestIDs = filterAcknowledgedV2Requests(candidate.Group.RequestIDs, existing.AcknowledgedRequestDigests)
		newRequestCount := len(candidate.Group.RequestIDs)
		existing.Group.RequestIDs = uniqueSorted(append(existing.Group.RequestIDs, candidate.Group.RequestIDs...))
		allocationCount := len(existing.Group.CommitAllocations)
		existing.Group.CommitAllocations = mergeV2Allocations(existing.Group.CommitAllocations, candidate.Group.CommitAllocations)
		allocationChanged := len(existing.Group.CommitAllocations) > allocationCount
		existing.Group.EvidenceDigest = v2AllocationEvidenceDigest(existing.Group.CommitAllocations)
		calibrationChanged := false
		if existing.Group.Calibration == nil && candidate.Group.Calibration != nil && candidate.Group.Calibration.Digest != existing.AcknowledgedCalibrationDigest {
			existing.Group.Calibration = candidate.Group.Calibration
			calibrationChanged = true
		}
		existing.UpdatedAt = candidate.UpdatedAt
		if allocationChanged {
			existing.GroupAcknowledged = false
		}
		if newRequestCount > 0 || calibrationChanged || allocationChanged {
			existing.DeliveryStatus = V2DeliveryPending
			existing.LastDeliveryError = ""
		}
		if existing.GapReason != "" && candidate.GapReason == "" {
			existing.GapReason = ""
			existing.Group.EvidenceDigest = candidate.Group.EvidenceDigest
			existing.Group.Calibration = candidate.Group.Calibration
		}
	}
	state.Claims = kept
}

func filterAcknowledgedV2Requests(requestIDs, acknowledgedDigests []string) []string {
	acknowledged := make(map[string]struct{}, len(acknowledgedDigests))
	for _, digest := range acknowledgedDigests {
		acknowledged[digest] = struct{}{}
	}
	result := requestIDs[:0]
	for _, requestID := range requestIDs {
		if _, ok := acknowledged[claimDigest(requestID)]; !ok {
			result = append(result, requestID)
		}
	}
	return result
}

type v2Mutation struct {
	path string
	hash string
	kind string
}

type v2Turn struct {
	threadID     string
	turnID       string
	requests     map[string]struct{}
	transportIDs map[string]struct{}
	mutations    []v2Mutation
	replayFiles  map[string]v2ReplayFile
	calibration  client.AttributionV2Calibration
	startedAt    time.Time
}

type v2ReplayFile struct {
	content string
	exists  bool
}

type v2RequestEvidence struct {
	threadID     string
	turnID       string
	requestID    string
	transportIDs []string
}

func ScanCodexV2Claims(ctx context.Context, paths []string, opts V2ClaimScanOptions) ([]V2ClaimCandidate, error) {
	return scanCodexV2ClaimsWithEvidence(ctx, paths, opts, nil)
}

func scanCodexV2ClaimsWithEvidence(ctx context.Context, paths []string, opts V2ClaimScanOptions, requestEvidence []v2RequestEvidence) ([]V2ClaimCandidate, error) {
	var scanned []V2ClaimCandidate
	for _, path := range paths {
		candidates, err := parseCodexV2ClaimFile(ctx, path, opts, requestEvidence)
		if err != nil {
			return nil, fmt.Errorf("scan Codex v2 source: %w", err)
		}
		scanned = append(scanned, candidates...)
	}
	return mergeV2ScannedCandidates(scanned), nil
}

func mergeV2ScannedCandidates(scanned []V2ClaimCandidate) []V2ClaimCandidate {
	merged := map[string]*V2ClaimCandidate{}
	for _, candidate := range scanned {
		existing := merged[candidate.Group.GroupID]
		if existing == nil {
			copy := candidate
			merged[candidate.Group.GroupID] = &copy
			continue
		}
		existing.Group.CommitAllocations = mergeV2Allocations(existing.Group.CommitAllocations, candidate.Group.CommitAllocations)
		existing.Group.EvidenceDigest = v2AllocationEvidenceDigest(existing.Group.CommitAllocations)
		existing.Group.RequestIDs = uniqueSorted(append(existing.Group.RequestIDs, candidate.Group.RequestIDs...))
		if existing.GapReason != "" && candidate.GapReason == "" {
			requests := existing.Group.RequestIDs
			existing.GapReason = ""
			existing.Group = candidate.Group
			existing.Group.RequestIDs = requests
		}
	}
	result := make([]V2ClaimCandidate, 0, len(merged))
	for _, candidate := range merged {
		result = append(result, *candidate)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Group.GroupID < result[j].Group.GroupID })
	return result
}

func UploadableV2ClaimGroups(candidates []V2ClaimCandidate) []client.AttributionV2ClaimGroup {
	groups := make([]client.AttributionV2ClaimGroup, 0, len(candidates))
	for _, candidate := range candidates {
		if v2ClaimUploadable(candidate) {
			groups = append(groups, candidate.Group)
		}
	}
	return groups
}

func parseCodexV2ClaimFile(ctx context.Context, path string, opts V2ClaimScanOptions, requestEvidence []v2RequestEvidence) ([]V2ClaimCandidate, error) {
	return parseCodexV2ClaimFileBatch(ctx, path, []V2ClaimScanOptions{opts}, requestEvidence)
}

func parseCodexV2ClaimFileBatch(ctx context.Context, path string, options []V2ClaimScanOptions, requestEvidence []v2RequestEvidence) ([]V2ClaimCandidate, error) {
	var sessionID, threadID string
	turnSets := make([]map[string]*v2Turn, len(options))
	for index := range turnSets {
		turnSets[index] = map[string]*v2Turn{}
	}
	currentTurnID := ""
	err := forEachCodexJSONLLine(ctx, path, func(_ int, raw []byte) error {
		var row struct {
			Type      string `json:"type"`
			Timestamp string `json:"timestamp"`
			Payload   struct {
				ID        string `json:"id"`
				CallID    string `json:"call_id"`
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
					Content       string `json:"content"`
				} `json:"changes"`
			} `json:"payload"`
		}
		if err := json.Unmarshal(raw, &row); err != nil {
			return nil
		}
		observedAt := parseObservedAt(row.Timestamp)
		switch strings.TrimSpace(row.Type) {
		case "session_meta":
			sessionID = strings.TrimSpace(row.Payload.ID)
			threadID = firstNonEmptyCompact(strings.TrimSpace(row.Payload.ThreadID), sessionID)
		case "turn_context":
			turnID := strings.TrimSpace(row.Payload.TurnID)
			if turnID == "" {
				currentTurnID = ""
				return nil
			}
			currentTurnID = turnID
			for _, turns := range turnSets {
				if turns[turnID] == nil {
					turns[turnID] = &v2Turn{threadID: firstNonEmptyCompact(strings.TrimSpace(row.Payload.ThreadID), threadID, sessionID), turnID: turnID, requests: map[string]struct{}{}, transportIDs: map[string]struct{}{}, replayFiles: map[string]v2ReplayFile{}, startedAt: observedAt}
				}
			}
		case "response_item":
			for index, turns := range turnSets {
				current := turns[currentTurnID]
				if current == nil {
					continue
				}
				for _, transportID := range []string{row.Payload.ID, row.Payload.CallID} {
					if transportID = strings.TrimSpace(transportID); transportID != "" {
						current.transportIDs[transportID] = struct{}{}
					}
				}
				if patch := v2StructuredPatchInput(row.Payload.Name, row.Payload.Input, row.Payload.Arguments); patch != "" {
					opts := options[index]
					current.mutations = append(current.mutations, v2PatchMutations(ctx, patch, opts.RepoRoot, opts.CommitSHA, current.replayFiles)...)
				}
			}
		case "event_msg":
			for index, turns := range turnSets {
				current := turns[currentTurnID]
				if current == nil {
					continue
				}
				if strings.TrimSpace(row.Payload.Type) == "patch_apply_end" {
					for path, change := range row.Payload.Changes {
						hash := firstNonEmptyCompact(strings.TrimSpace(change.ContentSHA256), strings.TrimSpace(change.SHA256))
						if hash == "" && change.Content != "" {
							hash = claimDigest(change.Content)
						}
						if hash != "" {
							current.mutations = append(current.mutations, v2Mutation{path: canonicalClaimPath(options[index].RepoRoot, path), hash: strings.ToLower(hash), kind: strings.TrimSpace(change.Type)})
						}
					}
				}
				if strings.TrimSpace(row.Payload.Type) == "token_count" && row.Payload.Info != nil {
					addV2Calibration(&current.calibration, row.Payload.Info)
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	var result []V2ClaimCandidate
	for index, turns := range turnSets {
		result = append(result, buildCodexV2ClaimCandidates(ctx, path, sessionID, options[index], turns, requestEvidence)...)
	}
	return result, nil
}

func buildCodexV2ClaimCandidates(ctx context.Context, path, sessionID string, opts V2ClaimScanOptions, turns map[string]*v2Turn, requestEvidence []v2RequestEvidence) []V2ClaimCandidate {
	orderedTurns := make([]*v2Turn, 0, len(turns))
	for _, turn := range turns {
		orderedTurns = append(orderedTurns, turn)
	}
	sort.Slice(orderedTurns, func(i, j int) bool { return orderedTurns[i].startedAt.Before(orderedTurns[j].startedAt) })
	for _, evidence := range requestEvidence {
		if evidence.threadID != "" && evidence.turnID != "" {
			for _, turn := range orderedTurns {
				if turn.threadID == evidence.threadID && turn.turnID == evidence.turnID {
					turn.requests[evidence.requestID] = struct{}{}
				}
			}
			continue
		}
		var matched *v2Turn
		for _, turn := range orderedTurns {
			if intersectsV2TransportIDs(turn.transportIDs, evidence.transportIDs) {
				if matched != nil {
					matched = nil
					break
				}
				matched = turn
			}
		}
		if matched != nil {
			matched.requests[evidence.requestID] = struct{}{}
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
		} else if !validV2Mutations(turn.mutations) {
			candidate.GapReason = "invalid_structured_mutation"
		} else if introduced := introducedV2Mutations(ctx, opts.RepoRoot, opts.CommitSHA, turn.mutations); len(introduced) == 0 {
			candidate.GapReason = "commit_content_mismatch"
		} else {
			evidenceDigest := v2MutationDigest(introduced)
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
	return result
}

func v2StructuredPatchInput(toolName, input, arguments string) string {
	payload := input + "\n" + arguments
	if compactIsPatchTool(toolName) {
		return payload
	}
	match := v2WrappedPatchPattern.FindStringSubmatch(payload)
	if len(match) != 2 {
		return ""
	}
	patch, err := strconv.Unquote(match[1])
	if err != nil {
		return ""
	}
	patch = strings.TrimSpace(patch)
	if !strings.HasPrefix(patch, "*** Begin Patch\n") || !strings.HasSuffix(patch, "\n*** End Patch") {
		return ""
	}
	return patch
}

func v2PatchMutations(ctx context.Context, patch, repoRoot, commitSHA string, replayFiles map[string]v2ReplayFile) []v2Mutation {
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
			value := strings.Join(content, "\n") + "\n"
			file, loaded := replayFiles[path]
			if !loaded {
				parent, err := gitShowClaimFile(ctx, repoRoot, strings.TrimSpace(commitSHA)+"^", path)
				file = v2ReplayFile{content: string(parent), exists: err == nil}
			}
			if file.exists {
				if file.content == value {
					mutations = append(mutations, v2Mutation{path: path, hash: claimDigest(value), kind: kind})
				} else {
					mutations = append(mutations, v2Mutation{path: path, kind: kind})
				}
				continue
			}
			replayFiles[path] = v2ReplayFile{content: value, exists: true}
			mutations = append(mutations, v2Mutation{path: path, hash: claimDigest(value), kind: kind})
		case "delete":
			file, loaded := replayFiles[path]
			if !loaded {
				parent, err := gitShowClaimFile(ctx, repoRoot, strings.TrimSpace(commitSHA)+"^", path)
				file = v2ReplayFile{content: string(parent), exists: err == nil}
			}
			if file.exists {
				mutations = append(mutations, v2Mutation{path: path, hash: claimDigest("deleted"), kind: kind})
			}
			replayFiles[path] = v2ReplayFile{}
		case "update":
			file, loaded := replayFiles[path]
			if !loaded {
				parent, err := gitShowClaimFile(ctx, repoRoot, strings.TrimSpace(commitSHA)+"^", path)
				file = v2ReplayFile{content: string(parent), exists: err == nil}
			}
			if !file.exists {
				mutations = append(mutations, v2Mutation{path: path, kind: kind})
				continue
			}
			expected, ok := applyV2PatchBlock(file.content, block)
			if ok {
				replayFiles[path] = v2ReplayFile{content: expected, exists: true}
				mutations = append(mutations, v2Mutation{path: path, hash: claimDigest(expected), kind: kind})
			} else if _, ok := applyV2PatchBlock(file.content, reverseV2PatchBlock(block)); ok {
				// The patch was introduced by an earlier commit and the current
				// parent already contains its result.
				mutations = append(mutations, v2Mutation{path: path, hash: claimDigest(file.content), kind: kind})
			} else {
				mutations = append(mutations, v2Mutation{path: path, kind: kind})
			}
		}
	}
	return mutations
}

func reverseV2PatchBlock(block []string) []string {
	reversed := make([]string, len(block))
	for index, line := range block {
		switch {
		case strings.HasPrefix(line, "+"):
			reversed[index] = "-" + line[1:]
		case strings.HasPrefix(line, "-"):
			reversed[index] = "+" + line[1:]
		default:
			reversed[index] = line
		}
	}
	return reversed
}

func validV2Mutations(mutations []v2Mutation) bool {
	for _, mutation := range mutations {
		if mutation.path == "" || mutation.hash == "" {
			return false
		}
	}
	return true
}

func introducedV2Mutations(ctx context.Context, repoRoot, commitSHA string, mutations []v2Mutation) []v2Mutation {
	if strings.TrimSpace(repoRoot) == "" || strings.TrimSpace(commitSHA) == "" {
		return nil
	}
	selected := make([]bool, len(mutations))
	byPath := map[string][]int{}
	for index, mutation := range mutations {
		byPath[mutation.path] = append(byPath[mutation.path], index)
	}
	for path, indices := range byPath {
		parent, parentErr := gitShowClaimFile(ctx, repoRoot, strings.TrimSpace(commitSHA)+"^", path)
		current, currentErr := gitShowClaimFile(ctx, repoRoot, strings.TrimSpace(commitSHA), path)
		parentState := v2TreeState(parent, parentErr)
		currentState := v2TreeState(current, currentErr)
		if parentState == currentState {
			continue
		}
		currentPosition := -1
		for position, index := range indices {
			if v2MutationState(mutations[index]) == currentState {
				currentPosition = position
			}
		}
		if currentPosition < 0 {
			continue
		}
		parentPosition := -1
		for position := 0; position < currentPosition; position++ {
			if v2MutationState(mutations[indices[position]]) == parentState {
				parentPosition = position
			}
		}
		for position := parentPosition + 1; position <= currentPosition; position++ {
			selected[indices[position]] = true
		}
	}
	result := make([]v2Mutation, 0, len(mutations))
	for index, mutation := range mutations {
		if selected[index] {
			result = append(result, mutation)
		}
	}
	return result
}

func v2TreeState(content []byte, err error) string {
	if err != nil {
		return "deleted"
	}
	return claimDigest(string(content))
}

func v2MutationState(mutation v2Mutation) string {
	if mutation.kind == "delete" {
		return "deleted"
	}
	return strings.ToLower(mutation.hash)
}

func canonicalClaimPath(repoRoot, path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if filepath.IsAbs(path) {
		if resolved, err := filepath.EvalSymlinks(repoRoot); err == nil {
			repoRoot = resolved
		}
		if resolved, err := filepath.EvalSymlinks(filepath.Dir(path)); err == nil {
			path = filepath.Join(resolved, filepath.Base(path))
		}
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

func loadCodexV2RequestEvidence(ctx context.Context, homeDir string, cutoff time.Time) ([]v2RequestEvidence, error) {
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
		SELECT thread_id, feedback_log_body
		FROM logs
		WHERE target = ?
		  AND ts >= ?
		  AND feedback_log_body LIKE '%Request completed method=POST%'
		  AND feedback_log_body LIKE '%api.path="responses"%'
		  AND feedback_log_body LIKE '%status=2%'
		  AND feedback_log_body LIKE '%"x-client-request-id"%'
		ORDER BY ts, ts_nanos, id`, codexResponsesHTTPClientTarget, cutoff.Unix())
	if err != nil {
		return nil, fmt.Errorf("query Codex request log: %w", err)
	}
	defer rows.Close()
	byRequest := map[string]v2RequestEvidence{}
	ambiguous := map[string]struct{}{}
	for rows.Next() {
		var threadID sql.NullString
		var body string
		if err := rows.Scan(&threadID, &body); err != nil {
			return nil, fmt.Errorf("scan Codex request log: %w", err)
		}
		requestID := normalizeV2RequestID(firstSubmatch(reFailHdrClientReqID, body))
		turnID := firstSubmatch(v2TurnIDPattern, body)
		thread := firstNonEmptyCompact(firstSubmatch(v2ThreadIDPattern, body), threadID.String)
		if requestID == "" || thread == "" || turnID == "" || !v2SuccessfulResponseStatus.MatchString(body) {
			continue
		}
		evidence := v2RequestEvidence{threadID: thread, turnID: turnID, requestID: requestID}
		if existing, ok := byRequest[requestID]; ok && (existing.threadID != thread || existing.turnID != turnID) {
			delete(byRequest, requestID)
			ambiguous[requestID] = struct{}{}
			continue
		}
		if _, rejected := ambiguous[requestID]; !rejected {
			byRequest[requestID] = evidence
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate Codex request log: %w", err)
	}
	result := make([]v2RequestEvidence, 0, len(byRequest))
	for _, evidence := range byRequest {
		result = append(result, evidence)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].requestID < result[j].requestID })
	return result, nil
}

func intersectsV2TransportIDs(turnIDs map[string]struct{}, evidenceIDs []string) bool {
	for _, evidenceID := range evidenceIDs {
		if _, ok := turnIDs[evidenceID]; ok {
			return true
		}
	}
	return false
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
	value = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(value), "client:"))
	if value == "" {
		return ""
	}
	return "client:" + value
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
