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
	"file_path", "file_text", "old_string", "new_string", "old_str", "new_str",
	"content", "path",
}

// PilotScanOptions selects the Pilot output to read and carries the commit the
// scan is binding against.
type PilotScanOptions struct {
	V2ClaimScanOptions
	// OutputDir defaults to the Pilot local JSONL directory.
	OutputDir string
	// WorkspaceSessionIDs names the Codex sessions already known to belong to
	// the scanned repository. Pilot's Codex tailer starts at the end of an
	// existing session file and never reads the session_meta line carrying
	// cwd, so its Codex events name no workspace at all and every one of them
	// would be dropped as unscoped. The session identity is what is left, and
	// Codex's own session files still carry the cwd that binds it.
	//
	// Empty means no fallback: an event that names no workspace stays
	// unscoped, which is the correct answer for an agent whose sessions this
	// machine cannot independently place.
	WorkspaceSessionIDs map[string]struct{}
}

// PilotScanResult separates the two surfaces a scan produces: claims that bind
// Token to a commit, and usage events that account for consumption whether or
// not anything was committed.
type PilotScanResult struct {
	Claims []V2ClaimCandidate
	Usage  []LocalToolUsageEvent
	// UnidentifiedResponses counts usage-bearing responses Pilot reported with
	// no gen_ai.response.id. Their consumption is still counted, once per
	// physical event, but it cannot be deduplicated against a replay of the same
	// response — so the count is reported rather than left invisible.
	UnidentifiedResponses int
	// UnscopedRecords counts events that named no workspace at all. They cannot
	// be attributed to the repository being scanned, so they are excluded rather
	// than assumed, and counted so the exclusion is visible.
	UnscopedRecords int
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

// pilotTurn accumulates every event sharing one gen_ai.turn.id. A turn is the
// unit of commit binding only: usage is accounted per response instead, because
// gen_ai.turn.id is not a native identifier — see pilotResponse.
type pilotTurn struct {
	agentType           string
	sessionID           string
	turnID              string
	model               string
	mutations           []v2Mutation
	replayFiles         map[string]v2ReplayFile
	unrecognizedWrapper bool
	unhandledTools      map[string]struct{}
	source              string
	firstSeen           time.Time
	order               int
}

// pilotResponse is one native model response, the unit usage is accounted in.
//
// Pilot derives gen_ai.turn.id for Claude Code and Kiro CLI as
// `<gen_ai.session.id>:t<N>`, where `t<N>` is a counter the collector maintains
// rather than anything the agent reports. Resuming a session gives Pilot a new
// session id and restarts that counter while the agent replays the same
// transcript, so one native response reappears under a second turn id carrying
// the usage it already reported. Summing per turn counted that consumption
// twice; on the machine this was diagnosed against, 59 responses were replayed
// and inflated the Token total by 9.2%.
//
// gen_ai.response.id is native and stable for every agent Pilot covers
// (`msg_011…` from Anthropic, `msg_0ba…`/`rs_0ba…` from OpenAI, a UUID from
// Kiro), so it is the identity usage is deduplicated on — both inside one scan
// and, through DedupeKey, across scans and across machines.
type pilotResponse struct {
	key              string
	agentType        string
	sessionID        string
	turnID           string
	responseID       string
	model            string
	usage            pilotUsage
	credit           float64
	creditSeen       bool
	tokenUnavailable bool
	// observedAt is when the consumption happened, taken from the event's own
	// time rather than from when the collector saw it: on a replay the
	// observation time is "now" while the consumption may be days old, and the
	// checkpoint-window binding would then attach old usage to a new commit.
	observedAt time.Time
	sourcePath string
	sourceLine int
}

type pilotUsage struct {
	input         int64
	output        int64
	cacheRead     int64
	cacheCreation int64
	reasoning     int64
	seen          bool
}

// uncachedInput is the input Token that was actually re-read for this response.
//
// Pilot normalizes gen_ai.usage.input_tokens to the whole input, cache included,
// for every agent: a Claude response reporting 46024 read plus 22823 created
// arrives as 68849 input. The usage surface keeps the cached part in its own
// field, so returning the raw value would count every cached Token twice — on
// this machine that inflated one repository's Claude total by roughly eight
// times.
//
// Subtracting makes the four Token fields disjoint for every agent, so their sum
// is the consumption. That is a change of split, not of total, and it also ends
// a disagreement between the two per-agent readers this source replaces: the
// Claude reader already reported input and cache as disjoint, while the Codex
// reader carried Codex's own convention, where the cached Token are part of the
// input count and any sum of the two double counts them.
//
// The floor is not defensive tidiness. It is what keeps a normalization change
// upstream — a cache field that stops being part of the input total — from
// turning into negative consumption.
func (u pilotUsage) uncachedInput() int64 {
	uncached := u.input - u.cacheRead - u.cacheCreation
	if uncached < 0 {
		return 0
	}
	return uncached
}

// cachedInput is every input Token served from cache, whether read from an
// existing entry or written into a new one. The Claude reader this replaces
// already sums the two, and the usage surface has one field for both.
func (u pilotUsage) cachedInput() int64 {
	return u.cacheRead + u.cacheCreation
}

// earlierThan orders two occurrences of the same response by where they were
// written, not by when the scan happened to read them. Pilot names its output
// `<agent>-<date>.jsonl`, so the smaller (path, line) pair is the earlier
// occurrence: the original run rather than the replay.
func (r *pilotResponse) earlierThan(other *pilotResponse) bool {
	if r.sourcePath != other.sourcePath {
		return r.sourcePath < other.sourcePath
	}
	return r.sourceLine < other.sourceLine
}

// pilotEventObservedAt reads the moment an event describes. Pilot writes
// nanoseconds since the epoch as a string. A missing or unparsable value yields
// the zero time, which the backend reads as "no observation" and refuses to
// bind — the correct outcome, since a time may never be invented.
func pilotEventObservedAt(event pilotEvent) time.Time {
	nanos := asInt64(event.attrs["time_unix_nano"])
	if nanos <= 0 {
		return time.Time{}
	}
	return time.Unix(0, nanos).UTC()
}

// pilotEventInScope reports whether an event belongs to the repository being
// scanned. Pilot writes every workspace into one file per agent, so a scan that
// did not filter would count another repository's consumption — on the machine
// this was measured against, 41.6% of the Token in the local output belonged to
// other workspaces, one of them a parent directory of the scanned repository.
//
// This uses the workspace only to decide which records are in scope. It never
// decides which commit a record binds to: that stays a content proof for agents
// that support one, and the backend's checkpoint window for those that do not.
func pilotEventInScope(event pilotEvent, repoRoot string) (inScope bool, named bool) {
	workspace := event.str("workspace.current_root")
	if workspace == "" {
		workspace = event.str("workspace.path")
	}
	if workspace == "" {
		return false, false
	}
	if strings.TrimSpace(repoRoot) == "" {
		return true, true
	}
	return sameWorkspacePathOrGitCommon(workspace, repoRoot), true
}

// pilotEventInWorkspaceSession places an event Pilot left unscoped, using the
// session identity the agent's own local files still bind to a workspace.
//
// This exists for one upstream defect: Pilot's Codex tailer opens an existing
// session file at its end, so it never reads the session_meta line carrying
// cwd, and every event from that session arrives with no workspace. Measured on
// one machine, that was every Codex session — including one started three days
// after Pilot was installed — so "start a new session" is not a workaround.
//
// It is deliberately narrow. Only Codex is placed this way, because only Codex
// has session files this repository already knows how to read; the identity
// must match exactly; and an unknown session stays unscoped rather than being
// assumed to belong to the repository being scanned.
func pilotEventInWorkspaceSession(event pilotEvent, sessions map[string]struct{}) bool {
	if len(sessions) == 0 {
		return false
	}
	if event.str("gen_ai.agent.type") != pilotCodexAgentType {
		return false
	}
	sessionID := strings.TrimSpace(event.str("gen_ai.session.id"))
	if sessionID == "" {
		return false
	}
	_, ok := sessions[sessionID]
	return ok
}

// pilotCodexAgentType is how Pilot names Codex in gen_ai.agent.type.
const pilotCodexAgentType = "codex"

// CodexWorkspaceSessionIDs resolves which Codex sessions belong to one
// repository, by reading the session_meta line each Codex session file opens
// with. It is the input to the fallback above, and returns nothing when Codex
// has no session files this machine can read.
func CodexWorkspaceSessionIDs(ctx context.Context, homeDir, workspaceRoot string) map[string]struct{} {
	if strings.TrimSpace(workspaceRoot) == "" {
		return nil
	}
	if strings.TrimSpace(homeDir) == "" {
		resolved, err := os.UserHomeDir()
		if err != nil {
			return nil
		}
		homeDir = resolved
	}
	result := map[string]struct{}{}
	for _, path := range findCodexJSONLFiles(workspaceRoot, homeDir) {
		if ctx.Err() != nil {
			return result
		}
		ids, err := findCodexWorkspaceSessionIDsContext(ctx, path, workspaceRoot)
		if err != nil {
			continue
		}
		for _, id := range ids {
			if id = strings.TrimSpace(id); id != "" {
				result[id] = struct{}{}
			}
		}
	}
	return result
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

	var result PilotScanResult
	turns := map[string]*pilotTurn{}
	responses := map[string]*pilotResponse{}
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
			inScope, named := pilotEventInScope(event, opts.RepoRoot)
			if !named {
				if !pilotEventInWorkspaceSession(event, opts.WorkspaceSessionIDs) {
					result.UnscopedRecords++
					continue
				}
				inScope = true
			}
			if !inScope {
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
			applyPilotResponse(responses, turn, event)
		}
	}

	ordered := make([]*pilotTurn, 0, len(turns))
	for _, turn := range turns {
		ordered = append(ordered, turn)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].order < ordered[j].order })

	for _, response := range orderedPilotResponses(responses) {
		if response.responseID == "" {
			result.UnidentifiedResponses++
		}
		result.Usage = append(result.Usage, pilotUsageEvent(response, opts))
	}
	// Claims are priced from the same deduplicated responses the usage surface
	// reports, grouped by the turn that performed the mutation. Pricing from the
	// raw events instead would re-count every response a resume replayed.
	byTurn := map[string][]*pilotResponse{}
	for _, response := range orderedPilotResponses(responses) {
		byTurn[response.turnID] = append(byTurn[response.turnID], response)
	}
	for _, turn := range ordered {
		if claim, ok := pilotClaimCandidate(ctx, turn, byTurn[turn.turnID], opts); ok {
			result.Claims = append(result.Claims, claim)
		}
	}
	return result, nil
}

// orderedPilotResponses sorts the deduplicated responses by where they were
// written. Map iteration order is never allowed to reach the output.
func orderedPilotResponses(responses map[string]*pilotResponse) []*pilotResponse {
	ordered := make([]*pilotResponse, 0, len(responses))
	for _, response := range responses {
		ordered = append(ordered, response)
	}
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].earlierThan(ordered[j]) {
			return true
		}
		if ordered[j].earlierThan(ordered[i]) {
			return false
		}
		return ordered[i].key < ordered[j].key
	})
	return ordered
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

	if event.str("event.name") == "tool.call" {
		applyPilotToolCall(ctx, turn, event, opts)
	}
}

// applyPilotResponse records what one llm.response reported, keyed on the native
// response id so a replayed response is recognised as the one it already is.
//
// Two occurrences of the same response are never summed. The occurrence that
// wins is the earlier one by (source file, line), and it supplies both the
// numbers and the turn the usage is attributed to; the later occurrence
// contributes nothing. Refusing to sum is what makes this fail closed: a
// replayed response can only ever be counted at the value Pilot reported for
// it, never at a multiple of it.
func applyPilotResponse(responses map[string]*pilotResponse, turn *pilotTurn, event pilotEvent) {
	if event.str("event.name") != "llm.response" {
		return
	}
	current := pilotResponseUsage(turn, event)
	if current == nil {
		return
	}
	existing, ok := responses[current.key]
	if !ok || current.earlierThan(existing) {
		responses[current.key] = current
	}
}

// pilotResponseUsage reads one llm.response into a pilotResponse, or reports nil
// when the event carried no usage at all.
func pilotResponseUsage(turn *pilotTurn, event pilotEvent) *pilotResponse {
	response := &pilotResponse{
		agentType:  turn.agentType,
		sessionID:  turn.sessionID,
		turnID:     turn.turnID,
		responseID: event.str("gen_ai.response.id"),
		model:      firstNonEmpty(event.str("gen_ai.response.model"), event.str("gen_ai.request.model")),
		observedAt: pilotEventObservedAt(event),
		sourcePath: event.path,
		sourceLine: event.line,
	}
	for key, target := range map[string]*int64{
		"gen_ai.usage.input_tokens":                &response.usage.input,
		"gen_ai.usage.output_tokens":               &response.usage.output,
		"gen_ai.usage.cache_read.input_tokens":     &response.usage.cacheRead,
		"gen_ai.usage.cache_creation.input_tokens": &response.usage.cacheCreation,
		"gen_ai.usage.reasoning_output_tokens":     &response.usage.reasoning,
	} {
		if _, present := event.attrs[key]; present {
			*target = event.i64(key)
			response.usage.seen = true
		}
	}
	// Kiro reports credit and declares Token unavailable. Credit is a
	// tool-specific attribute outside Pilot's normalized schema, so it is read
	// by name and never mixed into a Token total. It rides on the same
	// llm.response event as the response id, so deduplicating the response
	// deduplicates the credit: replay inflates both, and one identity fixes both.
	if _, present := event.attrs["kiro.credit_cost"]; present {
		response.credit = event.f64("kiro.credit_cost")
		response.creditSeen = true
	}
	if event.str("kiro.token_source") == "unavailable" {
		response.tokenUnavailable = true
	}
	if !response.usage.seen && !response.creditSeen {
		return nil
	}
	response.key = pilotResponseKey(response)
	return response
}

// pilotResponseKey is the identity usage is accounted under.
//
// A response Pilot reported with no gen_ai.response.id falls back to its own
// location in Pilot's output. That counts it exactly once per physical event —
// it is neither dropped, which would understate real consumption, nor merged
// with a neighbouring response, which would lose it. What it cannot do is
// recognise a replay of itself, so the scan reports how many such responses it
// saw in PilotScanResult.UnidentifiedResponses. The two forms live in separate
// namespaces so a synthetic key can never collide with a native response id.
func pilotResponseKey(response *pilotResponse) string {
	if response.responseID != "" {
		return "pilot:" + response.agentType + ":response:" + response.responseID
	}
	return "pilot:" + response.agentType + ":event:" + pilotResponseEventID(response)
}

// pilotResponseEventID names the response for the usage surface. With no native
// id it falls back to the event's own location, the way the Codex session file
// source already does — the `line:` prefix keeps a synthesized name plainly
// distinguishable from a native response id.
func pilotResponseEventID(response *pilotResponse) string {
	if response.responseID != "" {
		return response.responseID
	}
	return fmt.Sprintf("line:%s#%d", filepath.Base(response.sourcePath), response.sourceLine)
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
	case "Edit":
		// Edit always names one file, so a call this source cannot replay is a
		// refusal rather than a miss: it appends a mutation with no hash, which
		// gaps the turn instead of letting it disappear.
		turn.mutations = append(turn.mutations, pilotEditMutation(ctx, arguments, opts, turn.replayFiles))
	case "fs_write":
		if mutation, handled := pilotFsWriteMutation(ctx, arguments, opts, turn.replayFiles); handled {
			turn.mutations = append(turn.mutations, mutation)
			return
		}
		// fs_write commands this source does not replay fall through to the
		// same coverage counter as an entirely unknown tool.
		if pilotArgumentsCarryMutation(arguments) {
			turn.unhandledTools[event.str("gen_ai.tool.name")] = struct{}{}
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

// pilotEditMutation replays a Claude Code Edit call. Unlike Write, Edit carries
// the replacement rather than the result, so the post-state has to be rebuilt
// from the file the turn has reached so far.
//
// The argument shape is the one the installed Claude Code build declares for
// Edit: `file_path`, `old_string`, `new_string` and `replace_all` (default
// false), with the documented contract that the edit fails unless `old_string`
// is unique or `replace_all` is set. This source mirrors that contract, because
// a call Claude itself would have rejected changed nothing.
//
// A refusal returns a mutation with no hash. That is the same marker
// v2PatchMutations emits for a patch block it cannot apply: it fails the turn's
// mutations closed and surfaces as a gap, rather than vanishing.
func pilotEditMutation(ctx context.Context, arguments string, opts PilotScanOptions, replayFiles map[string]v2ReplayFile) v2Mutation {
	var payload struct {
		FilePath   string `json:"file_path"`
		OldString  string `json:"old_string"`
		NewString  string `json:"new_string"`
		ReplaceAll any    `json:"replace_all"`
	}
	if err := json.Unmarshal([]byte(arguments), &payload); err != nil {
		return v2Mutation{kind: "update"}
	}
	path := canonicalClaimPath(opts.RepoRoot, payload.FilePath)
	if path == "" {
		return v2Mutation{kind: "update"}
	}
	return pilotReplaceMutation(ctx, opts, replayFiles, path,
		payload.OldString, payload.NewString, pilotBoolArgument(payload.ReplaceAll))
}

// pilotFsWriteMutation replays a Kiro CLI fs_write call. The second value
// reports whether this source claims the command at all; commands it does not
// claim are counted as a coverage gap by the caller instead.
//
// The argument shape is the one the installed `kiro-cli-chat` binary declares
// for fs_write: `command` is one of `create`, `str_replace`, `insert` or
// `append`, with `path` alongside `file_text` (create), `old_str`/`new_str`
// (str_replace), `new_str` (append) or `insert_line`/`new_str` (insert).
//
// Only `create` and `str_replace` are replayed. Their post-state follows from
// the arguments alone. `insert` and `append` do not: both normalize newlines
// around the inserted text, and this source has no primary source pinning that
// byte-exactly for this build, so guessing would either mis-hash or invent
// evidence. They go to the unhandled-tool counter, where the gap stays visible.
//
// Known model gap, out of scope here: Kiro bills in credit, not Token, and
// AttributionV2ClaimGroup carries no credit field. A Kiro claim can therefore
// bind a mutation to a commit but has nowhere to record what that turn cost;
// the credit reaches the usage surface only, through pilotUsageEvent.
func pilotFsWriteMutation(ctx context.Context, arguments string, opts PilotScanOptions, replayFiles map[string]v2ReplayFile) (v2Mutation, bool) {
	var payload struct {
		Command  string `json:"command"`
		Path     string `json:"path"`
		FileText string `json:"file_text"`
		OldStr   string `json:"old_str"`
		NewStr   string `json:"new_str"`
	}
	if err := json.Unmarshal([]byte(arguments), &payload); err != nil {
		return v2Mutation{}, false
	}
	command := strings.TrimSpace(payload.Command)
	if command != "create" && command != "str_replace" {
		return v2Mutation{}, false
	}
	path := canonicalClaimPath(opts.RepoRoot, payload.Path)
	if path == "" {
		return v2Mutation{kind: "update"}, true
	}
	if command == "str_replace" {
		// fs_write has no replace_all: its own description refuses a non-unique
		// old_str outright, so a single occurrence is always required.
		return pilotReplaceMutation(ctx, opts, replayFiles, path, payload.OldStr, payload.NewStr, false), true
	}
	// `create` writes the whole file, so the content is already the post-state.
	kind := "add"
	file, loaded := replayFiles[path]
	if !loaded {
		parent, err := gitShowClaimFile(ctx, opts.RepoRoot, strings.TrimSpace(opts.CommitSHA)+"^", path)
		file = v2ReplayFile{content: string(parent), exists: err == nil}
	}
	if file.exists {
		kind = "update"
	}
	replayFiles[path] = v2ReplayFile{content: payload.FileText, exists: true}
	return v2Mutation{path: path, hash: claimDigest(payload.FileText), kind: kind}, true
}

// pilotReplaceMutation is the shared exact-string replacement replay behind
// Claude Code's Edit and Kiro CLI's fs_write str_replace. Both refuse the same
// three ways, and refusing returns a mutation with no hash so the turn fails
// closed and stays countable.
func pilotReplaceMutation(ctx context.Context, opts PilotScanOptions, replayFiles map[string]v2ReplayFile, path, oldString, newString string, replaceAll bool) v2Mutation {
	refused := v2Mutation{path: path, kind: "update"}
	if oldString == "" {
		return refused
	}
	file, loaded := replayFiles[path]
	if !loaded {
		parent, err := gitShowClaimFile(ctx, opts.RepoRoot, strings.TrimSpace(opts.CommitSHA)+"^", path)
		file = v2ReplayFile{content: string(parent), exists: err == nil}
	}
	if !file.exists {
		return refused
	}
	occurrences := strings.Count(file.content, oldString)
	if occurrences == 0 || (occurrences > 1 && !replaceAll) {
		return refused
	}
	expected := strings.Replace(file.content, oldString, newString, 1)
	if replaceAll {
		expected = strings.ReplaceAll(file.content, oldString, newString)
	}
	replayFiles[path] = v2ReplayFile{content: expected, exists: true}
	return v2Mutation{path: path, hash: claimDigest(expected), kind: "update"}
}

// pilotBoolArgument reads a boolean tool argument. Pilot passes tool arguments
// through verbatim from each agent, and a flag can therefore arrive as a JSON
// boolean or as a stringified one ("True"/"False"), so both are accepted and
// anything that is not explicitly true is false.
func pilotBoolArgument(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "true", "1", "yes":
			return true
		}
	}
	return false
}

// pilotUsageEvent reports one native response's consumption.
//
// ToolEventID and DedupeKey both carry the response identity rather than the
// turn id, so the same event survives a resume: a later scan that only still has
// the resumed file reports the same DedupeKey the original scan did, and the
// server counts it once. Keying on the turn id could not do that — the turn id
// changes on every resume. This matches what the Codex and Claude Code session
// file sources already do, both of which key usage on the native response id.
//
// The turn the usage is attributed to is kept in RawSourceLocator, alongside the
// session and file the winning occurrence came from.
func pilotUsageEvent(response *pilotResponse, opts PilotScanOptions) LocalToolUsageEvent {
	unit := UsageUnitToken
	if response.creditSeen && !response.usage.seen {
		unit = UsageUnitCredit
	}
	return LocalToolUsageEvent{
		Tool:              pilotToolName(response.agentType),
		WorkspaceID:       strings.TrimSpace(opts.WorkspaceID),
		RepoConfigID:      opts.RepoConfigID,
		RepoKey:           strings.TrimSpace(opts.RepoKey),
		ToolSessionID:     response.sessionID,
		ToolEventID:       pilotResponseEventID(response),
		DedupeKey:         response.key,
		RequestCount:      1,
		UsageUnit:         unit,
		InputTokens:       response.usage.uncachedInput(),
		OutputTokens:      response.usage.output,
		CachedInputTokens: response.usage.cachedInput(),
		ReasoningTokens:   response.usage.reasoning,
		CreditUsage:       response.credit,
		ObservedStartAt:   response.observedAt,
		ObservedEndAt:     response.observedAt,
		RawSourcePath:     response.sourcePath,
		RawSourceLocator:  "turn:" + response.turnID,
	}
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
func pilotClaimCandidate(ctx context.Context, turn *pilotTurn, responses []*pilotResponse, opts PilotScanOptions) (V2ClaimCandidate, bool) {
	if len(turn.mutations) == 0 && !turn.unrecognizedWrapper && len(turn.unhandledTools) == 0 {
		return V2ClaimCandidate{}, false
	}
	candidate := V2ClaimCandidate{
		LocalKey:    claimDigest(turn.sessionID, turn.turnID),
		Source:      turn.source,
		FirstSeenAt: turn.firstSeen,
		Group: client.AttributionV2ClaimGroup{
			SchemaVersion:   v2ClaimSchemaVersion,
			RelayProviderID: opts.RelayProviderID,
			// Every agent Pilot covers is priced from what Pilot observed, not
			// from the relay. Only Codex routes through the relay at all, and
			// pricing one agent from a source the other two structurally cannot
			// have would leave one commit carrying two kinds of number.
			TokenSource: client.AttributionV2TokenSourceCodexLocal,
			ThreadID:    turn.sessionID,
			TurnID:      turn.turnID,
			LocalUsage:  pilotLocalUsage(responses),
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
	// The backend derives the request count from the buckets, so it is not sent.
	candidate.Group.GroupID = pilotClaimGroupID(turn, opts, candidate.Group.EvidenceDigest)
	return candidate, true
}

// pilotClaimGroupID names a claim by what it proves rather than by where it was
// observed.
//
// Deriving it from the session and turn was safe while only Codex produced
// claims: Codex reports both natively and Pilot passes them through unchanged.
// It is not safe for Claude Code, which has no turn concept at all — Pilot
// numbers the turns itself, and a resumed session gets a new session id and a
// restarted counter while the agent replays the same work. On this machine 190
// of 1318 responses already appear under two turn ids. Named that way, one piece
// of work would arrive as two groups and be counted twice.
//
// The commit and the evidence digest are both content-derived, so the same work
// always names the same group however the collector happened to segment it.
// Turns that prove nothing keep the observational name: they are not delivered,
// and they have no evidence to be named by.
func pilotClaimGroupID(turn *pilotTurn, opts PilotScanOptions, evidenceDigest string) string {
	provider := fmt.Sprintf("%d", opts.RelayProviderID)
	if strings.TrimSpace(evidenceDigest) == "" {
		return claimDigest(provider, turn.sessionID, turn.turnID)
	}
	return claimDigest(provider, opts.CommitSHA, evidenceDigest)
}

// pilotLocalUsage prices a claim from the responses that produced it.
//
// The backend aggregates local usage into quarter-hour buckets per model and
// rejects a bucket start that is not aligned to one, so the alignment happens
// here rather than being discovered as a rejection later.
func pilotLocalUsage(responses []*pilotResponse) []client.AttributionV2LocalUsageBucket {
	if len(responses) == 0 {
		return nil
	}
	type key struct {
		model  string
		bucket time.Time
	}
	order := make([]key, 0, len(responses))
	buckets := map[key]*client.AttributionV2LocalUsageBucket{}
	for _, response := range responses {
		// A response with no observation time cannot be placed in a bucket, and
		// inventing one would attribute its cost to an arbitrary quarter hour.
		if response == nil || response.observedAt.IsZero() || (!response.usage.seen && !response.creditSeen) {
			continue
		}
		k := key{model: strings.TrimSpace(response.model), bucket: response.observedAt.UTC().Truncate(15 * time.Minute)}
		bucket, ok := buckets[k]
		if !ok {
			bucket = &client.AttributionV2LocalUsageBucket{RequestedModel: k.model, BucketStartUTC: k.bucket}
			buckets[k] = bucket
			order = append(order, k)
		}
		bucket.InputTokens += response.usage.uncachedInput()
		bucket.OutputTokens += response.usage.output
		bucket.CacheCreationTokens += response.usage.cacheCreation
		bucket.CacheReadTokens += response.usage.cacheRead
		bucket.CreditUsage += response.credit
		bucket.RequestCount++
	}
	out := make([]client.AttributionV2LocalUsageBucket, 0, len(order))
	for _, k := range order {
		bucket := buckets[k]
		bucket.TotalTokens = bucket.InputTokens + bucket.OutputTokens + bucket.CacheCreationTokens + bucket.CacheReadTokens
		out = append(out, *bucket)
	}
	return out
}
