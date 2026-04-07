package handler

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/ent/enttest"
	"github.com/ai-efficiency/backend/internal/auth"
	"github.com/ai-efficiency/backend/internal/middleware"
	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/ai-efficiency/backend/internal/repo"
	"github.com/ai-efficiency/backend/internal/sessionbootstrap"
	"github.com/ai-efficiency/backend/internal/webhook"
	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
	"go.uber.org/zap"
)

type fakeRelayProviderForBootstrap struct {
	listUserAPIKeysFn func(ctx context.Context, userID int64) ([]relay.APIKey, error)
	createUserAPIKeyFn func(ctx context.Context, userID int64, req relay.APIKeyCreateRequest) (*relay.APIKeyWithSecret, error)
}

var _ relay.Provider = (*fakeRelayProviderForBootstrap)(nil)

func (f *fakeRelayProviderForBootstrap) Ping(ctx context.Context) error { return nil }
func (f *fakeRelayProviderForBootstrap) Name() string                   { return "sub2api" }
func (f *fakeRelayProviderForBootstrap) Authenticate(ctx context.Context, username, password string) (*relay.User, error) {
	return nil, nil
}
func (f *fakeRelayProviderForBootstrap) GetUser(ctx context.Context, userID int64) (*relay.User, error) {
	return nil, nil
}
func (f *fakeRelayProviderForBootstrap) FindUserByEmail(ctx context.Context, email string) (*relay.User, error) {
	return nil, nil
}
func (f *fakeRelayProviderForBootstrap) FindUserByUsername(ctx context.Context, username string) (*relay.User, error) {
	return nil, nil
}
func (f *fakeRelayProviderForBootstrap) CreateUser(ctx context.Context, req relay.CreateUserRequest) (*relay.User, error) {
	return nil, nil
}
func (f *fakeRelayProviderForBootstrap) ChatCompletion(ctx context.Context, req relay.ChatCompletionRequest) (*relay.ChatCompletionResponse, error) {
	return nil, nil
}
func (f *fakeRelayProviderForBootstrap) ChatCompletionWithTools(ctx context.Context, req relay.ChatCompletionRequest, tools []relay.ToolDef) (*relay.ChatCompletionWithToolsResponse, error) {
	return nil, nil
}
func (f *fakeRelayProviderForBootstrap) GetUsageStats(ctx context.Context, userID int64, from, to time.Time) (*relay.UsageStats, error) {
	return nil, nil
}
func (f *fakeRelayProviderForBootstrap) ListUserAPIKeys(ctx context.Context, userID int64) ([]relay.APIKey, error) {
	if f.listUserAPIKeysFn != nil {
		return f.listUserAPIKeysFn(ctx, userID)
	}
	return nil, nil
}
func (f *fakeRelayProviderForBootstrap) CreateUserAPIKey(ctx context.Context, userID int64, req relay.APIKeyCreateRequest) (*relay.APIKeyWithSecret, error) {
	if f.createUserAPIKeyFn != nil {
		return f.createUserAPIKeyFn(ctx, userID, req)
	}
	return &relay.APIKeyWithSecret{
		APIKey: relay.APIKey{ID: 555, UserID: userID, Name: req.Name, Status: "active"},
		Secret: "sk-session-555",
	}, nil
}
func (f *fakeRelayProviderForBootstrap) RevokeUserAPIKey(ctx context.Context, keyID int64) error {
	return nil
}
func (f *fakeRelayProviderForBootstrap) ListUsageLogsByAPIKeyExact(ctx context.Context, apiKeyID int64, from, to time.Time) ([]relay.UsageLog, error) {
	return nil, nil
}

func setupBootstrapHTTPTestEnv(t *testing.T) *testEnv {
	t.Helper()

	client := enttest.Open(t, "sqlite3", "file:ent?mode=memory&_fk=1")
	logger := zap.NewNop()

	authSvc := auth.NewService(client, "test-jwt-secret-32-bytes-long!!!", 7200, 604800, logger)
	repoSvc := repo.NewService(client, "0000000000000000000000000000000000000000000000000000000000000000", logger)
	webhookHandler := webhook.NewHandler(client, nil, logger)

	rp := &fakeRelayProviderForBootstrap{}
	bootstrapSvc := sessionbootstrap.NewService(client, rp, nil, "sub2api", "http://relay.local/v1", "g-default", 2*time.Hour)

	router := SetupRouter(
		client,
		authSvc,
		repoSvc,
		nil, // analysisService
		webhookHandler,
		nil, // syncService
		nil, // settingsHandler
		nil, // chatHandler
		nil, // aggregator
		nil, // optimizer
		"0000000000000000000000000000000000000000000000000000000000000000",
		middleware.CORS(nil),
		nil, nil, nil,
		bootstrapSvc,
		nil,
	)

	u, err := client.User.Create().
		SetUsername("admin").
		SetEmail("admin@test.com").
		SetAuthSource("sub2api_sso").
		SetRole("admin").
		SetRelayUserID(99).
		Save(context.Background())
	if err != nil {
		t.Fatalf("create test user: %v", err)
	}

	pair, err := authSvc.GenerateTokenPairForUser(&auth.UserInfo{
		ID:       u.ID,
		Username: "admin",
		Role:     "admin",
	})
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}

	return &testEnv{
		client:  client,
		router:  router,
		authSvc: authSvc,
		token:   pair.AccessToken,
	}
}

func TestSessionBootstrapHTTP_Success(t *testing.T) {
	env := setupBootstrapHTTPTestEnv(t)
	ctx := context.Background()

	sp, err := env.client.ScmProvider.Create().
		SetName("mock-gh").
		SetType("github").
		SetBaseURL("https://api.github.com").
		SetCredentials("enc").
		Save(ctx)
	if err != nil {
		t.Fatalf("create scm provider: %v", err)
	}

	_, err = env.client.RepoConfig.Create().
		SetScmProviderID(sp.ID).
		SetName("mock-repo").
		SetFullName("org/mock-repo").
		SetCloneURL("https://github.com/org/mock-repo.git").
		SetDefaultBranch("main").
		SetRelayGroupID("g-repo").
		Save(ctx)
	if err != nil {
		t.Fatalf("create repo config: %v", err)
	}

	w := doRequest(env, "POST", "/api/v1/sessions/bootstrap", map[string]interface{}{
		"repo_full_name":  "org/mock-repo",
		"branch_snapshot": "main",
		"head_sha":        "abc123",
		"workspace_root":  "/tmp/ws",
		"git_dir":         "/tmp/ws/.git",
		"git_common_dir":  "/tmp/ws/.git",
		"workspace_id":    "ws-1",
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body: %s", w.Code, http.StatusCreated, w.Body.String())
	}

	resp := parseResponse(t, w)
	data, ok := resp["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected data object, got: %v", resp["data"])
	}
	if data["session_id"] == "" || data["session_id"] == nil {
		t.Fatalf("expected session_id, got: %v", data["session_id"])
	}
	envBundle, ok := data["env_bundle"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected env_bundle, got: %T", data["env_bundle"])
	}
	for _, forbidden := range []string{"OPENAI_API_KEY", "ANTHROPIC_API_KEY", "OPENAI_BASE_URL", "ANTHROPIC_BASE_URL"} {
		if _, ok := envBundle[forbidden]; ok {
			t.Fatalf("unexpected %s in env_bundle: %v", forbidden, envBundle[forbidden])
		}
	}
}

func TestSessionBootstrapHTTP_Failure_ServiceErrorStill422(t *testing.T) {
	env := setupBootstrapHTTPTestEnv(t)

	// Repo does not exist -> bootstrap service returns an error, handler maps to 422.
	w := doRequest(env, "POST", "/api/v1/sessions/bootstrap", map[string]interface{}{
		"repo_full_name":  "org/missing",
		"branch_snapshot": "main",
		"head_sha":        "abc123",
		"workspace_root":  "/tmp/ws",
		"git_dir":         "/tmp/ws/.git",
		"git_common_dir":  "/tmp/ws/.git",
		"workspace_id":    "ws-1",
	})
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d, body: %s", w.Code, http.StatusUnprocessableEntity, w.Body.String())
	}
}

func TestSessionProviderCredentialHTTP_ReusesExistingOpenAIKey(t *testing.T) {
	env := setupBootstrapHTTPTestEnv(t)
	ctx := context.Background()

	sp := env.client.ScmProvider.Create().
		SetName("mock-gh").
		SetType("github").
		SetBaseURL("https://api.github.com").
		SetCredentials("enc").
		SaveX(ctx)
	rc := env.client.RepoConfig.Create().
		SetScmProviderID(sp.ID).
		SetName("mock-repo").
		SetFullName("org/mock-repo").
		SetCloneURL("https://github.com/org/mock-repo.git").
		SetDefaultBranch("main").
		SetRelayGroupID("42").
		SaveX(ctx)

	u := env.client.User.Query().OnlyX(ctx)
	sid := uuid.New()
	env.client.Session.Create().
		SetID(sid).
		SetRepoConfigID(rc.ID).
		SetUserID(u.ID).
		SetBranch("main").
		SetProviderName("sub2api").
		SetStartedAt(time.Now()).
		SaveX(ctx)

	rp := &fakeRelayProviderForBootstrap{
		listUserAPIKeysFn: func(_ context.Context, userID int64) ([]relay.APIKey, error) {
			return []relay.APIKey{
				{
					ID:        900,
					UserID:    userID,
					Key:       "sk-existing-openai",
					Name:      "admin",
					Status:    "active",
					CreatedAt: time.Now(),
					Group:     &relay.Group{ID: 42, Platform: "openai"},
				},
			}, nil
		},
	}
	bootstrapSvc := sessionbootstrap.NewService(env.client, rp, nil, "sub2api", "http://relay.local/v1", "42", 2*time.Hour)
	env.router = SetupRouter(
		env.client,
		env.authSvc,
		repo.NewService(env.client, "0000000000000000000000000000000000000000000000000000000000000000", zap.NewNop()),
		nil,
		webhook.NewHandler(env.client, nil, zap.NewNop()),
		nil,
		nil,
		nil,
		nil,
		nil,
		"0000000000000000000000000000000000000000000000000000000000000000",
		middleware.CORS(nil),
		nil,
		nil,
		nil,
		bootstrapSvc,
		nil,
	)

	w := doRequest(env, "GET", "/api/v1/sessions/"+sid.String()+"/provider-credentials?platform=openai", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	resp := parseResponse(t, w)
	data := resp["data"].(map[string]interface{})
	if data["api_key_id"] != float64(900) {
		t.Fatalf("api_key_id = %v, want 900", data["api_key_id"])
	}
	if data["api_key"] != "sk-existing-openai" {
		t.Fatalf("api_key = %v, want sk-existing-openai", data["api_key"])
	}
}
