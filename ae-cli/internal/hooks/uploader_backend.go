package hooks

import (
	"context"
	"fmt"
	"time"

	"github.com/ai-efficiency/ae-cli/internal/client"
)

type checkpointSender interface {
	SendCommitCheckpoint(ctx context.Context, req client.CommitCheckpointRequest) error
	SendCommitRewrite(ctx context.Context, req client.CommitRewriteRequest) error
}

type BackendUploader struct {
	client checkpointSender
}

func NewBackendUploader(c checkpointSender) BackendUploader {
	return BackendUploader{client: c}
}

func (u BackendUploader) UploadHookEvent(ctx context.Context, ev HookEvent) error {
	if u.client == nil {
		return fmt.Errorf("backend uploader client is nil")
	}

	var capturedAt *time.Time
	if ev.CapturedAt != "" {
		t, err := time.Parse(time.RFC3339, ev.CapturedAt)
		if err != nil {
			return fmt.Errorf("parse captured_at: %w", err)
		}
		capturedAt = &t
	}

	switch ev.Kind {
	case "post-commit":
		return u.client.SendCommitCheckpoint(ctx, client.CommitCheckpointRequest{
			EventID:        ev.EventID,
			SessionID:      ev.SessionID,
			RepoFullName:   ev.RepoFullName,
			WorkspaceID:    ev.WorkspaceID,
			CommitSHA:      ev.CommitSHA,
			ParentSHAs:     ev.ParentSHAs,
			BranchSnapshot: ev.BranchSnapshot,
			HeadSnapshot:   ev.HeadSnapshot,
			BindingSource:  ev.BindingSource,
			AgentSnapshot:  ev.AgentSnapshot,
			CapturedAt:     capturedAt,
		})
	case "post-rewrite":
		return u.client.SendCommitRewrite(ctx, client.CommitRewriteRequest{
			EventID:       ev.EventID,
			SessionID:     ev.SessionID,
			RepoFullName:  ev.RepoFullName,
			WorkspaceID:   ev.WorkspaceID,
			RewriteType:   ev.RewriteType,
			OldCommitSHA:  ev.OldCommitSHA,
			NewCommitSHA:  ev.NewCommitSHA,
			BindingSource: ev.BindingSource,
			CapturedAt:    capturedAt,
		})
	default:
		return fmt.Errorf("unsupported hook event kind: %s", ev.Kind)
	}
}
