# ae-cli Reporting Durability Hardening Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make ae-cli reporting durable enough that every locally captured valid hook/checkpoint/tool-usage event is eventually uploaded or remains visible in a retryable/dead-letter state instead of being silently lost.

**Architecture:** Keep git hooks fail-open for `git commit`, but make the fail-open path durable: queue writes are locked, enqueue failures are visible, unresolved repo eligibility gets a recoverable queue, binding mismatches stay pending, corrupt state is quarantined, and permanent validation failures move to dead-letter without blocking later valid events. Backend APIs stay unchanged; this plan hardens the client-side state machine and diagnostics around existing checkpoint and tool-usage endpoints.

**Tech Stack:** Go CLI with Cobra, local JSON/JSONL state under `~/.ae-cli/state`, existing `ae-cli/internal/hooks` queues/tasks, `ae-cli/internal/attributionlocal` spool/replay, backend HTTP client tests with `httptest`, and `go test`.

**Status:** In progress. Baseline stale eligibility durability fix is complete; reporting durability hardening tasks below remain unchecked until each implementation and verification step runs.

---

## Durability Boundary

This plan does not promise physical 100 percent delivery under power loss, disk corruption, a permanently unwritable `HOME`, revoked auth, or a backend that permanently rejects valid data. The hard contract after this work is:

1. If a hook/checkpoint/tool-usage event is captured and local state is writable, it is not silently dropped.
2. A transient backend/network failure leaves retryable local state.
3. A permanent backend validation/auth failure leaves a visible dead-letter record with the original event and error.
4. Events from a different server/auth/repo binding remain pending under their original binding instead of being deleted by the current binding.

## File Map

- Modify: `ae-cli/internal/hooks/queue.go`
  - Add per-queue file locking, unlocked list/enqueue/rewrite helpers, and corruption-tolerant queue reads.
- Modify: `ae-cli/internal/hooks/queue_test.go`
  - Cover enqueue/rewrite races and corrupt-line quarantine.
- Modify: `ae-cli/internal/hooks/handler.go`
  - Use locked queue flush, preserve mismatched queue items, surface enqueue failures, trigger sync from post-rewrite, and flush unresolved hook events.
- Modify: `ae-cli/internal/hooks/handler_test.go`
  - Cover locked concurrent enqueue, mismatch preservation, post-rewrite sync scheduling, and visible enqueue failure.
- Create: `ae-cli/internal/hooks/unresolved_queue.go`
  - Store checkpoint-like hook events when repo eligibility cannot be resolved yet, then resolve and replay them after a stable binding exists.
- Create: `ae-cli/internal/hooks/unresolved_queue_test.go`
  - Cover unresolved post-commit enqueue, dedupe by remote/workspace/commit, and replay after repo_config_id is known.
- Modify: `ae-cli/cmd/hook.go`
  - Enqueue unresolved post-commit events when `resolveHookExecutionContext` cannot produce a stable execution context.
- Modify: `ae-cli/cmd/hook_test.go`
  - Cover first-run resolve timeout with no eligibility cache still leaving a durable unresolved event.
- Modify: `ae-cli/internal/attributionlocal/sync.go`
  - Preserve mismatched spooled events, quarantine corrupt spool, and dead-letter permanent single-event failures while continuing later uploads.
- Modify: `ae-cli/internal/attributionlocal/sync_test.go`
  - Cover mismatch preservation, corrupt spool quarantine, and permanent bad-event dead-letter without blocking following valid events.
- Modify: `ae-cli/internal/client/client.go`
  - Return structured HTTP status errors for tool-usage uploads.
- Modify: `ae-cli/internal/client/client_test.go`
  - Cover structured 422 and retryable 502 behavior.
- Modify: `ae-cli/cmd/sync.go`
  - Print dead-letter/unresolved counts in `ae-cli sync status`.
- Modify: `ae-cli/cmd/sync_test.go`
  - Cover status output for unresolved and dead-letter state.
- Modify: `docs/architecture.md`
  - Update current reporting durability contract.
- Modify: `docs/superpowers/specs/2026-05-26-ae-cli-post-commit-async-attribution-sync-design.md`
  - Update active post-commit/replay contract.

---

### Task 1: Lock Workspace Hook Queue and Preserve Concurrent Enqueue

**Files:**
- Modify: `ae-cli/internal/hooks/queue.go`
- Modify: `ae-cli/internal/hooks/queue_test.go`

- [x] **Step 1: Write the failing race-preservation test**

Add this test to `ae-cli/internal/hooks/queue_test.go`:

```go
func TestQueueLockedRewriteDoesNotDropConcurrentEnqueue(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	q, err := NewWorkspaceQueue("ws-locked")
	if err != nil {
		t.Fatalf("NewWorkspaceQueue: %v", err)
	}
	first := HookEvent{Kind: "post-commit", EventID: "evt-first", WorkspaceID: "ws-locked", CommitSHA: "first"}
	second := HookEvent{Kind: "post-commit", EventID: "evt-second", WorkspaceID: "ws-locked", CommitSHA: "second"}
	if err := q.Enqueue(first); err != nil {
		t.Fatalf("Enqueue(first): %v", err)
	}

	enqueueStarted := make(chan struct{})
	enqueueDone := make(chan error, 1)
	err = q.withLock(func() error {
		items, err := q.listUnlocked()
		if err != nil {
			return err
		}
		if len(items) != 1 || items[0].Event.EventID != "evt-first" {
			t.Fatalf("locked list = %+v, want first event", items)
		}
		go func() {
			close(enqueueStarted)
			enqueueDone <- q.Enqueue(second)
		}()
		<-enqueueStarted
		time.Sleep(50 * time.Millisecond)
		return q.rewriteUnlocked(nil)
	})
	if err != nil {
		t.Fatalf("withLock rewrite: %v", err)
	}
	if err := <-enqueueDone; err != nil {
		t.Fatalf("concurrent Enqueue(second): %v", err)
	}

	items, err := q.List()
	if err != nil {
		t.Fatalf("List after locked rewrite: %v", err)
	}
	if len(items) != 1 || items[0].Event.EventID != "evt-second" {
		t.Fatalf("items after locked rewrite = %+v, want only concurrent second event", items)
	}
}
```

- [x] **Step 2: Run the test and verify it fails**

Run:

```bash
cd ae-cli
go test ./internal/hooks -run TestQueueLockedRewriteDoesNotDropConcurrentEnqueue -count=1
```

Expected: FAIL because `Queue` has no `withLock`, `listUnlocked`, or `rewriteUnlocked` methods.

- [x] **Step 3: Implement queue locking and unlocked helpers**

Modify `ae-cli/internal/hooks/queue.go`. Add these imports if missing:

```go
import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)
```

Add these helpers near `func (q *Queue) Path()`:

```go
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

func queueLockIsStale(lockPath string, now time.Time) bool {
	info, err := os.Stat(lockPath)
	if err != nil {
		return false
	}
	return now.Sub(info.ModTime()) > 30*time.Second
}
```

Replace `List`, `Enqueue`, and `rewrite` with locked public methods and unlocked implementations:

```go
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
```

- [x] **Step 4: Make `flushWorkspace` hold the queue lock across list and rewrite**

Modify `ae-cli/internal/hooks/handler.go`:

```go
func (h *Handler) flushWorkspace(ctx context.Context, execCtx ExecutionContext) error {
	q, err := NewWorkspaceQueue(execCtx.WorkspaceID)
	if err != nil {
		return err
	}
	return q.withLock(func() error {
		items, err := q.listUnlocked()
		if err != nil {
			return err
		}
		if len(items) == 0 {
			return nil
		}

		var keep []QueueItem
		for _, it := range items {
			now := time.Now().UTC()
			if !hookEventMatchesContext(it.Event, execCtx) {
				keep = append(keep, it)
				_ = AppendLedger(execCtx.WorkspaceID, LedgerRecord{
					Kind:         ledgerKind(it.Event.Kind),
					DedupeKey:    it.Event.EventID,
					ServerURL:    execCtx.ServerURL,
					AuthSubject:  execCtx.AuthSubject,
					RepoConfigID: execCtx.RepoConfigID,
					RepoKey:      execCtx.RepoKey,
					WorkspaceID:  execCtx.WorkspaceID,
					Status:       "deferred",
					AttemptCount: 1,
					AttemptedAt:  now,
					LastError:    "context mismatch",
				})
				continue
			}
			if h == nil || h.uploader == nil {
				keep = append(keep, it)
				continue
			}
			if err := h.uploader.UploadHookEvent(ctx, it.Event); err != nil {
				_ = AppendLedger(execCtx.WorkspaceID, LedgerRecord{
					Kind:         ledgerKind(it.Event.Kind),
					DedupeKey:    it.Event.EventID,
					ServerURL:    execCtx.ServerURL,
					AuthSubject:  execCtx.AuthSubject,
					RepoConfigID: execCtx.RepoConfigID,
					RepoKey:      execCtx.RepoKey,
					WorkspaceID:  execCtx.WorkspaceID,
					Status:       "failed",
					AttemptCount: 1,
					AttemptedAt:  now,
					LastError:    err.Error(),
				})
				keep = append(keep, it)
				continue
			}
			_ = AppendLedger(execCtx.WorkspaceID, LedgerRecord{
				Kind:         ledgerKind(it.Event.Kind),
				DedupeKey:    it.Event.EventID,
				ServerURL:    execCtx.ServerURL,
				AuthSubject:  execCtx.AuthSubject,
				RepoConfigID: execCtx.RepoConfigID,
				RepoKey:      execCtx.RepoKey,
				WorkspaceID:  execCtx.WorkspaceID,
				Status:       "uploaded",
				AttemptCount: 1,
				AttemptedAt:  now,
				UploadedAt:   &now,
			})
		}
		return q.rewriteUnlocked(keep)
	})
}
```

- [x] **Step 5: Run focused tests**

Run:

```bash
cd ae-cli
go test ./internal/hooks -run 'TestQueueLockedRewriteDoesNotDropConcurrentEnqueue|TestQueuePersistsAndDedupByEventID|TestPostCommitResolvedLeavesQueuedEventsForAsyncRunner' -count=1
```

Expected: PASS.

- [x] **Step 6: Commit**

```bash
git add ae-cli/internal/hooks/queue.go ae-cli/internal/hooks/queue_test.go ae-cli/internal/hooks/handler.go
git commit -m "fix(ae-cli): lock hook replay queue"
```

---

### Task 2: Make Enqueue Failures Visible and Durable

**Files:**
- Modify: `ae-cli/internal/hooks/handler.go`
- Modify: `ae-cli/internal/hooks/handler_test.go`

- [x] **Step 1: Write the failing stderr test**

Add this test to `ae-cli/internal/hooks/handler_test.go`:

```go
func TestPostCommitResolvedReportsQueueFailure(t *testing.T) {
	repo := initRepoWithCommit2(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	execCtx := resolvedContextForRepo(t, repo)

	workspaceDir := filepath.Join(attributionlocal.AttributionRootDir(), "workspaces", execCtx.WorkspaceID)
	if err := os.MkdirAll(workspaceDir, 0o700); err != nil {
		t.Fatalf("mkdir workspace dir: %v", err)
	}
	queuePath, err := workspaceQueuePath(execCtx.WorkspaceID)
	if err != nil {
		t.Fatalf("workspaceQueuePath: %v", err)
	}
	if err := os.MkdirAll(queuePath, 0o700); err != nil {
		t.Fatalf("mkdir queue path as directory: %v", err)
	}

	var stderr bytes.Buffer
	oldStderr := hookStderr
	hookStderr = &stderr
	t.Cleanup(func() { hookStderr = oldStderr })

	u := &fakeUploader{err: errors.New("upload failed")}
	if err := NewHandler(u).PostCommitResolved(context.Background(), execCtx); err != nil {
		t.Fatalf("PostCommitResolved: %v", err)
	}
	if !strings.Contains(stderr.String(), "failed to queue checkpoint event") {
		t.Fatalf("stderr = %q, want queue failure warning", stderr.String())
	}
}
```

- [x] **Step 2: Run the test and verify it fails**

Run:

```bash
cd ae-cli
go test ./internal/hooks -run TestPostCommitResolvedReportsQueueFailure -count=1
```

Expected: FAIL because `hookStderr` does not exist and enqueue errors are ignored.

- [x] **Step 3: Add stderr injection and queue warning helper**

Modify `ae-cli/internal/hooks/handler.go`. Add package-level writer:

```go
var hookStderr io.Writer = os.Stderr
```

Add helper:

```go
func queueForReplayOrWarn(execCtx ExecutionContext, ev HookEvent) {
	if err := enqueueForReplay(execCtx, ev); err != nil {
		fmt.Fprintf(hookStderr, "ae-cli: failed to queue %s event for replay: %v\n", ledgerKind(ev.Kind), err)
	}
}
```

Replace ignored enqueue calls:

```go
if h == nil || h.uploader == nil {
	queueForReplayOrWarn(execCtx, ev)
} else if err := h.uploader.UploadHookEvent(ctx, ev); err != nil {
	queueForReplayOrWarn(execCtx, ev)
}
```

For post-rewrite, replace both ignored enqueue calls with `queueForReplayOrWarn(execCtx, ev)`.

- [x] **Step 4: Run focused tests**

Run:

```bash
cd ae-cli
go test ./internal/hooks -run 'TestPostCommitResolvedReportsQueueFailure|TestPostCommitResolvedQueuesOnlyWithStableBinding|TestPostRewriteResolvedQueuesEventsWhenUploadFails' -count=1
```

Expected: PASS.

- [x] **Step 5: Commit**

```bash
git add ae-cli/internal/hooks/handler.go ae-cli/internal/hooks/handler_test.go
git commit -m "fix(ae-cli): report hook queue failures"
```

---

### Task 3: Add Unresolved Hook Queue for First-Run Resolve Failure

**Files:**
- Create: `ae-cli/internal/hooks/unresolved_queue.go`
- Create: `ae-cli/internal/hooks/unresolved_queue_test.go`
- Modify: `ae-cli/cmd/hook.go`
- Modify: `ae-cli/cmd/hook_test.go`
- Modify: `ae-cli/internal/hooks/handler.go`

- [x] **Step 1: Write unresolved queue tests**

Create `ae-cli/internal/hooks/unresolved_queue_test.go`:

```go
package hooks

import (
	"testing"
	"time"
)

func TestUnresolvedQueuePersistsAndDedupesPostCommit(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	ev := UnresolvedHookEvent{
		Kind:           "post-commit",
		RemoteURL:      "https://github.com/acme/repo.git",
		RepoKey:        "github.com/acme/repo",
		WorkspaceID:    "ws-unresolved",
		ServerURL:      "https://ae.example.com",
		AuthSubject:    "user:123",
		CommitSHA:      "abc123",
		BranchSnapshot: "main",
		HeadSnapshot:   "abc123",
		CapturedAt:     time.Date(2026, 6, 2, 9, 0, 0, 0, time.UTC).Format(time.RFC3339),
	}
	if err := EnqueueUnresolvedHookEvent(ev); err != nil {
		t.Fatalf("EnqueueUnresolvedHookEvent first: %v", err)
	}
	if err := EnqueueUnresolvedHookEvent(ev); err != nil {
		t.Fatalf("EnqueueUnresolvedHookEvent duplicate: %v", err)
	}

	items, err := ListUnresolvedHookEvents()
	if err != nil {
		t.Fatalf("ListUnresolvedHookEvents: %v", err)
	}
	if len(items) != 1 || items[0].CommitSHA != "abc123" {
		t.Fatalf("items = %+v, want one unresolved commit", items)
	}
}
```

- [x] **Step 2: Run unresolved queue test and verify it fails**

Run:

```bash
cd ae-cli
go test ./internal/hooks -run TestUnresolvedQueuePersistsAndDedupesPostCommit -count=1
```

Expected: FAIL because `UnresolvedHookEvent` and queue functions do not exist.

- [x] **Step 3: Implement unresolved queue**

Create `ae-cli/internal/hooks/unresolved_queue.go`:

```go
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
	CapturedAt      string         `json:"captured_at"`
	AgentSnapshot   map[string]any `json:"agent_snapshot,omitempty"`
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
```

- [x] **Step 4: Add command-level unresolved enqueue test**

Add this test to `ae-cli/cmd/hook_test.go`:

```go
func TestHookPostCommitQueuesUnresolvedWhenInitialResolveTimesOut(t *testing.T) {
	repo := initRepoWithCommitForCmdTests(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusGatewayTimeout)
	}))
	defer srv.Close()
	writeTestTokenForServer(t, home, srv.URL, "user:123")
	withHookAPIClient(t, srv.URL, "test-access-token")

	origResolveTimeout := hookEligibilityResolveTimeout
	hookEligibilityResolveTimeout = 25 * time.Millisecond
	t.Cleanup(func() { hookEligibilityResolveTimeout = origResolveTimeout })

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	defer func() { _ = os.Chdir(wd) }()
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("Chdir(repo): %v", err)
	}

	if err := hookPostCommitCmd.RunE(hookPostCommitCmd, nil); err != nil {
		t.Fatalf("hook post-commit RunE: %v", err)
	}
	items, err := hooks.ListUnresolvedHookEvents()
	if err != nil {
		t.Fatalf("ListUnresolvedHookEvents: %v", err)
	}
	if len(items) != 1 || items[0].RemoteURL != "https://github.com/acme/repo.git" || items[0].CommitSHA == "" {
		t.Fatalf("unresolved items = %+v, want unresolved post-commit", items)
	}
}
```

- [x] **Step 5: Run command test and verify it fails**

Run:

```bash
cd ae-cli
go test ./cmd -run TestHookPostCommitQueuesUnresolvedWhenInitialResolveTimesOut -count=1
```

Expected: FAIL because `hookPostCommitCmd` does not enqueue unresolved events.

- [x] **Step 6: Enqueue unresolved post-commit when execution context is unavailable**

Modify `ae-cli/cmd/hook.go` in `hookPostCommitCmd.RunE`:

```go
execCtx, ok := resolveHookExecutionContext(ctx, gitCtx)
if !ok {
	queueUnresolvedPostCommit(ctx, gitCtx)
	return nil
}
```

Add helper in `ae-cli/cmd/hook.go`:

```go
func queueUnresolvedPostCommit(ctx context.Context, gitCtx *hooks.GitContext) {
	if gitCtx == nil {
		return
	}
	repoRoot := strings.TrimSpace(gitCtx.RepoRoot)
	if repoRoot == "" {
		return
	}
	head, err := hooks.GitOutputForHook(repoRoot, "rev-parse", "HEAD")
	if err != nil {
		return
	}
	tokenPath, _ := auth.DefaultTokenPath()
	tf := readTokenFile(tokenPath)
	serverURL := ""
	authSubject := ""
	if tf != nil {
		serverURL = tf.ServerURL
		if cfg != nil && cfg.Server.URL != "" {
			serverURL = cfg.Server.URL
		}
		authSubject = tf.StableAuthSubject()
	}
	ev := hooks.UnresolvedHookEvent{
		Kind:           "post-commit",
		RemoteURL:      gitCtx.RemoteURL,
		RepoKey:        gitCtx.RepoKey,
		WorkspaceID:    gitCtx.WorkspaceID,
		ServerURL:      serverURL,
		AuthSubject:    authSubject,
		CommitSHA:      head,
		ParentSHAs:     hooks.ParentSHAsForHook(repoRoot),
		BranchSnapshot: firstNonEmpty(gitCtx.Branch, hooks.BranchSnapshotForHook(repoRoot)),
		HeadSnapshot:   head,
		CapturedAt:     time.Now().UTC().Format(time.RFC3339),
	}
	if err := hooks.EnqueueUnresolvedHookEvent(ev); err != nil {
		fmt.Fprintf(os.Stderr, "ae-cli: failed to queue unresolved checkpoint event: %v\n", err)
	}
}
```

Add exported wrappers in `ae-cli/internal/hooks/handler.go` so command code does not duplicate private git helpers:

```go
func GitOutputForHook(dir string, args ...string) (string, error) {
	return gitOutput(dir, args...)
}

func ParentSHAsForHook(repoRoot string) []string {
	return parentSHAs(repoRoot)
}

func BranchSnapshotForHook(repoRoot string) string {
	return branchSnapshot(repoRoot)
}
```

- [x] **Step 6a: Write failing unresolved replay test**

Add a handler-level test proving unresolved events are uploaded and removed once a stable execution context exists.

- [x] **Step 7: Replay unresolved events after stable context exists**

Add to `ae-cli/internal/hooks/handler.go`:

```go
func (h *Handler) FlushUnresolvedResolved(ctx context.Context, execCtx ExecutionContext) error {
	if !execCtx.hasStableReplayBinding() {
		return nil
	}
	items, err := ListUnresolvedHookEvents()
	if err != nil {
		return err
	}
	if len(items) == 0 {
		return nil
	}
	var keep []UnresolvedHookEvent
	for _, item := range items {
		if strings.TrimSpace(item.WorkspaceID) != strings.TrimSpace(execCtx.WorkspaceID) {
			keep = append(keep, item)
			continue
		}
		if strings.TrimSpace(item.RepoKey) != "" && strings.TrimSpace(item.RepoKey) != strings.TrimSpace(execCtx.RepoKey) {
			keep = append(keep, item)
			continue
		}
		eventID, err := CheckpointEventID(eventIDRepoHint(execCtx), item.CommitSHA)
		if err != nil {
			keep = append(keep, item)
			continue
		}
		ev := HookEvent{
			Kind:           "post-commit",
			EventID:        eventID,
			WorkspaceID:    execCtx.WorkspaceID,
			ServerURL:      execCtx.ServerURL,
			AuthSubject:    execCtx.AuthSubject,
			RepoConfigID:   execCtx.RepoConfigID,
			RepoKey:        execCtx.RepoKey,
			RepoFullName:   firstNonEmptyValue(execCtx.RepoFullName, execCtx.RepoKey),
			BindingSource:  "unbound",
			AgentSnapshot:  item.AgentSnapshot,
			CommitSHA:      item.CommitSHA,
			ParentSHAs:     item.ParentSHAs,
			BranchSnapshot: item.BranchSnapshot,
			HeadSnapshot:   item.HeadSnapshot,
			CapturedAt:     item.CapturedAt,
		}
		if h == nil || h.uploader == nil {
			keep = append(keep, item)
			continue
		}
		if err := h.uploader.UploadHookEvent(ctx, ev); err != nil {
			keep = append(keep, item)
			continue
		}
	}
	return SaveUnresolvedHookEvents(keep)
}
```

Modify `RunPendingSyncTask` in `ae-cli/internal/hooks/background_runner.go` before `h.FlushResolved(...)`:

```go
if err := h.FlushUnresolvedResolved(ctx, execCtx); err != nil {
	_ = MarkSyncTaskFailure(task, time.Now().UTC(), err)
	return err
}
```

- [x] **Step 8: Run focused tests**

Run:

```bash
cd ae-cli
go test ./cmd -run TestHookPostCommitQueuesUnresolvedWhenInitialResolveTimesOut -count=1
go test ./internal/hooks -run 'TestUnresolvedQueuePersistsAndDedupesPostCommit|TestPostCommitResolvedQueuesOnlyWithStableBinding' -count=1
```

Expected: PASS.

- [x] **Step 9: Commit**

```bash
git add ae-cli/cmd/hook.go ae-cli/cmd/hook_test.go ae-cli/internal/hooks/handler.go ae-cli/internal/hooks/background_runner.go ae-cli/internal/hooks/unresolved_queue.go ae-cli/internal/hooks/unresolved_queue_test.go
git commit -m "fix(ae-cli): queue unresolved hook checkpoints"
```

---

### Task 4: Keep Binding-Mismatched Events Pending Instead of Dropping

**Files:**
- Modify: `ae-cli/internal/hooks/handler_test.go`
- Modify: `ae-cli/internal/hooks/handler.go`
- Modify: `ae-cli/internal/attributionlocal/sync_test.go`
- Modify: `ae-cli/internal/attributionlocal/sync.go`

- [x] **Step 1: Change hook queue mismatch test to require preservation**

Modify `TestFlushResolvedSkipsContextMismatchAndWritesLedger` in `ae-cli/internal/hooks/handler_test.go`:

```go
if len(items) != 1 || items[0].Event.EventID != "evt-mismatch" {
	t.Fatalf("items after mismatch defer = %+v, want mismatched event preserved", items)
}
records, err := ReadLedger(execCtx.WorkspaceID)
if err != nil {
	t.Fatalf("ReadLedger: %v", err)
}
if len(records) != 1 || records[0].Status != "deferred" || records[0].DedupeKey != "evt-mismatch" {
	t.Fatalf("ledger records = %+v, want deferred mismatch", records)
}
```

- [x] **Step 2: Run hook mismatch test and verify it fails**

Run:

```bash
cd ae-cli
go test ./internal/hooks -run TestFlushResolvedSkipsContextMismatchAndWritesLedger -count=1
```

Expected: FAIL because current code removes mismatched queue items and records `skipped`.

- [x] **Step 3: Preserve mismatched hook queue items**

If Task 1 has not already applied this, modify the mismatch branch in `flushWorkspace`:

```go
if !hookEventMatchesContext(it.Event, execCtx) {
	keep = append(keep, it)
	_ = AppendLedger(execCtx.WorkspaceID, LedgerRecord{
		Kind:         ledgerKind(it.Event.Kind),
		DedupeKey:    it.Event.EventID,
		ServerURL:    execCtx.ServerURL,
		AuthSubject:  execCtx.AuthSubject,
		RepoConfigID: execCtx.RepoConfigID,
		RepoKey:      execCtx.RepoKey,
		WorkspaceID:  execCtx.WorkspaceID,
		Status:       "deferred",
		AttemptCount: 1,
		AttemptedAt:  now,
		LastError:    "context mismatch",
	})
	continue
}
```

- [x] **Step 4: Change tool-usage spool mismatch test to require preservation**

Modify `TestSync_RunSkipsSpooledEventsFromDifferentBinding` in `ae-cli/internal/attributionlocal/sync_test.go`:

```go
if len(remaining) != 1 || remaining[0].DedupeKey != "stale-binding" {
	t.Fatalf("remaining spool = %+v, want stale mismatched event preserved", remaining)
}
```

Modify `TestSync_RunWritesSkippedLedgerForMismatchedSpooledEvents` expected status:

```go
if rec.Kind != "tool_usage" || rec.DedupeKey != "stale-binding" || rec.Status != "deferred" || rec.LastError != "context mismatch" {
	t.Fatalf("ledger = %+v, want deferred tool_usage context mismatch", rec)
}
```

- [x] **Step 5: Run tool-usage mismatch tests and verify they fail**

Run:

```bash
cd ae-cli
go test ./internal/attributionlocal -run 'TestSync_RunSkipsSpooledEventsFromDifferentBinding|TestSync_RunWritesSkippedLedgerForMismatchedSpooledEvents' -count=1
```

Expected: FAIL because current replay removes mismatched events and records `skipped`.

- [x] **Step 6: Preserve mismatched spooled events**

Modify `ae-cli/internal/attributionlocal/sync.go` inside `replay`:

```go
	var deferred []LocalToolUsageEvent
	for _, ev := range spooled {
		ev = normalizeObservedWindow(ev)
		if filterByBinding && !eventMatchesRunOptions(ev, opts) {
			deferred = append(deferred, ev)
			_ = appendToolUsageLedger(opts.WorkspaceID, toolUsageLedgerRecord{
				Version:      1,
				Kind:         "tool_usage",
				DedupeKey:    ev.DedupeKey,
				ServerURL:    opts.ServerURL,
				AuthSubject:  opts.AuthSubject,
				RepoConfigID: opts.RepoConfigID,
				RepoKey:      opts.RepoKey,
				WorkspaceID:  opts.WorkspaceID,
				Status:       "deferred",
				AttemptCount: 1,
				AttemptedAt:  time.Now().UTC(),
				LastError:    "context mismatch",
			})
			continue
		}
		candidates = append(candidates, ev)
	}
	persistRemaining := func(uploaded int) error {
		remaining := append([]LocalToolUsageEvent{}, deferred...)
		remaining = append(remaining, candidates[uploaded:]...)
		if len(remaining) == 0 {
			return clearSpooledEvents(e.spoolPath)
		}
		return saveSpooledEvents(e.spoolPath, remaining)
	}
```

- [x] **Step 7: Run focused tests**

Run:

```bash
cd ae-cli
go test ./internal/hooks -run TestFlushResolvedSkipsContextMismatchAndWritesLedger -count=1
go test ./internal/attributionlocal -run 'TestSync_RunSkipsSpooledEventsFromDifferentBinding|TestSync_RunWritesSkippedLedgerForMismatchedSpooledEvents' -count=1
```

Expected: PASS.

- [x] **Step 8: Commit**

```bash
git add ae-cli/internal/hooks/handler.go ae-cli/internal/hooks/handler_test.go ae-cli/internal/attributionlocal/sync.go ae-cli/internal/attributionlocal/sync_test.go
git commit -m "fix(ae-cli): preserve mismatched reporting backlog"
```

---

### Task 5: Quarantine Corrupt Queue and Spool State

**Files:**
- Modify: `ae-cli/internal/hooks/queue.go`
- Modify: `ae-cli/internal/hooks/queue_test.go`
- Modify: `ae-cli/internal/attributionlocal/sync.go`
- Modify: `ae-cli/internal/attributionlocal/sync_test.go`

- [x] **Step 1: Write corrupt queue test**

Add to `ae-cli/internal/hooks/queue_test.go`:

```go
func TestQueueListQuarantinesCorruptLineAndKeepsValidEvents(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	q, err := NewWorkspaceQueue("ws-corrupt-queue")
	if err != nil {
		t.Fatalf("NewWorkspaceQueue: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(q.Path()), 0o700); err != nil {
		t.Fatalf("mkdir queue dir: %v", err)
	}
	body := []byte(`{"event":{"kind":"post-commit","event_id":"evt-good","workspace_id":"ws-corrupt-queue","commit_sha":"abc"}}` + "\n" + `{not-json}` + "\n")
	if err := os.WriteFile(q.Path(), body, 0o600); err != nil {
		t.Fatalf("write queue: %v", err)
	}

	items, err := q.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 1 || items[0].Event.EventID != "evt-good" {
		t.Fatalf("items = %+v, want valid event preserved", items)
	}
	matches, err := filepath.Glob(q.Path() + ".corrupt-line.*")
	if err != nil {
		t.Fatalf("glob corrupt lines: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("corrupt line backups = %+v, want one", matches)
	}
}
```

- [x] **Step 2: Run corrupt queue test and verify it fails**

Run:

```bash
cd ae-cli
go test ./internal/hooks -run TestQueueListQuarantinesCorruptLineAndKeepsValidEvents -count=1
```

Expected: FAIL because queue parsing returns an error on the corrupt line.

- [x] **Step 3: Quarantine corrupt queue lines**

In `ae-cli/internal/hooks/queue.go`, replace the JSON unmarshal failure branch in `listUnlocked`:

```go
if err := json.Unmarshal(line, &it); err != nil {
	_ = q.appendCorruptLine(line)
	if errors.Is(err, io.EOF) {
		break
	}
	continue
}
```

Add helper:

```go
func (q *Queue) appendCorruptLine(line []byte) error {
	if q == nil || strings.TrimSpace(q.path) == "" || len(bytes.TrimSpace(line)) == 0 {
		return nil
	}
	path := fmt.Sprintf("%s.corrupt-line.%d", q.path, time.Now().UTC().UnixNano())
	return os.WriteFile(path, append(bytes.TrimSpace(line), '\n'), 0o600)
}
```

- [x] **Step 4: Write corrupt spool test**

Add to `ae-cli/internal/attributionlocal/sync_test.go`:

```go
func TestSync_RunQuarantinesCorruptSpoolAndContinuesScan(t *testing.T) {
	fixture := buildAttributionFixture(t)
	workspaceID, err := mustWorkspaceID(fixture.WorkspaceRoot)
	if err != nil {
		t.Fatalf("mustWorkspaceID: %v", err)
	}
	spoolPath := filepath.Join(AttributionRootDir(), "workspaces", workspaceID, "spool.json")
	if err := os.MkdirAll(filepath.Dir(spoolPath), 0o700); err != nil {
		t.Fatalf("mkdir spool dir: %v", err)
	}
	if err := os.WriteFile(spoolPath, []byte("{not-json"), 0o600); err != nil {
		t.Fatalf("write corrupt spool: %v", err)
	}

	client := &syncBackendClientStub{}
	engine := NewSyncEngine(client)
	if err := engine.Run(context.Background(), RunOptions{
		WorkspaceRoot: fixture.WorkspaceRoot,
		WorkspaceID:   workspaceID,
		ServerURL:     "https://ae.example.com",
		AuthSubject:   "user:123",
		RepoConfigID:  123,
		RepoKey:       "github.com/acme/repo",
		DurableReplay: true,
		ManagedUpload: true,
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	matches, err := filepath.Glob(spoolPath + ".corrupt.*")
	if err != nil {
		t.Fatalf("glob corrupt spool: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("corrupt spool backups = %+v, want one", matches)
	}
}
```

- [x] **Step 5: Run corrupt spool test and verify it fails**

Run:

```bash
cd ae-cli
go test ./internal/attributionlocal -run TestSync_RunQuarantinesCorruptSpoolAndContinuesScan -count=1
```

Expected: FAIL because corrupt spool currently aborts `Run`.

- [x] **Step 6: Quarantine corrupt spool**

Modify `loadSpooledEvents` in `ae-cli/internal/attributionlocal/sync.go`:

```go
func loadSpooledEvents(path string) ([]LocalToolUsageEvent, error) {
	if path == "" {
		return nil, nil
	}
	var out []LocalToolUsageEvent
	if err := LoadJSON(path, &out); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		if strings.Contains(err.Error(), "unmarshal json:") {
			if qerr := quarantineCorruptSpool(path); qerr != nil {
				return nil, qerr
			}
			return nil, nil
		}
		return nil, err
	}
	return out, nil
}

func quarantineCorruptSpool(path string) error {
	backup := fmt.Sprintf("%s.corrupt.%d", path, time.Now().UTC().UnixNano())
	if err := os.Rename(path, backup); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("quarantine corrupt spool: %w", err)
	}
	return nil
}
```

Ensure `sync.go` imports `strings`:

```go
import "strings"
```

- [x] **Step 7: Run focused tests**

Run:

```bash
cd ae-cli
go test ./internal/hooks -run TestQueueListQuarantinesCorruptLineAndKeepsValidEvents -count=1
go test ./internal/attributionlocal -run TestSync_RunQuarantinesCorruptSpoolAndContinuesScan -count=1
```

Expected: PASS.

- [x] **Step 8: Commit**

```bash
git add ae-cli/internal/hooks/queue.go ae-cli/internal/hooks/queue_test.go ae-cli/internal/attributionlocal/sync.go ae-cli/internal/attributionlocal/sync_test.go
git commit -m "fix(ae-cli): quarantine corrupt reporting state"
```

---

### Task 6: Dead-Letter Permanent Tool-Usage Failures and Continue

**Files:**
- Modify: `ae-cli/internal/client/client.go`
- Modify: `ae-cli/internal/client/client_test.go`
- Modify: `ae-cli/internal/attributionlocal/sync.go`
- Modify: `ae-cli/internal/attributionlocal/sync_test.go`

- [ ] **Step 1: Write structured HTTP error test**

Add to `ae-cli/internal/client/client_test.go`:

```go
func TestSendToolUsageEventReturnsHTTPStatusErrorForValidationFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"error":"bad event"}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "tok")
	err := c.SendToolUsageEvent(context.Background(), ToolUsageEventRequest{
		RepoConfigID:    123,
		Tool:            "codex",
		WorkspaceID:     "ws-1",
		ToolSessionID:   "sess-1",
		DedupeKey:       "bad-event",
		UsageUnit:       "token",
		ObservedStartAt: time.Now(),
		ObservedEndAt:   time.Now(),
	})
	var statusErr *HTTPStatusError
	if !errors.As(err, &statusErr) {
		t.Fatalf("error = %T %v, want HTTPStatusError", err, err)
	}
	if statusErr.StatusCode != http.StatusUnprocessableEntity || statusErr.Body == "" {
		t.Fatalf("statusErr = %+v, want 422 with body", statusErr)
	}
}
```

- [ ] **Step 2: Run client test and verify it fails**

Run:

```bash
cd ae-cli
go test ./internal/client -run TestSendToolUsageEventReturnsHTTPStatusErrorForValidationFailure -count=1
```

Expected: FAIL because `HTTPStatusError` does not exist.

- [ ] **Step 3: Implement structured HTTP status errors**

Modify `ae-cli/internal/client/client.go`:

```go
type HTTPStatusError struct {
	Endpoint   string
	StatusCode int
	Body       string
}

func (e *HTTPStatusError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("unexpected %s status %d: %s", e.Endpoint, e.StatusCode, e.Body)
}

func IsPermanentToolUsageError(err error) bool {
	var statusErr *HTTPStatusError
	if !errors.As(err, &statusErr) {
		return false
	}
	return statusErr.StatusCode == http.StatusBadRequest ||
		statusErr.StatusCode == http.StatusUnauthorized ||
		statusErr.StatusCode == http.StatusForbidden ||
		statusErr.StatusCode == http.StatusUnprocessableEntity
}
```

Add `errors` to imports. In `SendToolUsageEvent`, replace non-success status error creation:

```go
statusErr := &HTTPStatusError{Endpoint: "tool usage", StatusCode: resp.StatusCode, Body: string(respBody)}
lastErr = statusErr
if !isRetryableToolUsageStatus(resp.StatusCode) {
	return statusErr
}
```

In `SendToolUsageEvents`, replace batch non-success error creation:

```go
statusErr := &HTTPStatusError{Endpoint: "tool usage batch", StatusCode: resp.StatusCode, Body: string(respBody)}
if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed || resp.StatusCode == http.StatusBadRequest || resp.StatusCode == http.StatusUnprocessableEntity {
	return c.sendToolUsageEventsIndividually(ctx, reqs, statusErr)
}
lastErr = statusErr
if !isRetryableToolUsageStatus(resp.StatusCode) {
	return statusErr
}
```

- [ ] **Step 4: Write dead-letter replay test**

Add to `ae-cli/internal/attributionlocal/sync_test.go`:

```go
func TestSync_ReplayDeadLettersPermanentFailureAndContinues(t *testing.T) {
	fixture := setupSyncEngineWithSpool(t)
	fixture.Client.failOn = "spooled-dedupe-key"
	fixture.Client.failWith = &client.HTTPStatusError{Endpoint: "tool usage", StatusCode: http.StatusUnprocessableEntity, Body: "bad event"}
	second := LocalToolUsageEvent{
		Tool:            "codex",
		WorkspaceID:     "ws-1",
		ToolSessionID:   "conv-2",
		ToolEventID:     "resp-2",
		DedupeKey:       "good-after-bad",
		UsageUnit:       UsageUnitToken,
		RequestCount:    1,
		ObservedStartAt: jsonTime("2026-05-13T10:00:02Z"),
		ObservedEndAt:   jsonTime("2026-05-13T10:00:03Z"),
	}
	if err := appendSpooledEvents(fixture.Engine.spoolPath, []LocalToolUsageEvent{second}); err != nil {
		t.Fatalf("appendSpooledEvents(second): %v", err)
	}

	if err := fixture.Engine.Replay(context.Background(), fixture.WorkspaceRoot); err != nil {
		t.Fatalf("Replay: %v", err)
	}
	if !fixture.Client.SawUpload("good-after-bad") {
		t.Fatalf("uploads = %+v, want good event after bad event", fixture.Client.uploads)
	}
	remaining, err := loadSpooledEvents(fixture.Engine.spoolPath)
	if err != nil {
		t.Fatalf("loadSpooledEvents: %v", err)
	}
	if len(remaining) != 0 {
		t.Fatalf("remaining spool = %+v, want no retryable items", remaining)
	}
	deadLetters, err := loadToolUsageDeadLetters(filepath.Dir(fixture.Engine.spoolPath))
	if err != nil {
		t.Fatalf("loadToolUsageDeadLetters: %v", err)
	}
	if len(deadLetters) != 1 || deadLetters[0].Event.DedupeKey != "spooled-dedupe-key" {
		t.Fatalf("deadLetters = %+v, want failed spooled event", deadLetters)
	}
}
```

Update `syncBackendClientStub` in `ae-cli/internal/attributionlocal/test_helpers_test.go`:

```go
failWith error
```

Use it in both send methods:

```go
if s.failOn != "" && req.DedupeKey == s.failOn {
	if s.failWith != nil {
		return s.failWith
	}
	return fmt.Errorf("upload failed for %s", req.DedupeKey)
}
```

- [ ] **Step 5: Run dead-letter test and verify it fails**

Run:

```bash
cd ae-cli
go test ./internal/attributionlocal -run TestSync_ReplayDeadLettersPermanentFailureAndContinues -count=1
```

Expected: FAIL because dead-letter helpers and permanent-failure continuation do not exist.

- [ ] **Step 6: Implement dead-letter records and continuation**

Add to `ae-cli/internal/attributionlocal/sync.go`:

```go
type toolUsageDeadLetter struct {
	Version    int                 `json:"version"`
	Event      LocalToolUsageEvent `json:"event"`
	Error      string              `json:"error"`
	RecordedAt time.Time           `json:"recorded_at"`
}

func toolUsageDeadLetterPath(workspaceDir string) string {
	return filepath.Join(workspaceDir, "dead-letter-tool-usage.jsonl")
}

func appendToolUsageDeadLetter(spoolPath string, ev LocalToolUsageEvent, uploadErr error) error {
	if spoolPath == "" {
		return nil
	}
	path := toolUsageDeadLetterPath(filepath.Dir(spoolPath))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	rec := toolUsageDeadLetter{
		Version:    1,
		Event:      ev,
		Error:      uploadErr.Error(),
		RecordedAt: time.Now().UTC(),
	}
	data, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	_, err = f.Write(append(data, '\n'))
	return err
}

func loadToolUsageDeadLetters(workspaceDir string) ([]toolUsageDeadLetter, error) {
	path := toolUsageDeadLetterPath(workspaceDir)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []toolUsageDeadLetter
	for _, line := range bytes.Split(data, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var rec toolUsageDeadLetter
		if err := json.Unmarshal(line, &rec); err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, nil
}
```

Modify single-event branch of `sendSpooledEvents`:

```go
	for idx, ev := range events {
		if err := e.Client.SendToolUsageEvent(ctx, toClientUsageRequest(ev)); err != nil {
			if client.IsPermanentToolUsageError(err) {
				if dlErr := appendToolUsageDeadLetter(e.spoolPath, ev, err); dlErr != nil {
					return idx, dlErr
				}
				if onProgress != nil {
					if progressErr := onProgress(idx + 1); progressErr != nil {
						return idx + 1, progressErr
					}
				}
				continue
			}
			return idx, err
		}
		if onProgress != nil {
			if err := onProgress(idx + 1); err != nil {
				return idx + 1, err
			}
		}
	}
```

- [ ] **Step 7: Run focused tests**

Run:

```bash
cd ae-cli
go test ./internal/client -run TestSendToolUsageEventReturnsHTTPStatusErrorForValidationFailure -count=1
go test ./internal/attributionlocal -run TestSync_ReplayDeadLettersPermanentFailureAndContinues -count=1
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add ae-cli/internal/client/client.go ae-cli/internal/client/client_test.go ae-cli/internal/attributionlocal/sync.go ae-cli/internal/attributionlocal/sync_test.go ae-cli/internal/attributionlocal/test_helpers_test.go
git commit -m "fix(ae-cli): dead-letter permanent tool usage failures"
```

---

### Task 7: Trigger Recovery from Post-Rewrite Failures

**Files:**
- Modify: `ae-cli/internal/hooks/handler.go`
- Modify: `ae-cli/internal/hooks/handler_test.go`

- [ ] **Step 1: Write post-rewrite sync task test**

Add to `ae-cli/internal/hooks/handler_test.go`:

```go
func TestPostRewriteResolvedCreatesPendingSyncTaskWhenUploadFails(t *testing.T) {
	repo := initRepoWithCommit2(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	execCtx := resolvedContextForRepo(t, repo)

	origSpawn := spawnBackgroundSyncRunner
	spawned := false
	spawnBackgroundSyncRunner = func(repoRoot string) error {
		spawned = true
		return nil
	}
	t.Cleanup(func() { spawnBackgroundSyncRunner = origSpawn })

	u := syncCapableFakeUploader{fakeUploader: &fakeUploader{err: errors.New("rewrite upload failed")}}
	if err := NewHandler(u).PostRewriteResolved(context.Background(), execCtx, "amend", strings.NewReader("oldsha1 newsha1\n")); err != nil {
		t.Fatalf("PostRewriteResolved: %v", err)
	}
	task, err := LoadSyncTask(execCtx.WorkspaceID)
	if err != nil {
		t.Fatalf("LoadSyncTask: %v", err)
	}
	if task == nil || task.Status != SyncTaskStatusPending {
		t.Fatalf("task = %+v, want pending task", task)
	}
	if !spawned {
		t.Fatalf("background sync runner was not spawned")
	}
}
```

- [ ] **Step 2: Run test and verify it fails**

Run:

```bash
cd ae-cli
go test ./internal/hooks -run TestPostRewriteResolvedCreatesPendingSyncTaskWhenUploadFails -count=1
```

Expected: FAIL because post-rewrite does not create a sync task or spawn a runner.

- [ ] **Step 3: Extract sync scheduling helper**

Add helper to `ae-cli/internal/hooks/handler.go`:

```go
func (h *Handler) schedulePendingSync(ctx ExecutionContext) {
	task := SyncTask{
		WorkspaceID:     ctx.WorkspaceID,
		RepoRoot:        ctx.RepoRoot,
		ServerURL:       ctx.ServerURL,
		AuthSubject:     ctx.AuthSubject,
		RepoConfigID:    ctx.RepoConfigID,
		RepoKey:         ctx.RepoKey,
		Status:          SyncTaskStatusPending,
		LastRequestedAt: time.Now().UTC(),
	}
	currentTask := &task
	syncClient := h.attributionSyncClient()
	if err := UpsertPendingSyncTask(task); err == nil {
		if loadedTask, loadErr := LoadSyncTask(ctx.WorkspaceID); loadErr == nil && loadedTask != nil {
			currentTask = loadedTask
		}
		if syncClient != nil {
			claimSpawn, claimedTask, claimErr := TryClaimSyncTaskSpawn(ctx.WorkspaceID, time.Now().UTC(), syncTaskSpawnCooldown)
			if claimedTask != nil {
				currentTask = claimedTask
			}
			if claimErr != nil {
				fmt.Fprintln(hookStderr, "ae-cli: attribution sync pending for this repo; run 'ae-cli doctor' for details")
			} else if claimSpawn {
				if err := spawnBackgroundSyncRunner(ctx.RepoRoot); err != nil {
					_ = MarkSyncTaskFailure(currentTask, time.Now().UTC(), err)
					fmt.Fprintln(hookStderr, "ae-cli: attribution sync pending for this repo; run 'ae-cli doctor' for details")
				}
			} else if strings.TrimSpace(currentTask.LastError) != "" {
				fmt.Fprintln(hookStderr, "ae-cli: attribution sync pending for this repo; run 'ae-cli doctor' for details")
			}
		}
	} else {
		fmt.Fprintln(hookStderr, "ae-cli: attribution sync pending for this repo; run 'ae-cli doctor' for details")
	}
}
```

Replace the duplicated scheduling block in `PostCommitResolved` with:

```go
h.schedulePendingSync(execCtx)
```

At the end of `PostRewriteResolved`, after processing pairs, call:

```go
h.schedulePendingSync(execCtx)
```

- [ ] **Step 4: Run focused tests**

Run:

```bash
cd ae-cli
go test ./internal/hooks -run 'TestPostRewriteResolvedCreatesPendingSyncTaskWhenUploadFails|TestPostCommitResolvedLeavesQueuedEventsForAsyncRunner' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add ae-cli/internal/hooks/handler.go ae-cli/internal/hooks/handler_test.go
git commit -m "fix(ae-cli): trigger replay after rewrite failures"
```

---

### Task 8: Surface Unresolved and Dead-Letter Counts in Sync Status

**Files:**
- Modify: `ae-cli/cmd/sync.go`
- Modify: `ae-cli/cmd/sync_test.go`
- Modify: `ae-cli/internal/hooks/unresolved_queue.go`
- Modify: `ae-cli/internal/attributionlocal/sync.go`

- [ ] **Step 1: Add count helpers**

In `ae-cli/internal/hooks/unresolved_queue.go`:

```go
func CountUnresolvedHookEvents() (int, error) {
	items, err := ListUnresolvedHookEvents()
	if err != nil {
		return 0, err
	}
	return len(items), nil
}
```

In `ae-cli/internal/attributionlocal/sync.go`:

```go
func CountToolUsageDeadLetters(workspaceID string) (int, error) {
	if strings.TrimSpace(workspaceID) == "" {
		return 0, nil
	}
	path := toolUsageDeadLetterPath(filepath.Join(AttributionRootDir(), "workspaces", workspaceID))
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	count := 0
	for _, line := range bytes.Split(data, []byte("\n")) {
		if len(bytes.TrimSpace(line)) > 0 {
			count++
		}
	}
	return count, nil
}
```

- [ ] **Step 2: Write sync status output test**

Add to `ae-cli/cmd/sync_test.go`:

```go
func TestSyncStatusShowsUnresolvedAndDeadLetterCounts(t *testing.T) {
	repo := initRepoWithCommitForCmdTests(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	defer func() { _ = os.Chdir(wd) }()
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("Chdir(repo): %v", err)
	}

	gitCtx, err := hooks.DetectGitContext(repo)
	if err != nil {
		t.Fatalf("DetectGitContext: %v", err)
	}
	if err := hooks.EnqueueUnresolvedHookEvent(hooks.UnresolvedHookEvent{
		Kind:        "post-commit",
		RemoteURL:   "https://github.com/acme/repo.git",
		RepoKey:     "github.com/acme/repo",
		WorkspaceID: gitCtx.WorkspaceID,
		CommitSHA:   "abc123",
	}); err != nil {
		t.Fatalf("EnqueueUnresolvedHookEvent: %v", err)
	}
	workspaceDir := filepath.Join(attributionlocal.AttributionRootDir(), "workspaces", gitCtx.WorkspaceID)
	if err := os.MkdirAll(workspaceDir, 0o700); err != nil {
		t.Fatalf("mkdir workspace dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspaceDir, "dead-letter-tool-usage.jsonl"), []byte(`{"version":1}`+"\n"), 0o600); err != nil {
		t.Fatalf("write dead-letter: %v", err)
	}

	var out bytes.Buffer
	syncStatusCmd.SetOut(&out)
	defer syncStatusCmd.SetOut(nil)
	if err := syncStatusCmd.RunE(syncStatusCmd, nil); err != nil {
		t.Fatalf("sync status RunE: %v", err)
	}
	if !strings.Contains(out.String(), "Unresolved Hook Events: 1") {
		t.Fatalf("output = %q, want unresolved count", out.String())
	}
	if !strings.Contains(out.String(), "Tool Usage Dead Letters: 1") {
		t.Fatalf("output = %q, want dead-letter count", out.String())
	}
}
```

- [ ] **Step 3: Run status test and verify it fails**

Run:

```bash
cd ae-cli
go test ./cmd -run TestSyncStatusShowsUnresolvedAndDeadLetterCounts -count=1
```

Expected: FAIL because sync status does not print these counts.

- [ ] **Step 4: Print counts in sync status**

Modify `ae-cli/cmd/sync.go` after `printSyncTaskStatus(cmd.OutOrStdout(), task)`:

```go
unresolvedCount, err := hooks.CountUnresolvedHookEvents()
if err != nil {
	return fmt.Errorf("count unresolved hook events: %w", err)
}
fmt.Fprintf(cmd.OutOrStdout(), "Unresolved Hook Events: %d\n", unresolvedCount)

deadLetterCount, err := attributionlocal.CountToolUsageDeadLetters(attrCtx.workspaceID)
if err != nil {
	return fmt.Errorf("count tool usage dead letters: %w", err)
}
fmt.Fprintf(cmd.OutOrStdout(), "Tool Usage Dead Letters: %d\n", deadLetterCount)
```

- [ ] **Step 5: Run focused test**

Run:

```bash
cd ae-cli
go test ./cmd -run TestSyncStatusShowsUnresolvedAndDeadLetterCounts -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add ae-cli/cmd/sync.go ae-cli/cmd/sync_test.go ae-cli/internal/hooks/unresolved_queue.go ae-cli/internal/attributionlocal/sync.go
git commit -m "feat(ae-cli): show reporting backlog counts"
```

---

### Task 9: Update Reporting Durability Documentation

**Files:**
- Modify: `docs/architecture.md`
- Modify: `docs/superpowers/specs/2026-05-26-ae-cli-post-commit-async-attribution-sync-design.md`
- Modify: `docs/superpowers/plans/2026-06-02-ae-cli-reporting-durability-hardening.md`

- [ ] **Step 1: Update architecture current state**

In `docs/architecture.md`, update the CLI/runtime status section to include this paragraph:

```markdown
Reporting durability is now at-least-once for locally captured events while local state is writable. Hook checkpoint/rewrite failures are stored in a locked workspace queue, first-run repo eligibility failures are stored in an unresolved hook queue, and tool-usage events are spooled before scan state advances. Replay never deletes events solely because the current auth/server/repo binding differs; those events remain pending for the binding that can upload them. Events that the backend permanently rejects are moved to visible dead-letter files instead of blocking later valid events.
```

- [ ] **Step 2: Update active async sync spec**

In `docs/superpowers/specs/2026-05-26-ae-cli-post-commit-async-attribution-sync-design.md`, add this contract under the `Hook fast path` section:

```markdown
Durability requirements:

1. A captured checkpoint/rewrite event must be uploaded, queued, or explicitly reported as a local queue write failure.
2. If repo eligibility cannot be resolved during hook execution, the hook stores an unresolved event with remote URL, workspace ID, commit SHA, and available auth/server subject so a later successful resolve can upload it with `repo_config_id`.
3. Replay must preserve events whose stored binding does not match the current binding; mismatch is a deferred state, not a deletion reason.
4. Corrupt queue/spool state is quarantined so valid events can continue to upload.
5. Permanent backend rejection moves the offending event to dead-letter and continues with later valid events.
```

- [ ] **Step 3: Mark plan status active during execution**

At the top of this plan, below the header, add:

```markdown
**Status:** In progress. Documentation has been updated; Task 10 verification remains unchecked until the listed commands pass.
```

- [ ] **Step 4: Commit docs**

```bash
git add docs/architecture.md docs/superpowers/specs/2026-05-26-ae-cli-post-commit-async-attribution-sync-design.md docs/superpowers/plans/2026-06-02-ae-cli-reporting-durability-hardening.md
git commit -m "docs(ae-cli): document reporting durability contract"
```

---

### Task 10: Full Verification

**Files:**
- No source changes expected.

- [ ] **Step 1: Run focused ae-cli reporting tests**

Run:

```bash
cd ae-cli
go test ./cmd ./internal/hooks ./internal/attributionlocal ./internal/client -count=1
```

Expected: PASS for all listed packages.

- [ ] **Step 2: Run full ae-cli tests**

Run:

```bash
cd ae-cli
go test ./... -count=1
```

Expected: PASS for all ae-cli packages.

- [ ] **Step 3: Run backend checkpoint/tool usage tests**

Run:

```bash
cd backend
go test ./internal/checkpoint ./internal/toolusage ./internal/handler -run 'Checkpoint|ToolUsage|UsageEvent|Batch' -count=1
```

Expected: PASS for checkpoint, tool usage, and handler tests touched by reporting flows.

- [ ] **Step 4: Check diff hygiene**

Run:

```bash
git diff --check
git status --short
```

Expected: `git diff --check` exits 0. `git status --short` shows only the committed branch state or intentional uncommitted plan/status edits.

- [ ] **Step 5: Final smoke with a temporary repo**

Run:

```bash
tmpdir="$(mktemp -d)"
cd "$tmpdir"
git init --template=/dev/null
git config user.email alice@example.com
git config user.name alice
git remote add origin https://github.com/acme/reporting-smoke.git
printf 'a\n' > a.txt
git add a.txt
git commit -m 'init'
HOME_BACKUP="$HOME"
export HOME="$(mktemp -d)"
cd /Users/admin/ai-efficiency/.worktrees/fix-hook-stale-eligibility/ae-cli
go test ./cmd -run 'TestHookPostCommitQueuesUnresolvedWhenInitialResolveTimesOut|TestHookPostCommitUsesExpiredPositiveEligibilityWhenRefreshTimesOut' -count=1
export HOME="$HOME_BACKUP"
```

Expected: PASS. This smoke uses tests instead of a real backend so it does not write production events.

- [ ] **Step 6: Commit verification status**

If Task 9 added the in-progress status line, update it to:

```markdown
**Status:** Complete. Verification on 2026-06-02 passed: `go test ./cmd ./internal/hooks ./internal/attributionlocal ./internal/client -count=1`, `go test ./... -count=1` under `ae-cli`, backend reporting tests, and `git diff --check`.
```

Then commit:

```bash
git add docs/superpowers/plans/2026-06-02-ae-cli-reporting-durability-hardening.md
git commit -m "docs(ae-cli): mark reporting durability verification complete"
```

---

## Self-Review

Spec coverage:

- Queue race and concurrent append loss: Task 1.
- Silent enqueue failure: Task 2.
- First-run resolve timeout without cache: Task 3.
- Binding mismatch deletion: Task 4.
- Corrupt queue/spool blocking replay: Task 5.
- Permanent bad event blocking later events: Task 6.
- Post-rewrite queued events not triggering recovery: Task 7.
- Operator visibility for unresolved/dead-letter state: Task 8.
- Documentation and current contract: Task 9.
- Full verification: Task 10.

Red-flag scan:

- This plan provides concrete file paths, test names, commands, expected outcomes, and implementation snippets.

Type consistency:

- `UnresolvedHookEvent`, `EnqueueUnresolvedHookEvent`, `ListUnresolvedHookEvents`, `SaveUnresolvedHookEvents`, and `CountUnresolvedHookEvents` are defined in Task 3 and reused in Task 8.
- `HTTPStatusError` and `IsPermanentToolUsageError` are defined in Task 6 and used by `attributionlocal`.
- `toolUsageDeadLetter`, `loadToolUsageDeadLetters`, and `CountToolUsageDeadLetters` use the same dead-letter path.
