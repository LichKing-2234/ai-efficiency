package handler

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/internal/auth"
)

// --- helpers ---

// createTestRepo creates an SCM provider + repo config and returns the repo ID.
func createTestRepo(t *testing.T, client *ent.Client) int {
	t.Helper()
	ctx := context.Background()

	provider, err := client.ScmProvider.Create().
		SetName("test-github").
		SetType("github").
		SetBaseURL("https://api.github.com").
		SetCredentials("encrypted").
		Save(ctx)
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}

	rc, err := client.RepoConfig.Create().
		SetScmProviderID(provider.ID).
		SetName("test-repo").
		SetFullName("org/test-repo").
		SetCloneURL("https://github.com/org/test-repo.git").
		SetDefaultBranch("main").
		Save(ctx)
	if err != nil {
		t.Fatalf("create repo: %v", err)
	}
	return rc.ID
}

// createNonAdminToken creates a regular user and returns a JWT token.
func createNonAdminToken(t *testing.T, env *testEnv) string {
	t.Helper()
	u, err := env.client.User.Create().
		SetUsername("regularuser").
		SetEmail("user@test.com").
		SetAuthSource("ldap").
		SetRole("user").
		Save(context.Background())
	if err != nil {
		t.Fatalf("create regular user: %v", err)
	}

	pair, err := env.authSvc.GenerateTokenPairForUser(&auth.UserInfo{
		ID:       u.ID,
		Username: "regularuser",
		Role:     "user",
	})
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}
	return pair.AccessToken
}

// =====================
// Analysis handler tests
// =====================

func TestListScansInvalidID(t *testing.T) {
	env := setupTestEnv(t)
	w := doRequest(env, "GET", "/api/v1/repos/abc/scans", nil)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d, body: %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
}

func TestListScansEmpty(t *testing.T) {
	env := setupTestEnv(t)
	repoID := createTestRepo(t, env.client)

	w := doRequest(env, "GET", fmt.Sprintf("/api/v1/repos/%d/scans", repoID), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	resp := parseResponse(t, w)
	code := int(resp["code"].(float64))
	if code != 200 {
		t.Errorf("code = %d, want 200", code)
	}

	// data should be an empty list
	data := resp["data"]
	if data == nil {
		// nil is acceptable for an empty list
		return
	}
	if items, ok := data.([]interface{}); ok {
		if len(items) != 0 {
			t.Errorf("expected empty list, got %d items", len(items))
		}
	}
}

func TestLatestScanNotFound(t *testing.T) {
	env := setupTestEnv(t)
	repoID := createTestRepo(t, env.client)

	w := doRequest(env, "GET", fmt.Sprintf("/api/v1/repos/%d/scans/latest", repoID), nil)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d, body: %s", w.Code, http.StatusNotFound, w.Body.String())
	}
}

func TestOptimizeNoOptimizer(t *testing.T) {
	// setupTestEnv passes nil for optimizer, so this should return 503
	env := setupTestEnv(t)
	repoID := createTestRepo(t, env.client)

	w := doRequest(env, "POST", fmt.Sprintf("/api/v1/repos/%d/optimize", repoID), nil)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d, body: %s", w.Code, http.StatusServiceUnavailable, w.Body.String())
	}
}

// =====================
// PR handler tests
// =====================

func TestListPRsByRepo(t *testing.T) {
	env := setupTestEnv(t)
	repoID := createTestRepo(t, env.client)
	ctx := context.Background()

	// Create PR records directly via ent
	for i := 1; i <= 3; i++ {
		_, err := env.client.PrRecord.Create().
			SetScmPrID(i).
			SetTitle(fmt.Sprintf("PR #%d", i)).
			SetAuthor("dev").
			SetSourceBranch(fmt.Sprintf("feature-%d", i)).
			SetTargetBranch("main").
			SetRepoConfigID(repoID).
			Save(ctx)
		if err != nil {
			t.Fatalf("create PR record %d: %v", i, err)
		}
	}

	w := doRequest(env, "GET", fmt.Sprintf("/api/v1/repos/%d/prs?months=0", repoID), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	resp := parseResponse(t, w)
	data := resp["data"].(map[string]interface{})
	total := int(data["total"].(float64))
	if total != 3 {
		t.Errorf("total = %d, want 3", total)
	}
	items := data["items"].([]interface{})
	if len(items) != 3 {
		t.Errorf("items count = %d, want 3", len(items))
	}
}

func TestListPRsByRepoEmpty(t *testing.T) {
	env := setupTestEnv(t)
	repoID := createTestRepo(t, env.client)

	w := doRequest(env, "GET", fmt.Sprintf("/api/v1/repos/%d/prs", repoID), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	resp := parseResponse(t, w)
	data := resp["data"].(map[string]interface{})
	total := int(data["total"].(float64))
	if total != 0 {
		t.Errorf("total = %d, want 0", total)
	}
	items := data["items"].([]interface{})
	if len(items) != 0 {
		t.Errorf("items count = %d, want 0", len(items))
	}
}

func TestGetPR(t *testing.T) {
	env := setupTestEnv(t)
	repoID := createTestRepo(t, env.client)
	ctx := context.Background()

	pr, err := env.client.PrRecord.Create().
		SetScmPrID(42).
		SetTitle("Fix the thing").
		SetAuthor("dev").
		SetSourceBranch("fix-thing").
		SetTargetBranch("main").
		SetRepoConfigID(repoID).
		Save(ctx)
	if err != nil {
		t.Fatalf("create PR: %v", err)
	}

	w := doRequest(env, "GET", fmt.Sprintf("/api/v1/prs/%d", pr.ID), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	resp := parseResponse(t, w)
	data := resp["data"].(map[string]interface{})
	if data["title"] != "Fix the thing" {
		t.Errorf("title = %v, want 'Fix the thing'", data["title"])
	}
}

func TestGetPRNotFound(t *testing.T) {
	env := setupTestEnv(t)

	w := doRequest(env, "GET", "/api/v1/prs/99999", nil)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d, body: %s", w.Code, http.StatusNotFound, w.Body.String())
	}
}

// =====================
// Efficiency handler tests
// =====================

func TestDashboard(t *testing.T) {
	env := setupTestEnv(t)

	w := doRequest(env, "GET", "/api/v1/efficiency/dashboard", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	resp := parseResponse(t, w)
	data := resp["data"].(map[string]interface{})

	// Verify expected keys exist
	for _, key := range []string{"total_repos", "active_sessions", "avg_ai_score", "total_ai_prs"} {
		if _, ok := data[key]; !ok {
			t.Errorf("missing key %q in dashboard response", key)
		}
	}
}

func TestRepoMetrics(t *testing.T) {
	env := setupTestEnv(t)
	repoID := createTestRepo(t, env.client)

	w := doRequest(env, "GET", fmt.Sprintf("/api/v1/efficiency/repos/%d/metrics", repoID), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	resp := parseResponse(t, w)
	code := int(resp["code"].(float64))
	if code != 200 {
		t.Errorf("code = %d, want 200", code)
	}
}

func TestTrend(t *testing.T) {
	env := setupTestEnv(t)
	repoID := createTestRepo(t, env.client)

	w := doRequest(env, "GET", fmt.Sprintf("/api/v1/efficiency/repos/%d/trend", repoID), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	resp := parseResponse(t, w)
	code := int(resp["code"].(float64))
	if code != 200 {
		t.Errorf("code = %d, want 200", code)
	}
}

func TestAggregateRequiresAdmin(t *testing.T) {
	env := setupTestEnv(t)

	// Replace token with a non-admin user token
	env.token = createNonAdminToken(t, env)

	w := doRequest(env, "POST", "/api/v1/efficiency/aggregate", nil)
	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d, body: %s", w.Code, http.StatusForbidden, w.Body.String())
	}
}

// =====================
// Settings handler tests
// =====================

func TestMaskAPIKey(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"sk-1234567890abcdef", "sk-****cdef"},
		{"short", "****"},
		{"12345678", "****"},       // exactly 8 chars
		{"123456789", "123****6789"}, // 9 chars — first 3 + **** + last 4
	}

	for _, tt := range tests {
		got := maskAPIKey(tt.input)
		if got != tt.want {
			t.Errorf("maskAPIKey(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestMaskAPIKeyEmpty(t *testing.T) {
	got := maskAPIKey("")
	if got != "****" {
		t.Errorf("maskAPIKey(\"\") = %q, want \"****\"", got)
	}
}

// =====================
// Chat handler tests
// =====================

func TestChatHandlerCheckRate(t *testing.T) {
	ch := &ChatHandler{
		counters: make(map[int]*rateBucket),
	}

	userID := 1

	// First 60 requests should be allowed
	for i := 0; i < 60; i++ {
		if !ch.checkRate(userID) {
			t.Fatalf("checkRate returned false on request %d, expected true", i+1)
		}
	}

	// 61st request should be blocked
	if ch.checkRate(userID) {
		t.Error("checkRate returned true on request 61, expected false (rate limit exceeded)")
	}
}

func TestBuildChatSystemPrompt(t *testing.T) {
	ch := &ChatHandler{
		dataDir: t.TempDir(),
	}

	// Create a minimal RepoConfig-like struct. We need an *ent.RepoConfig.
	// Since buildChatSystemPrompt only reads rc.FullName and rc.ID, we can
	// create one via the ent client.
	env := setupTestEnv(t)
	repoID := createTestRepo(t, env.client)
	rc, err := env.client.RepoConfig.Get(context.Background(), repoID)
	if err != nil {
		t.Fatalf("get repo config: %v", err)
	}

	prompt := ch.buildChatSystemPrompt(rc, nil)

	if !strings.Contains(prompt, "org/test-repo") {
		t.Errorf("prompt should contain repo full name 'org/test-repo', got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "AI code analysis assistant") {
		t.Errorf("prompt should contain 'AI code analysis assistant', got:\n%s", prompt)
	}
}
