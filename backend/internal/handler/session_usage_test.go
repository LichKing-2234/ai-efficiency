package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/ai-efficiency/backend/ent/sessionusageevent"
	entuser "github.com/ai-efficiency/backend/ent/user"
	"github.com/google/uuid"
)

func createOwnedSessionForUser(t *testing.T, env *fullTestEnv, userID int) uuid.UUID {
	t.Helper()
	repoID := createFullTestRepo(t, env.client)
	sessionID := uuid.New()
	env.client.Session.Create().
		SetID(sessionID).
		SetRepoConfigID(repoID).
		SetBranch("main").
		SetUserID(userID).
		SaveX(context.Background())
	return sessionID
}

func fullAdminUserID(t *testing.T, env *fullTestEnv) int {
	t.Helper()
	u := env.client.User.Query().Where(entuser.UsernameEQ("fulladmin")).OnlyX(context.Background())
	return u.ID
}

func TestSessionUsageIngest_CreatesUsageEvent(t *testing.T) {
	env := setupFullTestEnv(t)
	sessionID := createOwnedSessionForUser(t, env, fullAdminUserID(t, env))

	body := map[string]any{
		"event_id":      "usage-evt-1",
		"session_id":    sessionID.String(),
		"workspace_id":  "ws-1",
		"request_id":    "req-1",
		"provider_name": "sub2api",
		"model":         "gpt-5.4",
		"started_at":    "2026-04-02T10:00:00Z",
		"finished_at":   "2026-04-02T10:00:03Z",
		"input_tokens":  100,
		"output_tokens": 40,
		"total_tokens":  140,
		"status":        "completed",
	}
	w := doFullRequest(env, http.MethodPost, "/api/v1/session-usage-events", body)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusCreated, w.Body.String())
	}

	// idempotent duplicate no-op
	w = doFullRequest(env, http.MethodPost, "/api/v1/session-usage-events", body)
	if w.Code != http.StatusCreated {
		t.Fatalf("duplicate status = %d, want %d, body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
}

func TestSessionEventIngest_CreatesPromptEvent(t *testing.T) {
	env := setupFullTestEnv(t)
	sessionID := createOwnedSessionForUser(t, env, fullAdminUserID(t, env))

	body := map[string]any{
		"event_id":     "event-1",
		"session_id":   sessionID.String(),
		"workspace_id": "ws-1",
		"event_type":   "user_prompt_submit",
		"source":       "codex_hook",
		"captured_at":  "2026-04-02T10:00:00Z",
		"raw_payload":  map[string]any{"prompt": "explain the diff"},
	}
	w := doFullRequest(env, http.MethodPost, "/api/v1/session-events", body)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusCreated, w.Body.String())
	}

	// idempotent duplicate no-op
	w = doFullRequest(env, http.MethodPost, "/api/v1/session-events", body)
	if w.Code != http.StatusCreated {
		t.Fatalf("duplicate status = %d, want %d, body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
}

func TestSessionUsageIngest_RejectsCrossUserSession(t *testing.T) {
	env := setupFullTestEnv(t)
	sessionID := createOwnedSessionForUser(t, env, fullAdminUserID(t, env))
	otherToken := createFullNonAdminToken(t, env)

	w := doFullRequestWithToken(env, http.MethodPost, "/api/v1/session-usage-events", map[string]any{
		"event_id":      "usage-evt-cross-user",
		"session_id":    sessionID.String(),
		"workspace_id":  "ws-1",
		"request_id":    "req-1",
		"provider_name": "sub2api",
		"model":         "gpt-5.4",
		"started_at":    "2026-04-02T10:00:00Z",
		"finished_at":   "2026-04-02T10:00:03Z",
		"status":        "completed",
	}, otherToken)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusForbidden, w.Body.String())
	}
}

func TestSessionEventIngest_RejectsCrossUserSession(t *testing.T) {
	env := setupFullTestEnv(t)
	sessionID := createOwnedSessionForUser(t, env, fullAdminUserID(t, env))
	otherToken := createFullNonAdminToken(t, env)

	w := doFullRequestWithToken(env, http.MethodPost, "/api/v1/session-events", map[string]any{
		"event_id":     "event-cross-user",
		"session_id":   sessionID.String(),
		"workspace_id": "ws-1",
		"event_type":   "user_prompt_submit",
		"source":       "codex_hook",
		"captured_at":  "2026-04-02T10:00:00Z",
	}, otherToken)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusForbidden, w.Body.String())
	}
}

func TestSessionUsageIngest_RejectsInvalidPayload(t *testing.T) {
	env := setupFullTestEnv(t)
	sessionID := createOwnedSessionForUser(t, env, fullAdminUserID(t, env))

	w := doFullRequest(env, http.MethodPost, "/api/v1/session-usage-events", map[string]any{
		"event_id":      "usage-evt-invalid",
		"session_id":    sessionID.String(),
		"workspace_id":  "ws-1",
		"request_id":    "req-1",
		"provider_name": "sub2api",
		"model":         "gpt-5.4",
		"started_at":    "2026-04-02T10:00:00Z",
		"finished_at":   "2026-04-02T10:00:03Z",
		"input_tokens":  -1,
		"status":        "completed",
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("negative token status = %d, want %d, body=%s", w.Code, http.StatusBadRequest, w.Body.String())
	}

	w = doFullRequest(env, http.MethodPost, "/api/v1/session-usage-events", map[string]any{
		"event_id":      "usage-evt-invalid-time",
		"session_id":    sessionID.String(),
		"workspace_id":  "ws-1",
		"request_id":    "req-1",
		"provider_name": "sub2api",
		"model":         "gpt-5.4",
		"started_at":    "2026-04-02T10:00:03Z",
		"finished_at":   "2026-04-02T10:00:00Z",
		"status":        "completed",
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid time status = %d, want %d, body=%s", w.Code, http.StatusBadRequest, w.Body.String())
	}
}

func TestSessionUsageIngest_RejectsMissingSession(t *testing.T) {
	env := setupFullTestEnv(t)

	w := doFullRequest(env, http.MethodPost, "/api/v1/session-usage-events", map[string]any{
		"event_id":      "usage-evt-missing-session",
		"session_id":    uuid.New().String(),
		"workspace_id":  "ws-1",
		"request_id":    "req-1",
		"provider_name": "sub2api",
		"model":         "gpt-5.4",
		"started_at":    "2026-04-02T10:00:00Z",
		"finished_at":   "2026-04-02T10:00:03Z",
		"status":        "completed",
	})
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusUnprocessableEntity, w.Body.String())
	}
}

func TestSessionUsageIngest_StoresRawResponse(t *testing.T) {
	env := setupFullTestEnv(t)
	sessionID := createOwnedSessionForUser(t, env, fullAdminUserID(t, env))

	w := doFullRequest(env, http.MethodPost, "/api/v1/session-usage-events", map[string]any{
		"event_id":      "usage-evt-raw-http-1",
		"session_id":    sessionID.String(),
		"workspace_id":  "ws-1",
		"request_id":    "req-raw-1",
		"provider_name": "sub2api",
		"model":         "gpt-5.4",
		"started_at":    "2026-04-16T10:00:00Z",
		"finished_at":   "2026-04-16T10:00:01Z",
		"input_tokens":  27,
		"output_tokens": 10,
		"total_tokens":  37,
		"status":        "completed",
		"raw_response": map[string]any{
			"id": "resp_1",
		},
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusCreated, w.Body.String())
	}

	ev := env.client.SessionUsageEvent.Query().
		Where(sessionusageevent.EventIDEQ("usage-evt-raw-http-1")).
		OnlyX(context.Background())
	if ev.RawResponse == nil || ev.RawResponse["id"] != "resp_1" {
		t.Fatalf("raw_response = %+v, want id=resp_1", ev.RawResponse)
	}
}
