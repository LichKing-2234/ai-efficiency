package attributionlocal

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ai-efficiency/ae-cli/internal/session"
)

type ScanState struct {
	CodexSQLite map[string]CodexSQLiteWatermark `json:"codex_sqlite,omitempty"`
	FileModUnix map[string]int64                `json:"file_mod_unix,omitempty"`
}

type Scanner struct{}

func NewScanner() *Scanner {
	return &Scanner{}
}

func (s *Scanner) ScanWorkspace(workspaceRoot string, state ScanState) ([]LocalToolUsageEvent, ScanState, error) {
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
		for _, sessionID := range findCodexWorkspaceSessionIDs(path, workspaceRoot) {
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
		if !shouldScanFile(path, state) {
			continue
		}
		items, err := ParseCodexJSONLFallback(path, workspaceRoot)
		if err != nil {
			continue
		}
		rememberFileScan(&nextState, path)
		for _, item := range items {
			item.WorkspaceID = workspaceID
			out = append(out, item)
		}
	}

	for _, path := range findClaudeJSONLFiles(homeDir) {
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

	return dedupeAndSort(out), nextState, nil
}

func mustWorkspaceID(workspaceRoot string) (string, error) {
	gitDir, err := resolveWorkspaceGitDir(workspaceRoot)
	if err != nil {
		gitDir = filepath.Join(workspaceRoot, ".git")
	}
	gitCommonDir := gitDir
	commonPath := filepath.Join(gitDir, "commondir")
	if data, err := os.ReadFile(commonPath); err == nil {
		rel := strings.TrimSpace(string(data))
		if rel != "" {
			if filepath.IsAbs(rel) {
				gitCommonDir = filepath.Clean(rel)
			} else {
				gitCommonDir = filepath.Clean(filepath.Join(gitDir, rel))
			}
		}
	}
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
	return walkFiles(filepath.Join(homeDir, ".codex"), ".jsonl")
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
		CodexSQLite: make(map[string]CodexSQLiteWatermark, len(state.CodexSQLite)),
		FileModUnix: make(map[string]int64, len(state.FileModUnix)),
	}
	for path, watermark := range state.CodexSQLite {
		next.CodexSQLite[path] = watermark
	}
	for path, mod := range state.FileModUnix {
		next.FileModUnix[path] = mod
	}
	return next
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
