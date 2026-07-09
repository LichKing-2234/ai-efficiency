package workitems

import (
	"context"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/directoryoffboardingaction"
	"github.com/ai-efficiency/backend/ent/directorysource"
	"github.com/ai-efficiency/backend/ent/directorysyncrun"
	"github.com/ai-efficiency/backend/ent/quotaresetrequest"
	entuser "github.com/ai-efficiency/backend/ent/user"
	"github.com/ai-efficiency/backend/internal/testdb"
)

func TestCountsForRegularApproverIncludesAssignedPendingAndFailedResetApprovals(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	approver := createWorkItemsUser(t, ctx, client, "lead", "lead@example.com", nil, "user")
	requester := createWorkItemsUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
	createWorkItemsQuotaRequest(t, ctx, client, requester.ID, 1001, 1, "42", quotaresetrequest.StatusPending, []int{approver.ID})
	createWorkItemsQuotaRequest(t, ctx, client, approver.ID, 1002, 1, "43", quotaresetrequest.StatusPending, []int{approver.ID})
	createWorkItemsQuotaRequest(t, ctx, client, requester.ID, 1001, 1, "44", quotaresetrequest.StatusApprovedResetFailed, []int{approver.ID})

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
