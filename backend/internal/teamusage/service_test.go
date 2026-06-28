package teamusage

import (
	"context"
	"errors"
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
	dashboardResponse   *relay.UserUsageDashboardResponse
	dashboardErr        error
	subscriptionsByUser map[int64][]relay.UserSubscription
	subscriptionErr     error
	rateEntriesByGroup  map[int64][]relay.UserGroupRateEntry
	listRatesErr        error
	replaceFn           func(context.Context, int64, []relay.GroupRateMultiplierInput) error
	replaceErr          error
	replaceCalls        int
	summaryStats        map[int64]relay.TeamUserUsageStats
	summaryErr          error
	trendPoints         map[int64][]relay.UsageTrendPoint
	trendErr            error
}

func (f *fakeRelayProvider) Ping(context.Context) error { return nil }
func (f *fakeRelayProvider) Name() string               { return "fake-relay" }

func (f *fakeRelayProvider) Authenticate(context.Context, string, string) (*relay.User, error) {
	return nil, nil
}

func (f *fakeRelayProvider) GetUser(context.Context, int64) (*relay.User, error) {
	return nil, nil
}

func (f *fakeRelayProvider) ListAllowedGroupsForUser(context.Context, int64) ([]relay.Group, error) {
	return nil, nil
}

func (f *fakeRelayProvider) FindUserByEmail(context.Context, string) (*relay.User, error) {
	return nil, nil
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

func (f *fakeRelayProvider) GetUsageDashboardForUser(context.Context, int64, relay.UserUsageDashboardParams) (*relay.UserUsageDashboardResponse, error) {
	if f.dashboardErr != nil {
		return nil, f.dashboardErr
	}
	if f.dashboardResponse == nil {
		return &relay.UserUsageDashboardResponse{
			Configured:  true,
			GroupQuotas: relay.UserUsageGroupQuotaState{Status: "ok", Groups: []relay.UserUsageGroupQuotaGroupItem{}},
		}, nil
	}
	return f.dashboardResponse, nil
}

func (f *fakeRelayProvider) GetBatchUserUsageStats(context.Context, []int64, relay.TeamUsageSummaryParams) (map[int64]relay.TeamUserUsageStats, error) {
	if f.summaryErr != nil {
		return nil, f.summaryErr
	}
	return f.summaryStats, nil
}

func (f *fakeRelayProvider) GetUsageTrendForUsers(context.Context, []int64, relay.TeamMemberTrendParams) (map[int64][]relay.UsageTrendPoint, error) {
	if f.trendErr != nil {
		return nil, f.trendErr
	}
	return f.trendPoints, nil
}

func (f *fakeRelayProvider) ListUserSubscriptions(_ context.Context, relayUserID int64) ([]relay.UserSubscription, error) {
	if f.subscriptionErr != nil {
		return nil, f.subscriptionErr
	}
	return append([]relay.UserSubscription(nil), f.subscriptionsByUser[relayUserID]...), nil
}

func (f *fakeRelayProvider) ListGroupRateMultipliers(_ context.Context, groupID int64) ([]relay.UserGroupRateEntry, error) {
	if f.listRatesErr != nil {
		return nil, f.listRatesErr
	}
	return append([]relay.UserGroupRateEntry(nil), f.rateEntriesByGroup[groupID]...), nil
}

func (f *fakeRelayProvider) ReplaceGroupRateMultipliers(ctx context.Context, groupID int64, inputs []relay.GroupRateMultiplierInput) error {
	f.replaceCalls++
	if f.replaceFn != nil {
		return f.replaceFn(ctx, groupID, inputs)
	}
	return f.replaceErr
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
