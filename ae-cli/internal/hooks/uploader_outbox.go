package hooks

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// OutboxUploader is a minimal concrete uploader for Task 5.
//
// Until a backend checkpoint/rewrite ingestion API is available, we treat a durable local outbox
// as the "upload" sink so:
// - git hooks remain fail-open
// - ae-cli flush can drain the retry queue
// - event_id semantics remain stable and idempotent
//
// Events are stored under:
// ~/.ae-cli/runtime/<session-id>/outbox/hooks/<event-id>.json
//
// If the file already exists, UploadHookEvent is a no-op success (idempotent).
type OutboxUploader struct{}

func outboxEventPath(sessionID, eventID string) (string, error) {
	sessionID = strings.TrimSpace(sessionID)
	eventID = strings.TrimSpace(eventID)
	if sessionID == "" {
		return "", fmt.Errorf("session_id is required")
	}
	if eventID == "" {
		return "", fmt.Errorf("event_id is required")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("home dir: %w", err)
	}
	return filepath.Join(home, ".ae-cli", "runtime", sessionID, "outbox", "hooks", eventID+".json"), nil
}

func (u OutboxUploader) UploadHookEvent(ctx context.Context, ev HookEvent) error {
	_ = ctx

	p, err := outboxEventPath(ev.SessionID, ev.EventID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return fmt.Errorf("creating outbox dir: %w", err)
	}

	b, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("marshal outbox event: %w", err)
	}

	// Idempotent write: if already present, treat as success.
	f, err := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		if os.IsExist(err) {
			return nil
		}
		return fmt.Errorf("open outbox file: %w", err)
	}
	defer f.Close()

	if _, err := f.Write(b); err != nil {
		return fmt.Errorf("write outbox file: %w", err)
	}
	return nil
}

