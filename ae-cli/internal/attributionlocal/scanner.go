package attributionlocal

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ai-efficiency/ae-cli/internal/session"
)

type ScanState struct {
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

	for _, path := range findCodexJSONLFiles(workspaceRoot, homeDir) {
		items, err := ParseCodexJSONLFallback(path, workspaceRoot)
		if err != nil {
			continue
		}
		for _, item := range items {
			item.WorkspaceID = workspaceID
			out = append(out, item)
		}
	}

	for _, path := range findClaudeJSONLFiles(homeDir) {
		items, err := ParseClaudeJSONL(path, workspaceRoot)
		if err != nil {
			continue
		}
		for _, item := range items {
			item.WorkspaceID = workspaceID
			out = append(out, item)
		}
	}

	for _, path := range findKiroJSONFiles(homeDir) {
		items, err := ParseKiroJSON(path, workspaceRoot)
		if err != nil {
			continue
		}
		for _, item := range items {
			item.WorkspaceID = workspaceID
			out = append(out, item)
		}
	}

	return dedupeAndSort(out), state, nil
}

func mustWorkspaceID(workspaceRoot string) (string, error) {
	gitDir, err := filepath.EvalSymlinks(filepath.Join(workspaceRoot, ".git"))
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
