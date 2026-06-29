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
			RankBasis:         "range_actual_cost",
			Unavailable:       true,
			UnavailableReason: &reason,
			Series:            []TopMemberTrendSeries{},
		},
		Members: []OverviewMember{},
	}
}

func RankTopMembers(subjects []representativescope.Subject, stats map[int64]relay.TeamUserUsageStats, totals map[int64]overviewWindowTotal, limit int) []OverviewMember {
	members := make([]OverviewMember, 0, len(subjects))
	for _, subject := range subjects {
		if subject.RelayUserID == nil {
			continue
		}
		members = append(members, overviewMemberFromSubject(subject, stats, totals))
	}
	sortOverviewMembers(members)
	if limit > 0 && len(members) > limit {
		members = members[:limit]
	}
	for i := range members {
		members[i].Rank = i + 1
	}
	return members
}

func BuildOverviewMemberDetails(subjects []representativescope.Subject, stats map[int64]relay.TeamUserUsageStats, totals map[int64]overviewWindowTotal) []OverviewMember {
	members := make([]OverviewMember, 0, len(subjects))
	for _, subject := range subjects {
		if subject.SubjectType != "member" {
			continue
		}
		members = append(members, overviewMemberFromSubject(subject, stats, totals))
	}
	sortOverviewMembers(members)
	for i := range members {
		members[i].Rank = i + 1
	}
	return members
}

func MergeResolvedOverviewSubjects(subjects []representativescope.Subject, resolved []representativescope.Subject) []representativescope.Subject {
	byUserID := make(map[int]representativescope.Subject, len(resolved))
	for _, subject := range resolved {
		if subject.UserID <= 0 {
			continue
		}
		byUserID[subject.UserID] = subject
	}
	merged := make([]representativescope.Subject, 0, len(subjects))
	for _, subject := range subjects {
		if resolvedSubject, ok := byUserID[subject.UserID]; ok {
			merged = append(merged, resolvedSubject)
			continue
		}
		merged = append(merged, subject)
	}
	return merged
}

func overviewMemberFromSubject(subject representativescope.Subject, stats map[int64]relay.TeamUserUsageStats, totals map[int64]overviewWindowTotal) OverviewMember {
	member := OverviewMember{
		UserID:                    subject.UserID,
		DirectoryMemberExternalID: subject.DirectoryMemberExternalID,
		DisplayName:               subject.DisplayName,
		Email:                     subject.Email,
		DepartmentDisplayPath:     subject.DepartmentDisplayPath,
		RelayUserID:               subject.RelayUserID,
		Selectable:                subject.Selectable,
	}
	if subject.RelayUserID == nil {
		return member
	}
	relayUserID := int64(*subject.RelayUserID)
	stat := stats[relayUserID]
	total := totals[relayUserID]
	member.RangeActualCost = total.ActualCost
	member.TodayActualCost = stat.TodayActualCost
	member.TotalActualCost = stat.TotalActualCost
	member.TotalTokens = total.TotalTokens
	return member
}

func sortOverviewMembers(members []OverviewMember) {
	sort.SliceStable(members, func(i, j int) bool {
		if members[i].RangeActualCost == members[j].RangeActualCost {
			if members[i].DisplayName != members[j].DisplayName {
				return members[i].DisplayName < members[j].DisplayName
			}
			if members[i].Email != members[j].Email {
				return members[i].Email < members[j].Email
			}
			if members[i].DirectoryMemberExternalID != members[j].DirectoryMemberExternalID {
				return members[i].DirectoryMemberExternalID < members[j].DirectoryMemberExternalID
			}
			return members[i].UserID < members[j].UserID
		}
		return members[i].RangeActualCost > members[j].RangeActualCost
	})
}
