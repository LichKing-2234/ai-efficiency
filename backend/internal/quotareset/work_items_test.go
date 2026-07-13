package quotareset

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/quotaresetrequest"
	"github.com/ai-efficiency/backend/ent/quotaresetrequestdecision"
	"github.com/ai-efficiency/backend/ent/quotaresetrequestnode"
	"github.com/ai-efficiency/backend/internal/testdb"
)

func TestListApprovalsReturnsOnlyActiveV2Assignments(t *testing.T) {
	fixture := newWorkflowDecisionFixture(t, []workflowNodeFixture{{}, {}})
	fixture.replaceApproverIDs(t, 0, fixture.actorA.ID)
	fixture.replaceApproverIDs(t, 1, fixture.actorB.ID)

	future, err := fixture.service.ListApprovals(fixture.ctx, fixture.actorB.ID, ListParams{})
	if err != nil {
		t.Fatalf("ListApprovals() future actor error = %v", err)
	}
	if future.Total != 0 || len(future.Items) != 0 {
		t.Fatalf("future actor response = %+v, want no queued assignment", future)
	}

	active, err := fixture.service.ListApprovals(fixture.ctx, fixture.actorA.ID, ListParams{})
	if err != nil {
		t.Fatalf("ListApprovals() active actor error = %v", err)
	}
	if active.Total != 1 || len(active.Items) != 1 || active.Items[0].Workflow == nil {
		t.Fatalf("active actor response = %+v, want one visible active assignment", active)
	}

	fixture.client.QuotaResetRequestNode.UpdateOneID(fixture.nodes[0].ID).
		SetStatus(quotaresetrequestnode.StatusApproved).
		SaveX(fixture.ctx)
	fixture.client.QuotaResetRequestNode.UpdateOneID(fixture.nodes[1].ID).
		SetStatus(quotaresetrequestnode.StatusActive).
		SaveX(fixture.ctx)
	fixture.client.QuotaResetRequest.UpdateOneID(fixture.request.ID).
		SetCurrentNodeID(fixture.nodes[1].ID).
		SaveX(fixture.ctx)

	activated, err := fixture.service.ListApprovals(fixture.ctx, fixture.actorB.ID, ListParams{})
	if err != nil {
		t.Fatalf("ListApprovals() activated actor error = %v", err)
	}
	if activated.Total != 1 || len(activated.Items) != 1 || activated.Items[0].Workflow == nil {
		t.Fatalf("activated actor response = %+v, want one current assignment", activated)
	}

	fixture.client.QuotaResetRequest.UpdateOneID(fixture.request.ID).
		SetStatus(quotaresetrequest.StatusApprovedResetSucceeded).
		ClearCurrentNodeID().
		SaveX(fixture.ctx)
	failed, _, _ := createV2WorkItemRequest(t, fixture.ctx, fixture.client, fixture.requester.ID, "retry-list", quotaresetrequest.StatusApprovedResetFailed, nil, nil, &fixture.actorB.ID)
	retry, err := fixture.service.ListApprovals(fixture.ctx, fixture.actorB.ID, ListParams{})
	if err != nil {
		t.Fatalf("ListApprovals() retry actor error = %v", err)
	}
	if retry.Total != 1 || len(retry.Items) != 1 || retry.Items[0].ID != failed.ID || retry.Items[0].Workflow == nil || !retry.Items[0].Workflow.CanRetry {
		t.Fatalf("retry actor response = %+v, want failed completion assignment", retry)
	}
}

func TestListApprovalsKeepsV2DecisionHistoryAfterWorkflowAdvances(t *testing.T) {
	fixture := newWorkflowDecisionFixture(t, []workflowNodeFixture{{}, {}})
	fixture.replaceApproverIDs(t, 0, fixture.actorA.ID)
	fixture.replaceApproverIDs(t, 1, fixture.actorB.ID)
	unrelated := createQuotaResetUser(t, fixture.ctx, fixture.client, "unrelated-history", "unrelated-history@example.net", nil, "user")

	future, err := fixture.service.ListApprovals(fixture.ctx, fixture.actorB.ID, ListParams{})
	if err != nil {
		t.Fatalf("ListApprovals() future actor error = %v", err)
	}
	if future.Total != 0 || len(future.Items) != 0 {
		t.Fatalf("future actor response = %+v, want no queued assignment", future)
	}

	updated, err := fixture.service.Approve(fixture.ctx, DecisionInput{
		ActorUserID:    fixture.actorA.ID,
		RequestID:      fixture.request.ID,
		RequestNodeID:  fixture.nodes[0].ID,
		DecisionReason: "Approved the initial review",
	})
	if err != nil {
		t.Fatalf("Approve() error = %v", err)
	}
	if updated.CurrentNodeID == nil || *updated.CurrentNodeID != fixture.nodes[1].ID {
		t.Fatalf("current node = %v, want advanced node %d", updated.CurrentNodeID, fixture.nodes[1].ID)
	}

	history, err := fixture.service.ListApprovals(fixture.ctx, fixture.actorA.ID, ListParams{})
	if err != nil {
		t.Fatalf("ListApprovals() decision actor error = %v", err)
	}
	if history.Total != 1 || len(history.Items) != 1 || history.Items[0].ID != fixture.request.ID {
		t.Fatalf("decision actor response = %+v, want advanced request history", history)
	}
	workflow := history.Items[0].Workflow
	if workflow == nil || workflow.CurrentNode == nil || workflow.CurrentNode.ID != fixture.nodes[1].ID || len(workflow.Decisions) != 1 {
		t.Fatalf("workflow history = %+v, want advanced node and stored decision", workflow)
	}
	if workflow.Decisions[0].ActorUserID != fixture.actorA.ID || workflow.Decisions[0].Comment != "Approved the initial review" {
		t.Fatalf("decision history = %+v", workflow.Decisions)
	}
	if workflow.CanApprove || workflow.CanReject || workflow.CanCancel || workflow.CanRetry {
		t.Fatalf("prior actor permissions = %+v, want history-only access", workflow)
	}

	unrelatedHistory, err := fixture.service.ListApprovals(fixture.ctx, unrelated.ID, ListParams{})
	if err != nil {
		t.Fatalf("ListApprovals() unrelated actor error = %v", err)
	}
	if unrelatedHistory.Total != 0 || len(unrelatedHistory.Items) != 0 {
		t.Fatalf("unrelated actor response = %+v, want no history leak", unrelatedHistory)
	}
}

func TestListApprovalsReturnsRejectedV2DecisionHistory(t *testing.T) {
	fixture := newWorkflowDecisionFixture(t, []workflowNodeFixture{{}})
	fixture.replaceApproverIDs(t, 0, fixture.actorA.ID)
	unrelated := createQuotaResetUser(t, fixture.ctx, fixture.client, "unrelated-rejection", "unrelated-rejection@example.org", nil, "user")

	updated, err := fixture.service.Reject(fixture.ctx, DecisionInput{
		ActorUserID:    fixture.actorA.ID,
		RequestID:      fixture.request.ID,
		RequestNodeID:  fixture.nodes[0].ID,
		DecisionReason: "Rejected after reviewing the request",
	})
	if err != nil {
		t.Fatalf("Reject() error = %v", err)
	}
	if updated.Status != quotaresetrequest.StatusRejected {
		t.Fatalf("status = %s, want rejected", updated.Status)
	}

	history, err := fixture.service.ListApprovals(fixture.ctx, fixture.actorA.ID, ListParams{})
	if err != nil {
		t.Fatalf("ListApprovals() rejection actor error = %v", err)
	}
	if history.Total != 1 || len(history.Items) != 1 || history.Items[0].Status != quotaresetrequest.StatusRejected.String() {
		t.Fatalf("rejection actor response = %+v, want rejected request history", history)
	}
	workflow := history.Items[0].Workflow
	if workflow == nil || workflow.CurrentNode != nil || len(workflow.Decisions) != 1 || workflow.Decisions[0].Decision != quotaresetrequestdecision.DecisionReject.String() {
		t.Fatalf("rejected workflow history = %+v", workflow)
	}
	if workflow.CanApprove || workflow.CanReject || workflow.CanCancel || workflow.CanRetry {
		t.Fatalf("rejection actor permissions = %+v, want history-only access", workflow)
	}

	unrelatedHistory, err := fixture.service.ListApprovals(fixture.ctx, unrelated.ID, ListParams{})
	if err != nil {
		t.Fatalf("ListApprovals() unrelated actor error = %v", err)
	}
	if unrelatedHistory.Total != 0 || len(unrelatedHistory.Items) != 0 {
		t.Fatalf("unrelated actor response = %+v, want no rejected history leak", unrelatedHistory)
	}
}

func TestWorkflowSummaryReturnsOrderedNodesDecisionsAndPermissions(t *testing.T) {
	fixture := newWorkflowDecisionFixture(t, []workflowNodeFixture{{}, {}, {}})
	fixture.replaceApproverIDs(t, 0, fixture.actorB.ID, fixture.actorA.ID)
	fixture.replaceApproverIDs(t, 1, fixture.actorA.ID)
	fixture.replaceApproverIDs(t, 2, fixture.actorB.ID)

	decisionTime := time.Date(2026, 7, 10, 9, 30, 0, 0, time.UTC)
	firstDecision := fixture.client.QuotaResetRequestDecision.Create().
		SetRequestID(fixture.request.ID).
		SetRequestNodeID(fixture.nodes[1].ID).
		SetActorUserID(fixture.actorA.ID).
		SetActorDisplayName("Approver Alpha Snapshot").
		SetDecision(quotaresetrequestdecision.DecisionApprove).
		SetComment("Approved second node first in insertion order").
		SetCreatedAt(decisionTime).
		SaveX(fixture.ctx)
	secondDecision := fixture.client.QuotaResetRequestDecision.Create().
		SetRequestID(fixture.request.ID).
		SetRequestNodeID(fixture.nodes[0].ID).
		SetActorUserID(fixture.admin.ID).
		SetActorDisplayName("Admin Snapshot").
		SetDecision(quotaresetrequestdecision.DecisionApprove).
		SetComment("Approved first node second in insertion order").
		SetAdminOverride(true).
		SetCreatedAt(decisionTime).
		SaveX(fixture.ctx)
	fixture.client.QuotaResetRequestNode.UpdateOneID(fixture.nodes[0].ID).
		SetStatus(quotaresetrequestnode.StatusApproved).
		SetSatisfiedByDecisionID(secondDecision.ID).
		SaveX(fixture.ctx)
	fixture.client.QuotaResetRequestNode.UpdateOneID(fixture.nodes[1].ID).
		SetStatus(quotaresetrequestnode.StatusSatisfiedByPriorApproval).
		SetSatisfiedByDecisionID(firstDecision.ID).
		SaveX(fixture.ctx)
	fixture.client.QuotaResetRequestNode.UpdateOneID(fixture.nodes[2].ID).
		SetStatus(quotaresetrequestnode.StatusActive).
		SaveX(fixture.ctx)
	fixture.request = fixture.client.QuotaResetRequest.UpdateOneID(fixture.request.ID).
		SetCurrentNodeID(fixture.nodes[2].ID).
		SaveX(fixture.ctx)
	fixture.client.User.UpdateOneID(fixture.requester.ID).
		SetUsername("live-name-must-not-leak").
		SetEmail("live-email-must-not-leak@example.net").
		SaveX(fixture.ctx)

	activeItems, err := fixture.service.summaries(fixture.ctx, []*ent.QuotaResetRequest{fixture.request}, summaryViewer{UserID: fixture.actorB.ID})
	if err != nil {
		t.Fatalf("summaries() active actor error = %v", err)
	}
	active := activeItems[0]
	if active.RequesterDisplayName != "Alice Example" || active.RequesterEmail != "alice@example.com" || !reflect.DeepEqual(active.RequesterDepartmentPaths, []string{"Department Alpha"}) {
		t.Fatalf("requester identity = %q / %q / %#v, want immutable snapshots", active.RequesterDisplayName, active.RequesterEmail, active.RequesterDepartmentPaths)
	}
	if active.Workflow == nil {
		t.Fatal("workflow = nil, want visible workflow for active approver")
	}
	workflow := active.Workflow
	if workflow.CurrentNode == nil || workflow.CurrentNode.ID != fixture.nodes[2].ID || len(workflow.Nodes) != 3 {
		t.Fatalf("workflow nodes/current = %+v / %+v", workflow.Nodes, workflow.CurrentNode)
	}
	for i, node := range workflow.Nodes {
		if node.ID != fixture.nodes[i].ID || node.Position != i {
			t.Fatalf("node[%d] = %+v, want request position order", i, node)
		}
	}
	if got := []int{workflow.Nodes[0].Approvers[0].UserID, workflow.Nodes[0].Approvers[1].UserID}; !reflect.DeepEqual(got, []int{fixture.actorA.ID, fixture.actorB.ID}) {
		t.Fatalf("node approver order = %#v, want deterministic user id order", got)
	}
	if len(workflow.Nodes[0].Departments) != 1 || workflow.Nodes[0].Departments[0].ExternalID != "department-review" || workflow.Nodes[0].SatisfiedByDecisionID == nil || *workflow.Nodes[0].SatisfiedByDecisionID != secondDecision.ID {
		t.Fatalf("node department/satisfied decision = %+v", workflow.Nodes[0])
	}
	if len(workflow.Decisions) != 2 || workflow.Decisions[0].ID != firstDecision.ID || workflow.Decisions[1].ID != secondDecision.ID || !workflow.Decisions[1].AdminOverride {
		t.Fatalf("decision order/details = %+v, want created_at then id", workflow.Decisions)
	}
	if !workflow.CanApprove || !workflow.CanReject || workflow.CanCancel || workflow.CanRetry {
		t.Fatalf("active approver permissions = %+v", workflow)
	}

	requesterItems, err := fixture.service.summaries(fixture.ctx, []*ent.QuotaResetRequest{fixture.request}, summaryViewer{UserID: fixture.requester.ID, Requester: true})
	if err != nil {
		t.Fatalf("summaries() requester error = %v", err)
	}
	if got := requesterItems[0].Workflow; got == nil || got.CanApprove || got.CanReject || !got.CanCancel || got.CanRetry {
		t.Fatalf("requester permissions = %+v", got)
	}

	adminItems, err := fixture.service.summaries(fixture.ctx, []*ent.QuotaResetRequest{fixture.request}, summaryViewer{UserID: fixture.admin.ID, Admin: true})
	if err != nil {
		t.Fatalf("summaries() admin error = %v", err)
	}
	if got := adminItems[0].Workflow; got == nil || !got.CanApprove || !got.CanReject || got.CanCancel || got.CanRetry {
		t.Fatalf("admin permissions = %+v", got)
	}

	adminRequesterItems, err := fixture.service.summaries(fixture.ctx, []*ent.QuotaResetRequest{fixture.request}, summaryViewer{UserID: fixture.requester.ID, Admin: true, Requester: true})
	if err != nil {
		t.Fatalf("summaries() admin requester error = %v", err)
	}
	if got := adminRequesterItems[0].Workflow; got == nil || got.CanApprove || got.CanReject || !got.CanCancel {
		t.Fatalf("admin requester permissions = %+v, want self-decision denied", got)
	}

	decidedItems, err := fixture.service.summaries(fixture.ctx, []*ent.QuotaResetRequest{fixture.request}, summaryViewer{UserID: fixture.actorA.ID})
	if err != nil {
		t.Fatalf("summaries() prior decision actor error = %v", err)
	}
	if got := decidedItems[0].Workflow; got == nil || got.CanApprove || got.CanReject || got.CanCancel || got.CanRetry {
		t.Fatalf("prior decision actor permissions = %+v", got)
	}
}

func TestWorkflowSummaryHidesFutureQueueFromUnrelatedApprover(t *testing.T) {
	fixture := newWorkflowDecisionFixture(t, []workflowNodeFixture{{}, {}})
	fixture.replaceApproverIDs(t, 0, fixture.actorA.ID)
	fixture.replaceApproverIDs(t, 1, fixture.actorB.ID)
	unrelated := createQuotaResetUser(t, fixture.ctx, fixture.client, "unrelated", "unrelated@example.net", nil, "user")

	for _, viewerID := range []int{fixture.actorB.ID, unrelated.ID} {
		items, err := fixture.service.summaries(fixture.ctx, []*ent.QuotaResetRequest{fixture.request}, summaryViewer{UserID: viewerID})
		if err != nil {
			t.Fatalf("summaries() viewer %d error = %v", viewerID, err)
		}
		if len(items) != 1 || items[0].Workflow != nil {
			t.Fatalf("viewer %d workflow = %+v, want hidden future queue", viewerID, items[0].Workflow)
		}
	}
}

func TestCountWorkItemsIncludesActiveNodeAndCompletionActorRetry(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	actor := createQuotaResetUser(t, ctx, client, "approver", "approver@example.com", nil, "user")
	other := createQuotaResetUser(t, ctx, client, "other", "other@example.org", nil, "user")
	requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")

	createV2WorkItemRequest(t, ctx, client, requester.ID, "active", quotaresetrequest.StatusPending, []int{actor.ID}, nil, nil)
	createV2WorkItemRequest(t, ctx, client, requester.ID, "future", quotaresetrequest.StatusPending, []int{other.ID}, []int{actor.ID}, nil)
	createV2WorkItemRequest(t, ctx, client, requester.ID, "retry", quotaresetrequest.StatusApprovedResetFailed, nil, nil, &actor.ID)
	createV2WorkItemRequest(t, ctx, client, requester.ID, "other-retry", quotaresetrequest.StatusApprovedResetFailed, nil, nil, &other.ID)
	createV2WorkItemRequest(t, ctx, client, actor.ID, "own", quotaresetrequest.StatusPending, []int{actor.ID}, nil, nil)

	counts, err := CountWorkItems(ctx, client, actor.ID, false)
	if err != nil {
		t.Fatalf("CountWorkItems() error = %v", err)
	}
	if counts.Assigned != 2 || counts.Admin != 0 {
		t.Fatalf("counts = %+v, want active assignment plus owned retry", counts)
	}
}

func TestCountWorkItemsAdminUsesAllPendingWithoutDoubleCounting(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	admin := createQuotaResetUser(t, ctx, client, "admin", "admin@example.com", nil, "admin")
	requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")

	createLegacyWorkItemRequest(t, ctx, client, requester.ID, "legacy-pending", quotaresetrequest.StatusPending, []int{admin.ID})
	createLegacyWorkItemRequest(t, ctx, client, requester.ID, "legacy-failed", quotaresetrequest.StatusApprovedResetFailed, []int{admin.ID})
	createV2WorkItemRequest(t, ctx, client, requester.ID, "v2-pending", quotaresetrequest.StatusPending, []int{admin.ID}, nil, nil)
	createV2WorkItemRequest(t, ctx, client, requester.ID, "v2-failed", quotaresetrequest.StatusApprovedResetFailed, nil, nil, &admin.ID)
	createV2WorkItemRequest(t, ctx, client, requester.ID, "resetting", quotaresetrequest.StatusApprovedResetting, nil, nil, nil)
	createV2WorkItemRequest(t, ctx, client, requester.ID, "succeeded", quotaresetrequest.StatusApprovedResetSucceeded, nil, nil, nil)

	counts, err := CountWorkItems(ctx, client, admin.ID, true)
	if err != nil {
		t.Fatalf("CountWorkItems() error = %v", err)
	}
	if counts.Assigned != 4 {
		t.Fatalf("assigned = %d, want four actor-owned requests", counts.Assigned)
	}
	if counts.Admin != 4 {
		t.Fatalf("admin = %d, want each pending/failed request counted once", counts.Admin)
	}
}

func TestCountWorkItemsKeepsLegacyV1Semantics(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	actor := createQuotaResetUser(t, ctx, client, "approver", "approver@example.com", nil, "user")
	requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")

	createLegacyWorkItemRequest(t, ctx, client, requester.ID, "pending", quotaresetrequest.StatusPending, []int{actor.ID})
	createLegacyWorkItemRequest(t, ctx, client, requester.ID, "failed", quotaresetrequest.StatusApprovedResetFailed, []int{actor.ID})
	createLegacyWorkItemRequest(t, ctx, client, actor.ID, "own", quotaresetrequest.StatusPending, []int{actor.ID})
	createLegacyWorkItemRequest(t, ctx, client, requester.ID, "succeeded", quotaresetrequest.StatusApprovedResetSucceeded, []int{actor.ID})
	createLegacyWorkItemRequest(t, ctx, client, requester.ID, "not-assigned", quotaresetrequest.StatusPending, nil)

	counts, err := CountWorkItems(ctx, client, actor.ID, false)
	if err != nil {
		t.Fatalf("CountWorkItems() error = %v", err)
	}
	if counts.Assigned != 2 || counts.Admin != 0 {
		t.Fatalf("counts = %+v, want legacy pending and failed assignment semantics", counts)
	}
}

func createV2WorkItemRequest(t *testing.T, ctx context.Context, client *ent.Client, requesterUserID int, groupID string, status quotaresetrequest.Status, currentApproverIDs, futureApproverIDs []int, completionActorID *int) (*ent.QuotaResetRequest, *ent.QuotaResetRequestNode, *ent.QuotaResetRequestNode) {
	t.Helper()
	request := client.QuotaResetRequest.Create().
		SetRequesterUserID(requesterUserID).
		SetRequesterRelayUserID(int64(1000 + requesterUserID)).
		SetProviderID(1).
		SetGroupID(groupID).
		SetGroupName("Group Alpha").
		SetGroupPlatform("openai").
		SetReason("Need reset for a build investigation").
		SetWorkflowVersion(WorkflowVersionV2).
		SetRequesterDisplayNameSnapshot("Alice Snapshot").
		SetRequesterEmailSnapshot("alice@example.com").
		SetRequesterDepartmentPaths([]string{"Department Alpha"}).
		SetRequesterNotificationIds(map[string]string{}).
		SetStatus(status).
		SetResolvedApproverUserIds([]int{}).
		SetMatchedDepartmentPaths([]map[string]any{}).
		SaveX(ctx)
	currentStatus := quotaresetrequestnode.StatusApproved
	if status == quotaresetrequest.StatusPending {
		currentStatus = quotaresetrequestnode.StatusActive
	}
	current := client.QuotaResetRequestNode.Create().
		SetRequestID(request.ID).
		SetPosition(0).
		SetNodeType(quotaresetrequestnode.NodeTypeConfiguredDepartment).
		SetLabel("Current review").
		SetDepartmentSnapshots([]map[string]any{{"external_id": "dept-current", "display_path": "Department Current", "resolution": "configured"}}).
		SetStatus(currentStatus).
		SaveX(ctx)
	for _, approverID := range currentApproverIDs {
		createWorkflowNodeApprover(t, ctx, client, current.ID, approverID)
	}
	var future *ent.QuotaResetRequestNode
	if futureApproverIDs != nil {
		future = client.QuotaResetRequestNode.Create().
			SetRequestID(request.ID).
			SetPosition(1).
			SetNodeType(quotaresetrequestnode.NodeTypeConfiguredDepartment).
			SetLabel("Future review").
			SetDepartmentSnapshots([]map[string]any{}).
			SetStatus(quotaresetrequestnode.StatusQueued).
			SaveX(ctx)
		for _, approverID := range futureApproverIDs {
			createWorkflowNodeApprover(t, ctx, client, future.ID, approverID)
		}
	}
	update := client.QuotaResetRequest.UpdateOneID(request.ID)
	if status == quotaresetrequest.StatusPending {
		update.SetCurrentNodeID(current.ID)
	}
	request = update.SaveX(ctx)
	if completionActorID != nil {
		actor := client.User.GetX(ctx, *completionActorID)
		decision := client.QuotaResetRequestDecision.Create().
			SetRequestID(request.ID).
			SetRequestNodeID(current.ID).
			SetActorUserID(actor.ID).
			SetActorDisplayName(actor.Username).
			SetDecision(quotaresetrequestdecision.DecisionApprove).
			SetComment("Approved before reset failure").
			SaveX(ctx)
		client.QuotaResetRequestNode.UpdateOneID(current.ID).
			SetSatisfiedByDecisionID(decision.ID).
			SaveX(ctx)
		request = client.QuotaResetRequest.UpdateOneID(request.ID).
			SetWorkflowCompletedByDecisionID(decision.ID).
			SaveX(ctx)
	}
	return request, current, future
}

func createLegacyWorkItemRequest(t *testing.T, ctx context.Context, client *ent.Client, requesterUserID int, groupID string, status quotaresetrequest.Status, approverIDs []int) *ent.QuotaResetRequest {
	t.Helper()
	create := client.QuotaResetRequest.Create().
		SetRequesterUserID(requesterUserID).
		SetRequesterRelayUserID(int64(1000 + requesterUserID)).
		SetProviderID(1).
		SetGroupID(groupID).
		SetGroupName("Group Alpha").
		SetGroupPlatform("openai").
		SetReason("Need reset for a build investigation").
		SetStatus(status).
		SetMatchedDepartmentPaths([]map[string]any{})
	if approverIDs != nil {
		create.SetResolvedApproverUserIds(approverIDs)
	}
	return create.SaveX(ctx)
}
