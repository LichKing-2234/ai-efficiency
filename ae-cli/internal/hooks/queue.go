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
	"time"

	"github.com/ai-efficiency/ae-cli/internal/attributionlocal"
)

type HookEvent struct {
	Kind          string `json:"kind"`
	EventID       string `json:"event_id,omitempty"`
	SessionID     string `json:"session_id,omitempty"`
	WorkspaceID   string `json:"workspace_id,omitempty"`
	ServerURL     string `json:"server_url,omitempty"`
	AuthSubject   string `json:"auth_subject,omitempty"`
	RepoConfigID  int    `json:"repo_config_id,omitempty"`
	RepoKey       string `json:"repo_key,omitempty"`
	RepoFullName  string `json:"repo_full_name,omitempty"`
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

var queueLockStaleAfter = 30 * time.Second
var queueLockHeartbeatInterval = 5 * time.Second

type Binding struct {
	ServerURL    string `json:"server_url,omitempty"`
	AuthSubject  string `json:"auth_subject,omitempty"`
	RepoConfigID int    `json:"repo_config_id,omitempty"`
	RepoKey      string `json:"repo_key,omitempty"`
	WorkspaceID  string `json:"workspace_id,omitempty"`
}

type LedgerRecord struct {
	Version      int        `json:"version"`
	Kind         string     `json:"kind"`
	DedupeKey    string     `json:"dedupe_key"`
	ServerURL    string     `json:"server_url"`
	AuthSubject  string     `json:"auth_subject"`
	RepoConfigID int        `json:"repo_config_id"`
	RepoKey      string     `json:"repo_key"`
	WorkspaceID  string     `json:"workspace_id"`
	Status       string     `json:"status"`
	AttemptCount int        `json:"attempt_count"`
	AttemptedAt  time.Time  `json:"attempted_at"`
	UploadedAt   *time.Time `json:"uploaded_at,omitempty"`
	HTTPStatus   int        `json:"http_status,omitempty"`
	LastError    string     `json:"last_error,omitempty"`
}

func queuePath(sessionID string) (string, error) {
	if strings.TrimSpace(sessionID) == "" {
		return "", fmt.Errorf("session_id is required")
	}
	return filepath.Join(attributionlocal.AttributionRootDir(), "legacy-session-queue", sessionID, "hooks.jsonl"), nil
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

func workspaceQueuePath(workspaceID string) (string, error) {
	if strings.TrimSpace(workspaceID) == "" {
		return "", fmt.Errorf("workspace_id is required")
	}
	return filepath.Join(attributionlocal.AttributionRootDir(), "workspaces", workspaceID, "hooks.jsonl"), nil
}

func NewWorkspaceQueue(workspaceID string) (*Queue, error) {
	p, err := workspaceQueuePath(workspaceID)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return nil, fmt.Errorf("creating queue dir: %w", err)
	}
	return &Queue{path: p}, nil
}

func LedgerPath(workspaceID string) (string, error) {
	if strings.TrimSpace(workspaceID) == "" {
		return "", fmt.Errorf("workspace_id is required")
	}
	return filepath.Join(attributionlocal.AttributionRootDir(), "workspaces", workspaceID, "upload-ledger.jsonl"), nil
}

func AppendLedger(workspaceID string, rec LedgerRecord) error {
	p, err := LedgerPath(workspaceID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return fmt.Errorf("creating ledger dir: %w", err)
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open ledger for append: %w", err)
	}
	defer f.Close()
	if rec.Version == 0 {
		rec.Version = 1
	}
	b, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("marshal ledger record: %w", err)
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		return fmt.Errorf("append ledger record: %w", err)
	}
	return nil
}

func ReadLedger(workspaceID string) ([]LedgerRecord, error) {
	p, err := LedgerPath(workspaceID)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("open ledger: %w", err)
	}
	defer f.Close()

	var out []LedgerRecord
	r := bufio.NewReaderSize(f, 64*1024)
	for {
		line, err := r.ReadBytes('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("read ledger line: %w", err)
		}
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			if errors.Is(err, io.EOF) {
				break
			}
			continue
		}
		var rec LedgerRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			return nil, fmt.Errorf("parse ledger line: %w", err)
		}
		out = append(out, rec)
		if errors.Is(err, io.EOF) {
			break
		}
	}
	return out, nil
}

func (q *Queue) Path() string {
	if q == nil {
		return ""
	}
	return q.path
}

func (q *Queue) lockPath() (string, error) {
	if q == nil || strings.TrimSpace(q.path) == "" {
		return "", fmt.Errorf("queue is not initialized")
	}
	return q.path + ".lock", nil
}

func (q *Queue) withLock(fn func() error) error {
	lockPath, err := q.lockPath()
	if err != nil {
		return err
	}
	return withFileLock(lockPath, fn)
}

func withFileLock(lockPath string, fn func() error) error {
	if strings.TrimSpace(lockPath) == "" {
		return fmt.Errorf("lock path is required")
	}
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o700); err != nil {
		return fmt.Errorf("create queue lock dir: %w", err)
	}
	const attempts = 200
	for attempt := 0; attempt < attempts; attempt++ {
		f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_, _ = fmt.Fprintf(f, "pid=%d\ncreated_at=%s\n", os.Getpid(), time.Now().UTC().Format(time.RFC3339Nano))
			_ = f.Close()
			defer func() { _ = os.Remove(lockPath) }()
			stopHeartbeat := startQueueLockHeartbeat(lockPath)
			defer stopHeartbeat()
			return fn()
		}
		if !os.IsExist(err) {
			return fmt.Errorf("create queue lock: %w", err)
		}
		if queueLockIsStale(lockPath, time.Now().UTC()) {
			_ = os.Remove(lockPath)
			continue
		}
		time.Sleep(5 * time.Millisecond)
	}
	return fmt.Errorf("queue lock is busy: %s", lockPath)
}

func startQueueLockHeartbeat(lockPath string) func() {
	if queueLockHeartbeatInterval <= 0 {
		return func() {}
	}
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(queueLockHeartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				now := time.Now().UTC()
				_ = os.Chtimes(lockPath, now, now)
			case <-stop:
				return
			}
		}
	}()
	return func() {
		close(stop)
		<-done
	}
}

func queueLockIsStale(lockPath string, now time.Time) bool {
	info, err := os.Stat(lockPath)
	if err != nil {
		return false
	}
	return now.Sub(info.ModTime()) > queueLockStaleAfter
}

func (q *Queue) List() ([]QueueItem, error) {
	var out []QueueItem
	err := q.withLock(func() error {
		items, err := q.listUnlocked()
		if err != nil {
			return err
		}
		out = items
		return nil
	})
	return out, err
}

func (q *Queue) listUnlocked() ([]QueueItem, error) {
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
		line, readErr := r.ReadBytes('\n')
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return nil, fmt.Errorf("read queue line: %w", readErr)
		}
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			if errors.Is(readErr, io.EOF) {
				break
			}
			continue
		}
		var it QueueItem
		if err := json.Unmarshal(line, &it); err != nil {
			_ = q.appendCorruptLine(line)
			if errors.Is(readErr, io.EOF) {
				break
			}
			continue
		}
		out = append(out, it)
		if errors.Is(readErr, io.EOF) {
			break
		}
	}
	return out, nil
}

func (q *Queue) appendCorruptLine(line []byte) error {
	if q == nil || strings.TrimSpace(q.path) == "" || len(bytes.TrimSpace(line)) == 0 {
		return nil
	}
	path := fmt.Sprintf("%s.corrupt-line.%d", q.path, time.Now().UTC().UnixNano())
	return os.WriteFile(path, append(bytes.TrimSpace(line), '\n'), 0o600)
}

func (q *Queue) Enqueue(ev HookEvent) error {
	return q.withLock(func() error {
		return q.enqueueUnlocked(ev)
	})
}

func (q *Queue) enqueueUnlocked(ev HookEvent) error {
	if q == nil || strings.TrimSpace(q.path) == "" {
		return fmt.Errorf("queue is not initialized")
	}
	if strings.TrimSpace(ev.EventID) == "" {
		return fmt.Errorf("event_id is required")
	}

	items, err := q.listUnlocked()
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
	return q.withLock(func() error {
		return q.rewriteUnlocked(items)
	})
}

func (q *Queue) rewriteUnlocked(items []QueueItem) error {
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
	root := filepath.Join(attributionlocal.AttributionRootDir(), "legacy-session-queue")
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
		path, err := queuePath(sessionID)
		if err != nil {
			return nil, err
		}
		info, err := os.Stat(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("stat queue file: %w", err)
		}
		hasPending := info.Size() > 0
		if hasPending {
			out = append(out, sessionID)
		}
	}
	sort.Strings(out)
	return out, nil
}

func PendingWorkspaceIDs() ([]string, error) {
	root := filepath.Join(attributionlocal.AttributionRootDir(), "workspaces")
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read workspace root: %w", err)
	}

	var out []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		workspaceID := strings.TrimSpace(entry.Name())
		if workspaceID == "" {
			continue
		}
		p, err := workspaceQueuePath(workspaceID)
		if err != nil {
			return nil, err
		}
		info, err := os.Stat(p)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("stat workspace queue: %w", err)
		}
		if info.Size() > 0 {
			out = append(out, workspaceID)
		}
	}
	sort.Strings(out)
	return out, nil
}
