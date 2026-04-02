package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/ent/enttest"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
)

func TestSessionDetailIncludesWorkspaceCheckpointUsageAndSessionEventEdges(t *testing.T) {
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
	client.SessionUsageEvent.Create().
		SetEventID("usage-1").
		SetSessionID(sessionID).
		SetWorkspaceID("ws-1").
		SetRequestID("req-1").
		SetProviderName("codex").
		SetModel("gpt-5").
		SetStartedAt(time.Now().UTC().Add(-2 * time.Minute)).
		SetFinishedAt(time.Now().UTC().Add(-1 * time.Minute)).
		SetInputTokens(100).
		SetOutputTokens(40).
		SetTotalTokens(140).
		SetStatus("completed").
		SetRawMetadata(map[string]any{"k": "v"}).
		SaveX(ctx)
	client.SessionEvent.Create().
		SetEventID("evt-1").
		SetSessionID(sessionID).
		SetWorkspaceID("ws-1").
		SetEventType("session.started").
		SetSource("cli").
		SetCapturedAt(time.Now().UTC().Add(-30 * time.Second)).
		SetRawPayload(map[string]any{"x": "y"}).
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
	usageEvents, _ := edges["session_usage_events"].([]any)
	if len(usageEvents) != 1 {
		t.Fatalf("session_usage_events len = %d, want 1", len(usageEvents))
	}
	sessionEvents, _ := edges["session_events"].([]any)
	if len(sessionEvents) != 1 {
		t.Fatalf("session_events len = %d, want 1", len(sessionEvents))
	}
}

func TestSessionDetailOrdersAndLimitsCheckpointUsageAndSessionEventEdges(t *testing.T) {
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

	baseWorkspace := time.Now().UTC().Add(-3 * time.Hour)
	for i := 0; i < 25; i++ {
		client.SessionWorkspace.Create().
			SetSessionID(sessionID).
			SetWorkspaceID("ws-" + uuid.NewString()).
			SetWorkspaceRoot("/tmp/repo").
			SetGitDir("/tmp/repo/.git").
			SetGitCommonDir("/tmp/repo/.git").
			SetBindingSource("marker").
			SetLastSeenAt(baseWorkspace.Add(time.Duration(i) * time.Minute)).
			SaveX(ctx)
	}

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
	usageBase := time.Now().UTC().Add(-90 * time.Minute)
	for i := 0; i < 120; i++ {
		client.SessionUsageEvent.Create().
			SetEventID("usage-limit-" + uuid.NewString()).
			SetSessionID(sessionID).
			SetWorkspaceID("ws-1").
			SetRequestID("req-" + uuid.NewString()).
			SetProviderName("codex").
			SetModel("gpt-5").
			SetStartedAt(usageBase.Add(time.Duration(i) * time.Minute)).
			SetFinishedAt(usageBase.Add(time.Duration(i)*time.Minute + 20*time.Second)).
			SetInputTokens(10).
			SetOutputTokens(2).
			SetTotalTokens(12).
			SetStatus("completed").
			SetRawMetadata(map[string]any{"i": i}).
			SaveX(ctx)
	}

	eventBase := time.Now().UTC().Add(-80 * time.Minute)
	for i := 0; i < 120; i++ {
		client.SessionEvent.Create().
			SetEventID("event-limit-" + uuid.NewString()).
			SetSessionID(sessionID).
			SetWorkspaceID("ws-1").
			SetEventType("checkpoint.captured").
			SetSource("cli").
			SetCapturedAt(eventBase.Add(time.Duration(i) * time.Minute)).
			SetRawPayload(map[string]any{"i": i}).
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
	workspaces, _ := edges["session_workspaces"].([]any)
	if len(workspaces) != 20 {
		t.Fatalf("session_workspaces len = %d, want 20", len(workspaces))
	}
	firstWorkspace, _ := workspaces[0].(map[string]any)
	lastWorkspace, _ := workspaces[len(workspaces)-1].(map[string]any)
	if firstWorkspace["last_seen_at"].(string) <= lastWorkspace["last_seen_at"].(string) {
		t.Fatalf("expected workspaces ordered desc by last_seen_at, got first=%v last=%v", firstWorkspace["last_seen_at"], lastWorkspace["last_seen_at"])
	}

	checkpoints, _ := edges["commit_checkpoints"].([]any)
	if len(checkpoints) != 50 {
		t.Fatalf("commit_checkpoints len = %d, want 50", len(checkpoints))
	}
	first, _ := checkpoints[0].(map[string]any)
	last, _ := checkpoints[len(checkpoints)-1].(map[string]any)
	if first["captured_at"].(string) <= last["captured_at"].(string) {
		t.Fatalf("expected checkpoints ordered desc by captured_at, got first=%v last=%v", first["captured_at"], last["captured_at"])
	}

	usageEvents, _ := edges["session_usage_events"].([]any)
	if len(usageEvents) != 100 {
		t.Fatalf("session_usage_events len = %d, want 100", len(usageEvents))
	}
	firstUsage, _ := usageEvents[0].(map[string]any)
	lastUsage, _ := usageEvents[len(usageEvents)-1].(map[string]any)
	if firstUsage["started_at"].(string) <= lastUsage["started_at"].(string) {
		t.Fatalf("expected usage events ordered desc by started_at, got first=%v last=%v", firstUsage["started_at"], lastUsage["started_at"])
	}

	sessionEvents, _ := edges["session_events"].([]any)
	if len(sessionEvents) != 100 {
		t.Fatalf("session_events len = %d, want 100", len(sessionEvents))
	}
	firstEvent, _ := sessionEvents[0].(map[string]any)
	lastEvent, _ := sessionEvents[len(sessionEvents)-1].(map[string]any)
	if firstEvent["captured_at"].(string) <= lastEvent["captured_at"].(string) {
		t.Fatalf("expected session events ordered desc by captured_at, got first=%v last=%v", firstEvent["captured_at"], lastEvent["captured_at"])
	}
}
