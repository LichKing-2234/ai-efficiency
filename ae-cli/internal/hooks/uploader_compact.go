package hooks

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ai-efficiency/ae-cli/internal/attributionlocal"
	"github.com/ai-efficiency/ae-cli/internal/client"
)

type compactCheckpointSender interface {
	SendAttributionCommitCheckpoint(context.Context, client.CommitCheckpointRequest) error
	SendAttributionCommitRewrite(context.Context, client.CommitRewriteRequest) error
	attributionlocal.CompactBackendClient
}

type CompactBackendUploader struct {
	client         compactCheckpointSender
	installationID string
}

func NewCompactBackendUploader(client compactCheckpointSender, installationID string) CompactBackendUploader {
	return CompactBackendUploader{client: client, installationID: strings.TrimSpace(installationID)}
}

func (u CompactBackendUploader) InstallationID() string { return u.installationID }

func (u CompactBackendUploader) CompactUsageClient() attributionlocal.CompactBackendClient {
	return u.client
}

func (u CompactBackendUploader) UploadHookEvent(ctx context.Context, ev HookEvent) error {
	if u.client == nil {
		return fmt.Errorf("compact backend uploader client is nil")
	}
	var capturedAt *time.Time
	if ev.CapturedAt != "" {
		parsed, err := time.Parse(time.RFC3339, ev.CapturedAt)
		if err != nil {
			return err
		}
		capturedAt = &parsed
	}
	switch ev.Kind {
	case "post-commit":
		return u.client.SendAttributionCommitCheckpoint(ctx, client.CommitCheckpointRequest{
			EventID: ev.EventID, RepoConfigID: ev.RepoConfigID, RepoFullName: ev.RepoFullName,
			WorkspaceID: ev.WorkspaceID, CommitSHA: ev.CommitSHA, ParentSHAs: ev.ParentSHAs,
			BranchSnapshot: ev.BranchSnapshot, HeadSnapshot: ev.HeadSnapshot,
			BindingSource: ev.BindingSource, CapturedAt: capturedAt,
		})
	case "post-rewrite":
		return u.client.SendAttributionCommitRewrite(ctx, client.CommitRewriteRequest{
			EventID: ev.EventID, RepoConfigID: ev.RepoConfigID, RepoFullName: ev.RepoFullName,
			WorkspaceID: ev.WorkspaceID, RewriteType: ev.RewriteType, OldCommitSHA: ev.OldCommitSHA,
			NewCommitSHA: ev.NewCommitSHA, BindingSource: ev.BindingSource, CapturedAt: capturedAt,
		})
	default:
		return fmt.Errorf("unsupported hook event kind: %s", ev.Kind)
	}
}
