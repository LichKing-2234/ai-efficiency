package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCreateSCMProviderRequiresAPICredential(t *testing.T) {
	env := setupTestEnv(t)

	body := `{"name":"GitHub","type":"github","base_url":"https://api.github.com","clone_protocol":"https"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/scm-providers", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+env.token)
	w := httptest.NewRecorder()
	env.router.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "api_credential_id") && !strings.Contains(w.Body.String(), "APICredentialID") {
		t.Fatalf("expected api_credential_id validation error, body=%s", w.Body.String())
	}
}
