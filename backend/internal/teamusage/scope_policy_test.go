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
}

func TestRankTopMembersUsesCompleteScopedStats(t *testing.T) {
	subjects := []representativescope.Subject{
		{SubjectType: "member", UserID: 1, DisplayName: "Alice", RelayUserID: intPtr(1001), Selectable: true},
		{SubjectType: "member", UserID: 2, DisplayName: "Bob", RelayUserID: intPtr(1002), Selectable: true},
	}
	stats := map[int64]relay.TeamUserUsageStats{
		1001: {UserID: 1001, Last30dActualCost: 20},
		1002: {UserID: 1002, Last30dActualCost: 40},
	}
	top := RankTopMembers(subjects, stats, 12)
	if got := []int{top[0].UserID, top[1].UserID}; !reflect.DeepEqual(got, []int{2, 1}) {
		t.Fatalf("ranked user ids = %#v, want Bob then Alice", got)
	}
}
