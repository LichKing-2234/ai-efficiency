package handler

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/ai-efficiency/backend/ent/repoconfig"
)

func TestRepairWebhookRouteRequiresAdmin(t *testing.T) {
	env := setupFullTestEnv(t)
	nonAdmin := issueFullTokenForRole(t, env, "repo-user", "user")

	w := doFullRequestWithToken(env, http.MethodPost, "/api/v1/repos/1/repair-webhook", map[string]any{"force": false}, nonAdmin)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusForbidden, w.Body.String())
	}
}

func TestRepairFailedWebhooksRouteRequiresAdmin(t *testing.T) {
	env := setupFullTestEnv(t)
	nonAdmin := issueFullTokenForRole(t, env, "repo-user-batch", "user")

	w := doFullRequestWithToken(env, http.MethodPost, "/api/v1/repos/repair-webhooks", map[string]any{"force": false}, nonAdmin)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusForbidden, w.Body.String())
	}
}

func TestRepairWebhookUnboundRepoReturnsConflict(t *testing.T) {
	env := setupFullTestEnv(t)
	repo := env.client.RepoConfig.Create().
		SetName("repo").
		SetFullName("PROJ/repo").
		SetCloneURL("https://bitbucket.example.com/scm/proj/repo.git").
		SetDefaultBranch("main").
		SetStatus(repoconfig.StatusWebhookFailed).
		SaveX(context.Background())

	w := doFullRequest(env, http.MethodPost, "/api/v1/repos/"+strconv.Itoa(repo.ID)+"/repair-webhook", map[string]any{"force": false})
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusConflict, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "repo_unbound") {
		t.Fatalf("body = %s, want repo_unbound", w.Body.String())
	}
}

func TestRepairFailedWebhooksEmptyBatchReturnsSummary(t *testing.T) {
	env := setupFullTestEnv(t)

	w := doFullRequest(env, http.MethodPost, "/api/v1/repos/repair-webhooks", map[string]any{"force": false})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	data := parseResponse(t, w)["data"].(map[string]any)
	summary := data["summary"].(map[string]any)
	if int(summary["scanned"].(float64)) != 0 {
		t.Fatalf("summary = %v, want scanned=0", summary)
	}
}
