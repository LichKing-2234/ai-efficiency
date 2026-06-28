package teamusage

import (
	"sort"

	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/ai-efficiency/backend/internal/representativescope"
)

func BuildOverviewUnavailableForLargeScope(subjects []representativescope.Subject, limit int) OverviewResponse {
	_ = limit
	reason := "scope_too_large"
	return OverviewResponse{
		Configured:       true,
		IsRepresentative: true,
		Summary: OverviewSummary{
			Unavailable:       true,
			UnavailableReason: &reason,
			MemberCount:       len(subjects),
			UnitLabel:         "USD",
		},
		TopMembers: []OverviewMember{},
		TopMemberTrend: TopMemberTrendState{
			UnitLabel:         "USD",
			RankBasis:         "last_30d_actual_cost",
			Unavailable:       true,
			UnavailableReason: &reason,
			Series:            []TopMemberTrendSeries{},
		},
		Members: []OverviewMember{},
	}
}

func RankTopMembers(subjects []representativescope.Subject, stats map[int64]relay.TeamUserUsageStats, limit int) []OverviewMember {
	members := make([]OverviewMember, 0, len(subjects))
	for _, subject := range subjects {
		if subject.RelayUserID == nil {
			continue
		}
		relayUserID := int64(*subject.RelayUserID)
		stat := stats[relayUserID]
		members = append(members, OverviewMember{
			UserID:                subject.UserID,
			DisplayName:           subject.DisplayName,
			Email:                 subject.Email,
			DepartmentDisplayPath: subject.DepartmentDisplayPath,
			RelayUserID:           subject.RelayUserID,
			TodayActualCost:       stat.TodayActualCost,
			Last30dActualCost:     stat.Last30dActualCost,
			TotalTokens:           stat.TotalTokens,
			Selectable:            subject.Selectable,
		})
	}
	sort.SliceStable(members, func(i, j int) bool {
		if members[i].Last30dActualCost == members[j].Last30dActualCost {
			return members[i].UserID < members[j].UserID
		}
		return members[i].Last30dActualCost > members[j].Last30dActualCost
	})
	if limit > 0 && len(members) > limit {
		members = members[:limit]
	}
	for i := range members {
		members[i].Rank = i + 1
	}
	return members
}
