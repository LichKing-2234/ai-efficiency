package handler

import (
	"context"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/internal/activity"
	"github.com/ai-efficiency/backend/internal/personalusage"
	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/ai-efficiency/backend/internal/teamusage"
	"github.com/ai-efficiency/backend/internal/testdb"
)

type fakePersonalMetrics struct {
	snapshot *personalusage.Snapshot
	request  personalusage.Request
}

func (f *fakePersonalMetrics) Dashboard(_ context.Context, request personalusage.Request) (*personalusage.Snapshot, error) {
	f.request = request
	return f.snapshot, nil
}

type fakeTeamMetrics struct {
	response *teamusage.DepartmentSummaryResponse
}

func (f *fakeTeamMetrics) DepartmentSummary(context.Context, int, string, teamusage.OverviewParams) (*teamusage.DepartmentSummaryResponse, error) {
	return f.response, nil
}

func TestActivityDenominatorUsesExactFreshPersonalUsage(t *testing.T) {
	now := time.Date(2026, 8, 12, 1, 0, 0, 0, time.UTC)
	usage := &fakePersonalMetrics{snapshot: &personalusage.Snapshot{Configured: true, Range: relay.UserUsageDashboardRange{StartDate: "2026-08-01", EndDate: "2026-08-07", Granularity: "day", Timezone: "Asia/Shanghai"}, Stats: &relay.UserUsageDashboardStats{TotalTokens: 900}, UsageFreshness: &personalusage.UsageFreshness{AsOf: now.Add(-time.Second), FreshUntil: now.Add(time.Minute), SourceStatus: "ok"}}}
	resolver := &activityDenominatorResolver{personal: usage, now: func() time.Time { return now }}
	result, err := resolver.ResolveDenominator(context.Background(), activity.V2DenominatorRequest{ActorUserID: 1, Scope: activity.V2ScopeMember, SubjectUserID: 2, FromDate: "2026-08-01", ToDate: "2026-08-07", Timezone: "Asia/Shanghai"})
	if err != nil {
		t.Fatal(err)
	}
	if result.TotalTokens != 900 || !result.Fresh || !result.Complete || usage.request.UserID != 2 || usage.request.IncludeGroupQuotas {
		t.Fatalf("result/request = %+v/%+v", result, usage.request)
	}
}

func TestActivityDenominatorRejectsStaleOrScopeMismatchedUsage(t *testing.T) {
	now := time.Date(2026, 8, 12, 1, 0, 0, 0, time.UTC)
	personal := &fakePersonalMetrics{snapshot: &personalusage.Snapshot{Configured: true, Range: relay.UserUsageDashboardRange{StartDate: "2026-08-01", EndDate: "2026-08-07", Granularity: "day", Timezone: "UTC"}, Stats: &relay.UserUsageDashboardStats{TotalTokens: 9}, UsageFreshness: &personalusage.UsageFreshness{AsOf: now.Add(-time.Hour), FreshUntil: now.Add(-time.Second), SourceStatus: "ok"}}}
	resolver := &activityDenominatorResolver{personal: personal, now: func() time.Time { return now }}
	result, err := resolver.ResolveDenominator(context.Background(), activity.V2DenominatorRequest{ActorUserID: 1, Scope: activity.V2ScopePersonal, FromDate: "2026-08-01", ToDate: "2026-08-07", Timezone: "UTC"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Fresh || !result.Complete {
		t.Fatalf("stale denominator = %+v", result)
	}
	tokens := int64(10)
	team := &fakeTeamMetrics{response: &teamusage.DepartmentSummaryResponse{ScopeVersion: "old", SnapshotFreshness: teamusage.SnapshotFreshness{AsOf: now, FreshUntil: now.Add(time.Minute), SourceStatus: "ok"}, RangeTotalTokens: &tokens}}
	resolver = &activityDenominatorResolver{team: team, now: func() time.Time { return now }}
	result, err = resolver.ResolveDenominator(context.Background(), activity.V2DenominatorRequest{ActorUserID: 1, Scope: activity.V2ScopeTeam, ScopeVersion: "new"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Complete {
		t.Fatalf("scope-mismatched denominator = %+v", result)
	}
}

func TestActivityDenominatorFailsClosedForUncoveredProviderSet(t *testing.T) {
	client := testdb.Open(t)
	ctx := context.Background()
	for index := 0; index < 2; index++ {
		client.RelayProvider.Create().SetName("provider-" + string(rune('a'+index))).SetDisplayName("Provider").SetBaseURL("https://relay.example.com").SetAdminAPIKey("encrypted-test-key").SetDefaultModel("example-model").SetIsPrimary(index == 0).SetEnabled(true).SaveX(ctx)
	}
	resolver := &activityDenominatorResolver{client: client, personal: &fakePersonalMetrics{snapshot: &personalusage.Snapshot{Configured: true}}}
	result, err := resolver.ResolveDenominator(ctx, activity.V2DenominatorRequest{Scope: activity.V2ScopePersonal})
	if err != nil {
		t.Fatal(err)
	}
	if result.Complete || result.Fresh || result.TotalTokens != 0 {
		t.Fatalf("multi-provider denominator=%+v", result)
	}
}
