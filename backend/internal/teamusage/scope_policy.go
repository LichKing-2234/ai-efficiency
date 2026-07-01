package teamusage

import (
	"sort"

	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/ai-efficiency/backend/internal/representativescope"
)

const (
	teamOverviewCostUnitLabel = "USD"
	topMemberRankBasisTokens  = "range_total_tokens"
	departmentTrendTeamTotal  = "team_total"
	departmentTrendDepartment = "department"
	teamTotalDisplayName      = "Team total"
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
			UnitLabel:         teamOverviewCostUnitLabel,
		},
		TopMembers: []OverviewMember{},
		TopMemberTrend: TopMemberTrendState{
			UnitLabel:         teamOverviewCostUnitLabel,
			RankBasis:         topMemberRankBasisTokens,
			Unavailable:       true,
			UnavailableReason: &reason,
			Series:            []TopMemberTrendSeries{},
		},
		DepartmentTrend: DepartmentTrendState{
			UnitLabel:         teamOverviewCostUnitLabel,
			Unavailable:       true,
			UnavailableReason: &reason,
			Series:            []DepartmentTrendSeries{},
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

func BuildOverviewDepartmentTrend(departments []representativescope.DepartmentScope, rootIDs []string, subjects []representativescope.Subject, pointsByUser map[int64][]relay.UsageTrendPoint) DepartmentTrendState {
	state := DepartmentTrendState{
		UnitLabel: teamOverviewCostUnitLabel,
		Series:    []DepartmentTrendSeries{},
	}
	total := newTrendAccumulator()
	nodeByID, departmentOrder := overviewDepartmentNodeIndex(departments)
	rootSet := overviewDepartmentRootSet(nodeByID, departmentOrder, rootIDs)
	bucketTotals := map[string]*trendAccumulator{}

	for _, subject := range subjects {
		if subject.SubjectType != "member" || subject.RelayUserID == nil {
			continue
		}
		points := pointsByUser[int64(*subject.RelayUserID)]
		if len(points) == 0 {
			continue
		}
		total.AddPoints(points)
		if bucketID := overviewDepartmentTrendBucket(subject.DepartmentExternalID, nodeByID, rootSet); bucketID != "" {
			if bucketTotals[bucketID] == nil {
				bucketTotals[bucketID] = newTrendAccumulator()
			}
			bucketTotals[bucketID].AddPoints(points)
		}
	}

	if total.Len() == 0 {
		return state
	}
	state.Series = append(state.Series, DepartmentTrendSeries{
		SeriesType:  departmentTrendTeamTotal,
		DisplayName: teamTotalDisplayName,
		Points:      total.Points(),
	})

	departmentSeries := make([]DepartmentTrendSeries, 0, len(bucketTotals))
	for _, departmentID := range departmentOrder {
		accumulator := bucketTotals[departmentID]
		if accumulator == nil || accumulator.Len() == 0 {
			continue
		}
		if shouldSkipSingleRootDepartmentSeries(departmentID, rootSet, bucketTotals) {
			continue
		}
		node := nodeByID[departmentID]
		if node == nil {
			continue
		}
		departmentSeries = append(departmentSeries, DepartmentTrendSeries{
			SeriesType:           departmentTrendDepartment,
			DepartmentExternalID: departmentID,
			DisplayName:          node.Name,
			Points:               accumulator.Points(),
		})
	}
	sort.SliceStable(departmentSeries, func(i, j int) bool {
		leftTokens, leftHasTokens := trendSeriesTokenTotal(departmentSeries[i].Points)
		rightTokens, rightHasTokens := trendSeriesTokenTotal(departmentSeries[j].Points)
		if leftHasTokens != rightHasTokens {
			return leftHasTokens
		}
		if leftTokens != rightTokens {
			return leftTokens > rightTokens
		}
		leftCost := trendSeriesActualCostTotal(departmentSeries[i].Points)
		rightCost := trendSeriesActualCostTotal(departmentSeries[j].Points)
		if leftCost != rightCost {
			return leftCost > rightCost
		}
		if departmentSeries[i].DisplayName != departmentSeries[j].DisplayName {
			return departmentSeries[i].DisplayName < departmentSeries[j].DisplayName
		}
		return departmentSeries[i].DepartmentExternalID < departmentSeries[j].DepartmentExternalID
	})
	for i := range departmentSeries {
		departmentSeries[i].Rank = i + 1
		state.Series = append(state.Series, departmentSeries[i])
	}
	return state
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

type overviewDepartmentTrendNode struct {
	ExternalID       string
	ParentExternalID *string
	Name             string
}

func overviewDepartmentNodeIndex(departments []representativescope.DepartmentScope) (map[string]*overviewDepartmentTrendNode, []string) {
	nodeByID := make(map[string]*overviewDepartmentTrendNode, len(departments))
	departmentOrder := make([]string, 0, len(departments))
	for _, department := range departments {
		if department.ExternalID == "" {
			continue
		}
		nodeByID[department.ExternalID] = &overviewDepartmentTrendNode{
			ExternalID:       department.ExternalID,
			ParentExternalID: department.ParentExternalID,
			Name:             department.Name,
		}
		departmentOrder = append(departmentOrder, department.ExternalID)
	}
	return nodeByID, departmentOrder
}

func overviewDepartmentRootSet(nodeByID map[string]*overviewDepartmentTrendNode, departmentOrder []string, rootIDs []string) map[string]struct{} {
	rootSet := overviewStringSet(rootIDs)
	if len(rootSet) > 0 {
		return rootSet
	}
	for _, departmentID := range departmentOrder {
		node := nodeByID[departmentID]
		if node == nil || node.ParentExternalID == nil {
			rootSet[departmentID] = struct{}{}
			continue
		}
		if _, ok := nodeByID[*node.ParentExternalID]; !ok {
			rootSet[departmentID] = struct{}{}
		}
	}
	return rootSet
}

func overviewDepartmentTrendBucket(departmentID string, nodeByID map[string]*overviewDepartmentTrendNode, rootSet map[string]struct{}) string {
	if departmentID == "" {
		return ""
	}
	path := make([]string, 0, 4)
	seen := map[string]struct{}{}
	for currentID := departmentID; currentID != ""; {
		if _, ok := seen[currentID]; ok {
			return ""
		}
		seen[currentID] = struct{}{}
		node := nodeByID[currentID]
		if node == nil {
			return ""
		}
		path = append(path, currentID)
		if node.ParentExternalID == nil {
			break
		}
		currentID = *node.ParentExternalID
	}
	for i := len(path) - 1; i >= 0; i-- {
		departmentID := path[i]
		if _, ok := rootSet[departmentID]; !ok {
			continue
		}
		if i > 0 {
			return path[i-1]
		}
		return departmentID
	}
	return ""
}

func shouldSkipSingleRootDepartmentSeries(departmentID string, rootSet map[string]struct{}, bucketTotals map[string]*trendAccumulator) bool {
	if len(bucketTotals) != 1 {
		return false
	}
	_, isRoot := rootSet[departmentID]
	return isRoot
}

type trendAccumulator struct {
	byDate map[string]*trendPointTotal
}

type trendPointTotal struct {
	actualCost  float64
	totalTokens int64
	hasTokens   bool
}

func newTrendAccumulator() *trendAccumulator {
	return &trendAccumulator{byDate: map[string]*trendPointTotal{}}
}

func (a *trendAccumulator) AddPoints(points []relay.UsageTrendPoint) {
	if a == nil {
		return
	}
	for _, point := range points {
		if point.Date == "" {
			continue
		}
		total := a.byDate[point.Date]
		if total == nil {
			total = &trendPointTotal{}
			a.byDate[point.Date] = total
		}
		total.actualCost += point.ActualCost
		if point.TotalTokens == nil {
			continue
		}
		total.totalTokens += *point.TotalTokens
		total.hasTokens = true
	}
}

func (a *trendAccumulator) Len() int {
	if a == nil {
		return 0
	}
	return len(a.byDate)
}

func (a *trendAccumulator) Points() []relay.UsageTrendPoint {
	if a == nil || len(a.byDate) == 0 {
		return []relay.UsageTrendPoint{}
	}
	dates := make([]string, 0, len(a.byDate))
	for date := range a.byDate {
		dates = append(dates, date)
	}
	sort.Strings(dates)
	points := make([]relay.UsageTrendPoint, 0, len(dates))
	for _, date := range dates {
		total := a.byDate[date]
		point := relay.UsageTrendPoint{
			Date:       date,
			ActualCost: total.actualCost,
		}
		if total.hasTokens {
			tokenTotal := total.totalTokens
			point.TotalTokens = &tokenTotal
		}
		points = append(points, point)
	}
	return points
}

func trendSeriesTokenTotal(points []relay.UsageTrendPoint) (int64, bool) {
	var total int64
	hasTokens := false
	for _, point := range points {
		if point.TotalTokens == nil {
			continue
		}
		total += *point.TotalTokens
		hasTokens = true
	}
	return total, hasTokens
}

func trendSeriesActualCostTotal(points []relay.UsageTrendPoint) float64 {
	total := 0.0
	for _, point := range points {
		total += point.ActualCost
	}
	return total
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
		leftTokens := overviewMemberTokenTotal(members[i])
		rightTokens := overviewMemberTokenTotal(members[j])
		if leftTokens != rightTokens {
			return leftTokens > rightTokens
		}
		if members[i].RangeActualCost != members[j].RangeActualCost {
			return members[i].RangeActualCost > members[j].RangeActualCost
		}
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
	})
}

func overviewMemberTokenTotal(member OverviewMember) int64 {
	if member.TotalTokens == nil {
		return 0
	}
	return *member.TotalTokens
}
