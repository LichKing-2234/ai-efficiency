package workitems

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/quotaresetrequest"
	entuser "github.com/ai-efficiency/backend/ent/user"
	"github.com/ai-efficiency/backend/internal/directorysync"
	"github.com/ai-efficiency/backend/internal/testdb"
	"github.com/ai-efficiency/backend/internal/usersetup"
)

type fakeProviderLister struct {
	resp *usersetup.ListProvidersResponse
	err  error
}

type fakeOffboardingCounter struct {
	count     int
	err       error
	countCall int
	listCall  int
}

func (f *fakeOffboardingCounter) CountOffboardingCandidates(context.Context, int) (int, error) {
	f.countCall++
	return f.count, f.err
}

func (f *fakeOffboardingCounter) ListOffboardingCandidates(context.Context, directorysync.OffboardingCandidateListParams) (*directorysync.OffboardingCandidatePage, error) {
	f.listCall++
	return nil, errors.New("candidate list must not be called by work item counts")
}

func (f fakeProviderLister) ListProviders(context.Context, usersetup.ListProvidersRequest) (*usersetup.ListProvidersResponse, error) {
	return f.resp, f.err
}

func TestCountsForRegularApproverIncludesAssignedPendingAndFailedResetApprovals(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	approver := createWorkItemsUser(t, ctx, client, "lead", "lead@example.com", nil, "user")
	requester := createWorkItemsUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
	createWorkItemsQuotaRequest(t, ctx, client, requester.ID, 1001, 1, "42", quotaresetrequest.StatusPending, []int{approver.ID})
	createWorkItemsQuotaRequest(t, ctx, client, approver.ID, 1002, 1, "43", quotaresetrequest.StatusPending, []int{approver.ID})
	createWorkItemsQuotaRequest(t, ctx, client, requester.ID, 1001, 1, "44", quotaresetrequest.StatusApprovedResetFailed, []int{approver.ID})

	counts, err := NewService(client, nil).Counts(ctx, approver.ID, false)
	if err != nil {
		t.Fatalf("Counts() error = %v", err)
	}

	if counts.QuotaResetApprovalCount != 2 {
		t.Fatalf("quota_reset_approval_count = %d, want 2", counts.QuotaResetApprovalCount)
	}
	if counts.QuotaResetAdminCount != 0 || counts.OffboardingCount != 0 || counts.TotalCount != 2 {
		t.Fatalf("counts = %+v, want two actionable regular approvals", counts)
	}
}

func TestCountsForRegularUserIncludesMissingAIAccessSetup(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	user := createWorkItemsUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
	lister := fakeProviderLister{resp: &usersetup.ListProvidersResponse{
		Providers: []usersetup.ProviderSummary{
			{
				ID:          1,
				Name:        "sub2api",
				DisplayName: "sub2api",
				Groups: []usersetup.GroupCredentialSummary{
					{
						GroupID:   "42",
						GroupName: "Group Alpha",
						Platform:  "openai",
						Credential: usersetup.GroupCredentialState{
							State: "missing",
						},
					},
				},
			},
		},
	}}

	counts, err := NewService(client, nil, lister).Counts(ctx, user.ID, false)
	if err != nil {
		t.Fatalf("Counts() error = %v", err)
	}

	if counts.AIAccessSetupCount != 1 {
		t.Fatalf("ai_access_setup_count = %d, want 1", counts.AIAccessSetupCount)
	}
	if counts.TotalCount != 1 {
		t.Fatalf("total_count = %d, want missing AI access setup count 1", counts.TotalCount)
	}
}

func TestCountsKeepLocalWorkItemsWhenAIAccessLookupFails(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	approver := createWorkItemsUser(t, ctx, client, "lead", "lead@example.com", nil, "user")
	requester := createWorkItemsUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
	createWorkItemsQuotaRequest(t, ctx, client, requester.ID, 1001, 1, "42", quotaresetrequest.StatusPending, []int{approver.ID})

	counts, err := NewService(client, nil, fakeProviderLister{err: errors.New("relay unavailable")}).Counts(ctx, approver.ID, false)
	if err != nil {
		t.Fatalf("Counts() error = %v, want local counts to remain available", err)
	}
	if counts.QuotaResetApprovalCount != 1 || counts.AIAccessSetupCount != 0 || counts.TotalCount != 1 {
		t.Fatalf("counts = %+v, want local approval count with unknown AI access omitted", counts)
	}
}

func TestCountsForAdminUsesInjectedOffboardingCounterWithoutListingCandidates(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	admin := createWorkItemsUser(t, ctx, client, "admin", "admin@example.com", nil, "admin")
	requester := createWorkItemsUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
	createWorkItemsQuotaRequest(t, ctx, client, requester.ID, 1001, 1, "42", quotaresetrequest.StatusPending, []int{admin.ID})
	createWorkItemsQuotaRequest(t, ctx, client, requester.ID, 1001, 1, "43", quotaresetrequest.StatusPending, nil)
	createWorkItemsQuotaRequest(t, ctx, client, requester.ID, 1001, 1, "44", quotaresetrequest.StatusApprovedResetSucceeded, []int{admin.ID})
	createWorkItemsQuotaRequest(t, ctx, client, requester.ID, 1001, 1, "45", quotaresetrequest.StatusApprovedResetFailed, []int{admin.ID})

	counter := &fakeOffboardingCounter{count: 1}

	counts, err := NewService(client, counter).Counts(ctx, admin.ID, true)
	if err != nil {
		t.Fatalf("Counts() error = %v", err)
	}

	if counts.QuotaResetApprovalCount != 2 {
		t.Fatalf("quota_reset_approval_count = %d, want assigned approval count 2", counts.QuotaResetApprovalCount)
	}
	if counts.QuotaResetAdminCount != 3 {
		t.Fatalf("quota_reset_admin_count = %d, want all pending and failed quota requests 3", counts.QuotaResetAdminCount)
	}
	if counts.OffboardingCount != 1 {
		t.Fatalf("offboarding_count = %d, want missing user count 1", counts.OffboardingCount)
	}
	if counts.TotalCount != 4 {
		t.Fatalf("total_count = %d, want admin actionable quota plus offboarding 4", counts.TotalCount)
	}
	if counter.countCall != 1 || counter.listCall != 0 {
		t.Fatalf("offboarding dependency calls count=%d list=%d, want count=1 list=0", counter.countCall, counter.listCall)
	}
}

func TestResolvedApproverUserIDsGINIndexSupportsContainsPlan(t *testing.T) {
	_, dsn := testdb.OpenWithDSN(t)
	ctx := context.Background()
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	var indexName, indexDefinition string
	err = db.QueryRowContext(ctx, `
		SELECT indexname, indexdef
		FROM pg_indexes
		WHERE schemaname = current_schema()
		  AND tablename = 'quota_reset_requests'
		  AND indexdef ILIKE '%USING gin%'
		  AND indexdef ILIKE '%resolved_approver_user_ids%'
	`).Scan(&indexName, &indexDefinition)
	if err != nil {
		t.Fatalf("find resolved approver GIN index: %v", err)
	}
	normalizedDefinition := strings.ToLower(indexDefinition)
	if !strings.Contains(normalizedDefinition, "using gin (resolved_approver_user_ids jsonb_path_ops)") {
		t.Fatalf("index definition = %q, want GIN jsonb_path_ops", indexDefinition)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin explain transaction: %v", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "SET LOCAL enable_seqscan = off"); err != nil {
		t.Fatalf("disable sequential scans: %v", err)
	}
	rows, err := tx.QueryContext(ctx, `
		EXPLAIN (COSTS OFF)
		SELECT id
		FROM quota_reset_requests
		WHERE resolved_approver_user_ids::jsonb @> '[123]'::jsonb
	`)
	if err != nil {
		t.Fatalf("explain resolved approver contains query: %v", err)
	}
	defer rows.Close()
	var planLines []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan explain line: %v", err)
		}
		planLines = append(planLines, line)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read explain plan: %v", err)
	}
	plan := strings.Join(planLines, "\n")
	if !strings.Contains(plan, indexName) || (!strings.Contains(plan, "Bitmap Index Scan") && !strings.Contains(plan, "Index Scan")) {
		t.Fatalf("EXPLAIN did not select %s for @> predicate:\n%s", indexName, plan)
	}
}

func createWorkItemsUser(t *testing.T, ctx context.Context, client *ent.Client, username string, email string, relayUserID *int, role string) *ent.User {
	t.Helper()
	create := client.User.Create().
		SetUsername(username).
		SetEmail(email).
		SetAuthSource(entuser.AuthSourceLdap).
		SetRole(entuser.Role(role))
	if relayUserID != nil {
		create.SetRelayUserID(*relayUserID)
	}
	user, err := create.Save(ctx)
	if err != nil {
		t.Fatalf("create user %s: %v", username, err)
	}
	return user
}

func createWorkItemsQuotaRequest(t *testing.T, ctx context.Context, client *ent.Client, requesterUserID int, requesterRelayUserID int64, providerID int, groupID string, status quotaresetrequest.Status, approverIDs []int) *ent.QuotaResetRequest {
	t.Helper()
	request, err := client.QuotaResetRequest.Create().
		SetRequesterUserID(requesterUserID).
		SetRequesterRelayUserID(requesterRelayUserID).
		SetProviderID(providerID).
		SetGroupID(groupID).
		SetGroupName("Group Alpha").
		SetGroupPlatform("openai").
		SetReason("Need reset for a build investigation").
		SetStatus(status).
		SetResolvedApproverUserIds(approverIDs).
		SetMatchedDepartmentPaths([]map[string]any{}).
		Save(ctx)
	if err != nil {
		t.Fatalf("create quota request %s: %v", groupID, err)
	}
	return request
}

func intPtr(value int) *int {
	return &value
}
