package hooks

import (
	"context"
	"fmt"
	"time"

	"github.com/ai-efficiency/ae-cli/internal/attributionlocal"
	"github.com/ai-efficiency/ae-cli/internal/client"
)

type compactCheckpointSender interface {
	SendAttributionCommitCheckpoint(context.Context, client.CommitCheckpointRequest) error
	SendAttributionCommitRewrite(context.Context, client.CommitRewriteRequest) error
}

type CompactBackendUploader struct {
	client          compactCheckpointSender
	toolUsage       attributionlocal.BackendClient
	relayProviderID int
	protocol        client.AttributionProtocol
}

func NewCompactBackendUploader(client compactCheckpointSender, relayProviderID int, protocol client.AttributionProtocol) CompactBackendUploader {
	return CompactBackendUploader{client: client, relayProviderID: relayProviderID, protocol: protocol}
}

// WithToolUsageClient attaches the client the usage surface uploads through.
//
// It is a separate client because the two surfaces authenticate differently.
// Compact reporting holds a reporter credential, which the usage endpoints do
// not accept — they sit behind the user session. Without this attachment a
// compact machine had no usage path at all: the sync pass saw no usage client
// and every tool usage event stayed on disk, silently, from the day compact
// reporting was enabled.
func (u CompactBackendUploader) WithToolUsageClient(toolUsage attributionlocal.BackendClient) CompactBackendUploader {
	u.toolUsage = toolUsage
	return u
}

// ToolUsageClient is what the sync pass uploads usage through, or nil on a
// machine with no user session to authenticate it.
func (u CompactBackendUploader) ToolUsageClient() attributionlocal.BackendClient {
	return u.toolUsage
}

func (u CompactBackendUploader) V2ClaimClient() attributionlocal.V2ClaimBackendClient {
	client, _ := u.client.(attributionlocal.V2ClaimBackendClient)
	return client
}
func (u CompactBackendUploader) RelayProviderID() int { return u.relayProviderID }

func (u CompactBackendUploader) AttributionProtocol() client.AttributionProtocol { return u.protocol }

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
			LineageKind: ev.LineageKind, SourceCommitSHA: ev.SourceCommitSHA,
			CommitPatchID: ev.CommitPatchID, SourcePatchID: ev.SourcePatchID,
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
