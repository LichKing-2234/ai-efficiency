package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/internal/analysis"
	"github.com/ai-efficiency/backend/internal/auth"
	"github.com/ai-efficiency/backend/internal/prsync"
	"github.com/ai-efficiency/backend/internal/scm"
	"github.com/ai-efficiency/backend/internal/testdb"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// --- Mock implementations ---

type mockAnalysisScanner struct {
	runScanFn       func(ctx context.Context, id int) (*ent.AiScanResult, error)
	listScansFn     func(ctx context.Context, id int, limit int) ([]*ent.AiScanResult, error)
	getLatestScanFn func(ctx context.Context, id int) (*ent.AiScanResult, error)
}

func (m *mockAnalysisScanner) RunScan(ctx context.Context, id int) (*ent.AiScanResult, error) {
	return m.runScanFn(ctx, id)
}
func (m *mockAnalysisScanner) ListScans(ctx context.Context, id int, limit int) ([]*ent.AiScanResult, error) {
	return m.listScansFn(ctx, id, limit)
}
func (m *mockAnalysisScanner) GetLatestScan(ctx context.Context, id int) (*ent.AiScanResult, error) {
	return m.getLatestScanFn(ctx, id)
}

type mockOptimizer struct {
	createPRFn func(ctx context.Context, provider scm.SCMProvider, rc *ent.RepoConfig, scan *ent.AiScanResult) (*analysis.OptimizeResult, error)
	previewFn  func(ctx context.Context, provider scm.SCMProvider, rc *ent.RepoConfig, scan *ent.AiScanResult) (*analysis.OptimizePreview, error)
	confirmFn  func(ctx context.Context, provider scm.SCMProvider, rc *ent.RepoConfig, files map[string]string, score int) (*analysis.OptimizeResult, error)
}

func (m *mockOptimizer) CreateOptimizationPR(ctx context.Context, provider scm.SCMProvider, rc *ent.RepoConfig, scan *ent.AiScanResult) (*analysis.OptimizeResult, error) {
	return m.createPRFn(ctx, provider, rc, scan)
}
func (m *mockOptimizer) PreviewOptimization(ctx context.Context, provider scm.SCMProvider, rc *ent.RepoConfig, scan *ent.AiScanResult) (*analysis.OptimizePreview, error) {
	return m.previewFn(ctx, provider, rc, scan)
}
func (m *mockOptimizer) ConfirmOptimization(ctx context.Context, provider scm.SCMProvider, rc *ent.RepoConfig, files map[string]string, score int) (*analysis.OptimizeResult, error) {
	return m.confirmFn(ctx, provider, rc, files, score)
}

type mockRepoSCMProvider struct {
	getSCMProviderFn func(ctx context.Context, id int) (scm.SCMProvider, *ent.RepoConfig, error)
}

func (m *mockRepoSCMProvider) GetSCMProvider(ctx context.Context, id int) (scm.SCMProvider, *ent.RepoConfig, error) {
	return m.getSCMProviderFn(ctx, id)
}

type mockPRSyncer struct {
	syncFn func(ctx context.Context, provider scm.SCMProvider, rc *ent.RepoConfig) (*prsync.SyncResult, error)
}

func (m *mockPRSyncer) Sync(ctx context.Context, provider scm.SCMProvider, rc *ent.RepoConfig) (*prsync.SyncResult, error) {
	return m.syncFn(ctx, provider, rc)
}

type mockSCMProvider struct{}

func (m *mockSCMProvider) GetRepo(ctx context.Context, fullName string) (*scm.Repo, error) {
	return nil, nil
}
func (m *mockSCMProvider) ListRepos(ctx context.Context, opts scm.ListOpts) ([]*scm.Repo, error) {
	return nil, nil
}
func (m *mockSCMProvider) CreatePR(ctx context.Context, req scm.CreatePRRequest) (*scm.PR, error) {
	return nil, nil
}
func (m *mockSCMProvider) GetPR(ctx context.Context, repoFullName string, prID int) (*scm.PR, error) {
	return nil, nil
}
func (m *mockSCMProvider) ListPRs(ctx context.Context, repoFullName string, opts scm.PRListOpts) ([]*scm.PR, error) {
	return nil, nil
}
func (m *mockSCMProvider) GetPRChangedFiles(ctx context.Context, repoFullName string, prID int) ([]string, error) {
	return nil, nil
}
func (m *mockSCMProvider) ListPRCommits(ctx context.Context, repoFullName string, prID int) ([]string, error) {
	return nil, nil
}
func (m *mockSCMProvider) GetPRApprovals(ctx context.Context, repoFullName string, prID int) (int, error) {
	return 0, nil
}
func (m *mockSCMProvider) AddLabels(ctx context.Context, repoFullName string, prID int, labels []string) error {
	return nil
}
func (m *mockSCMProvider) SetPRStatus(ctx context.Context, req scm.SetStatusRequest) error {
	return nil
}
func (m *mockSCMProvider) MergePR(ctx context.Context, repoFullName string, prID int, opts scm.MergeOpts) error {
	return nil
}
func (m *mockSCMProvider) RegisterWebhook(ctx context.Context, repoFullName string, events []string, secret string) (string, error) {
	return "", nil
}
func (m *mockSCMProvider) DeleteWebhook(ctx context.Context, repoFullName string, webhookID string) error {
	return nil
}
func (m *mockSCMProvider) ParseWebhookPayload(r *http.Request, secret string) (*scm.WebhookEvent, error) {
	return nil, nil
}
func (m *mockSCMProvider) GetFileContent(ctx context.Context, repoFullName, path, ref string) ([]byte, error) {
	return nil, nil
}
func (m *mockSCMProvider) GetTree(ctx context.Context, repoFullName, ref string) ([]*scm.TreeEntry, error) {
	return nil, nil
}
func (m *mockSCMProvider) GetBranchSHA(ctx context.Context, repoFullName, branch string) (string, error) {
	return "abc123", nil
}
func (m *mockSCMProvider) CreateBranch(ctx context.Context, repoFullName, branchName, baseSHA string) error {
	return nil
}
func (m *mockSCMProvider) CommitFiles(ctx context.Context, req scm.CommitFilesRequest) (string, error) {
	return "abc123", nil
}

// --- Mock test environment ---

type mockTestEnv struct {
	client  *ent.Client
	router  *gin.Engine
	authSvc *auth.Service
	token   string
}

func setupMockTestEnv(t *testing.T, scanner analysisScanner, opt optimizerService, repoSCM repoSCMProvider, syncer prSyncer) *mockTestEnv {
	t.Helper()
	client := testdb.Open(t)
	logger := zap.NewNop()
	authSvc := auth.NewService(client, "test-jwt-secret-32-bytes-long!!!", 7200, 604800, logger)

	// Build router directly to avoid nil *repo.Service in NewRepoHandler
	r := gin.New()
	r.Use(gin.Recovery())

	analysisHandler := NewAnalysisHandler(scanner, opt, repoSCM)
	prHandler := NewPRHandler(client, repoSCM, syncer)

	api := r.Group("/api/v1")
	api.Use(auth.RequireAuth(authSvc))
	{
		api.POST("/repos/:id/scan", analysisHandler.TriggerScan)
		api.GET("/repos/:id/scans", analysisHandler.ListScans)
		api.GET("/repos/:id/scans/latest", analysisHandler.LatestScan)
		api.POST("/repos/:id/optimize", analysisHandler.Optimize)
		api.POST("/repos/:id/optimize/preview", analysisHandler.OptimizePreview)
		api.POST("/repos/:id/optimize/confirm", analysisHandler.OptimizeConfirm)
		api.POST("/repos/:id/sync-prs", prHandler.SyncPRs)
		api.GET("/repos/:id/prs", prHandler.ListByRepo)
		api.GET("/prs/:id", prHandler.Get)
	}

	u, err := client.User.Create().
		SetUsername("mockadmin").SetEmail("mockadmin@test.com").
		SetAuthSource("sub2api_sso").SetRole("admin").
		Save(context.Background())
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	pair, err := authSvc.GenerateTokenPairForUser(&auth.UserInfo{ID: u.ID, Username: "mockadmin", Role: "admin"})
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}
	return &mockTestEnv{client: client, router: r, authSvc: authSvc, token: pair.AccessToken}
}

func doMockRequest(env *mockTestEnv, method, path string, body interface{}) *httptest.ResponseRecorder {
	var buf []byte
	if body != nil {
		buf, _ = json.Marshal(body)
	}
	w := httptest.NewRecorder()
	var req *http.Request
	if buf != nil {
		req = httptest.NewRequest(method, path, bytes.NewReader(buf))
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	req.Header.Set("Content-Type", "application/json")
	if env.token != "" {
		req.Header.Set("Authorization", "Bearer "+env.token)
	}
	env.router.ServeHTTP(w, req)
	return w
}

func parseMockResponse(t *testing.T, w *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse response: %v, body: %s", err, w.Body.String())
	}
	return resp
}

func createMockTestRepo(t *testing.T, client *ent.Client) *ent.RepoConfig {
	t.Helper()
	provider, err := client.ScmProvider.Create().
		SetName("mock-gh").SetType("github").
		SetBaseURL("https://api.github.com").SetCredentials("enc").
		Save(context.Background())
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}
	rc, err := client.RepoConfig.Create().
		SetScmProviderID(provider.ID).SetName("mock-repo").
		SetFullName("org/mock-repo").
		SetCloneURL("https://github.com/org/mock-repo.git").
		SetDefaultBranch("main").
		Save(context.Background())
	if err != nil {
		t.Fatalf("create repo: %v", err)
	}
	return rc
}

// =====================
// Optimize handler tests (mocked)
// =====================

func TestOptimize_Success(t *testing.T) {
	scanner := &mockAnalysisScanner{
		getLatestScanFn: func(_ context.Context, _ int) (*ent.AiScanResult, error) {
			return &ent.AiScanResult{ID: 1, Score: 60}, nil
		},
	}
	opt := &mockOptimizer{
		createPRFn: func(_ context.Context, _ scm.SCMProvider, _ *ent.RepoConfig, _ *ent.AiScanResult) (*analysis.OptimizeResult, error) {
			return &analysis.OptimizeResult{BranchName: "ai-optimize-1", PRURL: "https://github.com/org/repo/pull/1", PRID: 1, FilesAdded: 3}, nil
		},
	}
	repoSCM := &mockRepoSCMProvider{
		getSCMProviderFn: func(_ context.Context, _ int) (scm.SCMProvider, *ent.RepoConfig, error) {
			return &mockSCMProvider{}, &ent.RepoConfig{ID: 1}, nil
		},
	}
	env := setupMockTestEnv(t, scanner, opt, repoSCM, nil)
	rc := createMockTestRepo(t, env.client)

	w := doMockRequest(env, "POST", fmt.Sprintf("/api/v1/repos/%d/optimize", rc.ID), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", w.Code, http.StatusOK, w.Body.String())
	}
	resp := parseMockResponse(t, w)
	data := resp["data"].(map[string]interface{})
	if data["branch_name"] != "ai-optimize-1" {
		t.Errorf("branch_name = %v, want ai-optimize-1", data["branch_name"])
	}
}

func TestOptimize_NilResult(t *testing.T) {
	scanner := &mockAnalysisScanner{
		getLatestScanFn: func(_ context.Context, _ int) (*ent.AiScanResult, error) {
			return &ent.AiScanResult{ID: 1, Score: 100}, nil
		},
	}
	opt := &mockOptimizer{
		createPRFn: func(_ context.Context, _ scm.SCMProvider, _ *ent.RepoConfig, _ *ent.AiScanResult) (*analysis.OptimizeResult, error) {
			return nil, nil
		},
	}
	repoSCM := &mockRepoSCMProvider{
		getSCMProviderFn: func(_ context.Context, _ int) (scm.SCMProvider, *ent.RepoConfig, error) {
			return &mockSCMProvider{}, &ent.RepoConfig{ID: 1}, nil
		},
	}
	env := setupMockTestEnv(t, scanner, opt, repoSCM, nil)
	rc := createMockTestRepo(t, env.client)

	w := doMockRequest(env, "POST", fmt.Sprintf("/api/v1/repos/%d/optimize", rc.ID), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", w.Code, http.StatusOK, w.Body.String())
	}
	resp := parseMockResponse(t, w)
	data := resp["data"].(map[string]interface{})
	if data["message"] != "no auto-fixable issues found" {
		t.Errorf("message = %v", data["message"])
	}
}

func TestOptimize_OptimizerError(t *testing.T) {
	scanner := &mockAnalysisScanner{
		getLatestScanFn: func(_ context.Context, _ int) (*ent.AiScanResult, error) {
			return &ent.AiScanResult{ID: 1, Score: 60}, nil
		},
	}
	opt := &mockOptimizer{
		createPRFn: func(_ context.Context, _ scm.SCMProvider, _ *ent.RepoConfig, _ *ent.AiScanResult) (*analysis.OptimizeResult, error) {
			return nil, fmt.Errorf("LLM timeout")
		},
	}
	repoSCM := &mockRepoSCMProvider{
		getSCMProviderFn: func(_ context.Context, _ int) (scm.SCMProvider, *ent.RepoConfig, error) {
			return &mockSCMProvider{}, &ent.RepoConfig{ID: 1}, nil
		},
	}
	env := setupMockTestEnv(t, scanner, opt, repoSCM, nil)
	rc := createMockTestRepo(t, env.client)

	w := doMockRequest(env, "POST", fmt.Sprintf("/api/v1/repos/%d/optimize", rc.ID), nil)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
}

func TestOptimize_GetSCMProviderError(t *testing.T) {
	scanner := &mockAnalysisScanner{
		getLatestScanFn: func(_ context.Context, _ int) (*ent.AiScanResult, error) {
			return &ent.AiScanResult{ID: 1, Score: 60}, nil
		},
	}
	opt := &mockOptimizer{}
	repoSCM := &mockRepoSCMProvider{
		getSCMProviderFn: func(_ context.Context, _ int) (scm.SCMProvider, *ent.RepoConfig, error) {
			return nil, nil, fmt.Errorf("provider not found")
		},
	}
	env := setupMockTestEnv(t, scanner, opt, repoSCM, nil)
	rc := createMockTestRepo(t, env.client)

	w := doMockRequest(env, "POST", fmt.Sprintf("/api/v1/repos/%d/optimize", rc.ID), nil)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
}

func TestOptimize_NoScanResult(t *testing.T) {
	scanner := &mockAnalysisScanner{
		getLatestScanFn: func(_ context.Context, _ int) (*ent.AiScanResult, error) {
			return nil, fmt.Errorf("not found")
		},
	}
	opt := &mockOptimizer{}
	repoSCM := &mockRepoSCMProvider{}
	env := setupMockTestEnv(t, scanner, opt, repoSCM, nil)
	rc := createMockTestRepo(t, env.client)

	w := doMockRequest(env, "POST", fmt.Sprintf("/api/v1/repos/%d/optimize", rc.ID), nil)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

// =====================
// OptimizePreview handler tests (mocked)
// =====================

func TestOptimizePreview_Success(t *testing.T) {
	scanner := &mockAnalysisScanner{
		getLatestScanFn: func(_ context.Context, _ int) (*ent.AiScanResult, error) {
			return &ent.AiScanResult{ID: 1, Score: 60}, nil
		},
	}
	opt := &mockOptimizer{
		previewFn: func(_ context.Context, _ scm.SCMProvider, _ *ent.RepoConfig, _ *ent.AiScanResult) (*analysis.OptimizePreview, error) {
			return &analysis.OptimizePreview{
				Files: []analysis.OptimizePreviewFile{{Path: ".editorconfig", OldContent: "", NewContent: "root = true"}},
				Score: 75,
			}, nil
		},
	}
	repoSCM := &mockRepoSCMProvider{
		getSCMProviderFn: func(_ context.Context, _ int) (scm.SCMProvider, *ent.RepoConfig, error) {
			return &mockSCMProvider{}, &ent.RepoConfig{ID: 1}, nil
		},
	}
	env := setupMockTestEnv(t, scanner, opt, repoSCM, nil)
	rc := createMockTestRepo(t, env.client)

	w := doMockRequest(env, "POST", fmt.Sprintf("/api/v1/repos/%d/optimize/preview", rc.ID), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", w.Code, http.StatusOK, w.Body.String())
	}
	resp := parseMockResponse(t, w)
	data := resp["data"].(map[string]interface{})
	if int(data["score"].(float64)) != 75 {
		t.Errorf("score = %v, want 75", data["score"])
	}
}

func TestOptimizePreview_NilResult(t *testing.T) {
	scanner := &mockAnalysisScanner{
		getLatestScanFn: func(_ context.Context, _ int) (*ent.AiScanResult, error) {
			return &ent.AiScanResult{ID: 1, Score: 100}, nil
		},
	}
	opt := &mockOptimizer{
		previewFn: func(_ context.Context, _ scm.SCMProvider, _ *ent.RepoConfig, _ *ent.AiScanResult) (*analysis.OptimizePreview, error) {
			return nil, nil
		},
	}
	repoSCM := &mockRepoSCMProvider{
		getSCMProviderFn: func(_ context.Context, _ int) (scm.SCMProvider, *ent.RepoConfig, error) {
			return &mockSCMProvider{}, &ent.RepoConfig{ID: 1}, nil
		},
	}
	env := setupMockTestEnv(t, scanner, opt, repoSCM, nil)
	rc := createMockTestRepo(t, env.client)

	w := doMockRequest(env, "POST", fmt.Sprintf("/api/v1/repos/%d/optimize/preview", rc.ID), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", w.Code, http.StatusOK, w.Body.String())
	}
	resp := parseMockResponse(t, w)
	data := resp["data"].(map[string]interface{})
	if data["message"] != "no auto-fixable issues found" {
		t.Errorf("message = %v", data["message"])
	}
}

func TestOptimizePreview_Error(t *testing.T) {
	scanner := &mockAnalysisScanner{
		getLatestScanFn: func(_ context.Context, _ int) (*ent.AiScanResult, error) {
			return &ent.AiScanResult{ID: 1, Score: 60}, nil
		},
	}
	opt := &mockOptimizer{
		previewFn: func(_ context.Context, _ scm.SCMProvider, _ *ent.RepoConfig, _ *ent.AiScanResult) (*analysis.OptimizePreview, error) {
			return nil, fmt.Errorf("preview failed")
		},
	}
	repoSCM := &mockRepoSCMProvider{
		getSCMProviderFn: func(_ context.Context, _ int) (scm.SCMProvider, *ent.RepoConfig, error) {
			return &mockSCMProvider{}, &ent.RepoConfig{ID: 1}, nil
		},
	}
	env := setupMockTestEnv(t, scanner, opt, repoSCM, nil)
	rc := createMockTestRepo(t, env.client)

	w := doMockRequest(env, "POST", fmt.Sprintf("/api/v1/repos/%d/optimize/preview", rc.ID), nil)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
}

func TestOptimizePreview_GetSCMProviderError(t *testing.T) {
	scanner := &mockAnalysisScanner{
		getLatestScanFn: func(_ context.Context, _ int) (*ent.AiScanResult, error) {
			return &ent.AiScanResult{ID: 1, Score: 60}, nil
		},
	}
	opt := &mockOptimizer{}
	repoSCM := &mockRepoSCMProvider{
		getSCMProviderFn: func(_ context.Context, _ int) (scm.SCMProvider, *ent.RepoConfig, error) {
			return nil, nil, fmt.Errorf("provider not found")
		},
	}
	env := setupMockTestEnv(t, scanner, opt, repoSCM, nil)
	rc := createMockTestRepo(t, env.client)

	w := doMockRequest(env, "POST", fmt.Sprintf("/api/v1/repos/%d/optimize/preview", rc.ID), nil)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
}

func TestOptimizePreview_NoScanResult(t *testing.T) {
	scanner := &mockAnalysisScanner{
		getLatestScanFn: func(_ context.Context, _ int) (*ent.AiScanResult, error) {
			return nil, fmt.Errorf("not found")
		},
	}
	opt := &mockOptimizer{}
	env := setupMockTestEnv(t, scanner, opt, &mockRepoSCMProvider{}, nil)
	rc := createMockTestRepo(t, env.client)

	w := doMockRequest(env, "POST", fmt.Sprintf("/api/v1/repos/%d/optimize/preview", rc.ID), nil)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

// =====================
// OptimizeConfirm handler tests (mocked)
// =====================

func TestOptimizeConfirm_Success(t *testing.T) {
	opt := &mockOptimizer{
		confirmFn: func(_ context.Context, _ scm.SCMProvider, _ *ent.RepoConfig, _ map[string]string, _ int) (*analysis.OptimizeResult, error) {
			return &analysis.OptimizeResult{BranchName: "ai-optimize-1", PRURL: "https://github.com/org/repo/pull/2", PRID: 2, FilesAdded: 2}, nil
		},
	}
	repoSCM := &mockRepoSCMProvider{
		getSCMProviderFn: func(_ context.Context, _ int) (scm.SCMProvider, *ent.RepoConfig, error) {
			return &mockSCMProvider{}, &ent.RepoConfig{ID: 1}, nil
		},
	}
	env := setupMockTestEnv(t, &mockAnalysisScanner{}, opt, repoSCM, nil)
	rc := createMockTestRepo(t, env.client)

	body := map[string]interface{}{
		"files": map[string]string{".editorconfig": "root = true"},
		"score": 75,
	}
	w := doMockRequest(env, "POST", fmt.Sprintf("/api/v1/repos/%d/optimize/confirm", rc.ID), body)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", w.Code, http.StatusOK, w.Body.String())
	}
	resp := parseMockResponse(t, w)
	data := resp["data"].(map[string]interface{})
	if data["branch_name"] != "ai-optimize-1" {
		t.Errorf("branch_name = %v, want ai-optimize-1", data["branch_name"])
	}
}

func TestOptimizeConfirm_Error(t *testing.T) {
	opt := &mockOptimizer{
		confirmFn: func(_ context.Context, _ scm.SCMProvider, _ *ent.RepoConfig, _ map[string]string, _ int) (*analysis.OptimizeResult, error) {
			return nil, fmt.Errorf("commit failed")
		},
	}
	repoSCM := &mockRepoSCMProvider{
		getSCMProviderFn: func(_ context.Context, _ int) (scm.SCMProvider, *ent.RepoConfig, error) {
			return &mockSCMProvider{}, &ent.RepoConfig{ID: 1}, nil
		},
	}
	env := setupMockTestEnv(t, &mockAnalysisScanner{}, opt, repoSCM, nil)
	rc := createMockTestRepo(t, env.client)

	body := map[string]interface{}{
		"files": map[string]string{".editorconfig": "root = true"},
		"score": 75,
	}
	w := doMockRequest(env, "POST", fmt.Sprintf("/api/v1/repos/%d/optimize/confirm", rc.ID), body)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
}

func TestOptimizeConfirm_GetSCMProviderError(t *testing.T) {
	opt := &mockOptimizer{}
	repoSCM := &mockRepoSCMProvider{
		getSCMProviderFn: func(_ context.Context, _ int) (scm.SCMProvider, *ent.RepoConfig, error) {
			return nil, nil, fmt.Errorf("provider not found")
		},
	}
	env := setupMockTestEnv(t, &mockAnalysisScanner{}, opt, repoSCM, nil)
	rc := createMockTestRepo(t, env.client)

	body := map[string]interface{}{
		"files": map[string]string{".editorconfig": "root = true"},
		"score": 75,
	}
	w := doMockRequest(env, "POST", fmt.Sprintf("/api/v1/repos/%d/optimize/confirm", rc.ID), body)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
}

func TestOptimizeConfirm_InvalidBody(t *testing.T) {
	opt := &mockOptimizer{}
	repoSCM := &mockRepoSCMProvider{}
	env := setupMockTestEnv(t, &mockAnalysisScanner{}, opt, repoSCM, nil)
	rc := createMockTestRepo(t, env.client)

	// Send string instead of object — ShouldBindJSON will fail
	w := doMockRequest(env, "POST", fmt.Sprintf("/api/v1/repos/%d/optimize/confirm", rc.ID), "bad")
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

// =====================
// SyncPRs handler tests (mocked)
// =====================

func TestSyncPRs_Success(t *testing.T) {
	repoSCM := &mockRepoSCMProvider{
		getSCMProviderFn: func(_ context.Context, _ int) (scm.SCMProvider, *ent.RepoConfig, error) {
			return &mockSCMProvider{}, &ent.RepoConfig{ID: 1, FullName: "org/mock-repo"}, nil
		},
	}
	syncer := &mockPRSyncer{
		syncFn: func(_ context.Context, _ scm.SCMProvider, _ *ent.RepoConfig) (*prsync.SyncResult, error) {
			return &prsync.SyncResult{Created: 5, Updated: 2, Total: 7}, nil
		},
	}
	env := setupMockTestEnv(t, &mockAnalysisScanner{}, nil, repoSCM, syncer)
	rc := createMockTestRepo(t, env.client)

	w := doMockRequest(env, "POST", fmt.Sprintf("/api/v1/repos/%d/sync-prs", rc.ID), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", w.Code, http.StatusOK, w.Body.String())
	}
	resp := parseMockResponse(t, w)
	data := resp["data"].(map[string]interface{})
	if int(data["total"].(float64)) != 7 {
		t.Errorf("total = %v, want 7", data["total"])
	}
}

func TestSyncPRs_SyncError(t *testing.T) {
	repoSCM := &mockRepoSCMProvider{
		getSCMProviderFn: func(_ context.Context, _ int) (scm.SCMProvider, *ent.RepoConfig, error) {
			return &mockSCMProvider{}, &ent.RepoConfig{ID: 1}, nil
		},
	}
	syncer := &mockPRSyncer{
		syncFn: func(_ context.Context, _ scm.SCMProvider, _ *ent.RepoConfig) (*prsync.SyncResult, error) {
			return nil, fmt.Errorf("API rate limited")
		},
	}
	env := setupMockTestEnv(t, &mockAnalysisScanner{}, nil, repoSCM, syncer)
	rc := createMockTestRepo(t, env.client)

	w := doMockRequest(env, "POST", fmt.Sprintf("/api/v1/repos/%d/sync-prs", rc.ID), nil)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
}

func TestSyncPRs_GetSCMProviderError(t *testing.T) {
	repoSCM := &mockRepoSCMProvider{
		getSCMProviderFn: func(_ context.Context, _ int) (scm.SCMProvider, *ent.RepoConfig, error) {
			return nil, nil, fmt.Errorf("provider not configured")
		},
	}
	syncer := &mockPRSyncer{}
	env := setupMockTestEnv(t, &mockAnalysisScanner{}, nil, repoSCM, syncer)
	rc := createMockTestRepo(t, env.client)

	w := doMockRequest(env, "POST", fmt.Sprintf("/api/v1/repos/%d/sync-prs", rc.ID), nil)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
}

// =====================
// TriggerScan handler tests (mocked)
// =====================

func TestAnalysisTriggerScan_Success(t *testing.T) {
	scanner := &mockAnalysisScanner{
		runScanFn: func(_ context.Context, _ int) (*ent.AiScanResult, error) {
			return &ent.AiScanResult{ID: 1, Score: 85}, nil
		},
	}
	env := setupMockTestEnv(t, scanner, nil, &mockRepoSCMProvider{}, nil)
	rc := createMockTestRepo(t, env.client)

	w := doMockRequest(env, "POST", fmt.Sprintf("/api/v1/repos/%d/scan", rc.ID), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", w.Code, http.StatusOK, w.Body.String())
	}
	resp := parseMockResponse(t, w)
	data := resp["data"].(map[string]interface{})
	if int(data["score"].(float64)) != 85 {
		t.Errorf("score = %v, want 85", data["score"])
	}
}

func TestAnalysisTriggerScan_Error(t *testing.T) {
	scanner := &mockAnalysisScanner{
		runScanFn: func(_ context.Context, _ int) (*ent.AiScanResult, error) {
			return nil, fmt.Errorf("clone failed")
		},
	}
	env := setupMockTestEnv(t, scanner, nil, &mockRepoSCMProvider{}, nil)
	rc := createMockTestRepo(t, env.client)

	w := doMockRequest(env, "POST", fmt.Sprintf("/api/v1/repos/%d/scan", rc.ID), nil)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
}
