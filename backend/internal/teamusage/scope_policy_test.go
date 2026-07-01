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
