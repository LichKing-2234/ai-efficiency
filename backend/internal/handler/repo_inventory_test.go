package handler

import (
	"fmt"
	"net/http"
	"testing"
)

func TestRepoInventoryEndpointSummarizesProviderScopes(t *testing.T) {
	env := setupTestEnv(t)

	githubID := createHandlerSCMProvider(t, env, "GitHub Enterprise", "github", "https://github.example.com")
	bitbucketID := createHandlerSCMProvider(t, env, "Bitbucket Server", "bitbucket_server", "https://bitbucket.example.com")

	createHandlerRepo(t, env, githubID, "repo-a", "org/repo-a", "https://github.example.com/org/repo-a.git")
	createHandlerRepo(t, env, githubID, "repo-b", "org/repo-b", "https://github.example.com/org/repo-b.git")
	createHandlerRepo(t, env, bitbucketID, "repo-c", "PROJ/repo-c", "https://bitbucket.example.com/scm/proj/repo-c.git")
	createHandlerRepo(t, env, 0, "repo-unbound", "org/repo-unbound", "https://unknown.example.com/org/repo-unbound.git")

	w := doRequest(env, http.MethodGet, "/api/v1/repos/inventory", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("inventory status = %d, want %d, body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	resp := parseResponse(t, w)
	items := resp["data"].([]interface{})
	github := findInventoryProviderResponse(t, items, "GitHub Enterprise")
	if int(github["total_repos"].(float64)) != 2 {
		t.Fatalf("github total_repos = %v, want 2", github["total_repos"])
	}
	orgScope := findInventoryScopeResponse(t, github["scopes"].([]interface{}), "org")
	if int(orgScope["bound_repos"].(float64)) != 2 {
		t.Fatalf("github org bound_repos = %v, want 2", orgScope["bound_repos"])
	}

	bitbucket := findInventoryProviderResponse(t, items, "Bitbucket Server")
	projScope := findInventoryScopeResponse(t, bitbucket["scopes"].([]interface{}), "PROJ")
	if int(projScope["total_repos"].(float64)) != 1 {
		t.Fatalf("bitbucket PROJ total_repos = %v, want 1", projScope["total_repos"])
	}

	unbound := findInventoryProviderResponse(t, items, "unbound")
	if int(unbound["unbound_repos"].(float64)) != 1 {
		t.Fatalf("unbound repos = %v, want 1", unbound["unbound_repos"])
	}
}

func TestRepoListEndpointFiltersByProviderScopeAndBindingState(t *testing.T) {
	env := setupTestEnv(t)

	githubID := createHandlerSCMProvider(t, env, "GitHub Enterprise", "github", "https://github.example.com")
	bitbucketID := createHandlerSCMProvider(t, env, "Bitbucket Server", "bitbucket_server", "https://bitbucket.example.com")

	createHandlerRepo(t, env, githubID, "repo-a", "org/repo-a", "https://github.example.com/org/repo-a.git")
	createHandlerRepo(t, env, githubID, "repo-b", "sdk/repo-b", "https://github.example.com/sdk/repo-b.git")
	createHandlerRepo(t, env, bitbucketID, "repo-c", "org/repo-c", "https://bitbucket.example.com/scm/org/repo-c.git")
	createHandlerRepo(t, env, 0, "repo-unbound", "org/repo-unbound", "https://unknown.example.com/org/repo-unbound.git")

	w := doRequest(env, http.MethodGet, fmt.Sprintf("/api/v1/repos?scm_provider_id=%d&scope=org&binding_state=bound&page_size=20", githubID), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("scoped list status = %d, want %d, body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	resp := parseResponse(t, w)
	data := resp["data"].(map[string]interface{})
	items := data["items"].([]interface{})
	if int(data["total"].(float64)) != 1 || len(items) != 1 {
		t.Fatalf("scoped list total/items = %v/%d, want 1/1; body: %s", data["total"], len(items), w.Body.String())
	}
	first := items[0].(map[string]interface{})
	if first["full_name"] != "org/repo-a" {
		t.Fatalf("scoped list first repo = %v, want org/repo-a", first["full_name"])
	}
}

func createHandlerSCMProvider(t *testing.T, env *testEnv, name, providerType, baseURL string) int {
	t.Helper()
	w := doRequest(env, http.MethodPost, "/api/v1/scm-providers", map[string]interface{}{
		"name":        name,
		"type":        providerType,
		"base_url":    baseURL,
		"credentials": map[string]string{"token": "test-token"},
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("create provider %s status = %d, body: %s", name, w.Code, w.Body.String())
	}
	resp := parseResponse(t, w)
	data := resp["data"].(map[string]interface{})
	return int(data["id"].(float64))
}

func createHandlerRepo(t *testing.T, env *testEnv, providerID int, name, fullName, cloneURL string) {
	t.Helper()
	req := map[string]interface{}{
		"name":           name,
		"full_name":      fullName,
		"clone_url":      cloneURL,
		"default_branch": "main",
	}
	if providerID > 0 {
		req["scm_provider_id"] = providerID
	}
	w := doRequest(env, http.MethodPost, "/api/v1/repos/direct", req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create repo %s status = %d, body: %s", fullName, w.Code, w.Body.String())
	}
}

func findInventoryProviderResponse(t *testing.T, items []interface{}, key string) map[string]interface{} {
	t.Helper()
	for _, item := range items {
		provider := item.(map[string]interface{})
		if provider["provider_key"] == key {
			return provider
		}
	}
	t.Fatalf("provider %q missing in %#v", key, items)
	return nil
}

func findInventoryScopeResponse(t *testing.T, items []interface{}, scope string) map[string]interface{} {
	t.Helper()
	for _, item := range items {
		scopeItem := item.(map[string]interface{})
		if scopeItem["scope"] == scope {
			return scopeItem
		}
	}
	t.Fatalf("scope %q missing in %#v", scope, items)
	return nil
}
