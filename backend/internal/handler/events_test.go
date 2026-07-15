package handler

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
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
		SetRawSourcePath("/synthetic/users/alice/.claude/projects/user-bound.jsonl").
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

func TestEventsListDefaultsToTwenty(t *testing.T) {
	t.Parallel()

	env := setupFullTestEnv(t)
	createFullNonAdminToken(t, env)
	seedEventsFixture(t, env)

	w := doFullRequest(env, http.MethodGet, "/api/v1/events", nil)
	data := requireEventsListData(t, w)
	if got := int(data["page_size"].(float64)); got != 20 {
		t.Fatalf("page_size = %d, want 20", got)
	}
	if got := int(data["page"].(float64)); got != 0 {
		t.Fatalf("page = %d, want 0", got)
	}
}

func TestEventsListClampsLimitToOneHundred(t *testing.T) {
	t.Parallel()

	env := setupFullTestEnv(t)
	createFullNonAdminToken(t, env)
	seedEventsFixture(t, env)

	for _, limit := range []string{"101", "1000"} {
		t.Run("limit="+limit, func(t *testing.T) {
			w := doFullRequest(env, http.MethodGet, "/api/v1/events?limit="+limit, nil)
			data := requireEventsListData(t, w)
			if got := int(data["page_size"].(float64)); got != 100 {
				t.Fatalf("page_size = %d, want 100", got)
			}
			if got := int(data["page"].(float64)); got != 0 {
				t.Fatalf("page = %d, want 0", got)
			}
		})
	}
}

func TestEventsListNormalizesInvalidAndNegativePaging(t *testing.T) {
	t.Parallel()

	env := setupFullTestEnv(t)
	createFullNonAdminToken(t, env)
	seedEventsFixture(t, env)

	tests := []struct {
		name  string
		query string
	}{
		{name: "zero limit and negative offset", query: "limit=0&offset=-20"},
		{name: "invalid limit and offset", query: "limit=invalid&offset=invalid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := doFullRequest(env, http.MethodGet, "/api/v1/events?"+tt.query, nil)
			data := requireEventsListData(t, w)
			if got := int(data["page_size"].(float64)); got != 20 {
				t.Fatalf("page_size = %d, want 20", got)
			}
			if got := int(data["page"].(float64)); got != 0 {
				t.Fatalf("page = %d, want 0", got)
			}
		})
	}
}

func TestEventsListPreservesZeroBasedPageMetadata(t *testing.T) {
	t.Parallel()

	env := setupFullTestEnv(t)
	createFullNonAdminToken(t, env)
	seedEventsFixture(t, env)

	w := doFullRequest(env, http.MethodGet, "/api/v1/events?limit=20&offset=40", nil)
	data := requireEventsListData(t, w)
	if got := int(data["page_size"].(float64)); got != 20 {
		t.Fatalf("page_size = %d, want 20", got)
	}
	if got := int(data["page"].(float64)); got != 2 {
		t.Fatalf("page = %d, want 2", got)
	}
}

func TestEventsListOmitsRawPayloadAndRegularUsername(t *testing.T) {
	t.Parallel()

	env := setupFullTestEnv(t)
	nonAdminToken := createFullNonAdminToken(t, env)
	actors := seedEventsFixture(t, env)

	adminData := requireEventsListData(t, doFullRequest(env, http.MethodGet, "/api/v1/events", nil))
	for _, raw := range adminData["items"].([]interface{}) {
		row := raw.(map[string]interface{})
		if _, ok := row["raw_payload"]; ok {
			t.Fatalf("admin list unexpectedly exposes raw_payload: %+v", row)
		}
	}

	path := fmt.Sprintf("/api/v1/events?from=%s&to=%s&user_id=%d", time.Now().Add(-24*time.Hour).UTC().Format(time.RFC3339), time.Now().UTC().Format(time.RFC3339), actors.adminUserID)
	w := doFullRequestWithToken(env, http.MethodGet, path, nil, nonAdminToken)
	data := requireEventsListData(t, w)
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
		if _, ok := row["raw_payload"]; ok {
			t.Fatalf("non-admin list unexpectedly exposes raw_payload: %+v", row)
		}
	}
}

func requireEventsListData(t *testing.T, w *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	return parseFullResponse(t, w)["data"].(map[string]interface{})
}

func TestEventsUsersSearchAdminOnly(t *testing.T) {
	t.Parallel()

	env := setupFullTestEnv(t)
	nonAdminToken := createFullNonAdminToken(t, env)
	seedEventsFixture(t, env)

	userResp := doFullRequestWithToken(env, http.MethodGet, "/api/v1/events/users?q=cov&limit=20", nil, nonAdminToken)
	if userResp.Code != http.StatusForbidden {
		t.Fatalf("regular user status = %d, want 403, body=%s", userResp.Code, userResp.Body.String())
	}
}

func TestEventsUsersSearchReturnsUsersWithEventsForAdmin(t *testing.T) {
	t.Parallel()

	env := setupFullTestEnv(t)
	createFullNonAdminToken(t, env)
	seedEventsFixture(t, env)

	w := doFullRequest(env, http.MethodGet, "/api/v1/events/users?q=cov&limit=20", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}

	data := parseFullResponse(t, w)["data"].([]interface{})
	if len(data) != 1 {
		t.Fatalf("users=%d, want 1: %+v", len(data), data)
	}
	row := data[0].(map[string]interface{})
	if row["username"] != "covuser" {
		t.Fatalf("username=%v, want covuser", row["username"])
	}
	if int(row["event_count"].(float64)) != 2 {
		t.Fatalf("event_count=%v, want 2", row["event_count"])
	}
	if row["latest_event_at"] == "" {
		t.Fatalf("latest_event_at is empty: %+v", row)
	}
}

func TestEventDetailPreservesAdminPayloadAndRegularRedaction(t *testing.T) {
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
	matchedPRs := userData["matched_prs"].([]interface{})
	if len(matchedPRs) != 0 {
		t.Fatalf("matched_prs = %d, want 0 for fixture without PR snapshot", len(matchedPRs))
	}

	adminResp := doFullRequest(env, http.MethodGet, fmt.Sprintf("/api/v1/events/%d", event.ID), nil)
	if adminResp.Code != http.StatusOK {
		t.Fatalf("admin status = %d, want 200, body=%s", adminResp.Code, adminResp.Body.String())
	}
	adminData := parseFullResponse(t, adminResp)["data"].(map[string]interface{})

	adminOnlyFields := []struct {
		name string
		want interface{}
	}{
		{name: "username", want: "covuser"},
		{name: "raw_source_path", want: "/synthetic/users/alice/.claude/projects/user-bound.jsonl"},
		{name: "raw_source_locator", want: "line:10"},
		{name: "raw_payload", want: map[string]interface{}{"kind": "assistant", "scope": "user-bound"}},
	}
	for _, field := range adminOnlyFields {
		if _, ok := userData[field.name]; ok {
			t.Errorf("regular user detail unexpectedly exposes %s: %+v", field.name, userData)
		}
		got, ok := adminData[field.name]
		if !ok {
			t.Errorf("admin detail omits %s: %+v", field.name, adminData)
			continue
		}
		if !reflect.DeepEqual(got, field.want) {
			t.Errorf("admin detail %s = %#v, want %#v", field.name, got, field.want)
		}
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
