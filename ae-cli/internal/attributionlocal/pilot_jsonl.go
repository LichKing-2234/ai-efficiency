package attributionlocal

// LoongSuite Pilot source (proof of concept).
//
// Pilot is a local collector that installs hooks into each supported AI coding
// agent, normalizes their activity into one `gen_ai.*` event schema, and writes
// it to `~/.loongsuite-pilot/logs/output/<agent>-<date>.jsonl`. Reading that one
// place replaces a parser per agent.
//
// Pilot supplies turn identity, Token or credit usage, and the structured file
// mutation a turn performed. It never supplies a commit: its schema has no
// commit, revision, or checkpoint field, and it does not need one. The commit
// comes from the Git post-commit hook, and the binding proof comes from
// replaying the mutation against that commit's trees — the same deterministic,
// fail-closed machinery the Codex session-file path already uses.
//
// The mutation is the one part Pilot does not normalize: tool call arguments
// pass through verbatim, so their shape is per-tool. Codex carries a generated
// apply_patch wrapper; Claude Code carries a file path plus resulting content.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ai-efficiency/ae-cli/internal/client"
)

const (
	pilotAgentCodex  = "codex"
	pilotAgentClaude = "claude-code"
	pilotAgentKiro   = "kiro-cli"

	// pilotGapUnhandledMutationTool marks a turn that plainly changed a file
	// through a tool this source does not yet extract. Without it such a turn
	// would produce no claim and no reason at all, which is exactly how Codex
	// wrapper drift stayed invisible through a previous repair.
	pilotGapUnhandledMutationTool = "unhandled_mutation_tool"
)

// pilotMutationArgumentKeys are the argument names that mark a tool call as
// carrying a file mutation. Like the apply_patch hint, this detector is
// deliberately loose: it only decides whether a coverage gap is worth
// reporting, and can never authorise an allocation.
var pilotMutationArgumentKeys = []string{
	"file_path", "file_text", "old_string", "new_string", "content", "path",
}

// PilotScanOptions selects the Pilot output to read and carries the commit the
// scan is binding against.
type PilotScanOptions struct {
	V2ClaimScanOptions
	// OutputDir defaults to the Pilot local JSONL directory.
	OutputDir string
}

// PilotScanResult separates the two surfaces a scan produces: claims that bind
// Token to a commit, and usage events that account for consumption whether or
// not anything was committed.
type PilotScanResult struct {
	Claims []V2ClaimCandidate
	Usage  []LocalToolUsageEvent
}

// DefaultPilotOutputDir is where Pilot writes local JSONL by default.
func DefaultPilotOutputDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".loongsuite-pilot", "logs", "output")
}

// pilotEvent is one normalized Pilot record. Attributes may sit at the top level
// or under "attributes" depending on the writer, so both are accepted.
type pilotEvent struct {
	attrs map[string]any
	path  string
	line  int
}

func (e pilotEvent) str(key string) string  { return strings.TrimSpace(asString(e.attrs[key])) }
func (e pilotEvent) i64(key string) int64   { return asInt64(e.attrs[key]) }
func (e pilotEvent) f64(key string) float64 { return asFloat64(e.attrs[key]) }

// pilotTurn accumulates every event sharing one gen_ai.turn.id.
type pilotTurn struct {
	agentType           string
	sessionID           string
	turnID              string
	responseID          string
	model               string
	usage               pilotUsage
	credit              float64
	creditSeen          bool
	tokenUnavailable    bool
	mutations           []v2Mutation
	replayFiles         map[string]v2ReplayFile
	unrecognizedWrapper bool
	unhandledTools      map[string]struct{}
	source              string
	firstSeen           time.Time
	order               int
}

type pilotUsage struct {
	input     int64
	output    int64
	cacheRead int64
	reasoning int64
	seen      bool
}

// ScanPilotClaims reads Pilot's normalized local output and produces commit-bound
// claims plus usage events for every agent it covers.
func ScanPilotClaims(ctx context.Context, opts PilotScanOptions) (PilotScanResult, error) {
	dir := strings.TrimSpace(opts.OutputDir)
	if dir == "" {
		dir = DefaultPilotOutputDir()
	}
	if dir == "" {
		return PilotScanResult{}, fmt.Errorf("pilot output directory is unknown")
	}
	files, err := pilotOutputFiles(dir)
	if err != nil {
		return PilotScanResult{}, err
	}

	turns := map[string]*pilotTurn{}
	var order int
	for _, path := range files {
		events, err := readPilotEvents(path)
		if err != nil {
			return PilotScanResult{}, err
		}
		for _, event := range events {
			turnID := event.str("gen_ai.turn.id")
			if turnID == "" {
				continue
			}
			turn, ok := turns[turnID]
			if !ok {
				turn = &pilotTurn{
					turnID:         turnID,
					replayFiles:    map[string]v2ReplayFile{},
					unhandledTools: map[string]struct{}{},
					source:         path,
					order:          order,
				}
				order++
				turns[turnID] = turn
			}
			applyPilotEvent(ctx, turn, event, opts)
		}
	}

	ordered := make([]*pilotTurn, 0, len(turns))
	for _, turn := range turns {
		ordered = append(ordered, turn)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].order < ordered[j].order })

	var result PilotScanResult
	for _, turn := range ordered {
		if usage, ok := pilotUsageEvent(turn, opts); ok {
			result.Usage = append(result.Usage, usage)
		}
		if claim, ok := pilotClaimCandidate(ctx, turn, opts); ok {
			result.Claims = append(result.Claims, claim)
		}
	}
	return result, nil
}

func pilotOutputFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var files []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		files = append(files, filepath.Join(dir, entry.Name()))
	}
	sort.Strings(files)
	return files, nil
}

func readPilotEvents(path string) ([]pilotEvent, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var events []pilotEvent
	for index, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var raw map[string]any
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			// A malformed line is skipped rather than failing the scan: Pilot
			// appends concurrently and a partial trailing line is expected.
			continue
		}
		attrs := raw
		if nested, ok := raw["attributes"].(map[string]any); ok {
			attrs = nested
		}
		events = append(events, pilotEvent{attrs: attrs, path: path, line: index + 1})
	}
	return events, nil
}

func applyPilotEvent(ctx context.Context, turn *pilotTurn, event pilotEvent, opts PilotScanOptions) {
	if turn.agentType == "" {
		turn.agentType = event.str("gen_ai.agent.type")
	}
	if turn.sessionID == "" {
		turn.sessionID = event.str("gen_ai.session.id")
	}
	if turn.model == "" {
		turn.model = event.str("gen_ai.request.model")
	}

	switch event.str("event.name") {
	case "llm.response":
		if id := event.str("gen_ai.response.id"); id != "" {
			turn.responseID = id
		}
		applyPilotUsage(turn, event)
	case "tool.call":
		applyPilotToolCall(ctx, turn, event, opts)
	}
}

func applyPilotUsage(turn *pilotTurn, event pilotEvent) {
	for key, target := range map[string]*int64{
		"gen_ai.usage.input_tokens":            &turn.usage.input,
		"gen_ai.usage.output_tokens":           &turn.usage.output,
		"gen_ai.usage.cache_read.input_tokens": &turn.usage.cacheRead,
		"gen_ai.usage.reasoning_output_tokens": &turn.usage.reasoning,
	} {
		if _, present := event.attrs[key]; present {
			*target += event.i64(key)
			turn.usage.seen = true
		}
	}
	// Kiro reports credit and declares Token unavailable. Credit is a
	// tool-specific attribute outside Pilot's normalized schema, so it is read
	// by name and never mixed into a Token total.
	if _, present := event.attrs["kiro.credit_cost"]; present {
		turn.credit += event.f64("kiro.credit_cost")
		turn.creditSeen = true
	}
	if event.str("kiro.token_source") == "unavailable" {
		turn.tokenUnavailable = true
	}
}

// applyPilotToolCall extracts the structured mutation a tool call performed.
// This is the one per-tool branch: Pilot passes tool arguments through verbatim
// rather than normalizing them.
func applyPilotToolCall(ctx context.Context, turn *pilotTurn, event pilotEvent, opts PilotScanOptions) {
	arguments := pilotToolArguments(event)
	if arguments == "" {
		return
	}
	switch event.str("gen_ai.tool.name") {
	case "exec", "shell", "apply_patch", "container.exec":
		patch, unrecognized := v2StructuredPatchInput(event.str("gen_ai.tool.name"), arguments, "")
		if patch != "" {
			turn.mutations = append(turn.mutations,
				v2PatchMutations(ctx, patch, opts.RepoRoot, opts.CommitSHA, turn.replayFiles)...)
			return
		}
		if unrecognized {
			turn.unrecognizedWrapper = true
		}
	case "Write":
		if mutation, ok := pilotWriteMutation(ctx, arguments, opts, turn.replayFiles); ok {
			turn.mutations = append(turn.mutations, mutation)
		}
	default:
		if pilotArgumentsCarryMutation(arguments) {
			turn.unhandledTools[event.str("gen_ai.tool.name")] = struct{}{}
		}
	}
}

// pilotArgumentsCarryMutation reports whether a tool call's arguments name a
// file and its new contents. It is a coverage detector, not an acceptor.
func pilotArgumentsCarryMutation(arguments string) bool {
	var payload map[string]any
	if err := json.Unmarshal([]byte(arguments), &payload); err != nil {
		return false
	}
	named := false
	valued := false
	for _, key := range pilotMutationArgumentKeys {
		value, present := payload[key]
		if !present || strings.TrimSpace(asString(value)) == "" {
			continue
		}
		switch key {
		case "file_path", "path":
			named = true
		default:
			valued = true
		}
	}
	return named && valued
}

func pilotToolArguments(event pilotEvent) string {
	raw, present := event.attrs["gen_ai.tool.call.arguments"]
	if !present || raw == nil {
		return ""
	}
	if text, ok := raw.(string); ok {
		return text
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return ""
	}
	return string(encoded)
}

// pilotWriteMutation turns a Claude Code Write call into the same mutation shape
// a replayed patch produces. Write carries the resulting content directly, so no
// replay is needed: the content itself is the expected post-state.
func pilotWriteMutation(ctx context.Context, arguments string, opts PilotScanOptions, replayFiles map[string]v2ReplayFile) (v2Mutation, bool) {
	var payload struct {
		FilePath string `json:"file_path"`
		Content  string `json:"content"`
	}
	if err := json.Unmarshal([]byte(arguments), &payload); err != nil {
		return v2Mutation{}, false
	}
	path := canonicalClaimPath(opts.RepoRoot, payload.FilePath)
	if path == "" {
		return v2Mutation{}, false
	}
	kind := "add"
	file, loaded := replayFiles[path]
	if !loaded {
		parent, err := gitShowClaimFile(ctx, opts.RepoRoot, strings.TrimSpace(opts.CommitSHA)+"^", path)
		file = v2ReplayFile{content: string(parent), exists: err == nil}
	}
	if file.exists {
		kind = "update"
	}
	replayFiles[path] = v2ReplayFile{content: payload.Content, exists: true}
	return v2Mutation{path: path, hash: claimDigest(payload.Content), kind: kind}, true
}

func pilotUsageEvent(turn *pilotTurn, opts PilotScanOptions) (LocalToolUsageEvent, bool) {
	if !turn.usage.seen && !turn.creditSeen {
		return LocalToolUsageEvent{}, false
	}
	unit := UsageUnitToken
	if turn.creditSeen && !turn.usage.seen {
		unit = UsageUnitCredit
	}
	return LocalToolUsageEvent{
		Tool:              pilotToolName(turn.agentType),
		WorkspaceID:       strings.TrimSpace(opts.WorkspaceID),
		RepoConfigID:      opts.RepoConfigID,
		RepoKey:           strings.TrimSpace(opts.RepoKey),
		ToolSessionID:     turn.sessionID,
		ToolEventID:       turn.turnID,
		DedupeKey:         fmt.Sprintf("pilot:%s:%s", turn.agentType, turn.turnID),
		RequestCount:      1,
		UsageUnit:         unit,
		InputTokens:       turn.usage.input,
		OutputTokens:      turn.usage.output,
		CachedInputTokens: turn.usage.cacheRead,
		ReasoningTokens:   turn.usage.reasoning,
		CreditUsage:       turn.credit,
		RawSourcePath:     turn.source,
		RawSourceLocator:  "turn:" + turn.turnID,
	}, true
}

// pilotToolName maps Pilot's agent type onto the tool name the usage surface
// already uses.
func pilotToolName(agentType string) string {
	switch agentType {
	case pilotAgentClaude:
		return "claude"
	case pilotAgentKiro:
		return "kiro"
	case pilotAgentCodex:
		return "codex"
	default:
		return agentType
	}
}

// pilotClaimCandidate builds a commit-bound claim for a turn that performed a
// structured mutation. Turns with no mutation at all produce no claim: they are
// accounted for on the usage surface instead.
func pilotClaimCandidate(ctx context.Context, turn *pilotTurn, opts PilotScanOptions) (V2ClaimCandidate, bool) {
	if len(turn.mutations) == 0 && !turn.unrecognizedWrapper && len(turn.unhandledTools) == 0 {
		return V2ClaimCandidate{}, false
	}
	groupID := claimDigest(fmt.Sprintf("%d", opts.RelayProviderID), turn.sessionID, turn.turnID)
	candidate := V2ClaimCandidate{
		LocalKey:    claimDigest(turn.sessionID, turn.turnID),
		Source:      turn.source,
		FirstSeenAt: turn.firstSeen,
		Group: client.AttributionV2ClaimGroup{
			SchemaVersion:   v2ClaimSchemaVersion,
			GroupID:         groupID,
			RelayProviderID: opts.RelayProviderID,
			TokenSource:     client.AttributionV2TokenSourceRelayOfficial,
			ThreadID:        turn.sessionID,
			TurnID:          turn.turnID,
		},
	}

	switch {
	case len(turn.mutations) == 0 && turn.unrecognizedWrapper:
		candidate.GapReason = v2GapUnrecognizedPatchWrapper
	case len(turn.mutations) == 0 && len(turn.unhandledTools) > 0:
		candidate.GapReason = pilotGapUnhandledMutationTool
	case !validV2Mutations(turn.mutations):
		candidate.GapReason = "invalid_structured_mutation"
	default:
		introduced := introducedV2Mutations(ctx, opts.RepoRoot, opts.CommitSHA, turn.mutations)
		if len(introduced) == 0 {
			candidate.GapReason = "commit_content_mismatch"
			break
		}
		evidenceDigest := v2MutationDigest(introduced)
		candidate.Group.EvidenceDigest = evidenceDigest
		candidate.Group.CommitAllocations = []client.AttributionV2CommitAllocation{{
			Sequence:          1,
			RepoConfigID:      opts.RepoConfigID,
			RepoKey:           strings.TrimSpace(opts.RepoKey),
			WorkspaceID:       strings.TrimSpace(opts.WorkspaceID),
			CheckpointEventID: opts.CheckpointEventID,
			CommitSHA:         opts.CommitSHA,
			EvidenceDigest:    evidenceDigest,
		}}
	}
	return candidate, true
}
