package collector

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ai-efficiency/ae-cli/internal/session"
)

func BuildSnapshot(paths Paths) (*Snapshot, error) {
	out := &Snapshot{}

	for _, p := range orderFilesByModTime(paths.CodexFiles) {
		s, err := readCodexSnapshot(p, paths.WorkspaceRoot)
		if err != nil {
			continue
		}
		if s == nil {
			continue
		}
		out.Codex = s
		break
	}

	for _, p := range orderFilesByModTime(paths.ClaudeFiles) {
		s, err := readClaudeSnapshot(p, paths.WorkspaceRoot)
		if err != nil {
			continue
		}
		if s == nil {
			continue
		}
		out.Claude = s
		break
	}

	for _, p := range orderFilesByModTime(paths.KiroFiles) {
		s, err := readKiroSnapshot(p, paths.WorkspaceRoot)
		if err != nil {
			continue
		}
		if s == nil {
			continue
		}
		out.Kiro = s
		break
	}

	return out, nil
}

func WriteCache(sessionID string, snapshot *Snapshot) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return fmt.Errorf("session_id is required")
	}
	if snapshot == nil {
		return fmt.Errorf("snapshot is nil")
	}

	dir := session.RuntimeCollectorsDir(sessionID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create runtime collectors dir: %w", err)
	}

	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal snapshot: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "latest.json"), data, 0o600); err != nil {
		return fmt.Errorf("write snapshot cache: %w", err)
	}
	return nil
}

func DefaultPaths(workspaceRoot string) Paths {
	out := Paths{
		WorkspaceRoot: workspaceRoot,
	}

	if v := envList("AE_CODEX_SESSION_FILES"); len(v) > 0 {
		out.CodexFiles = v
	}
	if v := envList("AE_CLAUDE_SESSION_FILES"); len(v) > 0 {
		out.ClaudeFiles = v
	}
	if v := envList("AE_KIRO_SESSION_FILES"); len(v) > 0 {
		out.KiroFiles = v
	}

	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return out
	}

	if len(out.CodexFiles) == 0 {
		out.CodexFiles = walkFiles(filepath.Join(home, ".codex"), ".jsonl")
	}
	if len(out.ClaudeFiles) == 0 {
		out.ClaudeFiles = walkFiles(filepath.Join(home, ".claude"), ".jsonl")
	}
	if len(out.KiroFiles) == 0 {
		out.KiroFiles = walkFiles(filepath.Join(home, ".kiro"), ".json")
	}
	return out
}

func orderFilesByModTime(paths []string) []string {
	type candidate struct {
		path    string
		modTime time.Time
	}
	if len(paths) == 0 {
		return nil
	}
	candidates := make([]candidate, 0, len(paths))
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			candidates = append(candidates, candidate{path: path})
			continue
		}
		candidates = append(candidates, candidate{path: path, modTime: info.ModTime()})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].modTime.Equal(candidates[j].modTime) {
			return candidates[i].path > candidates[j].path
		}
		return candidates[i].modTime.After(candidates[j].modTime)
	})
	out := make([]string, 0, len(candidates))
	for _, c := range candidates {
		out = append(out, c.path)
	}
	return out
}

func envList(key string) []string {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, string(os.PathListSeparator))
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	return out
}

func walkFiles(root string, ext string) []string {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil
	}
	if ext == "" {
		return nil
	}
	type candidate struct {
		path    string
		modTime time.Time
	}
	var matches []candidate
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d == nil || d.IsDir() {
			return nil
		}
		if strings.EqualFold(filepath.Ext(path), ext) {
			info, statErr := d.Info()
			if statErr != nil {
				return nil
			}
			matches = append(matches, candidate{path: path, modTime: info.ModTime()})
		}
		return nil
	})
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].modTime.Equal(matches[j].modTime) {
			return matches[i].path > matches[j].path
		}
		return matches[i].modTime.After(matches[j].modTime)
	})

	out := make([]string, 0, len(matches))
	for _, match := range matches {
		out = append(out, match.path)
	}
	return out
}
