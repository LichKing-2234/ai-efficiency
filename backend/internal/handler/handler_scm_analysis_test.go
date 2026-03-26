package handler

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/ai-efficiency/backend/ent/aiscanresult"
)

// =====================
// SCM Provider handler tests
// =====================

func TestSCMProviderTest(t *testing.T) {
	env := setupTestEnv(t)

	// Create provider via API
	createReq := map[string]interface{}{
		"name":        "TestGH",
		"type":        "github",
		"base_url":    "https://api.github.com",
		"credentials": map[string]string{"token": "ghp_test"},
	}
	w := doRequest(env, "POST", "/api/v1/scm-providers", createReq)
	if w.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want %d, body: %s", w.Code, http.StatusCreated, w.Body.String())
	}
	resp := parseResponse(t, w)
	data := resp["data"].(map[string]interface{})
	providerID := int(data["id"].(float64))

	// POST /api/v1/scm-providers/:id/test -> 200
	w = doRequest(env, "POST", fmt.Sprintf("/api/v1/scm-providers/%d/test", providerID), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("test status = %d, want %d, body: %s", w.Code, http.StatusOK, w.Body.String())
	}
	resp = parseResponse(t, w)
	respData := resp["data"].(map[string]interface{})
	if respData["status"] != "ok" {
		t.Errorf("status = %v, want ok", respData["status"])
	}
	if respData["message"] != "connection test passed" {
		t.Errorf("message = %v, want 'connection test passed'", respData["message"])
	}
}

func TestSCMProviderTest_NotFound(t *testing.T) {
	env := setupTestEnv(t)

	w := doRequest(env, "POST", "/api/v1/scm-providers/99999/test", nil)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d, body: %s", w.Code, http.StatusNotFound, w.Body.String())
	}
}

func TestSCMProviderTest_InvalidID(t *testing.T) {
	env := setupTestEnv(t)

	w := doRequest(env, "POST", "/api/v1/scm-providers/abc/test", nil)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d, body: %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
}

func TestSCMProviderUpdate_NotFound(t *testing.T) {
	env := setupTestEnv(t)

	updateReq := map[string]interface{}{
		"name": "Updated Name",
	}
	w := doRequest(env, "PUT", "/api/v1/scm-providers/99999", updateReq)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d, body: %s", w.Code, http.StatusNotFound, w.Body.String())
	}
}

func TestSCMProviderUpdate_InvalidID(t *testing.T) {
	env := setupTestEnv(t)

	updateReq := map[string]interface{}{
		"name": "Updated Name",
	}
	w := doRequest(env, "PUT", "/api/v1/scm-providers/abc", updateReq)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d, body: %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
}

func TestSCMProviderUpdate_WithCredentials(t *testing.T) {
	env := setupTestEnv(t)

	// Create provider first
	createReq := map[string]interface{}{
		"name":        "CredGH",
		"type":        "github",
		"base_url":    "https://api.github.com",
		"credentials": map[string]string{"token": "ghp_original"},
	}
	w := doRequest(env, "POST", "/api/v1/scm-providers", createReq)
	if w.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want %d, body: %s", w.Code, http.StatusCreated, w.Body.String())
	}
	resp := parseResponse(t, w)
	data := resp["data"].(map[string]interface{})
	providerID := int(data["id"].(float64))

	// Update with new credentials — covers the encryption branch in Update
	updateReq := map[string]interface{}{
		"name":        "CredGH Updated",
		"credentials": map[string]string{"token": "ghp_new_token"},
	}
	w = doRequest(env, "PUT", fmt.Sprintf("/api/v1/scm-providers/%d", providerID), updateReq)
	if w.Code != http.StatusOK {
		t.Fatalf("update status = %d, want %d, body: %s", w.Code, http.StatusOK, w.Body.String())
	}
	resp = parseResponse(t, w)
	updatedData := resp["data"].(map[string]interface{})
	if updatedData["name"] != "CredGH Updated" {
		t.Errorf("name = %v, want 'CredGH Updated'", updatedData["name"])
	}

	// Verify the test endpoint still works (credentials were re-encrypted properly)
	w = doRequest(env, "POST", fmt.Sprintf("/api/v1/scm-providers/%d/test", providerID), nil)
	if w.Code != http.StatusOK {
		t.Errorf("test after update status = %d, want %d, body: %s", w.Code, http.StatusOK, w.Body.String())
	}
}

func TestSCMProviderDelete_InvalidID(t *testing.T) {
	env := setupTestEnv(t)

	w := doRequest(env, "DELETE", "/api/v1/scm-providers/abc", nil)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d, body: %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
}

func TestSCMProviderCreate_InvalidBody(t *testing.T) {
	env := setupTestEnv(t)

	// Missing required fields
	createReq := map[string]interface{}{
		"name": "Incomplete",
	}
	w := doRequest(env, "POST", "/api/v1/scm-providers", createReq)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d, body: %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
}

// =====================
// Analysis handler tests
// =====================

func TestAnalysisTriggerScan_InvalidID(t *testing.T) {
	env := setupTestEnv(t)

	w := doRequest(env, "POST", "/api/v1/repos/abc/scan", nil)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d, body: %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
}

func TestOptimizePreview_NoOptimizer(t *testing.T) {
	env := setupTestEnv(t)
	repoID := createTestRepo(t, env.client)

	w := doRequest(env, "POST", fmt.Sprintf("/api/v1/repos/%d/optimize/preview", repoID), nil)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d, body: %s", w.Code, http.StatusServiceUnavailable, w.Body.String())
	}
}

func TestOptimizePreview_InvalidID(t *testing.T) {
	env := setupTestEnv(t)

	w := doRequest(env, "POST", "/api/v1/repos/abc/optimize/preview", nil)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d, body: %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
}

func TestOptimizeConfirm_NoOptimizer(t *testing.T) {
	env := setupTestEnv(t)
	repoID := createTestRepo(t, env.client)

	body := map[string]interface{}{
		"files": map[string]string{".github/workflows/ci.yml": "content"},
		"score": 85,
	}
	w := doRequest(env, "POST", fmt.Sprintf("/api/v1/repos/%d/optimize/confirm", repoID), body)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d, body: %s", w.Code, http.StatusServiceUnavailable, w.Body.String())
	}
}

func TestOptimizeConfirm_InvalidID(t *testing.T) {
	env := setupTestEnv(t)

	w := doRequest(env, "POST", "/api/v1/repos/abc/optimize/confirm", nil)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d, body: %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
}

func TestLatestScan_InvalidID(t *testing.T) {
	env := setupTestEnv(t)

	w := doRequest(env, "GET", "/api/v1/repos/abc/scans/latest", nil)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d, body: %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
}

func TestListScans_WithResults(t *testing.T) {
	env := setupTestEnv(t)
	repoID := createTestRepo(t, env.client)
	ctx := context.Background()

	// Create scan results directly via ent
	for i := 0; i < 3; i++ {
		_, err := env.client.AiScanResult.Create().
			SetRepoConfigID(repoID).
			SetScore(60 + i*10).
			SetScanType(aiscanresult.ScanTypeStatic).
			SetDimensions(map[string]interface{}{
				"ci_cd":    80,
				"security": 70,
			}).
			SetSuggestions([]map[string]interface{}{
				{"title": fmt.Sprintf("suggestion-%d", i), "severity": "medium"},
			}).
			Save(ctx)
		if err != nil {
			t.Fatalf("create scan result %d: %v", i, err)
		}
	}

	w := doRequest(env, "GET", fmt.Sprintf("/api/v1/repos/%d/scans", repoID), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	resp := parseResponse(t, w)
	data, ok := resp["data"].([]interface{})
	if !ok {
		t.Fatalf("expected data to be a list, got: %T", resp["data"])
	}
	if len(data) != 3 {
		t.Errorf("scans count = %d, want 3", len(data))
	}

	// Verify the latest scan endpoint also works now
	w = doRequest(env, "GET", fmt.Sprintf("/api/v1/repos/%d/scans/latest", repoID), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("latest scan status = %d, want %d, body: %s", w.Code, http.StatusOK, w.Body.String())
	}
	resp = parseResponse(t, w)
	scanData := resp["data"].(map[string]interface{})
	score := int(scanData["score"].(float64))
	if score < 60 || score > 80 {
		t.Errorf("score = %d, want between 60 and 80", score)
	}
}

// =====================
// PR handler tests
// =====================

func TestListPRsByRepo_InvalidID(t *testing.T) {
	env := setupTestEnv(t)

	w := doRequest(env, "GET", "/api/v1/repos/abc/prs", nil)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d, body: %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
}

func TestGetPR_InvalidID(t *testing.T) {
	env := setupTestEnv(t)

	w := doRequest(env, "GET", "/api/v1/prs/abc", nil)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d, body: %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
}

func TestListPRsByRepo_WithStatusFilter(t *testing.T) {
	env := setupTestEnv(t)
	repoID := createTestRepo(t, env.client)
	ctx := context.Background()

	// Create PRs with different statuses
	statuses := []string{"open", "merged", "closed"}
	for i, status := range statuses {
		builder := env.client.PrRecord.Create().
			SetScmPrID(100 + i).
			SetTitle(fmt.Sprintf("PR %s #%d", status, i)).
			SetAuthor("dev").
			SetSourceBranch(fmt.Sprintf("branch-%d", i)).
			SetTargetBranch("main").
			SetRepoConfigID(repoID)
		if status == "merged" {
			builder.SetStatus("merged")
		} else if status == "closed" {
			builder.SetStatus("closed")
		}
		// "open" is the default
		_, err := builder.Save(ctx)
		if err != nil {
			t.Fatalf("create PR %d: %v", i, err)
		}
	}

	// Filter by status=open, months=0 to get all time
	w := doRequest(env, "GET", fmt.Sprintf("/api/v1/repos/%d/prs?status=open&months=0", repoID), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", w.Code, http.StatusOK, w.Body.String())
	}
	resp := parseResponse(t, w)
	data := resp["data"].(map[string]interface{})
	total := int(data["total"].(float64))
	if total != 1 {
		t.Errorf("total open PRs = %d, want 1", total)
	}

	// Filter by status=merged
	w = doRequest(env, "GET", fmt.Sprintf("/api/v1/repos/%d/prs?status=merged&months=0", repoID), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", w.Code, http.StatusOK, w.Body.String())
	}
	resp = parseResponse(t, w)
	data = resp["data"].(map[string]interface{})
	total = int(data["total"].(float64))
	if total != 1 {
		t.Errorf("total merged PRs = %d, want 1", total)
	}

	// No filter, months=0 -> all 3
	w = doRequest(env, "GET", fmt.Sprintf("/api/v1/repos/%d/prs?months=0", repoID), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", w.Code, http.StatusOK, w.Body.String())
	}
	resp = parseResponse(t, w)
	data = resp["data"].(map[string]interface{})
	total = int(data["total"].(float64))
	if total != 3 {
		t.Errorf("total all PRs = %d, want 3", total)
	}
}

func TestSyncPRs_InvalidID(t *testing.T) {
	env := setupTestEnv(t)

	w := doRequest(env, "POST", "/api/v1/repos/abc/sync-prs", nil)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d, body: %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
}
