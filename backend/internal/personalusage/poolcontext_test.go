package personalusage

import (
	"context"
	"sync"
	"testing"

	"github.com/ai-efficiency/backend/internal/relay"
)

// poolContextProviderStub serves the two provider-wide reads the managed-pool
// directory needs, plus the per-user group list and pool metrics.
type poolContextProviderStub struct {
	relay.Provider
	mu          sync.Mutex
	userLoads   int
	accounts    []relay.Account
	users       []relay.User
	activeGroup map[int64][]int64
	pool        relay.UserUsageGroupPoolUsageState
}

func (s *poolContextProviderStub) ListAllowedGroupsForUser(context.Context, int64) ([]relay.Group, error) {
	return []relay.Group{{ID: 42, Name: "Group Alpha", Platform: "openai"}}, nil
}

func (s *poolContextProviderStub) ReadGroupOAuthPoolUsage(context.Context, []int64) (relay.UserUsageGroupPoolUsageState, error) {
	return s.pool, nil
}

func (s *poolContextProviderStub) ListAccountsForPlatform(_ context.Context, platform string) ([]relay.Account, error) {
	if platform != "openai" {
		return nil, nil
	}
	return s.accounts, nil
}

func (s *poolContextProviderStub) ListUsersWithActiveSubscriptions(context.Context) ([]relay.User, map[int64][]int64, error) {
	s.mu.Lock()
	s.userLoads++
	s.mu.Unlock()
	return s.users, s.activeGroup, nil
}

func (s *poolContextProviderStub) loadCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.userLoads
}

func newPoolContextStub() *poolContextProviderStub {
	return &poolContextProviderStub{
		accounts: []relay.Account{
			{ID: 2, Name: "second@example.com", Platform: "openai", Type: "oauth", Status: "active", PlanType: "prolite", GroupRelationships: []relay.AccountGroupRelationship{{GroupID: 42, Priority: 2}}},
			{ID: 1, Name: "upstream@example.com", Platform: "openai", Type: "oauth", Status: "active", PlanType: "pro", GroupRelationships: []relay.AccountGroupRelationship{{GroupID: 42, Priority: 1}}},
			// Not an OAuth account: the pool metric never counts it.
			{ID: 3, Name: "apikey@example.com", Platform: "openai", Type: "api_key", Status: "active", GroupRelationships: []relay.AccountGroupRelationship{{GroupID: 42, Priority: 3}}},
			// Another platform group that no mapping owns.
			{ID: 4, Name: "unmanaged@example.com", Platform: "openai", Type: "oauth", Status: "active", PlanType: "pro", GroupRelationships: []relay.AccountGroupRelationship{{GroupID: 99, Priority: 1}}},
		},
		users: []relay.User{
			{ID: 100, Email: "alice@example.com"},
			{ID: 101, Email: "bob@example.org"},
			{ID: 102, Email: "carol@example.net"},
			{ID: 103, Email: "  "},
		},
		activeGroup: map[int64][]int64{
			100: {42},
			101: {42, 99},
			102: {99},
			103: {42},
		},
		pool: relay.UserUsageGroupPoolUsageState{
			Status: "ok",
			Groups: []relay.UserUsageGroupPoolUsageGroupItem{{
				GroupID:                  "42",
				Status:                   "ok",
				AverageWeeklyUtilization: 13.8,
				ValidOAuthAccounts:       2,
				TotalActiveOAuthAccounts: 2,
			}},
		},
	}
}

func poolContextRequest(userID int) Request {
	return Request{
		UserID: userID,
		Params: relay.UserUsageDashboardParams{StartDate: "2026-07-01", EndDate: "2026-07-15", Granularity: "day"},
	}
}

func TestGroupPoolUsageAttachesManagedPoolAccountsAndMembers(t *testing.T) {
	service, _, userID, _ := createPersonalUsageFixture(t, true)
	stub := newPoolContextStub()
	service.resolver.(*providerResolverStub).provider = stub

	providerRow, err := service.client.RelayProvider.Query().Only(context.Background())
	if err != nil {
		t.Fatalf("load provider: %v", err)
	}
	if _, err := service.client.RelayGroupMapping.Create().
		SetProviderID(providerRow.ID).
		SetDepartmentExternalID("dept-alpha").
		SetDepartmentName("Department Alpha").
		SetPlatform("openai").
		SetGroupIds([]int64{42}).
		Save(context.Background()); err != nil {
		t.Fatalf("create mapping: %v", err)
	}

	response, err := service.GroupPoolUsage(context.Background(), poolContextRequest(userID))
	if err != nil {
		t.Fatalf("GroupPoolUsage() error = %v", err)
	}
	if len(response.PoolUsage.Groups) != 1 {
		t.Fatalf("pool groups = %+v", response.PoolUsage.Groups)
	}
	group := response.PoolUsage.Groups[0]

	wantAccounts := []relay.UserUsageGroupPoolUsageAccount{
		{ID: 1, Name: "upstream@example.com", PlanType: "pro"},
		{ID: 2, Name: "second@example.com", PlanType: "prolite"},
	}
	if len(group.Accounts) != len(wantAccounts) {
		t.Fatalf("accounts = %+v, want %+v", group.Accounts, wantAccounts)
	}
	for index, want := range wantAccounts {
		if group.Accounts[index] != want {
			t.Fatalf("account %d = %+v, want %+v", index, group.Accounts[index], want)
		}
	}

	wantMembers := []string{"alice@example.com", "bob@example.org"}
	if len(group.Members) != len(wantMembers) {
		t.Fatalf("members = %+v, want %+v", group.Members, wantMembers)
	}
	for index, want := range wantMembers {
		if group.Members[index] != want {
			t.Fatalf("member %d = %q, want %q", index, group.Members[index], want)
		}
	}
}

func TestGroupPoolUsageSkipsTheDirectoryForUnmanagedGroups(t *testing.T) {
	service, _, userID, _ := createPersonalUsageFixture(t, true)
	stub := newPoolContextStub()
	// The mapped group is 43; the pool row reports 42, which no mapping owns.
	stub.pool.Groups[0].GroupID = "44"
	service.resolver.(*providerResolverStub).provider = stub

	providerRow, err := service.client.RelayProvider.Query().Only(context.Background())
	if err != nil {
		t.Fatalf("load provider: %v", err)
	}
	if _, err := service.client.RelayGroupMapping.Create().
		SetProviderID(providerRow.ID).
		SetDepartmentExternalID("dept-alpha").
		SetPlatform("openai").
		SetGroupIds([]int64{43}).
		Save(context.Background()); err != nil {
		t.Fatalf("create mapping: %v", err)
	}

	response, err := service.GroupPoolUsage(context.Background(), poolContextRequest(userID))
	if err != nil {
		t.Fatalf("GroupPoolUsage() error = %v", err)
	}
	group := response.PoolUsage.Groups[0]
	if len(group.Accounts) != 0 || len(group.Members) != 0 {
		t.Fatalf("unmanaged group leaked pool context: %+v", group)
	}
	if got := stub.loadCount(); got != 0 {
		t.Fatalf("provider user loads = %d, want 0 for an unmanaged pool", got)
	}
}

func TestGroupPoolUsageCachesTheDirectoryWithinItsTTL(t *testing.T) {
	service, _, userID, _ := createPersonalUsageFixture(t, true)
	stub := newPoolContextStub()
	service.resolver.(*providerResolverStub).provider = stub

	providerRow, err := service.client.RelayProvider.Query().Only(context.Background())
	if err != nil {
		t.Fatalf("load provider: %v", err)
	}
	if _, err := service.client.RelayGroupMapping.Create().
		SetProviderID(providerRow.ID).
		SetDepartmentExternalID("dept-alpha").
		SetPlatform("openai").
		SetGroupIds([]int64{42}).
		Save(context.Background()); err != nil {
		t.Fatalf("create mapping: %v", err)
	}

	for attempt := 0; attempt < 2; attempt++ {
		if _, err := service.GroupPoolUsage(context.Background(), poolContextRequest(userID)); err != nil {
			t.Fatalf("GroupPoolUsage() attempt %d error = %v", attempt, err)
		}
	}
	if got := stub.loadCount(); got != 1 {
		t.Fatalf("provider user loads = %d, want 1", got)
	}
}
