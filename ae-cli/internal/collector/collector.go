package collector

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ai-efficiency/ae-cli/internal/session"
)

func BuildSnapshot(paths Paths) (*Snapshot, error) {
	out := &Snapshot{}

	for _, p := range paths.CodexFiles {
		s, err := readCodexSnapshot(p, paths.WorkspaceRoot)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("read codex snapshot %q: %w", p, err)
		}
		if s == nil {
			continue
		}
		if out.Codex == nil || s.TotalTokens >= out.Codex.TotalTokens {
			out.Codex = s
		}
	}

	for _, p := range paths.ClaudeFiles {
		s, err := readClaudeSnapshot(p, paths.WorkspaceRoot)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("read claude snapshot %q: %w", p, err)
		}
		if s == nil {
			continue
		}
		if out.Claude == nil {
			out.Claude = s
			continue
		}
		if out.Claude.SourceSessionID == "" {
			out.Claude.SourceSessionID = s.SourceSessionID
		}
		out.Claude.InputTokens += s.InputTokens
		out.Claude.OutputTokens += s.OutputTokens
		out.Claude.CachedInputTokens += s.CachedInputTokens
		if len(s.RawPayload) > 0 {
			out.Claude.RawPayload = s.RawPayload
		}
	}

	for _, p := range paths.KiroFiles {
		s, err := readKiroSnapshot(p, paths.WorkspaceRoot)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("read kiro snapshot %q: %w", p, err)
		}
		if s == nil {
			continue
		}
		out.Kiro = s
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
	if len(out.CodexFiles) > 0 || len(out.ClaudeFiles) > 0 || len(out.KiroFiles) > 0 {
		return out
	}

	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return out
	}

	out.CodexFiles = walkFiles(filepath.Join(home, ".codex"), ".jsonl")
	out.ClaudeFiles = walkFiles(filepath.Join(home, ".claude"), ".jsonl")
	out.KiroFiles = walkFiles(filepath.Join(home, ".kiro"), ".json")
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
	sort.Strings(out)
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
	var out []string
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d == nil || d.IsDir() {
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
