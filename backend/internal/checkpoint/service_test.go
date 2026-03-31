package checkpoint

import (
	"context"
	"testing"

	"github.com/ai-efficiency/backend/ent/agentmetadataevent"
	"github.com/ai-efficiency/backend/ent/commitcheckpoint"
	"github.com/ai-efficiency/backend/ent/commitrewrite"
	"github.com/ai-efficiency/backend/ent/enttest"
	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
)

func TestRecordCheckpointUpsertsByEventIDAndWritesMetadataEvents(t *testing.T) {
	t.Parallel()

	client := enttest.Open(t, "sqlite3", "file:ent?mode=memory&_fk=1")
	ctx := context.Background()

	sp := client.ScmProvider.Create().
		SetName("github-test").
		SetType("github").
		SetBaseURL("https://api.github.com").
		SetCredentials("enc").
		SaveX(ctx)

	rc := client.RepoConfig.Create().
		SetScmProviderID(sp.ID).
		SetName("demo").
		SetFullName("org/demo").
		SetCloneURL("https://github.com/org/demo.git").
		SetDefaultBranch("main").
		SaveX(ctx)

	sess := client.Session.Create().
		SetID(uuid.New()).
		SetRepoConfigID(rc.ID).
		SetBranch("main").
		SaveX(ctx)

	svc := NewService(client)
	req := CommitCheckpointRequest{
		EventID:        "evt-commit-1",
		SessionID:      sess.ID.String(),
		RepoFullName:   rc.FullName,
		WorkspaceID:    "ws-1",
		CommitSHA:      "abc123",
		ParentSHAs:     []string{"p1", "p2"},
		BranchSnapshot: "main",
		HeadSnapshot:   "abc123",
		BindingSource:  "marker",
		AgentSnapshot: map[string]any{
			"source":              "codex",
			"source_session_id":   "source-sess-1",
			"usage_unit":          "token",
			"input_tokens":        float64(11),
			"output_tokens":       float64(22),
			"cached_input_tokens": float64(33),
			"reasoning_tokens":    float64(44),
			"credit_usage":        1.25,
			"context_usage_pct":   0.62,
			"raw_payload":         map[string]any{"kind": "snapshot"},
		},
	}

	if err := svc.RecordCheckpoint(ctx, req); err != nil {
		t.Fatalf("record checkpoint first call: %v", err)
	}
	if err := svc.RecordCheckpoint(ctx, req); err != nil {
		t.Fatalf("record checkpoint duplicate event: %v", err)
	}

	checkpointCount := client.CommitCheckpoint.Query().Where(commitcheckpoint.EventIDEQ(req.EventID)).CountX(ctx)
	if checkpointCount != 1 {
		t.Fatalf("checkpoint count = %d, want 1", checkpointCount)
	}

	metadataCount := client.AgentMetadataEvent.Query().Where(agentmetadataevent.SessionIDEQ(sess.ID)).CountX(ctx)
	if metadataCount != 1 {
		t.Fatalf("metadata event count = %d, want 1", metadataCount)
	}

	me := client.AgentMetadataEvent.Query().Where(agentmetadataevent.SessionIDEQ(sess.ID)).OnlyX(ctx)
	if me.Source != agentmetadataevent.SourceCodex {
		t.Fatalf("metadata source = %q, want %q", me.Source, agentmetadataevent.SourceCodex)
	}
	if me.InputTokens != 11 || me.OutputTokens != 22 {
		t.Fatalf("metadata tokens = (%d, %d), want (11, 22)", me.InputTokens, me.OutputTokens)
	}
}

func TestRecordRewriteAcceptsUnboundEvents(t *testing.T) {
	t.Parallel()

	client := enttest.Open(t, "sqlite3", "file:ent?mode=memory&_fk=1")
	ctx := context.Background()

	sp := client.ScmProvider.Create().
		SetName("github-test").
		SetType("github").
		SetBaseURL("https://api.github.com").
		SetCredentials("enc").
		SaveX(ctx)

	rc := client.RepoConfig.Create().
		SetScmProviderID(sp.ID).
		SetName("demo").
		SetFullName("org/demo").
		SetCloneURL("https://github.com/org/demo.git").
		SetDefaultBranch("main").
		SaveX(ctx)

	svc := NewService(client)
	req := CommitRewriteRequest{
		EventID:       "evt-rewrite-1",
		CloneURL:      rc.CloneURL,
		WorkspaceID:   "ws-2",
		RewriteType:   "amend",
		OldCommitSHA:  "old123",
		NewCommitSHA:  "new123",
		BindingSource: "unbound",
	}

	if err := svc.RecordRewrite(ctx, req); err != nil {
		t.Fatalf("record rewrite first call: %v", err)
	}
	if err := svc.RecordRewrite(ctx, req); err != nil {
		t.Fatalf("record rewrite duplicate event: %v", err)
	}

	rewriteCount := client.CommitRewrite.Query().Where(commitrewrite.EventIDEQ(req.EventID)).CountX(ctx)
	if rewriteCount != 1 {
		t.Fatalf("rewrite count = %d, want 1", rewriteCount)
	}

	rw := client.CommitRewrite.Query().Where(commitrewrite.EventIDEQ(req.EventID)).OnlyX(ctx)
	if rw.SessionID != nil {
		t.Fatalf("session_id = %v, want nil for unbound event", rw.SessionID)
	}
}
