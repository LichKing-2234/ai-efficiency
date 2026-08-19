package relayplanning

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/ai-efficiency/backend/internal/relay"
)

func TestPreviewRequestJSONUsesSnakeCase(t *testing.T) {
	var got PreviewRequest
	if err := json.Unmarshal([]byte(`{"provider_id":7,"department_id":"dept-alpha","platform":"openai","source_group_id":42,"weekly_cost_target":12.5,"group_count":2,"selected_user_ids":[1,2],"existing_mapping_id":9}`), &got); err != nil {
		t.Fatalf("unmarshal preview request: %v", err)
	}
	if got.ProviderID != 7 || got.DepartmentID != "dept-alpha" || got.Platform != "openai" || got.SourceGroupID != 42 || got.WeeklyCostTarget != 12.5 || got.GroupCount != 2 || len(got.SelectedUserIDs) != 2 || got.ExistingMappingID != 9 {
		t.Fatalf("decoded preview request = %#v", got)
	}
}

func TestAllocatePlacesNextMemberInLowestCostGroup(t *testing.T) {
	candidates := []Candidate{
		{UserID: 1, RangeCost: 8, Eligible: true},
		{UserID: 2, RangeCost: 7, Eligible: true},
		{UserID: 3, RangeCost: 4, Eligible: true},
		{UserID: 4, RangeCost: 3, Eligible: true},
	}
	got := allocate(candidates, 2)
	if len(got) != 2 {
		t.Fatalf("assignment count = %d, want 2", len(got))
	}
	if got[0].TotalCost != 11 || got[1].TotalCost != 11 {
		t.Fatalf("balanced costs = %.1f/%.1f, want 11/11", got[0].TotalCost, got[1].TotalCost)
	}
	if len(got[0].UserIDs) != 2 || len(got[1].UserIDs) != 2 {
		t.Fatalf("user distribution = %#v, want two users per group", got)
	}
}

func TestAllocateUsesStableGroupTieBreak(t *testing.T) {
	got := allocate([]Candidate{{UserID: 2, RangeCost: 1}, {UserID: 1, RangeCost: 1}}, 2)
	if got[0].UserIDs[0] != 2 || got[1].UserIDs[0] != 1 {
		t.Fatalf("tie break assignments = %#v, want insertion order across empty groups", got)
	}
}

func TestAllocateSerializesEmptyGroupsAsEmptyUserLists(t *testing.T) {
	got, err := json.Marshal(allocate(nil, 2))
	if err != nil {
		t.Fatalf("marshal assignments: %v", err)
	}
	if string(got) != `[{"index":0,"total_cost":0,"user_ids":[]},{"index":1,"total_cost":0,"user_ids":[]}]` {
		t.Fatalf("assignments JSON = %s, want empty user arrays", got)
	}
}

func TestResolveGroupCountCapsRecommendationAtEligibleMembers(t *testing.T) {
	candidates := []Candidate{
		{UserID: 1, RangeCost: 10000},
		{UserID: 2, RangeCost: 10000},
		{UserID: 3, RangeCost: 10000},
		{UserID: 4, RangeCost: 10000},
	}
	recommended, count := resolveGroupCount(PreviewRequest{WeeklyCostTarget: 2500, GroupCount: 2}, candidates)
	if recommended != 4 || count != 4 {
		t.Fatalf("group counts = recommended %d / planned %d, want 4 / 4", recommended, count)
	}
}

func TestResolveGroupCountAllowsExplicitReplanResize(t *testing.T) {
	candidates := []Candidate{{UserID: 1, RangeCost: 10000}, {UserID: 2, RangeCost: 10000}}
	recommended, count := resolveGroupCount(PreviewRequest{WeeklyCostTarget: 2500, GroupCount: 1, ExistingMappingID: 9}, candidates)
	if recommended != 2 || count != 1 {
		t.Fatalf("group counts = recommended %d / planned %d, want 2 / 1", recommended, count)
	}
}

func TestProposedGroupNamesFollowRelayCopySequence(t *testing.T) {
	groups := []relay.Group{
		{Name: "Group Alpha"},
		{Name: "Group Alpha (Copy)"},
		{Name: "Group Alpha (Copy 3)"},
	}
	got := proposedGroupNames("Group Alpha", groups, 3)
	want := []string{"Group Alpha (Copy 2)", "Group Alpha (Copy 4)", "Group Alpha (Copy 5)"}
	if len(got) != len(want) {
		t.Fatalf("proposed names = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("proposed names = %#v, want %#v", got, want)
		}
	}
}

func TestMergeTrendUsageFillsMissingRangeTokens(t *testing.T) {
	stats := map[int64]relay.TeamUserUsageStats{
		101: {UserID: 101},
		102: {UserID: 102, RangeTotalTokens: int64Pointer(9)},
	}
	firstTokens, secondTokens := int64(120), int64(80)
	mergeTrendUsage(stats, map[int64][]relay.UsageTrendPoint{
		101: {
			{Date: "2026-08-01", TotalTokens: &firstTokens, ActualCost: 1.25},
			{Date: "2026-08-02", TotalTokens: &secondTokens, ActualCost: 2.75},
		},
	})
	if stats[101].RangeTotalTokens == nil || *stats[101].RangeTotalTokens != 200 {
		t.Fatalf("range tokens = %#v, want 200", stats[101].RangeTotalTokens)
	}
	if stats[101].RangeActualCost == nil || *stats[101].RangeActualCost != 4 {
		t.Fatalf("range cost = %#v, want 4", stats[101].RangeActualCost)
	}
	if *stats[102].RangeTotalTokens != 9 {
		t.Fatalf("existing range tokens overwritten: %d", *stats[102].RangeTotalTokens)
	}
}

type usageStatsTestProvider struct {
	relay.Provider
	trend map[int64][]relay.UsageTrendPoint
}

func (p usageStatsTestProvider) GetBatchUserUsageStats(_ context.Context, ids []int64, _ relay.TeamUsageSummaryParams) (map[int64]relay.TeamUserUsageStats, error) {
	stats := make(map[int64]relay.TeamUserUsageStats, len(ids))
	for _, id := range ids {
		cost := 7.5
		stats[id] = relay.TeamUserUsageStats{UserID: id, RangeActualCost: &cost}
	}
	return stats, nil
}

func (p usageStatsTestProvider) GetUsageTrendForUsers(_ context.Context, _ []int64, _ relay.TeamMemberTrendParams) (map[int64][]relay.UsageTrendPoint, error) {
	return p.trend, nil
}

func TestUsageStatsUsesTrendTokensWhenBatchSummaryOmitsThem(t *testing.T) {
	first, second := int64(120), int64(80)
	provider := usageStatsTestProvider{trend: map[int64][]relay.UsageTrendPoint{
		101: {{TotalTokens: &first}, {TotalTokens: &second}},
	}}
	got, err := usageStats(context.Background(), provider, []int64{101})
	if err != nil {
		t.Fatalf("usageStats() error = %v", err)
	}
	if got[101].RangeTotalTokens == nil || *got[101].RangeTotalTokens != 200 {
		t.Fatalf("range tokens = %#v, want 200", got[101].RangeTotalTokens)
	}
}

func int64Pointer(value int64) *int64 {
	return &value
}
