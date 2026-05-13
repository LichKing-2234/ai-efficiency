package attributionlocal

import (
	"os"
	"path/filepath"
	"sort"

	"github.com/ai-efficiency/ae-cli/internal/session"
)

type ScanState struct {
	CodexSQLite CodexSQLiteWatermark `json:"codex_sqlite"`
}

type Scanner struct {
	codexSQLite *CodexSQLiteParser
}

func NewScanner() *Scanner {
	return &Scanner{codexSQLite: NewCodexSQLiteParser()}
}

func (s *Scanner) ScanWorkspace(workspaceRoot string, state ScanState) ([]LocalToolUsageEvent, ScanState, error) {
	var out []LocalToolUsageEvent

	codexDB := filepath.Join(os.Getenv("HOME"), ".codex", "logs_2.sqlite")
	if _, err := os.Stat(codexDB); err == nil {
		items, wm, err := s.codexSQLite.Parse(codexDB, state.CodexSQLite)
		if err != nil {
			return nil, state, err
		}
		workspaceID, err := mustWorkspaceID(workspaceRoot)
		if err != nil {
			return nil, state, err
		}
		for _, item := range items {
			item.WorkspaceID = workspaceID
			out = append(out, item)
		}
		state.CodexSQLite = wm
	}

	return dedupeAndSort(out), state, nil
}

func mustWorkspaceID(workspaceRoot string) (string, error) {
	gitDir := filepath.Join(workspaceRoot, ".git")
	return session.DeriveWorkspaceID(workspaceRoot, workspaceRoot, gitDir, gitDir)
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
