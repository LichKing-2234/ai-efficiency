package teamusage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/internal/readcache"
	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/ai-efficiency/backend/internal/representativescope"
	"github.com/ai-efficiency/backend/internal/testdb"
)

func TestOrganizationProjectsRootAndIndependentlyPagedBranchCollections(t *testing.T) {
	svc, provider, _ := newOrganizationTestService(t, 30, 120, nil, nil)
	params := testOrganizationParams("")

	root, err := svc.Organization(context.Background(), 1, params)
	if err != nil {
		t.Fatalf("Organization(root) error = %v", err)
	}
	if root.ParentDepartmentExternalID != nil || len(root.Departments) != 1 || root.Departments[0].DepartmentExternalID != "department-root" || len(root.Members) != 0 {
		t.Fatalf("root response = %+v", root)
	}
	if root.Departments[0].ChildCount != 30 || !root.Departments[0].HasChildren || root.Departments[0].DirectMemberCount != 120 || root.Departments[0].AggregateMemberCount != 120 {
		t.Fatalf("root department metadata = %+v", root.Departments[0])
	}

	branchParams := testOrganizationParams("department-root")
	first, err := svc.Organization(context.Background(), 1, branchParams)
	if err != nil {
		t.Fatalf("Organization(first branch page) error = %v", err)
	}
	if len(first.Departments) != 25 || len(first.Members) != 50 || first.NextDepartmentCursor == "" || first.NextMemberCursor == "" {
		t.Fatalf("first branch page = departments %d members %d cursors %q/%q", len(first.Departments), len(first.Members), first.NextDepartmentCursor, first.NextMemberCursor)
	}
	if first.Departments[0].Name != "Alpha 01" || first.Departments[24].Name != "Alpha 25" {
		t.Fatalf("department order = first %+v last %+v", first.Departments[0], first.Departments[24])
	}
	if first.Members[0].Rank != 1 || first.Members[49].Rank != 50 || first.Members[0].TotalTokens == nil || *first.Members[0].TotalTokens <= *first.Members[49].TotalTokens {
		t.Fatalf("member order/ranks = first %+v last %+v", first.Members[0], first.Members[49])
	}

	departmentPage := branchParams
	departmentPage.DepartmentCursor = first.NextDepartmentCursor
	departmentPage.DepartmentLimit = 100
	secondDepartments, err := svc.Organization(context.Background(), 1, departmentPage)
	if err != nil {
		t.Fatalf("Organization(second department page) error = %v", err)
	}
	if len(secondDepartments.Departments) != 5 || secondDepartments.Departments[0].Name != "Alpha 26" || secondDepartments.NextDepartmentCursor != "" {
		t.Fatalf("second department page = %+v", secondDepartments.Departments)
	}

	memberPage := branchParams
	memberPage.MemberCursor = first.NextMemberCursor
	memberPage.MemberLimit = 100
	secondMembers, err := svc.Organization(context.Background(), 1, memberPage)
	if err != nil {
		t.Fatalf("Organization(second member page) error = %v", err)
	}
	if len(secondMembers.Members) != 70 || secondMembers.Members[0].Rank != 51 || secondMembers.Members[69].Rank != 120 || secondMembers.NextMemberCursor != "" {
		t.Fatalf("second member page = len %d first %+v last %+v", len(secondMembers.Members), secondMembers.Members[0], secondMembers.Members[len(secondMembers.Members)-1])
	}
	encoded, err := json.Marshal(first)
	if err != nil {
		t.Fatalf("json.Marshal(first) error = %v", err)
	}
	if len(encoded) >= 64*1024 || strings.Contains(string(encoded), `"children"`) || strings.Contains(string(encoded), `"member_tree"`) {
		t.Fatalf("organization response bytes/shape = %d %s", len(encoded), encoded)
	}
	if provider.trendCalls != 1 {
		t.Fatalf("shared origin trend calls = %d, want 1", provider.trendCalls)
	}
}

func TestOrganizationRejectsLimitsAndUnauthorizedParent(t *testing.T) {
	svc, _, _ := newOrganizationTestService(t, 2, 3, nil, nil)
	tests := []OrganizationParams{
		{OverviewParams: testMembersParams().OverviewParams, DepartmentLimit: 101},
		{OverviewParams: testMembersParams().OverviewParams, MemberLimit: 101},
		{OverviewParams: testMembersParams().OverviewParams, DepartmentLimit: -1},
		{OverviewParams: testMembersParams().OverviewParams, MemberLimit: -1},
	}
	for _, params := range tests {
		if _, err := svc.Organization(context.Background(), 1, params); !errors.Is(err, ErrInvalidOverviewParams) {
			t.Fatalf("Organization(%+v) error = %v, want ErrInvalidOverviewParams", params, err)
		}
	}
	params := testOrganizationParams("department-outside-scope")
	if _, err := svc.Organization(context.Background(), 1, params); !errors.Is(err, ErrOutOfScope) {
		t.Fatalf("Organization(outside parent) error = %v, want ErrOutOfScope", err)
	}
}

func TestOrganizationCursorsRejectWrongRequestAndExpireOnScopeOrContentChange(t *testing.T) {
	now := time.Date(2026, 7, 16, 8, 0, 0, 0, time.UTC)
	svc, provider, scope := newOrganizationTestService(t, 3, 3, func() time.Time { return now }, nil)
	params := testOrganizationParams("department-root")
	params.DepartmentLimit = 1
	params.MemberLimit = 1
	first, err := svc.Organization(context.Background(), 1, params)
	if err != nil {
		t.Fatalf("Organization(first) error = %v", err)
	}

	tests := []struct {
		name    string
		actorID int
		mutate  func(*OrganizationParams)
	}{
		{name: "cross actor", actorID: 2},
		{name: "cross range", actorID: 1, mutate: func(p *OrganizationParams) { p.StartDate = "2026-07-02" }},
		{name: "cross parent", actorID: 1, mutate: func(p *OrganizationParams) { p.ParentDepartmentExternalID = "department-alpha-01" }},
		{name: "cross collection", actorID: 1, mutate: func(p *OrganizationParams) { p.MemberCursor, p.DepartmentCursor = p.DepartmentCursor, "" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			next := params
			next.DepartmentCursor = first.NextDepartmentCursor
			if tt.mutate != nil {
				tt.mutate(&next)
			}
			if _, err := svc.Organization(context.Background(), tt.actorID, next); !errors.Is(err, ErrInvalidOrganizationCursor) {
				t.Fatalf("Organization(%s) error = %v, want ErrInvalidOrganizationCursor", tt.name, err)
			}
		})
	}

	scope.Version = "scope-version-2"
	expired := params
	expired.DepartmentCursor = first.NextDepartmentCursor
	if _, err := svc.Organization(context.Background(), 1, expired); !errors.Is(err, ErrOrganizationSnapshotExpired) {
		t.Fatalf("Organization(changed scope) error = %v, want ErrOrganizationSnapshotExpired", err)
	}
	scope.Version = "scope-version-1"
	now = now.Add(55 * time.Second)
	provider.trendPoints[10001][0].TotalTokens = int64Ptr(999999)
	if _, err := svc.Organization(context.Background(), 1, expired); !errors.Is(err, ErrOrganizationSnapshotExpired) {
		t.Fatalf("Organization(changed content) error = %v, want ErrOrganizationSnapshotExpired", err)
	}
}

func TestOrganizationCursorContinuesAcrossRedisOutageWhenContentMatches(t *testing.T) {
	store := failingSnapshotStore{err: errors.New("Redis unavailable")}
	svc, provider, _ := newOrganizationTestService(t, 2, 3, time.Now, store)
	params := testOrganizationParams("department-root")
	params.MemberLimit = 1
	first, err := svc.Organization(context.Background(), 1, params)
	if err != nil {
		t.Fatalf("Organization(first) error = %v", err)
	}
	params.MemberCursor = first.NextMemberCursor
	second, err := svc.Organization(context.Background(), 1, params)
	if err != nil {
		t.Fatalf("Organization(second) error = %v", err)
	}
	if len(second.Members) != 1 || second.Members[0].Rank != 2 || provider.trendCalls != 2 {
		t.Fatalf("second page = members %+v origin calls %d", second.Members, provider.trendCalls)
	}
}

func newOrganizationTestService(t *testing.T, childCount, directMemberCount int, now func() time.Time, store readcache.Store) (*Service, *fakeRelayProvider, *representativescope.Scope) {
	t.Helper()
	client := testdb.Open(t)
	createPrimaryRelayProvider(t, client)
	rootID := "department-root"
	departments := []representativescope.DepartmentScope{{ExternalID: rootID, Name: "Root", DisplayPath: "Root", Depth: 0, ChildCount: childCount}}
	for index := childCount; index >= 1; index-- {
		externalID := fmt.Sprintf("department-alpha-%02d", index)
		departments = append(departments, representativescope.DepartmentScope{
			ExternalID: externalID, ParentExternalID: &rootID, Name: fmt.Sprintf("Alpha %02d", index), DisplayPath: fmt.Sprintf("Root / Alpha %02d", index), Depth: 1,
		})
	}
	subjects := make([]representativescope.Subject, 0, directMemberCount)
	stats := make(map[int64]relay.TeamUserUsageStats, directMemberCount)
	trends := make(map[int64][]relay.UsageTrendPoint, directMemberCount)
	for index := 1; index <= directMemberCount; index++ {
		relayUserID := 10000 + index
		tokens := int64(directMemberCount - index + 1)
		subjects = append(subjects, representativescope.Subject{
			SubjectType: "member", UserID: index, DirectoryMemberExternalID: fmt.Sprintf("member-%03d", index),
			DisplayName: fmt.Sprintf("Member %03d", index), Email: fmt.Sprintf("member-%03d@example.com", index),
			DepartmentExternalID: rootID, DepartmentExternalIDs: []string{rootID}, DepartmentDisplayPath: "Root", RelayUserID: &relayUserID, Selectable: true,
		})
		stats[int64(relayUserID)] = relay.TeamUserUsageStats{UserID: int64(relayUserID), TodayActualCost: 1, TotalActualCost: 2}
		trends[int64(relayUserID)] = []relay.UsageTrendPoint{{Date: "2026-07-07", ActualCost: 1, TotalTokens: &tokens}}
	}
	scope := &representativescope.Scope{
		Version: "scope-version-1", ActorUserID: 1, IsRepresentative: true, OverviewSubjects: subjects,
		MemberTreeRootIDs: []string{rootID}, MemberTreeDepartments: departments,
	}
	provider := &fakeRelayProvider{summaryStats: stats, trendPoints: trends}
	if now == nil {
		fixed := time.Date(2026, 7, 16, 8, 0, 0, 0, time.UTC)
		now = func() time.Time { return fixed }
	}
	if store == nil {
		cache, _ := testSnapshotCacheWithClock(t, now, 0)
		return newServiceWithSnapshotCacheForTest(client, fakeScopeResolver{scope: scope}, fakeProviderResolver{provider: provider}, nil, cache, testMemberCursorSecret), provider, scope
	}
	cache := newTestSnapshotCache(t, store, now, 0)
	return newServiceWithSnapshotCacheForTest(client, fakeScopeResolver{scope: scope}, fakeProviderResolver{provider: provider}, nil, cache, testMemberCursorSecret), provider, scope
}

func testOrganizationParams(parent string) OrganizationParams {
	return OrganizationParams{OverviewParams: testMembersParams().OverviewParams, ParentDepartmentExternalID: parent}
}
