package handler

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/ent/commitcheckpoint"
	"github.com/ai-efficiency/backend/ent/toolusageevent"
)

type httpToolUsageScope struct {
	UserID       int
	RepoConfigID int
	WorkspaceID  string
}

func seedToolUsageScopeHTTP(t *testing.T, env *fullTestEnv) httpToolUsageScope {
	t.Helper()

	ctx := context.Background()
	repoID := createFullTestRepo(t, env.client)
	userID := fullAdminUserID(t, env)
	env.client.CommitCheckpoint.Create().
		SetEventID("cp-http-seed-scope").
		SetUserID(userID).
		SetWorkspaceID("ws-1").
		SetRepoConfigID(repoID).
		SetCommitSha("seed-sha").
		SetParentShas([]string{"base"}).
		SetBindingSource(commitcheckpoint.BindingSourceUnbound).
		SetCapturedAt(time.Unix(110, 0).UTC()).
		SaveX(ctx)

	return httpToolUsageScope{
		UserID:       userID,
		RepoConfigID: repoID,
		WorkspaceID:  "ws-1",
	}
}

func TestToolUsageHandler_CreateUsageEvent(t *testing.T) {
	t.Parallel()

	env := setupFullTestEnv(t)
	scope := seedToolUsageScopeHTTP(t, env)

	w := doFullRequest(env, http.MethodPost, "/api/v1/tool-usage-events", map[string]any{
		"tool":               "claude",
		"workspace_id":       scope.WorkspaceID,
		"tool_session_id":    "claude-sess-1",
		"tool_event_id":      "msg-1",
		"dedupe_key":         "claude:claude-sess-1:msg-1",
		"usage_unit":         "token",
		"input_tokens":       11,
		"output_tokens":      7,
		"observed_start_at":  "2026-05-13T10:00:00Z",
		"observed_end_at":    "2026-05-13T10:00:01Z",
		"raw_source_path":    "/Users/admin/.claude/projects/x.jsonl",
		"raw_source_locator": "line:42",
	})

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusCreated, w.Body.String())
	}

	ev := env.client.ToolUsageEvent.Query().
		Where(toolusageevent.DedupeKeyEQ("claude:claude-sess-1:msg-1")).
		OnlyX(context.Background())
	if ev.Tool != "claude" {
		t.Fatalf("tool = %q, want claude", ev.Tool)
	}
}

func TestToolUsageHandler_CreateUsageEventsBatch(t *testing.T) {
	t.Parallel()

	env := setupFullTestEnv(t)
	scope := seedToolUsageScopeHTTP(t, env)

	payload := map[string]any{
		"events": []map[string]any{
			{
				"repo_config_id":     scope.RepoConfigID,
				"tool":               "codex",
				"workspace_id":       scope.WorkspaceID,
				"tool_session_id":    "codex-sess-1",
				"tool_event_id":      "resp-1",
				"dedupe_key":         "codex:codex-sess-1:resp-1",
				"usage_unit":         "token",
				"input_tokens":       11,
				"output_tokens":      7,
				"observed_start_at":  "2026-05-13T10:00:00Z",
				"observed_end_at":    "2026-05-13T10:00:01Z",
				"raw_source_path":    "/Users/example/.codex/sessions/x.jsonl",
				"raw_source_locator": "line:42",
			},
			{
				"repo_config_id":    scope.RepoConfigID,
				"tool":              "codex",
				"workspace_id":      scope.WorkspaceID,
				"tool_session_id":   "codex-sess-1",
				"tool_event_id":     "resp-2",
				"dedupe_key":        "codex:codex-sess-1:resp-2",
				"usage_unit":        "token",
				"input_tokens":      13,
				"output_tokens":     5,
				"observed_start_at": "2026-05-13T10:00:02Z",
				"observed_end_at":   "2026-05-13T10:00:03Z",
			},
		},
	}

	w := doFullRequest(env, http.MethodPost, "/api/v1/tool-usage-events/batch", payload)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
	data := parseFullResponse(t, w)["data"].(map[string]interface{})
	if got := int(data["created"].(float64)); got != 2 {
		t.Fatalf("created = %d, want 2", got)
	}

	w = doFullRequest(env, http.MethodPost, "/api/v1/tool-usage-events/batch", payload)
	if w.Code != http.StatusCreated {
		t.Fatalf("second status = %d, want %d, body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
	data = parseFullResponse(t, w)["data"].(map[string]interface{})
	if got := int(data["duplicates"].(float64)); got != 2 {
		t.Fatalf("duplicates = %d, want 2", got)
	}

	count := env.client.ToolUsageEvent.Query().
		Where(toolusageevent.DedupeKeyIn("codex:codex-sess-1:resp-1", "codex:codex-sess-1:resp-2")).
		CountX(context.Background())
	if count != 2 {
		t.Fatalf("tool usage row count = %d, want 2", count)
	}
}

func TestToolUsageHandler_CreateUsageEventRejectsCrossUserScope(t *testing.T) {
	t.Parallel()

	env := setupFullTestEnv(t)
	scope := seedToolUsageScopeHTTP(t, env)
	otherToken := createFullNonAdminToken(t, env)

	w := doFullRequestWithToken(env, http.MethodPost, "/api/v1/tool-usage-events", map[string]any{
		"tool":               "claude",
		"workspace_id":       scope.WorkspaceID,
		"tool_session_id":    "claude-sess-1",
		"tool_event_id":      "msg-cross-user-1",
		"dedupe_key":         "claude:claude-sess-1:msg-cross-user-1",
		"usage_unit":         "token",
		"input_tokens":       11,
		"output_tokens":      7,
		"observed_start_at":  "2026-05-13T10:00:00Z",
		"observed_end_at":    "2026-05-13T10:00:01Z",
		"raw_source_path":    "/Users/admin/.claude/projects/x.jsonl",
		"raw_source_locator": "line:42",
	}, otherToken)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusForbidden, w.Body.String())
	}
}

func TestToolUsageHandler_BindUsageEventsRouteRemoved(t *testing.T) {
	t.Parallel()

	env := setupFullTestEnv(t)
	w := doFullRequest(env, http.MethodPost, "/api/v1/tool-usage-events/bind", map[string]any{})

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusNotFound, w.Body.String())
	}
}
