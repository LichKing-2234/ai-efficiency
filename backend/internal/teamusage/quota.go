package teamusage

type SubscriptionInput struct {
	GroupID                 string
	GroupName               string
	Platform                string
	SubscriptionStatus      string
	GroupDefaultMultiplier  *float64
	SystemDefaultMultiplier float64
	UserMultiplier          *float64
	DailyLimitUSD           *float64
	WeeklyLimitUSD          *float64
	MonthlyLimitUSD         *float64
	DailyUsageUSD           float64
	WeeklyUsageUSD          float64
	MonthlyUsageUSD         float64
	UsageValueBasis         string
	Editable                bool
	EditableReason          *string
}

func BuildSubscriptionRow(input SubscriptionInput) SubscriptionRow {
	effective, source := effectiveMultiplier(input.UserMultiplier, input.GroupDefaultMultiplier, input.SystemDefaultMultiplier)
	inherited, _ := effectiveMultiplier(nil, input.GroupDefaultMultiplier, input.SystemDefaultMultiplier)
	dailyAllowance, dailyUnlimited := displayQuota(input.DailyLimitUSD, effective, input.UsageValueBasis)
	weeklyAllowance, weeklyUnlimited := displayQuota(input.WeeklyLimitUSD, effective, input.UsageValueBasis)
	monthlyAllowance, monthlyUnlimited := displayQuota(input.MonthlyLimitUSD, effective, input.UsageValueBasis)
	return SubscriptionRow{
		GroupID:                            input.GroupID,
		GroupName:                          input.GroupName,
		Platform:                           input.Platform,
		SubscriptionStatus:                 input.SubscriptionStatus,
		GroupDefaultMultiplier:             input.GroupDefaultMultiplier,
		SystemDefaultMultiplier:            input.SystemDefaultMultiplier,
		InheritedDefaultMultiplier:         inherited,
		UserMultiplier:                     input.UserMultiplier,
		EffectiveMultiplier:                effective,
		MultiplierSource:                   source,
		DailyLimitUSD:                      input.DailyLimitUSD,
		WeeklyLimitUSD:                     input.WeeklyLimitUSD,
		MonthlyLimitUSD:                    input.MonthlyLimitUSD,
		DailyEffectiveAllowanceUSD:         dailyAllowance,
		WeeklyEffectiveAllowanceUSD:        weeklyAllowance,
		MonthlyEffectiveAllowanceUSD:       monthlyAllowance,
		DailyEffectiveAllowanceUnlimited:   dailyUnlimited,
		WeeklyEffectiveAllowanceUnlimited:  weeklyUnlimited,
		MonthlyEffectiveAllowanceUnlimited: monthlyUnlimited,
		DailyDisplayUsedUSD:                displayUsage(input.DailyUsageUSD, effective, input.UsageValueBasis),
		WeeklyDisplayUsedUSD:               displayUsage(input.WeeklyUsageUSD, effective, input.UsageValueBasis),
		MonthlyDisplayUsedUSD:              displayUsage(input.MonthlyUsageUSD, effective, input.UsageValueBasis),
		DailyUsageUSD:                      input.DailyUsageUSD,
		WeeklyUsageUSD:                     input.WeeklyUsageUSD,
		MonthlyUsageUSD:                    input.MonthlyUsageUSD,
		UsageValueBasis:                    input.UsageValueBasis,
		QuotaWindowBasis:                   "sub2api_enforcement_window",
		Editable:                           input.Editable,
		EditableReason:                     input.EditableReason,
	}
}

func effectiveMultiplier(userMultiplier *float64, groupDefault *float64, systemDefault float64) (float64, string) {
	if userMultiplier != nil {
		return *userMultiplier, "user"
	}
	if groupDefault != nil {
		return *groupDefault, "group"
	}
	return systemDefault, "system"
}

func displayUsage(rawUsage, multiplier float64, basis string) float64 {
	if basis == "normalized_display_cost" {
		return rawUsage
	}
	if multiplier == 0 {
		return 0
	}
	return rawUsage / multiplier
}

func displayQuota(rawQuota *float64, multiplier float64, basis string) (*float64, bool) {
	if rawQuota == nil {
		return nil, true
	}
	if basis == "normalized_display_cost" {
		return rawQuota, false
	}
	if multiplier == 0 {
		return nil, true
	}
	value := *rawQuota / multiplier
	return &value, false
}
