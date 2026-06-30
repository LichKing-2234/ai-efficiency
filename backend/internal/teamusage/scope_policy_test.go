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
	if state.TopMemberTrend.RankBasis != "range_actual_cost" {
		t.Fatalf("rank basis = %q, want range_actual_cost", state.TopMemberTrend.RankBasis)
	}
}

func TestRankTopMembersUsesSelectedWindowTotals(t *testing.T) {
	subjects := []representativescope.Subject{
		{SubjectType: "member", UserID: 1, DisplayName: "Alice", RelayUserID: intPtr(1001), Selectable: true},
		{SubjectType: "member", UserID: 2, DisplayName: "Bob", RelayUserID: intPtr(1002), Selectable: true},
	}
	stats := map[int64]relay.TeamUserUsageStats{
		1001: {UserID: 1001, TotalActualCost: 20},
		1002: {UserID: 1002, TotalActualCost: 40},
	}
	tokens := int64(123)
	totals := map[int64]overviewWindowTotal{
		1001: {ActualCost: 50, TotalTokens: &tokens},
		1002: {ActualCost: 10},
	}
	top := RankTopMembers(subjects, stats, totals, 12)
	if got := []int{top[0].UserID, top[1].UserID}; !reflect.DeepEqual(got, []int{1, 2}) {
		t.Fatalf("ranked user ids = %#v, want Alice then Bob by selected-window total", got)
	}
	if top[0].RangeActualCost != 50 || top[1].RangeActualCost != 10 {
		t.Fatalf("ranked range costs = %#v, want 50 then 10", []float64{top[0].RangeActualCost, top[1].RangeActualCost})
	}
	if top[0].TotalTokens == nil || *top[0].TotalTokens != 123 {
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
