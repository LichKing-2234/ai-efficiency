package personalusage

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ai-efficiency/backend/internal/relay"
)

func unavailableGroupQuotas() relay.UserUsageGroupQuotaState {
	return relay.UserUsageGroupQuotaState{
		Status:  "unavailable",
		Message: "Group quotas are temporarily unavailable.",
		Groups:  []relay.UserUsageGroupQuotaGroupItem{},
	}
}

func emptyGroupQuotas() relay.UserUsageGroupQuotaState {
	return relay.UserUsageGroupQuotaState{
		Status: "empty",
		Groups: []relay.UserUsageGroupQuotaGroupItem{},
	}
}

func resetAtForWindow(subscription relay.UserSubscription, selectedWindow string) *time.Time {
	switch selectedWindow {
	case "daily":
		return subscription.DailyResetAt
	case "weekly":
		return subscription.WeeklyResetAt
	default:
		return subscription.MonthlyResetAt
	}
}

func mergeGroupQuotas(keys []relay.APIKey, subscriptions []relay.UserSubscription, selectedWindow string) relay.UserUsageGroupQuotaState {
	subscriptionsByGroup := make(map[string]relay.UserSubscription, len(subscriptions))
	for _, subscription := range subscriptions {
		if subscription.GroupID <= 0 {
			continue
		}
		subscriptionsByGroup[strconv.FormatInt(subscription.GroupID, 10)] = subscription
	}
	if len(keys) == 0 {
		return emptyGroupQuotas()
	}

	type quotaCard struct {
		groupID   string
		groupName string
		platform  string
		used      *float64
		quota     *float64
		unlimited bool
		source    string
		resetAt   *time.Time
	}
	byGroup := make(map[string]quotaCard)
	for _, key := range keys {
		if !strings.EqualFold(strings.TrimSpace(key.Status), "active") || key.Group == nil || key.Group.ID <= 0 {
			continue
		}
		groupID := strconv.FormatInt(key.Group.ID, 10)
		used, quota, source, unlimited := selectQuotaPresentation(&key, subscriptionsByGroup[groupID], selectedWindow)
		var resetAt *time.Time
		if isSubscriptionQuotaSource(source) {
			resetAt = resetAtForWindow(subscriptionsByGroup[groupID], selectedWindow)
		}
		byGroup[groupID] = quotaCard{
			groupID:   groupID,
			groupName: firstNonEmpty(strings.TrimSpace(key.Group.Name), groupID),
			platform:  strings.TrimSpace(key.Group.Platform),
			used:      used,
			quota:     quota,
			unlimited: unlimited,
			source:    source,
			resetAt:   resetAt,
		}
	}
	if len(byGroup) == 0 {
		return emptyGroupQuotas()
	}

	groupIDs := make([]string, 0, len(byGroup))
	for groupID := range byGroup {
		groupIDs = append(groupIDs, groupID)
	}
	sort.Strings(groupIDs)
	out := relay.UserUsageGroupQuotaState{
		Status:    "ok",
		UnitLabel: "USD",
		Groups:    make([]relay.UserUsageGroupQuotaGroupItem, 0, len(groupIDs)),
	}
	for _, groupID := range groupIDs {
		item := byGroup[groupID]
		out.Groups = append(out.Groups, relay.UserUsageGroupQuotaGroupItem{
			GroupID: item.groupID, GroupName: item.groupName, Platform: item.platform,
			UsedAmount: item.used, QuotaAmount: item.quota, IsUnlimited: item.unlimited, QuotaSource: item.source, ResetAt: item.resetAt,
		})
	}
	return out
}

func isSubscriptionQuotaSource(source string) bool {
	return strings.HasPrefix(source, "group_") && strings.Contains(source, "_subscription")
}

func selectQuotaPresentation(key *relay.APIKey, subscription relay.UserSubscription, selectedWindow string) (*float64, *float64, string, bool) {
	if key == nil {
		return nil, nil, "", true
	}
	if key.Quota > 0 {
		return float64Pointer(key.QuotaUsed), float64Pointer(key.Quota), "api_key", false
	}
	if key.Group == nil {
		return nil, nil, "", true
	}
	if strings.EqualFold(strings.TrimSpace(key.Group.SubscriptionType), "subscription") {
		used := usageForWindow(subscription, selectedWindow)
		quota := quotaForWindow(key.Group, selectedWindow)
		if quota != nil {
			return float64Pointer(used), quota, "group_" + selectedWindow + "_subscription", false
		}
		if hasAnyPositiveGroupLimit(key.Group) {
			return float64Pointer(used), nil, "group_" + selectedWindow + "_subscription_window_unconfigured", false
		}
		return float64Pointer(used), nil, "group_subscription_unlimited", true
	}
	quota := quotaForWindow(key.Group, selectedWindow)
	if quota != nil {
		return nil, quota, "group_" + selectedWindow, false
	}
	if hasAnyPositiveGroupLimit(key.Group) {
		return nil, nil, "group_" + selectedWindow + "_window_unconfigured", false
	}
	return nil, nil, "", true
}

func usageForWindow(subscription relay.UserSubscription, selectedWindow string) float64 {
	switch selectedWindow {
	case "daily":
		return subscription.DailyUsageUSD
	case "weekly":
		return subscription.WeeklyUsageUSD
	default:
		return subscription.MonthlyUsageUSD
	}
}

func quotaForWindow(group *relay.Group, selectedWindow string) *float64 {
	if group == nil {
		return nil
	}
	switch selectedWindow {
	case "daily":
		if positiveLimit(group.DailyLimitUSD) {
			return group.DailyLimitUSD
		}
	case "weekly":
		if positiveLimit(group.WeeklyLimitUSD) {
			return group.WeeklyLimitUSD
		}
	default:
		if positiveLimit(group.MonthlyLimitUSD) {
			return group.MonthlyLimitUSD
		}
	}
	return nil
}

func quotaWindow(params relay.UserUsageDashboardParams) string {
	if strings.EqualFold(strings.TrimSpace(params.Granularity), "hour") {
		return "daily"
	}
	start, startErr := time.Parse("2006-01-02", strings.TrimSpace(params.StartDate))
	end, endErr := time.Parse("2006-01-02", strings.TrimSpace(params.EndDate))
	if startErr == nil && endErr == nil {
		days := int(end.Sub(start).Hours()/24) + 1
		if days <= 1 {
			return "daily"
		}
		if days <= 7 {
			return "weekly"
		}
	}
	return "monthly"
}

func hasAnyPositiveGroupLimit(group *relay.Group) bool {
	return group != nil && (positiveLimit(group.DailyLimitUSD) || positiveLimit(group.WeeklyLimitUSD) || positiveLimit(group.MonthlyLimitUSD))
}

func positiveLimit(value *float64) bool { return value != nil && *value > 0 }

func float64Pointer(value float64) *float64 { return &value }

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}
