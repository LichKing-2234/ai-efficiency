package workitems

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/directoryoffboardingaction"
	"github.com/ai-efficiency/backend/ent/directorysource"
	"github.com/ai-efficiency/backend/ent/directorysyncrun"
	"github.com/ai-efficiency/backend/ent/quotaresetrequest"
	"github.com/ai-efficiency/backend/ent/quotaresetrequestdecision"
	"github.com/ai-efficiency/backend/ent/quotaresetrequestnode"
	"github.com/ai-efficiency/backend/ent/quotaresetrequestnodeapprover"
	entuser "github.com/ai-efficiency/backend/ent/user"
	"github.com/ai-efficiency/backend/internal/quotareset"
	"github.com/ai-efficiency/backend/internal/testdb"
	"github.com/ai-efficiency/backend/internal/usersetup"
)

type fakeProviderLister struct {
	resp *usersetup.ListProvidersResponse
	err  error
}

func (f fakeProviderLister) ListProviders(context.Context, usersetup.ListProvidersRequest) (*usersetup.ListProvidersResponse, error) {
	return f.resp, f.err
}

func TestCountsForRegularApproverIncludesAssignedPendingAndFailedResetApprovals(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	approver := createWorkItemsUser(t, ctx, client, "lead", "lead@example.com", nil, "user")
	requester := createWorkItemsUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
	createWorkItemsV2QuotaRequest(t, ctx, client, requester.ID, "42", quotaresetrequest.StatusPending, []int{approver.ID}, nil)
	createWorkItemsV2QuotaRequest(t, ctx, client, approver.ID, "43", quotaresetrequest.StatusPending, []int{approver.ID}, nil)
	createWorkItemsV2QuotaRequest(t, ctx, client, requester.ID, "44", quotaresetrequest.StatusApprovedResetFailed, nil, &approver.ID)

	counts, err := NewService(client).Counts(ctx, approver.ID, false)
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

	counts, err := NewService(client, lister).Counts(ctx, user.ID, false)
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

	counts, err := NewService(client, fakeProviderLister{err: errors.New("relay unavailable")}).Counts(ctx, approver.ID, false)
	if err != nil {
		t.Fatalf("Counts() error = %v, want local counts to remain available", err)
	}
	if counts.QuotaResetApprovalCount != 1 || counts.AIAccessSetupCount != 0 || counts.TotalCount != 1 {
		t.Fatalf("counts = %+v, want local approval count with unknown AI access omitted", counts)
	}
}

func TestCountsForAdminIncludesPendingAndFailedResetQuotaWithOffboardingCandidates(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	admin := createWorkItemsUser(t, ctx, client, "admin", "admin@example.com", nil, "admin")
	requester := createWorkItemsUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
	createWorkItemsQuotaRequest(t, ctx, client, requester.ID, 1001, 1, "42", quotaresetrequest.StatusPending, []int{admin.ID})
	createWorkItemsQuotaRequest(t, ctx, client, requester.ID, 1001, 1, "43", quotaresetrequest.StatusPending, nil)
	createWorkItemsQuotaRequest(t, ctx, client, requester.ID, 1001, 1, "44", quotaresetrequest.StatusApprovedResetSucceeded, []int{admin.ID})
	createWorkItemsQuotaRequest(t, ctx, client, requester.ID, 1001, 1, "45", quotaresetrequest.StatusApprovedResetFailed, []int{admin.ID})

	source, run := createWorkItemsDirectorySnapshot(t, ctx, client, "alice@example.com")
	missing := createWorkItemsUser(t, ctx, client, "bob", "bob@example.org", intPtr(2002), "user")
	disabled := createWorkItemsUser(t, ctx, client, "carol", "carol@example.net", intPtr(2003), "user")
	client.DirectoryOffboardingAction.Create().
		SetSourceID(source.ID).
		SetUserID(disabled.ID).
		SetRelayUserID(2003).
		SetDirectoryRunID(run.ID).
		SetAction(directoryoffboardingaction.ActionDisableRelayUser).
		SetStatus(directoryoffboardingaction.StatusSucceeded).
		SetReason("missing_from_latest_full_company_directory").
		SetPerformedByUserID(admin.ID).
		SaveX(ctx)
	_ = missing

	counts, err := NewService(client).Counts(ctx, admin.ID, true)
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
	create := client.QuotaResetRequest.Create().
		SetRequesterUserID(requesterUserID).
		SetRequesterRelayUserID(requesterRelayUserID).
		SetProviderID(providerID).
		SetGroupID(groupID).
		SetGroupName("Group Alpha").
		SetGroupPlatform("openai").
		SetReason("Need reset for a build investigation").
		SetStatus(status).
		SetMatchedDepartmentPaths([]map[string]any{})
	if approverIDs != nil {
		create.SetResolvedApproverUserIds(approverIDs)
	}
	request, err := create.Save(ctx)
	if err != nil {
		t.Fatalf("create quota request %s: %v", groupID, err)
	}
	return request
}

func createWorkItemsV2QuotaRequest(t *testing.T, ctx context.Context, client *ent.Client, requesterUserID int, groupID string, status quotaresetrequest.Status, approverIDs []int, completionActorID *int) *ent.QuotaResetRequest {
	t.Helper()
	request := client.QuotaResetRequest.Create().
		SetRequesterUserID(requesterUserID).
		SetRequesterRelayUserID(int64(1000 + requesterUserID)).
		SetProviderID(1).
		SetGroupID(groupID).
		SetGroupName("Group Alpha").
		SetGroupPlatform("openai").
		SetReason("Need reset for a build investigation").
		SetWorkflowVersion(quotareset.WorkflowVersionV2).
		SetRequesterDisplayNameSnapshot("Alice Snapshot").
		SetRequesterEmailSnapshot("alice@example.com").
		SetRequesterDepartmentPaths([]string{"Department Alpha"}).
		SetRequesterNotificationIds(map[string]string{}).
		SetStatus(status).
		SetResolvedApproverUserIds([]int{}).
		SetMatchedDepartmentPaths([]map[string]any{}).
		SaveX(ctx)
	nodeStatus := quotaresetrequestnode.StatusApproved
	if status == quotaresetrequest.StatusPending {
		nodeStatus = quotaresetrequestnode.StatusActive
	}
	node := client.QuotaResetRequestNode.Create().
		SetRequestID(request.ID).
		SetPosition(0).
		SetNodeType(quotaresetrequestnode.NodeTypeConfiguredDepartment).
		SetLabel("Department review").
		SetDepartmentSnapshots([]map[string]any{}).
		SetStatus(nodeStatus).
		SaveX(ctx)
	for _, approverID := range approverIDs {
		user := client.User.GetX(ctx, approverID)
		client.QuotaResetRequestNodeApprover.Create().
			SetRequestNodeID(node.ID).
			SetUserID(user.ID).
			SetDisplayName(user.Username).
			SetEmail(user.Email).
			SetSource(quotaresetrequestnodeapprover.SourceConfigured).
			SetSourceDepartmentExternalIds([]string{"dept-alpha"}).
			SetNotificationIds(map[string]string{}).
			SaveX(ctx)
	}
	if status == quotaresetrequest.StatusPending {
		request = client.QuotaResetRequest.UpdateOneID(request.ID).SetCurrentNodeID(node.ID).SaveX(ctx)
	}
	if completionActorID != nil {
		actor := client.User.GetX(ctx, *completionActorID)
		decision := client.QuotaResetRequestDecision.Create().
			SetRequestID(request.ID).
			SetRequestNodeID(node.ID).
			SetActorUserID(actor.ID).
			SetActorDisplayName(actor.Username).
			SetDecision(quotaresetrequestdecision.DecisionApprove).
			SetComment("Approved before reset failure").
			SaveX(ctx)
		request = client.QuotaResetRequest.UpdateOneID(request.ID).
			SetWorkflowCompletedByDecisionID(decision.ID).
			SaveX(ctx)
	}
	return request
}

func createWorkItemsDirectorySnapshot(t *testing.T, ctx context.Context, client *ent.Client, memberEmail string) (*ent.DirectorySource, *ent.DirectorySyncRun) {
	t.Helper()
	completedAt := time.Date(2026, 7, 7, 10, 0, 0, 0, time.UTC)
	source := client.DirectorySource.Create().
		SetName("Directory Alpha").
		SetDescription("Synthetic directory").
		SetScope(directorysource.ScopeFullCompany).
		SetEnabled(true).
		SetDsl("steps: []").
		SaveX(ctx)
	run := client.DirectorySyncRun.Create().
		SetSourceID(source.ID).
		SetMode(directorysyncrun.ModeApply).
		SetTrigger(directorysyncrun.TriggerManual).
		SetStatus(directorysyncrun.StatusCompleted).
		SetPhase(directorysyncrun.PhaseCompleted).
		SetCompletedAt(completedAt).
		SaveX(ctx)
	client.DirectorySource.UpdateOneID(source.ID).SetLastSuccessfulRunID(run.ID).SaveX(ctx)
	client.DirectoryMember.Create().
		SetSourceID(source.ID).
		SetExternalID("member-alpha").
		SetEmailNormalized(memberEmail).
		SetDisplayName("Alice").
		SetDepartmentExternalID("dept-alpha").
		SetLastSeenRunID(run.ID).
		SaveX(ctx)
	return source, run
}

func intPtr(value int) *int {
	return &value
}
