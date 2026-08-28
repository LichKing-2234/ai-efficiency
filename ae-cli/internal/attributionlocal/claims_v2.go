package attributionlocal

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ai-efficiency/ae-cli/internal/client"
	"github.com/google/uuid"
)

const v2ClaimSchemaVersion = 2
const codexResponsesHTTPClientTarget = "codex_http_client::client"
const codexResponsesWebSocketEventTarget = "codex_api::sse::responses"
const codexResponsesWebSocketCompletionTarget = "codex_core::session::turn"
const v2LocalEvidenceWindow = 90 * 24 * time.Hour

const (
	v2GapMissingRequestID         = "missing_request_id"
	v2GapAmbiguousRequestEvidence = "ambiguous_request_evidence"
	v2GapRequestEvidenceExpired   = "request_evidence_expired"
	// v2GapUnrecognizedPatchWrapper marks a turn where a generated apply_patch
	// wrapper was present but did not satisfy the accepted grammar. It is kept
	// distinct from a turn with no mutation at all so that wrapper drift is
	// countable instead of silent.
	v2GapUnrecognizedPatchWrapper = "unrecognized_patch_wrapper"
)

var codexV2SourceReadObserver = func(string) {}

var (
	v2ThreadIDPattern            = regexp.MustCompile(`thread\.id=([^ }]+)`)
	v2TurnIDPattern              = regexp.MustCompile(`turn\.id=([^ }]+)`)
	v2SuccessfulResponseStatus   = regexp.MustCompile(` status=2[0-9]{2} `)
	v2WrappedPatchPattern        = regexp.MustCompile(`(?s)^\s*const\s+([A-Za-z_$][A-Za-z0-9_$]*)\s*=\s*("(?:\\.|[^"\\])*")\s*;\s*text\s*\(\s*await\s+tools\.apply_patch\s*\(\s*([A-Za-z_$][A-Za-z0-9_$]*)\s*\)\s*\)\s*;\s*$`)
	v2ThreeStatementPatchPattern = regexp.MustCompile(`(?s)^\s*const\s+([A-Za-z_$][A-Za-z0-9_$]*)\s*=\s*("(?:\\.|[^"\\])*")\s*;\s*const\s+([A-Za-z_$][A-Za-z0-9_$]*)\s*=\s*await\s+tools\.apply_patch\s*\(\s*([A-Za-z_$][A-Za-z0-9_$]*)\s*\)\s*;\s*text\s*\(\s*([A-Za-z_$][A-Za-z0-9_$]*)\s*\)\s*;\s*$`)
	// v2PatchWrapperHintPattern is deliberately loose. It never authorises an
	// allocation; it only tells us a turn tried to call apply_patch so that a
	// wrapper we do not accept can be counted rather than lost.
	v2PatchWrapperHintPattern = regexp.MustCompile(`tools\.apply_patch\s*\(`)
	v2InlinePatchPattern      = regexp.MustCompile(`(?s)^\s*const\s+([A-Za-z_$][A-Za-z0-9_$]*)\s*=\s*await\s+tools\.apply_patch\s*\(\s*("(?:\\.|[^"\\])*")\s*\)\s*;\s*text\s*\(\s*JSON\.stringify\s*\(\s*([A-Za-z_$][A-Za-z0-9_$]*)\s*\)\s*\)\s*;\s*$`)
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
	sources            []codexV2ClaimSource
	evidence           []v2RequestEvidence
	evidenceLowerBound time.Time
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
	evidence, evidenceLowerBound, err := loadCodexV2RequestEvidence(ctx, homeDir, cutoff)
	if err != nil {
		return nil, fmt.Errorf("load Codex v2 request evidence: %w", err)
	}
	if evidenceLowerBound.After(cutoff) {
		cutoff = evidenceLowerBound
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
	return &CodexV2ClaimScan{sources: sources, evidence: evidence, evidenceLowerBound: evidenceLowerBound}, nil
}

// FinalizeCandidates applies cross-source Request evidence classifications
// after every source in the runner pass has contributed compact candidates.
func (s *CodexV2ClaimScan) FinalizeCandidates(candidates []V2ClaimCandidate) {
	if s == nil {
		return
	}
	finalizeV2RequestEvidence(candidates, s.evidence, s.evidenceLowerBound)
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
	scan.FinalizeCandidates(candidates)
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
		if existing.Group.TokenSource != "" && candidate.Group.TokenSource != "" && existing.Group.TokenSource != candidate.Group.TokenSource {
			existing.GapReason = "mixed_token_sources"
			existing.DeliveryStatus = ""
			existing.LastDeliveryError = ""
			existing.UpdatedAt = candidate.UpdatedAt
			continue
		}
		if candidate.GapReason == "mixed_token_sources" {
			existing.GapReason = candidate.GapReason
			existing.DeliveryStatus = ""
			existing.LastDeliveryError = ""
			existing.UpdatedAt = candidate.UpdatedAt
			continue
		}
		candidate.Group.RequestIDs = filterAcknowledgedV2Requests(candidate.Group.RequestIDs, existing.AcknowledgedRequestDigests)
		requestCount := len(existing.Group.RequestIDs)
		existing.Group.RequestIDs = uniqueSorted(append(existing.Group.RequestIDs, candidate.Group.RequestIDs...))
		newRequestCount := len(existing.Group.RequestIDs) - requestCount
		allocationCount := len(existing.Group.CommitAllocations)
		existing.Group.CommitAllocations = mergeV2Allocations(existing.Group.CommitAllocations, candidate.Group.CommitAllocations)
		allocationChanged := len(existing.Group.CommitAllocations) > allocationCount
		evidenceDigest := existing.Group.EvidenceDigest
		existing.Group.EvidenceDigest = v2AllocationEvidenceDigest(existing.Group.CommitAllocations)
		evidenceChanged := existing.Group.EvidenceDigest != evidenceDigest
		if existing.Group.TokenSource == "" {
			existing.Group.TokenSource = candidate.Group.TokenSource
		}
		localUsageBefore := v2LocalUsageDigest(existing.Group.LocalUsage)
		if existing.Group.TokenSource == candidate.Group.TokenSource {
			existing.Group.LocalUsage = mergeV2LocalUsage(existing.Group.LocalUsage, candidate.Group.LocalUsage)
		}
		localUsageChanged := localUsageBefore != v2LocalUsageDigest(existing.Group.LocalUsage)
		calibrationChanged := false
		if existing.Group.Calibration == nil && candidate.Group.Calibration != nil && candidate.Group.Calibration.Digest != existing.AcknowledgedCalibrationDigest {
			existing.Group.Calibration = candidate.Group.Calibration
			calibrationChanged = true
		}
		existing.UpdatedAt = candidate.UpdatedAt
		if allocationChanged || localUsageChanged {
			existing.GroupAcknowledged = false
		}
		if newRequestCount > 0 || calibrationChanged || allocationChanged || evidenceChanged || localUsageChanged {
			existing.DeliveryStatus = V2DeliveryPending
			existing.LastDeliveryError = ""
		}
		if existing.GapReason != "" && candidate.GapReason == "" {
			existing.GapReason = ""
			existing.Group.TokenSource = candidate.Group.TokenSource
			existing.Group.EvidenceDigest = candidate.Group.EvidenceDigest
			existing.Group.Calibration = candidate.Group.Calibration
			existing.Group.LocalUsage = candidate.Group.LocalUsage
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
	threadID            string
	turnID              string
	model               string
	webSocket           bool
	requests            map[string]struct{}
	transportIDs        map[string]struct{}
	mutations           []v2Mutation
	replayFiles         map[string]v2ReplayFile
	calibration         client.AttributionV2Calibration
	localUsage          map[string]*client.AttributionV2LocalUsageBucket
	localInvalid        bool
	unrecognizedWrapper bool
	startedAt           time.Time
}

type v2ReplayFile struct {
	content string
	exists  bool
}

type v2RequestEvidence struct {
	threadID     string
	turnID       string
	requestID    string
	webSocket    bool
	ambiguous    bool
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
		existing.Group.LocalUsage = mergeV2LocalUsage(existing.Group.LocalUsage, candidate.Group.LocalUsage)
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

// UploadableV2ClaimGroups collects the groups ready to send, with each group id
// appearing once.
//
// Local state is keyed by the turn a claim was observed in, while a group is
// named by what it proves. Those disagree after a Claude Code resume: the agent
// replays the same work under a new session id and a restarted turn counter, so
// two entries describe one piece of work and name one group. Sending both put
// the same group id in a batch twice, and the backend rejected the second for
// disagreeing with the first about which session and turn it came from, which
// failed the whole batch.
//
// Collapsing them is not a workaround for that rejection but the correct
// reading of it: the same group id means the same commit and the same evidence.
// Their commit allocations are unioned, and their usage is added. Adding is
// safe because the scan already settled which turn each response belongs to:
// every response is priced into exactly one turn's claim (the earliest
// occurrence wins), so two claims that meet here carry disjoint partitions of
// the consumption, never two copies of it. Measured on live data, each
// candidate's buckets equalled its winner partition exactly, and keeping only
// the first partition — the previous behaviour — silently dropped the others,
// permanently: the acknowledgement maps back by group id, so the dropped
// claims were still marked delivered and never re-sent.
func UploadableV2ClaimGroups(candidates []V2ClaimCandidate) []client.AttributionV2ClaimGroup {
	groups := make([]client.AttributionV2ClaimGroup, 0, len(candidates))
	index := map[string]int{}
	for _, candidate := range candidates {
		if !v2ClaimUploadable(candidate) {
			continue
		}
		groupID := strings.TrimSpace(candidate.Group.GroupID)
		at, seen := index[groupID]
		if !seen || groupID == "" {
			groups = append(groups, candidate.Group)
			if groupID != "" {
				index[groupID] = len(groups) - 1
			}
			continue
		}
		kept := &groups[at]
		kept.CommitAllocations = mergeV2Allocations(kept.CommitAllocations, candidate.Group.CommitAllocations)
		kept.EvidenceDigest = v2AllocationEvidenceDigest(kept.CommitAllocations)
		kept.RequestIDs = uniqueSorted(append(kept.RequestIDs, candidate.Group.RequestIDs...))
		kept.LocalUsage = sumV2LocalUsage(kept.LocalUsage, candidate.Group.LocalUsage)
		if kept.Calibration == nil {
			kept.Calibration = candidate.Group.Calibration
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
	var previousCumulativeUsage v2TokenUsage
	var previousIncrementalUsage v2TokenUsage
	firstTokenInTurn := false
	compactedTurnID := ""
	err := forEachCodexJSONLLine(ctx, path, func(_ int, raw []byte) error {
		var row struct {
			Type      string `json:"type"`
			Timestamp string `json:"timestamp"`
			Payload   struct {
				ID        string `json:"id"`
				CallID    string `json:"call_id"`
				ThreadID  string `json:"thread_id"`
				TurnID    string `json:"turn_id"`
				Model     string `json:"model"`
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
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		if err := decoder.Decode(&row); err != nil {
			return nil
		}
		var trailing any
		if err := decoder.Decode(&trailing); err != io.EOF {
			return nil
		}
		observedAt := parseObservedAt(row.Timestamp)
		switch strings.TrimSpace(row.Type) {
		case "session_meta":
			sessionID = strings.TrimSpace(row.Payload.ID)
			threadID = firstNonEmpty(strings.TrimSpace(row.Payload.ThreadID), sessionID)
		case "turn_context":
			turnID := strings.TrimSpace(row.Payload.TurnID)
			if turnID == "" {
				currentTurnID = ""
				return nil
			}
			if turnID != currentTurnID {
				firstTokenInTurn = true
			}
			currentTurnID = turnID
			for _, turns := range turnSets {
				if turns[turnID] == nil {
					turns[turnID] = &v2Turn{threadID: firstNonEmpty(strings.TrimSpace(row.Payload.ThreadID), threadID, sessionID), turnID: turnID, model: strings.TrimSpace(row.Payload.Model), requests: map[string]struct{}{}, transportIDs: map[string]struct{}{}, replayFiles: map[string]v2ReplayFile{}, localUsage: map[string]*client.AttributionV2LocalUsageBucket{}, startedAt: observedAt}
				}
			}
		case "compacted":
			if currentTurnID != "" && previousCumulativeUsage.valid {
				compactedTurnID = currentTurnID
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
				patch, unrecognizedWrapper := v2StructuredPatchInput(row.Payload.Name, row.Payload.Input, row.Payload.Arguments)
				if patch != "" {
					opts := options[index]
					current.mutations = append(current.mutations, v2PatchMutations(ctx, patch, opts.RepoRoot, opts.CommitSHA, current.replayFiles)...)
				} else if unrecognizedWrapper {
					current.unrecognizedWrapper = true
				}
			}
		case "event_msg":
			var tokenUsage v2TokenUsage
			invalidLocalUsage := false
			if strings.TrimSpace(row.Payload.Type) == "token_count" && row.Payload.Info != nil {
				var cumulativeUsage v2TokenUsage
				tokenUsage, cumulativeUsage = parseV2TokenUsage(row.Payload.Info)
				isFirstTokenInTurn := firstTokenInTurn
				firstTokenInTurn = false
				if !isFirstTokenInTurn && tokenUsage.valid && cumulativeUsage.valid && v2TokenUsageEqual(cumulativeUsage, previousCumulativeUsage) {
					if compactedTurnID == currentTurnID {
						previousIncrementalUsage = tokenUsage
						tokenUsage = v2TokenUsage{}
					} else if v2TokenUsageEqual(tokenUsage, previousIncrementalUsage) {
						tokenUsage = v2TokenUsage{}
					} else {
						invalidLocalUsage = true
					}
				} else if !tokenUsage.valid || !cumulativeUsage.valid || (!v2TokenUsageDeltaMatches(previousCumulativeUsage, cumulativeUsage, tokenUsage) && (!isFirstTokenInTurn || !v2TokenUsageDeltaMatches(v2TokenUsage{}, cumulativeUsage, tokenUsage))) {
					invalidLocalUsage = true
				} else {
					previousCumulativeUsage = cumulativeUsage
					previousIncrementalUsage = tokenUsage
				}
				compactedTurnID = ""
			}
			for index, turns := range turnSets {
				current := turns[currentTurnID]
				if current == nil {
					continue
				}
				if strings.TrimSpace(row.Payload.Type) == "patch_apply_end" {
					for path, change := range row.Payload.Changes {
						hash := firstNonEmpty(strings.TrimSpace(change.ContentSHA256), strings.TrimSpace(change.SHA256))
						if hash == "" && change.Content != "" {
							hash = claimDigest(change.Content)
						}
						if hash != "" {
							current.mutations = append(current.mutations, v2Mutation{path: canonicalClaimPath(options[index].RepoRoot, path), hash: strings.ToLower(hash), kind: strings.TrimSpace(change.Type)})
						}
					}
				}
				if tokenUsage.valid {
					addV2Calibration(&current.calibration, tokenUsage)
					if !invalidLocalUsage && !addV2LocalUsage(current, tokenUsage, observedAt) {
						invalidLocalUsage = true
					}
				}
				if invalidLocalUsage {
					current.localInvalid = true
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
		if evidence.ambiguous {
			continue
		}
		if evidence.threadID != "" && evidence.turnID != "" {
			for _, turn := range orderedTurns {
				if turn.threadID == evidence.threadID && turn.turnID == evidence.turnID {
					if evidence.requestID != "" {
						turn.requests[evidence.requestID] = struct{}{}
					}
					turn.webSocket = turn.webSocket || evidence.webSocket
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
			TokenSource: client.AttributionV2TokenSourceRelayOfficial, ThreadID: turn.threadID, TurnID: turn.turnID, RequestIDs: requests,
		}}
		proofGap := ""
		if turn.webSocket {
			candidate.Group.TokenSource = client.AttributionV2TokenSourceCodexLocal
		}
		switch {
		case turn.webSocket && turn.localInvalid:
			proofGap = "invalid_local_usage"
		case turn.webSocket && len(turn.localUsage) == 0:
			proofGap = "missing_local_usage"
		case len(turn.mutations) == 0 && turn.unrecognizedWrapper:
			proofGap = v2GapUnrecognizedPatchWrapper
		case len(turn.mutations) == 0:
			proofGap = "missing_structured_mutation"
		case !validV2Mutations(turn.mutations):
			proofGap = "invalid_structured_mutation"
		default:
			introduced := introducedV2Mutations(ctx, opts.RepoRoot, opts.CommitSHA, turn.mutations)
			if len(introduced) == 0 {
				proofGap = "commit_content_mismatch"
				break
			}
			if turn.webSocket {
				candidate.Group.LocalUsage = sortedV2LocalUsage(turn.localUsage)
			}
			evidenceDigest := v2MutationDigest(introduced)
			candidate.Group.EvidenceDigest = evidenceDigest
			candidate.Group.CommitAllocations = []client.AttributionV2CommitAllocation{{
				Sequence: 1, RepoConfigID: opts.RepoConfigID, RepoKey: strings.TrimSpace(opts.RepoKey), WorkspaceID: strings.TrimSpace(opts.WorkspaceID),
				CheckpointEventID: opts.CheckpointEventID, CommitSHA: opts.CommitSHA, EvidenceDigest: evidenceDigest,
			}}
			if !turn.webSocket && turn.calibration.TotalTokens > 0 {
				turn.calibration.Digest = v2CalibrationDigest(turn.calibration)
				calibration := turn.calibration
				candidate.Group.Calibration = &calibration
			}
		}
		switch {
		case len(requests) > 0 && turn.webSocket:
			candidate.GapReason = "mixed_token_sources"
		case len(requests) == 0 && !turn.webSocket:
			candidate.GapReason = v2GapMissingRequestID
		default:
			candidate.GapReason = proofGap
		}
		result = append(result, candidate)
	}
	return result
}

func finalizeV2RequestEvidence(candidates []V2ClaimCandidate, evidence []v2RequestEvidence, lowerBound time.Time) {
	jsonlIdentities := map[string]map[string]struct{}{}
	for index := range candidates {
		candidate := &candidates[index]
		if candidate.GapReason == v2GapMissingRequestID && !lowerBound.IsZero() && candidate.FirstSeenAt.Before(lowerBound) {
			candidate.GapReason = v2GapRequestEvidenceExpired
		}
		turnID := strings.TrimSpace(candidate.Group.TurnID)
		if turnID == "" || candidate.GapReason == v2GapRequestEvidenceExpired {
			continue
		}
		identities := jsonlIdentities[turnID]
		if identities == nil {
			identities = map[string]struct{}{}
			jsonlIdentities[turnID] = identities
		}
		identities[candidate.LocalKey] = struct{}{}
	}
	type sqliteTurnEvidence struct {
		threads   map[string]struct{}
		requests  []string
		ambiguous bool
	}
	sqliteTurns := map[string]*sqliteTurnEvidence{}
	for _, item := range evidence {
		if item.webSocket || strings.TrimSpace(item.threadID) == "" || strings.TrimSpace(item.turnID) == "" {
			continue
		}
		turn := sqliteTurns[item.turnID]
		if turn == nil {
			turn = &sqliteTurnEvidence{threads: map[string]struct{}{}}
			sqliteTurns[item.turnID] = turn
		}
		turn.threads[item.threadID] = struct{}{}
		turn.ambiguous = turn.ambiguous || item.ambiguous
		if !item.ambiguous && strings.TrimSpace(item.requestID) != "" {
			turn.requests = append(turn.requests, item.requestID)
		}
	}
	for index := range candidates {
		candidate := &candidates[index]
		if candidate.GapReason != v2GapMissingRequestID || len(candidate.Group.RequestIDs) > 0 {
			continue
		}
		turn := sqliteTurns[candidate.Group.TurnID]
		if turn == nil {
			continue
		}
		if _, err := uuid.Parse(candidate.Group.TurnID); err != nil {
			continue
		}
		if turn.ambiguous || len(turn.requests) == 0 || len(jsonlIdentities[candidate.Group.TurnID]) != 1 || len(turn.threads) != 1 {
			candidate.GapReason = v2GapAmbiguousRequestEvidence
			continue
		}
		if len(candidate.Group.CommitAllocations) == 0 || strings.TrimSpace(candidate.Group.EvidenceDigest) == "" {
			continue
		}
		candidate.Group.RequestIDs = uniqueSorted(turn.requests)
		candidate.GapReason = ""
	}
}

// v2StructuredPatchInput returns the patch a generated wrapper applied, and
// whether the payload looked like an apply_patch attempt that the accepted
// grammar rejected. The second value is diagnostic only and never widens what
// may authorise a commit allocation.
func v2StructuredPatchInput(toolName, input, arguments string) (string, bool) {
	payload := input + "\n" + arguments
	if isPatchTool(toolName) {
		return payload, false
	}
	encodedPatch := ""
	if match := v2WrappedPatchPattern.FindStringSubmatch(payload); len(match) == 4 && match[1] == match[3] {
		encodedPatch = match[2]
	} else if match := v2ThreeStatementPatchPattern.FindStringSubmatch(payload); len(match) == 6 && match[1] == match[4] && match[3] == match[5] {
		encodedPatch = match[2]
	} else if match := v2InlinePatchPattern.FindStringSubmatch(payload); len(match) == 4 && match[1] == match[3] {
		encodedPatch = match[2]
	}
	if encodedPatch == "" {
		return "", v2PatchWrapperHintPattern.MatchString(payload)
	}
	patch, err := strconv.Unquote(encodedPatch)
	if err != nil {
		return "", true
	}
	patch = strings.TrimSpace(patch)
	if !strings.HasPrefix(patch, "*** Begin Patch\n") || !strings.HasSuffix(patch, "\n*** End Patch") {
		return "", true
	}
	return patch, false
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

func loadCodexV2RequestEvidence(ctx context.Context, homeDir string, cutoff time.Time) ([]v2RequestEvidence, time.Time, error) {
	paths := findCodexSQLiteFiles(homeDir)
	if len(paths) == 0 {
		return nil, time.Time{}, nil
	}
	db, err := sql.Open("sqlite", codexSQLiteReadOnlyDSN(paths[0]))
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("open Codex request log: %w", err)
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, `
		SELECT ts, ts_nanos, thread_id, feedback_log_body
		FROM logs
		WHERE target = ?
		  AND ts >= ?
		  AND feedback_log_body LIKE '%Request completed method=POST%'
		  AND feedback_log_body LIKE '%api.path="responses"%'
		  AND feedback_log_body LIKE '%status=2%'
		  AND feedback_log_body LIKE '%"x-client-request-id"%'
		ORDER BY ts, ts_nanos, id`, codexResponsesHTTPClientTarget, cutoff.Unix())
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("query Codex request log: %w", err)
	}
	defer rows.Close()
	byRequest := map[string]v2RequestEvidence{}
	ambiguous := map[string]struct{}{}
	ambiguousEvidence := map[string]v2RequestEvidence{}
	var evidenceLowerBound time.Time
	for rows.Next() {
		var ts, tsNanos int64
		var threadID sql.NullString
		var body string
		if err := rows.Scan(&ts, &tsNanos, &threadID, &body); err != nil {
			return nil, time.Time{}, fmt.Errorf("scan Codex request log: %w", err)
		}
		requestID := normalizeV2RequestID(firstSubmatch(reFailHdrClientReqID, body))
		turnID := firstSubmatch(v2TurnIDPattern, body)
		thread := firstNonEmpty(firstSubmatch(v2ThreadIDPattern, body), threadID.String)
		if requestID == "" || thread == "" || turnID == "" || !v2SuccessfulResponseStatus.MatchString(body) {
			continue
		}
		observedAt := time.Unix(ts, tsNanos).UTC()
		if evidenceLowerBound.IsZero() || observedAt.Before(evidenceLowerBound) {
			evidenceLowerBound = observedAt
		}
		evidence := v2RequestEvidence{threadID: thread, turnID: turnID, requestID: requestID}
		if existing, ok := byRequest[requestID]; ok && (existing.threadID != thread || existing.turnID != turnID) {
			delete(byRequest, requestID)
			ambiguous[requestID] = struct{}{}
			existing.ambiguous = true
			evidence.ambiguous = true
			ambiguousEvidence[claimDigest(existing.requestID, existing.threadID, existing.turnID)] = existing
			ambiguousEvidence[claimDigest(evidence.requestID, evidence.threadID, evidence.turnID)] = evidence
			continue
		}
		if _, rejected := ambiguous[requestID]; rejected {
			evidence.ambiguous = true
			ambiguousEvidence[claimDigest(evidence.requestID, evidence.threadID, evidence.turnID)] = evidence
			continue
		}
		byRequest[requestID] = evidence
	}
	if err := rows.Err(); err != nil {
		return nil, time.Time{}, fmt.Errorf("iterate Codex request log: %w", err)
	}
	result := make([]v2RequestEvidence, 0, len(byRequest)+len(ambiguousEvidence))
	for _, evidence := range byRequest {
		result = append(result, evidence)
	}
	for _, evidence := range ambiguousEvidence {
		result = append(result, evidence)
	}
	webSocketRows, err := db.QueryContext(ctx, `
		SELECT thread_id, feedback_log_body
		FROM logs
		WHERE target = ?
		  AND ts >= ?
		  AND feedback_log_body LIKE '%model_client.stream_responses_websocket%'
		  AND feedback_log_body LIKE '%websocket event:%'
		ORDER BY ts, ts_nanos, id`, codexResponsesWebSocketEventTarget, cutoff.Unix())
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("query Codex WebSocket turn evidence: %w", err)
	}
	defer webSocketRows.Close()
	seenTurns := map[string]struct{}{}
	appendWebSocketTurn := func(thread, turn string) {
		if thread == "" || turn == "" {
			return
		}
		key := claimDigest(thread, turn)
		if _, exists := seenTurns[key]; exists {
			return
		}
		seenTurns[key] = struct{}{}
		result = append(result, v2RequestEvidence{threadID: thread, turnID: turn, webSocket: true})
	}
	for webSocketRows.Next() {
		var threadID sql.NullString
		var body string
		if err := webSocketRows.Scan(&threadID, &body); err != nil {
			return nil, time.Time{}, fmt.Errorf("scan Codex WebSocket turn evidence: %w", err)
		}
		thread, turn, ok := parseCodexV2WebSocketTurnEvidence(threadID.String, body)
		if !ok {
			continue
		}
		appendWebSocketTurn(thread, turn)
	}
	if err := webSocketRows.Err(); err != nil {
		return nil, time.Time{}, fmt.Errorf("iterate Codex WebSocket turn evidence: %w", err)
	}
	webSocketTransportRows, err := db.QueryContext(ctx, `
		SELECT thread_id, feedback_log_body
		FROM logs
		WHERE target = ?
		  AND ts >= ?
		  AND instr(feedback_log_body, 'model_client.stream_responses_websocket') > 0
		  AND instr(feedback_log_body, 'websocket.warmup=false') > 0
		  AND instr(feedback_log_body, 'unhandled responses event: response.in_progress') > 0
		ORDER BY ts, ts_nanos, id`, codexResponsesWebSocketEventTarget, cutoff.Unix())
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("query Codex WebSocket transport evidence: %w", err)
	}
	defer webSocketTransportRows.Close()
	transportTurns := map[string]v2RequestEvidence{}
	for webSocketTransportRows.Next() {
		var threadID sql.NullString
		var body string
		if err := webSocketTransportRows.Scan(&threadID, &body); err != nil {
			return nil, time.Time{}, fmt.Errorf("scan Codex WebSocket transport evidence: %w", err)
		}
		thread, turn, ok := parseCodexV2TurnSpan(threadID.String, body)
		if ok {
			transportTurns[claimDigest(thread, turn)] = v2RequestEvidence{threadID: thread, turnID: turn}
		}
	}
	if err := webSocketTransportRows.Err(); err != nil {
		return nil, time.Time{}, fmt.Errorf("iterate Codex WebSocket transport evidence: %w", err)
	}
	webSocketCompletionRows, err := db.QueryContext(ctx, `
		SELECT thread_id, feedback_log_body
		FROM logs
		WHERE target = ?
		  AND ts >= ?
		  AND instr(feedback_log_body, ':session_task.run:run_turn: post sampling token usage ') > 0
		ORDER BY ts, ts_nanos, id`, codexResponsesWebSocketCompletionTarget, cutoff.Unix())
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("query Codex sampling completion evidence: %w", err)
	}
	defer webSocketCompletionRows.Close()
	for webSocketCompletionRows.Next() {
		var threadID sql.NullString
		var body string
		if err := webSocketCompletionRows.Scan(&threadID, &body); err != nil {
			return nil, time.Time{}, fmt.Errorf("scan Codex sampling completion evidence: %w", err)
		}
		thread, turn, ok := parseCodexV2TurnSpan(threadID.String, body)
		if !ok {
			continue
		}
		if transport, exists := transportTurns[claimDigest(thread, turn)]; exists {
			appendWebSocketTurn(transport.threadID, transport.turnID)
		}
	}
	if err := webSocketCompletionRows.Err(); err != nil {
		return nil, time.Time{}, fmt.Errorf("iterate Codex sampling completion evidence: %w", err)
	}
	sort.Slice(result, func(i, j int) bool {
		left := result[i].threadID + "\x00" + result[i].turnID + "\x00" + result[i].requestID
		right := result[j].threadID + "\x00" + result[j].turnID + "\x00" + result[j].requestID
		return left < right
	})
	return result, evidenceLowerBound, nil
}

func parseCodexV2WebSocketTurnEvidence(fallbackThreadID, body string) (string, string, bool) {
	const marker = "websocket event:"
	markerIndex := strings.Index(body, marker)
	if markerIndex < 0 {
		return "", "", false
	}
	prefix := body[:markerIndex]
	if !strings.Contains(prefix, "model_client.stream_responses_websocket{") {
		return "", "", false
	}
	var event struct {
		Type     string `json:"type"`
		Response struct {
			ID string `json:"id"`
		} `json:"response"`
	}
	if err := json.NewDecoder(strings.NewReader(strings.TrimSpace(body[markerIndex+len(marker):]))).Decode(&event); err != nil || event.Type != "response.completed" || strings.TrimSpace(event.Response.ID) == "" {
		return "", "", false
	}
	return parseCodexV2TurnSpan(fallbackThreadID, prefix)
}

func parseCodexV2TurnSpan(fallbackThreadID, body string) (string, string, bool) {
	threadID := firstNonEmpty(firstSubmatch(v2ThreadIDPattern, body), strings.TrimSpace(fallbackThreadID))
	turnID := firstSubmatch(v2TurnIDPattern, body)
	return threadID, turnID, threadID != "" && turnID != ""
}

func intersectsV2TransportIDs(turnIDs map[string]struct{}, evidenceIDs []string) bool {
	for _, evidenceID := range evidenceIDs {
		if _, ok := turnIDs[evidenceID]; ok {
			return true
		}
	}
	return false
}

type v2TokenUsage struct {
	valid       bool
	input       int64
	output      int64
	cacheCreate int64
	cacheRead   int64
	total       int64
}

func parseV2TokenUsage(raw any) (v2TokenUsage, v2TokenUsage) {
	info, _ := raw.(map[string]any)
	last, _ := info["last_token_usage"].(map[string]any)
	total, _ := info["total_token_usage"].(map[string]any)
	return parseV2TokenUsageValues(last), parseV2TokenUsageValues(total)
}

func parseV2TokenUsageValues(selected map[string]any) v2TokenUsage {
	if len(selected) == 0 {
		return v2TokenUsage{}
	}
	input, inputOK := v2ExactTokenInt64(selected["input_tokens"])
	output, outputOK := v2ExactTokenInt64(selected["output_tokens"])
	cacheRead, cacheReadOK := v2ExactTokenInt64(selected["cached_input_tokens"])
	cacheCreate, cacheCreateOK := v2ExactTokenInt64(selected["cache_write_input_tokens"])
	total, totalOK := v2ExactTokenInt64(selected["total_tokens"])
	if !inputOK || !outputOK || !cacheReadOK || !cacheCreateOK || !totalOK {
		return v2TokenUsage{}
	}
	if total == 0 {
		if input > math.MaxInt64-output {
			return v2TokenUsage{}
		}
		total = input + output
	}
	if input < 0 || output < 0 || cacheRead < 0 || cacheCreate < 0 || cacheRead > input || cacheCreate > input-cacheRead || input > math.MaxInt64-output || total != input+output || total == 0 {
		return v2TokenUsage{}
	}
	return v2TokenUsage{valid: true, input: input - cacheRead - cacheCreate, output: output, cacheCreate: cacheCreate, cacheRead: cacheRead, total: total}
}

func v2ExactTokenInt64(value any) (int64, bool) {
	if value == nil {
		return 0, true
	}
	switch value := value.(type) {
	case int:
		return int64(value), true
	case int64:
		return value, true
	case json.Number:
		parsed, err := value.Int64()
		return parsed, err == nil
	case float64:
		if math.IsNaN(value) || math.IsInf(value, 0) || math.Trunc(value) != value || value < math.MinInt64 || value > math.MaxInt64 {
			return 0, false
		}
		parsed := int64(value)
		return parsed, float64(parsed) == value
	default:
		return 0, false
	}
}

func v2TokenUsageEqual(left, right v2TokenUsage) bool {
	return left.valid == right.valid && left.input == right.input && left.output == right.output && left.cacheCreate == right.cacheCreate && left.cacheRead == right.cacheRead && left.total == right.total
}

func v2TokenUsageDeltaMatches(previous, cumulative, delta v2TokenUsage) bool {
	if !cumulative.valid || !delta.valid || cumulative.input < previous.input || cumulative.output < previous.output ||
		cumulative.cacheCreate < previous.cacheCreate || cumulative.cacheRead < previous.cacheRead || cumulative.total < previous.total {
		return false
	}
	return cumulative.input-previous.input == delta.input && cumulative.output-previous.output == delta.output &&
		cumulative.cacheCreate-previous.cacheCreate == delta.cacheCreate && cumulative.cacheRead-previous.cacheRead == delta.cacheRead &&
		cumulative.total-previous.total == delta.total
}

func addV2Calibration(calibration *client.AttributionV2Calibration, usage v2TokenUsage) {
	calibration.InputTokens += usage.input
	calibration.OutputTokens += usage.output
	calibration.CacheReadTokens += usage.cacheRead
	calibration.CacheCreationTokens += usage.cacheCreate
	calibration.TotalTokens += usage.total
}

func addV2LocalUsage(turn *v2Turn, usage v2TokenUsage, observedAt time.Time) bool {
	if turn == nil || strings.TrimSpace(turn.model) == "" || observedAt.IsZero() {
		return false
	}
	bucket := observedAt.UTC().Truncate(15 * time.Minute)
	key := strings.TrimSpace(turn.model) + "\x00" + bucket.Format(time.RFC3339)
	value := turn.localUsage[key]
	if value == nil {
		value = &client.AttributionV2LocalUsageBucket{RequestedModel: strings.TrimSpace(turn.model), BucketStartUTC: bucket}
		turn.localUsage[key] = value
	}
	if value.InputTokens > math.MaxInt64-usage.input || value.OutputTokens > math.MaxInt64-usage.output ||
		value.CacheCreationTokens > math.MaxInt64-usage.cacheCreate || value.CacheReadTokens > math.MaxInt64-usage.cacheRead ||
		value.TotalTokens > math.MaxInt64-usage.total || value.RequestCount == math.MaxInt {
		return false
	}
	value.InputTokens += usage.input
	value.OutputTokens += usage.output
	value.CacheCreationTokens += usage.cacheCreate
	value.CacheReadTokens += usage.cacheRead
	value.TotalTokens += usage.total
	value.RequestCount++
	return true
}

func sortedV2LocalUsage(values map[string]*client.AttributionV2LocalUsageBucket) []client.AttributionV2LocalUsageBucket {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]client.AttributionV2LocalUsageBucket, 0, len(keys))
	for _, key := range keys {
		result = append(result, *values[key])
	}
	return result
}

// sumV2LocalUsage adds two usage partitions bucket by bucket.
//
// It is the collapse-time counterpart of mergeV2LocalUsage and must not be
// confused with it. mergeV2LocalUsage reconciles two observations of the SAME
// turn across scans, where an identical bucket is the same consumption seen
// twice and is kept once. Here the inputs come from DIFFERENT turns, whose
// responses the scan has already partitioned — each response priced into
// exactly one turn — so a shared bucket key just means two turns consuming in
// the same quarter hour, and the amounts add.
func sumV2LocalUsage(existing, incoming []client.AttributionV2LocalUsageBucket) []client.AttributionV2LocalUsageBucket {
	if len(incoming) == 0 {
		return existing
	}
	byKey := make(map[string]*client.AttributionV2LocalUsageBucket, len(existing)+len(incoming))
	order := make([]string, 0, len(existing)+len(incoming))
	for _, usage := range append(append([]client.AttributionV2LocalUsageBucket(nil), existing...), incoming...) {
		key := strings.TrimSpace(usage.RequestedModel) + "\x00" + usage.BucketStartUTC.UTC().Format(time.RFC3339)
		bucket, ok := byKey[key]
		if !ok {
			copied := usage
			copied.RequestedModel = strings.TrimSpace(copied.RequestedModel)
			copied.BucketStartUTC = copied.BucketStartUTC.UTC()
			byKey[key] = &copied
			order = append(order, key)
			continue
		}
		bucket.InputTokens += usage.InputTokens
		bucket.OutputTokens += usage.OutputTokens
		bucket.CacheCreationTokens += usage.CacheCreationTokens
		bucket.CacheReadTokens += usage.CacheReadTokens
		bucket.TotalTokens += usage.TotalTokens
		bucket.CreditUsage += usage.CreditUsage
		bucket.RequestCount += usage.RequestCount
	}
	out := make([]client.AttributionV2LocalUsageBucket, 0, len(order))
	for _, key := range order {
		out = append(out, *byKey[key])
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].RequestedModel != out[j].RequestedModel {
			return out[i].RequestedModel < out[j].RequestedModel
		}
		return out[i].BucketStartUTC.Before(out[j].BucketStartUTC)
	})
	return out
}

func mergeV2LocalUsage(existing, incoming []client.AttributionV2LocalUsageBucket) []client.AttributionV2LocalUsageBucket {
	values := make(map[string]*client.AttributionV2LocalUsageBucket, len(existing)+len(incoming))
	for _, usage := range append(append([]client.AttributionV2LocalUsageBucket(nil), existing...), incoming...) {
		key := strings.TrimSpace(usage.RequestedModel) + "\x00" + usage.BucketStartUTC.UTC().Format(time.RFC3339)
		current := values[key]
		if current == nil || localUsageContains(usage, *current) {
			copy := usage
			copy.RequestedModel = strings.TrimSpace(copy.RequestedModel)
			copy.BucketStartUTC = copy.BucketStartUTC.UTC()
			values[key] = &copy
		}
	}
	return sortedV2LocalUsage(values)
}

func localUsageContains(left, right client.AttributionV2LocalUsageBucket) bool {
	return left.InputTokens >= right.InputTokens && left.OutputTokens >= right.OutputTokens &&
		left.CacheCreationTokens >= right.CacheCreationTokens && left.CacheReadTokens >= right.CacheReadTokens &&
		left.TotalTokens >= right.TotalTokens && left.CreditUsage >= right.CreditUsage && left.RequestCount >= right.RequestCount
}

func v2LocalUsageDigest(values []client.AttributionV2LocalUsageBucket) string {
	parts := make([]string, 0, len(values))
	for _, usage := range mergeV2LocalUsage(nil, values) {
		parts = append(parts, strings.Join([]string{
			usage.RequestedModel, usage.BucketStartUTC.UTC().Format(time.RFC3339),
			fmt.Sprintf("%d", usage.InputTokens), fmt.Sprintf("%d", usage.OutputTokens),
			fmt.Sprintf("%d", usage.CacheCreationTokens), fmt.Sprintf("%d", usage.CacheReadTokens),
			fmt.Sprintf("%d", usage.TotalTokens), fmt.Sprintf("%g", usage.CreditUsage), fmt.Sprintf("%d", usage.RequestCount),
		}, "\x00"))
	}
	return claimDigest(parts...)
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
	if len(allocations) == 1 {
		return allocations[0].EvidenceDigest
	}
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
