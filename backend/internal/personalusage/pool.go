package personalusage

import (
	"time"

	"github.com/ai-efficiency/backend/internal/relay"
)

func emptyGroupPoolUsage() relay.UserUsageGroupPoolUsageState {
	return relay.UserUsageGroupPoolUsageState{
		Status: "empty",
		Groups: []relay.UserUsageGroupPoolUsageGroupItem{},
	}
}

func unavailableGroupPoolUsage() relay.UserUsageGroupPoolUsageState {
	return relay.UserUsageGroupPoolUsageState{
		Status:  "unavailable",
		Message: "OAuth account-pool usage is temporarily unavailable.",
		Groups:  []relay.UserUsageGroupPoolUsageGroupItem{},
	}
}

func latestPoolAsOf(state relay.UserUsageGroupPoolUsageState) *time.Time {
	var latest *time.Time
	for _, group := range state.Groups {
		if group.AsOf == nil || (latest != nil && !group.AsOf.After(*latest)) {
			continue
		}
		value := group.AsOf.UTC()
		latest = &value
	}
	return latest
}
