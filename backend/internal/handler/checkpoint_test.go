package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ai-efficiency/backend/internal/auth"
	"github.com/ai-efficiency/backend/internal/checkpoint"
	"github.com/ai-efficiency/backend/internal/testdb"
	"github.com/gin-gonic/gin"
)

func withAuthUser(userID int, role string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set(auth.ContextKeyUser, &auth.UserContext{
			UserID:   userID,
			Username: "test-user",
			Role:     role,
		})
		c.Next()
	}
}

func TestCheckpointCommitHappyPath(t *testing.T) {
	t.Parallel()

	client := testdb.Open(t)
	ctx := context.Background()

	sp := client.ScmProvider.Create().
		SetName("github-test").
		SetType("github").
		SetBaseURL("https://api.github.com").
		SetCredentials("enc").
		SaveX(ctx)

	rc := client.RepoConfig.Create().
		SetScmProviderID(sp.ID).
		SetName("demo").
		SetFullName("org/demo").
		SetCloneURL("https://github.com/org/demo.git").
		SetDefaultBranch("main").
		SaveX(ctx)

	owner := client.User.Create().
		SetUsername("checkpoint-owner").
		SetEmail("checkpoint-owner@test.com").
		SetAuthSource("ldap").
		SaveX(ctx)

	h := NewCheckpointHandler(checkpoint.NewService(client))
	r := gin.New()
	r.Use(withAuthUser(owner.ID, "user"))
	r.POST("/commit", h.Commit)

	body := map[string]any{
		"event_id":       "evt-http-commit-1",
		"repo_full_name": rc.FullName,
		"workspace_id":   "ws-1",
		"commit_sha":     "abc123",
		"parent_shas":    []string{"p1"},
		"binding_source": "marker",
	}
	raw, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, "/commit", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body: %s", w.Code, http.StatusCreated, w.Body.String())
	}
}

func TestCheckpointRewriteHappyPath(t *testing.T) {
	t.Parallel()

	client := testdb.Open(t)
	ctx := context.Background()

	sp := client.ScmProvider.Create().
		SetName("github-test").
		SetType("github").
		SetBaseURL("https://api.github.com").
		SetCredentials("enc").
		SaveX(ctx)

	rc := client.RepoConfig.Create().
		SetScmProviderID(sp.ID).
		SetName("demo").
		SetFullName("org/demo").
		SetCloneURL("https://github.com/org/demo.git").
		SetDefaultBranch("main").
		SaveX(ctx)

	owner := client.User.Create().
		SetUsername("checkpoint-rewrite-owner").
		SetEmail("checkpoint-rewrite-owner@test.com").
		SetAuthSource("ldap").
		SaveX(ctx)

	h := NewCheckpointHandler(checkpoint.NewService(client))
	r := gin.New()
	r.Use(withAuthUser(owner.ID, "user"))
	r.POST("/rewrite", h.Rewrite)

	body := map[string]any{
		"event_id":       "evt-http-rewrite-1",
		"clone_url":      rc.CloneURL,
		"workspace_id":   "ws-2",
		"rewrite_type":   "rebase",
		"old_commit_sha": "old123",
		"new_commit_sha": "new123",
		"binding_source": "unbound",
	}
	raw, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, "/rewrite", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body: %s", w.Code, http.StatusCreated, w.Body.String())
	}
}

func TestCheckpointCommitBadJSON(t *testing.T) {
	t.Parallel()

	client := testdb.Open(t)
	h := NewCheckpointHandler(checkpoint.NewService(client))
	r := gin.New()
	r.Use(withAuthUser(1, "user"))
	r.POST("/commit", h.Commit)

	req, _ := http.NewRequest(http.MethodPost, "/commit", bytes.NewBufferString("{"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body: %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
}
