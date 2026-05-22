package usersetup_test

import (
	"context"
	"errors"
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
	createResult               *relay.APIKeyWithSecret
	createErr                  error
	updatedStatuses            map[int64]string
	updateCredentialLogin      string
	updateCredentialPassword   string
	lastCreateReq              relay.APIKeyCreateRequest
	listAllowedGroupsForUserFn func(ctx context.Context, userID int64) ([]relay.Group, error)
}

func (f *fakeRelayProvider) Ping(ctx context.Context) error { return nil }
func (f *fakeRelayProvider) Name() string                   { return "fake-relay" }
func (f *fakeRelayProvider) Authenticate(ctx context.Context, username, password string) (*relay.User, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeRelayProvider) GetUser(ctx context.Context, userID int64) (*relay.User, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeRelayProvider) ListAllowedGroupsForUser(ctx context.Context, userID int64) ([]relay.Group, error) {
	if f.listAllowedGroupsForUserFn != nil {
		return f.listAllowedGroupsForUserFn(ctx, userID)
	}
	return nil, nil
}
func (f *fakeRelayProvider) FindUserByEmail(ctx context.Context, email string) (*relay.User, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeRelayProvider) FindUserByUsername(ctx context.Context, username string) (*relay.User, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeRelayProvider) CreateUser(ctx context.Context, req relay.CreateUserRequest) (*relay.User, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeRelayProvider) UpdateUser(ctx context.Context, userID int64, req relay.UpdateUserRequest) (*relay.User, error) {
	return nil, errors.New("not implemented")
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
	if keys, ok := f.keysByUser[userID]; ok {
		cloned := make([]relay.APIKey, len(keys))
		copy(cloned, keys)
		return cloned, nil
	}
	return nil, nil
}
func (f *fakeRelayProvider) CreateUserAPIKey(ctx context.Context, userID int64, req relay.APIKeyCreateRequest) (*relay.APIKeyWithSecret, error) {
	f.lastCreateReq = req
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

func TestListProvidersReturnsOnlyAllowedGroups(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)

	provider := client.RelayProvider.Create().
		SetName("sub2api").
		SetDisplayName("sub2api").
		SetBaseURL("https://sub2api.agoraio.cn/").
		SetAdminURL("https://sub2api.agoraio.cn/").
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

	provider := client.RelayProvider.Create().
		SetName("sub2api").
		SetDisplayName("sub2api").
		SetBaseURL("https://sub2api.agoraio.cn/").
		SetAdminURL("https://sub2api.agoraio.cn/").
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
		keysByUser: map[int64][]relay.APIKey{1: {}},
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
	}), "d98460dc58409c713d1586802217c23932d58c95479641e4b0fec1c740386696")

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
		SetBaseURL("https://sub2api.agoraio.cn/").
		SetAdminURL("https://sub2api.agoraio.cn/").
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

func groupIDs(groups []usersetup.GroupCredentialSummary) []string {
	out := make([]string, 0, len(groups))
	for _, group := range groups {
		out = append(out, group.GroupID)
	}
	return out
}
