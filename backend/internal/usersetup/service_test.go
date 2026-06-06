package usersetup_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/ent/user"
	"github.com/ai-efficiency/backend/internal/pkg"
	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/ai-efficiency/backend/internal/testdb"
	"github.com/ai-efficiency/backend/internal/usersetup"
	"github.com/google/go-cmp/cmp"
)

type fakeRelayProvider struct {
	keysByUser                 map[int64][]relay.APIKey
	usersByID                  map[int64]relay.User
	usersByEmail               map[string]relay.User
	usersByUsername            map[string]relay.User
	requireExistingUser        bool
	createUserCalls            []relay.CreateUserRequest
	nextUserID                 int64
	createResult               *relay.APIKeyWithSecret
	createErr                  error
	updatedStatuses            map[int64]string
	updatedUsers               map[int64]relay.UpdateUserRequest
	createCredentialLogin      string
	createCredentialPassword   string
	createCredentialUserID     int64
	updateCredentialLogin      string
	updateCredentialPassword   string
	lastCreateReq              relay.APIKeyCreateRequest
	requireUpdatedPassword     bool
	listUserAPIKeysFn          func(ctx context.Context, userID int64) ([]relay.APIKey, error)
	createUserAPIKeyFn         func(ctx context.Context, userID int64, req relay.APIKeyCreateRequest) (*relay.APIKeyWithSecret, error)
	listAllowedGroupsForUserFn func(ctx context.Context, userID int64) ([]relay.Group, error)
}

func (f *fakeRelayProvider) Ping(ctx context.Context) error { return nil }
func (f *fakeRelayProvider) Name() string                   { return "fake-relay" }
func (f *fakeRelayProvider) Authenticate(ctx context.Context, username, password string) (*relay.User, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeRelayProvider) GetUser(ctx context.Context, userID int64) (*relay.User, error) {
	if f.usersByID != nil {
		if u, ok := f.usersByID[userID]; ok {
			copy := u
			return &copy, nil
		}
	}
	if f.requireExistingUser {
		return nil, nil
	}
	if _, ok := f.keysByUser[userID]; ok {
		return &relay.User{ID: userID}, nil
	}
	return nil, errors.New("not implemented")
}
func (f *fakeRelayProvider) ListAllowedGroupsForUser(ctx context.Context, userID int64) ([]relay.Group, error) {
	if f.listAllowedGroupsForUserFn != nil {
		return f.listAllowedGroupsForUserFn(ctx, userID)
	}
	return nil, nil
}
func (f *fakeRelayProvider) FindUserByEmail(ctx context.Context, email string) (*relay.User, error) {
	if f.usersByEmail != nil {
		if u, ok := f.usersByEmail[email]; ok {
			copy := u
			return &copy, nil
		}
	}
	return nil, nil
}
func (f *fakeRelayProvider) FindUserByUsername(ctx context.Context, username string) (*relay.User, error) {
	if f.usersByUsername != nil {
		if u, ok := f.usersByUsername[username]; ok {
			copy := u
			return &copy, nil
		}
	}
	return nil, nil
}
func (f *fakeRelayProvider) CreateUser(ctx context.Context, req relay.CreateUserRequest) (*relay.User, error) {
	f.createUserCalls = append(f.createUserCalls, req)
	id := f.nextUserID
	if id == 0 {
		id = 99
	}
	role := req.Role
	if strings.TrimSpace(role) == "" {
		role = "user"
	}
	u := relay.User{ID: id, Username: req.Username, Email: req.Email, Role: role, Notes: req.Notes}
	if f.usersByID == nil {
		f.usersByID = map[int64]relay.User{}
	}
	f.usersByID[id] = u
	if f.usersByEmail == nil {
		f.usersByEmail = map[string]relay.User{}
	}
	f.usersByEmail[req.Email] = u
	if f.usersByUsername == nil {
		f.usersByUsername = map[string]relay.User{}
	}
	f.usersByUsername[req.Username] = u
	return &u, nil
}
func (f *fakeRelayProvider) UpdateUser(ctx context.Context, userID int64, req relay.UpdateUserRequest) (*relay.User, error) {
	if f.updatedUsers == nil {
		f.updatedUsers = map[int64]relay.UpdateUserRequest{}
	}
	f.updatedUsers[userID] = req
	return &relay.User{ID: userID, Username: req.Username, Email: req.Email, Role: "user"}, nil
}
func (f *fakeRelayProvider) ChatCompletion(ctx context.Context, req relay.ChatCompletionRequest) (*relay.ChatCompletionResponse, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeRelayProvider) ChatCompletionWithTools(ctx context.Context, req relay.ChatCompletionRequest, tools []relay.ToolDef) (*relay.ChatCompletionWithToolsResponse, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeRelayProvider) GetUsageStats(ctx context.Context, userID int64, from, to time.Time) (*relay.UsageStats, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeRelayProvider) ListUserAPIKeys(ctx context.Context, userID int64) ([]relay.APIKey, error) {
	if f.listUserAPIKeysFn != nil {
		return f.listUserAPIKeysFn(ctx, userID)
	}
	if f.requireExistingUser {
		if _, ok := f.usersByID[userID]; !ok {
			return nil, errors.New("relay user missing")
		}
	}
	if keys, ok := f.keysByUser[userID]; ok {
		cloned := make([]relay.APIKey, len(keys))
		copy(cloned, keys)
		return cloned, nil
	}
	return nil, nil
}
func (f *fakeRelayProvider) CreateUserAPIKey(ctx context.Context, userID int64, req relay.APIKeyCreateRequest) (*relay.APIKeyWithSecret, error) {
	if f.createUserAPIKeyFn != nil {
		return f.createUserAPIKeyFn(ctx, userID, req)
	}
	login, password, _ := relay.UserCredentialsFromContext(ctx)
	f.createCredentialLogin = login
	f.createCredentialPassword = password
	f.createCredentialUserID = userID
	f.lastCreateReq = req
	if f.requireUpdatedPassword {
		updateReq, ok := f.updatedUsers[userID]
		if !ok || strings.TrimSpace(updateReq.Password) == "" || password != updateReq.Password {
			return nil, relay.ErrInvalidCredentials
		}
	}
	if f.createErr != nil {
		return nil, f.createErr
	}
	return f.createResult, nil
}
func (f *fakeRelayProvider) UpdateUserAPIKeyStatus(ctx context.Context, keyID int64, status string) error {
	login, password, _ := relay.UserCredentialsFromContext(ctx)
	f.updateCredentialLogin = login
	f.updateCredentialPassword = password
	if f.updatedStatuses == nil {
		f.updatedStatuses = map[int64]string{}
	}
	f.updatedStatuses[keyID] = status
	return nil
}
func (f *fakeRelayProvider) RevokeUserAPIKey(ctx context.Context, keyID int64) error {
	return errors.New("not implemented")
}
func (f *fakeRelayProvider) ListUsageLogsByAPIKeyExact(ctx context.Context, apiKeyID int64, from, to time.Time) ([]relay.UsageLog, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeRelayProvider) GetUserUsageDashboard(ctx context.Context, login, password string, params relay.UserUsageDashboardParams) (*relay.UserUsageDashboardResponse, error) {
	return nil, errors.New("not implemented")
}

func TestListProvidersReturnsOnlyAllowedGroups(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)

	provider := client.RelayProvider.Create().
		SetName("sub2api").
		SetDisplayName("sub2api").
		SetBaseURL("https://relay.example.com/").
		SetAdminAPIKey("test-admin-key").
		SetDefaultModel("gpt-5.4").
		SetIsPrimary(true).
		SetEnabled(true).
		SaveX(ctx)

	localUser := client.User.Create().
		SetUsername("alice@example.com").
		SetEmail("alice@example.com").
		SetAuthSource(user.AuthSourceRelaySSO).
		SetRole(user.RoleAdmin).
		SetRelayUserID(1).
		SaveX(ctx)

	fakeRelay := &fakeRelayProvider{
		keysByUser: map[int64][]relay.APIKey{
			1: {
				{ID: 20, UserID: 1, Key: "sk-existing-openai-123456", Name: "alice", Status: "active", Group: &relay.Group{ID: 6, Name: "Group Alpha", Platform: "openai"}, CreatedAt: time.Now()},
				{ID: 21, UserID: 1, Name: "other", Status: "active", Group: &relay.Group{ID: 99, Name: "Other", Platform: "openai"}, CreatedAt: time.Now()},
			},
		},
		listAllowedGroupsForUserFn: func(_ context.Context, userID int64) ([]relay.Group, error) {
			if userID != 1 {
				t.Fatalf("userID = %d, want 1", userID)
			}
			return []relay.Group{
				{ID: 5, Name: "Group Gamma", Platform: "anthropic"},
				{ID: 6, Name: "Group Alpha", Platform: "openai"},
			}, nil
		},
	}

	svc := usersetup.NewService(client, usersetup.ProviderResolverFunc(func(_ context.Context, providerID int) (relay.Provider, error) {
		if providerID != provider.ID {
			t.Fatalf("providerID = %d, want %d", providerID, provider.ID)
		}
		return fakeRelay, nil
	}), "d98460dc58409c713d1586802217c23932d58c95479641e4b0fec1c740386696")

	resp, err := svc.ListProviders(ctx, usersetup.ListProvidersRequest{UserID: localUser.ID})
	if err != nil {
		t.Fatalf("ListProviders() unexpected error: %v", err)
	}
	if len(resp.Providers) != 1 {
		t.Fatalf("providers len = %d, want 1", len(resp.Providers))
	}
	got := resp.Providers[0]
	if diff := cmp.Diff([]string{"5", "6"}, groupIDs(got.Groups)); diff != "" {
		t.Fatalf("group mismatch (-want +got):\n%s", diff)
	}
	if got.Groups[0].GroupName != "Group Gamma" || got.Groups[0].Platform != "anthropic" {
		t.Fatalf("unexpected first group: %+v", got.Groups[0])
	}
	if got.Groups[1].Credential.APIKeyID != 20 {
		t.Fatalf("group credential api key id = %d, want 20", got.Groups[1].Credential.APIKeyID)
	}
	if got.Groups[1].Credential.Key != "sk-existing-openai-123456" {
		t.Fatalf("group credential key = %q, want full API key", got.Groups[1].Credential.Key)
	}
}

func TestCreateGroupCredentialUsesSelectedGroupID(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	encryptionKey := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	encryptedPassword, err := pkg.Encrypt("sso-password", encryptionKey)
	if err != nil {
		t.Fatalf("Encrypt() unexpected error: %v", err)
	}

	provider := client.RelayProvider.Create().
		SetName("sub2api").
		SetDisplayName("sub2api").
		SetBaseURL("https://relay.example.com/").
		SetAdminAPIKey("test-admin-key").
		SetDefaultModel("gpt-5.4").
		SetIsPrimary(true).
		SetEnabled(true).
		SaveX(ctx)

	localUser := client.User.Create().
		SetUsername("alice@example.com").
		SetEmail("alice@example.com").
		SetAuthSource(user.AuthSourceRelaySSO).
		SetRole(user.RoleAdmin).
		SetRelayUserID(1).
		SetRelayAuthPassword(encryptedPassword).
		SaveX(ctx)

	fakeRelay := &fakeRelayProvider{
		keysByUser: map[int64][]relay.APIKey{1: {}},
		usersByID: map[int64]relay.User{
			1: {ID: 1, Email: "alice@example.com", Username: "alice@example.com", Role: "user"},
		},
		createResult: &relay.APIKeyWithSecret{
			APIKey: relay.APIKey{
				ID:        701,
				UserID:    1,
				Name:      "alice",
				Status:    "active",
				CreatedAt: time.Now(),
				Group:     &relay.Group{ID: 42, Name: "Group Alpha", Platform: "openai"},
			},
			Secret: "sk-openai",
		},
		listAllowedGroupsForUserFn: func(_ context.Context, userID int64) ([]relay.Group, error) {
			return []relay.Group{
				{ID: 42, Name: "Group Alpha", Platform: "openai"},
				{ID: 43, Name: "Group Beta", Platform: "openai"},
			}, nil
		},
	}

	svc := usersetup.NewService(client, usersetup.ProviderResolverFunc(func(_ context.Context, providerID int) (relay.Provider, error) {
		return fakeRelay, nil
	}), encryptionKey)

	got, err := svc.CreateGroupCredential(ctx, usersetup.CreateGroupCredentialRequest{
		UserID:     localUser.ID,
		ProviderID: provider.ID,
		GroupID:    "42",
	})
	if err != nil {
		t.Fatalf("CreateGroupCredential() unexpected error: %v", err)
	}
	if got.Name != "alice" {
		t.Fatalf("name = %q, want alice", got.Name)
	}
	if fakeRelay.lastCreateReq.GroupID != "42" {
		t.Fatalf("group id = %q, want 42", fakeRelay.lastCreateReq.GroupID)
	}
	if fakeRelay.createCredentialLogin != "alice@example.com" || fakeRelay.createCredentialPassword != "sso-password" {
		t.Fatalf("create credentials = (%q, %q), want stored SSO credentials", fakeRelay.createCredentialLogin, fakeRelay.createCredentialPassword)
	}
}

func TestCreateGroupCredentialConcurrentCallsAreIdempotent(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	encryptionKey := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	encryptedPassword, err := pkg.Encrypt("sso-password", encryptionKey)
	if err != nil {
		t.Fatalf("Encrypt() unexpected error: %v", err)
	}

	provider := client.RelayProvider.Create().
		SetName("sub2api").
		SetDisplayName("sub2api").
		SetBaseURL("https://relay.example.com/").
		SetAdminAPIKey("test-admin-key").
		SetDefaultModel("gpt-5.4").
		SetIsPrimary(true).
		SetEnabled(true).
		SaveX(ctx)

	localUser := client.User.Create().
		SetUsername("alice@example.com").
		SetEmail("alice@example.com").
		SetAuthSource(user.AuthSourceRelaySSO).
		SetRole(user.RoleUser).
		SetRelayUserID(1).
		SetRelayAuthPassword(encryptedPassword).
		SaveX(ctx)

	var keysMu sync.Mutex
	var keys []relay.APIKey
	var listCalls int32
	var createCalls int32
	fakeRelay := &fakeRelayProvider{
		usersByID: map[int64]relay.User{
			1: {ID: 1, Email: "alice@example.com", Username: "alice@example.com", Role: "user"},
		},
	}
	fakeRelay.listUserAPIKeysFn = func(_ context.Context, userID int64) ([]relay.APIKey, error) {
		keysMu.Lock()
		snapshot := append([]relay.APIKey(nil), keys...)
		keysMu.Unlock()
		call := atomic.AddInt32(&listCalls, 1)
		if call == 1 {
			deadline := time.After(100 * time.Millisecond)
			for atomic.LoadInt32(&listCalls) < 2 {
				select {
				case <-deadline:
					return snapshot, nil
				default:
					time.Sleep(time.Millisecond)
				}
			}
		}
		return snapshot, nil
	}
	fakeRelay.createUserAPIKeyFn = func(ctx context.Context, userID int64, req relay.APIKeyCreateRequest) (*relay.APIKeyWithSecret, error) {
		login, password, _ := relay.UserCredentialsFromContext(ctx)
		if login != "alice@example.com" || password != "sso-password" {
			t.Fatalf("create credentials = (%q, %q), want stored SSO credentials", login, password)
		}
		id := int64(700 + atomic.AddInt32(&createCalls, 1))
		created := relay.APIKey{
			ID:        id,
			UserID:    userID,
			Key:       fmt.Sprintf("sk-created-%d", id),
			Name:      req.Name,
			Status:    "active",
			CreatedAt: time.Now(),
			Group:     &relay.Group{ID: 42, Name: "Group Alpha", Platform: "openai"},
		}
		keysMu.Lock()
		keys = append(keys, created)
		keysMu.Unlock()
		return &relay.APIKeyWithSecret{APIKey: created, Secret: created.Key}, nil
	}

	svc := usersetup.NewService(client, usersetup.ProviderResolverFunc(func(_ context.Context, providerID int) (relay.Provider, error) {
		return fakeRelay, nil
	}), encryptionKey)

	type result struct {
		got *usersetup.CreateGroupCredentialResult
		err error
	}
	results := make(chan result, 2)
	for i := 0; i < 2; i++ {
		go func() {
			got, err := svc.CreateGroupCredential(ctx, usersetup.CreateGroupCredentialRequest{
				UserID:     localUser.ID,
				ProviderID: provider.ID,
				GroupID:    "42",
			})
			results <- result{got: got, err: err}
		}()
	}

	first := <-results
	second := <-results
	if first.err != nil || second.err != nil {
		t.Fatalf("CreateGroupCredential() errors = (%v, %v), want nil", first.err, second.err)
	}
	if first.got == nil || second.got == nil {
		t.Fatalf("CreateGroupCredential() returned nil result: %#v %#v", first.got, second.got)
	}
	if first.got.APIKeyID != second.got.APIKeyID {
		t.Fatalf("api key ids = (%d, %d), want same id", first.got.APIKeyID, second.got.APIKeyID)
	}
	if got := atomic.LoadInt32(&createCalls); got != 1 {
		t.Fatalf("create calls = %d, want 1", got)
	}
}

func TestCreateGroupCredentialCreatesRelayUserWhenLocalUserHasNoRelayBinding(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	encryptionKey := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

	provider := client.RelayProvider.Create().
		SetName("sub2api").
		SetDisplayName("sub2api").
		SetBaseURL("https://relay.example.com/").
		SetAdminAPIKey("test-admin-key").
		SetDefaultModel("gpt-5.4").
		SetIsPrimary(true).
		SetEnabled(true).
		SaveX(ctx)

	localUser := client.User.Create().
		SetUsername("alice@example.com").
		SetEmail("alice@example.com").
		SetAuthSource(user.AuthSourceLdap).
		SetRole(user.RoleUser).
		SaveX(ctx)

	fakeRelay := &fakeRelayProvider{
		nextUserID: 55,
		createResult: &relay.APIKeyWithSecret{
			APIKey: relay.APIKey{
				ID:     701,
				UserID: 55,
				Name:   "alice",
				Status: "active",
				Group:  &relay.Group{ID: 42, Name: "Group Alpha", Platform: "openai"},
			},
			Secret: "sk-openai",
		},
	}

	svc := usersetup.NewService(client, usersetup.ProviderResolverFunc(func(_ context.Context, providerID int) (relay.Provider, error) {
		return fakeRelay, nil
	}), encryptionKey)

	got, err := svc.CreateGroupCredential(ctx, usersetup.CreateGroupCredentialRequest{
		UserID:     localUser.ID,
		ProviderID: provider.ID,
		GroupID:    "42",
	})
	if err != nil {
		t.Fatalf("CreateGroupCredential() unexpected error: %v", err)
	}
	if got.Secret != "sk-openai" {
		t.Fatalf("secret = %q, want sk-openai", got.Secret)
	}
	if len(fakeRelay.createUserCalls) != 1 {
		t.Fatalf("expected relay user creation, got %+v", fakeRelay.createUserCalls)
	}
	createdUserReq := fakeRelay.createUserCalls[0]
	if createdUserReq.Username != "alice" || createdUserReq.Email != "alice@example.com" || strings.TrimSpace(createdUserReq.Password) == "" {
		t.Fatalf("unexpected relay create user request: %+v", createdUserReq)
	}
	if fakeRelay.createCredentialUserID != 55 {
		t.Fatalf("create key user id = %d, want 55", fakeRelay.createCredentialUserID)
	}
	if fakeRelay.createCredentialLogin != "alice@example.com" || fakeRelay.createCredentialPassword != createdUserReq.Password {
		t.Fatalf("create credentials = (%q, %q), want generated relay credentials", fakeRelay.createCredentialLogin, fakeRelay.createCredentialPassword)
	}

	reloaded, err := client.User.Get(ctx, localUser.ID)
	if err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if reloaded.RelayUserID == nil || *reloaded.RelayUserID != 55 {
		t.Fatalf("relay_user_id = %v, want 55", reloaded.RelayUserID)
	}
	if reloaded.RelayAuthPassword == nil || strings.TrimSpace(*reloaded.RelayAuthPassword) == "" {
		t.Fatal("expected generated relay password to be stored")
	}
	decrypted, err := pkg.Decrypt(*reloaded.RelayAuthPassword, encryptionKey)
	if err != nil {
		t.Fatalf("Decrypt() unexpected error: %v", err)
	}
	if decrypted != createdUserReq.Password {
		t.Fatalf("stored relay password does not match created relay password")
	}
}

func TestCreateGroupCredentialRecreatesRelayUserWhenStoredRelayBindingIsMissingUpstream(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	encryptionKey := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

	provider := client.RelayProvider.Create().
		SetName("sub2api").
		SetDisplayName("sub2api").
		SetBaseURL("https://relay.example.com/").
		SetAdminAPIKey("test-admin-key").
		SetDefaultModel("gpt-5.4").
		SetIsPrimary(true).
		SetEnabled(true).
		SaveX(ctx)

	localUser := client.User.Create().
		SetUsername("alice@example.com").
		SetEmail("alice@example.com").
		SetAuthSource(user.AuthSourceLdap).
		SetRole(user.RoleUser).
		SetRelayUserID(1).
		SaveX(ctx)

	fakeRelay := &fakeRelayProvider{
		requireExistingUser: true,
		nextUserID:          55,
		createResult: &relay.APIKeyWithSecret{
			APIKey: relay.APIKey{
				ID:     701,
				UserID: 55,
				Name:   "alice",
				Status: "active",
				Group:  &relay.Group{ID: 42, Name: "Group Alpha", Platform: "openai"},
			},
			Secret: "sk-openai",
		},
	}

	svc := usersetup.NewService(client, usersetup.ProviderResolverFunc(func(_ context.Context, providerID int) (relay.Provider, error) {
		return fakeRelay, nil
	}), encryptionKey)

	got, err := svc.CreateGroupCredential(ctx, usersetup.CreateGroupCredentialRequest{
		UserID:     localUser.ID,
		ProviderID: provider.ID,
		GroupID:    "42",
	})
	if err != nil {
		t.Fatalf("CreateGroupCredential() unexpected error: %v", err)
	}
	if got.Secret != "sk-openai" {
		t.Fatalf("secret = %q, want sk-openai", got.Secret)
	}
	if len(fakeRelay.createUserCalls) != 1 {
		t.Fatalf("expected relay user recreation, got %+v", fakeRelay.createUserCalls)
	}
	createdUserReq := fakeRelay.createUserCalls[0]
	if createdUserReq.Username != "alice" || createdUserReq.Email != "alice@example.com" || strings.TrimSpace(createdUserReq.Password) == "" {
		t.Fatalf("unexpected relay create user request: %+v", createdUserReq)
	}
	if fakeRelay.createCredentialUserID != 55 {
		t.Fatalf("create key user id = %d, want 55", fakeRelay.createCredentialUserID)
	}
	if fakeRelay.createCredentialLogin != "alice@example.com" || fakeRelay.createCredentialPassword != createdUserReq.Password {
		t.Fatalf("create credentials = (%q, %q), want generated relay credentials", fakeRelay.createCredentialLogin, fakeRelay.createCredentialPassword)
	}

	reloaded, err := client.User.Get(ctx, localUser.ID)
	if err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if reloaded.RelayUserID == nil || *reloaded.RelayUserID != 55 {
		t.Fatalf("relay_user_id = %v, want 55", reloaded.RelayUserID)
	}
	if reloaded.RelayAuthPassword == nil || strings.TrimSpace(*reloaded.RelayAuthPassword) == "" {
		t.Fatal("expected generated relay password to be stored")
	}
	decrypted, err := pkg.Decrypt(*reloaded.RelayAuthPassword, encryptionKey)
	if err != nil {
		t.Fatalf("Decrypt() unexpected error: %v", err)
	}
	if decrypted != createdUserReq.Password {
		t.Fatalf("stored relay password does not match recreated relay password")
	}
}

func TestCreateGroupCredentialRotatesRelayPasswordForExistingLDAPUserWithoutStoredRelayPassword(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	encryptionKey := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

	provider := client.RelayProvider.Create().
		SetName("sub2api").
		SetDisplayName("sub2api").
		SetBaseURL("https://relay.example.com/").
		SetAdminAPIKey("test-admin-key").
		SetDefaultModel("gpt-5.4").
		SetIsPrimary(true).
		SetEnabled(true).
		SaveX(ctx)

	localUser := client.User.Create().
		SetUsername("alice@example.com").
		SetEmail("alice@example.com").
		SetAuthSource(user.AuthSourceLdap).
		SetRole(user.RoleUser).
		SetRelayUserID(1).
		SaveX(ctx)

	fakeRelay := &fakeRelayProvider{
		keysByUser: map[int64][]relay.APIKey{1: {}},
		usersByID: map[int64]relay.User{
			1: {ID: 1, Email: "alice@example.com", Username: "alice@example.com", Role: "user"},
		},
		createResult: &relay.APIKeyWithSecret{
			APIKey: relay.APIKey{
				ID:     701,
				UserID: 1,
				Name:   "alice",
				Status: "active",
				Group:  &relay.Group{ID: 42, Name: "Group Alpha", Platform: "openai"},
			},
			Secret: "sk-openai",
		},
	}

	svc := usersetup.NewService(client, usersetup.ProviderResolverFunc(func(_ context.Context, providerID int) (relay.Provider, error) {
		return fakeRelay, nil
	}), encryptionKey)

	got, err := svc.CreateGroupCredential(ctx, usersetup.CreateGroupCredentialRequest{
		UserID:     localUser.ID,
		ProviderID: provider.ID,
		GroupID:    "42",
	})
	if err != nil {
		t.Fatalf("CreateGroupCredential() unexpected error: %v", err)
	}
	if got.Secret != "sk-openai" {
		t.Fatalf("secret = %q, want sk-openai", got.Secret)
	}
	updateReq, ok := fakeRelay.updatedUsers[1]
	if !ok || strings.TrimSpace(updateReq.Password) == "" {
		t.Fatalf("expected generated relay password update, got %+v", fakeRelay.updatedUsers)
	}
	if fakeRelay.createCredentialLogin != "alice@example.com" || fakeRelay.createCredentialPassword != updateReq.Password {
		t.Fatalf("create credentials = (%q, %q), want generated relay credentials", fakeRelay.createCredentialLogin, fakeRelay.createCredentialPassword)
	}

	reloaded, err := client.User.Get(ctx, localUser.ID)
	if err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if reloaded.RelayAuthPassword == nil || strings.TrimSpace(*reloaded.RelayAuthPassword) == "" {
		t.Fatal("expected generated relay password to be stored")
	}
	decrypted, err := pkg.Decrypt(*reloaded.RelayAuthPassword, encryptionKey)
	if err != nil {
		t.Fatalf("Decrypt() unexpected error: %v", err)
	}
	if decrypted != updateReq.Password {
		t.Fatalf("stored relay password does not match generated password")
	}
}

func TestCreateGroupCredentialRotatesRelayPasswordForExistingRelaySSOUserWithoutStoredRelayPassword(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	encryptionKey := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

	provider := client.RelayProvider.Create().
		SetName("sub2api").
		SetDisplayName("sub2api").
		SetBaseURL("https://relay.example.com/").
		SetAdminAPIKey("test-admin-key").
		SetDefaultModel("gpt-5.4").
		SetIsPrimary(true).
		SetEnabled(true).
		SaveX(ctx)

	localUser := client.User.Create().
		SetUsername("alice@example.com").
		SetEmail("alice@example.com").
		SetAuthSource(user.AuthSourceRelaySSO).
		SetRole(user.RoleUser).
		SetRelayUserID(1).
		SaveX(ctx)

	fakeRelay := &fakeRelayProvider{
		keysByUser: map[int64][]relay.APIKey{1: {}},
		usersByID: map[int64]relay.User{
			1: {ID: 1, Email: "alice@example.com", Username: "alice@example.com", Role: "user"},
		},
		createResult: &relay.APIKeyWithSecret{
			APIKey: relay.APIKey{
				ID:     711,
				UserID: 1,
				Name:   "alice",
				Status: "active",
				Group:  &relay.Group{ID: 42, Name: "Group Alpha", Platform: "openai"},
			},
			Secret: "sk-openai",
		},
	}

	svc := usersetup.NewService(client, usersetup.ProviderResolverFunc(func(_ context.Context, providerID int) (relay.Provider, error) {
		return fakeRelay, nil
	}), encryptionKey)

	got, err := svc.CreateGroupCredential(ctx, usersetup.CreateGroupCredentialRequest{
		UserID:     localUser.ID,
		ProviderID: provider.ID,
		GroupID:    "42",
	})
	if err != nil {
		t.Fatalf("CreateGroupCredential() unexpected error: %v", err)
	}
	if got.Secret != "sk-openai" {
		t.Fatalf("secret = %q, want sk-openai", got.Secret)
	}
	updateReq, ok := fakeRelay.updatedUsers[1]
	if !ok || strings.TrimSpace(updateReq.Password) == "" {
		t.Fatalf("expected generated relay password update, got %+v", fakeRelay.updatedUsers)
	}
	if fakeRelay.createCredentialLogin != "alice@example.com" || fakeRelay.createCredentialPassword != updateReq.Password {
		t.Fatalf("create credentials = (%q, %q), want generated relay credentials", fakeRelay.createCredentialLogin, fakeRelay.createCredentialPassword)
	}
}

func TestCreateGroupCredentialRetriesWithRotatedPasswordWhenStoredRelayPasswordIsInvalid(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	encryptionKey := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	encryptedPassword, err := pkg.Encrypt("stale-password", encryptionKey)
	if err != nil {
		t.Fatalf("Encrypt() unexpected error: %v", err)
	}

	provider := client.RelayProvider.Create().
		SetName("sub2api").
		SetDisplayName("sub2api").
		SetBaseURL("https://relay.example.com/").
		SetAdminAPIKey("test-admin-key").
		SetDefaultModel("gpt-5.4").
		SetIsPrimary(true).
		SetEnabled(true).
		SaveX(ctx)

	localUser := client.User.Create().
		SetUsername("alice@example.com").
		SetEmail("alice@example.com").
		SetAuthSource(user.AuthSourceLdap).
		SetRole(user.RoleUser).
		SetRelayUserID(1).
		SetRelayAuthPassword(encryptedPassword).
		SaveX(ctx)

	fakeRelay := &fakeRelayProvider{
		keysByUser:             map[int64][]relay.APIKey{1: {}},
		requireUpdatedPassword: true,
		usersByID: map[int64]relay.User{
			1: {ID: 1, Email: "alice@example.com", Username: "alice@example.com", Role: "user"},
		},
		createResult: &relay.APIKeyWithSecret{
			APIKey: relay.APIKey{
				ID:     711,
				UserID: 1,
				Name:   "alice",
				Status: "active",
				Group:  &relay.Group{ID: 42, Name: "Group Alpha", Platform: "openai"},
			},
			Secret: "sk-openai",
		},
	}

	svc := usersetup.NewService(client, usersetup.ProviderResolverFunc(func(_ context.Context, providerID int) (relay.Provider, error) {
		return fakeRelay, nil
	}), encryptionKey)

	got, err := svc.CreateGroupCredential(ctx, usersetup.CreateGroupCredentialRequest{
		UserID:     localUser.ID,
		ProviderID: provider.ID,
		GroupID:    "42",
	})
	if err != nil {
		t.Fatalf("CreateGroupCredential() unexpected error: %v", err)
	}
	if got.Secret != "sk-openai" {
		t.Fatalf("secret = %q, want sk-openai", got.Secret)
	}
	updateReq, ok := fakeRelay.updatedUsers[1]
	if !ok || strings.TrimSpace(updateReq.Password) == "" || updateReq.Password == "stale-password" {
		t.Fatalf("expected stale password to be rotated, got %+v", fakeRelay.updatedUsers)
	}
	if fakeRelay.createCredentialLogin != "alice@example.com" || fakeRelay.createCredentialPassword != updateReq.Password {
		t.Fatalf("create credentials = (%q, %q), want rotated relay credentials", fakeRelay.createCredentialLogin, fakeRelay.createCredentialPassword)
	}

	reloaded, err := client.User.Get(ctx, localUser.ID)
	if err != nil {
		t.Fatalf("reload user: %v", err)
	}
	decrypted, err := pkg.Decrypt(*reloaded.RelayAuthPassword, encryptionKey)
	if err != nil {
		t.Fatalf("Decrypt() unexpected error: %v", err)
	}
	if decrypted != updateReq.Password {
		t.Fatalf("stored relay password does not match rotated password")
	}
}

func TestCreateGroupCredentialRotatesRelayPasswordForRelayAdminUserWithoutStoredRelayPassword(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	encryptionKey := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

	provider := client.RelayProvider.Create().
		SetName("sub2api").
		SetDisplayName("sub2api").
		SetBaseURL("https://relay.example.com/").
		SetAdminAPIKey("test-admin-key").
		SetDefaultModel("gpt-5.4").
		SetIsPrimary(true).
		SetEnabled(true).
		SaveX(ctx)

	localUser := client.User.Create().
		SetUsername("alice@example.com").
		SetEmail("alice@example.com").
		SetAuthSource(user.AuthSourceLdap).
		SetRole(user.RoleUser).
		SetRelayUserID(1).
		SaveX(ctx)

	fakeRelay := &fakeRelayProvider{
		keysByUser: map[int64][]relay.APIKey{1: {}},
		usersByID: map[int64]relay.User{
			1: {ID: 1, Email: "alice@example.com", Username: "alice@example.com", Role: "admin"},
		},
		createResult: &relay.APIKeyWithSecret{
			APIKey: relay.APIKey{
				ID:     721,
				UserID: 1,
				Name:   "alice",
				Status: "active",
				Group:  &relay.Group{ID: 42, Name: "Group Alpha", Platform: "openai"},
			},
			Secret: "sk-openai",
		},
	}

	svc := usersetup.NewService(client, usersetup.ProviderResolverFunc(func(_ context.Context, providerID int) (relay.Provider, error) {
		return fakeRelay, nil
	}), encryptionKey)

	got, err := svc.CreateGroupCredential(ctx, usersetup.CreateGroupCredentialRequest{
		UserID:     localUser.ID,
		ProviderID: provider.ID,
		GroupID:    "42",
	})
	if err != nil {
		t.Fatalf("CreateGroupCredential() unexpected error: %v", err)
	}
	if got.Secret != "sk-openai" {
		t.Fatalf("secret = %q, want sk-openai", got.Secret)
	}
	updateReq, ok := fakeRelay.updatedUsers[1]
	if !ok || strings.TrimSpace(updateReq.Password) == "" {
		t.Fatalf("expected generated relay password update, got %+v", fakeRelay.updatedUsers)
	}
	if fakeRelay.createCredentialLogin != "alice@example.com" || fakeRelay.createCredentialPassword != updateReq.Password {
		t.Fatalf("create credentials = (%q, %q), want generated relay credentials", fakeRelay.createCredentialLogin, fakeRelay.createCredentialPassword)
	}
	reloaded, err := client.User.Get(ctx, localUser.ID)
	if err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if reloaded.RelayAuthPassword == nil || strings.TrimSpace(*reloaded.RelayAuthPassword) == "" {
		t.Fatalf("expected generated relay password to be stored locally")
	}
	decrypted, err := pkg.Decrypt(*reloaded.RelayAuthPassword, encryptionKey)
	if err != nil {
		t.Fatalf("Decrypt() unexpected error: %v", err)
	}
	if decrypted != updateReq.Password {
		t.Fatalf("stored relay password does not match generated password")
	}
}

func TestCreateGroupCredentialUsesStoredRelayPasswordForRelayAdminUser(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	encryptionKey := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	encryptedPassword, err := pkg.Encrypt("sso-password", encryptionKey)
	if err != nil {
		t.Fatalf("Encrypt() unexpected error: %v", err)
	}

	provider := client.RelayProvider.Create().
		SetName("sub2api").
		SetDisplayName("sub2api").
		SetBaseURL("https://relay.example.com/").
		SetAdminAPIKey("test-admin-key").
		SetDefaultModel("gpt-5.4").
		SetIsPrimary(true).
		SetEnabled(true).
		SaveX(ctx)

	localUser := client.User.Create().
		SetUsername("alice@example.com").
		SetEmail("alice@example.com").
		SetAuthSource(user.AuthSourceLdap).
		SetRole(user.RoleAdmin).
		SetRelayUserID(1).
		SetRelayAuthPassword(encryptedPassword).
		SaveX(ctx)

	fakeRelay := &fakeRelayProvider{
		keysByUser: map[int64][]relay.APIKey{1: {}},
		usersByID: map[int64]relay.User{
			1: {ID: 1, Email: "alice@example.com", Username: "alice@example.com", Role: "admin"},
		},
		createResult: &relay.APIKeyWithSecret{
			APIKey: relay.APIKey{
				ID:     722,
				UserID: 1,
				Name:   "alice",
				Status: "active",
				Group:  &relay.Group{ID: 42, Name: "Group Alpha", Platform: "openai"},
			},
			Secret: "sk-openai",
		},
	}

	svc := usersetup.NewService(client, usersetup.ProviderResolverFunc(func(_ context.Context, providerID int) (relay.Provider, error) {
		return fakeRelay, nil
	}), encryptionKey)

	got, err := svc.CreateGroupCredential(ctx, usersetup.CreateGroupCredentialRequest{
		UserID:     localUser.ID,
		ProviderID: provider.ID,
		GroupID:    "42",
	})
	if err != nil {
		t.Fatalf("CreateGroupCredential() unexpected error: %v", err)
	}
	if got.Secret != "sk-openai" {
		t.Fatalf("secret = %q, want sk-openai", got.Secret)
	}
	if len(fakeRelay.updatedUsers) != 0 {
		t.Fatalf("relay admin user password must not be repaired, got updates: %+v", fakeRelay.updatedUsers)
	}
	if fakeRelay.createCredentialLogin != "alice@example.com" || fakeRelay.createCredentialPassword != "sso-password" {
		t.Fatalf("create credentials = (%q, %q), want stored SSO credentials", fakeRelay.createCredentialLogin, fakeRelay.createCredentialPassword)
	}
	reloaded, err := client.User.Get(ctx, localUser.ID)
	if err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if reloaded.RelayAuthPassword == nil || strings.TrimSpace(*reloaded.RelayAuthPassword) == "" {
		t.Fatalf("relay admin user stored password must be preserved")
	}
	decrypted, err := pkg.Decrypt(*reloaded.RelayAuthPassword, encryptionKey)
	if err != nil {
		t.Fatalf("Decrypt() unexpected error: %v", err)
	}
	if decrypted != "sso-password" {
		t.Fatalf("stored relay password = %q, want sso-password", decrypted)
	}
}

func TestRegenerateGroupCredentialUsesStoredRelayPasswordForRelayAdminUser(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	encryptionKey := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	encryptedPassword, err := pkg.Encrypt("sso-password", encryptionKey)
	if err != nil {
		t.Fatalf("Encrypt() unexpected error: %v", err)
	}

	provider := client.RelayProvider.Create().
		SetName("sub2api").
		SetDisplayName("sub2api").
		SetBaseURL("https://relay.example.com/").
		SetAdminAPIKey("test-admin-key").
		SetDefaultModel("gpt-5.4").
		SetIsPrimary(true).
		SetEnabled(true).
		SaveX(ctx)

	localUser := client.User.Create().
		SetUsername("alice@example.com").
		SetEmail("alice@example.com").
		SetAuthSource(user.AuthSourceLdap).
		SetRole(user.RoleAdmin).
		SetRelayUserID(1).
		SetRelayAuthPassword(encryptedPassword).
		SaveX(ctx)

	fakeRelay := &fakeRelayProvider{
		keysByUser: map[int64][]relay.APIKey{
			1: {
				{ID: 801, UserID: 1, Name: "alice", Status: "active", Group: &relay.Group{ID: 42, Name: "Group Alpha", Platform: "openai"}, CreatedAt: time.Now()},
			},
		},
		usersByID: map[int64]relay.User{
			1: {ID: 1, Email: "alice@example.com", Username: "alice@example.com", Role: "admin"},
		},
		createResult: &relay.APIKeyWithSecret{
			APIKey: relay.APIKey{ID: 802, UserID: 1, Name: "alice", Status: "active", Group: &relay.Group{ID: 42, Name: "Group Alpha", Platform: "openai"}},
			Secret: "sk-regenerated",
		},
	}

	svc := usersetup.NewService(client, usersetup.ProviderResolverFunc(func(_ context.Context, providerID int) (relay.Provider, error) {
		return fakeRelay, nil
	}), encryptionKey)

	got, err := svc.RegenerateGroupCredential(ctx, usersetup.RegenerateGroupCredentialRequest{
		UserID:     localUser.ID,
		ProviderID: provider.ID,
		GroupID:    "42",
	})
	if err != nil {
		t.Fatalf("RegenerateGroupCredential() unexpected error: %v", err)
	}
	if got.Secret != "sk-regenerated" {
		t.Fatalf("secret = %q, want sk-regenerated", got.Secret)
	}
	if len(fakeRelay.updatedUsers) != 0 {
		t.Fatalf("relay admin user password must not be repaired, got updates: %+v", fakeRelay.updatedUsers)
	}
	if fakeRelay.createCredentialLogin != "alice@example.com" || fakeRelay.createCredentialPassword != "sso-password" {
		t.Fatalf("create credentials = (%q, %q), want stored SSO credentials", fakeRelay.createCredentialLogin, fakeRelay.createCredentialPassword)
	}
	if fakeRelay.updateCredentialLogin != "alice@example.com" || fakeRelay.updateCredentialPassword != "sso-password" {
		t.Fatalf("update credentials = (%q, %q), want stored SSO credentials", fakeRelay.updateCredentialLogin, fakeRelay.updateCredentialPassword)
	}
	if diff := cmp.Diff(map[int64]string{801: "inactive"}, fakeRelay.updatedStatuses); diff != "" {
		t.Fatalf("updated statuses mismatch (-want +got):\n%s", diff)
	}
	reloaded, err := client.User.Get(ctx, localUser.ID)
	if err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if reloaded.RelayAuthPassword == nil || strings.TrimSpace(*reloaded.RelayAuthPassword) == "" {
		t.Fatalf("relay admin user stored password must be preserved")
	}
	decrypted, err := pkg.Decrypt(*reloaded.RelayAuthPassword, encryptionKey)
	if err != nil {
		t.Fatalf("Decrypt() unexpected error: %v", err)
	}
	if decrypted != "sso-password" {
		t.Fatalf("stored relay password = %q, want sso-password", decrypted)
	}
}

func TestRegenerateGroupCredentialOnlyTouchesSelectedGroup(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	encryptionKey := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	encryptedPassword, err := pkg.Encrypt("test-password", encryptionKey)
	if err != nil {
		t.Fatalf("Encrypt() unexpected error: %v", err)
	}

	provider := client.RelayProvider.Create().
		SetName("sub2api").
		SetDisplayName("sub2api").
		SetBaseURL("https://relay.example.com/").
		SetAdminAPIKey("test-admin-key").
		SetDefaultModel("gpt-5.4").
		SetIsPrimary(true).
		SetEnabled(true).
		SaveX(ctx)

	localUser := client.User.Create().
		SetUsername("alice@example.com").
		SetEmail("alice@example.com").
		SetAuthSource(user.AuthSourceRelaySSO).
		SetRole(user.RoleAdmin).
		SetRelayUserID(1).
		SetRelayAuthPassword(encryptedPassword).
		SaveX(ctx)

	fakeRelay := &fakeRelayProvider{
		keysByUser: map[int64][]relay.APIKey{
			1: {
				{ID: 801, UserID: 1, Name: "alice", Status: "active", Group: &relay.Group{ID: 42, Name: "Group Alpha", Platform: "openai"}, CreatedAt: time.Now().Add(-time.Hour)},
				{ID: 802, UserID: 1, Name: "alice", Status: "active", Group: &relay.Group{ID: 43, Name: "Group Beta", Platform: "openai"}, CreatedAt: time.Now()},
			},
		},
		createResult: &relay.APIKeyWithSecret{
			APIKey: relay.APIKey{ID: 803, UserID: 1, Name: "alice", Status: "active", Group: &relay.Group{ID: 42, Name: "Group Alpha", Platform: "openai"}},
			Secret: "sk-regenerated",
		},
		listAllowedGroupsForUserFn: func(_ context.Context, userID int64) ([]relay.Group, error) {
			return []relay.Group{
				{ID: 42, Name: "Group Alpha", Platform: "openai"},
				{ID: 43, Name: "Group Beta", Platform: "openai"},
			}, nil
		},
	}

	svc := usersetup.NewService(client, usersetup.ProviderResolverFunc(func(_ context.Context, providerID int) (relay.Provider, error) {
		return fakeRelay, nil
	}), encryptionKey)

	got, err := svc.RegenerateGroupCredential(ctx, usersetup.RegenerateGroupCredentialRequest{
		UserID:     localUser.ID,
		ProviderID: provider.ID,
		GroupID:    "42",
	})
	if err != nil {
		t.Fatalf("RegenerateGroupCredential() unexpected error: %v", err)
	}
	if diff := cmp.Diff(map[int64]string{801: "inactive"}, fakeRelay.updatedStatuses); diff != "" {
		t.Fatalf("updated statuses mismatch (-want +got):\n%s", diff)
	}
	if fakeRelay.updateCredentialLogin != "alice@example.com" || fakeRelay.updateCredentialPassword != "test-password" {
		t.Fatalf("update credentials = (%q, %q), want (%q, %q)", fakeRelay.updateCredentialLogin, fakeRelay.updateCredentialPassword, "alice@example.com", "test-password")
	}
	if got.Secret != "sk-regenerated" {
		t.Fatalf("secret = %q, want sk-regenerated", got.Secret)
	}
	if fakeRelay.lastCreateReq.GroupID != "42" {
		t.Fatalf("create group id = %q, want 42", fakeRelay.lastCreateReq.GroupID)
	}
}

func TestRegenerateGroupCredentialRotatesRelayPasswordForExistingLDAPUserWithoutStoredRelayPassword(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	encryptionKey := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

	provider := client.RelayProvider.Create().
		SetName("sub2api").
		SetDisplayName("sub2api").
		SetBaseURL("https://relay.example.com/").
		SetAdminAPIKey("test-admin-key").
		SetDefaultModel("gpt-5.4").
		SetIsPrimary(true).
		SetEnabled(true).
		SaveX(ctx)

	localUser := client.User.Create().
		SetUsername("alice@example.com").
		SetEmail("alice@example.com").
		SetAuthSource(user.AuthSourceLdap).
		SetRole(user.RoleUser).
		SetRelayUserID(1).
		SaveX(ctx)

	fakeRelay := &fakeRelayProvider{
		keysByUser: map[int64][]relay.APIKey{
			1: {
				{ID: 801, UserID: 1, Name: "alice", Status: "active", Group: &relay.Group{ID: 42, Name: "Group Alpha", Platform: "openai"}, CreatedAt: time.Now()},
			},
		},
		usersByID: map[int64]relay.User{
			1: {ID: 1, Email: "alice@example.com", Username: "alice@example.com", Role: "user"},
		},
		createResult: &relay.APIKeyWithSecret{
			APIKey: relay.APIKey{ID: 802, UserID: 1, Name: "alice", Status: "active", Group: &relay.Group{ID: 42, Name: "Group Alpha", Platform: "openai"}},
			Secret: "sk-regenerated",
		},
	}

	svc := usersetup.NewService(client, usersetup.ProviderResolverFunc(func(_ context.Context, providerID int) (relay.Provider, error) {
		return fakeRelay, nil
	}), encryptionKey)

	got, err := svc.RegenerateGroupCredential(ctx, usersetup.RegenerateGroupCredentialRequest{
		UserID:     localUser.ID,
		ProviderID: provider.ID,
		GroupID:    "42",
	})
	if err != nil {
		t.Fatalf("RegenerateGroupCredential() unexpected error: %v", err)
	}
	if got.Secret != "sk-regenerated" {
		t.Fatalf("secret = %q, want sk-regenerated", got.Secret)
	}
	updateReq, ok := fakeRelay.updatedUsers[1]
	if !ok || strings.TrimSpace(updateReq.Password) == "" {
		t.Fatalf("expected generated relay password update, got %+v", fakeRelay.updatedUsers)
	}
	if diff := cmp.Diff(map[int64]string{801: "inactive"}, fakeRelay.updatedStatuses); diff != "" {
		t.Fatalf("updated statuses mismatch (-want +got):\n%s", diff)
	}
	if fakeRelay.createCredentialLogin != "alice@example.com" || fakeRelay.createCredentialPassword != updateReq.Password {
		t.Fatalf("create credentials = (%q, %q), want generated relay credentials", fakeRelay.createCredentialLogin, fakeRelay.createCredentialPassword)
	}
	if fakeRelay.updateCredentialLogin != "alice@example.com" || fakeRelay.updateCredentialPassword != updateReq.Password {
		t.Fatalf("update credentials = (%q, %q), want generated relay credentials", fakeRelay.updateCredentialLogin, fakeRelay.updateCredentialPassword)
	}

	reloaded, err := client.User.Get(ctx, localUser.ID)
	if err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if reloaded.RelayAuthPassword == nil || strings.TrimSpace(*reloaded.RelayAuthPassword) == "" {
		t.Fatal("expected generated relay password to be stored")
	}
	decrypted, err := pkg.Decrypt(*reloaded.RelayAuthPassword, encryptionKey)
	if err != nil {
		t.Fatalf("Decrypt() unexpected error: %v", err)
	}
	if decrypted != updateReq.Password {
		t.Fatalf("stored relay password does not match generated password")
	}
}

func groupIDs(groups []usersetup.GroupCredentialSummary) []string {
	out := make([]string, 0, len(groups))
	for _, group := range groups {
		out = append(out, group.GroupID)
	}
	return out
}
