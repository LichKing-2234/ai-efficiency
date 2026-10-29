package relayplanning

import (
	"context"
	"testing"

	"github.com/ai-efficiency/backend/internal/relay"
)

type accountConfigurationListProvider struct {
	relay.Provider
	accounts []relay.Account
	records  map[int64]relay.AccountConfigurationRecord
}

func (p accountConfigurationListProvider) ListAccountsForPlatform(context.Context, string) ([]relay.Account, error) {
	return p.accounts, nil
}

func (p accountConfigurationListProvider) ReadAccountConfigurations(context.Context, string) (map[int64]relay.AccountConfigurationRecord, error) {
	return p.records, nil
}

func intPtr(value int) *int {
	return &value
}

// TestListAccountConfigurationsCarriesResetCreditCounts keeps the display-only
// reset-credit count on the page item. The count is rendered and filtered by the
// copy page; it must travel with the account even when no baseline is chosen, and
// an account without a cached snapshot must stay unknown instead of defaulting to
// zero.
func TestListAccountConfigurationsCarriesResetCreditCounts(t *testing.T) {
	provider := accountConfigurationListProvider{
		accounts: []relay.Account{
			{ID: 42, Name: "Account Alpha", Platform: "openai", Type: "oauth"},
			{ID: 43, Name: "Account Beta", Platform: "openai", Type: "oauth"},
		},
		records: map[int64]relay.AccountConfigurationRecord{
			42: {
				Configuration:         relay.AccountConfiguration{ModelMapping: map[string]string{"gpt-5.5": "gpt-6.1-sol"}},
				ResetCreditsAvailable: intPtr(2),
			},
			43: {
				Configuration: relay.AccountConfiguration{ModelMapping: map[string]string{"gpt-5.5": "gpt-6.1-sol"}},
			},
		},
	}
	service := NewService(nil, relayPlanningProviderResolver(func(context.Context, int) (relay.Provider, error) {
		return provider, nil
	}), nil)

	page, err := service.ListAccountConfigurations(context.Background(), ListAccountConfigurationsRequest{
		ProviderID:        7,
		Platform:          "openai",
		BaselineAccountID: 42,
	})
	if err != nil {
		t.Fatalf("ListAccountConfigurations() error = %v", err)
	}
	if page.Total != 2 || len(page.Items) != 2 {
		t.Fatalf("page total/items = %d/%d, want 2/2", page.Total, len(page.Items))
	}
	if page.Baseline == nil || len(page.Baseline.ModelMapping) != 1 {
		t.Fatalf("baseline configuration = %+v, want the baseline account's model mapping", page.Baseline)
	}

	itemsByID := make(map[int64]AccountConfigurationItem, len(page.Items))
	for _, item := range page.Items {
		itemsByID[item.Account.ID] = item
	}

	alpha, ok := itemsByID[42]
	if !ok {
		t.Fatalf("account 42 is missing from the page")
	}
	if alpha.ResetCreditsAvailable == nil || *alpha.ResetCreditsAvailable != 2 {
		t.Fatalf("account 42 reset credits = %v, want 2", alpha.ResetCreditsAvailable)
	}
	if alpha.Diff == nil || !alpha.Diff.Matches {
		t.Fatalf("account 42 diff = %+v, want a baseline match", alpha.Diff)
	}

	beta, ok := itemsByID[43]
	if !ok {
		t.Fatalf("account 43 is missing from the page")
	}
	if beta.ResetCreditsAvailable != nil {
		t.Fatalf("account 43 reset credits = %v, want unknown (nil)", *beta.ResetCreditsAvailable)
	}
}
