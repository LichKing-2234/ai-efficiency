package relayplanning_test

import (
	"context"
	"testing"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/ai-efficiency/backend/internal/relayplanning"
	"github.com/ai-efficiency/backend/internal/testdb"
)

// accountPriorityProvider serves the reviewed platform Accounts. Relay holds the
// applied precedence, so the reviewed numbers never reach it from here.
type accountPriorityProvider struct {
	relay.Provider
	accounts []relay.Account
}

func (p accountPriorityProvider) ListAccountsForPlatform(context.Context, string) ([]relay.Account, error) {
	return p.accounts, nil
}

type accountPriorityResolver struct {
	provider relay.Provider
}

func (r accountPriorityResolver) Resolve(context.Context, int) (relay.Provider, error) {
	return r.provider, nil
}

func newAccountPriorityMapping(ctx context.Context, t *testing.T, client *ent.Client, providerID int) int {
	t.Helper()
	row := client.RelayGroupMapping.Create().
		SetProviderID(providerID).
		SetDepartmentExternalID("dept-account-priority").
		SetDepartmentName("Department Account Priority").
		SetPlatform("openai").
		SetTemplateGroupID(10).
		SetTemplateGroupName("Template").
		SetGroupIds([]int64{20}).
		SetAccountManagementInitialized(true).
		SetDesiredAccounts(map[string][]map[string]int64{
			"20": {{"account_id": 26, "priority": 1}, {"account_id": 35, "priority": 2}},
		}).
		SaveX(ctx)
	return row.ID
}

// TestSaveDesiredAccountsStoresEveryAccountAtEqualPriority covers the reviewed
// Account model: Accounts sharing a Target carry no scheduling preference over
// each other, so a reviewed pool of more than one Account is stored at priority
// 1 instead of the old contiguous 1..N numbering. Relay priorities are one
// positional ordering per Account, so the reviewed number was never the applied
// precedence anyway.
func TestSaveDesiredAccountsStoresEveryAccountAtEqualPriority(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	providerRow := client.RelayProvider.Create().
		SetName("relay-account-priority").
		SetDisplayName("Relay Account Priority").
		SetBaseURL("https://relay.example.com").
		SetAdminAPIKey("test-admin-key").
		SaveX(ctx)
	mappingID := newAccountPriorityMapping(ctx, t, client, providerRow.ID)

	provider := accountPriorityProvider{accounts: []relay.Account{
		{ID: 26, Platform: "openai"},
		{ID: 35, Platform: "openai"},
	}}
	service := relayplanning.NewService(client, accountPriorityResolver{provider: provider}, nil)

	// The reviewed pool arrives with distinct priorities, as the browser still
	// numbers its rows, and must be stored without preference. The previous
	// contiguous-priority validation accepted this shape only because the numbers
	// happened to be unique.
	mapping, err := service.SaveDesiredAccounts(ctx, mappingID, map[string][]relayplanning.AccountIntent{
		"20": {{AccountID: 26, Priority: 1}, {AccountID: 35, Priority: 2}},
	})
	if err != nil {
		t.Fatalf("SaveDesiredAccounts() error = %v", err)
	}
	assertEqualAccountPriorities(t, mapping.DesiredAccounts["20"])

	// Equal reviewed priorities must persist too: rejecting duplicates here is
	// what made a fully equal pool unwritable.
	mapping, err = service.SaveDesiredAccounts(ctx, mappingID, map[string][]relayplanning.AccountIntent{
		"20": {{AccountID: 26, Priority: 1}, {AccountID: 35, Priority: 1}},
	})
	if err != nil {
		t.Fatalf("SaveDesiredAccounts() with equal reviewed priorities error = %v", err)
	}
	assertEqualAccountPriorities(t, mapping.DesiredAccounts["20"])
}

// TestLoadMappingRoundTripsEqualAccountPriorities proves the stored equal
// priorities survive a fresh read, which is the state drift detection compares
// against Relay.
func TestLoadMappingRoundTripsEqualAccountPriorities(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	providerRow := client.RelayProvider.Create().
		SetName("relay-account-priority-roundtrip").
		SetDisplayName("Relay Account Priority Roundtrip").
		SetBaseURL("https://relay.example.com").
		SetAdminAPIKey("test-admin-key").
		SaveX(ctx)
	mappingID := newAccountPriorityMapping(ctx, t, client, providerRow.ID)

	provider := accountPriorityProvider{accounts: []relay.Account{
		{ID: 26, Platform: "openai"},
		{ID: 35, Platform: "openai"},
	}}
	service := relayplanning.NewService(client, accountPriorityResolver{provider: provider}, nil)
	if _, err := service.SaveDesiredAccounts(ctx, mappingID, map[string][]relayplanning.AccountIntent{
		"20": {{AccountID: 26, Priority: 1}, {AccountID: 35, Priority: 2}},
	}); err != nil {
		t.Fatalf("SaveDesiredAccounts() error = %v", err)
	}

	loaded, err := service.GetMapping(ctx, mappingID)
	if err != nil {
		t.Fatalf("GetMapping() error = %v", err)
	}
	assertEqualAccountPriorities(t, loaded.DesiredAccounts["20"])
}

func assertEqualAccountPriorities(t *testing.T, intents []relayplanning.AccountIntent) {
	t.Helper()
	if len(intents) != 2 {
		t.Fatalf("desired accounts = %#v, want both accounts", intents)
	}
	for _, intent := range intents {
		if intent.Priority != 1 {
			t.Fatalf("priority for account %d = %d, want 1", intent.AccountID, intent.Priority)
		}
	}
}
