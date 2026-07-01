package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/repoconfig"
)

func createEligibilityRepo(t *testing.T, client *ent.Client, status repoconfig.Status, cloneURL string) int {
	t.Helper()
	return client.RepoConfig.Create().
		SetName("repo").
		SetFullName("org/repo").
		SetCloneURL(cloneURL).
		SetDefaultBranch("main").
		SetStatus(status).
		SaveX(context.Background()).ID
}

func TestResolveRemoteEligibleForActiveRepo(t *testing.T) {
	t.Parallel()
	env := setupFullTestEnv(t)
	createEligibilityRepo(t, env.client, repoconfig.StatusActive, "https://repo-host.example.com/org/repo.git")

	w := doFullRequest(env, http.MethodPost, "/api/v1/repos/resolve-remote", map[string]any{
		"remote_url":           "git@repo-host.example.com:org/repo.git",
		"branch":               "main",
		"client_cache_version": "repo-eligibility-v1",
	})

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	data := parseResponse(t, w)["data"].(map[string]any)
	if data["eligible"] != true {
		t.Fatalf("eligible = %v, want true", data["eligible"])
	}
	if data["repo_key"] != "repo-host.example.com/org/repo" {
		t.Fatalf("repo_key = %v", data["repo_key"])
	}
	if int(data["repo_config_id"].(float64)) == 0 {
		t.Fatalf("repo_config_id missing: %v", data)
	}
}

func TestResolveRemoteDoesNotCreateUnknownRepo(t *testing.T) {
	t.Parallel()
	env := setupFullTestEnv(t)

	w := doFullRequest(env, http.MethodPost, "/api/v1/repos/resolve-remote", map[string]any{
		"remote_url": "https://repo-host.example.com/org/missing.git",
	})

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	data := parseResponse(t, w)["data"].(map[string]any)
	if data["eligible"] != false || data["reason"] != "not_found" {
		t.Fatalf("unexpected data: %v", data)
	}
	if count := env.client.RepoConfig.Query().CountX(context.Background()); count != 0 {
		t.Fatalf("repo count = %d, want 0", count)
	}
}

func TestResolveRemoteInactiveIsIneligible(t *testing.T) {
	t.Parallel()
	env := setupFullTestEnv(t)
	createEligibilityRepo(t, env.client, repoconfig.StatusInactive, "https://repo-host.example.com/org/repo.git")

	w := doFullRequest(env, http.MethodPost, "/api/v1/repos/resolve-remote", map[string]any{
		"remote_url": "https://repo-host.example.com/org/repo.git",
	})

	data := parseResponse(t, w)["data"].(map[string]any)
	if data["eligible"] != false || data["reason"] != "inactive" {
		t.Fatalf("unexpected data: %v", data)
	}
}

func TestResolveRemoteWebhookFailedIsEligible(t *testing.T) {
	t.Parallel()
	env := setupFullTestEnv(t)
	createEligibilityRepo(t, env.client, repoconfig.StatusWebhookFailed, "https://repo-host.example.com/org/repo.git")

	w := doFullRequest(env, http.MethodPost, "/api/v1/repos/resolve-remote", map[string]any{
		"remote_url": "https://repo-host.example.com/org/repo.git",
	})

	data := parseResponse(t, w)["data"].(map[string]any)
	if data["eligible"] != true {
		t.Fatalf("unexpected data: %v", data)
	}
}

func TestHookEligibleReturnsOnlyRequestedRepos(t *testing.T) {
	t.Parallel()
	env := setupFullTestEnv(t)
	createEligibilityRepo(t, env.client, repoconfig.StatusActive, "https://repo-host.example.com/org/repo.git")
	env.client.RepoConfig.Create().
		SetName("other").
		SetFullName("org/other").
		SetCloneURL("https://repo-host.example.com/org/other.git").
		SetDefaultBranch("main").
		SetStatus(repoconfig.StatusActive).
		SaveX(context.Background())

	w := doFullRequest(env, http.MethodPost, "/api/v1/repos/hook-eligible", map[string]any{
		"repos": []map[string]any{
			{"repo_key": "repo-host.example.com/org/repo", "remote_url": "https://repo-host.example.com/org/repo.git"},
			{"repo_key": "repo-host.example.com/org/missing", "remote_url": "https://repo-host.example.com/org/missing.git"},
		},
	})

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	data := parseResponse(t, w)["data"].(map[string]any)
	repos := data["repos"].([]any)
	if len(repos) != 1 {
		t.Fatalf("eligible repos len = %d, want 1; data=%v", len(repos), data)
	}
	if repos[0].(map[string]any)["repo_key"] != "repo-host.example.com/org/repo" {
		t.Fatalf("unexpected repo: %v", repos[0])
	}
	ineligible := data["ineligible"].([]any)
	if len(ineligible) != 1 || ineligible[0].(map[string]any)["reason"] != "not_found" {
		t.Fatalf("unexpected ineligible: %v", ineligible)
	}
	if data["version"] != "repo-eligibility-v1" {
		t.Fatalf("version = %v", data["version"])
	}
}
