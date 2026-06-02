package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/ai-efficiency/backend/ent/repoconfig"
	"github.com/ai-efficiency/backend/ent/scmprovider"
	entuser "github.com/ai-efficiency/backend/ent/user"
	"github.com/ai-efficiency/backend/internal/auth"
)

func TestAutoBindUnboundRouteRequiresAdmin(t *testing.T) {
	env := setupFullTestEnv(t)
	nonAdmin := issueFullTokenForRole(t, env, "repo-user", "user")

	w := doFullRequestWithToken(env, http.MethodPost, "/api/v1/repos/auto-bind-unbound", nil, nonAdmin)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusForbidden, w.Body.String())
	}
}

func TestAutoBindUnboundRouteReturnsNoMatchSummary(t *testing.T) {
	env := setupFullTestEnv(t)
	env.client.RepoConfig.Create().
		SetName("platform").
		SetFullName("acme/platform").
		SetCloneURL("https://github.com/acme/platform.git").
		SetDefaultBranch("main").
		SetStatus(repoconfig.StatusActive).
		SaveX(context.Background())

	w := doFullRequest(env, http.MethodPost, "/api/v1/repos/auto-bind-unbound", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	data := parseResponse(t, w)["data"].(map[string]any)
	summary := data["summary"].(map[string]any)
	if int(summary["scanned"].(float64)) != 1 {
		t.Fatalf("summary = %v, want scanned=1", summary)
	}
	if int(summary["skipped_no_match"].(float64)) != 1 {
		t.Fatalf("summary = %v, want skipped_no_match=1", summary)
	}
}

func TestAutoBindUnboundRouteBindsMatchedRepo(t *testing.T) {
	env := setupFullTestEnv(t)
	ctx := context.Background()
	provider := env.client.ScmProvider.Create().
		SetName("GitHub").
		SetType(scmprovider.TypeGithub).
		SetBaseURL("https://api.github.com").
		SetStatus(scmprovider.StatusActive).
		SaveX(ctx)
	repo := env.client.RepoConfig.Create().
		SetName("platform").
		SetFullName("acme/platform").
		SetCloneURL("https://github.com/acme/platform.git").
		SetDefaultBranch("main").
		SetStatus(repoconfig.StatusActive).
		SaveX(ctx)

	w := doFullRequest(env, http.MethodPost, "/api/v1/repos/auto-bind-unbound", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	data := parseResponse(t, w)["data"].(map[string]any)
	summary := data["summary"].(map[string]any)
	if int(summary["scanned"].(float64)) != 1 || int(summary["bound"].(float64)) != 1 {
		t.Fatalf("summary = %v, want scanned=1 bound=1", summary)
	}
	items := data["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("items len = %d, want 1", len(items))
	}
	item := items[0].(map[string]any)
	if int(item["repo_config_id"].(float64)) != repo.ID {
		t.Fatalf("repo_config_id = %v, want %d", item["repo_config_id"], repo.ID)
	}
	if int(item["scm_provider_id"].(float64)) != provider.ID {
		t.Fatalf("scm_provider_id = %v, want %d", item["scm_provider_id"], provider.ID)
	}
	if item["result"] != "provider_error" {
		t.Fatalf("result = %v, want provider_error from missing test api credential", item["result"])
	}

	loaded := env.client.RepoConfig.Query().Where(repoconfig.IDEQ(repo.ID)).WithScmProvider().OnlyX(ctx)
	if loaded.Edges.ScmProvider == nil || loaded.Edges.ScmProvider.ID != provider.ID {
		t.Fatalf("bound provider = %#v, want %d", loaded.Edges.ScmProvider, provider.ID)
	}
}

func issueFullTokenForRole(t *testing.T, env *fullTestEnv, username, role string) string {
	t.Helper()
	user := env.client.User.Create().
		SetUsername(username).
		SetEmail(username + "@example.com").
		SetAuthSource("sub2api_sso").
		SetRole(entuser.Role(role)).
		SaveX(context.Background())
	token, err := env.authSvc.GenerateTokenPairForUser(&auth.UserInfo{
		ID:       user.ID,
		Username: username,
		Role:     role,
	})
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}
	return token.AccessToken
}
