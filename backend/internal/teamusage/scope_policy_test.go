package teamusage

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/ai-efficiency/backend/internal/representativescope"
)

func TestBuildOverviewDepartmentTrendBoundsAndReportsComparisons(t *testing.T) {
	departments := make([]representativescope.DepartmentScope, 0, 14)
	rootIDs := make([]string, 0, 14)
	subjects := make([]representativescope.Subject, 0, 14)
	pointsByUser := make(map[int64][]relay.UsageTrendPoint, 14)
	for index := 0; index < 14; index++ {
		departmentID := fmt.Sprintf("department-%02d", index)
		relayUserID := int64(1000 + index)
		tokens := int64(100)
		cost := float64(index)
		if index == 13 {
			cost = 12
		}
		departments = append(departments, representativescope.DepartmentScope{ExternalID: departmentID, Name: "Shared Name"})
		rootIDs = append(rootIDs, departmentID)
		subjects = append(subjects, representativescope.Subject{
			SubjectType: "member", UserID: index + 1, DepartmentExternalID: departmentID, RelayUserID: intPtr(int(relayUserID)),
		})
		pointsByUser[relayUserID] = []relay.UsageTrendPoint{{Date: "2026-07-16", ActualCost: cost, TotalTokens: &tokens}}
	}

	trend := BuildOverviewDepartmentTrend(departments, rootIDs, subjects, pointsByUser)

	if trend.ComparisonTotalCount != 14 || !trend.ComparisonTruncated {
		t.Fatalf("comparison metadata = %d/%v, want 14/true", trend.ComparisonTotalCount, trend.ComparisonTruncated)
	}
	if len(trend.Series) != 13 {
		t.Fatalf("series count = %d, want one team total plus 12 comparisons", len(trend.Series))
	}
	if trend.Series[0].SeriesType != "team_total" || trend.Series[0].Points[0].TotalTokens == nil || *trend.Series[0].Points[0].TotalTokens != 1400 {
		t.Fatalf("team total = %#v, want complete 1400 tokens", trend.Series[0])
	}
	wantIDs := []string{
		"department-12", "department-13", "department-11", "department-10", "department-09", "department-08",
		"department-07", "department-06", "department-05", "department-04", "department-03", "department-02",
	}
	gotIDs := make([]string, 0, 12)
	for index, series := range trend.Series[1:] {
		gotIDs = append(gotIDs, series.DepartmentExternalID)
		if series.Rank != index+1 {
			t.Fatalf("series %q rank = %d, want %d", series.DepartmentExternalID, series.Rank, index+1)
		}
	}
	if !reflect.DeepEqual(gotIDs, wantIDs) {
		t.Fatalf("comparison IDs = %#v, want %#v", gotIDs, wantIDs)
	}
}

func TestRankTopMembersUsesSelectedWindowTokens(t *testing.T) {
	subjects := []representativescope.Subject{
		{SubjectType: "member", UserID: 1, DisplayName: "Alice", RelayUserID: intPtr(1001), Selectable: true},
		{SubjectType: "member", UserID: 2, DisplayName: "Bob", RelayUserID: intPtr(1002), Selectable: true},
	}
	stats := map[int64]relay.TeamUserUsageStats{
		1001: {UserID: 1001, TotalActualCost: 20},
		1002: {UserID: 1002, TotalActualCost: 40},
	}
	aliceTokens := int64(123)
	bobTokens := int64(456)
	totals := map[int64]overviewWindowTotal{
		1001: {ActualCost: 50, TotalTokens: &aliceTokens},
		1002: {ActualCost: 10, TotalTokens: &bobTokens},
	}
	top := RankTopMembers(subjects, stats, totals, 12)
	if got := []int{top[0].UserID, top[1].UserID}; !reflect.DeepEqual(got, []int{2, 1}) {
		t.Fatalf("ranked user ids = %#v, want Bob then Alice by selected-window tokens", got)
	}
	if top[0].RangeActualCost != 10 || top[1].RangeActualCost != 50 {
		t.Fatalf("ranked range costs = %#v, want 10 then 50", []float64{top[0].RangeActualCost, top[1].RangeActualCost})
	}
	if top[0].TotalTokens == nil || *top[0].TotalTokens != 456 {
		t.Fatalf("ranked total tokens = %#v, want selected-window token total", top[0].TotalTokens)
	}
}

func TestMergeResolvedOverviewSubjectsKeepsUnresolvedScopedMembers(t *testing.T) {
	oldRelayUserID := 1001
	newRelayUserID := 2001
	subjects := []representativescope.Subject{
		{SubjectType: "member", UserID: 1, DisplayName: "Alice", RelayUserID: &oldRelayUserID, Selectable: true},
		{SubjectType: "member", UserID: 2, DisplayName: "Bob", Selectable: false},
	}
	resolved := []representativescope.Subject{
		{SubjectType: "member", UserID: 1, DisplayName: "Alice", RelayUserID: &newRelayUserID, Selectable: true},
	}

	merged := MergeResolvedOverviewSubjects(subjects, resolved)
	if len(merged) != 2 {
		t.Fatalf("merged subjects length = %d, want 2", len(merged))
	}
	if merged[0].RelayUserID == nil || *merged[0].RelayUserID != 2001 {
		t.Fatalf("merged first relay_user_id = %#v, want 2001", merged[0].RelayUserID)
	}
	if merged[1].RelayUserID != nil || merged[1].UserID != 2 {
		t.Fatalf("merged second subject = %#v, want unresolved Bob", merged[1])
	}
}

func TestBuildOverviewDepartmentTrendComparesMultipleRepresentedRoots(t *testing.T) {
	alphaTokens := int64(100)
	betaTokens := int64(300)
	departments := []representativescope.DepartmentScope{
		{ExternalID: "department-alpha", Name: "Department Alpha", DisplayPath: "Department Alpha", ChildCount: 1},
		{ExternalID: "department-alpha-team-one", ParentExternalID: stringPtr("department-alpha"), Name: "Team One", DisplayPath: "Department Alpha / Team One", Depth: 1},
		{ExternalID: "department-beta", Name: "Department Beta", DisplayPath: "Department Beta", ChildCount: 1},
		{ExternalID: "department-beta-team-one", ParentExternalID: stringPtr("department-beta"), Name: "Team One", DisplayPath: "Department Beta / Team One", Depth: 1},
	}
	subjects := []representativescope.Subject{
		{SubjectType: "member", UserID: 1, DepartmentExternalID: "department-alpha-team-one", RelayUserID: intPtr(1001)},
		{SubjectType: "member", UserID: 2, DepartmentExternalID: "department-beta-team-one", RelayUserID: intPtr(1002)},
	}
	pointsByUser := map[int64][]relay.UsageTrendPoint{
		1001: {{Date: "2026-06-28", ActualCost: 1, TotalTokens: &alphaTokens}},
		1002: {{Date: "2026-06-28", ActualCost: 3, TotalTokens: &betaTokens}},
	}

	trend := BuildOverviewDepartmentTrend(departments, []string{"department-alpha", "department-beta"}, subjects, pointsByUser)

	if got := len(trend.Series); got != 3 {
		t.Fatalf("department trend series = %d, want team total plus two root groups: %#v", got, trend.Series)
	}
	if trend.Series[0].SeriesType != "team_total" {
		t.Fatalf("first series = %#v, want team_total", trend.Series[0])
	}
	if got := []string{trend.Series[1].DepartmentExternalID, trend.Series[2].DepartmentExternalID}; !reflect.DeepEqual(got, []string{"department-beta", "department-alpha"}) {
		t.Fatalf("department series ids = %#v, want represented roots sorted by tokens", got)
	}
	if got := []string{trend.Series[1].DisplayName, trend.Series[2].DisplayName}; !reflect.DeepEqual(got, []string{"Department Beta", "Department Alpha"}) {
		t.Fatalf("department series names = %#v, want root department names", got)
	}
}

func TestBuildOverviewDepartmentTrendComparesSecondLevelForSingleRepresentedRoot(t *testing.T) {
	teamOneTokens := int64(100)
	teamTwoTokens := int64(300)
	departments := []representativescope.DepartmentScope{
		{ExternalID: "department-alpha", Name: "Department Alpha", DisplayPath: "Department Alpha", ChildCount: 2},
		{ExternalID: "department-alpha-team-one", ParentExternalID: stringPtr("department-alpha"), Name: "Team One", DisplayPath: "Department Alpha / Team One", Depth: 1},
		{ExternalID: "department-alpha-team-two", ParentExternalID: stringPtr("department-alpha"), Name: "Team Two", DisplayPath: "Department Alpha / Team Two", Depth: 1},
	}
	subjects := []representativescope.Subject{
		{SubjectType: "member", UserID: 1, DepartmentExternalID: "department-alpha-team-one", RelayUserID: intPtr(1001)},
		{SubjectType: "member", UserID: 2, DepartmentExternalID: "department-alpha-team-two", RelayUserID: intPtr(1002)},
	}
	pointsByUser := map[int64][]relay.UsageTrendPoint{
		1001: {{Date: "2026-06-28", ActualCost: 1, TotalTokens: &teamOneTokens}},
		1002: {{Date: "2026-06-28", ActualCost: 3, TotalTokens: &teamTwoTokens}},
	}

	trend := BuildOverviewDepartmentTrend(departments, []string{"department-alpha"}, subjects, pointsByUser)

	if got := len(trend.Series); got != 3 {
		t.Fatalf("department trend series = %d, want team total plus two second-level groups: %#v", got, trend.Series)
	}
	if got := []string{trend.Series[1].DepartmentExternalID, trend.Series[2].DepartmentExternalID}; !reflect.DeepEqual(got, []string{"department-alpha-team-two", "department-alpha-team-one"}) {
		t.Fatalf("department series ids = %#v, want child teams sorted by tokens", got)
	}
}

func TestBuildOverviewDepartmentTrendExpandsChildrenAfterExtraneousRootIsRemoved(t *testing.T) {
	teamOneTokens := int64(100)
	teamTwoTokens := int64(300)
	extraneousTokens := int64(10)
	departments := []representativescope.DepartmentScope{
		{ExternalID: "department-primary", Name: "Department Primary", DisplayPath: "Department Primary", ChildCount: 2},
		{ExternalID: "department-primary-team-one", ParentExternalID: stringPtr("department-primary"), Name: "Team One", DisplayPath: "Department Primary / Team One", Depth: 1},
		{ExternalID: "department-primary-team-two", ParentExternalID: stringPtr("department-primary"), Name: "Team Two", DisplayPath: "Department Primary / Team Two", Depth: 1},
		{ExternalID: "department-extraneous", Name: "Department Extraneous", DisplayPath: "Department Extraneous"},
	}
	subjects := []representativescope.Subject{
		{SubjectType: "member", UserID: 1, DepartmentExternalID: "department-primary-team-one", RelayUserID: intPtr(1001)},
		{SubjectType: "member", UserID: 2, DepartmentExternalID: "department-primary-team-two", RelayUserID: intPtr(1002)},
		{SubjectType: "member", UserID: 3, DepartmentExternalID: "department-extraneous", RelayUserID: intPtr(1003)},
	}
	pointsByUser := map[int64][]relay.UsageTrendPoint{
		1001: {{Date: "2026-06-28", ActualCost: 1, TotalTokens: &teamOneTokens}},
		1002: {{Date: "2026-06-28", ActualCost: 3, TotalTokens: &teamTwoTokens}},
		1003: {{Date: "2026-06-28", ActualCost: 0.1, TotalTokens: &extraneousTokens}},
	}

	before := BuildOverviewDepartmentTrend(departments, []string{"department-primary", "department-extraneous"}, subjects, pointsByUser)
	if got := []string{before.Series[1].DepartmentExternalID, before.Series[2].DepartmentExternalID}; !reflect.DeepEqual(got, []string{"department-primary", "department-extraneous"}) {
		t.Fatalf("multi-root comparison ids = %#v, want represented roots", got)
	}

	after := BuildOverviewDepartmentTrend(departments[:3], []string{"department-primary"}, subjects[:2], pointsByUser)
	if got := []string{after.Series[1].DepartmentExternalID, after.Series[2].DepartmentExternalID}; !reflect.DeepEqual(got, []string{"department-primary-team-two", "department-primary-team-one"}) {
		t.Fatalf("single-root comparison ids = %#v, want primary child departments", got)
	}
}

func TestBuildOverviewDepartmentTrendSkipsSingleWrapperBeforeComparingSecondLevel(t *testing.T) {
	cloudTokens := int64(100)
	dataTokens := int64(300)
	departments := []representativescope.DepartmentScope{
		{ExternalID: "department-company", Name: "Company", DisplayPath: "Company", ChildCount: 1},
		{ExternalID: "department-rd", ParentExternalID: stringPtr("department-company"), Name: "R&D", DisplayPath: "Company / R&D", Depth: 1, ChildCount: 2},
		{ExternalID: "department-rd-cloud", ParentExternalID: stringPtr("department-rd"), Name: "Cloud Platform", DisplayPath: "Company / R&D / Cloud Platform", Depth: 2},
		{ExternalID: "department-rd-data", ParentExternalID: stringPtr("department-rd"), Name: "Data Team", DisplayPath: "Company / R&D / Data Team", Depth: 2},
	}
	subjects := []representativescope.Subject{
		{SubjectType: "member", UserID: 1, DepartmentExternalID: "department-rd-cloud", RelayUserID: intPtr(1001)},
		{SubjectType: "member", UserID: 2, DepartmentExternalID: "department-rd-data", RelayUserID: intPtr(1002)},
	}
	pointsByUser := map[int64][]relay.UsageTrendPoint{
		1001: {{Date: "2026-06-28", ActualCost: 1, TotalTokens: &cloudTokens}},
		1002: {{Date: "2026-06-28", ActualCost: 3, TotalTokens: &dataTokens}},
	}

	trend := BuildOverviewDepartmentTrend(departments, []string{"department-company"}, subjects, pointsByUser)

	if got := len(trend.Series); got != 3 {
		t.Fatalf("department trend series = %d, want team total plus two second-level groups under the only wrapper: %#v", got, trend.Series)
	}
	if got := []string{trend.Series[1].DepartmentExternalID, trend.Series[2].DepartmentExternalID}; !reflect.DeepEqual(got, []string{"department-rd-data", "department-rd-cloud"}) {
		t.Fatalf("department series ids = %#v, want second-level groups sorted by tokens", got)
	}
}

func TestBuildOverviewDepartmentTrendHandlesRepresentedRootWithUnscopedParent(t *testing.T) {
	cloudTokens := int64(100)
	dataTokens := int64(300)
	unscopedParentID := "department-company"
	departments := []representativescope.DepartmentScope{
		{ExternalID: "department-rd", ParentExternalID: &unscopedParentID, Name: "R&D", DisplayPath: "Company / R&D", Depth: 1, ChildCount: 2},
		{ExternalID: "department-rd-cloud", ParentExternalID: stringPtr("department-rd"), Name: "Cloud Platform", DisplayPath: "Company / R&D / Cloud Platform", Depth: 2},
		{ExternalID: "department-rd-data", ParentExternalID: stringPtr("department-rd"), Name: "Data Team", DisplayPath: "Company / R&D / Data Team", Depth: 2},
	}
	subjects := []representativescope.Subject{
		{SubjectType: "member", UserID: 1, DepartmentExternalID: "department-rd-cloud", RelayUserID: intPtr(1001)},
		{SubjectType: "member", UserID: 2, DepartmentExternalID: "department-rd-data", RelayUserID: intPtr(1002)},
	}
	pointsByUser := map[int64][]relay.UsageTrendPoint{
		1001: {{Date: "2026-06-28", ActualCost: 1, TotalTokens: &cloudTokens}},
		1002: {{Date: "2026-06-28", ActualCost: 3, TotalTokens: &dataTokens}},
	}

	trend := BuildOverviewDepartmentTrend(departments, []string{"department-rd"}, subjects, pointsByUser)

	if got := len(trend.Series); got != 3 {
		t.Fatalf("department trend series = %d, want team total plus two child groups below represented root with unscoped parent: %#v", got, trend.Series)
	}
	if got := []string{trend.Series[1].DepartmentExternalID, trend.Series[2].DepartmentExternalID}; !reflect.DeepEqual(got, []string{"department-rd-data", "department-rd-cloud"}) {
		t.Fatalf("department series ids = %#v, want child groups sorted by tokens", got)
	}
}

func TestBuildOverviewDepartmentTrendKeepsSingleLeafRootIndependent(t *testing.T) {
	aliceTokens := int64(100)
	bobTokens := int64(300)
	departments := []representativescope.DepartmentScope{
		{ExternalID: "department-alpha", Name: "Department Alpha", DisplayPath: "Department Alpha"},
	}
	subjects := []representativescope.Subject{
		{SubjectType: "member", UserID: 1, DepartmentExternalID: "department-alpha", RelayUserID: intPtr(1001)},
		{SubjectType: "member", UserID: 2, DepartmentExternalID: "department-alpha", RelayUserID: intPtr(1002)},
	}
	pointsByUser := map[int64][]relay.UsageTrendPoint{
		1001: {{Date: "2026-06-28", ActualCost: 1, TotalTokens: &aliceTokens}},
		1002: {{Date: "2026-06-28", ActualCost: 3, TotalTokens: &bobTokens}},
	}

	trend := BuildOverviewDepartmentTrend(departments, []string{"department-alpha"}, subjects, pointsByUser)

	if got := len(trend.Series); got != 1 {
		t.Fatalf("department trend series = %d, want only independent team total: %#v", got, trend.Series)
	}
	if trend.Series[0].SeriesType != "team_total" || trend.Series[0].Points[0].TotalTokens == nil || *trend.Series[0].Points[0].TotalTokens != 400 {
		t.Fatalf("team total series = %#v, want independent total tokens 400", trend.Series[0])
	}
}

func TestBuildOverviewDepartmentTrendUsesMembershipBucketsAndDeduplicatesTeamTotal(t *testing.T) {
	tokens := int64(100)
	departments := []representativescope.DepartmentScope{
		{ExternalID: "department-root", Name: "Root", DisplayPath: "Root", ChildCount: 2},
		{ExternalID: "department-alpha", ParentExternalID: stringPtr("department-root"), Name: "Alpha", DisplayPath: "Root / Alpha", Depth: 1},
		{ExternalID: "department-beta", ParentExternalID: stringPtr("department-root"), Name: "Beta", DisplayPath: "Root / Beta", Depth: 1},
	}
	subjects := []representativescope.Subject{
		{
			SubjectType:           "member",
			UserID:                1,
			DepartmentExternalID:  "department-alpha",
			DepartmentExternalIDs: []string{"department-alpha", "department-beta"},
			RelayUserID:           intPtr(1001),
		},
	}
	pointsByUser := map[int64][]relay.UsageTrendPoint{
		1001: {{Date: "2026-06-28", ActualCost: 1, TotalTokens: &tokens}},
	}

	trend := BuildOverviewDepartmentTrend(departments, []string{"department-root"}, subjects, pointsByUser)

	if got := len(trend.Series); got != 3 {
		t.Fatalf("department trend series = %d, want team total plus two membership buckets: %#v", got, trend.Series)
	}
	if trend.Series[0].SeriesType != "team_total" || trend.Series[0].Points[0].TotalTokens == nil || *trend.Series[0].Points[0].TotalTokens != 100 {
		t.Fatalf("team total series = %#v, want de-duplicated total tokens 100", trend.Series[0])
	}
	if got := []string{trend.Series[1].DepartmentExternalID, trend.Series[2].DepartmentExternalID}; !reflect.DeepEqual(got, []string{"department-alpha", "department-beta"}) {
		t.Fatalf("department series ids = %#v, want both membership buckets", got)
	}
	for _, series := range trend.Series[1:] {
		if series.Points[0].TotalTokens == nil || *series.Points[0].TotalTokens != 100 {
			t.Fatalf("membership series = %#v, want Alice counted in each department bucket", series)
		}
	}
}
