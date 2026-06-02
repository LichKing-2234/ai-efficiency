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
	ParentSHAs     []string       `json:"parent_shas"`
	BranchSnapshot string         `json:"branch_snapshot"`
	HeadSnapshot   string         `json:"head_snapshot"`
	CapturedAt     string         `json:"captured_at"`
	AgentSnapshot  map[string]any `json:"agent_snapshot,omitempty"`
}

func unresolvedQueuePath() string {
	return filepath.Join(clistate.HooksStateDir(), "unresolved-hooks.jsonl")
}

func unresolvedDedupeKey(ev UnresolvedHookEvent) string {
	return strings.Join([]string{
		strings.TrimSpace(ev.Kind),
		strings.TrimSpace(ev.ServerURL),
		strings.TrimSpace(ev.AuthSubject),
		strings.TrimSpace(ev.RemoteURL),
		strings.TrimSpace(ev.WorkspaceID),
		strings.TrimSpace(ev.CommitSHA),
	}, "\x1f")
}

func ListUnresolvedHookEvents() ([]UnresolvedHookEvent, error) {
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
			return nil, fmt.Errorf("parse unresolved hook queue: %w", err)
		}
		out = append(out, ev)
		if errors.Is(err, io.EOF) {
			break
		}
	}
	return out, nil
}

func CountUnresolvedHookEvents() (int, error) {
	items, err := ListUnresolvedHookEvents()
	if err != nil {
		return 0, err
	}
	return len(items), nil
}

func EnqueueUnresolvedHookEvent(ev UnresolvedHookEvent) error {
	if strings.TrimSpace(ev.Kind) == "" || strings.TrimSpace(ev.RemoteURL) == "" || strings.TrimSpace(ev.WorkspaceID) == "" || strings.TrimSpace(ev.CommitSHA) == "" {
		return fmt.Errorf("unresolved hook event requires kind, remote_url, workspace_id, and commit_sha")
	}
	items, err := ListUnresolvedHookEvents()
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
}

func SaveUnresolvedHookEvents(items []UnresolvedHookEvent) error {
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
