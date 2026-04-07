package sessionbootstrap

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/enttest"
	"github.com/ai-efficiency/backend/internal/auth"
	"github.com/ai-efficiency/backend/internal/pkg"
	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
)

type fakeRelayProvider struct {
	findUserByUsernameFn func(ctx context.Context, username string) (*relay.User, error)
	createUserFn         func(ctx context.Context, req relay.CreateUserRequest) (*relay.User, error)
	listUserAPIKeysFn    func(ctx context.Context, userID int64) ([]relay.APIKey, error)

	createUserAPIKeyFn      func(ctx context.Context, userID int64, req relay.APIKeyCreateRequest) (*relay.APIKeyWithSecret, error)
	revokeUserAPIKeyFn      func(ctx context.Context, keyID int64) error
	resolveDefaultGroupIDFn func(ctx context.Context) (string, error)
	resolveDefaultGroupIDForPlatformFn func(ctx context.Context, platform string) (string, error)

	lastCreateUserAPIKeyUserID int64
	lastCreateUserAPIKeyReq    relay.APIKeyCreateRequest
	revokedKeyIDs              []int64
}

var _ relay.Provider = (*fakeRelayProvider)(nil)

func (f *fakeRelayProvider) Ping(ctx context.Context) error { return nil }
func (f *fakeRelayProvider) Name() string                   { return "sub2api" }
func (f *fakeRelayProvider) Authenticate(ctx context.Context, username, password string) (*relay.User, error) {
	return nil, nil
}
func (f *fakeRelayProvider) GetUser(ctx context.Context, userID int64) (*relay.User, error) {
	return nil, nil
}
func (f *fakeRelayProvider) FindUserByEmail(ctx context.Context, email string) (*relay.User, error) {
	return nil, nil
}
func (f *fakeRelayProvider) FindUserByUsername(ctx context.Context, username string) (*relay.User, error) {
	if f.findUserByUsernameFn != nil {
		return f.findUserByUsernameFn(ctx, username)
	}
	return nil, nil
}
func (f *fakeRelayProvider) CreateUser(ctx context.Context, req relay.CreateUserRequest) (*relay.User, error) {
	if f.createUserFn != nil {
		return f.createUserFn(ctx, req)
	}
	return &relay.User{ID: 1, Username: req.Username, Email: req.Email}, nil
}
func (f *fakeRelayProvider) ChatCompletion(ctx context.Context, req relay.ChatCompletionRequest) (*relay.ChatCompletionResponse, error) {
	return nil, nil
}
func (f *fakeRelayProvider) ChatCompletionWithTools(ctx context.Context, req relay.ChatCompletionRequest, tools []relay.ToolDef) (*relay.ChatCompletionWithToolsResponse, error) {
	return nil, nil
}
func (f *fakeRelayProvider) GetUsageStats(ctx context.Context, userID int64, from, to time.Time) (*relay.UsageStats, error) {
	return nil, nil
}
func (f *fakeRelayProvider) ListUserAPIKeys(ctx context.Context, userID int64) ([]relay.APIKey, error) {
	if f.listUserAPIKeysFn != nil {
		return f.listUserAPIKeysFn(ctx, userID)
	}
	return nil, nil
}
func (f *fakeRelayProvider) CreateUserAPIKey(ctx context.Context, userID int64, req relay.APIKeyCreateRequest) (*relay.APIKeyWithSecret, error) {
	f.lastCreateUserAPIKeyUserID = userID
	f.lastCreateUserAPIKeyReq = req
	if f.createUserAPIKeyFn != nil {
		return f.createUserAPIKeyFn(ctx, userID, req)
	}
	return &relay.APIKeyWithSecret{
		APIKey: relay.APIKey{ID: 1234, UserID: userID, Name: req.Name, Status: "active"},
		Secret: "sk-test",
	}, nil
}
func (f *fakeRelayProvider) RevokeUserAPIKey(ctx context.Context, keyID int64) error {
	f.revokedKeyIDs = append(f.revokedKeyIDs, keyID)
	if f.revokeUserAPIKeyFn != nil {
		return f.revokeUserAPIKeyFn(ctx, keyID)
	}
	return nil
}
func (f *fakeRelayProvider) ListUsageLogsByAPIKeyExact(ctx context.Context, apiKeyID int64, from, to time.Time) ([]relay.UsageLog, error) {
	return nil, nil
}
func (f *fakeRelayProvider) ResolveDefaultGroupID(ctx context.Context) (string, error) {
	if f.resolveDefaultGroupIDFn != nil {
		return f.resolveDefaultGroupIDFn(ctx)
	}
	return "", nil
}
func (f *fakeRelayProvider) ResolveDefaultGroupIDForPlatform(ctx context.Context, platform string) (string, error) {
	if f.resolveDefaultGroupIDForPlatformFn != nil {
		return f.resolveDefaultGroupIDForPlatformFn(ctx, platform)
	}
	return "", nil
}

func ptrTime(v time.Time) *time.Time {
	return &v
}

func TestBootstrapCreatesSessionKeyAndEnvBundle(t *testing.T) {
	ctx := context.Background()
	client := enttest.Open(t, "sqlite3", "file:ent?mode=memory&_fk=1")

	sp, err := client.ScmProvider.Create().
		SetName("mock-gh").
		SetType("github").
		SetBaseURL("https://api.github.com").
		SetCredentials("enc").
		Save(ctx)
	if err != nil {
		t.Fatalf("create scm provider: %v", err)
	}

	rc, err := client.RepoConfig.Create().
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

	u, err := client.User.Create().
		SetUsername("alice").
		SetEmail("alice@example.com").
		SetAuthSource("ldap").
		Save(ctx)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	rp := &fakeRelayProvider{
		findUserByUsernameFn: func(_ context.Context, username string) (*relay.User, error) {
			return &relay.User{ID: 99, Username: username, Email: "alice@relay.local"}, nil
		},
		createUserAPIKeyFn: func(_ context.Context, userID int64, req relay.APIKeyCreateRequest) (*relay.APIKeyWithSecret, error) {
			return &relay.APIKeyWithSecret{
				APIKey: relay.APIKey{ID: 555, UserID: userID, Name: req.Name, Status: "active"},
				Secret: "sk-session-555",
			}, nil
		},
	}
	resolver := auth.NewRelayIdentityResolver(rp, "ldap.local")

	svc := NewService(client, rp, resolver, "sub2api", "http://relay.local/v1", "g-default", 2*time.Hour)

	resp, err := svc.Bootstrap(ctx, u.ID, BootstrapRequest{
		RepoFullName:   rc.FullName,
		BranchSnapshot: "main",
		HeadSHA:        "abc123",
		WorkspaceRoot:  "/tmp/ws",
		GitDir:         "/tmp/ws/.git",
		GitCommonDir:   "/tmp/ws/.git",
		WorkspaceID:    "ws-1",
	})
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	if resp.SessionID == uuid.Nil {
		t.Fatalf("session_id is empty")
	}
	if resp.RelayUserID != 99 {
		t.Fatalf("relay_user_id = %d, want %d", resp.RelayUserID, 99)
	}
	if resp.RelayAPIKeyID != 555 {
		t.Fatalf("relay_api_key_id = %d, want %d", resp.RelayAPIKeyID, 555)
	}
	if resp.ProviderName != "sub2api" {
		t.Fatalf("provider_name = %q, want %q", resp.ProviderName, "sub2api")
	}
	if resp.GroupID != "g-repo" {
		t.Fatalf("group_id = %q, want %q", resp.GroupID, "g-repo")
	}
	if resp.RuntimeRef == "" {
		t.Fatalf("runtime_ref is empty")
	}
	if resp.EnvBundle == nil {
		t.Fatalf("env_bundle is nil")
	}
	if got := resp.EnvBundle["AE_SESSION_ID"]; got != resp.SessionID.String() {
		t.Fatalf("env AE_SESSION_ID = %q, want %q", got, resp.SessionID.String())
	}
	if got := resp.EnvBundle["AE_RUNTIME_REF"]; got != resp.RuntimeRef {
		t.Fatalf("env AE_RUNTIME_REF = %q, want %q", got, resp.RuntimeRef)
	}
	if got := resp.EnvBundle["AE_PROVIDER_NAME"]; got != "sub2api" {
		t.Fatalf("env AE_PROVIDER_NAME = %q, want %q", got, "sub2api")
	}
	if got := resp.EnvBundle["AE_RELAY_API_KEY_ID"]; got != "555" {
		t.Fatalf("env AE_RELAY_API_KEY_ID = %q, want %q", got, "555")
	}
	if got := resp.EnvBundle["OPENAI_API_KEY"]; got != "sk-session-555" {
		t.Fatalf("env OPENAI_API_KEY = %q, want %q", got, "sk-session-555")
	}
	if got := resp.EnvBundle["OPENAI_BASE_URL"]; got != "http://relay.local/v1" {
		t.Fatalf("env OPENAI_BASE_URL = %q, want %q", got, "http://relay.local/v1")
	}
	if got := resp.EnvBundle["ANTHROPIC_API_KEY"]; got != "sk-session-555" {
		t.Fatalf("env ANTHROPIC_API_KEY = %q, want %q", got, "sk-session-555")
	}
	if got := resp.EnvBundle["ANTHROPIC_BASE_URL"]; got != "http://relay.local/v1" {
		t.Fatalf("env ANTHROPIC_BASE_URL = %q, want %q", got, "http://relay.local/v1")
	}

	if rp.lastCreateUserAPIKeyUserID != 99 {
		t.Fatalf("CreateUserAPIKey userID = %d, want %d", rp.lastCreateUserAPIKeyUserID, 99)
	}
	if rp.lastCreateUserAPIKeyReq.GroupID != "g-repo" {
		t.Fatalf("CreateUserAPIKey groupID = %q, want %q", rp.lastCreateUserAPIKeyReq.GroupID, "g-repo")
	}
	if rp.lastCreateUserAPIKeyReq.ExpiresAt == nil || rp.lastCreateUserAPIKeyReq.ExpiresAt.IsZero() {
		t.Fatalf("CreateUserAPIKey ExpiresAt is empty")
	}

	s, err := client.Session.Get(ctx, resp.SessionID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if s.RelayAPIKeyID == nil || *s.RelayAPIKeyID != int(resp.RelayAPIKeyID) {
		t.Fatalf("stored relay_api_key_id = %v, want %d", s.RelayAPIKeyID, resp.RelayAPIKeyID)
	}
	if s.ProviderName == nil || *s.ProviderName != "sub2api" {
		t.Fatalf("stored provider_name = %v, want %q", s.ProviderName, "sub2api")
	}
	if s.RelayUserID == nil || *s.RelayUserID != int(resp.RelayUserID) {
		t.Fatalf("stored relay_user_id = %v, want %d", s.RelayUserID, resp.RelayUserID)
	}
}

func TestBootstrapNoLongerCreatesRelayKeyOrEnvSecrets(t *testing.T) {
	ctx := context.Background()
	client := enttest.Open(t, "sqlite3", "file:ent?mode=memory&_fk=1")

	sp := client.ScmProvider.Create().
		SetName("mock-gh").
		SetType("github").
		SetBaseURL("https://api.github.com").
		SetCredentials("enc").
		SaveX(ctx)
	rc := client.RepoConfig.Create().
		SetScmProviderID(sp.ID).
		SetName("mock-repo").
		SetFullName("org/mock-repo").
		SetCloneURL("https://github.com/org/mock-repo.git").
		SetDefaultBranch("main").
		SetRelayGroupID("g-repo").
		SaveX(ctx)
	u := client.User.Create().
		SetUsername("alice").
		SetEmail("alice@example.com").
		SetAuthSource("ldap").
		SetRelayUserID(99).
		SaveX(ctx)

	rp := &fakeRelayProvider{}
	svc := NewService(client, rp, nil, "sub2api", "http://relay.local/v1", "g-default", 2*time.Hour)

	resp, err := svc.Bootstrap(ctx, u.ID, BootstrapRequest{
		RepoFullName:   rc.FullName,
		BranchSnapshot: "main",
		HeadSHA:        "abc123",
		WorkspaceRoot:  "/tmp/ws",
		GitDir:         "/tmp/ws/.git",
		GitCommonDir:   "/tmp/ws/.git",
		WorkspaceID:    "ws-1",
	})
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	if rp.lastCreateUserAPIKeyUserID != 0 {
		t.Fatalf("unexpected CreateUserAPIKey call for userID=%d", rp.lastCreateUserAPIKeyUserID)
	}
	for _, forbidden := range []string{"OPENAI_API_KEY", "ANTHROPIC_API_KEY", "OPENAI_BASE_URL", "ANTHROPIC_BASE_URL"} {
		if _, ok := resp.EnvBundle[forbidden]; ok {
			t.Fatalf("bootstrap env must not include %s: %+v", forbidden, resp.EnvBundle)
		}
	}
}

func TestResolveProviderCredentialReusesUsernameMatchBeforeCreating(t *testing.T) {
	ctx := context.Background()
	client := enttest.Open(t, "sqlite3", "file:ent?mode=memory&_fk=1")

	sp := client.ScmProvider.Create().
		SetName("mock-gh").
		SetType("github").
		SetBaseURL("https://api.github.com").
		SetCredentials("enc").
		SaveX(ctx)
	rc := client.RepoConfig.Create().
		SetScmProviderID(sp.ID).
		SetName("mock-repo").
		SetFullName("org/mock-repo").
		SetCloneURL("https://github.com/org/mock-repo.git").
		SetDefaultBranch("main").
		SetRelayGroupID("42").
		SaveX(ctx)
	u := client.User.Create().
		SetUsername("alice").
		SetEmail("alice@example.com").
		SetAuthSource("ldap").
		SetRelayUserID(99).
		SaveX(ctx)
	sid := uuid.New()
	client.Session.Create().
		SetID(sid).
		SetRepoConfigID(rc.ID).
		SetUserID(u.ID).
		SetBranch("main").
		SetProviderName("sub2api").
		SetStartedAt(time.Now()).
		SaveX(ctx)

	now := time.Now()
	rp := &fakeRelayProvider{
		listUserAPIKeysFn: func(_ context.Context, userID int64) ([]relay.APIKey, error) {
			return []relay.APIKey{
				{
					ID:         900,
					UserID:     userID,
					Key:        "sk-existing-openai",
					Name:       "alice",
					Status:     "active",
					LastUsedAt: ptrTime(now),
					CreatedAt:  now.Add(-time.Hour),
					Group:      &relay.Group{ID: 42, Platform: "openai"},
				},
			}, nil
		},
	}

	svc := NewService(client, rp, nil, "sub2api", "http://relay.local/v1", "42", 2*time.Hour)
	cred, err := svc.ResolveProviderCredential(ctx, u.ID, sid, "openai")
	if err != nil {
		t.Fatalf("ResolveProviderCredential: %v", err)
	}

	if cred.APIKeyID != 900 {
		t.Fatalf("api_key_id = %d, want %d", cred.APIKeyID, 900)
	}
	if cred.APIKey != "sk-existing-openai" {
		t.Fatalf("api_key = %q, want %q", cred.APIKey, "sk-existing-openai")
	}
	if rp.lastCreateUserAPIKeyUserID != 0 {
		t.Fatalf("unexpected CreateUserAPIKey call: %d", rp.lastCreateUserAPIKeyUserID)
	}
}

func TestResolveProviderCredentialFallsBackToEmailPrefixThenCreates(t *testing.T) {
	ctx := context.Background()
	client := enttest.Open(t, "sqlite3", "file:ent?mode=memory&_fk=1")

	sp := client.ScmProvider.Create().
		SetName("mock-gh").
		SetType("github").
		SetBaseURL("https://api.github.com").
		SetCredentials("enc").
		SaveX(ctx)
	rc := client.RepoConfig.Create().
		SetScmProviderID(sp.ID).
		SetName("mock-repo").
		SetFullName("org/mock-repo").
		SetCloneURL("https://github.com/org/mock-repo.git").
		SetDefaultBranch("main").
		SetRelayGroupID("77").
		SaveX(ctx)
	u := client.User.Create().
		SetUsername("alice").
		SetEmail("a.smith@example.com").
		SetAuthSource("ldap").
		SetRelayUserID(99).
		SaveX(ctx)
	sid := uuid.New()
	client.Session.Create().
		SetID(sid).
		SetRepoConfigID(rc.ID).
		SetUserID(u.ID).
		SetBranch("main").
		SetProviderName("sub2api").
		SetStartedAt(time.Now()).
		SaveX(ctx)

	createCalls := 0
	rp := &fakeRelayProvider{
		listUserAPIKeysFn: func(_ context.Context, userID int64) ([]relay.APIKey, error) {
			return []relay.APIKey{
				{
					ID:        901,
					UserID:    userID,
					Key:       "sk-existing-other-platform",
					Name:      "alice",
					Status:    "active",
					CreatedAt: time.Now(),
					Group:     &relay.Group{ID: 77, Platform: "openai"},
				},
			}, nil
		},
		createUserAPIKeyFn: func(_ context.Context, userID int64, req relay.APIKeyCreateRequest) (*relay.APIKeyWithSecret, error) {
			createCalls++
			return &relay.APIKeyWithSecret{
				APIKey: relay.APIKey{
					ID:        999,
					UserID:    userID,
					Name:      req.Name,
					Status:    "active",
					CreatedAt: time.Now(),
				},
				Secret: "sk-created-anthropic",
			}, nil
		},
	}

	svc := NewService(client, rp, nil, "sub2api", "http://relay.local/v1", "77", 2*time.Hour)
	cred, err := svc.ResolveProviderCredential(ctx, u.ID, sid, "anthropic")
	if err != nil {
		t.Fatalf("ResolveProviderCredential: %v", err)
	}

	if createCalls != 1 {
		t.Fatalf("createCalls = %d, want 1", createCalls)
	}
	if cred.APIKeyID != 999 {
		t.Fatalf("api_key_id = %d, want %d", cred.APIKeyID, 999)
	}
	if rp.lastCreateUserAPIKeyReq.Name != "alice" {
		t.Fatalf("created key name = %q, want %q", rp.lastCreateUserAPIKeyReq.Name, "alice")
	}
}

func TestResolveProviderCredentialCreatesUsingPlatformSpecificGroup(t *testing.T) {
	ctx := context.Background()
	client := enttest.Open(t, "sqlite3", "file:ent?mode=memory&_fk=1")

	sp := client.ScmProvider.Create().
		SetName("mock-gh").
		SetType("github").
		SetBaseURL("https://api.github.com").
		SetCredentials("enc").
		SaveX(ctx)
	rc := client.RepoConfig.Create().
		SetScmProviderID(sp.ID).
		SetName("mock-repo").
		SetFullName("org/mock-repo").
		SetCloneURL("https://github.com/org/mock-repo.git").
		SetDefaultBranch("main").
		SetRelayGroupID("77").
		SaveX(ctx)
	u := client.User.Create().
		SetUsername("alice").
		SetEmail("alice@example.com").
		SetAuthSource("ldap").
		SetRelayUserID(99).
		SaveX(ctx)
	sid := uuid.New()
	client.Session.Create().
		SetID(sid).
		SetRepoConfigID(rc.ID).
		SetUserID(u.ID).
		SetBranch("main").
		SetProviderName("sub2api").
		SetStartedAt(time.Now()).
		SaveX(ctx)

	rp := &fakeRelayProvider{
		listUserAPIKeysFn: func(_ context.Context, userID int64) ([]relay.APIKey, error) {
			return nil, nil
		},
		createUserAPIKeyFn: func(_ context.Context, userID int64, req relay.APIKeyCreateRequest) (*relay.APIKeyWithSecret, error) {
			return &relay.APIKeyWithSecret{
				APIKey: relay.APIKey{
					ID:        1001,
					UserID:    userID,
					Name:      req.Name,
					Status:    "active",
					CreatedAt: time.Now(),
				},
				Secret: "sk-created-openai",
			}, nil
		},
		resolveDefaultGroupIDForPlatformFn: func(_ context.Context, platform string) (string, error) {
			if platform != "openai" {
				t.Fatalf("platform = %q, want openai", platform)
			}
			return "42", nil
		},
	}

	svc := NewService(client, rp, nil, "sub2api", "http://relay.local/v1", "77", 2*time.Hour)
	cred, err := svc.ResolveProviderCredential(ctx, u.ID, sid, "openai")
	if err != nil {
		t.Fatalf("ResolveProviderCredential: %v", err)
	}
	if cred.APIKeyID != 1001 {
		t.Fatalf("api_key_id = %d, want %d", cred.APIKeyID, 1001)
	}
	if rp.lastCreateUserAPIKeyReq.GroupID != "42" {
		t.Fatalf("created group_id = %q, want %q", rp.lastCreateUserAPIKeyReq.GroupID, "42")
	}
}

func TestResolveProviderCredentialRejectsNonOwner(t *testing.T) {
	ctx := context.Background()
	client := enttest.Open(t, "sqlite3", "file:ent?mode=memory&_fk=1")

	sp := client.ScmProvider.Create().
		SetName("mock-gh").
		SetType("github").
		SetBaseURL("https://api.github.com").
		SetCredentials("enc").
		SaveX(ctx)
	rc := client.RepoConfig.Create().
		SetScmProviderID(sp.ID).
		SetName("mock-repo").
		SetFullName("org/mock-repo").
		SetCloneURL("https://github.com/org/mock-repo.git").
		SetDefaultBranch("main").
		SetRelayGroupID("42").
		SaveX(ctx)
	owner := client.User.Create().
		SetUsername("owner").
		SetEmail("owner@example.com").
		SetAuthSource("ldap").
		SetRelayUserID(77).
		SaveX(ctx)
	other := client.User.Create().
		SetUsername("other").
		SetEmail("other@example.com").
		SetAuthSource("ldap").
		SetRelayUserID(88).
		SaveX(ctx)
	sid := uuid.New()
	client.Session.Create().
		SetID(sid).
		SetRepoConfigID(rc.ID).
		SetUserID(owner.ID).
		SetBranch("main").
		SetProviderName("sub2api").
		SetStartedAt(time.Now()).
		SaveX(ctx)

	svc := NewService(client, &fakeRelayProvider{}, nil, "sub2api", "http://relay.local/v1", "42", 2*time.Hour)
	if _, err := svc.ResolveProviderCredential(ctx, other.ID, sid, "openai"); err == nil {
		t.Fatalf("expected ownership error")
	}
}

func TestStopRevokesRelayKey(t *testing.T) {
	ctx := context.Background()
	client := enttest.Open(t, "sqlite3", "file:ent?mode=memory&_fk=1")

	sp, err := client.ScmProvider.Create().
		SetName("mock-gh").
		SetType("github").
		SetBaseURL("https://api.github.com").
		SetCredentials("enc").
		Save(ctx)
	if err != nil {
		t.Fatalf("create scm provider: %v", err)
	}
	rc, err := client.RepoConfig.Create().
		SetScmProviderID(sp.ID).
		SetName("mock-repo").
		SetFullName("org/mock-repo").
		SetCloneURL("https://github.com/org/mock-repo.git").
		SetDefaultBranch("main").
		Save(ctx)
	if err != nil {
		t.Fatalf("create repo config: %v", err)
	}

	sid := uuid.New()
	keyID := 777
	_, err = client.Session.Create().
		SetID(sid).
		SetRepoConfigID(rc.ID).
		SetBranch("main").
		SetRelayAPIKeyID(keyID).
		SetProviderName("sub2api").
		SetStartedAt(time.Now()).
		Save(ctx)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	rp := &fakeRelayProvider{}
	svc := NewService(client, rp, nil, "sub2api", "http://relay.local/v1", "g-default", 2*time.Hour)

	stopped, err := svc.Stop(ctx, sid)
	if err != nil {
		t.Fatalf("stop: %v", err)
	}
	if stopped.EndedAt == nil {
		t.Fatalf("ended_at is nil")
	}
	if stopped.Status != "completed" {
		t.Fatalf("status = %q, want %q", stopped.Status, "completed")
	}
	if len(rp.revokedKeyIDs) != 1 || rp.revokedKeyIDs[0] != int64(keyID) {
		t.Fatalf("revoked = %v, want [%d]", rp.revokedKeyIDs, keyID)
	}
}

func TestBootstrapFallsBackToRelayResolvedDefaultGroup(t *testing.T) {
	ctx := context.Background()
	client := enttest.Open(t, "sqlite3", "file:ent?mode=memory&_fk=1")

	sp, err := client.ScmProvider.Create().
		SetName("mock-gh").
		SetType("github").
		SetBaseURL("https://api.github.com").
		SetCredentials("enc").
		Save(ctx)
	if err != nil {
		t.Fatalf("create scm provider: %v", err)
	}

	rc, err := client.RepoConfig.Create().
		SetScmProviderID(sp.ID).
		SetName("mock-repo").
		SetFullName("org/mock-repo").
		SetCloneURL("https://github.com/org/mock-repo.git").
		SetDefaultBranch("main").
		Save(ctx)
	if err != nil {
		t.Fatalf("create repo config: %v", err)
	}

	u, err := client.User.Create().
		SetUsername("alice").
		SetEmail("alice@example.com").
		SetAuthSource("ldap").
		Save(ctx)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	rp := &fakeRelayProvider{
		findUserByUsernameFn: func(_ context.Context, username string) (*relay.User, error) {
			return &relay.User{ID: 99, Username: username, Email: "alice@relay.local"}, nil
		},
		createUserAPIKeyFn: func(_ context.Context, userID int64, req relay.APIKeyCreateRequest) (*relay.APIKeyWithSecret, error) {
			return &relay.APIKeyWithSecret{
				APIKey: relay.APIKey{ID: 555, UserID: userID, Name: req.Name, Status: "active"},
				Secret: "sk-session-555",
			}, nil
		},
		resolveDefaultGroupIDFn: func(_ context.Context) (string, error) {
			return "g-auto", nil
		},
	}
	resolver := auth.NewRelayIdentityResolver(rp, "ldap.local")
	svc := NewService(client, rp, resolver, "sub2api", "http://relay.local/v1", "", 2*time.Hour)

	resp, err := svc.Bootstrap(ctx, u.ID, BootstrapRequest{
		RepoFullName:   rc.FullName,
		BranchSnapshot: "main",
		HeadSHA:        "abc123",
		WorkspaceRoot:  "/tmp/ws",
		GitDir:         "/tmp/ws/.git",
		GitCommonDir:   "/tmp/ws/.git",
		WorkspaceID:    "ws-1",
	})
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if resp.GroupID != "g-auto" {
		t.Fatalf("group_id = %q, want %q", resp.GroupID, "g-auto")
	}
	if resp.RouteBindingSource != "relay_default" {
		t.Fatalf("route_binding_source = %q, want %q", resp.RouteBindingSource, "relay_default")
	}
	if rp.lastCreateUserAPIKeyReq.GroupID != "g-auto" {
		t.Fatalf("CreateUserAPIKey groupID = %q, want %q", rp.lastCreateUserAPIKeyReq.GroupID, "g-auto")
	}
}

func TestBootstrapUsesStoredRelayCredentialsForSessionKeyCreation(t *testing.T) {
	ctx := context.Background()
	client := enttest.Open(t, "sqlite3", "file:ent?mode=memory&_fk=1")
	encryptionKey := "0000000000000000000000000000000000000000000000000000000000000000"

	sp, err := client.ScmProvider.Create().
		SetName("mock-gh").
		SetType("github").
		SetBaseURL("https://api.github.com").
		SetCredentials("enc").
		Save(ctx)
	if err != nil {
		t.Fatalf("create scm provider: %v", err)
	}

	rc, err := client.RepoConfig.Create().
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

	encryptedPassword, err := pkg.Encrypt("stored-secret", encryptionKey)
	if err != nil {
		t.Fatalf("encrypt password: %v", err)
	}
	u, err := client.User.Create().
		SetUsername("alice@example.com").
		SetEmail("alice@example.com").
		SetAuthSource("relay_sso").
		SetRelayUserID(99).
		SetRelayAuthPassword(encryptedPassword).
		Save(ctx)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	rp := &fakeRelayProvider{
		createUserAPIKeyFn: func(ctx context.Context, userID int64, req relay.APIKeyCreateRequest) (*relay.APIKeyWithSecret, error) {
			login, password, ok := relay.UserCredentialsFromContext(ctx)
			if !ok {
				t.Fatal("expected relay user credentials in context")
			}
			if login != "alice@example.com" {
				t.Fatalf("login = %q, want %q", login, "alice@example.com")
			}
			if password != "stored-secret" {
				t.Fatalf("password = %q, want %q", password, "stored-secret")
			}
			return &relay.APIKeyWithSecret{
				APIKey: relay.APIKey{ID: 555, UserID: userID, Name: req.Name, Status: "active"},
				Secret: "sk-session-555",
			}, nil
		},
	}

	svc := NewService(client, rp, nil, "sub2api", "http://relay.local/v1", "g-default", 2*time.Hour, encryptionKey)
	resp, err := svc.Bootstrap(ctx, u.ID, BootstrapRequest{
		RepoFullName:   rc.FullName,
		BranchSnapshot: "main",
		HeadSHA:        "abc123",
		WorkspaceRoot:  "/tmp/ws",
		GitDir:         "/tmp/ws/.git",
		GitCommonDir:   "/tmp/ws/.git",
		WorkspaceID:    "ws-1",
	})
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if resp.RelayAPIKeyID != 555 {
		t.Fatalf("relay_api_key_id = %d, want %d", resp.RelayAPIKeyID, 555)
	}
}

func TestBootstrapRevokesRelayKeyWhenSessionSaveFails(t *testing.T) {
	ctx := context.Background()
	client := enttest.Open(t, "sqlite3", "file:ent?mode=memory&_fk=1")

	// Force session persistence to fail after the relay key is created.
	client.Session.Use(func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
			if m.Op() == ent.OpCreate {
				return nil, errors.New("db insert failed")
			}
			return next.Mutate(ctx, m)
		})
	})

	sp, err := client.ScmProvider.Create().
		SetName("mock-gh").
		SetType("github").
		SetBaseURL("https://api.github.com").
		SetCredentials("enc").
		Save(ctx)
	if err != nil {
		t.Fatalf("create scm provider: %v", err)
	}
	rc, err := client.RepoConfig.Create().
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

	u, err := client.User.Create().
		SetUsername("alice").
		SetEmail("alice@example.com").
		SetAuthSource("ldap").
		SetRelayUserID(99).
		Save(ctx)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	rp := &fakeRelayProvider{
		createUserAPIKeyFn: func(_ context.Context, userID int64, req relay.APIKeyCreateRequest) (*relay.APIKeyWithSecret, error) {
			return &relay.APIKeyWithSecret{
				APIKey: relay.APIKey{ID: 555, UserID: userID, Name: req.Name, Status: "active"},
				Secret: "sk-session-555",
			}, nil
		},
	}
	svc := NewService(client, rp, nil, "sub2api", "http://relay.local/v1", "g-default", 2*time.Hour)

	_, err = svc.Bootstrap(ctx, u.ID, BootstrapRequest{
		RepoFullName:   rc.FullName,
		BranchSnapshot: "main",
		HeadSHA:        "abc123",
		WorkspaceRoot:  "/tmp/ws",
		GitDir:         "/tmp/ws/.git",
		GitCommonDir:   "/tmp/ws/.git",
		WorkspaceID:    "ws-1",
	})
	if err == nil {
		t.Fatalf("bootstrap: expected error")
	}

	if len(rp.revokedKeyIDs) != 1 || rp.revokedKeyIDs[0] != 555 {
		t.Fatalf("revoked = %v, want [555]", rp.revokedKeyIDs)
	}
}

func TestBootstrapUsesRelayProviderNameWhenConfigEmpty(t *testing.T) {
	ctx := context.Background()
	client := enttest.Open(t, "sqlite3", "file:ent?mode=memory&_fk=1")

	sp, err := client.ScmProvider.Create().
		SetName("mock-gh").
		SetType("github").
		SetBaseURL("https://api.github.com").
		SetCredentials("enc").
		Save(ctx)
	if err != nil {
		t.Fatalf("create scm provider: %v", err)
	}
	rc, err := client.RepoConfig.Create().
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

	u, err := client.User.Create().
		SetUsername("alice").
		SetEmail("alice@example.com").
		SetAuthSource("ldap").
		SetRelayUserID(99).
		Save(ctx)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	rp := &fakeRelayProvider{
		createUserAPIKeyFn: func(_ context.Context, userID int64, req relay.APIKeyCreateRequest) (*relay.APIKeyWithSecret, error) {
			return &relay.APIKeyWithSecret{
				APIKey: relay.APIKey{ID: 555, UserID: userID, Name: req.Name, Status: "active"},
				Secret: "sk-session-555",
			}, nil
		},
	}

	// Config provider name is empty; the service should still use the relay provider identity.
	svc := NewService(client, rp, nil, "", "http://relay.local/v1", "g-default", 2*time.Hour)

	resp, err := svc.Bootstrap(ctx, u.ID, BootstrapRequest{
		RepoFullName:   rc.FullName,
		BranchSnapshot: "main",
		HeadSHA:        "abc123",
		WorkspaceRoot:  "/tmp/ws",
		GitDir:         "/tmp/ws/.git",
		GitCommonDir:   "/tmp/ws/.git",
		WorkspaceID:    "ws-1",
	})
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if resp.ProviderName != rp.Name() {
		t.Fatalf("provider_name = %q, want %q", resp.ProviderName, rp.Name())
	}
}
