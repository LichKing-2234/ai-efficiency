package handler

import (
	"net/http"
	"testing"
)

func TestSessionUsageIngestRouteRemoved(t *testing.T) {
	env := setupFullTestEnv(t)
	w := doFullRequest(env, http.MethodPost, "/api/v1/session-usage-events", map[string]any{})
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusNotFound, w.Body.String())
	}
}

func TestSessionEventIngestRouteRemoved(t *testing.T) {
	env := setupFullTestEnv(t)
	w := doFullRequest(env, http.MethodPost, "/api/v1/session-events", map[string]any{})
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusNotFound, w.Body.String())
	}
}
