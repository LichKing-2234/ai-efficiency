package hooks

import (
	"context"
	"testing"
	"time"

	"github.com/ai-efficiency/ae-cli/internal/client"
)

type recordingCompactCheckpointSender struct {
	checkpoints []client.CommitCheckpointRequest
}

func (r *recordingCompactCheckpointSender) SendAttributionCommitCheckpoint(_ context.Context, req client.CommitCheckpointRequest) error {
	r.checkpoints = append(r.checkpoints, req)
	return nil
}

func (r *recordingCompactCheckpointSender) SendAttributionCommitRewrite(context.Context, client.CommitRewriteRequest) error {
	return nil
}

func (r *recordingCompactCheckpointSender) SendAttributionBuckets(context.Context, []client.AttributionBucket) error {
	return nil
}

func (r *recordingCompactCheckpointSender) SendAttributionRevision(context.Context, string, client.AttributionRevision) error {
	return nil
}

func TestCompactBackendUploaderPreservesSubsecondCaptureTime(t *testing.T) {
	now := time.Date(2026, 8, 5, 10, 0, 0, 987_654_321, time.UTC)
	sender := &recordingCompactCheckpointSender{}
	uploader := NewCompactBackendUploader(sender, "installation-a")

	if err := uploader.UploadHookEvent(context.Background(), HookEvent{
		Kind: "post-commit", EventID: "event-a", RepoConfigID: 11, RepoFullName: "acme/repo",
		WorkspaceID: "workspace-a", CommitSHA: "abc123", CapturedAt: now.Format(time.RFC3339Nano),
	}); err != nil {
		t.Fatalf("UploadHookEvent: %v", err)
	}
	if len(sender.checkpoints) != 1 || sender.checkpoints[0].CapturedAt == nil || !sender.checkpoints[0].CapturedAt.Equal(now) {
		t.Fatalf("checkpoints = %+v, want subsecond captured_at %v", sender.checkpoints, now)
	}
}
