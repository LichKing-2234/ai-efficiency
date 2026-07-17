package teamusage

type SubscriptionInput struct {
	GroupID                   string
	GroupName                 string
	Platform                  string
	SubscriptionStatus        string
	GroupDefaultMultiplier    *float64
	SystemDefaultMultiplier   float64
	UserMultiplier            *float64
	DailyLimitUSD             *float64
	WeeklyLimitUSD            *float64
	MonthlyLimitUSD           *float64
	DailyUsageUSD             float64
	WeeklyUsageUSD            float64
	MonthlyUsageUSD           float64
	UsageValueBasis           string
	Editable                  bool
	EditableReason            *string
	MultiplierMetadataStatus  string
	MultiplierMetadataMessage *string
}

func BuildSubscriptionRow(input SubscriptionInput) SubscriptionRow {
	metadataStatus := input.MultiplierMetadataStatus
	if metadataStatus != MultiplierMetadataStatusUnavailable {
		metadataStatus = MultiplierMetadataStatusOK
		input.MultiplierMetadataMessage = nil
	}
	userMultiplier := input.UserMultiplier
	var effective *float64
	source := "unknown"
	if metadataStatus == MultiplierMetadataStatusUnavailable {
		userMultiplier = nil
	} else {
		value, valueSource := effectiveMultiplier(userMultiplier, input.GroupDefaultMultiplier, input.SystemDefaultMultiplier)
		effective = &value
		source = valueSource
	}
	inherited, _ := effectiveMultiplier(nil, input.GroupDefaultMultiplier, input.SystemDefaultMultiplier)
	dailyAllowance, dailyUnlimited := displayQuota(input.DailyLimitUSD)
	weeklyAllowance, weeklyUnlimited := displayQuota(input.WeeklyLimitUSD)
	monthlyAllowance, monthlyUnlimited := displayQuota(input.MonthlyLimitUSD)
	return SubscriptionRow{
		GroupID:                            input.GroupID,
		GroupName:                          input.GroupName,
		Platform:                           input.Platform,
		SubscriptionStatus:                 input.SubscriptionStatus,
		GroupDefaultMultiplier:             input.GroupDefaultMultiplier,
		SystemDefaultMultiplier:            input.SystemDefaultMultiplier,
		InheritedDefaultMultiplier:         inherited,
		UserMultiplier:                     userMultiplier,
		EffectiveMultiplier:                effective,
		MultiplierSource:                   source,
		MultiplierMetadataStatus:           metadataStatus,
		MultiplierMetadataMessage:          input.MultiplierMetadataMessage,
		DailyLimitUSD:                      input.DailyLimitUSD,
		WeeklyLimitUSD:                     input.WeeklyLimitUSD,
		MonthlyLimitUSD:                    input.MonthlyLimitUSD,
		DailyEffectiveAllowanceUSD:         dailyAllowance,
		WeeklyEffectiveAllowanceUSD:        weeklyAllowance,
		MonthlyEffectiveAllowanceUSD:       monthlyAllowance,
		DailyEffectiveAllowanceUnlimited:   dailyUnlimited,
		WeeklyEffectiveAllowanceUnlimited:  weeklyUnlimited,
		MonthlyEffectiveAllowanceUnlimited: monthlyUnlimited,
		DailyDisplayUsedUSD:                displayUsage(input.DailyUsageUSD),
		WeeklyDisplayUsedUSD:               displayUsage(input.WeeklyUsageUSD),
		MonthlyDisplayUsedUSD:              displayUsage(input.MonthlyUsageUSD),
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

func displayUsage(rawUsage float64) float64 {
	return rawUsage
}

func displayQuota(rawQuota *float64) (*float64, bool) {
	if rawQuota == nil {
		return nil, true
	}
	return rawQuota, false
}
