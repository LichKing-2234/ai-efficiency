package teamusage

import (
	"context"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/ai-efficiency/backend/internal/representativescope"
	"github.com/ai-efficiency/backend/internal/testdb"
)

func TestSharedOriginFourConcurrentLanesLoadOneScopeOrigin(t *testing.T) {
	client := testdb.Open(t)
	createPrimaryRelayProvider(t, client)
	scope, base := membersTestData(205)
	release := make(chan struct{})
	provider := &sharedOriginProbeProvider{
		fakeRelayProvider: base,
		statsStarted:      make(chan struct{}),
		release:           release,
	}
	cache, _ := testSnapshotCache(t, time.Date(2026, 7, 20, 8, 0, 0, 0, time.UTC), 0)
	originCache, _ := testOriginCache(t, time.Now, "shared-lanes-token")
	service := newServiceWithSnapshotCacheForTest(
		client,
		fakeScopeResolver{scope: scope},
		fakeProviderResolver{provider: provider},
		nil,
		cache,
		testMemberCursorSecret,
	)
	service.originCache = originCache

	start := make(chan struct{})
	errors := make(chan error, 4)
	var wait sync.WaitGroup
	wait.Add(4)
	go func() {
		defer wait.Done()
		<-start
		_, err := service.Summary(context.Background(), 1, testMembersParams().OverviewParams)
		errors <- err
	}()
	go func() {
		defer wait.Done()
		<-start
		_, err := service.Trend(context.Background(), 1, testMembersParams().OverviewParams)
		errors <- err
	}()
	go func() {
		defer wait.Done()
		<-start
		_, err := service.Members(context.Background(), 1, testMembersParams())
		errors <- err
	}()
	go func() {
		defer wait.Done()
		<-start
		_, err := service.Organization(context.Background(), 1, testOrganizationParams(""))
		errors <- err
	}()

	close(start)
	select {
	case <-provider.statsStarted:
	case <-time.After(time.Second):
		t.Fatal("four-lane origin did not start a stats request")
	}
	close(release)
	wait.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatalf("typed lane error = %v", err)
		}
	}

	if got := provider.statsCalls.Load(); got != 3 {
		t.Fatalf("stats chunk calls = %d, want 3 from one 205-user scope origin", got)
	}
	if got := provider.statsUsers.Load(); got != 205 {
		t.Fatalf("stats users = %d, want all 205 authorized Relay users", got)
	}
	if got := provider.trendCalls.Load(); got != 1 {
		t.Fatalf("users-trend calls = %d, want one from the shared scope origin", got)
	}
	if got := provider.trendUsers.Load(); got != 205 {
		t.Fatalf("users-trend users = %d, want all 205 authorized Relay users", got)
	}
}

func TestSharedOriginOrganizationProjectsOnlyAuthorizedBranch(t *testing.T) {
	client := testdb.Open(t)
	createPrimaryRelayProvider(t, client)
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
	provider := &fakeRelayProvider{
		summaryStats: map[int64]relay.TeamUserUsageStats{
			101: {UserID: 101}, 102: {UserID: 102}, 999: {UserID: 999},
		},
		trendPoints: map[int64][]relay.UsageTrendPoint{
			101: {{Date: "2026-07-07", ActualCost: 1, TotalTokens: int64Ptr(10)}},
			102: {{Date: "2026-07-07", ActualCost: 2, TotalTokens: int64Ptr(20)}},
			999: {{Date: "2026-07-07", ActualCost: 9, TotalTokens: int64Ptr(90)}},
		},
	}
	responseCache, _ := testSnapshotCache(t, time.Date(2026, 7, 20, 8, 0, 0, 0, time.UTC), 0)
	originCache, _ := testOriginCache(t, time.Now, "branch-token")
	service := newServiceWithSnapshotCacheForTest(
		client, fakeScopeResolver{scope: scope}, fakeProviderResolver{provider: provider}, nil,
		responseCache, testMemberCursorSecret,
	)
	service.originCache = originCache

	response, err := service.Organization(context.Background(), 1, OrganizationParams{
		OverviewParams: testMembersParams().OverviewParams, ParentDepartmentExternalID: rootID,
	})
	if err != nil {
		t.Fatalf("Organization() error = %v", err)
	}
	if len(response.Departments) != 1 || response.Departments[0].DepartmentExternalID != childID ||
		response.Departments[0].RangeTotalTokens == nil || *response.Departments[0].RangeTotalTokens != 10 {
		t.Fatalf("authorized child projection = %+v, want only child total 10", response.Departments)
	}
	if len(response.Members) != 1 || response.Members[0].UserID != 2 {
		t.Fatalf("authorized direct members = %+v, want only root member user 2", response.Members)
	}
	if len(provider.summaryRequestBatches) != 1 || len(provider.summaryRequestBatches[0]) != 3 ||
		provider.trendCalls != 1 || len(provider.trendRequestUserIDs) != 3 {
		t.Fatalf("full-scope origin calls = stats %#v trend %d/%#v", provider.summaryRequestBatches, provider.trendCalls, provider.trendRequestUserIDs)
	}
}

func TestSharedOriginOrganizationKeepsLargeScopeBranchBounded(t *testing.T) {
	client := testdb.Open(t)
	createPrimaryRelayProvider(t, client)
	scope, provider := membersTestData(501)
	branchID, siblingID := "department-branch", "department-sibling"
	scope.MemberTreeRootIDs = []string{branchID, siblingID}
	scope.MemberTreeDepartments = []representativescope.DepartmentScope{
		{ExternalID: branchID, Name: "Branch"},
		{ExternalID: siblingID, Name: "Sibling"},
	}
	for index := range scope.OverviewSubjects {
		departmentID := siblingID
		if index == 0 {
			departmentID = branchID
		}
		scope.OverviewSubjects[index].DepartmentExternalID = departmentID
		scope.OverviewSubjects[index].DepartmentExternalIDs = []string{departmentID}
		scope.OverviewSubjects[index].DepartmentDisplayPath = departmentID
	}
	branchRelayUserID := int64(*scope.OverviewSubjects[0].RelayUserID)
	branchStats := provider.summaryStats[branchRelayUserID]
	branchStats.RangeActualCost = nil
	branchStats.RangeTotalTokens = nil
	provider.summaryStats[branchRelayUserID] = branchStats

	responseCache, _ := testSnapshotCache(t, time.Date(2026, 7, 20, 8, 0, 0, 0, time.UTC), 0)
	originCache, originServer := testOriginCache(t, time.Now, "large-branch-token")
	service := newServiceWithSnapshotCacheForTest(
		client, fakeScopeResolver{scope: scope}, fakeProviderResolver{provider: provider}, nil,
		responseCache, testMemberCursorSecret,
	)
	service.originCache = originCache

	response, err := service.Organization(context.Background(), 1, OrganizationParams{
		OverviewParams: testMembersParams().OverviewParams, ParentDepartmentExternalID: branchID,
	})
	if err != nil {
		t.Fatalf("Organization() error = %v", err)
	}
	if len(response.Members) != 1 {
		t.Fatalf("branch members = %d, want 1", len(response.Members))
	}
	if len(provider.summaryRequestBatches) != 1 || len(provider.summaryRequestBatches[0]) != 1 {
		t.Fatalf("branch stats requests = %#v, want one single-user batch", provider.summaryRequestBatches)
	}
	if provider.trendCalls != 1 || !reflect.DeepEqual(provider.trendRequestUserIDs, []int64{branchRelayUserID}) {
		t.Fatalf("branch trend requests = %d/%#v, want one branch-local request", provider.trendCalls, provider.trendRequestUserIDs)
	}
	for _, key := range originServer.Keys() {
		if strings.Contains(key, "team-usage-origin") {
			t.Fatalf("large-scope Organization created full origin key %q", key)
		}
	}
}

func TestSharedOriginSummaryCountsConnectedSubjectsBeforeRelayIDDeduplication(t *testing.T) {
	rangeCost := 15.0
	rangeTokens := int64(1500)
	origin := &teamUsageScopeOrigin{
		subjects: []representativescope.Subject{
			{SubjectType: "member", UserID: 2, RelayUserID: intPtr(1002)},
			{SubjectType: "member", UserID: 3, RelayUserID: intPtr(1002)},
		},
		RelayUserIDs: []int64{1002},
		StatsByRelayUserID: map[int64]relay.TeamUserUsageStats{
			1002: {UserID: 1002, RangeActualCost: &rangeCost, RangeTotalTokens: &rangeTokens},
		},
	}
	scope := &representativescope.Scope{OverviewSubjects: append([]representativescope.Subject(nil), origin.subjects...)}

	snapshot := buildSummarySnapshotFromScopeOrigin(scope, OverviewParams{}, origin)

	if snapshot.Summary.MemberCount != 2 || snapshot.Summary.RelayMemberCount != 2 {
		t.Fatalf("summary counts = %d/%d, want canonical/connected 2/2", snapshot.Summary.MemberCount, snapshot.Summary.RelayMemberCount)
	}
	if snapshot.Summary.RangeActualCost == nil || *snapshot.Summary.RangeActualCost != 15 ||
		snapshot.Summary.RangeTotalTokens == nil || *snapshot.Summary.RangeTotalTokens != 1500 {
		t.Fatalf("summary range totals = %#v/%#v, want one deduplicated Relay total", snapshot.Summary.RangeActualCost, snapshot.Summary.RangeTotalTokens)
	}
}

func TestSharedOriginReloadsWhenRelayMappingsChangeDuringRedisTTL(t *testing.T) {
	cache, _ := testOriginCache(t, time.Now, "mapping-change-token")
	key := testOriginCacheKey()
	if _, err := cache.GetOrLoad(context.Background(), key, func(context.Context) (*teamUsageScopeOrigin, error) {
		return testScopeOrigin(), nil
	}); err != nil {
		t.Fatalf("prewarm origin cache: %v", err)
	}

	newRelayUserID := int64(202)
	rangeCost := 2.0
	rangeTokens := int64(20)
	provider := &fakeRelayProvider{
		summaryStats: map[int64]relay.TeamUserUsageStats{
			newRelayUserID: {UserID: newRelayUserID, RangeActualCost: &rangeCost, RangeTotalTokens: &rangeTokens},
		},
		trendPoints: map[int64][]relay.UsageTrendPoint{
			newRelayUserID: {{Date: "2026-07-01", ActualCost: rangeCost, TotalTokens: &rangeTokens}},
		},
	}
	scope := &representativescope.Scope{
		Version: key.ScopeVersion,
		OverviewSubjects: []representativescope.Subject{
			{SubjectType: "member", UserID: 2, RelayUserID: intPtr(int(newRelayUserID)), Selectable: true},
		},
	}
	service := &Service{
		originCache: cache, providerResolver: fakeProviderResolver{provider: provider},
		teamOverviewTrendTimeout: time.Second,
	}

	origin, err := service.loadSharedScopeOrigin(context.Background(), &splitReadRequest{
		params: key.Params, scope: scope, scopeHash: key.ScopeHash,
		providerConfig: primaryProviderConfig{ID: key.ProviderID, ConfigurationVersion: key.ProviderVersion},
	})
	if err != nil {
		t.Fatalf("loadSharedScopeOrigin() error = %v", err)
	}
	if !reflect.DeepEqual(origin.RelayUserIDs, []int64{newRelayUserID}) || origin.StatsByRelayUserID[newRelayUserID].UserID != newRelayUserID ||
		len(origin.PointsByUser[newRelayUserID]) != 1 {
		t.Fatalf("reloaded origin = %#v/%#v/%#v, want authoritative data for Relay user %d", origin.RelayUserIDs, origin.StatsByRelayUserID, origin.PointsByUser, newRelayUserID)
	}
	if !reflect.DeepEqual(provider.summaryRequestUserIDs, []int64{newRelayUserID}) || provider.trendCalls != 1 {
		t.Fatalf("authoritative reload calls = stats %#v trend %d, want one reload", provider.summaryRequestUserIDs, provider.trendCalls)
	}
}

func TestSharedOriginNormalizesNilTrendPoints(t *testing.T) {
	client := testdb.Open(t)
	scope, provider := membersTestData(1)
	provider.trendPoints[10001] = nil
	service := newUncachedServiceForTest(
		client, fakeScopeResolver{scope: scope}, fakeProviderResolver{provider: provider}, nil,
	)

	origin, err := service.loadTeamUsageScopeOrigin(context.Background(), scope, provider, testMembersParams().OverviewParams)
	if err != nil {
		t.Fatalf("loadTeamUsageScopeOrigin() error = %v", err)
	}
	if origin.PointsByUser[10001] == nil || !validTeamUsageScopeOrigin(origin) {
		t.Fatalf("nil provider points produced invalid origin: %#v", origin.PointsByUser)
	}
}

func TestSharedOriginPrewarmResolvesMappingsBeforeSegmentedRedis(t *testing.T) {
	now := time.Date(2026, 7, 21, 8, 0, 0, 0, time.UTC)
	client := testdb.Open(t)
	providerRow := createPrimaryRelayProvider(t, client)
	scope, provider := membersTestData(1)
	store := newRecordingPrewarmStore()
	cache := mustNewPrewarmCache(t, store, func() time.Time { return now })
	seedAuthorizedPrewarmManifest(t, cache, PrewarmCacheIdentity{
		ProviderID: providerRow.ID, ProviderVersion: providerRow.ConfigurationVersion, Timezone: "UTC", AnchorDate: "2026-07-21",
	}, now, []int64{10001})
	store.getAfter = func(string) {
		if provider.listUsersCalls == 0 {
			t.Error("segmented Redis read happened before current Relay mappings were resolved")
		}
	}
	reader := mustPrewarmReader(t, cache, &readerSourceLimiter{}, PrewarmReaderOptions{
		Timezones: []string{"UTC"}, Now: func() time.Time { return now },
	})
	service := newUncachedServiceForTest(client, fakeScopeResolver{scope: scope}, fakeProviderResolver{provider: provider}, nil)
	service.prewarmReader = reader

	if _, err := service.Summary(context.Background(), 1, prewarmReader7dParams()); err != nil {
		t.Fatalf("Summary() error = %v", err)
	}
}

func TestSharedOriginPrewarmServesAuthorizedScopeAboveLegacyCap(t *testing.T) {
	now := time.Date(2026, 7, 21, 8, 0, 0, 0, time.UTC)
	client := testdb.Open(t)
	providerRow := createPrimaryRelayProvider(t, client)
	scope, provider := membersTestData(501)
	roster := make([]int64, len(scope.OverviewSubjects))
	for index, subject := range scope.OverviewSubjects {
		roster[index] = int64(*subject.RelayUserID)
	}
	cache, _ := newRedisPrewarmCache(t, func() time.Time { return now })
	seedAuthorizedPrewarmManifest(t, cache, PrewarmCacheIdentity{
		ProviderID: providerRow.ID, ProviderVersion: providerRow.ConfigurationVersion, Timezone: "UTC", AnchorDate: "2026-07-21",
	}, now, roster)
	reader := mustPrewarmReader(t, cache, &readerSourceLimiter{}, PrewarmReaderOptions{
		Timezones: []string{"UTC"}, Now: func() time.Time { return now },
	})
	service := newUncachedServiceForTest(client, fakeScopeResolver{scope: scope}, fakeProviderResolver{provider: provider}, nil)
	service.prewarmReader = reader

	response, err := service.Summary(context.Background(), 1, prewarmReader7dParams())
	if err != nil {
		t.Fatalf("Summary() error = %v", err)
	}
	if response.Summary.Unavailable || response.Summary.MemberCount != 501 {
		t.Fatalf("large prewarmed summary = %+v, want available authorized 501-member scope", response.Summary)
	}
	if len(provider.summaryRequestBatches) != 0 || provider.trendCalls != 0 {
		t.Fatalf("large prewarmed summary used exact Relay source: stats=%d trend=%d", len(provider.summaryRequestBatches), provider.trendCalls)
	}
}

func TestPrewarmFallbackScopeVersionRaceDiscardsProjectionAndUsesExactOrigin(t *testing.T) {
	now := time.Date(2026, 7, 21, 8, 0, 0, 0, time.UTC)
	client := testdb.Open(t)
	providerRow := createPrimaryRelayProvider(t, client)
	scopeV1, provider := membersTestData(1)
	scopeV1.Version = "scope-v1"
	scopeV2 := *scopeV1
	scopeV2.Version = "scope-v2"
	resolver := &sequenceScopeResolver{scopes: []*representativescope.Scope{scopeV1, &scopeV2, &scopeV2}}
	rangeCost, rangeTokens := 9.0, int64(90)
	provider.summaryStats[10001] = relay.TeamUserUsageStats{
		UserID: 10001, TodayActualCost: 3, TotalActualCost: 30, RangeActualCost: &rangeCost, RangeTotalTokens: &rangeTokens,
	}
	provider.trendPoints[10001] = []relay.UsageTrendPoint{{Date: "2026-07-15", ActualCost: 9, TotalTokens: &rangeTokens}}
	cache, _ := newRedisPrewarmCache(t, func() time.Time { return now })
	seedAuthorizedPrewarmManifest(t, cache, PrewarmCacheIdentity{
		ProviderID: providerRow.ID, ProviderVersion: providerRow.ConfigurationVersion, Timezone: "UTC", AnchorDate: "2026-07-21",
	}, now, []int64{10001})
	reader := mustPrewarmReader(t, cache, &readerSourceLimiter{}, PrewarmReaderOptions{
		Timezones: []string{"UTC"}, Now: func() time.Time { return now },
	})
	service := newUncachedServiceForTest(client, resolver, fakeProviderResolver{provider: provider}, nil)
	service.prewarmReader = reader
	service.originCache, _ = testOriginCache(t, time.Now, "scope-race-exact-token")

	response, err := service.Summary(context.Background(), 1, prewarmReader7dParams())
	if err != nil {
		t.Fatalf("Summary() error = %v", err)
	}
	if response.ScopeVersion != "scope-v2" || response.Summary.RangeActualCost == nil || *response.Summary.RangeActualCost != 9 {
		t.Fatalf("scope-race summary = version %q range %#v, want v2 exact range 9", response.ScopeVersion, response.Summary.RangeActualCost)
	}
	if len(provider.summaryRequestBatches) == 0 || provider.trendCalls == 0 {
		t.Fatalf("scope-race fallback source calls = stats %d trend %d, want complete exact origin", len(provider.summaryRequestBatches), provider.trendCalls)
	}
}

type sharedOriginProbeProvider struct {
	*fakeRelayProvider
	statsCalls   atomic.Int32
	statsUsers   atomic.Int32
	trendCalls   atomic.Int32
	trendUsers   atomic.Int32
	startedOnce  sync.Once
	statsStarted chan struct{}
	release      <-chan struct{}
}

func (p *sharedOriginProbeProvider) ListUsers(context.Context) ([]relay.User, error) {
	return nil, nil
}

func (p *sharedOriginProbeProvider) GetBatchUserUsageStats(ctx context.Context, relayUserIDs []int64, _ relay.TeamUsageSummaryParams) (map[int64]relay.TeamUserUsageStats, error) {
	p.statsCalls.Add(1)
	p.statsUsers.Add(int32(len(relayUserIDs)))
	p.startedOnce.Do(func() { close(p.statsStarted) })
	select {
	case <-p.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	stats := make(map[int64]relay.TeamUserUsageStats, len(relayUserIDs))
	for _, relayUserID := range relayUserIDs {
		if value, ok := p.summaryStats[relayUserID]; ok {
			stats[relayUserID] = value
		}
	}
	return stats, nil
}

func (p *sharedOriginProbeProvider) GetUsageTrendForUsers(_ context.Context, relayUserIDs []int64, _ relay.TeamMemberTrendParams) (map[int64][]relay.UsageTrendPoint, error) {
	p.trendCalls.Add(1)
	p.trendUsers.Add(int32(len(relayUserIDs)))
	points := make(map[int64][]relay.UsageTrendPoint, len(relayUserIDs))
	for _, relayUserID := range relayUserIDs {
		points[relayUserID] = append([]relay.UsageTrendPoint(nil), p.trendPoints[relayUserID]...)
	}
	return points, nil
}
