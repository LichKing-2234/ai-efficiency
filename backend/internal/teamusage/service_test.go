package teamusage

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/teamusageratemultiplieraudit"
	entuser "github.com/ai-efficiency/backend/ent/user"
	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/ai-efficiency/backend/internal/representativescope"
	"github.com/ai-efficiency/backend/internal/testdb"
)

func TestSubjectDashboardReadsUniqueActiveGroupMultiplierMetadataInOneBatch(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	createPrimaryRelayProvider(t, client)

	scope := syntheticRepresentativeScope(1, 2, 2002, "alice@example.com")
	provider := &fakeRelayProvider{
		subscriptionsByUser: map[int64][]relay.UserSubscription{
			2002: {
				syntheticActiveSubscription(42, "Group Alpha", 1.25),
				syntheticActiveSubscription(7, "Group Beta", 0.75),
				syntheticActiveSubscription(42, "Group Alpha Secondary", 1.25),
				{
					GroupID: 99,
					Status:  "inactive",
					Group:   &relay.Group{ID: 99, Name: "Inactive Group"},
				},
			},
		},
		batchRateResults: []relay.GroupRateMultiplierReadResult{
			{GroupID: 42, Entries: []relay.UserGroupRateEntry{{UserID: 2002, RateMultiplier: floatPtr(1.5)}}},
			{GroupID: 7, Entries: []relay.UserGroupRateEntry{{UserID: 9001, RateMultiplier: floatPtr(2)}}},
		},
	}

	svc := NewService(client, fakeScopeResolver{scope: scope}, fakeProviderResolver{provider: provider}, nil)
	resp, err := svc.SubjectDashboard(ctx, 1, 2, relay.UserUsageDashboardParams{})
	if err != nil {
		t.Fatalf("SubjectDashboard() error = %v", err)
	}
	if provider.batchRateCalls != 1 {
		t.Fatalf("batch multiplier calls = %d, want 1", provider.batchRateCalls)
	}
	if got, want := provider.batchRateGroupIDs, [][]int64{{42, 7}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("batch group ids = %#v, want %#v", got, want)
	}
	if len(provider.listRateGroupIDs) != 0 {
		t.Fatalf("serial multiplier reads = %#v, want none", provider.listRateGroupIDs)
	}
	if len(resp.SubjectSubscriptionGroups) != 3 {
		t.Fatalf("subscription rows = %#v, want 3 active rows", resp.SubjectSubscriptionGroups)
	}
	for _, row := range resp.SubjectSubscriptionGroups {
		if row.MultiplierMetadataStatus != "ok" || row.MultiplierMetadataMessage != nil {
			t.Fatalf("row metadata = %q/%#v, want ok/nil: %#v", row.MultiplierMetadataStatus, row.MultiplierMetadataMessage, row)
		}
		if !row.Editable || row.EditableReason != nil {
			t.Fatalf("row edit policy = %v/%#v, want editable: %#v", row.Editable, row.EditableReason, row)
		}
		if row.EffectiveMultiplier == nil {
			t.Fatalf("row effective multiplier = nil, want successful metadata value: %#v", row)
		}
		switch row.GroupID {
		case "42":
			if *row.EffectiveMultiplier != 1.5 || row.MultiplierSource != "user" {
				t.Fatalf("group 42 multiplier = %#v/%q, want 1.5/user", row.EffectiveMultiplier, row.MultiplierSource)
			}
		case "7":
			if *row.EffectiveMultiplier != 0.75 || row.MultiplierSource != "group" {
				t.Fatalf("group 7 multiplier = %#v/%q, want 0.75/group", row.EffectiveMultiplier, row.MultiplierSource)
			}
		default:
			t.Fatalf("unexpected active group row: %#v", row)
		}
	}
}

func TestSubjectDashboardIsolatesFailedAndMissingBatchMultiplierMetadata(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	createPrimaryRelayProvider(t, client)

	scope := syntheticRepresentativeScope(1, 2, 2002, "alice@example.com")
	okSubscription := syntheticActiveSubscription(42, "Group Alpha", 1.25)
	okSubscription.Group.WeeklyLimitUSD = floatPtr(90)
	okSubscription.WeeklyUsageUSD = 12
	failedSubscription := syntheticActiveSubscription(43, "Group Beta", 1.5)
	failedSubscription.Group.WeeklyLimitUSD = floatPtr(70)
	failedSubscription.WeeklyUsageUSD = 34
	missingSubscription := syntheticActiveSubscription(44, "Group Gamma", 2)
	missingSubscription.Group.WeeklyLimitUSD = floatPtr(50)
	missingSubscription.WeeklyUsageUSD = 45
	provider := &fakeRelayProvider{
		subscriptionsByUser: map[int64][]relay.UserSubscription{
			2002: {okSubscription, failedSubscription, missingSubscription},
		},
		batchRateResults: []relay.GroupRateMultiplierReadResult{
			{GroupID: 42, Entries: []relay.UserGroupRateEntry{{UserID: 2002, RateMultiplier: floatPtr(1.75)}}},
			{GroupID: 43, Err: errors.New("synthetic group read failure")},
		},
	}

	svc := NewService(client, fakeScopeResolver{scope: scope}, fakeProviderResolver{provider: provider}, nil)
	resp, err := svc.SubjectDashboard(ctx, 1, 2, relay.UserUsageDashboardParams{StartDate: "2026-07-13", EndDate: "2026-07-16"})
	if err != nil {
		t.Fatalf("SubjectDashboard() error = %v", err)
	}
	if provider.batchRateCalls != 1 || len(provider.listRateGroupIDs) != 0 {
		t.Fatalf("multiplier reads = batch %d serial %#v, want one batch and no serial", provider.batchRateCalls, provider.listRateGroupIDs)
	}

	okRow := findSubscriptionRowByGroupID(resp.SubjectSubscriptionGroups, "42")
	if okRow == nil || okRow.MultiplierMetadataStatus != "ok" || okRow.EffectiveMultiplier == nil || *okRow.EffectiveMultiplier != 1.75 || !okRow.Editable {
		t.Fatalf("successful group row = %#v, want available editable 1.75 metadata", okRow)
	}
	for _, groupID := range []string{"43", "44"} {
		row := findSubscriptionRowByGroupID(resp.SubjectSubscriptionGroups, groupID)
		if row == nil {
			t.Fatalf("missing subscription row for group %s", groupID)
		}
		assertUnavailableMultiplierMetadata(t, row, ErrMultiplierMetadataUnavailable.Error())
	}
	failedRow := findSubscriptionRowByGroupID(resp.SubjectSubscriptionGroups, "43")
	if failedRow.WeeklyUsageUSD != 34 || failedRow.WeeklyLimitUSD == nil || *failedRow.WeeklyLimitUSD != 70 || failedRow.WeeklyEffectiveAllowanceUSD == nil || *failedRow.WeeklyEffectiveAllowanceUSD != 70 {
		t.Fatalf("failed metadata row usage/quota = %#v, want subscription values preserved", failedRow)
	}
}

func TestSubjectDashboardWithoutBatchCapabilityMarksMetadataUnavailableAndPreservesAuthorizationReason(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	createPrimaryRelayProvider(t, client)

	scope := syntheticRepresentativeScope(1, 2, 2002, "peer-rep@example.com")
	scope.RepresentedSubtreeIDs = map[string]map[string]struct{}{
		"department-root": {"department-root": {}, "department-child": {}},
	}
	scope.TargetRepresentedRoots = map[int][]string{2: {"department-root"}}
	subscription := syntheticActiveSubscription(42, "Group Alpha", 1.5)
	subscription.Group.MonthlyLimitUSD = floatPtr(300)
	subscription.MonthlyUsageUSD = 120
	base := &fakeRelayProvider{}
	provider := &fakeRelayProviderWithoutMultiplierBatch{
		Provider: base,
		subscriptionsByUser: map[int64][]relay.UserSubscription{
			2002: {subscription},
		},
	}

	svc := NewService(client, fakeScopeResolver{scope: scope}, fakeProviderResolver{provider: provider}, nil)
	resp, err := svc.SubjectDashboard(ctx, 1, 2, relay.UserUsageDashboardParams{})
	if err != nil {
		t.Fatalf("SubjectDashboard() error = %v", err)
	}
	if len(resp.SubjectSubscriptionGroups) != 1 {
		t.Fatalf("subscription rows = %#v, want 1 active row", resp.SubjectSubscriptionGroups)
	}
	row := resp.SubjectSubscriptionGroups[0]
	assertUnavailableMultiplierMetadata(t, &row, ErrNotUpperLevelRepresentative.Error())
	if row.MonthlyUsageUSD != 120 || row.MonthlyLimitUSD == nil || *row.MonthlyLimitUSD != 300 {
		t.Fatalf("row usage/quota = %#v, want subscription values preserved", row)
	}
	if len(base.listRateGroupIDs) != 0 {
		t.Fatalf("serial multiplier reads = %#v, want none without batch capability", base.listRateGroupIDs)
	}
}

func TestSubjectDashboardMarksPeerRepresentativeAsNonEditable(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	createPrimaryRelayProvider(t, client)

	scope := &representativescope.Scope{
		ActorUserID:      1,
		IsRepresentative: true,
		Subjects: []representativescope.Subject{
			{
				SubjectType: "member",
				UserID:      2,
				DisplayName: "Peer Rep",
				Email:       "peer-rep@example.com",
				RelayUserID: intPtr(2002),
				Selectable:  true,
			},
		},
		RepresentedSubtreeIDs: map[string]map[string]struct{}{
			"department-root": {
				"department-root":  {},
				"department-child": {},
			},
		},
		TargetRepresentedRoots: map[int][]string{
			2: {"department-root"},
		},
	}
	provider := &fakeRelayProvider{
		dashboardResponse: &relay.UserUsageDashboardResponse{
			Configured: true,
			Range: relay.UserUsageDashboardRange{
				StartDate:   "2026-06-01",
				EndDate:     "2026-06-30",
				Granularity: "day",
				Timezone:    "UTC",
			},
			GroupQuotas: relay.UserUsageGroupQuotaState{Status: "ok", Groups: []relay.UserUsageGroupQuotaGroupItem{}},
		},
		subscriptionsByUser: map[int64][]relay.UserSubscription{
			2002: {
				{
					GroupID: 42,
					Status:  "active",
					Group: &relay.Group{
						ID:              42,
						Name:            "Group Alpha",
						Platform:        "openai",
						RateMultiplier:  floatPtr(1.5),
						MonthlyLimitUSD: floatPtr(300),
						WeeklyLimitUSD:  floatPtr(90),
						DailyLimitUSD:   floatPtr(20),
					},
					MonthlyUsageUSD: 120,
				},
			},
		},
		rateEntriesByGroup: map[int64][]relay.UserGroupRateEntry{
			42: {{UserID: 2002, RateMultiplier: floatPtr(1.5)}},
		},
	}

	svc := NewService(client, fakeScopeResolver{scope: scope}, fakeProviderResolver{provider: provider}, nil)
	resp, err := svc.SubjectDashboard(ctx, 1, 2, relay.UserUsageDashboardParams{})
	if err != nil {
		t.Fatalf("SubjectDashboard() error = %v", err)
	}
	if len(resp.SubjectSubscriptionGroups) != 1 {
		t.Fatalf("subscription rows = %#v, want 1 row", resp.SubjectSubscriptionGroups)
	}
	row := resp.SubjectSubscriptionGroups[0]
	if row.Editable {
		t.Fatalf("row.Editable = true, want false for peer representative: %#v", row)
	}
	if row.EditableReason == nil || *row.EditableReason != ErrNotUpperLevelRepresentative.Error() {
		t.Fatalf("row.EditableReason = %#v, want %q", row.EditableReason, ErrNotUpperLevelRepresentative.Error())
	}
}

func TestSubjectDashboardRejectsOutOfScopeTargetBeforeRelayLookup(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	createPrimaryRelayProvider(t, client)

	scope := &representativescope.Scope{
		ActorUserID:      1,
		IsRepresentative: true,
		Subjects: []representativescope.Subject{
			{
				SubjectType: "member",
				UserID:      2,
				DisplayName: "Alice",
				Email:       "alice@example.com",
				RelayUserID: intPtr(2002),
				Selectable:  true,
			},
		},
	}
	provider := &fakeRelayProvider{}
	svc := NewService(client, fakeScopeResolver{scope: scope}, fakeProviderResolver{provider: provider}, nil)

	_, err := svc.SubjectDashboard(ctx, 1, 999, relay.UserUsageDashboardParams{})
	if err == nil {
		t.Fatal("SubjectDashboard() error = nil, want out_of_scope")
	}
	var forbidden *ForbiddenError
	if !errors.As(err, &forbidden) || forbidden.Reason != ErrOutOfScope.Error() {
		t.Fatalf("SubjectDashboard() error = %v, want ForbiddenError(%q)", err, ErrOutOfScope)
	}
	if len(provider.dashboardRequestUserIDs) != 0 || len(provider.subscriptionRequestUserIDs) != 0 {
		t.Fatalf("relay calls = dashboard %#v subscription %#v, want none for out-of-scope target", provider.dashboardRequestUserIDs, provider.subscriptionRequestUserIDs)
	}
}

func TestUpdateMultiplierClosesAuditOnScopeResolutionFailure(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	svc := NewService(client, fakeScopeResolver{err: errors.New("scope backend down")}, fakeProviderResolver{}, nil)

	_, err := svc.UpdateMultiplier(ctx, 11, 22, 42, UpdateMultiplierRequest{Mode: "reset"})
	if err == nil {
		t.Fatal("UpdateMultiplier() error = nil, want scope resolution failure")
	}

	row := client.TeamUsageRateMultiplierAudit.Query().OnlyX(ctx)
	if row.Status == teamusageratemultiplieraudit.StatusRunning {
		t.Fatalf("audit status = %s, want closed status", row.Status)
	}
	if row.Status != teamusageratemultiplieraudit.StatusFailed {
		t.Fatalf("audit status = %s, want failed", row.Status)
	}
}

func TestOverviewReturnsHardErrorForTrendAuthorizationFailure(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	createPrimaryRelayProvider(t, client)

	scope := &representativescope.Scope{
		ActorUserID:      1,
		IsRepresentative: true,
		Subjects: []representativescope.Subject{
			{SubjectType: "member", UserID: 2, DisplayName: "Alice", Email: "alice@example.com", RelayUserID: intPtr(1002), Selectable: true},
			{SubjectType: "member", UserID: 3, DisplayName: "Bob", Email: "bob@example.com", RelayUserID: intPtr(1003), Selectable: true},
		},
	}
	provider := &fakeRelayProvider{
		summaryStats: map[int64]relay.TeamUserUsageStats{
			1002: {UserID: 1002, TotalActualCost: 12, TodayActualCost: 2},
			1003: {UserID: 1003, TotalActualCost: 20, TodayActualCost: 5},
		},
		trendErr: relay.ErrInvalidCredentials,
	}
	svc := NewService(client, fakeScopeResolver{scope: scope}, fakeProviderResolver{provider: provider}, nil)

	_, err := svc.Overview(ctx, 1, OverviewParams{
		StartDate:   "2026-06-01",
		EndDate:     "2026-06-30",
		Granularity: "day",
		Timezone:    "UTC",
	})
	if !errors.Is(err, relay.ErrInvalidCredentials) {
		t.Fatalf("Overview() error = %v, want relay.ErrInvalidCredentials", err)
	}
}

func TestOverviewFetchesTopMemberTrendInOneBatch(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	createPrimaryRelayProvider(t, client)
	tokenAlice := int64(9000)
	tokenBob := int64(1000)

	scope := &representativescope.Scope{
		ActorUserID:      1,
		IsRepresentative: true,
		Subjects: []representativescope.Subject{
			{SubjectType: "member", UserID: 2, DisplayName: "Alice", Email: "alice@example.com", RelayUserID: intPtr(1002), Selectable: true},
			{SubjectType: "member", UserID: 3, DisplayName: "Bob", Email: "bob@example.org", RelayUserID: intPtr(1003), Selectable: true},
		},
	}
	provider := &fakeRelayProvider{
		summaryStats: map[int64]relay.TeamUserUsageStats{
			1002: {UserID: 1002, TotalActualCost: 12, TodayActualCost: 2},
			1003: {UserID: 1003, TotalActualCost: 20, TodayActualCost: 5},
		},
		trendPoints: map[int64][]relay.UsageTrendPoint{
			1002: {{Date: "2026-06-28", ActualCost: 2, TotalTokens: &tokenAlice}},
			1003: {{Date: "2026-06-28", ActualCost: 5, TotalTokens: &tokenBob}},
		},
	}
	svc := NewService(client, fakeScopeResolver{scope: scope}, fakeProviderResolver{provider: provider}, nil)

	resp, err := svc.Overview(ctx, 1, OverviewParams{
		StartDate:   "2026-06-01",
		EndDate:     "2026-06-30",
		Granularity: "day",
		Timezone:    "UTC",
	})
	if err != nil {
		t.Fatalf("Overview() error = %v", err)
	}
	if provider.trendCalls != 1 {
		t.Fatalf("trend calls = %d, want 1 batched call", provider.trendCalls)
	}
	if got, want := provider.trendRequestUserIDs, []int64{1002, 1003}; !reflect.DeepEqual(got, want) {
		t.Fatalf("trend request user ids = %#v, want %#v", got, want)
	}
	if got := []int{resp.TopMemberTrend.Series[0].UserID, resp.TopMemberTrend.Series[1].UserID}; !reflect.DeepEqual(got, []int{2, 3}) {
		t.Fatalf("trend series user ids = %#v, want ranked Alice then Bob by tokens", got)
	}
}

func TestOverviewAggregatesAndRanksMembersBySelectedWindowTrend(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	createPrimaryRelayProvider(t, client)

	token1002 := int64(1000)
	token1003a := int64(1500)
	token1003b := int64(2000)
	scope := &representativescope.Scope{
		ActorUserID:      1,
		IsRepresentative: true,
		Subjects: []representativescope.Subject{
			{SubjectType: "member", UserID: 2, DisplayName: "Alice", Email: "alice@example.com", RelayUserID: intPtr(1002), Selectable: true},
			{SubjectType: "member", UserID: 3, DisplayName: "Bob", Email: "bob@example.org", RelayUserID: intPtr(1003), Selectable: true},
		},
	}
	provider := &fakeRelayProvider{
		summaryStats: map[int64]relay.TeamUserUsageStats{
			1002: {UserID: 1002, TodayActualCost: 1, TotalActualCost: 10},
			1003: {UserID: 1003, TodayActualCost: 2, TotalActualCost: 99},
		},
		trendPoints: map[int64][]relay.UsageTrendPoint{
			1002: {{Date: "2026-06-24", ActualCost: 30, TotalTokens: &token1002}},
			1003: {
				{Date: "2026-06-23", ActualCost: 7, TotalTokens: &token1003a},
				{Date: "2026-06-24", ActualCost: 8, TotalTokens: &token1003b},
			},
		},
	}
	svc := NewService(client, fakeScopeResolver{scope: scope}, fakeProviderResolver{provider: provider}, nil)

	resp, err := svc.Overview(ctx, 1, OverviewParams{
		StartDate:   "2026-06-18",
		EndDate:     "2026-06-24",
		Granularity: "day",
		Timezone:    "UTC",
	})
	if err != nil {
		t.Fatalf("Overview() error = %v", err)
	}

	if resp.Summary.RangeActualCost == nil || *resp.Summary.RangeActualCost != 45 {
		t.Fatalf("summary range_actual_cost = %#v, want 45", resp.Summary.RangeActualCost)
	}
	if resp.Summary.RangeTotalTokens == nil || *resp.Summary.RangeTotalTokens != 4500 {
		t.Fatalf("summary range_total_tokens = %#v, want 4500", resp.Summary.RangeTotalTokens)
	}
	if resp.Summary.TotalActualCost != nil {
		t.Fatalf("summary total_actual_cost = %#v, want nil for selected-window Team Overview", resp.Summary.TotalActualCost)
	}
	if got, want := resp.TopMemberTrend.RankBasis, "range_total_tokens"; got != want {
		t.Fatalf("rank basis = %q, want %q", got, want)
	}
	if got := []int{resp.Members[0].UserID, resp.Members[1].UserID}; !reflect.DeepEqual(got, []int{3, 2}) {
		t.Fatalf("members order = %#v, want Bob ranked before Alice by selected-window tokens", got)
	}
	if resp.Members[0].RangeActualCost != 15 || resp.Members[1].RangeActualCost != 30 {
		t.Fatalf("member range costs = %.2f / %.2f, want 15 / 30", resp.Members[0].RangeActualCost, resp.Members[1].RangeActualCost)
	}
	if resp.Members[0].TotalTokens == nil || *resp.Members[0].TotalTokens != 3500 {
		t.Fatalf("Bob total tokens = %#v, want 3500 from selected-window trend", resp.Members[0].TotalTokens)
	}
	if resp.Members[1].TotalTokens == nil || *resp.Members[1].TotalTokens != 1000 {
		t.Fatalf("Alice total tokens = %#v, want 1000 from selected-window trend", resp.Members[1].TotalTokens)
	}
	if got := []int{resp.TopMemberTrend.Series[0].UserID, resp.TopMemberTrend.Series[1].UserID}; !reflect.DeepEqual(got, []int{3, 2}) {
		t.Fatalf("trend series user ids = %#v, want selected-window token ranking Bob then Alice", got)
	}
	if got, want := provider.trendRequestUserIDs, []int64{1002, 1003}; !reflect.DeepEqual(got, want) {
		t.Fatalf("trend request user ids = %#v, want %#v", got, want)
	}
}

func TestOverviewBuildsMemberTreeFromRepresentativeDepartments(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	createPrimaryRelayProvider(t, client)
	tokenAlice := int64(1200)
	tokenBob := int64(3000)

	scope := &representativescope.Scope{
		ActorUserID:      1,
		IsRepresentative: true,
		Subjects: []representativescope.Subject{
			{SubjectType: "member", UserID: 2, DirectoryMemberExternalID: "member-alice", DisplayName: "Alice", Email: "alice@example.com", DepartmentExternalID: "department-alpha-team-one", RelayUserID: intPtr(1002), Selectable: true},
			{SubjectType: "member", UserID: 3, DirectoryMemberExternalID: "member-bob", DisplayName: "Bob", Email: "bob@example.org", DepartmentExternalID: "department-beta", RelayUserID: intPtr(1003), Selectable: true},
			{SubjectType: "member", DirectoryMemberExternalID: "member-carol", DisplayName: "Carol", Email: "carol@example.net", DepartmentExternalID: "department-alpha-team-one", Selectable: false},
		},
		OverviewSubjects: []representativescope.Subject{
			{SubjectType: "member", UserID: 2, DirectoryMemberExternalID: "member-alice", DisplayName: "Alice", Email: "alice@example.com", DepartmentExternalID: "department-alpha-team-one", RelayUserID: intPtr(1002), Selectable: true},
			{SubjectType: "member", UserID: 3, DirectoryMemberExternalID: "member-bob", DisplayName: "Bob", Email: "bob@example.org", DepartmentExternalID: "department-beta", RelayUserID: intPtr(1003), Selectable: true},
			{SubjectType: "member", DirectoryMemberExternalID: "member-carol", DisplayName: "Carol", Email: "carol@example.net", DepartmentExternalID: "department-alpha-team-one", Selectable: false},
		},
		MemberTreeRootIDs: []string{"department-alpha", "department-beta"},
		MemberTreeDepartments: []representativescope.DepartmentScope{
			{ExternalID: "department-alpha", Name: "Department Alpha", DisplayPath: "Department Alpha", Depth: 5, ChildCount: 1},
			{ExternalID: "department-alpha-team-one", ParentExternalID: stringPtr("department-alpha"), Name: "Team One", DisplayPath: "Department Alpha / Team One", Depth: 6, ChildCount: 0},
			{ExternalID: "department-beta", Name: "Department Beta", DisplayPath: "Department Beta", Depth: 5, ChildCount: 0},
		},
	}
	provider := &fakeRelayProvider{
		summaryStats: map[int64]relay.TeamUserUsageStats{
			1002: {UserID: 1002, TodayActualCost: 2, TotalActualCost: 20},
			1003: {UserID: 1003, TodayActualCost: 3, TotalActualCost: 30},
		},
		trendPoints: map[int64][]relay.UsageTrendPoint{
			1002: {{Date: "2026-06-28", ActualCost: 12, TotalTokens: &tokenAlice}},
			1003: {{Date: "2026-06-28", ActualCost: 30, TotalTokens: &tokenBob}},
		},
	}
	svc := NewService(client, fakeScopeResolver{scope: scope}, fakeProviderResolver{provider: provider}, nil)

	resp, err := svc.Overview(ctx, 1, OverviewParams{StartDate: "2026-06-01", EndDate: "2026-06-30", Granularity: "day", Timezone: "UTC"})
	if err != nil {
		t.Fatalf("Overview() error = %v", err)
	}

	if len(resp.MemberTree) != 2 {
		t.Fatalf("member tree = %#v, want two top-level departments", resp.MemberTree)
	}
	alpha := resp.MemberTree[0]
	if alpha.DepartmentExternalID != "department-alpha" || alpha.Depth != 0 || len(alpha.Children) != 1 || len(alpha.Members) != 0 {
		t.Fatalf("alpha node = %#v, want root with one child and no direct members", alpha)
	}
	if alpha.MemberCount != 2 || alpha.ConnectedMemberCount != 1 || alpha.RangeActualCost != 12 {
		t.Fatalf("alpha summary = members %d connected %d cost %.2f, want 2 / 1 / 12", alpha.MemberCount, alpha.ConnectedMemberCount, alpha.RangeActualCost)
	}
	if alpha.RangeTotalTokens == nil || *alpha.RangeTotalTokens != 1200 {
		t.Fatalf("alpha range tokens = %#v, want 1200", alpha.RangeTotalTokens)
	}
	teamOne := alpha.Children[0]
	if teamOne.DepartmentExternalID != "department-alpha-team-one" || teamOne.Depth != 1 || len(teamOne.Members) != 2 {
		t.Fatalf("team one = %#v, want relative child depth 1 with two direct members", teamOne)
	}
	if teamOne.Members[1].Email != "carol@example.net" || teamOne.Members[1].RelayUserID != nil {
		t.Fatalf("unconnected member = %#v, want Carol without relay", teamOne.Members[1])
	}
	beta := resp.MemberTree[1]
	if beta.DepartmentExternalID != "department-beta" || beta.Depth != 0 || len(beta.Members) != 1 {
		t.Fatalf("beta node = %#v, want relative root depth 0 with one direct member", beta)
	}
	if beta.RangeTotalTokens == nil || *beta.RangeTotalTokens != 3000 {
		t.Fatalf("beta range tokens = %#v, want 3000", beta.RangeTotalTokens)
	}
}

func TestOverviewBuildsTeamAndSubteamTokenTrendFromRepresentativeDepartments(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	createPrimaryRelayProvider(t, client)
	aliceDayOneTokens := int64(100)
	aliceDayTwoTokens := int64(200)
	bobDayOneTokens := int64(50)
	bobDayTwoTokens := int64(70)

	scope := &representativescope.Scope{
		ActorUserID:      1,
		IsRepresentative: true,
		Subjects: []representativescope.Subject{
			{SubjectType: "member", UserID: 2, DirectoryMemberExternalID: "member-alice", DisplayName: "Alice", Email: "alice@example.com", DepartmentExternalID: "department-alpha-team-one", RelayUserID: intPtr(1002), Selectable: true},
			{SubjectType: "member", UserID: 3, DirectoryMemberExternalID: "member-bob", DisplayName: "Bob", Email: "bob@example.org", DepartmentExternalID: "department-alpha-team-two", RelayUserID: intPtr(1003), Selectable: true},
			{SubjectType: "member", DirectoryMemberExternalID: "member-carol", DisplayName: "Carol", Email: "carol@example.net", DepartmentExternalID: "department-alpha-team-one", Selectable: false},
		},
		MemberTreeRootIDs: []string{"department-alpha"},
		MemberTreeDepartments: []representativescope.DepartmentScope{
			{ExternalID: "department-alpha", Name: "Department Alpha", DisplayPath: "Department Alpha", Depth: 5, ChildCount: 2},
			{ExternalID: "department-alpha-team-one", ParentExternalID: stringPtr("department-alpha"), Name: "Team One", DisplayPath: "Department Alpha / Team One", Depth: 6, ChildCount: 0},
			{ExternalID: "department-alpha-team-two", ParentExternalID: stringPtr("department-alpha"), Name: "Team Two", DisplayPath: "Department Alpha / Team Two", Depth: 6, ChildCount: 0},
		},
	}
	provider := &fakeRelayProvider{
		summaryStats: map[int64]relay.TeamUserUsageStats{
			1002: {UserID: 1002, TodayActualCost: 2, TotalActualCost: 20},
			1003: {UserID: 1003, TodayActualCost: 3, TotalActualCost: 30},
		},
		trendPoints: map[int64][]relay.UsageTrendPoint{
			1002: {
				{Date: "2026-06-27", ActualCost: 1, TotalTokens: &aliceDayOneTokens},
				{Date: "2026-06-28", ActualCost: 2, TotalTokens: &aliceDayTwoTokens},
			},
			1003: {
				{Date: "2026-06-27", ActualCost: 3, TotalTokens: &bobDayOneTokens},
				{Date: "2026-06-28", ActualCost: 4, TotalTokens: &bobDayTwoTokens},
			},
		},
	}
	svc := NewService(client, fakeScopeResolver{scope: scope}, fakeProviderResolver{provider: provider}, nil)

	resp, err := svc.Overview(ctx, 1, OverviewParams{StartDate: "2026-06-01", EndDate: "2026-06-30", Granularity: "day", Timezone: "UTC"})
	if err != nil {
		t.Fatalf("Overview() error = %v", err)
	}

	if resp.DepartmentTrend.Unavailable {
		t.Fatalf("department trend unavailable = true, reason = %#v", resp.DepartmentTrend.UnavailableReason)
	}
	if got := len(resp.DepartmentTrend.Series); got != 3 {
		t.Fatalf("department trend series = %d, want team total plus two subteams: %#v", got, resp.DepartmentTrend.Series)
	}
	total := resp.DepartmentTrend.Series[0]
	if total.SeriesType != "team_total" || total.DisplayName != "Team total" {
		t.Fatalf("total series = %#v, want team_total Team total", total)
	}
	if got := []int64{*total.Points[0].TotalTokens, *total.Points[1].TotalTokens}; !reflect.DeepEqual(got, []int64{150, 270}) {
		t.Fatalf("total trend tokens = %#v, want 150 / 270", got)
	}
	if got := []float64{total.Points[0].ActualCost, total.Points[1].ActualCost}; !reflect.DeepEqual(got, []float64{4, 6}) {
		t.Fatalf("total trend costs = %#v, want 4 / 6", got)
	}
	teamOne := resp.DepartmentTrend.Series[1]
	if teamOne.SeriesType != "department" || teamOne.DepartmentExternalID != "department-alpha-team-one" || teamOne.DisplayName != "Team One" {
		t.Fatalf("first subteam series = %#v, want Team One department", teamOne)
	}
	if got := []int64{*teamOne.Points[0].TotalTokens, *teamOne.Points[1].TotalTokens}; !reflect.DeepEqual(got, []int64{100, 200}) {
		t.Fatalf("team one trend tokens = %#v, want 100 / 200", got)
	}
	teamTwo := resp.DepartmentTrend.Series[2]
	if teamTwo.SeriesType != "department" || teamTwo.DepartmentExternalID != "department-alpha-team-two" || teamTwo.DisplayName != "Team Two" {
		t.Fatalf("second subteam series = %#v, want Team Two department", teamTwo)
	}
}

func TestOverviewMemberDetailsIncludesScopedMembersWithoutRelayUsage(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	createPrimaryRelayProvider(t, client)

	scope := &representativescope.Scope{
		ActorUserID:      1,
		IsRepresentative: true,
		Subjects: []representativescope.Subject{
			{SubjectType: "member", UserID: 2, DisplayName: "Alice", Email: "alice@example.com", RelayUserID: intPtr(1002), Selectable: true},
			{SubjectType: "member", DirectoryMemberExternalID: "member-bob", DisplayName: "Bob", Email: "bob@example.org", Selectable: false},
			{SubjectType: "member", DirectoryMemberExternalID: "member-carol", DisplayName: "Carol", Email: "carol@example.net", Selectable: false},
			{SubjectType: "member", DirectoryMemberExternalID: "member-dana", DisplayName: "Dana", Email: "dana@example.net", Selectable: false},
		},
	}
	provider := &fakeRelayProvider{
		summaryStats: map[int64]relay.TeamUserUsageStats{
			1002: {UserID: 1002, TodayActualCost: 2, TotalActualCost: 20},
		},
		trendPoints: map[int64][]relay.UsageTrendPoint{
			1002: {{Date: "2026-06-28", ActualCost: 8}},
		},
	}
	svc := NewService(client, fakeScopeResolver{scope: scope}, fakeProviderResolver{provider: provider}, nil)

	resp, err := svc.Overview(ctx, 1, OverviewParams{
		StartDate:   "2026-06-01",
		EndDate:     "2026-06-30",
		Granularity: "day",
		Timezone:    "UTC",
	})
	if err != nil {
		t.Fatalf("Overview() error = %v", err)
	}

	if resp.Summary.MemberCount != 4 {
		t.Fatalf("summary member_count = %d, want 4", resp.Summary.MemberCount)
	}
	if resp.Summary.RelayMemberCount != 1 {
		t.Fatalf("summary relay_member_count = %d, want 1", resp.Summary.RelayMemberCount)
	}
	if got := len(resp.Members); got != 4 {
		t.Fatalf("members length = %d, want all 4 scoped members: %#v", got, resp.Members)
	}
	if got := []string{resp.Members[0].Email, resp.Members[1].Email, resp.Members[2].Email, resp.Members[3].Email}; !reflect.DeepEqual(got, []string{"alice@example.com", "bob@example.org", "carol@example.net", "dana@example.net"}) {
		t.Fatalf("member emails = %#v, want used member first then zero-usage scoped members", got)
	}
	if resp.Members[1].RelayUserID != nil || resp.Members[1].RangeActualCost != 0 || resp.Members[1].TotalTokens != nil {
		t.Fatalf("member without relay = %#v, want no relay id and zero usage", resp.Members[1])
	}
	if resp.Members[1].DirectoryMemberExternalID != "member-bob" || resp.Members[1].UserID != 0 || resp.Members[1].Selectable {
		t.Fatalf("directory-only member = %#v, want directory id, user_id 0, and selectable false", resp.Members[1])
	}
	if got, want := provider.summaryRequestUserIDs, []int64{1002}; !reflect.DeepEqual(got, want) {
		t.Fatalf("summary user ids = %#v, want only relay-backed members %#v", got, want)
	}
	if got, want := provider.trendRequestUserIDs, []int64{1002}; !reflect.DeepEqual(got, want) {
		t.Fatalf("trend user ids = %#v, want only relay-backed members %#v", got, want)
	}
}

func TestOverviewResolvesDirectoryOnlyMembersByEmailForUsage(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	createPrimaryRelayProvider(t, client)

	scope := &representativescope.Scope{
		ActorUserID:      1,
		IsRepresentative: true,
		Subjects: []representativescope.Subject{
			{SubjectType: "member", UserID: 2, DisplayName: "Alice", Email: "alice@example.com", RelayUserID: intPtr(1002), Selectable: true},
			{SubjectType: "member", DirectoryMemberExternalID: "member-bob", DisplayName: "Bob", Email: "bob@example.org", Selectable: false},
			{SubjectType: "member", DirectoryMemberExternalID: "member-carol", DisplayName: "Carol", Email: "carol@example.net", Selectable: false},
		},
	}
	provider := &fakeRelayProvider{
		usersByID: map[int64]*relay.User{
			1002: {ID: 1002, Email: "alice@example.com", Username: "alice"},
		},
		usersByEmail: map[string]*relay.User{
			"bob@example.org": {ID: 2002, Email: "bob@example.org", Username: "bob"},
		},
		summaryStats: map[int64]relay.TeamUserUsageStats{
			1002: {UserID: 1002, TodayActualCost: 2, TotalActualCost: 20},
			2002: {UserID: 2002, TodayActualCost: 3, TotalActualCost: 30},
		},
		trendPoints: map[int64][]relay.UsageTrendPoint{
			1002: {{Date: "2026-06-28", ActualCost: 8, TotalTokens: int64Ptr(800)}},
			2002: {{Date: "2026-06-28", ActualCost: 12, TotalTokens: int64Ptr(1200)}},
		},
	}
	svc := NewService(client, fakeScopeResolver{scope: scope}, fakeProviderResolver{provider: provider}, nil)

	resp, err := svc.Overview(ctx, 1, OverviewParams{
		StartDate:   "2026-06-01",
		EndDate:     "2026-06-30",
		Granularity: "day",
		Timezone:    "UTC",
	})
	if err != nil {
		t.Fatalf("Overview() error = %v", err)
	}

	if resp.Summary.MemberCount != 3 {
		t.Fatalf("summary member_count = %d, want full directory roster count 3", resp.Summary.MemberCount)
	}
	if resp.Summary.RelayMemberCount != 2 {
		t.Fatalf("summary relay_member_count = %d, want two email-resolved relay users", resp.Summary.RelayMemberCount)
	}
	if got, want := provider.summaryRequestUserIDs, []int64{1002, 2002}; !reflect.DeepEqual(got, want) {
		t.Fatalf("summary user ids = %#v, want local and email-resolved members %#v", got, want)
	}
	if got, want := provider.trendRequestUserIDs, []int64{1002, 2002}; !reflect.DeepEqual(got, want) {
		t.Fatalf("trend user ids = %#v, want local and email-resolved members %#v", got, want)
	}

	bob := findOverviewMemberByEmail(resp.Members, "bob@example.org")
	if bob == nil {
		t.Fatalf("members = %#v, want bob@example.org", resp.Members)
	}
	if bob.RelayUserID == nil || *bob.RelayUserID != 2002 {
		t.Fatalf("bob relay_user_id = %#v, want email-resolved relay user 2002", bob.RelayUserID)
	}
	if bob.RangeActualCost != 12 || bob.TodayActualCost != 3 || bob.TotalActualCost != 30 {
		t.Fatalf("bob usage = range %.2f today %.2f total %.2f, want email-resolved usage", bob.RangeActualCost, bob.TodayActualCost, bob.TotalActualCost)
	}
	if bob.UserID != 0 || bob.Selectable {
		t.Fatalf("bob identity = user_id %d selectable %v, want directory-only row not openable", bob.UserID, bob.Selectable)
	}
}

func TestSubjectDashboardReconcilesStaleRelayUserIDFromPrimaryProviderEmail(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	createPrimaryRelayProvider(t, client)
	target := createTeamUsageUser(t, client, "stale-target", "stale-target@example.com", intPtr(29))

	scope := &representativescope.Scope{
		ActorUserID:      999,
		IsRepresentative: true,
		Subjects: []representativescope.Subject{
			{
				SubjectType: "member",
				UserID:      target.ID,
				DisplayName: "Stale Target",
				Email:       target.Email,
				RelayUserID: intPtr(29),
				Selectable:  true,
			},
		},
	}
	provider := &fakeRelayProvider{
		usersByID: map[int64]*relay.User{
			29: {ID: 29, Email: "other-user@example.com", Username: "other-user"},
		},
		usersByEmail: map[string]*relay.User{
			"stale-target@example.com": {ID: 2, Email: "stale-target@example.com", Username: "stale-target"},
		},
		subscriptionsByUser: map[int64][]relay.UserSubscription{
			2: {
				{
					GroupID: 42,
					Status:  "active",
					Group: &relay.Group{
						ID:              42,
						Name:            "Group Alpha",
						Platform:        "openai",
						RateMultiplier:  floatPtr(1.0),
						WeeklyLimitUSD:  floatPtr(2000),
						MonthlyLimitUSD: floatPtr(10000),
					},
					WeeklyUsageUSD:  203.30303185,
					MonthlyUsageUSD: 113.6844942,
				},
			},
			29: {
				{
					GroupID: 42,
					Status:  "active",
					Group: &relay.Group{
						ID:              42,
						Name:            "Group Alpha",
						Platform:        "openai",
						RateMultiplier:  floatPtr(1.0),
						WeeklyLimitUSD:  floatPtr(2000),
						MonthlyLimitUSD: floatPtr(10000),
					},
					WeeklyUsageUSD:  44.393931,
					MonthlyUsageUSD: 301.386244,
				},
			},
		},
	}
	svc := NewService(client, fakeScopeResolver{scope: scope}, fakeProviderResolver{provider: provider}, nil)

	resp, err := svc.SubjectDashboard(ctx, 999, target.ID, relay.UserUsageDashboardParams{})
	if err != nil {
		t.Fatalf("SubjectDashboard() error = %v", err)
	}
	if len(resp.SubjectSubscriptionGroups) != 1 {
		t.Fatalf("subscription rows = %#v, want 1 row", resp.SubjectSubscriptionGroups)
	}
	row := resp.SubjectSubscriptionGroups[0]
	if row.WeeklyUsageUSD != 203.30303185 || row.MonthlyUsageUSD != 113.6844942 {
		t.Fatalf("usage = weekly %.8f monthly %.8f, want reconciled relay user 2 usage", row.WeeklyUsageUSD, row.MonthlyUsageUSD)
	}
	updated := client.User.GetX(ctx, target.ID)
	if updated.RelayUserID == nil || *updated.RelayUserID != 2 {
		t.Fatalf("persisted relay_user_id = %#v, want 2", updated.RelayUserID)
	}
	if got, want := provider.dashboardRequestUserIDs, []int64{2}; !reflect.DeepEqual(got, want) {
		t.Fatalf("dashboard user ids = %#v, want %#v", got, want)
	}
	if got, want := provider.subscriptionRequestUserIDs, []int64{2}; !reflect.DeepEqual(got, want) {
		t.Fatalf("subscription user ids = %#v, want %#v", got, want)
	}
}

func TestOverviewReconcilesStaleRelayUserIDBeforeBatchUsageAndTrend(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	createPrimaryRelayProvider(t, client)
	target := createTeamUsageUser(t, client, "overview-target", "overview-target@example.com", intPtr(29))

	scope := &representativescope.Scope{
		ActorUserID:      999,
		IsRepresentative: true,
		Subjects: []representativescope.Subject{
			{
				SubjectType: "member",
				UserID:      target.ID,
				DisplayName: "Overview Target",
				Email:       target.Email,
				RelayUserID: intPtr(29),
				Selectable:  true,
			},
		},
	}
	provider := &fakeRelayProvider{
		usersByID: map[int64]*relay.User{
			29: {ID: 29, Email: "other-user@example.com", Username: "other-user"},
		},
		usersByEmail: map[string]*relay.User{
			"overview-target@example.com": {ID: 2, Email: "overview-target@example.com", Username: "overview-target"},
		},
		summaryStats: map[int64]relay.TeamUserUsageStats{
			2: {UserID: 2, TotalActualCost: 113.6844942, TodayActualCost: 2.8071306},
		},
		trendPoints: map[int64][]relay.UsageTrendPoint{
			2: {{Date: "2026-06-28", ActualCost: 2.8071306}},
		},
	}
	svc := NewService(client, fakeScopeResolver{scope: scope}, fakeProviderResolver{provider: provider}, nil)

	resp, err := svc.Overview(ctx, 999, OverviewParams{
		StartDate:   "2026-06-01",
		EndDate:     "2026-06-28",
		Granularity: "day",
		Timezone:    "Asia/Shanghai",
	})
	if err != nil {
		t.Fatalf("Overview() error = %v", err)
	}
	if len(resp.Members) != 1 {
		t.Fatalf("members = %#v, want 1", resp.Members)
	}
	member := resp.Members[0]
	if member.RelayUserID == nil || *member.RelayUserID != 2 {
		t.Fatalf("member relay_user_id = %#v, want 2", member.RelayUserID)
	}
	if member.TotalActualCost != 113.6844942 {
		t.Fatalf("member total_actual_cost = %.8f, want reconciled relay user 2 cost", member.TotalActualCost)
	}
	if got, want := provider.summaryRequestUserIDs, []int64{2}; !reflect.DeepEqual(got, want) {
		t.Fatalf("summary user ids = %#v, want %#v", got, want)
	}
	if got, want := provider.trendRequestUserIDs, []int64{2}; !reflect.DeepEqual(got, want) {
		t.Fatalf("trend user ids = %#v, want %#v", got, want)
	}
	updated := client.User.GetX(ctx, target.ID)
	if updated.RelayUserID == nil || *updated.RelayUserID != 2 {
		t.Fatalf("persisted relay_user_id = %#v, want 2", updated.RelayUserID)
	}
}

func TestOverviewUsesRelayUserDirectoryForCachedRelayBindings(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	createPrimaryRelayProvider(t, client)
	staleTarget := createTeamUsageUser(t, client, "overview-stale", "overview-stale@example.com", intPtr(29))

	scope := &representativescope.Scope{
		ActorUserID:      999,
		IsRepresentative: true,
		Subjects: []representativescope.Subject{
			{
				SubjectType: "member",
				UserID:      101,
				DisplayName: "Alice",
				Email:       "alice@example.com",
				RelayUserID: intPtr(1001),
				Selectable:  true,
			},
			{
				SubjectType: "member",
				UserID:      staleTarget.ID,
				DisplayName: "Overview Stale",
				Email:       staleTarget.Email,
				RelayUserID: intPtr(29),
				Selectable:  true,
			},
		},
	}
	provider := &fakeRelayProvider{
		usersByID: map[int64]*relay.User{
			29:   {ID: 29, Email: "other-user@example.com", Username: "other-user"},
			1001: {ID: 1001, Email: "alice@example.com", Username: "alice"},
		},
		usersByEmail: map[string]*relay.User{
			"overview-stale@example.com": {ID: 2002, Email: "overview-stale@example.com", Username: "overview-stale"},
		},
		summaryStats: map[int64]relay.TeamUserUsageStats{
			1001: {UserID: 1001, TotalActualCost: 12, TodayActualCost: 1},
			2002: {UserID: 2002, TotalActualCost: 24, TodayActualCost: 2},
		},
		trendPoints: map[int64][]relay.UsageTrendPoint{
			1001: {{Date: "2026-06-28", ActualCost: 3}},
			2002: {{Date: "2026-06-28", ActualCost: 6}},
		},
	}
	svc := NewService(client, fakeScopeResolver{scope: scope}, fakeProviderResolver{provider: provider}, nil)

	resp, err := svc.Overview(ctx, 999, OverviewParams{
		StartDate:   "2026-06-01",
		EndDate:     "2026-06-28",
		Granularity: "day",
		Timezone:    "UTC",
	})
	if err != nil {
		t.Fatalf("Overview() error = %v", err)
	}
	if provider.listUsersCalls != 1 {
		t.Fatalf("list users calls = %d, want 1 batched directory lookup", provider.listUsersCalls)
	}
	if len(provider.getUserRequestUserIDs) != 0 {
		t.Fatalf("GetUser calls = %#v, want none in Team Overview hot path", provider.getUserRequestUserIDs)
	}
	if got, want := provider.summaryRequestUserIDs, []int64{1001, 2002}; !reflect.DeepEqual(got, want) {
		t.Fatalf("summary user ids = %#v, want %#v", got, want)
	}
	if got, want := provider.trendRequestUserIDs, []int64{1001, 2002}; !reflect.DeepEqual(got, want) {
		t.Fatalf("trend user ids = %#v, want %#v", got, want)
	}
	member := findOverviewMemberByEmail(resp.Members, staleTarget.Email)
	if member == nil || member.RelayUserID == nil || *member.RelayUserID != 2002 {
		t.Fatalf("stale member = %#v, want relay_user_id 2002 from relay user directory", member)
	}
	updated := client.User.GetX(ctx, staleTarget.ID)
	if updated.RelayUserID == nil || *updated.RelayUserID != 2002 {
		t.Fatalf("persisted relay_user_id = %#v, want 2002", updated.RelayUserID)
	}
}

func TestOverviewReturnsUnavailableSummaryWhenTrendFetchTimesOut(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	createPrimaryRelayProvider(t, client)

	scope := &representativescope.Scope{
		ActorUserID:      999,
		IsRepresentative: true,
		Subjects: []representativescope.Subject{
			{
				SubjectType: "member",
				UserID:      101,
				DisplayName: "Alice",
				Email:       "alice@example.com",
				RelayUserID: intPtr(1001),
				Selectable:  true,
			},
		},
	}
	provider := &fakeRelayProvider{
		summaryStats: map[int64]relay.TeamUserUsageStats{
			1001: {UserID: 1001, TotalActualCost: 12.3, TodayActualCost: 1.2},
		},
		trendWait: 200 * time.Millisecond,
	}
	svc := NewService(client, fakeScopeResolver{scope: scope}, fakeProviderResolver{provider: provider}, nil)
	svc.teamOverviewTrendTimeout = 10 * time.Millisecond

	start := time.Now()
	resp, err := svc.Overview(ctx, 999, OverviewParams{
		StartDate:   "2026-06-01",
		EndDate:     "2026-06-30",
		Granularity: "day",
		Timezone:    "UTC",
	})
	if err != nil {
		t.Fatalf("Overview() error = %v", err)
	}
	if elapsed := time.Since(start); elapsed >= 100*time.Millisecond {
		t.Fatalf("Overview() elapsed = %s, want trend timeout to return quickly", elapsed)
	}
	if !resp.Summary.Unavailable || resp.Summary.UnavailableReason == nil || *resp.Summary.UnavailableReason != "provider_error" {
		t.Fatalf("summary unavailable = %v reason = %#v, want provider_error", resp.Summary.Unavailable, resp.Summary.UnavailableReason)
	}
	if resp.Summary.RangeActualCost != nil || resp.Summary.RangeTotalTokens != nil {
		t.Fatalf("summary range totals = %#v/%#v, want nil when trend totals are unavailable", resp.Summary.RangeActualCost, resp.Summary.RangeTotalTokens)
	}
	if !resp.TopMemberTrend.Unavailable || resp.TopMemberTrend.UnavailableReason == nil || *resp.TopMemberTrend.UnavailableReason != "provider_error" {
		t.Fatalf("trend unavailable = %v reason = %#v, want provider_error", resp.TopMemberTrend.Unavailable, resp.TopMemberTrend.UnavailableReason)
	}
	if len(resp.TopMembers) != 0 || len(resp.TopMemberTrend.Series) != 0 {
		t.Fatalf("top members = %d trend series = %d, want no unavailable selected-window ranking", len(resp.TopMembers), len(resp.TopMemberTrend.Series))
	}
}

func TestSubjectDashboardBatchReadDoesNotChangeAuthoritativeMultiplierEditAndAuditPath(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	createPrimaryRelayProvider(t, client)
	target := createTeamUsageUser(t, client, "edit-target", "edit-target@example.com", intPtr(2002))

	scope := syntheticRepresentativeScope(999, target.ID, 2002, target.Email)
	provider := &fakeRelayProvider{
		subscriptionsByUser: map[int64][]relay.UserSubscription{
			2002: {syntheticActiveSubscription(42, "Group Alpha", 1)},
		},
		rateEntriesByGroup: map[int64][]relay.UserGroupRateEntry{
			42: {
				{UserID: 2002, RateMultiplier: floatPtr(1), RPMOverride: intPtr(80)},
				{UserID: 3003, RateMultiplier: floatPtr(1.2), RPMOverride: intPtr(120)},
			},
		},
	}
	provider.replaceFn = func(_ context.Context, groupID int64, inputs []relay.GroupRateMultiplierInput) error {
		provider.rateEntriesByGroup[groupID] = make([]relay.UserGroupRateEntry, 0, len(inputs))
		for _, input := range inputs {
			provider.rateEntriesByGroup[groupID] = append(provider.rateEntriesByGroup[groupID], relay.UserGroupRateEntry{
				UserID:         input.UserID,
				RateMultiplier: input.RateMultiplier,
				RPMOverride:    input.RPMOverride,
			})
		}
		return nil
	}
	locker := &fakeLocker{}
	svc := NewService(client, fakeScopeResolver{scope: scope}, fakeProviderResolver{provider: provider}, locker)

	dashboard, err := svc.SubjectDashboard(ctx, 999, target.ID, relay.UserUsageDashboardParams{})
	if err != nil {
		t.Fatalf("SubjectDashboard() error = %v", err)
	}
	if len(dashboard.SubjectSubscriptionGroups) != 1 || dashboard.SubjectSubscriptionGroups[0].EffectiveMultiplier == nil || *dashboard.SubjectSubscriptionGroups[0].EffectiveMultiplier != 1 {
		t.Fatalf("dashboard subscription rows = %#v, want batch-read multiplier 1", dashboard.SubjectSubscriptionGroups)
	}
	if provider.batchRateCalls != 1 || len(provider.listRateGroupIDs) != 0 {
		t.Fatalf("dashboard multiplier reads = batch %d serial %#v, want one batch and no serial", provider.batchRateCalls, provider.listRateGroupIDs)
	}

	resp, err := svc.UpdateMultiplier(ctx, 999, target.ID, 42, UpdateMultiplierRequest{
		Mode:           "set",
		RateMultiplier: floatPtr(1.5),
		Reason:         "Synthetic approved adjustment",
	})
	if err != nil {
		t.Fatalf("UpdateMultiplier() error = %v", err)
	}
	if resp.Status != teamusageratemultiplieraudit.StatusSucceeded.String() || !resp.Changed {
		t.Fatalf("UpdateMultiplier() response = %#v, want succeeded changed edit", resp)
	}
	if got, want := provider.listRateGroupIDs, []int64{42, 42, 42}; !reflect.DeepEqual(got, want) {
		t.Fatalf("authoritative single-group reads = %#v, want initial/lock/readback %#v", got, want)
	}
	if provider.batchRateCalls != 1 {
		t.Fatalf("batch multiplier calls after edit = %d, want dashboard-only call unchanged", provider.batchRateCalls)
	}
	if provider.replaceCalls != 1 || locker.lockCalls != 1 {
		t.Fatalf("mutation calls = replace %d lock %d, want one whole-group replace under one lock", provider.replaceCalls, locker.lockCalls)
	}
	if len(provider.replaceInputs) != 2 {
		t.Fatalf("whole-group replace inputs = %#v, want target and existing peer", provider.replaceInputs)
	}
	targetInput := findRateMultiplierInputByUserID(provider.replaceInputs, 2002)
	peerInput := findRateMultiplierInputByUserID(provider.replaceInputs, 3003)
	if targetInput == nil || targetInput.RateMultiplier == nil || *targetInput.RateMultiplier != 1.5 || targetInput.RPMOverride == nil || *targetInput.RPMOverride != 80 {
		t.Fatalf("target replacement input = %#v, want multiplier 1.5 with RPM override preserved", targetInput)
	}
	if peerInput == nil || peerInput.RateMultiplier == nil || *peerInput.RateMultiplier != 1.2 || peerInput.RPMOverride == nil || *peerInput.RPMOverride != 120 {
		t.Fatalf("peer replacement input = %#v, want whole-group state preserved", peerInput)
	}
	audit := client.TeamUsageRateMultiplierAudit.GetX(ctx, resp.AuditID)
	if audit.Status != teamusageratemultiplieraudit.StatusSucceeded || !audit.Changed || audit.OldMultiplier == nil || *audit.OldMultiplier != 1 || audit.NewMultiplier == nil || *audit.NewMultiplier != 1.5 {
		t.Fatalf("audit row = %#v, want successful authoritative 1 -> 1.5 change", audit)
	}
	if audit.OldMultiplierSource != teamusageratemultiplieraudit.OldMultiplierSourceUser || audit.NewMultiplierSource != teamusageratemultiplieraudit.NewMultiplierSourceUser || audit.ErrorMessage != "" {
		t.Fatalf("audit sources/error = %q/%q/%q, want user/user/empty", audit.OldMultiplierSource, audit.NewMultiplierSource, audit.ErrorMessage)
	}
}

func TestUpdateMultiplierReconcilesStaleRelayUserIDBeforeWrite(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	createPrimaryRelayProvider(t, client)
	target := createTeamUsageUser(t, client, "write-target", "write-target@example.com", intPtr(29))

	scope := &representativescope.Scope{
		ActorUserID:      999,
		IsRepresentative: true,
		Subjects: []representativescope.Subject{
			{SubjectType: "member", UserID: target.ID, DisplayName: "Write Target", Email: target.Email, RelayUserID: intPtr(29), Selectable: true},
		},
	}
	provider := &fakeRelayProvider{
		usersByID: map[int64]*relay.User{
			29: {ID: 29, Email: "other-user@example.com", Username: "other-user"},
		},
		usersByEmail: map[string]*relay.User{
			"write-target@example.com": {ID: 2, Email: "write-target@example.com", Username: "write-target"},
		},
		subscriptionsByUser: map[int64][]relay.UserSubscription{
			2: {
				{
					GroupID: 42,
					Status:  "active",
					Group: &relay.Group{
						ID:              42,
						Name:            "Group Alpha",
						Platform:        "openai",
						RateMultiplier:  floatPtr(1.0),
						MonthlyLimitUSD: floatPtr(10000),
					},
					MonthlyUsageUSD: 113.6844942,
				},
			},
		},
		rateEntriesByGroup: map[int64][]relay.UserGroupRateEntry{
			42: {{UserID: 2, RateMultiplier: floatPtr(1.0)}},
		},
	}
	provider.replaceFn = func(_ context.Context, _ int64, inputs []relay.GroupRateMultiplierInput) error {
		provider.rateEntriesByGroup[42] = make([]relay.UserGroupRateEntry, 0, len(inputs))
		for _, input := range inputs {
			provider.rateEntriesByGroup[42] = append(provider.rateEntriesByGroup[42], relay.UserGroupRateEntry{
				UserID:         input.UserID,
				RateMultiplier: input.RateMultiplier,
				RPMOverride:    input.RPMOverride,
			})
		}
		return nil
	}
	svc := NewService(client, fakeScopeResolver{scope: scope}, fakeProviderResolver{provider: provider}, &fakeLocker{})

	resp, err := svc.UpdateMultiplier(ctx, 999, target.ID, 42, UpdateMultiplierRequest{
		Mode:           "set",
		RateMultiplier: floatPtr(2.0),
	})
	if err != nil {
		t.Fatalf("UpdateMultiplier() error = %v", err)
	}
	if resp.Status != teamusageratemultiplieraudit.StatusSucceeded.String() {
		t.Fatalf("status = %q, want succeeded", resp.Status)
	}
	if provider.replaceCalls != 1 {
		t.Fatalf("replace calls = %d, want 1", provider.replaceCalls)
	}
	if got, want := provider.replaceUserIDs, []int64{2}; !reflect.DeepEqual(got, want) {
		t.Fatalf("replace user ids = %#v, want %#v", got, want)
	}
	row := client.TeamUsageRateMultiplierAudit.GetX(ctx, resp.AuditID)
	if row.RelayUserID == nil || *row.RelayUserID != 2 {
		t.Fatalf("audit relay_user_id = %#v, want 2", row.RelayUserID)
	}
	updated := client.User.GetX(ctx, target.ID)
	if updated.RelayUserID == nil || *updated.RelayUserID != 2 {
		t.Fatalf("persisted relay_user_id = %#v, want 2", updated.RelayUserID)
	}
}

func TestAuditListRedactsOutOfScopeMetadataForRepresentativeView(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	client.TeamUsageRateMultiplierAudit.Create().
		SetActorUserID(1).
		SetTargetUserID(2).
		SetGroupID("42").
		SetGroupName("Group Alpha").
		SetAction(teamusageratemultiplieraudit.ActionSetRateMultiplier).
		SetStatus(teamusageratemultiplieraudit.StatusRejected).
		SetRejectionReason(teamusageratemultiplieraudit.RejectionReasonOutOfScope).
		SetScopeEvidence(map[string]any{
			"target_display_name": "Bob",
			"target_email":        "bob@example.com",
		}).
		SetRequestMetadata(map[string]any{
			"mode":                "set",
			"has_rate_multiplier": true,
		}).
		SetReason("Synthetic reason").
		SaveX(ctx)

	svc := NewService(client, nil, nil, nil)

	userResp, err := svc.ListAudit(ctx, 1, AuditListParams{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("ListAudit() error = %v", err)
	}
	if len(userResp.Items) != 1 {
		t.Fatalf("ListAudit() items = %#v, want 1 row", userResp.Items)
	}
	userRow := userResp.Items[0]
	if userRow.TargetUserID != nil {
		t.Fatalf("representative target_user_id = %#v, want nil", userRow.TargetUserID)
	}
	if userRow.TargetDisplayName != "" || userRow.TargetEmail != "" {
		t.Fatalf("representative target fields = %#v / %#v, want redacted", userRow.TargetDisplayName, userRow.TargetEmail)
	}
	if userRow.RequestMetadata != nil {
		t.Fatalf("representative request metadata = %#v, want nil", userRow.RequestMetadata)
	}

	adminResp, err := svc.ListAdminAudit(ctx, AdminAuditListParams{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("ListAdminAudit() error = %v", err)
	}
	adminRow := adminResp.Items[0]
	if adminRow.TargetUserID == nil || *adminRow.TargetUserID != 2 {
		t.Fatalf("admin target_user_id = %#v, want 2", adminRow.TargetUserID)
	}
	if adminRow.TargetDisplayName != "Bob" || adminRow.TargetEmail != "bob@example.com" {
		t.Fatalf("admin target fields = %#v / %#v, want Bob / bob@example.com", adminRow.TargetDisplayName, adminRow.TargetEmail)
	}
	if adminRow.RequestMetadata == nil || adminRow.RequestMetadata["mode"] != "set" {
		t.Fatalf("admin request metadata = %#v, want mode=set", adminRow.RequestMetadata)
	}
}

func TestUpdateMultiplierOutOfScopeExistingTargetRedactsRepresentativeAuditButKeepsAdminMetadata(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	target := createTeamUsageUser(t, client, "blocked-target", "blocked-target@example.com", intPtr(3102))

	scope := &representativescope.Scope{
		ActorUserID:      999,
		IsRepresentative: true,
		Subjects: []representativescope.Subject{
			{
				SubjectType: "member",
				UserID:      1001,
				DisplayName: "Other Member",
				Email:       "other-member@example.com",
				RelayUserID: intPtr(9001),
				Selectable:  true,
			},
		},
	}
	svc := NewService(client, fakeScopeResolver{scope: scope}, fakeProviderResolver{}, nil)

	_, err := svc.UpdateMultiplier(ctx, 999, target.ID, 42, UpdateMultiplierRequest{Mode: "reset"})
	if err == nil {
		t.Fatal("UpdateMultiplier() error = nil, want out_of_scope forbidden error")
	}
	var forbidden *ForbiddenError
	if !errors.As(err, &forbidden) || forbidden.Reason != ErrOutOfScope.Error() {
		t.Fatalf("UpdateMultiplier() error = %v, want ForbiddenError(%q)", err, ErrOutOfScope)
	}

	row := client.TeamUsageRateMultiplierAudit.Query().OnlyX(ctx)
	if row.Status != teamusageratemultiplieraudit.StatusRejected {
		t.Fatalf("audit status = %s, want rejected", row.Status)
	}
	if row.RejectionReason == nil || *row.RejectionReason != teamusageratemultiplieraudit.RejectionReasonOutOfScope {
		t.Fatalf("audit rejection_reason = %#v, want out_of_scope", row.RejectionReason)
	}

	userResp, err := svc.ListAudit(ctx, 999, AuditListParams{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("ListAudit() error = %v", err)
	}
	if len(userResp.Items) != 1 {
		t.Fatalf("ListAudit() items = %#v, want 1 row", userResp.Items)
	}
	userRow := userResp.Items[0]
	if userRow.TargetUserID != nil {
		t.Fatalf("representative target_user_id = %#v, want nil", userRow.TargetUserID)
	}
	if userRow.TargetDisplayName != "" || userRow.TargetEmail != "" {
		t.Fatalf("representative target fields = %#v / %#v, want redacted", userRow.TargetDisplayName, userRow.TargetEmail)
	}
	if userRow.RequestMetadata != nil {
		t.Fatalf("representative request metadata = %#v, want nil", userRow.RequestMetadata)
	}

	adminResp, err := svc.ListAdminAudit(ctx, AdminAuditListParams{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("ListAdminAudit() error = %v", err)
	}
	if len(adminResp.Items) != 1 {
		t.Fatalf("ListAdminAudit() items = %#v, want 1 row", adminResp.Items)
	}
	adminRow := adminResp.Items[0]
	if adminRow.TargetUserID == nil || *adminRow.TargetUserID != target.ID {
		t.Fatalf("admin target_user_id = %#v, want %d", adminRow.TargetUserID, target.ID)
	}
	if adminRow.TargetDisplayName != target.Username || adminRow.TargetEmail != target.Email {
		t.Fatalf("admin target fields = %#v / %#v, want %q / %q", adminRow.TargetDisplayName, adminRow.TargetEmail, target.Username, target.Email)
	}
	if adminRow.RequestMetadata == nil || adminRow.RequestMetadata["mode"] != "reset" {
		t.Fatalf("admin request metadata = %#v, want mode=reset", adminRow.RequestMetadata)
	}
}

func TestUpdateMultiplierLockTimeProviderFailureMarksAuditFailed(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	createPrimaryRelayProvider(t, client)
	target := createTeamUsageUser(t, client, "lock-target", "lock-target@example.com", intPtr(3202))

	scope := &representativescope.Scope{
		ActorUserID:      999,
		IsRepresentative: true,
		Subjects: []representativescope.Subject{
			{SubjectType: "member", UserID: target.ID, DisplayName: "Lock Target", Email: target.Email, RelayUserID: intPtr(3202), Selectable: true},
		},
	}
	provider := &fakeRelayProvider{
		subscriptionsByUser: map[int64][]relay.UserSubscription{
			3202: {
				{
					GroupID: 42,
					Status:  "active",
					Group: &relay.Group{
						ID:              42,
						Name:            "Group Alpha",
						Platform:        "openai",
						RateMultiplier:  floatPtr(1.0),
						MonthlyLimitUSD: floatPtr(200),
					},
					MonthlyUsageUSD: 80,
				},
			},
		},
		rateEntriesByGroup: map[int64][]relay.UserGroupRateEntry{
			42: {{UserID: 3202, RateMultiplier: floatPtr(1.0)}},
		},
		replaceErr: errors.New("relay replace exploded"),
	}
	svc := NewService(client, fakeScopeResolver{scope: scope}, fakeProviderResolver{provider: provider}, &fakeLocker{})

	_, err := svc.UpdateMultiplier(ctx, 999, target.ID, 42, UpdateMultiplierRequest{
		Mode:           "set",
		RateMultiplier: floatPtr(2.0),
	})
	if err == nil {
		t.Fatal("UpdateMultiplier() error = nil, want relay provider failure")
	}
	if !strings.Contains(err.Error(), "replace group rate multipliers: relay replace exploded") {
		t.Fatalf("UpdateMultiplier() error = %v, want relay replace failure", err)
	}

	row := client.TeamUsageRateMultiplierAudit.Query().OnlyX(ctx)
	if row.Status != teamusageratemultiplieraudit.StatusFailed {
		t.Fatalf("audit status = %s, want failed", row.Status)
	}
	if !strings.Contains(row.ErrorMessage, "relay replace exploded") {
		t.Fatalf("audit error_message = %q, want relay replace failure detail", row.ErrorMessage)
	}
}

func TestUpdateMultiplierLockTimeAuditUpdateFailureIsSurfaced(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	createPrimaryRelayProvider(t, client)
	target := createTeamUsageUser(t, client, "audit-missing-target", "audit-missing-target@example.com", intPtr(3302))

	scope := &representativescope.Scope{
		ActorUserID:      999,
		IsRepresentative: true,
		Subjects: []representativescope.Subject{
			{SubjectType: "member", UserID: target.ID, DisplayName: "Audit Missing Target", Email: target.Email, RelayUserID: intPtr(3302), Selectable: true},
		},
	}
	provider := &fakeRelayProvider{
		subscriptionsByUser: map[int64][]relay.UserSubscription{
			3302: {
				{
					GroupID: 42,
					Status:  "active",
					Group: &relay.Group{
						ID:              42,
						Name:            "Group Alpha",
						Platform:        "openai",
						RateMultiplier:  floatPtr(1.0),
						MonthlyLimitUSD: floatPtr(200),
					},
					MonthlyUsageUSD: 80,
				},
			},
		},
		rateEntriesByGroup: map[int64][]relay.UserGroupRateEntry{
			42: {{UserID: 3302, RateMultiplier: floatPtr(1.0)}},
		},
		replaceFn: func(ctx context.Context, _ int64, _ []relay.GroupRateMultiplierInput) error {
			if _, delErr := client.TeamUsageRateMultiplierAudit.Delete().Exec(ctx); delErr != nil {
				t.Fatalf("delete audit row: %v", delErr)
			}
			return errors.New("relay replace exploded")
		},
	}
	svc := NewService(client, fakeScopeResolver{scope: scope}, fakeProviderResolver{provider: provider}, &fakeLocker{})

	_, err := svc.UpdateMultiplier(ctx, 999, target.ID, 42, UpdateMultiplierRequest{
		Mode:           "set",
		RateMultiplier: floatPtr(2.0),
	})
	if err == nil {
		t.Fatal("UpdateMultiplier() error = nil, want combined provider and audit-update failure")
	}
	if !strings.Contains(err.Error(), "replace group rate multipliers: relay replace exploded") {
		t.Fatalf("UpdateMultiplier() error = %v, want relay replace failure", err)
	}
	if !strings.Contains(err.Error(), "team usage audit update failed") {
		t.Fatalf("UpdateMultiplier() error = %v, want audit update failure detail", err)
	}
}

func TestUpdateMultiplierNoOpSkipsRelayReplacementAndSucceeds(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	createPrimaryRelayProvider(t, client)
	target := createTeamUsageUser(t, client, "target-user", "target-user@example.com", intPtr(3002))

	scope := &representativescope.Scope{
		ActorUserID:      999,
		IsRepresentative: true,
		Subjects: []representativescope.Subject{
			{SubjectType: "member", UserID: target.ID, DisplayName: "Target User", Email: target.Email, RelayUserID: intPtr(3002), Selectable: true},
		},
	}
	provider := &fakeRelayProvider{
		dashboardResponse: &relay.UserUsageDashboardResponse{
			Configured:  true,
			GroupQuotas: relay.UserUsageGroupQuotaState{Status: "ok", Groups: []relay.UserUsageGroupQuotaGroupItem{}},
		},
		subscriptionsByUser: map[int64][]relay.UserSubscription{
			3002: {
				{
					GroupID: 42,
					Status:  "active",
					Group: &relay.Group{
						ID:              42,
						Name:            "Group Alpha",
						Platform:        "openai",
						RateMultiplier:  floatPtr(1.0),
						MonthlyLimitUSD: floatPtr(200),
					},
					MonthlyUsageUSD: 80,
				},
			},
		},
		rateEntriesByGroup: map[int64][]relay.UserGroupRateEntry{
			42: {{UserID: 3002, RateMultiplier: floatPtr(2.0)}},
		},
	}
	locker := &fakeLocker{}
	svc := NewService(client, fakeScopeResolver{scope: scope}, fakeProviderResolver{provider: provider}, locker)

	resp, err := svc.UpdateMultiplier(ctx, 999, target.ID, 42, UpdateMultiplierRequest{
		Mode:           "set",
		RateMultiplier: floatPtr(2.0),
	})
	if err != nil {
		t.Fatalf("UpdateMultiplier() error = %v", err)
	}
	if resp.Status != teamusageratemultiplieraudit.StatusSucceeded.String() {
		t.Fatalf("response status = %q, want succeeded", resp.Status)
	}
	if locker.lockCalls != 0 {
		t.Fatalf("locker lock calls = %d, want 0 for no-op write", locker.lockCalls)
	}
	if provider.replaceCalls != 0 {
		t.Fatalf("ReplaceGroupRateMultipliers calls = %d, want 0", provider.replaceCalls)
	}

	row := client.TeamUsageRateMultiplierAudit.GetX(ctx, resp.AuditID)
	if row.Status != teamusageratemultiplieraudit.StatusSucceeded || row.Changed {
		t.Fatalf("audit row status/changed = %s/%v, want succeeded/false", row.Status, row.Changed)
	}
}

func TestUpdateMultiplierReturnsErrorWhenAuditEndsPartialFailed(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	createPrimaryRelayProvider(t, client)
	target := createTeamUsageUser(t, client, "partial-target", "partial-target@example.com", intPtr(3402))

	scope := &representativescope.Scope{
		ActorUserID:      999,
		IsRepresentative: true,
		Subjects: []representativescope.Subject{
			{SubjectType: "member", UserID: target.ID, DisplayName: "Partial Target", Email: target.Email, RelayUserID: intPtr(3402), Selectable: true},
		},
	}
	provider := &fakeRelayProvider{
		subscriptionsByUser: map[int64][]relay.UserSubscription{
			3402: {
				{
					GroupID: 42,
					Status:  "active",
					Group: &relay.Group{
						ID:              42,
						Name:            "Group Alpha",
						Platform:        "openai",
						RateMultiplier:  floatPtr(1.0),
						MonthlyLimitUSD: floatPtr(200),
					},
					MonthlyUsageUSD: 80,
				},
			},
		},
		rateEntriesByGroup: map[int64][]relay.UserGroupRateEntry{
			42: {{UserID: 3402, RateMultiplier: floatPtr(1.0)}},
		},
	}
	provider.replaceFn = func(_ context.Context, _ int64, _ []relay.GroupRateMultiplierInput) error {
		return nil
	}
	svc := NewService(client, fakeScopeResolver{scope: scope}, fakeProviderResolver{provider: provider}, &fakeLocker{})

	_, err := svc.UpdateMultiplier(ctx, 999, target.ID, 42, UpdateMultiplierRequest{
		Mode:           "set",
		RateMultiplier: floatPtr(2.0),
	})
	if err == nil {
		t.Fatal("UpdateMultiplier() error = nil, want partial_failed error")
	}

	row := client.TeamUsageRateMultiplierAudit.Query().OnlyX(ctx)
	if row.Status != teamusageratemultiplieraudit.StatusPartialFailed {
		t.Fatalf("audit status = %s, want partial_failed", row.Status)
	}
}

type fakeScopeResolver struct {
	scope *representativescope.Scope
	err   error
}

func (f fakeScopeResolver) Resolve(context.Context, int) (*representativescope.Scope, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.scope, nil
}

type fakeProviderResolver struct {
	provider relay.Provider
	err      error
}

func (f fakeProviderResolver) Resolve(context.Context, int) (relay.Provider, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.provider, nil
}

type fakeLocker struct {
	lockCalls int
}

func (f *fakeLocker) WithProviderGroupLock(ctx context.Context, providerID int, groupID int64, fn func(context.Context) error) error {
	f.lockCalls++
	return fn(ctx)
}

type fakeRelayProvider struct {
	dashboardResponse          *relay.UserUsageDashboardResponse
	dashboardErr               error
	usersByID                  map[int64]*relay.User
	usersByEmail               map[string]*relay.User
	subscriptionsByUser        map[int64][]relay.UserSubscription
	subscriptionErr            error
	subscriptionRequestUserIDs []int64
	rateEntriesByGroup         map[int64][]relay.UserGroupRateEntry
	batchRateResults           []relay.GroupRateMultiplierReadResult
	batchRateCalls             int
	batchRateGroupIDs          [][]int64
	listRatesErr               error
	listRateGroupIDs           []int64
	replaceFn                  func(context.Context, int64, []relay.GroupRateMultiplierInput) error
	replaceErr                 error
	replaceCalls               int
	replaceUserIDs             []int64
	replaceInputs              []relay.GroupRateMultiplierInput
	summaryStats               map[int64]relay.TeamUserUsageStats
	summaryErr                 error
	summaryRequestUserIDs      []int64
	trendPoints                map[int64][]relay.UsageTrendPoint
	trendErr                   error
	trendWait                  time.Duration
	trendCalls                 int
	trendRequestUserIDs        []int64
	dashboardRequestUserIDs    []int64
	listUsersCalls             int
	getUserRequestUserIDs      []int64
}

func (f *fakeRelayProvider) Ping(context.Context) error { return nil }
func (f *fakeRelayProvider) Name() string               { return "fake-relay" }

func (f *fakeRelayProvider) Authenticate(context.Context, string, string) (*relay.User, error) {
	return nil, nil
}

func (f *fakeRelayProvider) GetUser(_ context.Context, userID int64) (*relay.User, error) {
	f.getUserRequestUserIDs = append(f.getUserRequestUserIDs, userID)
	if f.usersByID == nil {
		return nil, nil
	}
	user := f.usersByID[userID]
	if user == nil {
		return nil, nil
	}
	copy := *user
	return &copy, nil
}

func (f *fakeRelayProvider) ListUsers(context.Context) ([]relay.User, error) {
	f.listUsersCalls++
	usersByID := map[int64]relay.User{}
	for _, user := range f.usersByID {
		if user == nil {
			continue
		}
		usersByID[user.ID] = *user
	}
	for _, user := range f.usersByEmail {
		if user == nil {
			continue
		}
		usersByID[user.ID] = *user
	}
	users := make([]relay.User, 0, len(usersByID))
	for _, user := range usersByID {
		users = append(users, user)
	}
	sort.Slice(users, func(i, j int) bool {
		return users[i].ID < users[j].ID
	})
	return users, nil
}

func (f *fakeRelayProvider) ListAllowedGroupsForUser(context.Context, int64) ([]relay.Group, error) {
	return nil, nil
}

func (f *fakeRelayProvider) FindUserByEmail(_ context.Context, email string) (*relay.User, error) {
	if f.usersByEmail == nil {
		return nil, nil
	}
	user := f.usersByEmail[strings.ToLower(strings.TrimSpace(email))]
	if user == nil {
		return nil, nil
	}
	copy := *user
	return &copy, nil
}

func (f *fakeRelayProvider) FindUserByUsername(context.Context, string) (*relay.User, error) {
	return nil, nil
}

func (f *fakeRelayProvider) CreateUser(context.Context, relay.CreateUserRequest) (*relay.User, error) {
	return nil, nil
}

func (f *fakeRelayProvider) UpdateUser(context.Context, int64, relay.UpdateUserRequest) (*relay.User, error) {
	return nil, nil
}

func (f *fakeRelayProvider) ChatCompletion(context.Context, relay.ChatCompletionRequest) (*relay.ChatCompletionResponse, error) {
	return nil, nil
}

func (f *fakeRelayProvider) ChatCompletionWithTools(context.Context, relay.ChatCompletionRequest, []relay.ToolDef) (*relay.ChatCompletionWithToolsResponse, error) {
	return nil, nil
}

func (f *fakeRelayProvider) GetUsageStats(context.Context, int64, time.Time, time.Time) (*relay.UsageStats, error) {
	return nil, nil
}

func (f *fakeRelayProvider) ListUserAPIKeys(context.Context, int64) ([]relay.APIKey, error) {
	return nil, nil
}

func (f *fakeRelayProvider) CreateUserAPIKey(context.Context, int64, relay.APIKeyCreateRequest) (*relay.APIKeyWithSecret, error) {
	return nil, nil
}

func (f *fakeRelayProvider) UpdateUserAPIKeyStatus(context.Context, int64, string) error {
	return nil
}

func (f *fakeRelayProvider) RevokeUserAPIKey(context.Context, int64) error {
	return nil
}

func (f *fakeRelayProvider) ListUsageLogsByAPIKeyExact(context.Context, int64, time.Time, time.Time) ([]relay.UsageLog, error) {
	return nil, nil
}

func (f *fakeRelayProvider) GetUserUsageDashboard(context.Context, string, string, relay.UserUsageDashboardParams) (*relay.UserUsageDashboardResponse, error) {
	return nil, nil
}

func (f *fakeRelayProvider) GetUsageDashboardForUser(_ context.Context, relayUserID int64, _ relay.UserUsageDashboardParams) (*relay.UserUsageDashboardResponse, error) {
	if f.dashboardErr != nil {
		return nil, f.dashboardErr
	}
	f.dashboardRequestUserIDs = append(f.dashboardRequestUserIDs, relayUserID)
	if f.dashboardResponse == nil {
		return &relay.UserUsageDashboardResponse{
			Configured:  true,
			GroupQuotas: relay.UserUsageGroupQuotaState{Status: "ok", Groups: []relay.UserUsageGroupQuotaGroupItem{}},
		}, nil
	}
	return f.dashboardResponse, nil
}

func (f *fakeRelayProvider) GetBatchUserUsageStats(_ context.Context, relayUserIDs []int64, _ relay.TeamUsageSummaryParams) (map[int64]relay.TeamUserUsageStats, error) {
	if f.summaryErr != nil {
		return nil, f.summaryErr
	}
	f.summaryRequestUserIDs = append(f.summaryRequestUserIDs, relayUserIDs...)
	return f.summaryStats, nil
}

func (f *fakeRelayProvider) GetUsageTrendForUsers(ctx context.Context, relayUserIDs []int64, _ relay.TeamMemberTrendParams) (map[int64][]relay.UsageTrendPoint, error) {
	if f.trendErr != nil {
		return nil, f.trendErr
	}
	if f.trendWait > 0 {
		select {
		case <-time.After(f.trendWait):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	f.trendCalls++
	f.trendRequestUserIDs = append([]int64(nil), relayUserIDs...)
	return f.trendPoints, nil
}

func (f *fakeRelayProvider) ListUserSubscriptions(_ context.Context, relayUserID int64) ([]relay.UserSubscription, error) {
	if f.subscriptionErr != nil {
		return nil, f.subscriptionErr
	}
	f.subscriptionRequestUserIDs = append(f.subscriptionRequestUserIDs, relayUserID)
	return append([]relay.UserSubscription(nil), f.subscriptionsByUser[relayUserID]...), nil
}

func (f *fakeRelayProvider) GroupRateMultipliersForGroups(_ context.Context, groupIDs []int64) []relay.GroupRateMultiplierReadResult {
	f.batchRateCalls++
	f.batchRateGroupIDs = append(f.batchRateGroupIDs, append([]int64(nil), groupIDs...))
	if f.batchRateResults != nil {
		return append([]relay.GroupRateMultiplierReadResult(nil), f.batchRateResults...)
	}
	results := make([]relay.GroupRateMultiplierReadResult, 0, len(groupIDs))
	for _, groupID := range groupIDs {
		results = append(results, relay.GroupRateMultiplierReadResult{
			GroupID: groupID,
			Entries: append([]relay.UserGroupRateEntry(nil), f.rateEntriesByGroup[groupID]...),
		})
	}
	return results
}

func (f *fakeRelayProvider) ListGroupRateMultipliers(_ context.Context, groupID int64) ([]relay.UserGroupRateEntry, error) {
	f.listRateGroupIDs = append(f.listRateGroupIDs, groupID)
	if f.listRatesErr != nil {
		return nil, f.listRatesErr
	}
	return append([]relay.UserGroupRateEntry(nil), f.rateEntriesByGroup[groupID]...), nil
}

func (f *fakeRelayProvider) ReplaceGroupRateMultipliers(ctx context.Context, groupID int64, inputs []relay.GroupRateMultiplierInput) error {
	f.replaceCalls++
	f.replaceUserIDs = f.replaceUserIDs[:0]
	f.replaceInputs = append([]relay.GroupRateMultiplierInput(nil), inputs...)
	for _, input := range inputs {
		f.replaceUserIDs = append(f.replaceUserIDs, input.UserID)
	}
	if f.replaceFn != nil {
		return f.replaceFn(ctx, groupID, inputs)
	}
	return f.replaceErr
}

type fakeRelayProviderWithoutMultiplierBatch struct {
	relay.Provider
	dashboardResponse   *relay.UserUsageDashboardResponse
	subscriptionsByUser map[int64][]relay.UserSubscription
}

func (f *fakeRelayProviderWithoutMultiplierBatch) GetUsageDashboardForUser(context.Context, int64, relay.UserUsageDashboardParams) (*relay.UserUsageDashboardResponse, error) {
	if f.dashboardResponse != nil {
		return f.dashboardResponse, nil
	}
	return &relay.UserUsageDashboardResponse{
		Configured:  true,
		GroupQuotas: relay.UserUsageGroupQuotaState{Status: "ok", Groups: []relay.UserUsageGroupQuotaGroupItem{}},
	}, nil
}

func (f *fakeRelayProviderWithoutMultiplierBatch) ListUserSubscriptions(_ context.Context, relayUserID int64) ([]relay.UserSubscription, error) {
	return append([]relay.UserSubscription(nil), f.subscriptionsByUser[relayUserID]...), nil
}

func createPrimaryRelayProvider(t *testing.T, client *ent.Client) *ent.RelayProvider {
	t.Helper()
	return client.RelayProvider.Create().
		SetName("sub2api-primary").
		SetDisplayName("Primary Relay").
		SetBaseURL("https://relay.example.com").
		SetRelayType("sub2api").
		SetAdminAPIKey("test-admin-api-key").
		SetDefaultModel("claude-sonnet-4-20250514").
		SetIsPrimary(true).
		SetEnabled(true).
		SaveX(context.Background())
}

func createTeamUsageUser(t *testing.T, client *ent.Client, username, email string, relayUserID *int) *ent.User {
	t.Helper()
	create := client.User.Create().
		SetUsername(username).
		SetEmail(email).
		SetAuthSource(entuser.AuthSourceLdap).
		SetRole(entuser.RoleUser)
	if relayUserID != nil {
		create.SetRelayUserID(*relayUserID)
	}
	return create.SaveX(context.Background())
}

func findOverviewMemberByEmail(members []OverviewMember, email string) *OverviewMember {
	for i := range members {
		if strings.EqualFold(members[i].Email, email) {
			return &members[i]
		}
	}
	return nil
}

func findSubscriptionRowByGroupID(rows []SubscriptionRow, groupID string) *SubscriptionRow {
	for i := range rows {
		if rows[i].GroupID == groupID {
			return &rows[i]
		}
	}
	return nil
}

func findRateMultiplierInputByUserID(inputs []relay.GroupRateMultiplierInput, userID int64) *relay.GroupRateMultiplierInput {
	for i := range inputs {
		if inputs[i].UserID == userID {
			return &inputs[i]
		}
	}
	return nil
}

func syntheticRepresentativeScope(actorUserID, targetUserID int, relayUserID int, email string) *representativescope.Scope {
	return &representativescope.Scope{
		ActorUserID:      actorUserID,
		IsRepresentative: true,
		Subjects: []representativescope.Subject{
			{
				SubjectType: "member",
				UserID:      targetUserID,
				DisplayName: "Synthetic Member",
				Email:       email,
				RelayUserID: intPtr(relayUserID),
				Selectable:  true,
			},
		},
	}
}

func syntheticActiveSubscription(groupID int64, groupName string, groupDefaultMultiplier float64) relay.UserSubscription {
	return relay.UserSubscription{
		GroupID: groupID,
		Status:  "active",
		Group: &relay.Group{
			ID:             groupID,
			Name:           groupName,
			Platform:       "openai",
			RateMultiplier: floatPtr(groupDefaultMultiplier),
		},
	}
}

func assertUnavailableMultiplierMetadata(t *testing.T, row *SubscriptionRow, editableReason string) {
	t.Helper()
	if row.MultiplierMetadataStatus != MultiplierMetadataStatusUnavailable || row.MultiplierMetadataMessage == nil || strings.TrimSpace(*row.MultiplierMetadataMessage) == "" {
		t.Fatalf("row metadata = %q/%#v, want unavailable with message", row.MultiplierMetadataStatus, row.MultiplierMetadataMessage)
	}
	if row.UserMultiplier != nil || row.EffectiveMultiplier != nil || row.MultiplierSource != "unknown" {
		t.Fatalf("row multiplier values = user %#v effective %#v source %q, want nil/nil/unknown", row.UserMultiplier, row.EffectiveMultiplier, row.MultiplierSource)
	}
	if row.Editable || row.EditableReason == nil || *row.EditableReason != editableReason {
		t.Fatalf("row edit policy = %v/%#v, want %q", row.Editable, row.EditableReason, editableReason)
	}
	payload, err := json.Marshal(row)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	var wire map[string]any
	if err := json.Unmarshal(payload, &wire); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if effective, exists := wire["effective_multiplier"]; !exists || effective != nil {
		t.Fatalf("wire effective_multiplier = %#v, exists %v, want explicit null: %s", effective, exists, payload)
	}
}

func int64Ptr(v int64) *int64 {
	return &v
}
