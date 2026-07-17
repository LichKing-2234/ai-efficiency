package teamusage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/internal/readcache"
	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/ai-efficiency/backend/internal/representativescope"
	"github.com/ai-efficiency/backend/internal/testdb"
)

const testMemberCursorSecret = "test-member-cursor-secret"

func TestMembersBoundsStableOrderAndResponseSize(t *testing.T) {
	svc, provider, _ := newMembersTestService(t, 500, nil)
	params := testMembersParams()

	first, err := svc.Members(context.Background(), 1, params)
	if err != nil {
		t.Fatalf("Members(first) error = %v", err)
	}
	if len(first.Items) != 50 || first.TotalCount != 500 || first.NextCursor == "" {
		t.Fatalf("first page = items %d total %d next %q", len(first.Items), first.TotalCount, first.NextCursor)
	}
	if first.Items[0].DirectoryMemberExternalID != "member-001" || first.Items[0].Rank != 1 || first.Items[49].DirectoryMemberExternalID != "member-050" || first.Items[49].Rank != 50 {
		t.Fatalf("first page bounds = first %+v last %+v", first.Items[0], first.Items[49])
	}
	encoded, err := json.Marshal(first)
	if err != nil {
		t.Fatalf("json.Marshal(first) error = %v", err)
	}
	if len(encoded) >= 64*1024 {
		t.Fatalf("first page JSON bytes = %d, want < 65536", len(encoded))
	}
	if string(encoded) == "" || containsJSONField(encoded, "member_tree") || containsJSONField(encoded, "members") || containsJSONField(encoded, "top_members") {
		t.Fatalf("members response contains compatibility collections: %s", encoded)
	}

	secondParams := params
	secondParams.Limit = 100
	secondParams.Cursor = first.NextCursor
	second, err := svc.Members(context.Background(), 1, secondParams)
	if err != nil {
		t.Fatalf("Members(second) error = %v", err)
	}
	if len(second.Items) != 100 || second.Items[0].DirectoryMemberExternalID != "member-051" || second.Items[0].Rank != 51 || second.Items[99].DirectoryMemberExternalID != "member-150" || second.Items[99].Rank != 150 {
		t.Fatalf("second page bounds = len %d first %+v last %+v", len(second.Items), second.Items[0], second.Items[len(second.Items)-1])
	}

	thirdParams := params
	thirdParams.Limit = 100
	thirdParams.Cursor = second.NextCursor
	third, err := svc.Members(context.Background(), 1, thirdParams)
	if err != nil {
		t.Fatalf("Members(third) error = %v", err)
	}
	if third.Items[99].DirectoryMemberExternalID != "member-250" || third.Items[99].Rank != 250 {
		t.Fatalf("third page last = %+v, want directory member rank 250", third.Items[99])
	}

	fourthParams := params
	fourthParams.Limit = 100
	fourthParams.Cursor = third.NextCursor
	fourth, err := svc.Members(context.Background(), 1, fourthParams)
	if err != nil {
		t.Fatalf("Members(fourth) error = %v", err)
	}
	if fourth.Items[0].UserID != 1 || fourth.Items[0].Rank != 251 || fourth.Items[99].UserID != 100 || fourth.Items[99].Rank != 350 {
		t.Fatalf("numeric user tie order = first %+v last %+v", fourth.Items[0], fourth.Items[99])
	}
	if provider.trendCalls != 1 || len(provider.summaryRequestUserIDs) != 500 {
		t.Fatalf("shared origin calls = trend %d stats %d, want 1/500", provider.trendCalls, len(provider.summaryRequestUserIDs))
	}

	tooLarge := params
	tooLarge.Limit = 101
	if _, err := svc.Members(context.Background(), 1, tooLarge); !errors.Is(err, ErrInvalidOverviewParams) {
		t.Fatalf("Members(limit=101) error = %v, want ErrInvalidOverviewParams", err)
	}
}

func TestMembersCursorRejectsCrossActorAndRangeBeforeOriginRead(t *testing.T) {
	svc, provider, _ := newMembersTestService(t, 3, nil)
	params := testMembersParams()
	params.Limit = 1
	first, err := svc.Members(context.Background(), 1, params)
	if err != nil {
		t.Fatalf("Members(first) error = %v", err)
	}

	next := params
	next.Cursor = first.NextCursor
	if _, err := svc.Members(context.Background(), 2, next); !errors.Is(err, ErrInvalidMemberCursor) {
		t.Fatalf("Members(cross actor) error = %v, want ErrInvalidMemberCursor", err)
	}
	next.StartDate = "2026-07-02"
	if _, err := svc.Members(context.Background(), 1, next); !errors.Is(err, ErrInvalidMemberCursor) {
		t.Fatalf("Members(cross range) error = %v, want ErrInvalidMemberCursor", err)
	}
	if provider.trendCalls != 1 {
		t.Fatalf("origin trend calls = %d, want invalid cursors rejected before a second read", provider.trendCalls)
	}
}

func TestMembersCursorExpiresWhenScopeOrSnapshotChanges(t *testing.T) {
	t.Run("scope version", func(t *testing.T) {
		svc, _, scope := newMembersTestService(t, 3, nil)
		params := testMembersParams()
		params.Limit = 1
		first, err := svc.Members(context.Background(), 1, params)
		if err != nil {
			t.Fatalf("Members(first) error = %v", err)
		}
		scope.Version = "scope-version-2"
		params.Cursor = first.NextCursor
		if _, err := svc.Members(context.Background(), 1, params); !errors.Is(err, ErrMemberSnapshotExpired) {
			t.Fatalf("Members(changed scope) error = %v, want ErrMemberSnapshotExpired", err)
		}
	})

	t.Run("member content", func(t *testing.T) {
		now := time.Date(2026, 7, 16, 8, 0, 0, 0, time.UTC)
		svc, provider, _ := newMembersTestService(t, 3, func() time.Time { return now })
		params := testMembersParams()
		params.Limit = 1
		first, err := svc.Members(context.Background(), 1, params)
		if err != nil {
			t.Fatalf("Members(first) error = %v", err)
		}
		now = now.Add(55 * time.Second)
		provider.trendPoints[10001][0].TotalTokens = int64Ptr(999999)
		params.Cursor = first.NextCursor
		if _, err := svc.Members(context.Background(), 1, params); !errors.Is(err, ErrMemberSnapshotExpired) {
			t.Fatalf("Members(changed content) error = %v, want ErrMemberSnapshotExpired", err)
		}
	})
}

func TestMembersCursorContinuesAcrossRedisOutageWhenAuthoritativeContentMatches(t *testing.T) {
	client := testdb.Open(t)
	createPrimaryRelayProvider(t, client)
	scope, provider := membersTestData(3)
	cache := newTestSnapshotCache(t, failingSnapshotStore{err: errors.New("Redis unavailable")}, time.Now, 0)
	svc := newServiceWithSnapshotCacheForTest(client, fakeScopeResolver{scope: scope}, fakeProviderResolver{provider: provider}, nil, cache, testMemberCursorSecret)
	params := testMembersParams()
	params.Limit = 1

	first, err := svc.Members(context.Background(), 1, params)
	if err != nil {
		t.Fatalf("Members(first) error = %v", err)
	}
	params.Cursor = first.NextCursor
	second, err := svc.Members(context.Background(), 1, params)
	if err != nil {
		t.Fatalf("Members(second) error = %v", err)
	}
	if len(second.Items) != 1 || second.Items[0].Rank != 2 || provider.trendCalls != 2 {
		t.Fatalf("second page = items %+v origin calls %d, want rank 2 after two authoritative reads", second.Items, provider.trendCalls)
	}
}

func TestMemberSnapshotIdentityIgnoresDepartmentMembershipOrder(t *testing.T) {
	tokens := int64(1000)
	left := []OverviewMember{{
		UserID: 1, DirectoryMemberExternalID: "member-alice", DepartmentExternalID: "department-alpha", DepartmentExternalIDs: []string{"department-beta", "department-alpha"}, DepartmentDisplayPath: "Department Alpha", TotalTokens: &tokens,
	}}
	right := []OverviewMember{{
		UserID: 1, DirectoryMemberExternalID: "member-alice", DepartmentExternalID: "department-alpha", DepartmentExternalIDs: []string{"department-alpha", "department-beta"}, DepartmentDisplayPath: "Department Alpha", TotalTokens: &tokens,
	}}

	leftID, err := memberSnapshotIdentity(left)
	if err != nil {
		t.Fatalf("memberSnapshotIdentity(left) error = %v", err)
	}
	rightID, err := memberSnapshotIdentity(right)
	if err != nil {
		t.Fatalf("memberSnapshotIdentity(right) error = %v", err)
	}
	if leftID != rightID {
		t.Fatalf("snapshot identities = %q/%q, want equal semantic content", leftID, rightID)
	}
}

func newMembersTestService(t *testing.T, count int, now func() time.Time) (*Service, *fakeRelayProvider, *representativescope.Scope) {
	t.Helper()
	client := testdb.Open(t)
	createPrimaryRelayProvider(t, client)
	scope, provider := membersTestData(count)
	if now == nil {
		fixed := time.Date(2026, 7, 16, 8, 0, 0, 0, time.UTC)
		now = func() time.Time { return fixed }
	}
	cache, _ := testSnapshotCacheWithClock(t, now, 0)
	svc := newServiceWithSnapshotCacheForTest(client, fakeScopeResolver{scope: scope}, fakeProviderResolver{provider: provider}, nil, cache, testMemberCursorSecret)
	return svc, provider, scope
}

func membersTestData(count int) (*representativescope.Scope, *fakeRelayProvider) {
	subjects := make([]representativescope.Subject, 0, count)
	stats := make(map[int64]relay.TeamUserUsageStats, count)
	trends := make(map[int64][]relay.UsageTrendPoint, count)
	for index := 1; index <= count; index++ {
		relayUserID := 10000 + index
		subject := representativescope.Subject{
			SubjectType: "member", DisplayName: fmt.Sprintf("Member %03d", index), Email: fmt.Sprintf("member-%03d@example.com", index),
			DepartmentExternalID: "department-alpha", DepartmentExternalIDs: []string{"department-alpha"},
			DepartmentDisplayPath: "Department Alpha", RelayUserID: &relayUserID, Selectable: true,
		}
		if index <= count/2 {
			subject.DirectoryMemberExternalID = fmt.Sprintf("member-%03d", index)
		} else {
			subject.UserID = index - count/2
		}
		tokens := int64(1000)
		subjects = append(subjects, subject)
		stats[int64(relayUserID)] = relay.TeamUserUsageStats{UserID: int64(relayUserID), TodayActualCost: 1, TotalActualCost: 2}
		trends[int64(relayUserID)] = []relay.UsageTrendPoint{{Date: "2026-07-07", ActualCost: 1, TotalTokens: &tokens}}
	}
	return &representativescope.Scope{
		Version: "scope-version-1", ActorUserID: 1, IsRepresentative: true, OverviewSubjects: subjects,
		MemberTreeRootIDs:     []string{"department-alpha"},
		MemberTreeDepartments: []representativescope.DepartmentScope{{ExternalID: "department-alpha", Name: "Department Alpha"}},
	}, &fakeRelayProvider{summaryStats: stats, trendPoints: trends}
}

func testMembersParams() MembersParams {
	return MembersParams{OverviewParams: OverviewParams{
		StartDate: "2026-07-01", EndDate: "2026-07-07", Granularity: "day", Timezone: "Asia/Shanghai",
	}}
}

func containsJSONField(encoded []byte, field string) bool {
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &payload); err != nil {
		return false
	}
	_, ok := payload[field]
	return ok
}

var _ readcache.Store = failingSnapshotStore{}
