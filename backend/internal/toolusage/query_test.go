package toolusage

import (
	"context"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/ent/commitcheckpoint"
	"github.com/ai-efficiency/backend/ent/prrecord"
	"github.com/ai-efficiency/backend/ent/toolusageevent"
	"github.com/ai-efficiency/backend/ent/user"
	"github.com/ai-efficiency/backend/internal/testdb"
)

func TestListEventsScopesRegularUserToOwnRows(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	client := testdb.Open(t)

	viewerScope := seedToolUsageScope(t, client)
	adminScope := seedToolUsageScope(t, client)
	client.User.UpdateOneID(adminScope.UserID).
		SetRole(user.RoleAdmin).
		ExecX(ctx)

	viewerObservedAt := time.Now().Add(-5 * time.Minute).UTC()
	adminObservedAt := time.Now().Add(-4 * time.Minute).UTC()

	client.ToolUsageEvent.Create().
		SetTool("claude").
		SetWorkspaceID(viewerScope.WorkspaceID).
		SetRepoConfigID(viewerScope.RepoConfigID).
		SetUserID(viewerScope.UserID).
		SetToolSessionID("sess-viewer").
		SetToolEventID("viewer-event").
		SetDedupeKey("viewer-1").
		SetUsageUnit(toolusageevent.UsageUnitToken).
		SetObservedStartAt(viewerObservedAt.Add(-1 * time.Second)).
		SetObservedEndAt(viewerObservedAt).
		SetRawSourcePath("/Users/admin/.claude/projects/viewer.jsonl").
		SaveX(ctx)
	client.ToolUsageEvent.Create().
		SetTool("codex").
		SetWorkspaceID(adminScope.WorkspaceID).
		SetRepoConfigID(adminScope.RepoConfigID).
		SetUserID(adminScope.UserID).
		SetToolSessionID("sess-admin").
		SetToolEventID("admin-event").
		SetDedupeKey("admin-1").
		SetUsageUnit(toolusageevent.UsageUnitToken).
		SetObservedStartAt(adminObservedAt.Add(-1 * time.Second)).
		SetObservedEndAt(adminObservedAt).
		SetRawSourcePath("/Users/admin/.codex/sessions/admin.jsonl").
		SaveX(ctx)

	svc := NewQueryService(client)
	rows, total, err := svc.ListEvents(ctx, ListEventsRequest{
		ActorUserID: viewerScope.UserID,
		ActorRole:   string(user.RoleUser),
		From:        time.Now().Add(-24 * time.Hour).UTC(),
		To:          time.Now().UTC(),
		Limit:       20,
	})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if total != 1 || len(rows) != 1 {
		t.Fatalf("got total=%d rows=%d, want 1/1", total, len(rows))
	}
	if rows[0].ToolSessionID != "sess-viewer" {
		t.Fatalf("row session=%q, want sess-viewer", rows[0].ToolSessionID)
	}
	if rows[0].SourceBasename != "viewer.jsonl" {
		t.Fatalf("source basename=%q, want viewer.jsonl", rows[0].SourceBasename)
	}
}

func TestGetEventDetailRedactsRawFieldsForRegularUser(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	client := testdb.Open(t)

	scope := seedToolUsageScope(t, client)
	client.User.UpdateOneID(scope.UserID).
		SetRole(user.RoleUser).
		ExecX(ctx)

	checkpoint := client.CommitCheckpoint.Create().
		SetEventID("cp-detail-1").
		SetUserID(scope.UserID).
		SetWorkspaceID(scope.WorkspaceID).
		SetRepoConfigID(scope.RepoConfigID).
		SetCommitSha("detail-sha").
		SetParentShas([]string{"base"}).
		SetBindingSource(commitcheckpoint.BindingSourceUnbound).
		SetCapturedAt(time.Now().Add(-10 * time.Minute).UTC()).
		SaveX(ctx)

	event := client.ToolUsageEvent.Create().
		SetTool("claude").
		SetWorkspaceID(scope.WorkspaceID).
		SetRepoConfigID(scope.RepoConfigID).
		SetUserID(scope.UserID).
		SetToolSessionID("detail-session").
		SetToolEventID("detail-event").
		SetDedupeKey("detail-dedupe").
		SetUsageUnit(toolusageevent.UsageUnitToken).
		SetObservedStartAt(time.Now().Add(-9 * time.Minute).UTC()).
		SetObservedEndAt(time.Now().Add(-9 * time.Minute).UTC()).
		SetRawSourcePath("/Users/admin/.claude/projects/detail.jsonl").
		SetRawSourceLocator("line:42").
		SetRawPayload(map[string]any{"kind": "assistant", "value": "secret"}).
		SetCommitCheckpointID(checkpoint.ID).
		SaveX(ctx)

	pr := client.PrRecord.Create().
		SetRepoConfigID(scope.RepoConfigID).
		SetScmPrID(38).
		SetTitle("detail pr").
		SetStatus(prrecord.StatusOpen).
		SetAuthor("arthas").
		SaveX(ctx)
	client.PRCommitUsageSnapshot.Create().
		SetPrRecordID(pr.ID).
		SetCommitSha("detail-sha").
		SetCommitCheckpointID(checkpoint.ID).
		SetCapturedAt(checkpoint.CapturedAt).
		SaveX(ctx)

	svc := NewQueryService(client)

	userDetail, err := svc.GetEventDetail(ctx, GetEventDetailRequest{
		ActorUserID: scope.UserID,
		ActorRole:   string(user.RoleUser),
		EventID:     event.ID,
	})
	if err != nil {
		t.Fatalf("GetEventDetail regular user: %v", err)
	}
	if userDetail.RawSourcePath != "" {
		t.Fatalf("RawSourcePath=%q, want redacted empty", userDetail.RawSourcePath)
	}
	if userDetail.RawSourceLocator != "" {
		t.Fatalf("RawSourceLocator=%q, want redacted empty", userDetail.RawSourceLocator)
	}
	if userDetail.RawPayload != nil {
		t.Fatalf("RawPayload=%v, want nil", userDetail.RawPayload)
	}
	if len(userDetail.MatchedPRs) != 1 || userDetail.MatchedPRs[0].ScmPRID != 38 {
		t.Fatalf("MatchedPRs=%+v, want one PR #38", userDetail.MatchedPRs)
	}

	adminDetail, err := svc.GetEventDetail(ctx, GetEventDetailRequest{
		ActorUserID: scope.UserID,
		ActorRole:   string(user.RoleAdmin),
		EventID:     event.ID,
	})
	if err != nil {
		t.Fatalf("GetEventDetail admin: %v", err)
	}
	if adminDetail.RawSourcePath != "/Users/admin/.claude/projects/detail.jsonl" {
		t.Fatalf("RawSourcePath=%q, want full path", adminDetail.RawSourcePath)
	}
	if adminDetail.RawSourceLocator != "line:42" {
		t.Fatalf("RawSourceLocator=%q, want line:42", adminDetail.RawSourceLocator)
	}
	if adminDetail.RawPayload == nil || adminDetail.RawPayload["kind"] != "assistant" {
		t.Fatalf("RawPayload=%v, want assistant payload", adminDetail.RawPayload)
	}
}
