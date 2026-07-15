package quotareset

import (
	"context"
	"testing"

	"github.com/ai-efficiency/backend/ent/quotaresetnotificationsetting"
	"github.com/ai-efficiency/backend/internal/testdb"
)

func TestSchemaPersistsCompactWorkflowAndApprovalChain(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
	provider := createQuotaResetRelayProvider(t, ctx, client)

	chain := client.QuotaResetApprovalChain.Create().
		SetProviderID(provider.ID).
		SetGroupID("42").
		SetGroupName("Group Alpha").
		SetDepartmentChain([]map[string]any{{
			"directory_source_id":     1,
			"department_external_id":  "dept-alpha",
			"department_display_path": "Company / Group Alpha",
		}}).
		SetCreatedByUserID(requester.ID).
		SetUpdatedByUserID(requester.ID).
		SaveX(ctx)
	if len(chain.DepartmentChain) != 1 || chain.DepartmentChain[0]["department_external_id"] != "dept-alpha" {
		t.Fatalf("department chain = %#v", chain.DepartmentChain)
	}

	workflow, err := EncodeWorkflow(workflowFixture())
	if err != nil {
		t.Fatalf("EncodeWorkflow() error = %v", err)
	}
	request := client.QuotaResetRequest.Create().
		SetRequesterUserID(requester.ID).
		SetRequesterRelayUserID(1001).
		SetProviderID(provider.ID).
		SetGroupID("42").
		SetGroupName("Group Alpha").
		SetGroupPlatform("openai").
		SetReason("Need a reset for a build investigation").
		SetWorkflowVersion(workflowVersionV2).
		SetWorkflow(workflow).
		SetWorkflowRevision(0).
		SetResolvedApproverUserIds([]int{2}).
		SetMatchedDepartmentPaths([]map[string]any{}).
		SaveX(ctx)
	if request.WorkflowVersion != workflowVersionV2 || request.WorkflowRevision != 0 || request.Workflow == nil {
		t.Fatalf("request workflow fields = version %d revision %d raw %#v", request.WorkflowVersion, request.WorkflowRevision, request.Workflow)
	}
}

func TestSchemaDefaultsHistoricalRequestAndNotificationChannel(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
	provider := createQuotaResetRelayProvider(t, ctx, client)

	request := client.QuotaResetRequest.Create().
		SetRequesterUserID(requester.ID).
		SetRequesterRelayUserID(1001).
		SetProviderID(provider.ID).
		SetGroupID("42").
		SetReason("Historical request").
		SaveX(ctx)
	if request.WorkflowVersion != 1 || request.WorkflowRevision != 0 {
		t.Fatalf("request defaults = version %d revision %d", request.WorkflowVersion, request.WorkflowRevision)
	}

	setting := client.QuotaResetNotificationSetting.Create().SaveX(ctx)
	if setting.Channel != quotaresetnotificationsetting.ChannelGenericWebhook {
		t.Fatalf("notification channel = %q, want generic_webhook", setting.Channel)
	}
}
