package hooks

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ai-efficiency/ae-cli/internal/clistate"
)

type UnresolvedHookEvent struct {
	Kind           string         `json:"kind"`
	RemoteURL      string         `json:"remote_url"`
	RepoKey        string         `json:"repo_key"`
	WorkspaceID    string         `json:"workspace_id"`
	ServerURL      string         `json:"server_url"`
	AuthSubject    string         `json:"auth_subject"`
	CommitSHA      string         `json:"commit_sha"`
	RewriteType    string         `json:"rewrite_type,omitempty"`
	OldCommitSHA   string         `json:"old_commit_sha,omitempty"`
	NewCommitSHA   string         `json:"new_commit_sha,omitempty"`
	ParentSHAs     []string       `json:"parent_shas"`
	BranchSnapshot string         `json:"branch_snapshot"`
	HeadSnapshot   string         `json:"head_snapshot"`
	CapturedAt     string         `json:"captured_at"`
	AgentSnapshot  map[string]any `json:"agent_snapshot,omitempty"`
}

func unresolvedQueuePath() string {
	return filepath.Join(clistate.HooksStateDir(), "unresolved-hooks.jsonl")
}

func unresolvedQueueLockPath() string {
	return unresolvedQueuePath() + ".lock"
}

func withUnresolvedQueueLock(fn func() error) error {
	return withFileLock(unresolvedQueueLockPath(), fn)
}

func unresolvedDedupeKey(ev UnresolvedHookEvent) string {
	return strings.Join([]string{
		strings.TrimSpace(ev.Kind),
		strings.TrimSpace(ev.ServerURL),
		strings.TrimSpace(ev.AuthSubject),
		strings.TrimSpace(ev.RemoteURL),
		strings.TrimSpace(ev.WorkspaceID),
		strings.TrimSpace(ev.CommitSHA),
		strings.TrimSpace(ev.RewriteType),
		strings.TrimSpace(ev.OldCommitSHA),
		strings.TrimSpace(ev.NewCommitSHA),
	}, "\x1f")
}

func ListUnresolvedHookEvents() ([]UnresolvedHookEvent, error) {
	var out []UnresolvedHookEvent
	err := withUnresolvedQueueLock(func() error {
		items, err := listUnresolvedHookEventsUnlocked()
		if err != nil {
			return err
		}
		out = items
		return nil
	})
	return out, err
}

func listUnresolvedHookEventsUnlocked() ([]UnresolvedHookEvent, error) {
	path := unresolvedQueuePath()
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("open unresolved hook queue: %w", err)
	}
	defer f.Close()

	var out []UnresolvedHookEvent
	r := bufio.NewReaderSize(f, 64*1024)
	for {
		line, err := r.ReadBytes('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("read unresolved hook queue: %w", err)
		}
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			if errors.Is(err, io.EOF) {
				break
			}
			continue
		}
		var ev UnresolvedHookEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			_ = appendCorruptUnresolvedHookLine(line)
			if errors.Is(err, io.EOF) {
				break
			}
			continue
		}
		out = append(out, ev)
		if errors.Is(err, io.EOF) {
			break
		}
	}
	return out, nil
}

func appendCorruptUnresolvedHookLine(line []byte) error {
	line = bytes.TrimSpace(line)
	if len(line) == 0 {
		return nil
	}
	path := fmt.Sprintf("%s.corrupt-line.%d", unresolvedQueuePath(), time.Now().UTC().UnixNano())
	return os.WriteFile(path, append(line, '\n'), 0o600)
}

func CountUnresolvedHookEvents() (int, error) {
	items, err := ListUnresolvedHookEvents()
	if err != nil {
		return 0, err
	}
	return len(items), nil
}

func EnqueueUnresolvedHookEvent(ev UnresolvedHookEvent) error {
	if err := validateUnresolvedHookEvent(ev); err != nil {
		return err
	}
	return withUnresolvedQueueLock(func() error {
		items, err := listUnresolvedHookEventsUnlocked()
		if err != nil {
			return err
		}
		key := unresolvedDedupeKey(ev)
		for _, item := range items {
			if unresolvedDedupeKey(item) == key {
				return nil
			}
		}
		path := unresolvedQueuePath()
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return fmt.Errorf("create unresolved hook queue dir: %w", err)
		}
		f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			return fmt.Errorf("open unresolved hook queue: %w", err)
		}
		defer f.Close()
		data, err := json.Marshal(ev)
		if err != nil {
			return fmt.Errorf("marshal unresolved hook event: %w", err)
		}
		if _, err := f.Write(append(data, '\n')); err != nil {
			return fmt.Errorf("append unresolved hook event: %w", err)
		}
		return nil
	})
}

func validateUnresolvedHookEvent(ev UnresolvedHookEvent) error {
	kind := strings.TrimSpace(ev.Kind)
	if kind == "" || strings.TrimSpace(ev.RemoteURL) == "" || strings.TrimSpace(ev.WorkspaceID) == "" {
		return fmt.Errorf("unresolved hook event requires kind, remote_url, and workspace_id")
	}
	switch kind {
	case "post-commit":
		if strings.TrimSpace(ev.CommitSHA) == "" {
			return fmt.Errorf("unresolved post-commit event requires commit_sha")
		}
	case "post-rewrite":
		if strings.TrimSpace(ev.RewriteType) == "" || strings.TrimSpace(ev.OldCommitSHA) == "" || strings.TrimSpace(ev.NewCommitSHA) == "" {
			return fmt.Errorf("unresolved post-rewrite event requires rewrite_type, old_commit_sha, and new_commit_sha")
		}
	default:
		return fmt.Errorf("unsupported unresolved hook event kind: %s", kind)
	}
	return nil
}

func SaveUnresolvedHookEvents(items []UnresolvedHookEvent) error {
	return withUnresolvedQueueLock(func() error {
		return saveUnresolvedHookEventsUnlocked(items)
	})
}

func saveUnresolvedHookEventsUnlocked(items []UnresolvedHookEvent) error {
	path := unresolvedQueuePath()
	if len(items) == 0 {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove unresolved hook queue: %w", err)
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create unresolved hook queue dir: %w", err)
	}
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open unresolved hook queue tmp: %w", err)
	}
	w := bufio.NewWriter(f)
	for _, item := range items {
		data, err := json.Marshal(item)
		if err != nil {
			_ = f.Close()
			return fmt.Errorf("marshal unresolved hook event: %w", err)
		}
		if _, err := w.Write(append(data, '\n')); err != nil {
			_ = f.Close()
			return fmt.Errorf("write unresolved hook queue tmp: %w", err)
		}
	}
	if err := w.Flush(); err != nil {
		_ = f.Close()
		return fmt.Errorf("flush unresolved hook queue tmp: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close unresolved hook queue tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename unresolved hook queue tmp: %w", err)
	}
	return nil
}
