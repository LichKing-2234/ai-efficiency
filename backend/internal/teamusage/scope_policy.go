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
		Members:    []OverviewMember{},
		MemberTree: []OverviewMemberNode{},
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
		DepartmentExternalID:      subject.DepartmentExternalID,
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

func BuildOverviewMemberTree(departments []representativescope.DepartmentScope, rootIDs []string, members []OverviewMember) []OverviewMemberNode {
	if len(departments) == 0 {
		return []OverviewMemberNode{}
	}
	nodeByID := make(map[string]*OverviewMemberNode, len(departments))
	departmentOrder := make([]string, 0, len(departments))
	for _, department := range departments {
		if department.ExternalID == "" {
			continue
		}
		nodeByID[department.ExternalID] = &OverviewMemberNode{
			DepartmentExternalID: department.ExternalID,
			ParentExternalID:     department.ParentExternalID,
			Name:                 department.Name,
			DisplayPath:          department.DisplayPath,
			Depth:                department.Depth,
			ChildCount:           department.ChildCount,
			Members:              []OverviewMember{},
			Children:             []OverviewMemberNode{},
		}
		departmentOrder = append(departmentOrder, department.ExternalID)
	}
	for _, member := range members {
		node := nodeByID[member.DepartmentExternalID]
		if node == nil {
			continue
		}
		node.Members = append(node.Members, member)
	}
	for i := len(departmentOrder) - 1; i >= 0; i-- {
		node := nodeByID[departmentOrder[i]]
		if node == nil {
			continue
		}
		node.MemberCount += len(node.Members)
		for _, member := range node.Members {
			if member.RelayUserID != nil {
				node.ConnectedMemberCount++
			}
			node.RangeActualCost += member.RangeActualCost
			node.RangeTotalTokens = addOptionalInt64(node.RangeTotalTokens, member.TotalTokens)
		}
		if node.ParentExternalID == nil {
			continue
		}
		parent := nodeByID[*node.ParentExternalID]
		if parent == nil {
			continue
		}
		parent.MemberCount += node.MemberCount
		parent.ConnectedMemberCount += node.ConnectedMemberCount
		parent.RangeActualCost += node.RangeActualCost
		parent.RangeTotalTokens = addOptionalInt64(parent.RangeTotalTokens, node.RangeTotalTokens)
	}

	childIDsByParent := make(map[string][]string, len(departments))
	for _, departmentID := range departmentOrder {
		node := nodeByID[departmentID]
		if node == nil || node.ParentExternalID == nil {
			continue
		}
		if _, ok := nodeByID[*node.ParentExternalID]; !ok {
			continue
		}
		childIDsByParent[*node.ParentExternalID] = append(childIDsByParent[*node.ParentExternalID], departmentID)
	}

	var buildNode func(id string, rootDepth int) OverviewMemberNode
	buildNode = func(id string, rootDepth int) OverviewMemberNode {
		source := nodeByID[id]
		if source == nil {
			return OverviewMemberNode{}
		}
		node := *source
		node.Depth = node.Depth - rootDepth
		if node.Depth < 0 {
			node.Depth = 0
		}
		node.Members = append(make([]OverviewMember, 0, len(source.Members)), source.Members...)
		node.Children = make([]OverviewMemberNode, 0, len(childIDsByParent[id]))
		for _, childID := range childIDsByParent[id] {
			node.Children = append(node.Children, buildNode(childID, rootDepth))
		}
		return node
	}

	rootSet := overviewStringSet(rootIDs)
	if len(rootSet) == 0 {
		for _, departmentID := range departmentOrder {
			node := nodeByID[departmentID]
			if node == nil || node.ParentExternalID == nil || nodeByID[*node.ParentExternalID] == nil {
				rootSet[departmentID] = struct{}{}
			}
		}
	}
	roots := make([]OverviewMemberNode, 0, len(rootSet))
	for _, departmentID := range departmentOrder {
		if _, ok := rootSet[departmentID]; !ok {
			continue
		}
		rootDepth := 0
		if node := nodeByID[departmentID]; node != nil {
			rootDepth = node.Depth
		}
		roots = append(roots, buildNode(departmentID, rootDepth))
	}
	return roots
}

func addOptionalInt64(current *int64, next *int64) *int64 {
	if next == nil {
		return current
	}
	total := *next
	if current != nil {
		total += *current
	}
	return &total
}

func overviewStringSet(values []string) map[string]struct{} {
	out := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		out[value] = struct{}{}
	}
	return out
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
