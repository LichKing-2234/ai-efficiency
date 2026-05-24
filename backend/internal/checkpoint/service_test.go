package checkpoint

import (
	"context"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/commitcheckpoint"
	"github.com/ai-efficiency/backend/ent/commitrewrite"
	"github.com/ai-efficiency/backend/ent/toolusageevent"
	"github.com/ai-efficiency/backend/internal/testdb"
)

func createCheckpointTestRepo(t *testing.T) (*ent.Client, context.Context, int, string, string) {
	t.Helper()

	client := testdb.Open(t)
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

	userID := client.User.Create().
		SetUsername("checkpoint-owner").
		SetEmail("checkpoint-owner@test.com").
		SetAuthSource("ldap").
		SaveX(ctx).ID

	return client, ctx, userID, rc.FullName, rc.CloneURL
}

func TestRecordCheckpointDedupesByEventIDAndStoresUserID(t *testing.T) {
	t.Parallel()

	client, ctx, userID, fullName, _ := createCheckpointTestRepo(t)
	defer client.Close()

	svc := NewService(client)
	req := CommitCheckpointRequest{
		EventID:        "evt-commit-1",
		RepoFullName:   fullName,
		WorkspaceID:    "ws-1",
		CommitSHA:      "abc123",
		ParentSHAs:     []string{"p1", "p2"},
		BranchSnapshot: "main",
		HeadSnapshot:   "abc123",
		BindingSource:  "marker",
		AgentSnapshot:  map[string]any{"codex": map[string]any{"total_tokens": 500}},
	}

	if err := svc.RecordCheckpointForUser(ctx, userID, req); err != nil {
		t.Fatalf("record checkpoint first call: %v", err)
	}
	if err := svc.RecordCheckpointForUser(ctx, userID, req); err != nil {
		t.Fatalf("record checkpoint duplicate event: %v", err)
	}

	rows := client.CommitCheckpoint.Query().Where(commitcheckpoint.EventIDEQ(req.EventID)).AllX(ctx)
	if len(rows) != 1 {
		t.Fatalf("checkpoint count = %d, want 1", len(rows))
	}
	if rows[0].UserID == nil || *rows[0].UserID != userID {
		t.Fatalf("user_id = %v, want %d", rows[0].UserID, userID)
	}
}

func TestRecordRewriteAcceptsUnboundEventsAndStoresUserID(t *testing.T) {
	t.Parallel()

	client, ctx, userID, _, cloneURL := createCheckpointTestRepo(t)
	defer client.Close()

	svc := NewService(client)
	req := CommitRewriteRequest{
		EventID:       "evt-rewrite-1",
		CloneURL:      cloneURL,
		WorkspaceID:   "ws-2",
		RewriteType:   "amend",
		OldCommitSHA:  "old123",
		NewCommitSHA:  "new123",
		BindingSource: "unbound",
	}

	if err := svc.RecordRewriteForUser(ctx, userID, req); err != nil {
		t.Fatalf("record rewrite first call: %v", err)
	}
	if err := svc.RecordRewriteForUser(ctx, userID, req); err != nil {
		t.Fatalf("record rewrite duplicate event: %v", err)
	}

	row := client.CommitRewrite.Query().Where(commitrewrite.EventIDEQ(req.EventID)).OnlyX(ctx)
	if row.UserID == nil || *row.UserID != userID {
		t.Fatalf("user_id = %v, want %d", row.UserID, userID)
	}
}

func TestRecordCheckpointBindsToolUsageEventsForWorkspaceWindow(t *testing.T) {
	t.Parallel()

	client, ctx, userID, fullName, _ := createCheckpointTestRepo(t)
	defer client.Close()

	repo := client.RepoConfig.Query().Where().OnlyX(ctx)

	client.CommitCheckpoint.Create().
		SetEventID("cp-prev").
		SetUserID(userID).
		SetWorkspaceID("ws-1").
		SetRepoConfigID(repo.ID).
		SetCommitSha("base-sha").
		SetParentShas([]string{"p0"}).
		SetBindingSource("unbound").
		SetCapturedAt(time.Unix(150, 0).UTC()).
		SaveX(ctx)

	client.ToolUsageEvent.Create().
		SetTool("codex").
		SetWorkspaceID("ws-1").
		SetRepoConfigID(repo.ID).
		SetUserID(userID).
		SetToolSessionID("conv-1").
		SetToolEventID("resp-1").
		SetObservedStartAt(time.Unix(160, 0).UTC()).
		SetObservedEndAt(time.Unix(161, 0).UTC()).
		SetUsageUnit("token").
		SetInputTokens(10).
		SetOutputTokens(5).
		SetDedupeKey("codex:bind-1").
		SaveX(ctx)

	svc := NewService(client)
	if err := svc.RecordCheckpointForUser(ctx, userID, CommitCheckpointRequest{
		EventID:       "cp-bind-1",
		RepoFullName:  fullName,
		WorkspaceID:   "ws-1",
		CommitSHA:     "head-sha",
		ParentSHAs:    []string{"base-sha"},
		BindingSource: "unbound",
		CapturedAt:    ptrTime(time.Unix(200, 0).UTC()),
	}); err != nil {
		t.Fatalf("RecordCheckpoint: %v", err)
	}

	created := client.CommitCheckpoint.Query().
		Where(commitcheckpoint.EventIDEQ("cp-bind-1")).
		OnlyX(ctx)

	bound := client.ToolUsageEvent.Query().
		Where(toolusageevent.CommitCheckpointIDEQ(created.ID)).
		AllX(ctx)
	if len(bound) != 1 {
		t.Fatalf("bound tool usage events = %d, want 1", len(bound))
	}
}

func TestRecordCheckpointForUser_AutoCreatesRepoOnRemoteMiss(t *testing.T) {
	t.Parallel()

	client := testdb.Open(t)
	ctx := context.Background()
	defer client.Close()

	userID := client.User.Create().
		SetUsername("checkpoint-owner-auto-create").
		SetEmail("checkpoint-owner-auto-create@test.com").
		SetAuthSource("ldap").
		SaveX(ctx).ID

	svc := NewService(client)
	err := svc.RecordCheckpointForUser(ctx, userID, CommitCheckpointRequest{
		EventID:        "cp-auto-create",
		RepoFullName:   "https://github.com/acme/platform.git",
		WorkspaceID:    "ws-1",
		CommitSHA:      "abc123",
		BranchSnapshot: "main",
		BindingSource:  "marker",
	})
	if err != nil {
		t.Fatalf("RecordCheckpointForUser error: %v", err)
	}

	rc := client.RepoConfig.Query().OnlyX(ctx)
	if rc.RepoKey != "github.com/acme/platform" {
		t.Fatalf("RepoKey = %q, want %q", rc.RepoKey, "github.com/acme/platform")
	}
}

func TestRecordCheckpointWithRepoConfigIDDoesNotAutoCreateOnRemoteMiss(t *testing.T) {
	t.Parallel()
	client, ctx, userID, _, _ := createCheckpointTestRepo(t)
	defer client.Close()
	repo := client.RepoConfig.Query().OnlyX(ctx)

	err := NewService(client).RecordCheckpointForUser(ctx, userID, CommitCheckpointRequest{
		EventID:        "cp-by-id",
		RepoConfigID:   repo.ID,
		RepoFullName:   "https://repo-host.example.com/unknown/repo.git",
		WorkspaceID:    "ws-by-id",
		CommitSHA:      "abc123",
		BranchSnapshot: "main",
		BindingSource:  "unbound",
	})
	if err != nil {
		t.Fatalf("RecordCheckpointForUser: %v", err)
	}
	if count := client.RepoConfig.Query().CountX(ctx); count != 1 {
		t.Fatalf("repo count = %d, want 1", count)
	}
	row := client.CommitCheckpoint.Query().Where(commitcheckpoint.EventIDEQ("cp-by-id")).OnlyX(ctx)
	if row.RepoConfigID != repo.ID {
		t.Fatalf("repo_config_id = %d, want %d", row.RepoConfigID, repo.ID)
	}
}

func TestRecordCheckpointWithInactiveRepoConfigIDRejected(t *testing.T) {
	t.Parallel()
	client, ctx, userID, _, _ := createCheckpointTestRepo(t)
	defer client.Close()
	repo := client.RepoConfig.Query().OnlyX(ctx)
	client.RepoConfig.UpdateOneID(repo.ID).SetStatus("inactive").ExecX(ctx)

	err := NewService(client).RecordCheckpointForUser(ctx, userID, CommitCheckpointRequest{
		EventID:       "cp-inactive",
		RepoConfigID:  repo.ID,
		WorkspaceID:   "ws-inactive",
		CommitSHA:     "abc123",
		BindingSource: "unbound",
	})
	if err == nil {
		t.Fatalf("expected inactive repo rejection")
	}
	if count := client.CommitCheckpoint.Query().Where(commitcheckpoint.EventIDEQ("cp-inactive")).CountX(ctx); count != 0 {
		t.Fatalf("checkpoint count = %d, want 0", count)
	}
}

func TestRecordRewriteWithRepoConfigIDDoesNotAutoCreate(t *testing.T) {
	t.Parallel()
	client, ctx, userID, _, _ := createCheckpointTestRepo(t)
	defer client.Close()
	repo := client.RepoConfig.Query().OnlyX(ctx)

	err := NewService(client).RecordRewriteForUser(ctx, userID, CommitRewriteRequest{
		EventID:       "rw-by-id",
		RepoConfigID:  repo.ID,
		RepoFullName:  "https://repo-host.example.com/unknown/repo.git",
		WorkspaceID:   "ws-rw",
		RewriteType:   "amend",
		OldCommitSHA:  "old",
		NewCommitSHA:  "new",
		BindingSource: "unbound",
	})
	if err != nil {
		t.Fatalf("RecordRewriteForUser: %v", err)
	}
	if count := client.RepoConfig.Query().CountX(ctx); count != 1 {
		t.Fatalf("repo count = %d, want 1", count)
	}
}

func ptrTime(v time.Time) *time.Time {
	return &v
}
