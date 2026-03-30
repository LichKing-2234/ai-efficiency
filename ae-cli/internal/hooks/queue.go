package hooks

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type HookEvent struct {
	Kind      string `json:"kind"`
	EventID   string `json:"event_id,omitempty"`
	SessionID string `json:"session_id,omitempty"`

	// Git context (minimal slice for Task 5).
	CommitSHA string `json:"commit_sha,omitempty"`
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
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("home dir: %w", err)
	}
	return filepath.Join(home, ".ae-cli", "runtime", sessionID, "queue", "hooks.jsonl"), nil
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

func (q *Queue) computeEventID(ev HookEvent) string {
	// Keep stable and local-only for now; backend idempotency will refine this.
	h := sha256.Sum256([]byte(strings.TrimSpace(ev.Kind) + "\x1f" + strings.TrimSpace(ev.SessionID) + "\x1f" + strings.TrimSpace(ev.CommitSHA)))
	return hex.EncodeToString(h[:])
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
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var it QueueItem
		if err := json.Unmarshal([]byte(line), &it); err != nil {
			return nil, fmt.Errorf("parse queue line: %w", err)
		}
		out = append(out, it)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("scan queue: %w", err)
	}
	return out, nil
}

func (q *Queue) Enqueue(ev HookEvent) error {
	if q == nil || strings.TrimSpace(q.path) == "" {
		return fmt.Errorf("queue is not initialized")
	}
	if strings.TrimSpace(ev.EventID) == "" {
		ev.EventID = q.computeEventID(ev)
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
