package teamusage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/ai-efficiency/backend/internal/representativescope"
	"github.com/ai-efficiency/backend/internal/testdb"
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

func TestPrewarmEquivalenceMatchesExactPublicLanesCursorsFreshnessAndKeys(t *testing.T) {
	now := time.Date(2026, 7, 21, 8, 0, 0, 0, time.UTC)
	exact, exactProvider, exactScope, exactKeys := newPrewarmEquivalenceService(t, now, false)
	prewarmed, prewarmProvider, prewarmScope, prewarmKeys := newPrewarmEquivalenceService(t, now, true)
	params := prewarmReader7dParams()

	exactRequest, err := exact.newSplitReadRequest(context.Background(), 1, params)
	if err != nil {
		t.Fatalf("exact newSplitReadRequest() error = %v", err)
	}
	prewarmRequest, err := prewarmed.newSplitReadRequest(context.Background(), 1, params)
	if err != nil {
		t.Fatalf("prewarm newSplitReadRequest() error = %v", err)
	}
	if !reflect.DeepEqual(exactRequest.snapshotCacheKey(), prewarmRequest.snapshotCacheKey()) {
		t.Fatalf("response cache dimensions differ: exact=%#v prewarm=%#v", exactRequest.snapshotCacheKey(), prewarmRequest.snapshotCacheKey())
	}
	wantResponseKeys := prewarmEquivalenceResponseKeys(t, exactRequest.snapshotCacheKey(), "department-alpha")

	exactSummary, err := exact.Summary(context.Background(), 1, params)
	if err != nil {
		t.Fatalf("exact Summary() error = %v", err)
	}
	prewarmSummary, err := prewarmed.Summary(context.Background(), 1, params)
	if err != nil {
		t.Fatalf("prewarm Summary() error = %v", err)
	}
	assertPrewarmEquivalentJSON(t, "Summary", exactSummary, prewarmSummary)
	assertPrewarmFreshness(t, "Summary exact miss", exactSummary.SnapshotFreshness, "miss")
	assertPrewarmFreshness(t, "Summary prewarm miss", prewarmSummary.SnapshotFreshness, "miss")

	exactTrend, err := exact.Trend(context.Background(), 1, params)
	if err != nil {
		t.Fatalf("exact Trend() error = %v", err)
	}
	prewarmTrend, err := prewarmed.Trend(context.Background(), 1, params)
	if err != nil {
		t.Fatalf("prewarm Trend() error = %v", err)
	}
	assertPrewarmEquivalentJSON(t, "Trend", exactTrend, prewarmTrend)
	assertPrewarmFreshness(t, "Trend exact miss", exactTrend.SnapshotFreshness, "miss")
	assertPrewarmFreshness(t, "Trend prewarm miss", prewarmTrend.SnapshotFreshness, "miss")

	membersParams := MembersParams{OverviewParams: params, Limit: 1}
	exactMembers, err := exact.Members(context.Background(), 1, membersParams)
	if err != nil {
		t.Fatalf("exact Members() error = %v", err)
	}
	prewarmMembers, err := prewarmed.Members(context.Background(), 1, membersParams)
	if err != nil {
		t.Fatalf("prewarm Members() error = %v", err)
	}
	assertPrewarmEquivalentJSON(t, "Members", exactMembers, prewarmMembers)
	if exactMembers.NextCursor == "" || exactMembers.NextCursor != prewarmMembers.NextCursor {
		t.Fatalf("Members cursors differ or are empty: exact=%q prewarm=%q", exactMembers.NextCursor, prewarmMembers.NextCursor)
	}
	assertPrewarmFreshness(t, "Members exact miss", exactMembers.SnapshotFreshness, "miss")
	assertPrewarmFreshness(t, "Members prewarm miss", prewarmMembers.SnapshotFreshness, "miss")

	organizationParams := OrganizationParams{
		OverviewParams: params, ParentDepartmentExternalID: "department-alpha", MemberLimit: 1,
	}
	exactOrganization, err := exact.Organization(context.Background(), 1, organizationParams)
	if err != nil {
		t.Fatalf("exact Organization() error = %v", err)
	}
	prewarmOrganization, err := prewarmed.Organization(context.Background(), 1, organizationParams)
	if err != nil {
		t.Fatalf("prewarm Organization() error = %v", err)
	}
	assertPrewarmEquivalentJSON(t, "Organization", exactOrganization, prewarmOrganization)
	if exactOrganization.NextMemberCursor == "" || exactOrganization.NextMemberCursor != prewarmOrganization.NextMemberCursor {
		t.Fatalf("Organization cursors differ or are empty: exact=%q prewarm=%q", exactOrganization.NextMemberCursor, prewarmOrganization.NextMemberCursor)
	}
	assertPrewarmFreshness(t, "Organization exact miss", exactOrganization.SnapshotFreshness, "miss")
	assertPrewarmFreshness(t, "Organization prewarm miss", prewarmOrganization.SnapshotFreshness, "miss")

	exactOverview, err := exact.Overview(context.Background(), 1, params)
	if err != nil {
		t.Fatalf("exact Overview() error = %v", err)
	}
	prewarmOverview, err := prewarmed.Overview(context.Background(), 1, params)
	if err != nil {
		t.Fatalf("prewarm Overview() error = %v", err)
	}
	assertPrewarmEquivalentJSON(t, "Overview", exactOverview, prewarmOverview)

	warmSummaryExact, err := exact.Summary(context.Background(), 1, params)
	if err != nil {
		t.Fatalf("exact warm Summary() error = %v", err)
	}
	warmSummaryPrewarm, err := prewarmed.Summary(context.Background(), 1, params)
	if err != nil {
		t.Fatalf("prewarm warm Summary() error = %v", err)
	}
	warmTrendExact, err := exact.Trend(context.Background(), 1, params)
	if err != nil {
		t.Fatalf("exact warm Trend() error = %v", err)
	}
	warmTrendPrewarm, err := prewarmed.Trend(context.Background(), 1, params)
	if err != nil {
		t.Fatalf("prewarm warm Trend() error = %v", err)
	}
	warmMembersExact, err := exact.Members(context.Background(), 1, membersParams)
	if err != nil {
		t.Fatalf("exact warm Members() error = %v", err)
	}
	warmMembersPrewarm, err := prewarmed.Members(context.Background(), 1, membersParams)
	if err != nil {
		t.Fatalf("prewarm warm Members() error = %v", err)
	}
	warmOrganizationExact, err := exact.Organization(context.Background(), 1, organizationParams)
	if err != nil {
		t.Fatalf("exact warm Organization() error = %v", err)
	}
	warmOrganizationPrewarm, err := prewarmed.Organization(context.Background(), 1, organizationParams)
	if err != nil {
		t.Fatalf("prewarm warm Organization() error = %v", err)
	}
	for _, check := range []struct {
		name             string
		exact, prewarmed SnapshotFreshness
	}{
		{name: "Summary", exact: warmSummaryExact.SnapshotFreshness, prewarmed: warmSummaryPrewarm.SnapshotFreshness},
		{name: "Trend", exact: warmTrendExact.SnapshotFreshness, prewarmed: warmTrendPrewarm.SnapshotFreshness},
		{name: "Members", exact: warmMembersExact.SnapshotFreshness, prewarmed: warmMembersPrewarm.SnapshotFreshness},
		{name: "Organization", exact: warmOrganizationExact.SnapshotFreshness, prewarmed: warmOrganizationPrewarm.SnapshotFreshness},
	} {
		assertPrewarmFreshness(t, check.name+" exact warm", check.exact, "fresh")
		assertPrewarmFreshness(t, check.name+" prewarm warm", check.prewarmed, "fresh")
		if !reflect.DeepEqual(check.exact, check.prewarmed) {
			t.Fatalf("%s warm freshness differs: exact=%#v prewarm=%#v", check.name, check.exact, check.prewarmed)
		}
	}

	if got := exactKeys(); !reflect.DeepEqual(got, wantResponseKeys) {
		t.Fatalf("exact response keys = %#v, want %#v", got, wantResponseKeys)
	}
	if got := prewarmKeys(); !reflect.DeepEqual(got, wantResponseKeys) {
		t.Fatalf("prewarm response keys = %#v, want %#v", got, wantResponseKeys)
	}
	if len(prewarmProvider.summaryRequestBatches) != 0 || prewarmProvider.trendCalls != 0 {
		t.Fatalf("full prewarm path used exact Relay source: stats=%d trend=%d", len(prewarmProvider.summaryRequestBatches), prewarmProvider.trendCalls)
	}
	if len(exactProvider.summaryRequestBatches) == 0 || exactProvider.trendCalls == 0 || exactScope.Version != prewarmScope.Version {
		t.Fatalf("exact fixture/source/version = stats %d trend %d versions %q/%q", len(exactProvider.summaryRequestBatches), exactProvider.trendCalls, exactScope.Version, prewarmScope.Version)
	}
}

func newPrewarmEquivalenceService(
	t *testing.T,
	now time.Time,
	withPrewarm bool,
) (*Service, *fakeRelayProvider, *representativescope.Scope, func() []string) {
	t.Helper()
	client := testdb.Open(t)
	providerRow := createPrimaryRelayProvider(t, client)
	scope, provider := prewarmEquivalenceData()
	snapshotCache, snapshotServer := testSnapshotCache(t, now, 0)
	originCache, _ := testOriginCache(t, func() time.Time { return now }, "equivalence-origin-token")
	service := newServiceWithSnapshotCacheForTest(
		client, fakeScopeResolver{scope: scope}, fakeProviderResolver{provider: provider}, nil,
		snapshotCache, testMemberCursorSecret,
	)
	service.originCache = originCache
	if withPrewarm {
		prewarmCache, _ := newRedisPrewarmCache(t, func() time.Time { return now })
		seedPrewarmEquivalenceManifest(t, prewarmCache, PrewarmCacheIdentity{
			ProviderID: providerRow.ID, ProviderVersion: providerRow.ConfigurationVersion,
			Timezone: "UTC", AnchorDate: "2026-07-21",
		}, now)
		reader := mustPrewarmReader(t, prewarmCache, PrewarmReaderOptions{Now: func() time.Time { return now }})

		installTestPrewarmReader(service, reader)
	}
	return service, provider, scope, func() []string {
		keys := append([]string(nil), snapshotServer.Keys()...)
		sort.Strings(keys)
		return keys
	}
}

func prewarmEquivalenceData() (*representativescope.Scope, *fakeRelayProvider) {
	scope, provider := membersTestData(2)
	scope.Version = "scope-equivalence-v1"
	tokens40, tokens20 := int64(40), int64(20)
	total300, total200 := int64(300), int64(200)
	cost4, cost2 := 4.0, 2.0
	provider.summaryStats = map[int64]relay.TeamUserUsageStats{
		10001: {UserID: 10001, TodayActualCost: 3, TotalActualCost: 30, TotalTokens: &total300, RangeActualCost: &cost4, RangeTotalTokens: &tokens40},
		10002: {UserID: 10002, TodayActualCost: 0, TotalActualCost: 20, TotalTokens: &total200, RangeActualCost: &cost2, RangeTotalTokens: &tokens20},
	}
	provider.trendPoints = map[int64][]relay.UsageTrendPoint{
		10001: {
			{Date: "2026-07-15", ActualCost: 1, TotalTokens: int64Ptr(10)},
			{Date: "2026-07-21", ActualCost: 3, TotalTokens: int64Ptr(30)},
		},
		10002: {{Date: "2026-07-16", ActualCost: 2, TotalTokens: int64Ptr(20)}},
	}
	return scope, provider
}

func seedPrewarmEquivalenceManifest(t *testing.T, cache *PrewarmCache, identity PrewarmCacheIdentity, now time.Time) {
	t.Helper()
	total300, total200 := int64(300), int64(200)
	current, err := cache.WriteCurrentStats(context.Background(), PrewarmCurrentStatsEnvelope{
		SchemaVersion: prewarmCacheSchemaVersion, ProviderID: identity.ProviderID, ProviderVersion: identity.ProviderVersion,
		GenerationID: strings.Repeat("1", 64), GeneratedAt: now, RosterCount: 2,
		RosterDigest: prewarmRosterDigest([]int64{10001, 10002}), ResponseBytes: 64,
		Stats: []PrewarmCurrentStat{
			{UserID: 10001, TodayActualCost: 3, TotalActualCost: 30, TotalTokens: &total300},
			{UserID: 10002, TodayActualCost: 0, TotalActualCost: 20, TotalTokens: &total200},
		},
	})
	if err != nil {
		t.Fatalf("WriteCurrentStats(equivalence) error = %v", err)
	}
	historyPoints := []relay.ProviderWideTrendPoint{
		{UserID: 10001, Date: "2026-07-15", ActualCost: 1, TotalTokens: int64Ptr(10)},
		{UserID: 10002, Date: "2026-07-16", ActualCost: 2, TotalTokens: int64Ptr(20)},
	}
	todayPoints := []relay.ProviderWideTrendPoint{
		{UserID: 10001, Date: "2026-07-21 00:00", ActualCost: 1, TotalTokens: int64Ptr(10)},
		{UserID: 10001, Date: "2026-07-21 01:00", ActualCost: 2, TotalTokens: int64Ptr(20)},
	}
	refs := make(map[PrewarmSegmentClass]PrewarmValueReference, 3)
	for index, item := range []struct {
		class  PrewarmSegmentClass
		points []relay.ProviderWideTrendPoint
	}{
		{class: SegmentHistory29d, points: historyPoints},
		{class: SegmentHistory6d, points: historyPoints},
		{class: SegmentTodayHour, points: todayPoints},
	} {
		coverage, coverageErr := prewarmSegmentCoverage(item.class, identity.AnchorDate, identity.Timezone)
		if coverageErr != nil {
			t.Fatalf("prewarmSegmentCoverage(%s) error = %v", item.class, coverageErr)
		}
		segment := PrewarmTrendSegment{
			SchemaVersion: prewarmCacheSchemaVersion, ProviderID: identity.ProviderID, ProviderVersion: identity.ProviderVersion,
			TimezoneDigest: prewarmTimezoneDigest(identity.Timezone), GenerationID: strings.Repeat(string(rune('2'+index)), 64),
			GeneratedAt: now, Timezone: identity.Timezone, AnchorDate: identity.AnchorDate, Class: item.class, Coverage: coverage,
			Points: item.points, ResponseBytes: 64, PointCount: len(item.points), UniqueUserCount: 2, Complete: true,
		}
		if item.class == SegmentTodayHour {
			segment.UniqueUserCount = 1
		}
		refs[item.class], err = cache.WriteSegment(context.Background(), segment)
		if err != nil {
			t.Fatalf("WriteSegment(%s equivalence) error = %v", item.class, err)
		}
	}
	publishSeedManifest(t, cache, PrewarmManifest{
		SchemaVersion: prewarmCacheSchemaVersion, ProviderID: identity.ProviderID, ProviderVersion: identity.ProviderVersion,
		Timezone: identity.Timezone, TimezoneDigest: prewarmTimezoneDigest(identity.Timezone), AnchorDate: identity.AnchorDate,
		CreatedAt: now, CurrentStats: current, History29d: refs[SegmentHistory29d], History6d: refs[SegmentHistory6d], TodayHour: refs[SegmentTodayHour],
	})
}

func prewarmEquivalenceResponseKeys(t *testing.T, key SnapshotCacheKey, parentID string) []string {
	t.Helper()
	keys := make([]string, 0, 4)
	for name, encode := range map[string]func(string, SnapshotCacheKey) (string, error){
		"summary": summaryCacheKey, "trend": trendCacheKey, "members": membersCacheKey,
	} {
		encoded, err := encode("test", key)
		if err != nil {
			t.Fatalf("%s cache key error = %v", name, err)
		}
		keys = append(keys, encoded)
	}
	organization, err := organizationCacheKey("test", OrganizationCacheKey{
		SnapshotCacheKey: key, ParentDepartmentExternalID: parentID,
	})
	if err != nil {
		t.Fatalf("organization cache key error = %v", err)
	}
	keys = append(keys, organization)
	sort.Strings(keys)
	return keys
}

func assertPrewarmEquivalentJSON(t *testing.T, name string, exact, prewarmed any) {
	t.Helper()
	exactJSON, err := json.Marshal(exact)
	if err != nil {
		t.Fatalf("marshal exact %s: %v", name, err)
	}
	prewarmJSON, err := json.Marshal(prewarmed)
	if err != nil {
		t.Fatalf("marshal prewarm %s: %v", name, err)
	}
	if !bytes.Equal(exactJSON, prewarmJSON) {
		t.Fatalf("%s JSON differs:\nexact: %s\nprewarm: %s", name, exactJSON, prewarmJSON)
	}
}

func assertPrewarmFreshness(t *testing.T, name string, freshness SnapshotFreshness, wantCacheStatus string) {
	t.Helper()
	if freshness.CacheStatus != wantCacheStatus || freshness.SourceStatus != "ok" {
		t.Fatalf("%s freshness = %#v, want cache=%q source=ok", name, freshness, wantCacheStatus)
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
