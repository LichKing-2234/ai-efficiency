package attributionlocal

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

type CompactCodexAtom struct {
	ID             string
	ConversationID string
	ChangeSetID    string
	Model          string
	ObservedAt     time.Time
	FreshInput     int64
	CacheRead      int64
	CacheWrite     int64
	Output         int64
	Reasoning      int64
	ProviderTotal  int64
	Processed      int64
	Quality        string
	Evidence       string
}

type compactCodexSessionContext struct {
	ConversationID   string
	Model            string
	Evidence         string
	HasMeasuredJSONL bool
}

var (
	patchPathPattern = regexp.MustCompile(`(?m)^\*\*\* (?:Add|Update|Delete) File:\s*(.+?)\s*$`)
)

func ScanCompactCodexAtoms(ctx context.Context, currentRepoRoot string) ([]CompactCodexAtom, error) {
	return scanCompactCodexAtomsSince(ctx, currentRepoRoot, time.Time{})
}

func scanCompactCodexAtomsSince(ctx context.Context, currentRepoRoot string, since time.Time) ([]CompactCodexAtom, error) {
	home, _ := os.UserHomeDir()
	currentRepoRoot = cleanEvaluatedPath(currentRepoRoot)
	var atoms []CompactCodexAtom
	sessions := map[string]compactCodexSessionContext{}
	for _, path := range findCodexJSONLFiles("", home) {
		if !compactSourceMayContainNewFacts(path, since) {
			continue
		}
		items, session, err := parseCompactCodexFile(ctx, path, currentRepoRoot)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			continue
		}
		atoms = append(atoms, items...)
		if session.ConversationID != "" {
			if existing := sessions[session.ConversationID]; existing.HasMeasuredJSONL {
				session.HasMeasuredJSONL = true
			}
			sessions[session.ConversationID] = session
		}
	}

	hasMeasuredJSONL := map[string]bool{}
	for conversationID, session := range sessions {
		if session.HasMeasuredJSONL {
			hasMeasuredJSONL[conversationID] = true
		}
	}
	fallbackByConversation := map[string][]CompactCodexAtom{}
	for _, path := range findCodexSQLiteFiles(home) {
		if !compactSourceMayContainNewFacts(path, since) {
			continue
		}
		events, _, err := NewCodexSQLiteParser().Parse(path, CodexSQLiteWatermark{})
		if err != nil {
			continue
		}
		for _, event := range events {
			session, ok := sessions[event.ToolSessionID]
			if !ok || hasMeasuredJSONL[event.ToolSessionID] {
				continue
			}
			atom := compactCodexSQLiteAtom(event, session)
			if atom.Quality == "measured" {
				fallbackByConversation[event.ToolSessionID] = append(fallbackByConversation[event.ToolSessionID], atom)
			}
		}
	}
	if len(fallbackByConversation) == 0 {
		return atoms, nil
	}
	selected := make([]CompactCodexAtom, 0, len(atoms))
	for _, atom := range atoms {
		if len(fallbackByConversation[atom.ConversationID]) == 0 {
			selected = append(selected, atom)
		}
	}
	for _, fallback := range fallbackByConversation {
		selected = append(selected, fallback...)
	}
	sortCompactAtoms(selected)
	return selected, nil
}

func compactSourceMayContainNewFacts(path string, since time.Time) bool {
	if since.IsZero() {
		return true
	}
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.ModTime().Before(since)
}

func parseCompactCodexFile(ctx context.Context, path, currentRepoRoot string) ([]CompactCodexAtom, compactCodexSessionContext, error) {
	currentRepoRoot = cleanEvaluatedPath(currentRepoRoot)
	var sessionID, turnID, turnCWD, model string
	var hasMeasuredJSONL bool
	evidenceRoots := map[string]struct{}{}
	allEvidenceRoots := map[string]struct{}{}
	rootCache := map[string]string{}
	var atoms []CompactCodexAtom
	err := forEachCodexJSONLLine(ctx, path, func(index int, raw []byte) error {
		var row struct {
			Type      string `json:"type"`
			Timestamp string `json:"timestamp"`
			Payload   struct {
				ID        string `json:"id"`
				Type      string `json:"type"`
				TurnID    string `json:"turn_id"`
				CWD       string `json:"cwd"`
				Model     string `json:"model"`
				Name      string `json:"name"`
				Input     string `json:"input"`
				Arguments string `json:"arguments"`
				Success   bool   `json:"success"`
				Changes   map[string]struct {
					Type string `json:"type"`
				} `json:"changes"`
				Info *struct {
					LastTokenUsage  map[string]any `json:"last_token_usage"`
					TotalTokenUsage map[string]any `json:"total_token_usage"`
				} `json:"info"`
			} `json:"payload"`
		}
		if err := json.Unmarshal(raw, &row); err != nil {
			return nil
		}
		switch strings.TrimSpace(row.Type) {
		case "session_meta":
			sessionID = strings.TrimSpace(row.Payload.ID)
			turnCWD = strings.TrimSpace(row.Payload.CWD)
		case "turn_context":
			turnID = strings.TrimSpace(row.Payload.TurnID)
			turnCWD = strings.TrimSpace(row.Payload.CWD)
			model = strings.TrimSpace(row.Payload.Model)
			evidenceRoots = map[string]struct{}{}
		case "response_item":
			if currentRepoRoot == "" {
				return nil
			}
			if row.Payload.Type != "custom_tool_call" && row.Payload.Type != "function_call" {
				return nil
			}
			for _, candidate := range compactToolPathCandidates(row.Payload.Name, row.Payload.Input, row.Payload.Arguments, turnCWD) {
				root := cachedGitRoot(candidate, rootCache)
				if root != "" {
					evidenceRoots[root] = struct{}{}
					allEvidenceRoots[root] = struct{}{}
				}
			}
		case "event_msg":
			if strings.TrimSpace(row.Payload.Type) == "patch_apply_end" {
				if currentRepoRoot == "" || !row.Payload.Success {
					return nil
				}
				for candidate := range row.Payload.Changes {
					root := cachedGitRoot(candidate, rootCache)
					if root != "" {
						evidenceRoots[root] = struct{}{}
						allEvidenceRoots[root] = struct{}{}
					}
				}
				return nil
			}
			if sessionID == "" || strings.TrimSpace(row.Payload.Type) != "token_count" || row.Payload.Info == nil {
				return nil
			}
			observedAt := parseObservedAt(row.Timestamp)
			if observedAt.IsZero() {
				return nil
			}
			atom := CompactCodexAtom{
				ID:             compactAtomID(sessionID, index+1),
				ConversationID: sessionID,
				ChangeSetID:    strings.Join([]string{"codex", sessionID, firstNonEmptyCompact(turnID, "unknown")}, ":"),
				Model:          firstNonEmptyCompact(model, "unknown"),
				ObservedAt:     observedAt,
				Quality:        "invalid",
			}
			selected := row.Payload.Info.LastTokenUsage
			if len(selected) > 0 {
				applyCompactCodexTokenUsage(&atom,
					asInt64(selected["input_tokens"]),
					asInt64(selected["cached_input_tokens"]),
					asInt64(selected["cache_write_input_tokens"]),
					asInt64(selected["output_tokens"]),
					asInt64(selected["reasoning_output_tokens"]),
					asInt64(selected["total_tokens"]),
				)
				if atom.Quality == "measured" {
					hasMeasuredJSONL = true
				}
			}
			if currentRepoRoot == "" {
				atoms = append(atoms, atom)
				evidenceRoots = map[string]struct{}{}
				return nil
			}
			if len(evidenceRoots) == 1 {
				if _, ok := evidenceRoots[currentRepoRoot]; ok {
					atom.Evidence = "direct"
					atoms = append(atoms, atom)
				}
			} else if len(evidenceRoots) > 1 {
				if _, ok := evidenceRoots[currentRepoRoot]; ok {
					atom.Evidence = "multi_repo_shared"
					atoms = append(atoms, atom)
				}
			} else if cachedGitRoot(turnCWD, rootCache) == currentRepoRoot {
				atom.Evidence = "weak_cwd"
				atoms = append(atoms, atom)
			}
			evidenceRoots = map[string]struct{}{}
		}
		return nil
	})
	session := compactCodexSessionContext{
		ConversationID:   sessionID,
		Model:            firstNonEmptyCompact(model, "unknown"),
		Evidence:         compactEvidenceForRepo(allEvidenceRoots, turnCWD, currentRepoRoot, rootCache),
		HasMeasuredJSONL: hasMeasuredJSONL,
	}
	return atoms, session, err
}

func compactCodexSQLiteAtom(event LocalToolUsageEvent, session compactCodexSessionContext) CompactCodexAtom {
	atom := CompactCodexAtom{
		ID:             compactSQLiteAtomID(event.ToolSessionID, event.ToolEventID),
		ConversationID: event.ToolSessionID,
		ChangeSetID:    strings.Join([]string{"codex", event.ToolSessionID, "sqlite", firstNonEmptyCompact(event.ToolEventID, "unknown")}, ":"),
		Model:          firstNonEmptyCompact(session.Model, "unknown"),
		ObservedAt:     event.ObservedEndAt.UTC(),
		Quality:        "invalid",
		Evidence:       session.Evidence,
	}
	applyCompactCodexTokenUsage(&atom, event.InputTokens, event.CachedInputTokens, 0, event.OutputTokens, event.ReasoningTokens, event.InputTokens+event.OutputTokens)
	return atom
}

func applyCompactCodexTokenUsage(atom *CompactCodexAtom, input, cacheRead, cacheWrite, output, reasoning, providerTotal int64) {
	atom.CacheRead = cacheRead
	atom.CacheWrite = cacheWrite
	atom.FreshInput = input - cacheRead - cacheWrite
	if atom.FreshInput < 0 {
		atom.FreshInput = 0
	}
	atom.Output = output
	atom.Reasoning = reasoning
	atom.ProviderTotal = providerTotal
	atom.Processed = atom.FreshInput + atom.CacheRead + atom.CacheWrite + atom.Output
	if atom.ProviderTotal == 0 {
		atom.ProviderTotal = atom.Processed
	}
	if atom.Processed > 0 && atom.Reasoning <= atom.Output {
		atom.Quality = "measured"
	}
}

func compactEvidenceForRepo(evidenceRoots map[string]struct{}, cwd, currentRepoRoot string, rootCache map[string]string) string {
	if currentRepoRoot == "" {
		return ""
	}
	if len(evidenceRoots) == 1 {
		if _, ok := evidenceRoots[currentRepoRoot]; ok {
			return "direct"
		}
		return ""
	}
	if len(evidenceRoots) > 1 {
		if _, ok := evidenceRoots[currentRepoRoot]; ok {
			return "multi_repo_shared"
		}
		return ""
	}
	if cachedGitRoot(cwd, rootCache) == currentRepoRoot {
		return "weak_cwd"
	}
	return ""
}

func compactSQLiteAtomID(conversationID, responseID string) string {
	sum := sha256.Sum256([]byte("codex-sqlite\x00" + conversationID + "\x00" + responseID))
	return hex.EncodeToString(sum[:])
}

func sortCompactAtoms(atoms []CompactCodexAtom) {
	sort.Slice(atoms, func(i, j int) bool {
		if atoms[i].ObservedAt.Equal(atoms[j].ObservedAt) {
			return atoms[i].ID < atoms[j].ID
		}
		return atoms[i].ObservedAt.Before(atoms[j].ObservedAt)
	})
}

func compactToolPathCandidates(toolName, input, arguments, turnCWD string) []string {
	seen := map[string]struct{}{}
	var candidates []string
	workdirs := compactExplicitWorkdirs(input, arguments)
	appendCandidate := func(candidate string) {
		candidate = strings.TrimSpace(strings.ReplaceAll(candidate, `\/`, `/`))
		if candidate == "" || !filepath.IsAbs(candidate) {
			return
		}
		if _, ok := seen[candidate]; ok {
			return
		}
		seen[candidate] = struct{}{}
		candidates = append(candidates, candidate)
	}
	for _, workdir := range workdirs {
		appendCandidate(workdir)
	}
	if compactIsPatchTool(toolName) {
		patchBase := strings.TrimSpace(turnCWD)
		if len(workdirs) == 1 {
			patchBase = workdirs[0]
		}
		for _, match := range patchPathPattern.FindAllStringSubmatch(input+"\n"+arguments, -1) {
			if len(match) < 2 {
				continue
			}
			candidate := strings.TrimSpace(strings.ReplaceAll(match[1], `\/`, `/`))
			if candidate != "" && !filepath.IsAbs(candidate) && patchBase != "" {
				candidate = filepath.Join(patchBase, candidate)
			}
			appendCandidate(candidate)
		}
	}
	return candidates
}

func compactExplicitWorkdirs(values ...string) []string {
	seen := map[string]struct{}{}
	var workdirs []string
	for _, value := range values {
		var object map[string]json.RawMessage
		if err := json.Unmarshal([]byte(strings.TrimSpace(value)), &object); err != nil {
			continue
		}
		raw, ok := object["workdir"]
		if !ok {
			continue
		}
		var workdir string
		if err := json.Unmarshal(raw, &workdir); err != nil {
			continue
		}
		workdir = strings.TrimSpace(strings.ReplaceAll(workdir, `\/`, `/`))
		if workdir == "" || !filepath.IsAbs(workdir) {
			continue
		}
		if _, ok := seen[workdir]; ok {
			continue
		}
		seen[workdir] = struct{}{}
		workdirs = append(workdirs, workdir)
	}
	return workdirs
}

func compactIsPatchTool(toolName string) bool {
	name := strings.ToLower(strings.TrimSpace(toolName))
	return name == "apply_patch" || strings.HasSuffix(name, ".apply_patch") || strings.HasSuffix(name, "__apply_patch")
}

func cachedGitRoot(candidate string, cache map[string]string) string {
	candidate = cleanEvaluatedPath(candidate)
	if candidate == "" {
		return ""
	}
	if root, ok := cache[candidate]; ok {
		return root
	}
	probe := candidate
	if info, err := os.Stat(probe); err == nil && !info.IsDir() {
		probe = filepath.Dir(probe)
	} else if err != nil {
		for {
			parent := filepath.Dir(probe)
			if parent == probe {
				break
			}
			probe = parent
			if info, statErr := os.Stat(probe); statErr == nil && info.IsDir() {
				break
			}
		}
	}
	command := exec.Command("git", "-C", probe, "rev-parse", "--show-toplevel")
	payload, err := command.Output()
	root := ""
	if err == nil {
		root = cleanEvaluatedPath(strings.TrimSpace(string(payload)))
	}
	cache[candidate] = root
	return root
}

func cleanEvaluatedPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	path = filepath.Clean(path)
	if evaluated, err := filepath.EvalSymlinks(path); err == nil {
		return filepath.Clean(evaluated)
	}
	return path
}

func compactAtomID(sessionID string, line int) string {
	sum := sha256.Sum256([]byte(sessionID + "\x00" + strconv.Itoa(line)))
	return hex.EncodeToString(sum[:])
}

func firstNonEmptyCompact(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
