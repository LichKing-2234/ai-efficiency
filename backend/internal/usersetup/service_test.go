package usersetup_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/ent/user"
	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/ai-efficiency/backend/internal/testdb"
	"github.com/ai-efficiency/backend/internal/usersetup"
	"github.com/google/go-cmp/cmp"
)

type fakeRelayProvider struct {
	keysByUser    map[int64][]relay.APIKey
	createResult  *relay.APIKeyWithSecret
	createErr     error
	revokeErr     error
	revokedKeyIDs []int64
}

func (f *fakeRelayProvider) Ping(ctx context.Context) error { return nil }
func (f *fakeRelayProvider) Name() string                   { return "fake-relay" }
func (f *fakeRelayProvider) Authenticate(ctx context.Context, username, password string) (*relay.User, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeRelayProvider) GetUser(ctx context.Context, userID int64) (*relay.User, error) {
	return nil, errors.New("not implemented")
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
	if f.createErr != nil {
		return nil, f.createErr
	}
	return f.createResult, nil
}
func (f *fakeRelayProvider) UpdateUserAPIKeyStatus(ctx context.Context, keyID int64, status string) error {
	return errors.New("not implemented")
}
func (f *fakeRelayProvider) RevokeUserAPIKey(ctx context.Context, keyID int64) error {
	if f.revokeErr != nil {
		return f.revokeErr
	}
	f.revokedKeyIDs = append(f.revokedKeyIDs, keyID)
	return nil
}
func (f *fakeRelayProvider) ListUsageLogsByAPIKeyExact(ctx context.Context, apiKeyID int64, from, to time.Time) ([]relay.UsageLog, error) {
	return nil, errors.New("not implemented")
}

func TestListProvidersReturnsMissingWhenNoManagedKeyExists(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)

	provider := client.RelayProvider.Create().
		SetName("sub2api-prod").
		SetDisplayName("sub2api Production").
		SetBaseURL("https://relay.example.com").
		SetAdminURL("https://relay-admin.example.com").
		SetAdminAPIKey("test-admin-key").
		SetDefaultModel("claude-sonnet-4-20250514").
		SetIsPrimary(true).
		SetEnabled(true).
		SaveX(ctx)

	relayID := 101
	localUser := client.User.Create().
		SetUsername("alice").
		SetEmail("alice@example.com").
		SetAuthSource(user.AuthSourceSub2apiSSO).
		SetRole(user.RoleUser).
		SetRelayUserID(relayID).
		SaveX(ctx)

	fakeRelay := &fakeRelayProvider{
		keysByUser: map[int64][]relay.APIKey{
			int64(relayID): {},
		},
	}

	svc := usersetup.NewService(client, usersetup.ProviderResolverFunc(func(ctx context.Context, providerID int) (relay.Provider, error) {
		if providerID != provider.ID {
			t.Fatalf("providerID = %d, want %d", providerID, provider.ID)
		}
		return fakeRelay, nil
	}))

	resp, err := svc.ListProviders(ctx, usersetup.ListProvidersRequest{UserID: localUser.ID})
	if err != nil {
		t.Fatalf("ListProviders() unexpected error: %v", err)
	}
	if len(resp.Providers) != 1 {
		t.Fatalf("providers len = %d, want 1", len(resp.Providers))
	}
	if resp.Providers[0].ManagedKey.State != "missing" {
		t.Fatalf("managed key state = %q, want missing", resp.Providers[0].ManagedKey.State)
	}
}

func TestListProvidersReturnsExistingHiddenForLatestActiveManagedKey(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	provider := client.RelayProvider.Create().
		SetName("sub2api-prod").
		SetDisplayName("sub2api Production").
		SetBaseURL("https://relay.example.com").
		SetAdminURL("https://relay-admin.example.com").
		SetAdminAPIKey("test-admin-key").
		SetDefaultModel("claude-sonnet-4-20250514").
		SetIsPrimary(true).
		SetEnabled(true).
		SaveX(ctx)
	relayID := 101
	localUser := client.User.Create().
		SetUsername("alice").
		SetEmail("alice@example.com").
		SetAuthSource(user.AuthSourceSub2apiSSO).
		SetRole(user.RoleUser).
		SetRelayUserID(relayID).
		SaveX(ctx)

	oldTime := time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC)
	newTime := time.Date(2026, 5, 21, 10, 0, 0, 0, time.UTC)
	fakeRelay := &fakeRelayProvider{
		keysByUser: map[int64][]relay.APIKey{
			int64(relayID): {
				{ID: 10, UserID: int64(relayID), Name: "ae-cli-auto", Status: "active", CreatedAt: oldTime},
				{ID: 11, UserID: int64(relayID), Name: "manual-key", Status: "active", CreatedAt: newTime},
				{ID: 12, UserID: int64(relayID), Name: "ae-cli-auto", Status: "active", CreatedAt: newTime},
			},
		},
	}

	svc := usersetup.NewService(client, usersetup.ProviderResolverFunc(func(ctx context.Context, providerID int) (relay.Provider, error) {
		if providerID != provider.ID {
			t.Fatalf("providerID = %d, want %d", providerID, provider.ID)
		}
		return fakeRelay, nil
	}))

	resp, err := svc.ListProviders(ctx, usersetup.ListProvidersRequest{UserID: localUser.ID})
	if err != nil {
		t.Fatalf("ListProviders() unexpected error: %v", err)
	}
	got := resp.Providers[0].ManagedKey
	if got.State != "existing_hidden" {
		t.Fatalf("managed key state = %q, want existing_hidden", got.State)
	}
	if got.APIKeyID != 12 {
		t.Fatalf("api key id = %d, want 12", got.APIKeyID)
	}
}

func TestCreateManagedKeyRejectsExistingManagedKey(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	provider := client.RelayProvider.Create().
		SetName("sub2api-prod").
		SetDisplayName("sub2api Production").
		SetBaseURL("https://relay.example.com").
		SetAdminURL("https://relay-admin.example.com").
		SetAdminAPIKey("test-admin-key").
		SetDefaultModel("claude-sonnet-4-20250514").
		SetIsPrimary(true).
		SetEnabled(true).
		SaveX(ctx)
	relayID := 101
	localUser := client.User.Create().
		SetUsername("alice").
		SetEmail("alice@example.com").
		SetAuthSource(user.AuthSourceSub2apiSSO).
		SetRole(user.RoleUser).
		SetRelayUserID(relayID).
		SaveX(ctx)

	fakeRelay := &fakeRelayProvider{
		keysByUser: map[int64][]relay.APIKey{
			int64(relayID): {
				{ID: 22, UserID: int64(relayID), Name: "ae-cli-auto", Status: "active", CreatedAt: time.Now()},
			},
		},
	}

	svc := usersetup.NewService(client, usersetup.ProviderResolverFunc(func(ctx context.Context, providerID int) (relay.Provider, error) {
		return fakeRelay, nil
	}))

	_, err := svc.CreateManagedKey(ctx, usersetup.CreateManagedKeyRequest{UserID: localUser.ID, ProviderID: provider.ID})
	if !errors.Is(err, usersetup.ErrManagedKeyAlreadyExists) {
		t.Fatalf("CreateManagedKey() error = %v, want ErrManagedKeyAlreadyExists", err)
	}
}

func TestRegenerateManagedKeyRevokesAllActiveManagedKeysAndCreatesANewOne(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	provider := client.RelayProvider.Create().
		SetName("sub2api-prod").
		SetDisplayName("sub2api Production").
		SetBaseURL("https://relay.example.com").
		SetAdminURL("https://relay-admin.example.com").
		SetAdminAPIKey("test-admin-key").
		SetDefaultModel("claude-sonnet-4-20250514").
		SetIsPrimary(true).
		SetEnabled(true).
		SaveX(ctx)
	relayID := 101
	localUser := client.User.Create().
		SetUsername("alice").
		SetEmail("alice@example.com").
		SetAuthSource(user.AuthSourceSub2apiSSO).
		SetRole(user.RoleUser).
		SetRelayUserID(relayID).
		SaveX(ctx)

	fakeRelay := &fakeRelayProvider{
		keysByUser: map[int64][]relay.APIKey{
			int64(relayID): {
				{ID: 30, UserID: int64(relayID), Name: "ae-cli-auto", Status: "active", CreatedAt: time.Now().Add(-time.Hour)},
				{ID: 31, UserID: int64(relayID), Name: "ae-cli-auto", Status: "active", CreatedAt: time.Now()},
			},
		},
		createResult: &relay.APIKeyWithSecret{
			APIKey: relay.APIKey{ID: 99, UserID: int64(relayID), Name: "ae-cli-auto", Status: "active"},
			Secret: "sk-new-managed-key",
		},
	}

	svc := usersetup.NewService(client, usersetup.ProviderResolverFunc(func(ctx context.Context, providerID int) (relay.Provider, error) {
		return fakeRelay, nil
	}))

	got, err := svc.RegenerateManagedKey(ctx, usersetup.RegenerateManagedKeyRequest{UserID: localUser.ID, ProviderID: provider.ID})
	if err != nil {
		t.Fatalf("RegenerateManagedKey() unexpected error: %v", err)
	}
	if diff := cmp.Diff([]int64{31, 30}, fakeRelay.revokedKeyIDs); diff != "" {
		t.Fatalf("revoked ids mismatch (-want +got):\n%s", diff)
	}
	if got.Secret != "sk-new-managed-key" {
		t.Fatalf("secret = %q, want sk-new-managed-key", got.Secret)
	}
}
