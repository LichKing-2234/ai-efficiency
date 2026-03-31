package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/ent/enttest"
	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
	"github.com/gin-gonic/gin"
)

func TestSessionGetIncludesWorkspaceAndCheckpointEdges(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	client := enttest.Open(t, "sqlite3", "file:ent?mode=memory&_fk=1")
	defer client.Close()

	ctx := t.Context()
	sp := client.ScmProvider.Create().
		SetName("github-test").
		SetType("github").
		SetBaseURL("https://api.github.com").
		SetCredentials("enc").
		SaveX(ctx)
	repo := client.RepoConfig.Create().
		SetScmProviderID(sp.ID).
		SetName("demo").
		SetFullName("org/demo").
		SetCloneURL("https://github.com/org/demo.git").
		SetDefaultBranch("main").
		SaveX(ctx)
	sessionID := uuid.New()
	client.Session.Create().
		SetID(sessionID).
		SetRepoConfigID(repo.ID).
		SetBranch("feat/x").
		SetStartedAt(time.Now().UTC()).
		SaveX(ctx)
	client.SessionWorkspace.Create().
		SetSessionID(sessionID).
		SetWorkspaceID("ws-1").
		SetWorkspaceRoot("/tmp/repo").
		SetGitDir("/tmp/repo/.git").
		SetGitCommonDir("/tmp/repo/.git").
		SetBindingSource("marker").
		SetLastSeenAt(time.Now().UTC()).
		SaveX(ctx)
	client.CommitCheckpoint.Create().
		SetEventID("cp-1").
		SetSessionID(sessionID).
		SetWorkspaceID("ws-1").
		SetRepoConfigID(repo.ID).
		SetCommitSha("abc123").
		SetParentShas([]string{"000000"}).
		SetBindingSource("marker").
		SetCapturedAt(time.Now().UTC()).
		SaveX(ctx)

	h := NewSessionHandler(client, nil)
	r := gin.New()
	r.GET("/sessions/:id", h.Get)

	req := httptest.NewRequest(http.MethodGet, "/sessions/"+sessionID.String(), nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	data, _ := body["data"].(map[string]any)
	edges, _ := data["edges"].(map[string]any)
	workspaces, _ := edges["session_workspaces"].([]any)
	if len(workspaces) != 1 {
		t.Fatalf("session_workspaces len = %d, want 1", len(workspaces))
	}
	checkpoints, _ := edges["commit_checkpoints"].([]any)
	if len(checkpoints) != 1 {
		t.Fatalf("commit_checkpoints len = %d, want 1", len(checkpoints))
	}
}

func TestSessionGetOrdersAndLimitsCheckpointEdges(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	client := enttest.Open(t, "sqlite3", "file:ent?mode=memory&_fk=1")
	defer client.Close()

	ctx := t.Context()
	sp := client.ScmProvider.Create().
		SetName("github-test").
		SetType("github").
		SetBaseURL("https://api.github.com").
		SetCredentials("enc").
		SaveX(ctx)
	repo := client.RepoConfig.Create().
		SetScmProviderID(sp.ID).
		SetName("demo").
		SetFullName("org/demo").
		SetCloneURL("https://github.com/org/demo.git").
		SetDefaultBranch("main").
		SaveX(ctx)
	sessionID := uuid.New()
	client.Session.Create().
		SetID(sessionID).
		SetRepoConfigID(repo.ID).
		SetBranch("feat/x").
		SetStartedAt(time.Now().UTC()).
		SaveX(ctx)

	base := time.Now().UTC().Add(-2 * time.Hour)
	for i := 0; i < 60; i++ {
		client.CommitCheckpoint.Create().
			SetEventID("cp-limit-" + uuid.NewString()).
			SetSessionID(sessionID).
			SetWorkspaceID("ws-1").
			SetRepoConfigID(repo.ID).
			SetCommitSha(time.Now().UTC().Add(time.Duration(i) * time.Second).Format("20060102150405")).
			SetParentShas([]string{"000000"}).
			SetBindingSource("marker").
			SetCapturedAt(base.Add(time.Duration(i) * time.Minute)).
			SaveX(ctx)
	}

	h := NewSessionHandler(client, nil)
	r := gin.New()
	r.GET("/sessions/:id", h.Get)

	req := httptest.NewRequest(http.MethodGet, "/sessions/"+sessionID.String(), nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	data, _ := body["data"].(map[string]any)
	edges, _ := data["edges"].(map[string]any)
	checkpoints, _ := edges["commit_checkpoints"].([]any)
	if len(checkpoints) != 50 {
		t.Fatalf("commit_checkpoints len = %d, want 50", len(checkpoints))
	}
	first, _ := checkpoints[0].(map[string]any)
	last, _ := checkpoints[len(checkpoints)-1].(map[string]any)
	if first["captured_at"] == last["captured_at"] {
		t.Fatalf("expected ordered checkpoints, got identical endpoints")
	}
}
