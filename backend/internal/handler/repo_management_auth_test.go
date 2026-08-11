package handler

import (
	"net/http"
	"testing"
)

func TestRepositoryManagementIsAdminOnlyButCLIDiscoveryRemainsAuthenticated(t *testing.T) {
	env := setupFullTestEnv(t)
	token := issueFullTokenForRole(t, env, "repo-browser-user", "user")
	for _, request := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/repos"},
		{http.MethodGet, "/api/v1/repos/inventory"},
		{http.MethodPost, "/api/v1/repos/direct"},
		{http.MethodGet, "/api/v1/repos/1"},
		{http.MethodPost, "/api/v1/repos/1/sync-prs"},
	} {
		response := doFullRequestWithToken(env, request.method, request.path, nil, token)
		if response.Code != http.StatusForbidden {
			t.Fatalf("%s %s status=%d body=%s", request.method, request.path, response.Code, response.Body.String())
		}
	}
	response := doFullRequestWithToken(env, http.MethodPost, "/api/v1/repos/hook-eligible", map[string]any{"repos": []any{}}, token)
	if response.Code == http.StatusForbidden || response.Code == http.StatusUnauthorized {
		t.Fatalf("CLI discovery was placed behind admin auth: status=%d body=%s", response.Code, response.Body.String())
	}
}
