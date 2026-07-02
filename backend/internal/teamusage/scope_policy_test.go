package teamusage

import (
	"reflect"
	"testing"

	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/ai-efficiency/backend/internal/representativescope"
)

func TestOverviewScopeTooLargeDoesNotRankTruncatedTop12(t *testing.T) {
	subjects := make([]representativescope.Subject, 0, 501)
	for i := 0; i < 501; i++ {
		relayUserID := 1000 + i
		subjects = append(subjects, representativescope.Subject{
			SubjectType: "member",
			UserID:      i + 1,
			RelayUserID: &relayUserID,
			Selectable:  true,
		})
	}
	state := BuildOverviewUnavailableForLargeScope(subjects, 500)
	if !state.Summary.Unavailable || state.Summary.UnavailableReason == nil || *state.Summary.UnavailableReason != "scope_too_large" {
		t.Fatalf("summary unavailable = %#v, want scope_too_large", state.Summary)
	}
	if !state.TopMemberTrend.Unavailable || len(state.TopMembers) != 0 {
		t.Fatalf("top member trend = %#v top_members=%#v, want unavailable with no ranking", state.TopMemberTrend, state.TopMembers)
	}
	if state.TopMemberTrend.RankBasis != "range_total_tokens" {
		t.Fatalf("rank basis = %q, want range_total_tokens", state.TopMemberTrend.RankBasis)
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

func TestBuildOverviewMemberTreeKeepsEmptyMemberSlicesNonNil(t *testing.T) {
	departments := []representativescope.DepartmentScope{
		{ExternalID: "dept-alpha", Name: "Department Alpha", DisplayPath: "Department Alpha", ChildCount: 1},
		{ExternalID: "dept-alpha-empty", ParentExternalID: stringPtr("dept-alpha"), Name: "Empty Team", DisplayPath: "Department Alpha / Empty Team", Depth: 1},
	}
	members := []OverviewMember{
		{UserID: 1, DisplayName: "Alice", DepartmentExternalID: "dept-alpha"},
	}

	tree := BuildOverviewMemberTree(departments, []string{"dept-alpha"}, members)

	if len(tree) != 1 || len(tree[0].Children) != 1 {
		t.Fatalf("tree = %#v, want root with one child", tree)
	}
	if tree[0].Members == nil {
		t.Fatalf("root members slice is nil, want empty or populated slice")
	}
	if tree[0].Children[0].Members == nil {
		t.Fatalf("empty child members slice is nil, want non-nil empty slice")
	}
}

func TestBuildOverviewMemberTreeDisplaysMultiDepartmentMemberInEachMembershipWithoutDuplicatingParentTotals(t *testing.T) {
	tokens := int64(1200)
	departments := []representativescope.DepartmentScope{
		{ExternalID: "dept-root", Name: "Department Root", DisplayPath: "Department Root", ChildCount: 2},
		{ExternalID: "dept-alpha", ParentExternalID: stringPtr("dept-root"), Name: "Alpha", DisplayPath: "Department Root / Alpha", Depth: 1},
		{ExternalID: "dept-beta", ParentExternalID: stringPtr("dept-root"), Name: "Beta", DisplayPath: "Department Root / Beta", Depth: 1},
	}
	members := []OverviewMember{
		{
			UserID:                1,
			DisplayName:           "Alice",
			Email:                 "alice@example.com",
			DepartmentExternalID:  "dept-alpha",
			DepartmentExternalIDs: []string{"dept-alpha", "dept-beta"},
			RelayUserID:           intPtr(1001),
			RangeActualCost:       12,
			TotalTokens:           &tokens,
		},
	}

	tree := BuildOverviewMemberTree(departments, []string{"dept-root"}, members)

	if len(tree) != 1 || len(tree[0].Children) != 2 {
		t.Fatalf("tree = %#v, want root with alpha and beta children", tree)
	}
	root := tree[0]
	if root.MemberCount != 1 || root.ConnectedMemberCount != 1 || root.RangeActualCost != 12 {
		t.Fatalf("root totals = members %d connected %d cost %.2f, want de-duplicated 1 / 1 / 12", root.MemberCount, root.ConnectedMemberCount, root.RangeActualCost)
	}
	if root.RangeTotalTokens == nil || *root.RangeTotalTokens != 1200 {
		t.Fatalf("root tokens = %#v, want 1200", root.RangeTotalTokens)
	}
	if len(root.Children[0].Members) != 1 || len(root.Children[1].Members) != 1 {
		t.Fatalf("child members = %#v / %#v, want Alice displayed in both departments", root.Children[0].Members, root.Children[1].Members)
	}
}
