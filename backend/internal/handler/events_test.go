package handler

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/ent/commitcheckpoint"
	"github.com/ai-efficiency/backend/ent/toolusageevent"
	entuser "github.com/ai-efficiency/backend/ent/user"
)

type seededEventActors struct {
	adminUserID int
	userUserID  int
	repoID      int
}

func seedEventsFixture(t *testing.T, env *fullTestEnv) seededEventActors {
	t.Helper()

	ctx := context.Background()
	repoID := createFullTestRepo(t, env.client)

	adminUser := env.client.User.Query().Where(entuser.UsernameEQ("fulladmin")).OnlyX(ctx)
	userUser := env.client.User.Query().Where(entuser.UsernameEQ("covuser")).OnlyX(ctx)

	cp := env.client.CommitCheckpoint.Create().
		SetEventID("events-cp-1").
		SetUserID(userUser.ID).
		SetWorkspaceID("events-ws-1").
		SetRepoConfigID(repoID).
		SetCommitSha("events-sha-1").
		SetParentShas([]string{"base"}).
		SetBindingSource(commitcheckpoint.BindingSourceUnbound).
		SetCapturedAt(time.Now().Add(-30 * time.Minute).UTC()).
		SaveX(ctx)

	env.client.ToolUsageEvent.Create().
		SetTool("claude").
		SetWorkspaceID("events-ws-1").
		SetRepoConfigID(repoID).
		SetUserID(userUser.ID).
		SetToolSessionID("user-bound-session").
		SetToolEventID("user-bound-event").
		SetDedupeKey("events-user-bound").
		SetUsageUnit("token").
		SetInputTokens(10).
		SetOutputTokens(5).
		SetObservedStartAt(time.Now().Add(-29 * time.Minute).UTC()).
		SetObservedEndAt(time.Now().Add(-29 * time.Minute).UTC()).
		SetCommitCheckpointID(cp.ID).
		SetRawSourcePath("/Users/admin/.claude/projects/user-bound.jsonl").
		SetRawSourceLocator("line:10").
		SetRawPayload(map[string]any{"kind": "assistant", "scope": "user-bound"}).
		SaveX(ctx)

	env.client.ToolUsageEvent.Create().
		SetTool("kiro").
		SetWorkspaceID("events-ws-1").
		SetRepoConfigID(repoID).
		SetUserID(userUser.ID).
		SetToolSessionID("user-unbound-session").
		SetToolEventID("user-unbound-event").
		SetDedupeKey("events-user-unbound").
		SetUsageUnit("credit").
		SetCreditUsage(1.2).
		SetObservedStartAt(time.Now().Add(-20 * time.Minute).UTC()).
		SetObservedEndAt(time.Now().Add(-20 * time.Minute).UTC()).
		SetRawSourcePath("/Users/admin/Library/Application Support/kiro-cli/data.sqlite3").
		SetRawSourceLocator("conversation:1").
		SetRawPayload(map[string]any{"kind": "turn", "scope": "user-unbound"}).
		SaveX(ctx)

	env.client.ToolUsageEvent.Create().
		SetTool("codex").
		SetWorkspaceID("events-ws-admin").
		SetRepoConfigID(repoID).
		SetUserID(adminUser.ID).
		SetToolSessionID("admin-session").
		SetToolEventID("admin-event").
		SetDedupeKey("events-admin").
		SetUsageUnit("token").
		SetInputTokens(99).
		SetOutputTokens(11).
		SetObservedStartAt(time.Now().Add(-10 * time.Minute).UTC()).
		SetObservedEndAt(time.Now().Add(-10 * time.Minute).UTC()).
		SetRawSourcePath("/Users/admin/.codex/sessions/admin.jsonl").
		SaveX(ctx)

	return seededEventActors{
		adminUserID: adminUser.ID,
		userUserID:  userUser.ID,
		repoID:      repoID,
	}
}

func TestEventsListScopesNonAdminToOwnRows(t *testing.T) {
	t.Parallel()

	env := setupFullTestEnv(t)
	nonAdminToken := createFullNonAdminToken(t, env)
	actors := seedEventsFixture(t, env)

	path := fmt.Sprintf("/api/v1/events?from=%s&to=%s&user_id=%d", time.Now().Add(-24*time.Hour).UTC().Format(time.RFC3339), time.Now().UTC().Format(time.RFC3339), actors.adminUserID)
	w := doFullRequestWithToken(env, http.MethodGet, path, nil, nonAdminToken)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}

	resp := parseFullResponse(t, w)
	data := resp["data"].(map[string]interface{})
	if got := int(data["total"].(float64)); got != 2 {
		t.Fatalf("total = %d, want 2", got)
	}
	items := data["items"].([]interface{})
	if len(items) != 2 {
		t.Fatalf("items = %d, want 2", len(items))
	}
	for _, raw := range items {
		row := raw.(map[string]interface{})
		if row["tool_session_id"] == "admin-session" {
			t.Fatalf("non-admin response unexpectedly contains admin event row: %+v", row)
		}
		if _, ok := row["username"]; ok {
			t.Fatalf("non-admin response unexpectedly exposes username: %+v", row)
		}
	}
}

func TestEventDetailReturnsRawFieldsOnlyForAdmin(t *testing.T) {
	t.Parallel()

	env := setupFullTestEnv(t)
	nonAdminToken := createFullNonAdminToken(t, env)
	seedEventsFixture(t, env)

	event := env.client.ToolUsageEvent.Query().
		Where(toolusageevent.DedupeKeyEQ("events-user-bound")).
		OnlyX(context.Background())

	userResp := doFullRequestWithToken(env, http.MethodGet, fmt.Sprintf("/api/v1/events/%d", event.ID), nil, nonAdminToken)
	if userResp.Code != http.StatusOK {
		t.Fatalf("user status = %d, want 200, body=%s", userResp.Code, userResp.Body.String())
	}
	userData := parseFullResponse(t, userResp)["data"].(map[string]interface{})
	if _, ok := userData["raw_source_path"]; ok {
		t.Fatalf("regular user detail unexpectedly exposes raw_source_path: %+v", userData)
	}
	if _, ok := userData["raw_payload"]; ok {
		t.Fatalf("regular user detail unexpectedly exposes raw_payload: %+v", userData)
	}
	matchedPRs := userData["matched_prs"].([]interface{})
	if len(matchedPRs) != 0 {
		t.Fatalf("matched_prs = %d, want 0 for fixture without PR snapshot", len(matchedPRs))
	}

	adminResp := doFullRequest(env, http.MethodGet, fmt.Sprintf("/api/v1/events/%d", event.ID), nil)
	if adminResp.Code != http.StatusOK {
		t.Fatalf("admin status = %d, want 200, body=%s", adminResp.Code, adminResp.Body.String())
	}
	adminData := parseFullResponse(t, adminResp)["data"].(map[string]interface{})
	if adminData["raw_source_path"] != "/Users/admin/.claude/projects/user-bound.jsonl" {
		t.Fatalf("raw_source_path = %v, want full path", adminData["raw_source_path"])
	}
	rawPayload := adminData["raw_payload"].(map[string]interface{})
	if rawPayload["scope"] != "user-bound" {
		t.Fatalf("raw_payload.scope = %v, want user-bound", rawPayload["scope"])
	}
}

func TestEventsSummaryCountsBoundAndUnboundForCurrentUser(t *testing.T) {
	t.Parallel()

	env := setupFullTestEnv(t)
	nonAdminToken := createFullNonAdminToken(t, env)
	seedEventsFixture(t, env)

	path := fmt.Sprintf("/api/v1/events/summary?from=%s&to=%s", time.Now().Add(-24*time.Hour).UTC().Format(time.RFC3339), time.Now().UTC().Format(time.RFC3339))
	w := doFullRequestWithToken(env, http.MethodGet, path, nil, nonAdminToken)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}

	data := parseFullResponse(t, w)["data"].(map[string]interface{})
	if got := int(data["total_events"].(float64)); got != 2 {
		t.Fatalf("total_events = %d, want 2", got)
	}
	if got := int(data["bound_events"].(float64)); got != 1 {
		t.Fatalf("bound_events = %d, want 1", got)
	}
	if got := int(data["unbound_events"].(float64)); got != 1 {
		t.Fatalf("unbound_events = %d, want 1", got)
	}
}
