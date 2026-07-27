package teamusage

import (
	"sort"
	"strconv"

	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/ai-efficiency/backend/internal/representativescope"
)

const (
	teamOverviewCostUnitLabel = "USD"
	topMemberRankBasisTokens  = "range_total_tokens"
	departmentTrendTeamTotal  = "team_total"
	departmentTrendDepartment = "department"
	teamTotalDisplayName      = "Team total"
	maxDepartmentComparisons  = 12
)

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
		DepartmentExternalIDs:     appendUniqueStrings(nil, subject.DepartmentExternalIDs...),
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

func overviewMemberDepartmentIDs(member OverviewMember) []string {
	departmentIDs := appendUniqueStrings(nil, member.DepartmentExternalIDs...)
	if len(departmentIDs) == 0 {
		departmentIDs = appendUniqueStrings(departmentIDs, member.DepartmentExternalID)
	}
	return departmentIDs
}

func overviewSubjectDepartmentIDs(subject representativescope.Subject) []string {
	departmentIDs := appendUniqueStrings(nil, subject.DepartmentExternalIDs...)
	if len(departmentIDs) == 0 {
		departmentIDs = appendUniqueStrings(departmentIDs, subject.DepartmentExternalID)
	}
	return departmentIDs
}

func overviewMemberIdentityKey(member OverviewMember) string {
	if member.UserID > 0 {
		return "user:" + strconv.Itoa(member.UserID)
	}
	if member.DirectoryMemberExternalID != "" {
		return "directory:" + member.DirectoryMemberExternalID
	}
	if member.Email != "" {
		return "email:" + member.Email
	}
	return ""
}

func BuildOverviewDepartmentTrend(departments []representativescope.DepartmentScope, rootIDs []string, subjects []representativescope.Subject, pointsByUser map[int64][]relay.UsageTrendPoint) DepartmentTrendState {
	state := DepartmentTrendState{
		UnitLabel: teamOverviewCostUnitLabel,
		Series:    []DepartmentTrendSeries{},
	}
	total := newTrendAccumulator()
	nodeByID, departmentOrder := overviewDepartmentNodeIndex(departments)
	rootSet := overviewDepartmentRootSet(nodeByID, departmentOrder, rootIDs)
	childIDsByParent := overviewDepartmentChildrenByParent(nodeByID, departmentOrder)
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
		bucketIDs := map[string]struct{}{}
		for _, departmentID := range overviewSubjectDepartmentIDs(subject) {
			bucketID := overviewDepartmentTrendBucket(departmentID, nodeByID, rootSet, childIDsByParent)
			if bucketID != "" {
				bucketIDs[bucketID] = struct{}{}
			}
		}
		for bucketID := range bucketIDs {
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
		return departmentSeries[i].DepartmentExternalID < departmentSeries[j].DepartmentExternalID
	})
	state.ComparisonTotalCount = len(departmentSeries)
	if len(departmentSeries) > maxDepartmentComparisons {
		state.ComparisonTruncated = true
		departmentSeries = departmentSeries[:maxDepartmentComparisons]
	}
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

func overviewDepartmentChildrenByParent(nodeByID map[string]*overviewDepartmentTrendNode, departmentOrder []string) map[string][]string {
	childrenByParent := make(map[string][]string, len(nodeByID))
	for _, departmentID := range departmentOrder {
		node := nodeByID[departmentID]
		if node == nil || node.ParentExternalID == nil {
			continue
		}
		if _, ok := nodeByID[*node.ParentExternalID]; !ok {
			continue
		}
		childrenByParent[*node.ParentExternalID] = append(childrenByParent[*node.ParentExternalID], departmentID)
	}
	return childrenByParent
}

func overviewDepartmentTrendBucket(departmentID string, nodeByID map[string]*overviewDepartmentTrendNode, rootSet map[string]struct{}, childIDsByParent map[string][]string) string {
	if departmentID == "" {
		return ""
	}
	leafToRoot := make([]string, 0, 4)
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
		leafToRoot = append(leafToRoot, currentID)
		if node.ParentExternalID == nil {
			break
		}
		if _, ok := nodeByID[*node.ParentExternalID]; !ok {
			break
		}
		currentID = *node.ParentExternalID
	}
	rootToLeaf := make([]string, 0, len(leafToRoot))
	for i := len(leafToRoot) - 1; i >= 0; i-- {
		rootToLeaf = append(rootToLeaf, leafToRoot[i])
	}
	for i, candidateID := range rootToLeaf {
		if _, ok := rootSet[candidateID]; !ok {
			continue
		}
		if len(rootSet) > 1 {
			return candidateID
		}
		comparisonRoot := overviewDepartmentComparisonRoot(candidateID, childIDsByParent)
		comparisonRootIndex := -1
		for j := i; j < len(rootToLeaf); j++ {
			if rootToLeaf[j] == comparisonRoot {
				comparisonRootIndex = j
				break
			}
		}
		if comparisonRootIndex < 0 {
			return candidateID
		}
		if comparisonRootIndex+1 < len(rootToLeaf) {
			return rootToLeaf[comparisonRootIndex+1]
		}
		return comparisonRoot
	}
	return ""
}

func overviewDepartmentComparisonRoot(rootID string, childIDsByParent map[string][]string) string {
	comparisonRoot := rootID
	seen := map[string]struct{}{}
	for {
		if _, ok := seen[comparisonRoot]; ok {
			return comparisonRoot
		}
		seen[comparisonRoot] = struct{}{}
		children := childIDsByParent[comparisonRoot]
		if len(children) != 1 {
			return comparisonRoot
		}
		onlyChild := children[0]
		if len(childIDsByParent[onlyChild]) == 0 {
			return comparisonRoot
		}
		comparisonRoot = onlyChild
	}
}

func shouldSkipSingleRootDepartmentSeries(departmentID string, rootSet map[string]struct{}, bucketTotals map[string]*trendAccumulator) bool {
	if len(rootSet) != 1 {
		return false
	}
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

func appendUniqueStrings(current []string, values ...string) []string {
	seen := make(map[string]struct{}, len(current)+len(values))
	out := make([]string, 0, len(current)+len(values))
	for _, value := range current {
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
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
