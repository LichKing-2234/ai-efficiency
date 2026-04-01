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
	"sort"
	"strings"

	"github.com/ai-efficiency/ae-cli/internal/session"
)

type HookEvent struct {
	Kind          string `json:"kind"`
	EventID       string `json:"event_id,omitempty"`
	SessionID     string `json:"session_id,omitempty"`
	RepoFullName  string `json:"repo_full_name,omitempty"`
	WorkspaceID   string `json:"workspace_id,omitempty"`
	BindingSource string `json:"binding_source,omitempty"`

	AgentSnapshot map[string]any `json:"agent_snapshot,omitempty"`

	// Git context (minimal slice for Task 5).
	CommitSHA      string   `json:"commit_sha,omitempty"`
	ParentSHAs     []string `json:"parent_shas,omitempty"`
	BranchSnapshot string   `json:"branch_snapshot,omitempty"`
	HeadSnapshot   string   `json:"head_snapshot,omitempty"`
	CapturedAt     string   `json:"captured_at,omitempty"`

	// post-rewrite specific fields.
	RewriteType  string `json:"rewrite_type,omitempty"`
	OldCommitSHA string `json:"old_commit_sha,omitempty"`
	NewCommitSHA string `json:"new_commit_sha,omitempty"`
}

type QueueItem struct {
	Event HookEvent `json:"event"`
}

type Queue struct {
	path string
}

func queuePath(sessionID string) (string, error) {
	if strings.TrimSpace(sessionID) == "" {
		return "", fmt.Errorf("session_id is required")
	}
	return session.RuntimeQueueFilePath(sessionID), nil
}

func NewLocalQueue(sessionID string) (*Queue, error) {
	p, err := queuePath(sessionID)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return nil, fmt.Errorf("creating queue dir: %w", err)
	}
	return &Queue{path: p}, nil
}

func (q *Queue) Path() string {
	if q == nil {
		return ""
	}
	return q.path
}

func (q *Queue) List() ([]QueueItem, error) {
	if q == nil || strings.TrimSpace(q.path) == "" {
		return nil, fmt.Errorf("queue is not initialized")
	}
	f, err := os.Open(q.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("open queue: %w", err)
	}
	defer f.Close()

	var out []QueueItem
	r := bufio.NewReaderSize(f, 64*1024)
	for {
		line, err := r.ReadBytes('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("read queue line: %w", err)
		}
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			if errors.Is(err, io.EOF) {
				break
			}
			continue
		}
		var it QueueItem
		if err := json.Unmarshal(line, &it); err != nil {
			return nil, fmt.Errorf("parse queue line: %w", err)
		}
		out = append(out, it)
		if errors.Is(err, io.EOF) {
			break
		}
	}
	return out, nil
}

func (q *Queue) Enqueue(ev HookEvent) error {
	if q == nil || strings.TrimSpace(q.path) == "" {
		return fmt.Errorf("queue is not initialized")
	}
	if strings.TrimSpace(ev.EventID) == "" {
		return fmt.Errorf("event_id is required")
	}

	items, err := q.List()
	if err != nil {
		return err
	}
	for _, it := range items {
		if strings.TrimSpace(it.Event.EventID) != "" && it.Event.EventID == ev.EventID {
			// Dedup: fail-open queue should not spam on repeated retries.
			return nil
		}
	}

	if err := os.MkdirAll(filepath.Dir(q.path), 0o700); err != nil {
		return fmt.Errorf("creating queue dir: %w", err)
	}
	f, err := os.OpenFile(q.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open queue for append: %w", err)
	}
	defer f.Close()

	b, err := json.Marshal(QueueItem{Event: ev})
	if err != nil {
		return fmt.Errorf("marshal queue item: %w", err)
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		return fmt.Errorf("append queue item: %w", err)
	}
	return nil
}

func (q *Queue) rewrite(items []QueueItem) error {
	if q == nil || strings.TrimSpace(q.path) == "" {
		return fmt.Errorf("queue is not initialized")
	}
	if len(items) == 0 {
		if err := os.Remove(q.path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove empty queue: %w", err)
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(q.path), 0o700); err != nil {
		return fmt.Errorf("creating queue dir: %w", err)
	}
	tmp := q.path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open tmp queue: %w", err)
	}
	w := bufio.NewWriter(f)
	for _, it := range items {
		b, err := json.Marshal(it)
		if err != nil {
			_ = f.Close()
			return fmt.Errorf("marshal queue item: %w", err)
		}
		if _, err := w.Write(append(b, '\n')); err != nil {
			_ = f.Close()
			return fmt.Errorf("write tmp queue: %w", err)
		}
	}
	if err := w.Flush(); err != nil {
		_ = f.Close()
		return fmt.Errorf("flush tmp queue: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close tmp queue: %w", err)
	}
	if err := os.Rename(tmp, q.path); err != nil {
		return fmt.Errorf("rename tmp queue: %w", err)
	}
	return nil
}

func PendingSessionIDs() ([]string, error) {
	root := session.RuntimeRootDir()
	if strings.TrimSpace(root) == "" {
		return nil, fmt.Errorf("runtime root is empty")
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read runtime root: %w", err)
	}

	var out []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		sessionID := strings.TrimSpace(entry.Name())
		if sessionID == "" {
			continue
		}
		hasPending, err := session.HasPendingQueue(sessionID)
		if err != nil {
			return nil, err
		}
		if hasPending {
			out = append(out, sessionID)
		}
	}
	sort.Strings(out)
	return out, nil
}
