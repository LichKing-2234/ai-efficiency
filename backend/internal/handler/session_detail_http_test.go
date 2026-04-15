package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/ent/user"
	"github.com/ai-efficiency/backend/internal/auth"
	"github.com/ai-efficiency/backend/internal/testdb"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func TestSessionDetailIncludesWorkspaceCheckpointUsageAndSessionEventEdges(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	client := testdb.Open(t)
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

	client := testdb.Open(t)
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

func TestSessionDetailAdminCanReadOtherUsersSessionButUserCannot(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	client := testdb.Open(t)
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

	owner := client.User.Create().
		SetUsername("owner").
		SetEmail("owner@example.com").
		SetAuthSource(user.AuthSourceLdap).
		SaveX(ctx)
	other := client.User.Create().
		SetUsername("other").
		SetEmail("other@example.com").
		SetAuthSource(user.AuthSourceLdap).
		SaveX(ctx)

	admin := client.User.Create().
		SetUsername("admin").
		SetEmail("admin@example.com").
		SetAuthSource(user.AuthSourceLdap).
		SetRole(user.RoleAdmin).
		SaveX(ctx)

	ownerSessionID := uuid.New()
	client.Session.Create().
		SetID(ownerSessionID).
		SetRepoConfigID(repo.ID).
		SetUserID(owner.ID).
		SetBranch("feat/x").
		SetStartedAt(time.Now().UTC()).
		SaveX(ctx)

	authSvc := auth.NewService(client, "test-jwt-secret-32-bytes-long!!!", 7200, 604800, nil)
	h := NewSessionHandler(client, nil)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		if strings.HasPrefix(header, "Bearer ") {
			token := strings.TrimPrefix(header, "Bearer ")
			claims, err := authSvc.ValidateAccessToken(token)
			if err == nil {
				userIDValue, _ := claims["user_id"].(float64)
				username, _ := claims["username"].(string)
				role, _ := claims["role"].(string)
				c.Set(auth.ContextKeyUser, &auth.UserContext{
					UserID:   int(userIDValue),
					Username: username,
					Role:     role,
				})
			}
		}
		c.Next()
	})
	r.GET("/sessions/:id", h.Get)

	makeToken := func(t *testing.T, id int, username, role string) string {
		t.Helper()
		pair, err := authSvc.GenerateTokenPairForUser(&auth.UserInfo{
			ID:       id,
			Username: username,
			Role:     role,
		})
		if err != nil {
			t.Fatalf("generate token: %v", err)
		}
		return pair.AccessToken
	}

	tests := []struct {
		name       string
		token      string
		wantStatus int
	}{
		{
			name:       "admin can read another users session",
			token:      makeToken(t, admin.ID, admin.Username, "admin"),
			wantStatus: http.StatusOK,
		},
		{
			name:       "owner can read own session",
			token:      makeToken(t, owner.ID, owner.Username, "user"),
			wantStatus: http.StatusOK,
		},
		{
			name:       "different non admin user gets not found",
			token:      makeToken(t, other.ID, other.Username, "user"),
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/sessions/"+ownerSessionID.String(), nil)
			req.Header.Set("Authorization", "Bearer "+tc.token)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d, body=%s", w.Code, tc.wantStatus, w.Body.String())
			}
		})
	}
}

func TestSessionListReturnsOnlyCurrentUserSessions(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	client := testdb.Open(t)
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

	owner := client.User.Create().
		SetUsername("owner-list").
		SetEmail("owner-list@example.com").
		SetAuthSource(user.AuthSourceLdap).
		SetRelayUserID(42).
		SetLdapDn("cn=owner-list,dc=example,dc=com").
		SaveX(ctx)
	other := client.User.Create().
		SetUsername("other-list").
		SetEmail("other-list@example.com").
		SetAuthSource(user.AuthSourceLdap).
		SaveX(ctx)

	ownerSessionID := uuid.New()
	client.Session.Create().
		SetID(ownerSessionID).
		SetRepoConfigID(repo.ID).
		SetUserID(owner.ID).
		SetProviderName("sub2api").
		SetRelayAPIKeyID(900).
		SetBranch("feat/owner").
		SetStartedAt(time.Now().UTC()).
		SaveX(ctx)
	client.Session.Create().
		SetID(uuid.New()).
		SetRepoConfigID(repo.ID).
		SetUserID(other.ID).
		SetBranch("feat/other").
		SetStartedAt(time.Now().UTC().Add(-1 * time.Minute)).
		SaveX(ctx)

	h := NewSessionHandler(client, nil)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(auth.ContextKeyUser, &auth.UserContext{
			UserID:   owner.ID,
			Username: "owner-list",
			Role:     "user",
		})
		c.Next()
	})
	r.GET("/sessions", h.List)

	req := httptest.NewRequest(http.MethodGet, "/sessions", nil)
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
	items, _ := data["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("items len = %d, want 1", len(items))
	}
	item, _ := items[0].(map[string]any)
	if item["id"] != ownerSessionID.String() {
		t.Fatalf("session id = %v, want %s", item["id"], ownerSessionID.String())
	}
	if item["relay_api_key_id"] != float64(900) {
		t.Fatalf("relay_api_key_id = %v, want %d", item["relay_api_key_id"], 900)
	}
	edges, _ := item["edges"].(map[string]any)
	ownerEdge, _ := edges["user"].(map[string]any)
	if ownerEdge["username"] != owner.Username {
		t.Fatalf("owner username = %v, want %q", ownerEdge["username"], owner.Username)
	}
	if _, ok := ownerEdge["relay_user_id"]; ok {
		t.Fatalf("owner edge unexpectedly exposed relay_user_id: %v", ownerEdge["relay_user_id"])
	}
	if _, ok := ownerEdge["ldap_dn"]; ok {
		t.Fatalf("owner edge unexpectedly exposed ldap_dn: %v", ownerEdge["ldap_dn"])
	}
}

func TestSessionListAdminCanFilterByOwnerQuery(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	client := testdb.Open(t)
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

	alice := client.User.Create().
		SetUsername("alice").
		SetEmail("alice@example.com").
		SetAuthSource(user.AuthSourceLdap).
		SaveX(ctx)
	bob := client.User.Create().
		SetUsername("bob").
		SetEmail("bob@example.com").
		SetAuthSource(user.AuthSourceLdap).
		SaveX(ctx)

	aliceSessionID := uuid.New()
	client.Session.Create().
		SetID(aliceSessionID).
		SetRepoConfigID(repo.ID).
		SetUserID(alice.ID).
		SetBranch("feat/alice").
		SetStartedAt(time.Now().UTC()).
		SaveX(ctx)
	client.Session.Create().
		SetID(uuid.New()).
		SetRepoConfigID(repo.ID).
		SetUserID(bob.ID).
		SetBranch("feat/bob").
		SetStartedAt(time.Now().UTC().Add(-1 * time.Minute)).
		SaveX(ctx)

	h := NewSessionHandler(client, nil)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(auth.ContextKeyUser, &auth.UserContext{
			UserID:   999,
			Username: "admin",
			Role:     "admin",
		})
		c.Next()
	})
	r.GET("/sessions", h.List)

	req := httptest.NewRequest(http.MethodGet, "/sessions?owner_scope=all&owner_query=ali", nil)
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
	items, _ := data["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("items len = %d, want 1", len(items))
	}
	item, _ := items[0].(map[string]any)
	if item["id"] != aliceSessionID.String() {
		t.Fatalf("session id = %v, want %s", item["id"], aliceSessionID.String())
	}
	edges, _ := item["edges"].(map[string]any)
	owner, _ := edges["user"].(map[string]any)
	if owner["username"] != "alice" {
		t.Fatalf("owner username = %v, want %q", owner["username"], "alice")
	}
}

func TestSessionListUnownedScopeIgnoresOwnerQuery(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	client := testdb.Open(t)
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

	client.User.Create().
		SetUsername("alice").
		SetEmail("alice@example.com").
		SetAuthSource(user.AuthSourceLdap).
		SaveX(ctx)

	unownedSessionID := uuid.New()
	client.Session.Create().
		SetID(unownedSessionID).
		SetRepoConfigID(repo.ID).
		SetBranch("feat/unowned").
		SetStartedAt(time.Now().UTC()).
		SaveX(ctx)
	client.Session.Create().
		SetID(uuid.New()).
		SetRepoConfigID(repo.ID).
		SetBranch("feat/owned").
		SetStartedAt(time.Now().UTC().Add(-1 * time.Minute)).
		SaveX(ctx)

	h := NewSessionHandler(client, nil)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(auth.ContextKeyUser, &auth.UserContext{
			UserID:   999,
			Username: "admin",
			Role:     "admin",
		})
		c.Next()
	})
	r.GET("/sessions", h.List)

	req := httptest.NewRequest(http.MethodGet, "/sessions?owner_scope=unowned&owner_query=alice", nil)
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
	items, _ := data["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("items len = %d, want 2", len(items))
	}
	for _, raw := range items {
		item, _ := raw.(map[string]any)
		if item["id"] == unownedSessionID.String() {
			return
		}
	}
	t.Fatalf("expected unowned session %s in response", unownedSessionID.String())
}
