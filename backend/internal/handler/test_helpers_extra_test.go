package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/internal/auth"
	"github.com/ai-efficiency/backend/internal/middleware"
	"github.com/ai-efficiency/backend/internal/scm"
	"github.com/ai-efficiency/backend/internal/testdb"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

type mockTestEnv struct {
	client  *ent.Client
	router  *gin.Engine
	authSvc *auth.Service
	token   string
	userID  int
}

type mockRepoSCMProvider struct {
	getSCMProviderFn func(ctx context.Context, repoConfigID int) (scm.SCMProvider, *ent.RepoConfig, error)
}

func (m *mockRepoSCMProvider) GetSCMProvider(ctx context.Context, repoConfigID int) (scm.SCMProvider, *ent.RepoConfig, error) {
	if m != nil && m.getSCMProviderFn != nil {
		return m.getSCMProviderFn(ctx, repoConfigID)
	}
	return nil, nil, nil
}

type mockSCMProvider struct{}

func (m *mockSCMProvider) GetRepo(context.Context, string) (*scm.Repo, error) { return nil, nil }
func (m *mockSCMProvider) ListRepos(context.Context, scm.ListOpts) ([]*scm.Repo, error) {
	return nil, nil
}
func (m *mockSCMProvider) CreatePR(context.Context, scm.CreatePRRequest) (*scm.PR, error) {
	return nil, nil
}
func (m *mockSCMProvider) GetPR(context.Context, string, int) (*scm.PR, error) { return nil, nil }
func (m *mockSCMProvider) ListPRs(context.Context, string, scm.PRListOpts) ([]*scm.PR, error) {
	return nil, nil
}
func (m *mockSCMProvider) GetPRChangedFiles(context.Context, string, int) ([]string, error) {
	return nil, nil
}
func (m *mockSCMProvider) ListPRCommits(context.Context, string, int) ([]string, error) {
	return nil, nil
}
func (m *mockSCMProvider) GetPRApprovals(context.Context, string, int) (int, error)  { return 0, nil }
func (m *mockSCMProvider) AddLabels(context.Context, string, int, []string) error    { return nil }
func (m *mockSCMProvider) SetPRStatus(context.Context, scm.SetStatusRequest) error   { return nil }
func (m *mockSCMProvider) MergePR(context.Context, string, int, scm.MergeOpts) error { return nil }
func (m *mockSCMProvider) RegisterWebhook(context.Context, string, []string, string) (string, error) {
	return "", nil
}
func (m *mockSCMProvider) DeleteWebhook(context.Context, string, string) error { return nil }
func (m *mockSCMProvider) ParseWebhookPayload(*http.Request, string) (*scm.WebhookEvent, error) {
	return nil, nil
}
func (m *mockSCMProvider) GetFileContent(context.Context, string, string, string) ([]byte, error) {
	return nil, nil
}
func (m *mockSCMProvider) GetTree(context.Context, string, string) ([]*scm.TreeEntry, error) {
	return nil, nil
}
func (m *mockSCMProvider) GetBranchSHA(context.Context, string, string) (string, error) {
	return "", nil
}
func (m *mockSCMProvider) CreateBranch(context.Context, string, string, string) error { return nil }
func (m *mockSCMProvider) CommitFiles(context.Context, scm.CommitFilesRequest) (string, error) {
	return "", nil
}

func setupMockTestEnv(t *testing.T, _ interface{}, _ interface{}, repoSCM repoSCMProvider, _ interface{}) *mockTestEnv {
	t.Helper()

	client := testdb.Open(t)
	logger := zap.NewNop()
	authSvc := auth.NewService(client, "test-jwt-secret-32-bytes-long!!!", 7200, 604800, logger)
	authSvc.SetRefreshSessionStore(newHandlerTestRefreshSessionStore(t))

	prHandler := NewPRHandler(client, repoSCM, nil, nil)

	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(middleware.CORS(nil))

	api := router.Group("/api/v1")
	api.Use(auth.RequireAuth(authSvc))
	api.GET("/prs/:id", prHandler.Get)

	u, err := client.User.Create().
		SetUsername("mockadmin").
		SetEmail("mockadmin@test.com").
		SetAuthSource("sub2api_sso").
		SetRole("admin").
		Save(context.Background())
	if err != nil {
		t.Fatalf("create test user: %v", err)
	}

	pair, err := authSvc.GenerateTokenPairForUser(&auth.UserInfo{
		ID:       u.ID,
		Username: "mockadmin",
		Role:     "admin",
	})
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}

	return &mockTestEnv{
		client:  client,
		router:  router,
		authSvc: authSvc,
		token:   pair.AccessToken,
		userID:  u.ID,
	}
}

func doMockRequest(env *mockTestEnv, method, path string, body interface{}) *httptest.ResponseRecorder {
	var reqBody *bytes.Buffer
	if body != nil {
		b, _ := json.Marshal(body)
		reqBody = bytes.NewBuffer(b)
	} else {
		reqBody = bytes.NewBuffer(nil)
	}

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(method, path, reqBody)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+env.token)
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
	rc, err := client.RepoConfig.Create().
		SetRepoKey("github.com/org/mock-repo").
		SetName("mock-repo").
		SetFullName("org/mock-repo").
		SetCloneURL("https://github.com/org/mock-repo.git").
		SetDefaultBranch("main").
		Save(context.Background())
	if err != nil {
		t.Fatalf("create mock repo: %v", err)
	}
	return rc
}

func createTestRepo(t *testing.T, client *ent.Client) int {
	t.Helper()
	return createMockTestRepo(t, client).ID
}
