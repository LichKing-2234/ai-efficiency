package handler

import (
	"net/http"
	"testing"
)

func TestEnsureRemoteWithValidToken(t *testing.T) {
	env := setupTestEnv(t)

	w := doRequest(env, http.MethodPost, "/api/v1/repos/ensure-remote", map[string]any{
		"remote_url": "https://github.com/acme/platform.git",
		"branch":     "main",
	})

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	resp := parseResponse(t, w)
	data, ok := resp["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected data object, got: %v", resp)
	}
	if got := data["repo_key"]; got != "github.com/acme/platform" {
		t.Fatalf("repo_key = %v, want github.com/acme/platform", got)
	}
}
