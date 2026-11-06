package teamaccount

import (
	"context"
	"errors"
	"fmt"
	"strings"
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
	keySecret       string
	keyAuthFailures int
	keyCalls        int
	usageTokens     int64
	usageCost     float64
}

func newFakeProvider() *fakeProvider {
	return &fakeProvider{subscriptions: map[int64]map[int64]bool{}, nextRelayID: 900, keySecret: "sk-team-secret-value-0123456789"}
}

// defaults mirrors the provider behaviour: every new Relay user is subscribed
// to Codex, Agent, Claude and China automatically.
func (f *fakeProvider) seedDefaults(relayUserID int64) {
	if f.subscriptions[relayUserID] == nil {
		f.subscriptions[relayUserID] = map[int64]bool{}
	}
	for _, group := range []int64{2, 4, 10, 12} {
		f.subscriptions[relayUserID][group] = true
	}
}

func (f *fakeProvider) CreateUser(_ context.Context, req relay.CreateUserRequest) (*relay.User, error) {
	f.created = append(f.created, req)
	f.nextRelayID++
	f.seedDefaults(f.nextRelayID)
	return &relay.User{ID: f.nextRelayID, Username: req.Username, Email: req.Email}, nil
}

func (f *fakeProvider) CreateUserAPIKey(_ context.Context, _ int64, req relay.APIKeyCreateRequest) (*relay.APIKeyWithSecret, error) {
	f.keyCalls++
	if f.keyAuthFailures > 0 {
		f.keyAuthFailures--
		return nil, fmt.Errorf("relay: create api key via jwt: relay: invalid credentials")
	}
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

func (f *fakeProvider) ListUserSubscriptions(_ context.Context, relayUserID int64) ([]relay.UserSubscription, error) {
	out := make([]relay.UserSubscription, 0, len(f.subscriptions[relayUserID]))
	for groupID := range f.subscriptions[relayUserID] {
		out = append(out, relay.UserSubscription{GroupID: groupID, Status: "active"})
	}
	return out, nil
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


func seedDepartmentMember(t *testing.T, client *ent.Client, departmentID, email string) {
	t.Helper()
	ctx := context.Background()
	source, err := client.DirectorySource.Create().
		SetName("test-directory").
		SetDsl("{}").
		Save(ctx)
	if err != nil {
		t.Fatalf("seed directory source: %v", err)
	}
	if _, err := client.DirectoryMember.Create().
		SetSourceID(source.ID).
		SetExternalID(email).
		SetEmailNormalized(email).
		SetDepartmentExternalID(departmentID).
		SetStatus("active").
		SetLastSeenRunID(1).
		Save(ctx); err != nil {
		t.Fatalf("seed department member: %v", err)
	}
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

func TestCreateProvisionsRelayIdentityAndOwnership(t *testing.T) {
	client := testdb.Open(t)
	ownerID, _ := seedUsers(t, client)
	provider := newFakeProvider()
	seedProvider(t, client)

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
	if len(req.AllowedGroups) != 0 {
		t.Errorf("new accounts must not pin an allow-list, got %v", req.AllowedGroups)
	}

	subs := provider.subscriptions[account.RelayUserID]
	for _, defaultGroup := range []int64{2, 4, 10, 12} {
		if !subs[defaultGroup] {
			t.Errorf("default group %d missing after provisioning: %v", defaultGroup, subs)
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
	seedProvider(t, client)

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
	seedProvider(t, client)
	seedDepartmentMember(t, client, "department-alpha", "other@example.com")

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
	seedProvider(t, client)
	seedDepartmentMember(t, client, "department-alpha", "other@example.com")
	service := New(client, fakeResolver{provider}, fakeScope{departments: []string{"department-alpha"}}, "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", nil)
	account, err := service.Create(context.Background(), CreateInput{Name: "Alpha", ActorUserID: ownerID})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, err := service.AddManager(context.Background(), ownerID, false, account.ID, otherID); err != nil {
		t.Fatalf("AddManager() error = %v", err)
	}
	if err := service.Transfer(context.Background(), otherID, false, account.ID, otherID); err == nil {
		t.Fatal("a plain manager must not transfer ownership")
	}
}

func TestAdditionalGroupsRespectCreatorSubscriptions(t *testing.T) {
	client := testdb.Open(t)
	ownerID, _ := seedUsers(t, client)
	seedProvider(t, client)
	provider := newFakeProvider()
	// The creator's own account holds China (10) and Claude (4) only.
	creatorRelayID := int64(2001)
	if _, err := client.User.UpdateOneID(ownerID).SetRelayUserID(int(creatorRelayID)).Save(context.Background()); err != nil {
		t.Fatalf("bind creator relay identity: %v", err)
	}
	provider.subscriptions[creatorRelayID] = map[int64]bool{4: true, 10: true}
	service := New(client, fakeResolver{provider}, fakeScope{departments: []string{"department-alpha"}}, "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", nil)

	if _, err := service.Create(context.Background(), CreateInput{
		Name: "Alpha", ActorUserID: ownerID, AdditionalGroupIDs: []int64{10},
	}); err != nil {
		t.Fatalf("creator may grant a group they hold: %v", err)
	}
	accounts, err := service.List(context.Background(), ownerID, false)
	if err != nil || len(accounts) != 1 {
		t.Fatalf("list accounts: %v %d", err, len(accounts))
	}
	if !provider.subscriptions[accounts[0].RelayUserID][10] {
		t.Fatalf("additional group was not subscribed: %v", provider.subscriptions[accounts[0].RelayUserID])
	}
	if !provider.subscriptions[accounts[0].RelayUserID][2] {
		t.Fatalf("default subscriptions must stay: %v", provider.subscriptions[accounts[0].RelayUserID])
	}
}

func TestAdditionalGroupsRejectGroupsTheCreatorDoesNotHold(t *testing.T) {
	client := testdb.Open(t)
	ownerID, _ := seedUsers(t, client)
	seedProvider(t, client)
	provider := newFakeProvider()
	creatorRelayID := int64(2002)
	if _, err := client.User.UpdateOneID(ownerID).SetRelayUserID(int(creatorRelayID)).Save(context.Background()); err != nil {
		t.Fatalf("bind creator relay identity: %v", err)
	}
	provider.subscriptions[creatorRelayID] = map[int64]bool{10: true}
	service := New(client, fakeResolver{provider}, fakeScope{departments: []string{"department-alpha"}}, "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", nil)

	_, err := service.Create(context.Background(), CreateInput{
		Name: "Alpha", ActorUserID: ownerID, AdditionalGroupIDs: []int64{25},
	})
	if err == nil {
		t.Fatal("granting a group the creator does not hold must fail")
	}
	var permission *PermissionError
	if !errors.As(err, &permission) {
		t.Fatalf("expected a permission error, got %v", err)
	}
	if len(provider.created) != 0 {
		t.Fatal("relay user must not be created for a rejected grant")
	}
}

func TestAdminMayGrantAnyGroup(t *testing.T) {
	client := testdb.Open(t)
	adminID, _ := seedUsers(t, client)
	seedProvider(t, client)
	provider := newFakeProvider()
	service := New(client, fakeResolver{provider}, fakeScope{}, "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", nil)

	account, err := service.Create(context.Background(), CreateInput{
		Name: "Alpha", DepartmentExternalID: "department-alpha", ActorUserID: adminID, IsAdmin: true,
		AdditionalGroupIDs: []int64{25},
	})
	if err != nil {
		t.Fatalf("admin grant failed: %v", err)
	}
	if !provider.subscriptions[account.RelayUserID][25] {
		t.Fatalf("admin grant missing: %v", provider.subscriptions[account.RelayUserID])
	}
}

func TestCreateKeyRejectsGroupsWithoutSubscription(t *testing.T) {
	client := testdb.Open(t)
	ownerID, _ := seedUsers(t, client)
	seedProvider(t, client)
	provider := newFakeProvider()
	service := New(client, fakeResolver{provider}, fakeScope{departments: []string{"department-alpha"}}, "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", nil)
	account, err := service.Create(context.Background(), CreateInput{Name: "Alpha", ActorUserID: ownerID})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if _, _, err := service.CreateKey(context.Background(), ownerID, false, account.ID, 10, "china"); err != nil {
		t.Fatalf("subscribed group must be allowed: %v", err)
	}
	if _, _, err := service.CreateKey(context.Background(), ownerID, false, account.ID, 25, "not-subscribed"); err == nil {
		t.Fatal("unsubscribed group must be rejected")
	}
}

func TestRepresentativeCannotTransferOutsideDepartment(t *testing.T) {
	client := testdb.Open(t)
	ownerID, otherID := seedUsers(t, client)
	provider := newFakeProvider()
	seedProvider(t, client)
	seedDepartmentMember(t, client, "department-alpha", "owner@example.com")
	service := New(client, fakeResolver{provider}, fakeScope{departments: []string{"department-alpha"}}, "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", nil)
	account, err := service.Create(context.Background(), CreateInput{Name: "Alpha", ActorUserID: ownerID})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	err = service.Transfer(context.Background(), ownerID, false, account.ID, otherID)
	var permission *PermissionError
	if !errors.As(err, &permission) {
		t.Fatalf("Transfer() error = %v, want PermissionError", err)
	}
}

func TestRepresentativeCannotAddManagerOutsideDepartment(t *testing.T) {
	client := testdb.Open(t)
	ownerID, otherID := seedUsers(t, client)
	provider := newFakeProvider()
	seedProvider(t, client)
	seedDepartmentMember(t, client, "department-alpha", "owner@example.com")
	service := New(client, fakeResolver{provider}, fakeScope{departments: []string{"department-alpha"}}, "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", nil)
	account, err := service.Create(context.Background(), CreateInput{Name: "Alpha", ActorUserID: ownerID})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	_, err = service.AddManager(context.Background(), ownerID, false, account.ID, otherID)
	var permission *PermissionError
	if !errors.As(err, &permission) {
		t.Fatalf("AddManager() error = %v, want PermissionError", err)
	}
}

func TestGroupOptionsRequireRepresentativeRole(t *testing.T) {
	client := testdb.Open(t)
	userID, _ := seedUsers(t, client)
	provider := newFakeProvider()
	seedProvider(t, client)
	service := New(client, fakeResolver{provider}, fakeScope{departments: nil}, "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", nil)

	_, err := service.GroupOptionsForActor(context.Background(), userID, false, 0)
	var permission *PermissionError
	if !errors.As(err, &permission) {
		t.Fatalf("GroupOptionsForActor() error = %v, want PermissionError", err)
	}
}

func TestCreateKeyRetriesAfterPasswordRotation(t *testing.T) {
	client := testdb.Open(t)
	ownerID, _ := seedUsers(t, client)
	provider := newFakeProvider()
	provider.keyAuthFailures = 1
	seedProvider(t, client)
	service := New(client, fakeResolver{provider}, fakeScope{departments: []string{"department-alpha"}}, "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", nil)
	account, err := service.Create(context.Background(), CreateInput{Name: "Alpha", ActorUserID: ownerID})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	key, secret, err := service.CreateKey(context.Background(), ownerID, false, account.ID, 10, "ci")
	if err != nil {
		t.Fatalf("CreateKey() error = %v", err)
	}
	if key == nil || strings.TrimSpace(secret) == "" {
		t.Fatalf("key = %+v secret = %q", key, secret)
	}
	if provider.keyCalls != 2 {
		t.Fatalf("relay key calls = %d, want 2 (one retry)", provider.keyCalls)
	}
}

func TestAddManagerIsIdempotent(t *testing.T) {
	client := testdb.Open(t)
	ownerID, otherID := seedUsers(t, client)
	provider := newFakeProvider()
	seedProvider(t, client)
	seedDepartmentMember(t, client, "department-alpha", "other@example.com")
	service := New(client, fakeResolver{provider}, fakeScope{departments: []string{"department-alpha"}}, "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", nil)
	account, err := service.Create(context.Background(), CreateInput{Name: "Alpha", ActorUserID: ownerID})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	added, err := service.AddManager(context.Background(), ownerID, false, account.ID, otherID)
	if err != nil || !added {
		t.Fatalf("first AddManager() = %v, %v, want added", added, err)
	}
	added, err = service.AddManager(context.Background(), ownerID, false, account.ID, otherID)
	if err != nil || added {
		t.Fatalf("second AddManager() = %v, %v, want not added", added, err)
	}
	rows, err := client.TeamAccountManager.Query().All(context.Background())
	if err != nil {
		t.Fatalf("list managers: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("manager rows = %d, want owner + manager only", len(rows))
	}
	events, err := client.TeamAccountEvent.Query().All(context.Background())
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	addedEvents := 0
	for _, event := range events {
		if event.Action == "manager_added" {
			addedEvents++
		}
	}
	if addedEvents != 1 {
		t.Fatalf("manager_added events = %d, want 1", addedEvents)
	}
}

func TestReplacementAccountAfterSuspension(t *testing.T) {
	client := testdb.Open(t)
	ownerID, _ := seedUsers(t, client)
	provider := newFakeProvider()
	seedProvider(t, client)
	service := New(client, fakeResolver{provider}, fakeScope{departments: []string{"department-alpha"}}, "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", nil)

	first, err := service.Create(context.Background(), CreateInput{Name: "Alpha", ActorUserID: ownerID})
	if err != nil {
		t.Fatalf("Create() first error = %v", err)
	}
	if err := service.SetStatus(context.Background(), ownerID, first.ID, "suspended"); err != nil {
		t.Fatalf("SetStatus(suspended) error = %v", err)
	}

	second, err := service.Create(context.Background(), CreateInput{Name: "Alpha replacement", ActorUserID: ownerID})
	if err != nil {
		t.Fatalf("Create() replacement error = %v", err)
	}
	if second.RelayUsername == first.RelayUsername {
		t.Fatalf("replacement reused relay identity %q", second.RelayUsername)
	}

	if _, err := service.Create(context.Background(), CreateInput{Name: "Third", ActorUserID: ownerID}); err == nil {
		t.Fatal("Create() while another account is active must fail")
	}
	if err := service.SetStatus(context.Background(), ownerID, first.ID, "active"); err == nil {
		t.Fatal("restoring the suspended account while another is active must fail")
	}
}

func TestSuspendedAccountHiddenFromMembers(t *testing.T) {
	client := testdb.Open(t)
	ownerID, otherID := seedUsers(t, client)
	provider := newFakeProvider()
	seedProvider(t, client)
	seedDepartmentMember(t, client, "department-alpha", "other@example.com")
	service := New(client, fakeResolver{provider}, fakeScope{departments: []string{"department-alpha"}}, "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", nil)
	account, err := service.Create(context.Background(), CreateInput{Name: "Alpha", ActorUserID: ownerID})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, err := service.AddManager(context.Background(), ownerID, false, account.ID, otherID); err != nil {
		t.Fatalf("AddManager() error = %v", err)
	}
	if err := service.SetStatus(context.Background(), ownerID, account.ID, "suspended"); err != nil {
		t.Fatalf("SetStatus() error = %v", err)
	}

	accounts, err := service.List(context.Background(), ownerID, false)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(accounts) != 0 {
		t.Fatalf("suspended account leaked into member list: %+v", accounts)
	}
	_, err = service.Get(context.Background(), otherID, false, account.ID)
	var missing *NotFoundError
	if !errors.As(err, &missing) {
		t.Fatalf("Get() error = %v, want NotFoundError", err)
	}
	if _, err := service.Get(context.Background(), ownerID, true, account.ID); err != nil {
		t.Fatalf("admin Get() error = %v", err)
	}
}
