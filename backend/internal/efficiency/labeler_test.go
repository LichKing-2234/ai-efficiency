package efficiency

import (
	"context"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/commitcheckpoint"
	"github.com/ai-efficiency/backend/ent/prrecord"
	"github.com/ai-efficiency/backend/ent/scmprovider"
	"github.com/ai-efficiency/backend/internal/testdb"
	"go.uber.org/zap"
)

func newLabelerTestClient(t *testing.T) *ent.Client {
	t.Helper()
	return testdb.Open(t)
}

func createLabelerTestRepo(t *testing.T, ctx context.Context, client *ent.Client, name string) *ent.RepoConfig {
	t.Helper()
	sp := client.ScmProvider.Create().
		SetName("test-provider").
		SetType(scmprovider.TypeGithub).
		SetBaseURL("https://github.com").
		SetCredentials("test-token").
		SaveX(ctx)

	return client.RepoConfig.Create().
		SetName(name).
		SetFullName("org/" + name).
		SetCloneURL("https://github.com/org/" + name + ".git").
		SetDefaultBranch("main").
		SetScmProviderID(sp.ID).
		SaveX(ctx)
}

func TestNewLabeler(t *testing.T) {
	client := newLabelerTestClient(t)
	defer client.Close()

	labeler := NewLabeler(client, nil, zap.NewNop())
	if labeler == nil {
		t.Fatal("expected non-nil labeler")
	}
}

func TestLabelPRNoToolUsage(t *testing.T) {
	client := newLabelerTestClient(t)
	defer client.Close()

	ctx := context.Background()
	rc := createLabelerTestRepo(t, ctx, client, "labeler-no-usage")
	pr := client.PrRecord.Create().
		SetRepoConfigID(rc.ID).
		SetScmPrID(1).
		SetScmPrURL("https://github.com/org/labeler-no-usage/pull/1").
		SetAuthor("alice").
		SetTitle("No usage").
		SetSourceBranch("feat/no-usage").
		SetTargetBranch("main").
		SetStatus(prrecord.StatusOpen).
		SetCreatedAt(time.Now()).
		SaveX(ctx)

	labeler := NewLabeler(client, nil, zap.NewNop())
	result, err := labeler.LabelPR(ctx, pr.ID)
	if err != nil {
		t.Fatalf("LabelPR: %v", err)
	}
	if result.AILabel != "no_ai_detected" {
		t.Fatalf("AILabel = %q, want no_ai_detected", result.AILabel)
	}

	updated := client.PrRecord.GetX(ctx, pr.ID)
	if updated.AiLabel != prrecord.AiLabelNoAiDetected {
		t.Fatalf("pr ai_label = %q, want %q", updated.AiLabel, prrecord.AiLabelNoAiDetected)
	}
}

func TestLabelPRWithBoundToolUsage(t *testing.T) {
	client := newLabelerTestClient(t)
	defer client.Close()

	ctx := context.Background()
	rc := createLabelerTestRepo(t, ctx, client, "labeler-with-usage")
	now := time.Now()
	user := client.User.Create().
		SetUsername("alice").
		SetEmail("alice@example.com").
		SetAuthSource("ldap").
		SetRole("user").
		SaveX(ctx)
	pr := client.PrRecord.Create().
		SetRepoConfigID(rc.ID).
		SetScmPrID(2).
		SetScmPrURL("https://github.com/org/labeler-with-usage/pull/2").
		SetAuthor("bob").
		SetTitle("With usage").
		SetSourceBranch("feat/with-usage").
		SetTargetBranch("main").
		SetStatus(prrecord.StatusOpen).
		SetCreatedAt(now).
		SaveX(ctx)

	checkpoint := client.CommitCheckpoint.Create().
		SetEventID("evt-1").
		SetRepoConfigID(rc.ID).
		SetUserID(user.ID).
		SetWorkspaceID("ws-1").
		SetCommitSha("abc123").
		SetParentShas([]string{"def456"}).
		SetBranchSnapshot("feat/with-usage").
		SetCapturedAt(now.Add(-time.Hour)).
		SetBindingSource(commitcheckpoint.BindingSourceMarker).
		SaveX(ctx)

	client.ToolUsageEvent.Create().
		SetRepoConfigID(rc.ID).
		SetUserID(user.ID).
		SetCommitCheckpointID(checkpoint.ID).
		SetTool("codex").
		SetWorkspaceID("ws-1").
		SetToolSessionID("tool-session-1").
		SetDedupeKey("dedupe-1").
		SetUsageUnit("token").
		SetInputTokens(120).
		SetObservedStartAt(now.Add(-2 * time.Hour)).
		SetObservedEndAt(now.Add(-90 * time.Minute)).
		SaveX(ctx)

	labeler := NewLabeler(client, nil, zap.NewNop())
	result, err := labeler.LabelPR(ctx, pr.ID)
	if err != nil {
		t.Fatalf("LabelPR: %v", err)
	}
	if result.AILabel != "ai_via_sub2api" {
		t.Fatalf("AILabel = %q, want ai_via_sub2api", result.AILabel)
	}
	if len(result.SessionIDs) != 1 || result.SessionIDs[0] != "tool-session-1" {
		t.Fatalf("SessionIDs = %v, want [tool-session-1]", result.SessionIDs)
	}

	updated := client.PrRecord.GetX(ctx, pr.ID)
	if updated.AiLabel != prrecord.AiLabelAiViaSub2api {
		t.Fatalf("pr ai_label = %q, want %q", updated.AiLabel, prrecord.AiLabelAiViaSub2api)
	}
}
