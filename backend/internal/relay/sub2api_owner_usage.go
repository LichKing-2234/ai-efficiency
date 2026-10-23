package relay

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// ownerAccountUsageBatchLimit bounds one batch read. The console requests at most
// one page of account IDs, so this is a defensive ceiling rather than a product
// limit.
const ownerAccountUsageBatchLimit = 20

// ReadOwnerAccountUsage reads one bounded usage batch through the shared batch
// usage read. Sub2API probes upstream and persists snapshots when a window is
// missing, the account is rate-limited, or its snapshot is stale, so this is a
// side-effecting read: page loads pass force=false and only an explicit refresh
// passes force=true.
func (s *sub2apiRelay) ReadOwnerAccountUsage(ctx context.Context, accountIDs []int64, force bool) ([]OwnerAccountUsage, error) {
	requested := make([]int64, 0, len(accountIDs))
	seen := make(map[int64]struct{}, len(accountIDs))
	for _, accountID := range accountIDs {
		if accountID <= 0 {
			continue
		}
		if _, exists := seen[accountID]; exists {
			continue
		}
		seen[accountID] = struct{}{}
		requested = append(requested, accountID)
	}
	if len(requested) == 0 {
		return []OwnerAccountUsage{}, nil
	}
	if len(requested) > ownerAccountUsageBatchLimit {
		return nil, fmt.Errorf("relay: read owner account usage: at most %d accounts per batch", ownerAccountUsageBatchLimit)
	}
	sort.Slice(requested, func(i, j int) bool { return requested[i] < requested[j] })

	usageByAccount, errorsByAccount, err := s.getBatchAccountUsage(ctx, requested, force)
	if err != nil {
		return nil, fmt.Errorf("relay: read owner account usage: %w", err)
	}

	out := make([]OwnerAccountUsage, 0, len(requested))
	for _, accountID := range requested {
		key := strconv.FormatInt(accountID, 10)
		if message := strings.TrimSpace(errorsByAccount[key]); message != "" {
			// A per-account upstream failure is reported on the entry so one slow
			// account cannot hide the rest of the page. The text stays internal.
			out = append(out, OwnerAccountUsage{AccountID: accountID, Err: errors.New(message)})
			continue
		}
		out = append(out, projectOwnerAccountUsage(accountID, usageByAccount[key]))
	}
	return out, nil
}

func projectOwnerAccountUsage(accountID int64, usage *sub2apiUsageInfo) OwnerAccountUsage {
	entry := OwnerAccountUsage{AccountID: accountID}
	if usage == nil {
		return entry
	}
	entry.UpdatedAt = utcTimePtr(usage.UpdatedAt)
	entry.FiveHour = projectOwnerAccountUsageWindow(usage.FiveHour)
	entry.SevenDay = projectOwnerAccountUsageWindow(usage.SevenDay)
	entry.Health = OwnerAccountUsageHealth{
		NeedsReauth: usage.NeedsReauth,
		IsBanned:    usage.IsBanned,
		IsForbidden: usage.IsForbidden,
		ErrorCode:   strings.TrimSpace(usage.ErrorCode),
	}
	return entry
}

func projectOwnerAccountUsageWindow(window *sub2apiUsageProgress) *OwnerAccountUsageWindow {
	if window == nil {
		return nil
	}
	out := &OwnerAccountUsageWindow{
		Utilization: window.Utilization,
		ResetsAt:    utcTimePtr(window.ResetsAt),
	}
	if window.WindowStats != nil {
		out.Requests = window.WindowStats.Requests
		out.Tokens = window.WindowStats.Tokens
		out.StandardCost = window.WindowStats.StandardCost
		out.UserCost = window.WindowStats.UserCost
	}
	return out
}
