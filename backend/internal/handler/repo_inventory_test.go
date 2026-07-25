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
	github := findInventoryProviderResponse(t, items, fmt.Sprintf("scm_provider:%d", githubID))
	if int(github["total_repos"].(float64)) != 2 {
		t.Fatalf("github total_repos = %v, want 2", github["total_repos"])
	}
	orgScope := findInventoryScopeResponse(t, github["scopes"].([]interface{}), "org")
	if int(orgScope["bound_repos"].(float64)) != 2 {
		t.Fatalf("github org bound_repos = %v, want 2", orgScope["bound_repos"])
	}

	bitbucket := findInventoryProviderResponse(t, items, fmt.Sprintf("scm_provider:%d", bitbucketID))
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

func TestRepoListEndpointReturnsStableDefaultSelection(t *testing.T) {
	env := setupTestEnv(t)

	bitbucketID := createHandlerSCMProvider(t, env, "A Bitbucket", "bitbucket_server", "https://bitbucket.example.com")
	githubZID := createHandlerSCMProvider(t, env, "Z GitHub", "github", "https://github-z.example.com")
	githubAID := createHandlerSCMProvider(t, env, "A GitHub", "github", "https://github-a.example.com")
	createHandlerRepo(t, env, bitbucketID, "repo-bitbucket", "alpha/repo-bitbucket", "https://bitbucket.example.com/alpha/repo-bitbucket.git")
	createHandlerRepo(t, env, githubZID, "repo-zeta", "zeta/repo-zeta", "https://github-z.example.com/zeta/repo-zeta.git")
	createHandlerRepo(t, env, githubAID, "repo-gamma", "gamma/repo-gamma", "https://github-a.example.com/gamma/repo-gamma.git")

	w := doRequest(env, http.MethodGet, "/api/v1/repos", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("default list status = %d, want %d, body: %s", w.Code, http.StatusOK, w.Body.String())
	}
	data := parseResponse(t, w)["data"].(map[string]interface{})
	selection, ok := data["selection"].(map[string]interface{})
	if !ok {
		t.Fatalf("default list selection = %#v, want object; body: %s", data["selection"], w.Body.String())
	}
	if int(selection["provider_id"].(float64)) != githubAID || selection["provider_key"] != fmt.Sprintf("scm_provider:%d", githubAID) || selection["scope"] != "gamma" || selection["binding_state"] != "bound" {
		t.Fatalf("default selection = %#v, want GitHub %d gamma/bound", selection, githubAID)
	}
	items := data["items"].([]interface{})
	if int(data["total"].(float64)) != 1 || len(items) != 1 || items[0].(map[string]interface{})["full_name"] != "gamma/repo-gamma" {
		t.Fatalf("default list data = %#v, want only gamma/repo-gamma", data)
	}
	if int(data["page"].(float64)) != 1 || int(data["page_size"].(float64)) != 20 {
		t.Fatalf("default pagination = page %v page_size %v, want 1/20", data["page"], data["page_size"])
	}
}

func TestRepoListEndpointSelectsUnboundDefault(t *testing.T) {
	env := setupTestEnv(t)
	createHandlerRepo(t, env, 0, "repo-zeta", "zeta/repo-zeta", "https://unknown.example.com/zeta/repo-zeta.git")
	createHandlerRepo(t, env, 0, "repo-alpha", "alpha/repo-alpha", "https://unknown.example.com/alpha/repo-alpha.git")

	w := doRequest(env, http.MethodGet, "/api/v1/repos", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("unbound default status = %d, body: %s", w.Code, w.Body.String())
	}
	data := parseResponse(t, w)["data"].(map[string]interface{})
	selection := data["selection"].(map[string]interface{})
	if selection["provider_key"] != "unbound" || selection["scope"] != "alpha" || selection["binding_state"] != "unbound" {
		t.Fatalf("unbound selection = %#v, want unbound/alpha", selection)
	}
	if _, ok := selection["provider_id"]; ok {
		t.Fatalf("unbound selection provider_id = %#v, want omitted", selection["provider_id"])
	}
	items := data["items"].([]interface{})
	if len(items) != 1 || items[0].(map[string]interface{})["full_name"] != "alpha/repo-alpha" {
		t.Fatalf("unbound items = %#v, want only alpha/repo-alpha", items)
	}
}

func TestRepoListEndpointEmptyDefaultHasNoSelection(t *testing.T) {
	env := setupTestEnv(t)

	w := doRequest(env, http.MethodGet, "/api/v1/repos", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("empty default status = %d, body: %s", w.Code, w.Body.String())
	}
	data := parseResponse(t, w)["data"].(map[string]interface{})
	if selection, ok := data["selection"]; ok && selection != nil {
		t.Fatalf("empty selection = %#v, want omitted or null", selection)
	}
	if int(data["total"].(float64)) != 0 || len(data["items"].([]interface{})) != 0 {
		t.Fatalf("empty default data = %#v, want empty page", data)
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
