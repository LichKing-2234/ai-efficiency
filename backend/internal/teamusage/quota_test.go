package teamusage

import "testing"

func TestRawActualCostDisplayKeepsEnforcementUsedAndQuota(t *testing.T) {
	row := BuildSubscriptionRow(SubscriptionInput{
		GroupID:                 "42",
		GroupName:               "Group Alpha",
		GroupDefaultMultiplier:  floatPtr(1.0),
		SystemDefaultMultiplier: 1.0,
		UserMultiplier:          floatPtr(2.0),
		MonthlyLimitUSD:         floatPtr(500.0),
		MonthlyUsageUSD:         80.0,
		UsageValueBasis:         "raw_actual_cost",
	})
	if row.MonthlyDisplayUsedUSD != 80 || row.MonthlyEffectiveAllowanceUSD == nil || *row.MonthlyEffectiveAllowanceUSD != 500 {
		t.Fatalf("display used/quota = %.2f / %#v, want 80 / 500", row.MonthlyDisplayUsedUSD, row.MonthlyEffectiveAllowanceUSD)
	}
}

func TestDoesNotDoubleNormalizeDisplayCost(t *testing.T) {
	row := BuildSubscriptionRow(SubscriptionInput{
		GroupID:                 "42",
		GroupName:               "Group Alpha",
		GroupDefaultMultiplier:  floatPtr(1.0),
		SystemDefaultMultiplier: 1.0,
		UserMultiplier:          floatPtr(2.0),
		MonthlyLimitUSD:         floatPtr(250.0),
		MonthlyUsageUSD:         40.0,
		UsageValueBasis:         "normalized_display_cost",
	})
	if row.MonthlyDisplayUsedUSD != 40 || row.MonthlyEffectiveAllowanceUSD == nil || *row.MonthlyEffectiveAllowanceUSD != 250 {
		t.Fatalf("display used/quota = %.2f / %#v, want 40 / 250", row.MonthlyDisplayUsedUSD, row.MonthlyEffectiveAllowanceUSD)
	}
}

func TestZeroMultiplierDoesNotRewriteHistoricalUsedOrQuota(t *testing.T) {
	row := BuildSubscriptionRow(SubscriptionInput{
		GroupID:                 "42",
		GroupName:               "Group Alpha",
		GroupDefaultMultiplier:  floatPtr(0),
		SystemDefaultMultiplier: 1.0,
		MonthlyLimitUSD:         floatPtr(500.0),
		MonthlyUsageUSD:         80.0,
		UsageValueBasis:         "raw_actual_cost",
	})
	if row.MonthlyDisplayUsedUSD != 80 {
		t.Fatalf("monthly display used = %.2f, want 80", row.MonthlyDisplayUsedUSD)
	}
	if row.MonthlyEffectiveAllowanceUSD == nil || *row.MonthlyEffectiveAllowanceUSD != 500 {
		t.Fatalf("monthly quota = %#v, want 500", row.MonthlyEffectiveAllowanceUSD)
	}
	if row.MonthlyEffectiveAllowanceUnlimited {
		t.Fatalf("monthly effective allowance unlimited = %v, want false", row.MonthlyEffectiveAllowanceUnlimited)
	}
}
