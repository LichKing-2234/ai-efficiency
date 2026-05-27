package attributionlocal

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ai-efficiency/ae-cli/internal/session"
)

type ScanState struct {
	CodexSQLite     map[string]CodexSQLiteWatermark `json:"codex_sqlite,omitempty"`
	CodexSessionIDs map[string]CodexSessionIDCache  `json:"codex_session_ids,omitempty"`
	FileModUnix     map[string]int64                `json:"file_mod_unix,omitempty"`
}

type Scanner struct{}

func NewScanner() *Scanner {
	return &Scanner{}
}

func (s *Scanner) ScanWorkspace(workspaceRoot string, state ScanState) ([]LocalToolUsageEvent, ScanState, error) {
	return s.ScanWorkspaceContext(context.Background(), workspaceRoot, state)
}

func (s *Scanner) ScanWorkspaceContext(ctx context.Context, workspaceRoot string, state ScanState) ([]LocalToolUsageEvent, ScanState, error) {
	var out []LocalToolUsageEvent
	workspaceID, err := mustWorkspaceID(workspaceRoot)
	if err != nil {
		return nil, state, err
	}
	homeDir, _ := os.UserHomeDir()
	nextState := cloneScanState(state)

	codexJSONLFiles := findCodexJSONLFiles(workspaceRoot, homeDir)
	codexSessionIDs := make(map[string]struct{})
	for _, path := range codexJSONLFiles {
		sessionIDs, err := cachedCodexWorkspaceSessionIDs(ctx, &nextState, path, workspaceRoot)
		if err != nil {
			return nil, state, err
		}
		for _, sessionID := range sessionIDs {
			codexSessionIDs[sessionID] = struct{}{}
		}
	}

	for _, path := range findCodexSQLiteFiles(homeDir) {
		parser := NewCodexSQLiteParser()
		events, watermark, err := parser.Parse(path, nextState.CodexSQLite[path])
		if err != nil {
			continue
		}
		nextState.CodexSQLite[path] = watermark
		for _, item := range events {
			if _, ok := codexSessionIDs[item.ToolSessionID]; !ok {
				continue
			}
			item.WorkspaceID = workspaceID
			out = append(out, item)
		}
	}

	for _, path := range codexJSONLFiles {
		if err := ctx.Err(); err != nil {
			return nil, state, err
		}
		if !shouldScanFile(path, state) {
			continue
		}
		items, err := ParseCodexJSONLFallbackContext(ctx, path, workspaceRoot)
		if err != nil {
			if ctx.Err() != nil {
				return nil, state, ctx.Err()
			}
			continue
		}
		rememberFileScan(&nextState, path)
		for _, item := range items {
			item.WorkspaceID = workspaceID
			out = append(out, item)
		}
	}

	for _, path := range findClaudeJSONLFiles(homeDir) {
		if err := ctx.Err(); err != nil {
			return nil, state, err
		}
		if !shouldScanFile(path, state) {
			continue
		}
		items, err := ParseClaudeJSONL(path, workspaceRoot)
		if err != nil {
			continue
		}
		rememberFileScan(&nextState, path)
		for _, item := range items {
			item.WorkspaceID = workspaceID
			out = append(out, item)
		}
	}

	for _, path := range findKiroJSONFiles(homeDir) {
		if err := ctx.Err(); err != nil {
			return nil, state, err
		}
		if !shouldScanFile(path, state) {
			continue
		}
		items, err := ParseKiroJSON(path, workspaceRoot)
		if err != nil {
			continue
		}
		rememberFileScan(&nextState, path)
		for _, item := range items {
			item.WorkspaceID = workspaceID
			out = append(out, item)
		}
	}

	for _, path := range findKiroCLISQLiteFiles(homeDir) {
		if err := ctx.Err(); err != nil {
			return nil, state, err
		}
		if !shouldScanFile(path, state) {
			continue
		}
		items, err := ParseKiroCLISQLite(path, workspaceRoot)
		if err != nil {
			continue
		}
		rememberFileScan(&nextState, path)
		for _, item := range items {
			item.WorkspaceID = workspaceID
			out = append(out, item)
		}
	}

	kiroIDESessionIDs := FindKiroIDESessionIDs(homeDir, workspaceRoot)
	for _, path := range FindKiroIDEExecutionFiles(homeDir) {
		if err := ctx.Err(); err != nil {
			return nil, state, err
		}
		if !shouldScanFile(path, state) {
			continue
		}
		items, err := ParseKiroIDEExecution(path, kiroIDESessionIDs)
		if err != nil {
			continue
		}
		rememberFileScan(&nextState, path)
		for _, item := range items {
			item.WorkspaceID = workspaceID
			out = append(out, item)
		}
	}

	return dedupeAndSort(out), nextState, nil
}

func mustWorkspaceID(workspaceRoot string) (string, error) {
	gitDir, err := resolveWorkspaceGitDir(workspaceRoot)
	if err != nil {
		gitDir = filepath.Join(workspaceRoot, ".git")
	}
	gitCommonDir := filepath.Clean(resolveGitCommonDir(gitDir))
	return session.DeriveWorkspaceID(workspaceRoot, workspaceRoot, gitDir, gitCommonDir)
}

func resolveWorkspaceGitDir(workspaceRoot string) (string, error) {
	gitPath := filepath.Join(workspaceRoot, ".git")
	info, err := os.Stat(gitPath)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return filepath.EvalSymlinks(gitPath)
	}

	data, err := os.ReadFile(gitPath)
	if err != nil {
		return "", err
	}
	line := strings.TrimSpace(string(data))
	const prefix = "gitdir:"
	if !strings.HasPrefix(strings.ToLower(line), prefix) {
		return filepath.EvalSymlinks(gitPath)
	}

	rawGitDir := strings.TrimSpace(line[len(prefix):])
	if rawGitDir == "" {
		return "", os.ErrInvalid
	}
	if !filepath.IsAbs(rawGitDir) {
		rawGitDir = filepath.Join(workspaceRoot, rawGitDir)
	}
	return filepath.EvalSymlinks(rawGitDir)
}

func dedupeAndSort(items []LocalToolUsageEvent) []LocalToolUsageEvent {
	if len(items) == 0 {
		return nil
	}
	seen := map[string]LocalToolUsageEvent{}
	for _, item := range items {
		seen[item.DedupeKey] = item
	}
	keys := make([]string, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]LocalToolUsageEvent, 0, len(keys))
	for _, key := range keys {
		out = append(out, seen[key])
	}
	return out
}

func findCodexJSONLFiles(workspaceRoot, homeDir string) []string {
	_ = workspaceRoot
	return walkFiles(filepath.Join(homeDir, ".codex", "sessions"), ".jsonl")
}

func findCodexSQLiteFiles(homeDir string) []string {
	dbPath := filepath.Join(strings.TrimSpace(homeDir), ".codex", "logs_2.sqlite")
	if strings.TrimSpace(homeDir) == "" {
		return nil
	}
	if _, err := os.Stat(dbPath); err != nil {
		return nil
	}
	return []string{dbPath}
}

func findClaudeJSONLFiles(homeDir string) []string {
	return walkFiles(filepath.Join(homeDir, ".claude"), ".jsonl")
}

func findKiroJSONFiles(homeDir string) []string {
	return walkFiles(filepath.Join(homeDir, ".kiro"), ".json")
}

func findKiroCLISQLiteFiles(homeDir string) []string {
	dbPath := filepath.Join(strings.TrimSpace(homeDir), "Library", "Application Support", "kiro-cli", "data.sqlite3")
	if strings.TrimSpace(homeDir) == "" {
		return nil
	}
	if _, err := os.Stat(dbPath); err != nil {
		return nil
	}
	return []string{dbPath}
}

func walkFiles(root string, ext string) []string {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil
	}
	var out []string
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		if strings.EqualFold(filepath.Ext(path), ext) {
			out = append(out, path)
		}
		return nil
	})
	sort.Strings(out)
	return out
}

func cloneScanState(state ScanState) ScanState {
	next := ScanState{
		CodexSQLite:     make(map[string]CodexSQLiteWatermark, len(state.CodexSQLite)),
		CodexSessionIDs: make(map[string]CodexSessionIDCache, len(state.CodexSessionIDs)),
		FileModUnix:     make(map[string]int64, len(state.FileModUnix)),
	}
	for path, watermark := range state.CodexSQLite {
		next.CodexSQLite[path] = watermark
	}
	for path, cache := range state.CodexSessionIDs {
		next.CodexSessionIDs[path] = CodexSessionIDCache{
			ModUnix:    cache.ModUnix,
			Size:       cache.Size,
			SessionIDs: append([]string(nil), cache.SessionIDs...),
		}
	}
	for path, mod := range state.FileModUnix {
		next.FileModUnix[path] = mod
	}
	return next
}

func cachedCodexWorkspaceSessionIDs(ctx context.Context, state *ScanState, path, workspaceRoot string) ([]string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, nil
	}
	if state != nil && state.CodexSessionIDs != nil {
		if cached, ok := state.CodexSessionIDs[path]; ok && cached.ModUnix == info.ModTime().Unix() && cached.Size == info.Size() {
			return append([]string(nil), cached.SessionIDs...), nil
		}
	}

	sessionIDs, err := findCodexWorkspaceSessionIDsContext(ctx, path, workspaceRoot)
	if err != nil {
		return nil, err
	}
	if state != nil {
		if state.CodexSessionIDs == nil {
			state.CodexSessionIDs = map[string]CodexSessionIDCache{}
		}
		state.CodexSessionIDs[path] = CodexSessionIDCache{
			ModUnix:    info.ModTime().Unix(),
			Size:       info.Size(),
			SessionIDs: append([]string(nil), sessionIDs...),
		}
	}
	return sessionIDs, nil
}

func shouldScanFile(path string, state ScanState) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	if state.FileModUnix == nil {
		return true
	}
	return info.ModTime().Unix() > state.FileModUnix[path]
}

func rememberFileScan(state *ScanState, path string) {
	if state == nil {
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		return
	}
	if state.FileModUnix == nil {
		state.FileModUnix = map[string]int64{}
	}
	state.FileModUnix[path] = info.ModTime().Unix()
}
