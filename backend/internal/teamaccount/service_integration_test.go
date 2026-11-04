package teamaccount

import (
	"context"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/ai-efficiency/backend/internal/representativescope"
	"github.com/ai-efficiency/backend/internal/testdb"
)

type fakeProvider struct {
	relay.Provider
	created       []relay.CreateUserRequest
	subscriptions map[int64]map[int64]bool
	revokedKeys   []int64
	disabled      []int64
	nextRelayID   int64
	keySecret     string
	usageTokens   int64
	usageCost     float64
}

func newFakeProvider() *fakeProvider {
	return &fakeProvider{subscriptions: map[int64]map[int64]bool{}, nextRelayID: 900, keySecret: "sk-team-secret-value-0123456789"}
}

func (f *fakeProvider) CreateUser(_ context.Context, req relay.CreateUserRequest) (*relay.User, error) {
	f.created = append(f.created, req)
	f.nextRelayID++
	return &relay.User{ID: f.nextRelayID, Username: req.Username, Email: req.Email}, nil
}

func (f *fakeProvider) CreateUserAPIKey(_ context.Context, _ int64, req relay.APIKeyCreateRequest) (*relay.APIKeyWithSecret, error) {
	return &relay.APIKeyWithSecret{APIKey: relay.APIKey{ID: 555, Name: req.Name}, Secret: f.keySecret}, nil
}

func (f *fakeProvider) RevokeUserAPIKey(_ context.Context, keyID int64) error {
	f.revokedKeys = append(f.revokedKeys, keyID)
	return nil
}

func (f *fakeProvider) GetUsageStats(_ context.Context, _ int64, _, _ time.Time) (*relay.UsageStats, error) {
	return &relay.UsageStats{TotalTokens: f.usageTokens, TotalCost: f.usageCost}, nil
}

func (f *fakeProvider) ListUsageLogsByAPIKeyExact(_ context.Context, _ int64, _, _ time.Time) ([]relay.UsageLog, error) {
	return []relay.UsageLog{{TotalTokens: 10, TotalCost: 0.5, CreatedAt: time.Now()}}, nil
}

func (f *fakeProvider) AssignSubscriptionForUser(_ context.Context, userID, groupID int64, _ int) error {
	if f.subscriptions[userID] == nil {
		f.subscriptions[userID] = map[int64]bool{}
	}
	f.subscriptions[userID][groupID] = true
	return nil
}

func (f *fakeProvider) ListUserSubscriptions(_ context.Context, _ int64) ([]relay.UserSubscription, error) {
	// Provider defaults that were never requested by the department.
	return []relay.UserSubscription{{GroupID: 2}, {GroupID: 4}, {GroupID: 10}}, nil
}

func (f *fakeProvider) RemoveSubscriptionForUser(_ context.Context, userID, groupID int64) error {
	delete(f.subscriptions[userID], groupID)
	return nil
}

func (f *fakeProvider) DisableUser(_ context.Context, userID int64) error {
	f.disabled = append(f.disabled, userID)
	return nil
}

type fakeScope struct{ departments []string }

func (s fakeScope) Resolve(context.Context, int) (*representativescope.Scope, error) {
	return &representativescope.Scope{IsRepresentative: len(s.departments) > 0, RepresentedDepartmentIDs: s.departments}, nil
}

type fakeResolver struct{ provider *fakeProvider }

func (r fakeResolver) Resolve(context.Context, int) (relay.Provider, error) { return r.provider, nil }

func seedUsers(t *testing.T, client *ent.Client) (int, int) {
	t.Helper()
	ctx := context.Background()
	owner, err := client.User.Create().SetUsername("owner").SetEmail("owner@example.com").SetAuthSource("ldap").Save(ctx)
	if err != nil {
		t.Fatalf("create owner: %v", err)
	}
	other, err := client.User.Create().SetUsername("other").SetEmail("other@example.com").SetAuthSource("ldap").Save(ctx)
	if err != nil {
		t.Fatalf("create other: %v", err)
	}
	return owner.ID, other.ID
}

func seedProvider(t *testing.T, client *ent.Client) int {
	t.Helper()
	row, err := client.RelayProvider.Create().
		SetName("primary").
		SetDisplayName("Primary").
		SetBaseURL("https://relay.example.com").
		SetRelayType("sub2api").
		SetAdminAPIKey("admin-key").
		SetDefaultModel("deepseek-flash").
		SetIsPrimary(true).
		SetEnabled(true).
		SetConfigurationVersion(1).
		Save(context.Background())
	if err != nil {
		t.Fatalf("seed provider: %v", err)
	}
	return row.ID
}

func seedMapping(t *testing.T, client *ent.Client, providerID int, departmentID string, groupID int64) {
	t.Helper()
	if _, err := client.DepartmentGroupMap.Create().
		SetDepartmentExternalID(departmentID).
		SetRelayProviderID(providerID).
		SetRelayGroupID(groupID).
		Save(context.Background()); err != nil {
		t.Fatalf("seed mapping: %v", err)
	}
}

func TestCreateProvisionsRelayIdentityAndOwnership(t *testing.T) {
	client := testdb.Open(t)
	ownerID, _ := seedUsers(t, client)
	provider := newFakeProvider()
	providerID := seedProvider(t, client)
	seedMapping(t, client, providerID, "department-alpha", 10)

	service := New(client, fakeResolver{provider}, fakeScope{departments: []string{"department-alpha"}}, "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", nil)
	account, err := service.Create(context.Background(), CreateInput{Name: "Alpha Team", ActorUserID: ownerID})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if account.DepartmentExternalID != "department-alpha" {
		t.Errorf("department = %q", account.DepartmentExternalID)
	}
	if account.OwnerUserID != ownerID {
		t.Errorf("owner = %d, want %d", account.OwnerUserID, ownerID)
	}
	if len(provider.created) != 1 {
		t.Fatalf("relay users created = %d", len(provider.created))
	}
	req := provider.created[0]
	if req.Username != "team-department-alpha" {
		t.Errorf("username = %q", req.Username)
	}
	if req.Email != "team-department-alpha@invalid.account" {
		t.Errorf("email = %q", req.Email)
	}
	if len(req.AllowedGroups) != 1 || req.AllowedGroups[0] != 10 {
		t.Errorf("allowed groups = %v", req.AllowedGroups)
	}

	subs := provider.subscriptions[account.RelayUserID]
	if !subs[10] {
		t.Errorf("mapped group subscription missing: %v", subs)
	}
	for _, defaultGroup := range []int64{2, 4} {
		if subs[defaultGroup] {
			t.Errorf("default group %d was not revoked: %v", defaultGroup, subs)
		}
	}

	if account.Managers[0].Role != "owner" {
		t.Errorf("owner manager row missing: %+v", account.Managers)
	}

	if _, err := service.Create(context.Background(), CreateInput{Name: "Alpha Again", ActorUserID: ownerID}); err == nil {
		t.Fatal("second account for the same department must fail")
	}
}

func TestCreateRequiresRepresentative(t *testing.T) {
	client := testdb.Open(t)
	ownerID, _ := seedUsers(t, client)
	provider := newFakeProvider()
	providerID := seedProvider(t, client)
	seedMapping(t, client, providerID, "department-alpha", 10)

	service := New(client, fakeResolver{provider}, fakeScope{}, "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", nil)
	if _, err := service.Create(context.Background(), CreateInput{Name: "Alpha", ActorUserID: ownerID}); err == nil {
		t.Fatal("non-representative must not create an account")
	}
	if len(provider.created) != 0 {
		t.Fatalf("relay user created for a rejected request")
	}
}

func TestTransferRemovesPreviousOwner(t *testing.T) {
	client := testdb.Open(t)
	ownerID, otherID := seedUsers(t, client)
	provider := newFakeProvider()
	providerID := seedProvider(t, client)
	seedMapping(t, client, providerID, "department-alpha", 10)

	service := New(client, fakeResolver{provider}, fakeScope{departments: []string{"department-alpha"}}, "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", nil)
	account, err := service.Create(context.Background(), CreateInput{Name: "Alpha", ActorUserID: ownerID})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if err := service.Transfer(context.Background(), ownerID, false, account.ID, otherID); err != nil {
		t.Fatalf("Transfer() error = %v", err)
	}
	updated, err := service.Get(context.Background(), otherID, false, account.ID)
	if err != nil {
		t.Fatalf("Get() as new owner error = %v", err)
	}
	if updated.OwnerUserID != otherID {
		t.Errorf("owner = %d, want %d", updated.OwnerUserID, otherID)
	}
	for _, manager := range updated.Managers {
		if manager.UserID == ownerID {
			t.Fatalf("previous owner still has access: %+v", updated.Managers)
		}
	}
	if _, err := service.Get(context.Background(), ownerID, false, account.ID); err == nil {
		t.Fatal("previous owner must lose access immediately")
	}
}

func TestTransferRejectsNonOwnerManager(t *testing.T) {
	client := testdb.Open(t)
	ownerID, otherID := seedUsers(t, client)
	provider := newFakeProvider()
	providerID := seedProvider(t, client)
	seedMapping(t, client, providerID, "department-alpha", 10)
	service := New(client, fakeResolver{provider}, fakeScope{departments: []string{"department-alpha"}}, "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", nil)
	account, err := service.Create(context.Background(), CreateInput{Name: "Alpha", ActorUserID: ownerID})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if err := service.AddManager(context.Background(), ownerID, false, account.ID, otherID); err != nil {
		t.Fatalf("AddManager() error = %v", err)
	}
	if err := service.Transfer(context.Background(), otherID, false, account.ID, otherID); err == nil {
		t.Fatal("a plain manager must not transfer ownership")
	}
}
