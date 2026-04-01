package hooks

import (
	"context"
	"testing"
	"time"

	"github.com/ai-efficiency/ae-cli/internal/client"
)

type recordingCheckpointSender struct {
	checkpoints []client.CommitCheckpointRequest
	rewrites    []client.CommitRewriteRequest
}

func (r *recordingCheckpointSender) SendCommitCheckpoint(ctx context.Context, req client.CommitCheckpointRequest) error {
	r.checkpoints = append(r.checkpoints, req)
	return nil
}

func (r *recordingCheckpointSender) SendCommitRewrite(ctx context.Context, req client.CommitRewriteRequest) error {
	r.rewrites = append(r.rewrites, req)
	return nil
}

func TestBackendUploaderMapsCheckpointEvent(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	sender := &recordingCheckpointSender{}
	uploader := NewBackendUploader(sender)

	err := uploader.UploadHookEvent(context.Background(), HookEvent{
		Kind:          "post-commit",
		EventID:       "cp-1",
		SessionID:     "sess-1",
		RepoFullName:  "org/repo",
		WorkspaceID:   "ws-1",
		BindingSource: "marker",
		CommitSHA:     "abc123",
		ParentSHAs:    []string{"000000"},
		BranchSnapshot:"main",
		HeadSnapshot:  "abc123",
		AgentSnapshot: map[string]any{"codex": map[string]any{"total_tokens": 10}},
		CapturedAt:    now.Format(time.RFC3339),
	})
	if err != nil {
		t.Fatalf("UploadHookEvent: %v", err)
	}
	if len(sender.checkpoints) != 1 {
		t.Fatalf("checkpoints len = %d, want 1", len(sender.checkpoints))
	}
	if sender.checkpoints[0].RepoFullName != "org/repo" || sender.checkpoints[0].WorkspaceID != "ws-1" {
		t.Fatalf("unexpected checkpoint request: %+v", sender.checkpoints[0])
	}
}

func TestBackendUploaderMapsRewriteEvent(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	sender := &recordingCheckpointSender{}
	uploader := NewBackendUploader(sender)

	err := uploader.UploadHookEvent(context.Background(), HookEvent{
		Kind:          "post-rewrite",
		EventID:       "rw-1",
		SessionID:     "sess-1",
		RepoFullName:  "org/repo",
		WorkspaceID:   "ws-1",
		BindingSource: "marker",
		RewriteType:   "amend",
		OldCommitSHA:  "old123",
		NewCommitSHA:  "new456",
		CapturedAt:    now.Format(time.RFC3339),
	})
	if err != nil {
		t.Fatalf("UploadHookEvent: %v", err)
	}
	if len(sender.rewrites) != 1 {
		t.Fatalf("rewrites len = %d, want 1", len(sender.rewrites))
	}
	if sender.rewrites[0].OldCommitSHA != "old123" || sender.rewrites[0].NewCommitSHA != "new456" {
		t.Fatalf("unexpected rewrite request: %+v", sender.rewrites[0])
	}
}
