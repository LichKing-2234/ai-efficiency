package teamusage

import (
	"context"
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
