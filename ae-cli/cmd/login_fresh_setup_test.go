package cmd

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ai-efficiency/ae-cli/config"
	"github.com/ai-efficiency/ae-cli/internal/auth"
	"github.com/ai-efficiency/ae-cli/internal/reporting"
)

// A machine's very first login has no token when the command starts, so the
// global client is built unauthenticated. The setup that follows the OAuth flow
// must not use that stale client: it listed providers as nobody, the lookup
// failed unauthorized, and the relay provider was never recorded — commit
// attribution stayed off until a second login happened to run with the token
// already on disk.
func TestFirstLoginRecordsTheProviderWithTheFreshToken(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv(skipPilotEnv, "1")

	var providerAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "provider") {
			providerAuth = r.Header.Get("Authorization")
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"providers": []map[string]any{{
				"id": 7, "name": "sub2api", "display_name": "Sub2API", "is_primary": true,
			}}}})
			return
		}
		http.Error(w, `{"message":"not found"}`, http.StatusNotFound)
	}))
	defer server.Close()

	oldCfg, oldLogin, oldClient := cfg, loginFlow, apiClient
	oldLoad, oldSave, oldHooks := setupLoadReporting, setupSaveProviderID, setupEnableHooks
	oldHeadless := headlessBrowserEnv
	t.Cleanup(func() {
		cfg, loginFlow, apiClient = oldCfg, oldLogin, oldClient
		setupLoadReporting, setupSaveProviderID, setupEnableHooks = oldLoad, oldSave, oldHooks
		headlessBrowserEnv = oldHeadless
	})
	// The browser branch is what this test exercises, and the OAuth flow inside
	// it is stubbed. Without this the command refuses before reaching the flow
	// on any headless runner, which is every CI runner.
	headlessBrowserEnv = func(func(string) string, string) bool { return false }
	cfg = &config.Config{Server: config.ServerConfig{URL: server.URL}}
	apiClient = nil // what a fresh machine has before any token exists
	loginFlow = func(context.Context, auth.OAuthConfig) (*auth.OAuthResult, error) {
		return &auth.OAuthResult{AccessToken: "fresh-token", ExpiresIn: 3600}, nil
	}
	setupLoadReporting = func() (*reporting.Config, error) { return &reporting.Config{}, nil }
	savedProvider := 0
	setupSaveProviderID = func(id int) error { savedProvider = id; return nil }
	setupEnableHooks = func(string) error { return nil }

	if err := loginCmd.RunE(loginCmd, nil); err != nil {
		t.Fatalf("login: %v", err)
	}
	if savedProvider != 7 {
		t.Fatalf("saved provider = %d, want 7 recorded on the very first login", savedProvider)
	}
	if providerAuth != "Bearer fresh-token" {
		t.Fatalf("provider lookup authorization = %q, want the token the login just obtained", providerAuth)
	}
}
