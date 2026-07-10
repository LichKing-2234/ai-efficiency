package quotareset

import (
	"context"
	"testing"

	"github.com/ai-efficiency/backend/ent/quotaresetrequestnode"
	"github.com/ai-efficiency/backend/internal/testdb"
)

func TestWorkflowSchemasRoundTrip(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
	approver := createQuotaResetUser(t, ctx, client, "bob", "bob@example.org", nil, "user")
	provider := createQuotaResetRelayProvider(t, ctx, client)
	chain := client.QuotaResetApprovalChain.Create().
		SetProviderID(provider.ID).SetGroupID("42").SetGroupName("Group Alpha").SetEnabled(true).
		SetCreatedByUserID(requester.ID).SetUpdatedByUserID(requester.ID).SaveX(ctx)
	client.QuotaResetApprovalChainNode.Create().
		SetChainID(chain.ID).SetPosition(0).SetDirectorySourceID(1).
		SetDepartmentExternalID("department-alpha").SetDepartmentDisplayPath("Department Alpha").SaveX(ctx)
	request := client.QuotaResetRequest.Create().
		SetRequesterUserID(requester.ID).SetRequesterRelayUserID(1001).
		SetProviderID(provider.ID).SetGroupID("42").SetGroupName("Group Alpha").SetGroupPlatform("openai").
		SetReason("Need a reset for a build investigation").SetWorkflowVersion(2).
		SetRequesterDisplayNameSnapshot("Alice").SetRequesterEmailSnapshot("alice@example.com").
		SetRequesterDepartmentPaths([]string{"Department Alpha"}).
		SetRequesterNotificationIds(map[string]string{"wecom": "alice-wecom"}).SaveX(ctx)
	node := client.QuotaResetRequestNode.Create().
		SetRequestID(request.ID).SetPosition(0).SetNodeType("requester_departments").
		SetLabel("Requester departments").
		SetDepartmentSnapshots([]map[string]any{{"external_id": "department-alpha", "display_path": "Department Alpha"}}).
		SetStatus(quotaresetrequestnode.StatusActive).SaveX(ctx)
	client.QuotaResetRequestNodeApprover.Create().
		SetRequestNodeID(node.ID).SetUserID(approver.ID).SetDisplayName("Bob").SetEmail("bob@example.org").
		SetSource("configured").SetSourceDepartmentExternalIds([]string{"department-alpha"}).
		SetNotificationIds(map[string]string{"wecom": "bob-wecom"}).SaveX(ctx)
	decision := client.QuotaResetRequestDecision.Create().
		SetRequestID(request.ID).SetRequestNodeID(node.ID).SetActorUserID(approver.ID).
		SetActorDisplayName("Bob").SetDecision("approve").
		SetComment("Approved for the current investigation").SetAdminOverride(false).SaveX(ctx)
	client.QuotaResetRequestNode.UpdateOneID(node.ID).
		SetStatus(quotaresetrequestnode.StatusApproved).SetSatisfiedByDecisionID(decision.ID).SaveX(ctx)
	client.QuotaResetRequest.UpdateOneID(request.ID).
		SetCurrentNodeID(node.ID).SetWorkflowCompletedByDecisionID(decision.ID).SaveX(ctx)
	if got := client.QuotaResetRequestNodeApprover.Query().CountX(ctx); got != 1 {
		t.Fatalf("node approver count = %d, want 1", got)
	}
	if got := client.QuotaResetRequestDecision.Query().OnlyX(ctx).Comment; got != "Approved for the current investigation" {
		t.Fatalf("decision comment = %q", got)
	}
}
