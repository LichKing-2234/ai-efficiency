package teamusage

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/ai-efficiency/backend/internal/representativescope"
)

func TestMembersCompleteRangeAggregateRunsAfterAllStatsChunks(t *testing.T) {
	scope, base := membersTestData(205)
	for relayUserID, stat := range base.summaryStats {
		stat.RangeActualCost = nil
		stat.RangeTotalTokens = nil
		base.summaryStats[relayUserID] = stat
	}
	provider := &completeRangeProbeProvider{fakeRelayProvider: base}
	service := &Service{fullScopeCap: 500}

	snapshot, err := service.generateMembersSnapshot(context.Background(), scope, provider, testMembersParams().OverviewParams)
	if err != nil {
		t.Fatalf("generateMembersSnapshot() error = %v", err)
	}
	if len(provider.statsRequestIDs) != 3 {
		t.Fatalf("stats requests = %d, want three chunks", len(provider.statsRequestIDs))
	}
	for index, params := range provider.statsRequestParams {
		if params.RequireCompleteRange {
			t.Fatalf("stats request %d retained chunk-local complete-range fallback", index)
		}
	}
	if len(provider.trendRequestIDs) != 1 || provider.statsCallsAtTrend[0] != 3 {
		t.Fatalf("trend requests/stats completion = %v/%v, want one trend after all three stats chunks", provider.trendRequestIDs, provider.statsCallsAtTrend)
	}
	if !reflect.DeepEqual(provider.trendRequestIDs[0], relayIDsFromOneTo(10001, 205)) {
		t.Fatalf("trend request IDs = %v, want complete represented scope", provider.trendRequestIDs[0])
	}
	if len(snapshot.Members) != 205 || snapshot.Members[0].TotalTokens == nil || *snapshot.Members[0].TotalTokens != 1000 {
		t.Fatalf("completed members snapshot = len %d first %#v", len(snapshot.Members), snapshot.Members[0])
	}
}

func TestCompleteRangeFailureStaysSoft(t *testing.T) {
	scope, base := membersTestData(2)
	for relayUserID, stat := range base.summaryStats {
		stat.RangeActualCost = nil
		stat.RangeTotalTokens = nil
		base.summaryStats[relayUserID] = stat
	}
	provider := &completeRangeProbeProvider{
		fakeRelayProvider: base,
		aggregateTrendErr: errors.New("synthetic users-trend outage"),
	}
	service := &Service{fullScopeCap: 500}

	snapshot, err := service.generateSummarySnapshot(context.Background(), scope, provider, testMembersParams().OverviewParams)
	if err != nil {
		t.Fatalf("generateSummarySnapshot() error = %v, want soft range completion", err)
	}
	if !snapshot.Summary.Unavailable || snapshot.Summary.UnavailableReason == nil || *snapshot.Summary.UnavailableReason != "range_aggregation_unavailable" {
		t.Fatalf("summary = %+v, want section-local range_aggregation_unavailable", snapshot.Summary)
	}
	if len(provider.trendRequestIDs) != 1 || !reflect.DeepEqual(provider.trendRequestIDs[0], []int64{10001, 10002}) {
		t.Fatalf("soft-failed trend requests = %v, want one full-scope attempt", provider.trendRequestIDs)
	}
}

func TestCompleteRangeMapsAuthorizedMissingTrendUserToZero(t *testing.T) {
	provider := &completeRangeProbeProvider{fakeRelayProvider: &fakeRelayProvider{
		summaryStats: map[int64]relay.TeamUserUsageStats{101: {UserID: 101}},
		trendPoints:  map[int64][]relay.UsageTrendPoint{},
	}}

	stats, err := (&Service{}).loadTeamUsageStats(
		context.Background(), provider, []int64{101}, []int64{101},
		relay.TeamUsageSummaryParams{RequireCompleteRange: true},
	)
	if err != nil {
		t.Fatalf("loadTeamUsageStats() error = %v", err)
	}
	if stats[101].RangeActualCost == nil || *stats[101].RangeActualCost != 0 ||
		stats[101].RangeTotalTokens == nil || *stats[101].RangeTotalTokens != 0 {
		t.Fatalf("zero-usage completed range = %#v, want explicit 0/0", stats[101])
	}
}

func TestCompleteRangePreservesNilTokensWhenAnyTrendPointOmitsThem(t *testing.T) {
	provider := &completeRangeProbeProvider{fakeRelayProvider: &fakeRelayProvider{
		summaryStats: map[int64]relay.TeamUserUsageStats{101: {UserID: 101}},
		trendPoints: map[int64][]relay.UsageTrendPoint{101: {
			{Date: "2026-07-01", ActualCost: 1, TotalTokens: int64Ptr(10)},
			{Date: "2026-07-02", ActualCost: 2},
		}},
	}}

	stats, err := (&Service{}).loadTeamUsageStats(
		context.Background(), provider, []int64{101}, []int64{101},
		relay.TeamUsageSummaryParams{RequireCompleteRange: true},
	)
	if err != nil {
		t.Fatalf("loadTeamUsageStats() error = %v", err)
	}
	if stats[101].RangeActualCost == nil || *stats[101].RangeActualCost != 3 {
		t.Fatalf("completed range cost = %#v, want 3", stats[101].RangeActualCost)
	}
	if stats[101].RangeTotalTokens != nil {
		t.Fatalf("completed range tokens = %#v, want nil for incomplete trend tokens", stats[101].RangeTotalTokens)
	}
}

func TestOrganizationUsesBranchStatsAndFullRepresentedAggregateTrend(t *testing.T) {
	rootID, childID, siblingID := "department-root", "department-child", "department-sibling"
	scope := &representativescope.Scope{
		Version: "scope-v1", ActorUserID: 1, IsRepresentative: true,
		MemberTreeRootIDs: []string{rootID, siblingID},
		MemberTreeDepartments: []representativescope.DepartmentScope{
			{ExternalID: rootID, Name: "Root", ChildCount: 1},
			{ExternalID: childID, ParentExternalID: &rootID, Name: "Child"},
			{ExternalID: siblingID, Name: "Sibling"},
		},
		OverviewSubjects: []representativescope.Subject{
			organizationSubject(1, 101, "member-alice", "alice@example.com", childID, []string{childID}),
			organizationSubject(2, 102, "member-bob", "bob@example.org", rootID, []string{rootID}),
			organizationSubject(3, 999, "member-carol", "carol@example.net", siblingID, []string{siblingID}),
		},
	}
	provider := &completeRangeProbeProvider{fakeRelayProvider: &fakeRelayProvider{
		summaryStats: map[int64]relay.TeamUserUsageStats{
			101: {UserID: 101},
			102: {UserID: 102},
			999: {UserID: 999},
		},
		trendPoints: map[int64][]relay.UsageTrendPoint{
			101: {{Date: "2026-07-07", ActualCost: 1, TotalTokens: int64Ptr(10)}},
			102: {{Date: "2026-07-07", ActualCost: 2, TotalTokens: int64Ptr(20)}},
			999: {{Date: "2026-07-07", ActualCost: 9, TotalTokens: int64Ptr(90)}},
		},
	}}
	branch, found := selectOrganizationBranch(scope, rootID)
	if !found {
		t.Fatal("selectOrganizationBranch() did not find authorized root")
	}
	service := &Service{fullScopeCap: 500}

	snapshot, err := service.generateOrganizationSnapshot(context.Background(), branch, provider, testMembersParams().OverviewParams)
	if err != nil {
		t.Fatalf("generateOrganizationSnapshot() error = %v", err)
	}
	if !reflect.DeepEqual(provider.statsRequestIDs, [][]int64{{101, 102}}) {
		t.Fatalf("organization stats IDs = %v, want requested branch only", provider.statsRequestIDs)
	}
	if len(provider.trendRequestIDs) != 1 || !reflect.DeepEqual(provider.trendRequestIDs[0], []int64{101, 102, 999}) {
		t.Fatalf("organization aggregate trend IDs = %v, want full represented scope", provider.trendRequestIDs)
	}
	if len(snapshot.Departments) != 1 || snapshot.Departments[0].RangeTotalTokens == nil || *snapshot.Departments[0].RangeTotalTokens != 10 {
		t.Fatalf("completed child aggregate = %+v, want branch total tokens 10", snapshot.Departments)
	}
}

type completeRangeProbeProvider struct {
	*fakeRelayProvider
	statsRequestIDs    [][]int64
	statsRequestParams []relay.TeamUsageSummaryParams
	trendRequestIDs    [][]int64
	statsCallsAtTrend  []int
	aggregateTrendErr  error
}

func (p *completeRangeProbeProvider) GetBatchUserUsageStats(ctx context.Context, relayUserIDs []int64, params relay.TeamUsageSummaryParams) (map[int64]relay.TeamUserUsageStats, error) {
	p.statsRequestIDs = append(p.statsRequestIDs, append([]int64(nil), relayUserIDs...))
	p.statsRequestParams = append(p.statsRequestParams, params)
	stats := make(map[int64]relay.TeamUserUsageStats, len(relayUserIDs))
	for _, relayUserID := range relayUserIDs {
		if stat, ok := p.summaryStats[relayUserID]; ok {
			stats[relayUserID] = stat
		}
	}
	if params.RequireCompleteRange {
		_, _ = p.GetUsageTrendForUsers(ctx, relayUserIDs, relay.TeamMemberTrendParams{
			StartDate: params.StartDate, EndDate: params.EndDate, Granularity: params.Granularity, Timezone: params.Timezone,
		})
	}
	return stats, nil
}

func (p *completeRangeProbeProvider) GetUsageTrendForUsers(_ context.Context, relayUserIDs []int64, _ relay.TeamMemberTrendParams) (map[int64][]relay.UsageTrendPoint, error) {
	p.trendRequestIDs = append(p.trendRequestIDs, append([]int64(nil), relayUserIDs...))
	p.statsCallsAtTrend = append(p.statsCallsAtTrend, len(p.statsRequestIDs))
	if p.aggregateTrendErr != nil {
		return nil, p.aggregateTrendErr
	}
	points := make(map[int64][]relay.UsageTrendPoint, len(relayUserIDs))
	for _, relayUserID := range relayUserIDs {
		points[relayUserID] = append([]relay.UsageTrendPoint(nil), p.trendPoints[relayUserID]...)
	}
	return points, nil
}

func relayIDsFromOneTo(first int64, count int) []int64 {
	ids := make([]int64, count)
	for index := range ids {
		ids[index] = first + int64(index)
	}
	return ids
}
