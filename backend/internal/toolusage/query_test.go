package toolusage

import (
	"context"
	"fmt"
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

func TestSearchEventUsersReturnsOnlyUsersWithEvents(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	client := testdb.Open(t)

	aliceScope := seedToolUsageScope(t, client)
	bobScope := seedToolUsageScope(t, client)
	noEventScope := seedToolUsageScope(t, client)

	client.User.UpdateOneID(aliceScope.UserID).
		SetUsername("alice").
		SetEmail("alice@example.com").
		SetRole(user.RoleAdmin).
		ExecX(ctx)
	client.User.UpdateOneID(bobScope.UserID).
		SetUsername("bob").
		SetEmail("bob@example.org").
		SetRole(user.RoleUser).
		ExecX(ctx)
	client.User.UpdateOneID(noEventScope.UserID).
		SetUsername("carol").
		SetEmail("carol@example.net").
		ExecX(ctx)

	aliceLatest := time.Now().Add(-2 * time.Minute).UTC()
	bobLatest := time.Now().Add(-10 * time.Minute).UTC()
	client.ToolUsageEvent.Create().
		SetTool("codex").
		SetWorkspaceID(aliceScope.WorkspaceID).
		SetRepoConfigID(aliceScope.RepoConfigID).
		SetUserID(aliceScope.UserID).
		SetToolSessionID("alice-session-1").
		SetDedupeKey("alice-1").
		SetUsageUnit(toolusageevent.UsageUnitToken).
		SetObservedStartAt(aliceLatest.Add(-1 * time.Second)).
		SetObservedEndAt(aliceLatest).
		SaveX(ctx)
	client.ToolUsageEvent.Create().
		SetTool("claude").
		SetWorkspaceID(aliceScope.WorkspaceID).
		SetRepoConfigID(aliceScope.RepoConfigID).
		SetUserID(aliceScope.UserID).
		SetToolSessionID("alice-session-2").
		SetDedupeKey("alice-2").
		SetUsageUnit(toolusageevent.UsageUnitToken).
		SetObservedStartAt(aliceLatest.Add(-2 * time.Second)).
		SetObservedEndAt(aliceLatest.Add(-1 * time.Second)).
		SaveX(ctx)
	client.ToolUsageEvent.Create().
		SetTool("kiro").
		SetWorkspaceID(bobScope.WorkspaceID).
		SetRepoConfigID(bobScope.RepoConfigID).
		SetUserID(bobScope.UserID).
		SetToolSessionID("bob-session-1").
		SetDedupeKey("bob-1").
		SetUsageUnit(toolusageevent.UsageUnitCredit).
		SetObservedStartAt(bobLatest.Add(-1 * time.Second)).
		SetObservedEndAt(bobLatest).
		SaveX(ctx)

	svc := NewQueryService(client)
	users, err := svc.SearchEventUsers(ctx, EventUserSearchRequest{Limit: 20})
	if err != nil {
		t.Fatalf("SearchEventUsers: %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("users=%d, want 2: %+v", len(users), users)
	}
	if users[0].Email != "alice@example.com" || users[0].EventCount != 2 {
		t.Fatalf("first user=%+v, want alice with 2 events", users[0])
	}
	if users[1].Email != "bob@example.org" || users[1].EventCount != 1 {
		t.Fatalf("second user=%+v, want bob with 1 event", users[1])
	}
}

func TestSearchEventUsersFiltersByEmailOrUsernameAndClampsLimit(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	client := testdb.Open(t)

	aliceScope := seedToolUsageScope(t, client)
	bobScope := seedToolUsageScope(t, client)
	client.User.UpdateOneID(aliceScope.UserID).
		SetUsername("alice").
		SetEmail("alice@example.com").
		ExecX(ctx)
	client.User.UpdateOneID(bobScope.UserID).
		SetUsername("bob").
		SetEmail("bob@example.org").
		ExecX(ctx)

	for i, scope := range []TestToolUsageScope{aliceScope, bobScope} {
		observedAt := time.Now().Add(time.Duration(-i) * time.Minute).UTC()
		client.ToolUsageEvent.Create().
			SetTool("codex").
			SetWorkspaceID(scope.WorkspaceID).
			SetRepoConfigID(scope.RepoConfigID).
			SetUserID(scope.UserID).
			SetToolSessionID(fmt.Sprintf("search-session-%d", i)).
			SetDedupeKey(fmt.Sprintf("search-dedupe-%d", i)).
			SetUsageUnit(toolusageevent.UsageUnitToken).
			SetObservedStartAt(observedAt.Add(-1 * time.Second)).
			SetObservedEndAt(observedAt).
			SaveX(ctx)
	}

	svc := NewQueryService(client)
	users, err := svc.SearchEventUsers(ctx, EventUserSearchRequest{Q: "EXAMPLE.ORG", Limit: 100})
	if err != nil {
		t.Fatalf("SearchEventUsers: %v", err)
	}
	if len(users) != 1 {
		t.Fatalf("users=%d, want 1: %+v", len(users), users)
	}
	if users[0].Username != "bob" || users[0].Email != "bob@example.org" {
		t.Fatalf("user=%+v, want bob", users[0])
	}

	users, err = svc.SearchEventUsers(ctx, EventUserSearchRequest{Q: "ali", Limit: 0})
	if err != nil {
		t.Fatalf("SearchEventUsers username: %v", err)
	}
	if len(users) != 1 || users[0].Email != "alice@example.com" {
		t.Fatalf("users=%+v, want alice", users)
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
