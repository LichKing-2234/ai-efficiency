package quotareset

import (
	"context"
	stdsql "database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	entsql "entgo.io/ent/dialect/sql"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/quotaresetrequest"
	"github.com/ai-efficiency/backend/ent/quotaresetrequestdecision"
	"github.com/ai-efficiency/backend/ent/quotaresetrequestevent"
	"github.com/ai-efficiency/backend/ent/quotaresetrequestnode"
	"github.com/ai-efficiency/backend/ent/quotaresetrequestnodeapprover"
	entuser "github.com/ai-efficiency/backend/ent/user"
	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/ai-efficiency/backend/internal/testdb"
)

func TestCreateRequestSnapshotsV2WorkflowAndActivatesFirstReachableNode(t *testing.T) {
	fixture := newWorkflowCreationFixture(t, true, true)

	request, err := fixture.service.CreateRequest(fixture.ctx, CreateRequestInput{
		RequesterUserID: fixture.requester.ID,
		GroupID:         "42",
		Reason:          " Need a reset for a build investigation ",
	})
	if err != nil {
		t.Fatalf("CreateRequest() error = %v", err)
	}
	request = fixture.client.QuotaResetRequest.GetX(fixture.ctx, request.ID)
	if request.WorkflowVersion != WorkflowVersionV2 {
		t.Fatalf("workflow version = %d, want %d", request.WorkflowVersion, WorkflowVersionV2)
	}
	if request.Reason != "Need a reset for a build investigation" || request.RequesterDisplayNameSnapshot != "member-alice" || request.RequesterEmailSnapshot != fixture.requester.Email {
		t.Fatalf("request snapshots = %+v", request)
	}
	if !reflect.DeepEqual(request.RequesterDepartmentPaths, []string{"Department Alpha"}) || !reflect.DeepEqual(request.RequesterNotificationIds, map[string]string{"wecom": "member-alice"}) {
		t.Fatalf("requester directory snapshots = %#v / %#v", request.RequesterDepartmentPaths, request.RequesterNotificationIds)
	}
	if request.ResolvedApproverUserIds == nil || len(request.ResolvedApproverUserIds) != 0 || request.MatchedDepartmentPaths == nil || len(request.MatchedDepartmentPaths) != 0 {
		t.Fatalf("legacy snapshots = %#v / %#v, want fresh empty values", request.ResolvedApproverUserIds, request.MatchedDepartmentPaths)
	}
	nodes := workflowRequestNodes(t, fixture.ctx, fixture.client, request.ID)
	if len(nodes) != 2 || nodes[0].Status != quotaresetrequestnode.StatusActive || nodes[1].Status != quotaresetrequestnode.StatusQueued {
		t.Fatalf("nodes = %#v, want active initial and queued configured", nodes)
	}
	if request.CurrentNodeID == nil || *request.CurrentNodeID != nodes[0].ID || nodes[0].ActivatedAt == nil {
		t.Fatalf("current node = %v, initial = %+v", request.CurrentNodeID, nodes[0])
	}
	assertWorkflowNodeApprovers(t, fixture.ctx, fixture.client, nodes[0].ID, fixture.actorA.ID)
	assertWorkflowNodeApprovers(t, fixture.ctx, fixture.client, nodes[1].ID, fixture.actorB.ID)
	initialApprover := fixture.client.QuotaResetRequestNodeApprover.Query().
		Where(quotaresetrequestnodeapprover.RequestNodeIDEQ(nodes[0].ID)).
		OnlyX(fixture.ctx)
	if initialApprover.DisplayName != "member-approver-alpha" || initialApprover.Email != fixture.actorA.Email || initialApprover.Source != quotaresetrequestnodeapprover.SourceConfigured ||
		!reflect.DeepEqual(initialApprover.SourceDepartmentExternalIds, []string{fixture.initialDepartment.ExternalID}) ||
		!reflect.DeepEqual(initialApprover.NotificationIds, map[string]string{"wecom": "member-approver-alpha"}) {
		t.Fatalf("initial approver snapshot = %+v", initialApprover)
	}
	assertWorkflowEventCount(t, fixture.ctx, fixture.client, request.ID, quotaresetrequestevent.EventTypeWorkflowSnapshotted, 1)
	assertWorkflowEventCount(t, fixture.ctx, fixture.client, request.ID, quotaresetrequestevent.EventTypeNodeActivated, 1)
	if fixture.provider.resetCalls != 0 {
		t.Fatalf("reset calls = %d, want 0", fixture.provider.resetCalls)
	}
}

func TestCreateRequestSkipsEmptyInitialNodeAndActivatesConfiguredNode(t *testing.T) {
	fixture := newWorkflowCreationFixture(t, false, true)

	request, err := fixture.service.CreateRequest(fixture.ctx, CreateRequestInput{
		RequesterUserID: fixture.requester.ID,
		GroupID:         "42",
		Reason:          "Need a reset for a build investigation",
	})
	if err != nil {
		t.Fatalf("CreateRequest() error = %v", err)
	}
	request = fixture.client.QuotaResetRequest.GetX(fixture.ctx, request.ID)
	nodes := workflowRequestNodes(t, fixture.ctx, fixture.client, request.ID)
	if len(nodes) != 2 || nodes[0].Status != quotaresetrequestnode.StatusSkippedNoApprover || nodes[1].Status != quotaresetrequestnode.StatusActive {
		t.Fatalf("nodes = %#v, want skipped initial and active configured", nodes)
	}
	if request.CurrentNodeID == nil || *request.CurrentNodeID != nodes[1].ID {
		t.Fatalf("current node = %v, want %d", request.CurrentNodeID, nodes[1].ID)
	}
	assertWorkflowEventCount(t, fixture.ctx, fixture.client, request.ID, quotaresetrequestevent.EventTypeNodeSkippedNoApprover, 1)
}

func TestCreateRequestWithOnlySkippedInitialNodeExecutesReset(t *testing.T) {
	fixture := newWorkflowCreationFixture(t, false, false)

	request, err := fixture.service.CreateRequest(fixture.ctx, CreateRequestInput{
		RequesterUserID: fixture.requester.ID,
		GroupID:         "42",
		Reason:          "Need a reset for a build investigation",
	})
	if err != nil {
		t.Fatalf("CreateRequest() error = %v", err)
	}
	if request.Status != quotaresetrequest.StatusApprovedResetSucceeded || request.CurrentNodeID != nil {
		t.Fatalf("request = %+v, want reset success without current node", request)
	}
	nodes := workflowRequestNodes(t, fixture.ctx, fixture.client, request.ID)
	if len(nodes) != 1 || nodes[0].Status != quotaresetrequestnode.StatusSkippedNoApprover {
		t.Fatalf("nodes = %#v, want one skipped node", nodes)
	}
	if fixture.provider.resetCalls != 1 {
		t.Fatalf("reset calls = %d, want 1", fixture.provider.resetCalls)
	}
}

func TestCreateWorkflowRequestRollsBackBeforeDuplicateLookup(t *testing.T) {
	ctx := context.Background()
	setupClient, dsn := testdb.OpenWithDSN(t)
	requester := createQuotaResetUser(t, ctx, setupClient, "alice", "alice@example.com", intPtr(1001), "user")
	createPendingWorkflowDirectoryFixture(t, ctx, setupClient, requester)
	providerRow := createQuotaResetRelayProvider(t, ctx, setupClient)
	createPendingQuotaResetRequest(t, ctx, setupClient, requester.ID, 1001, providerRow.ID, "42", []int{})

	db, err := stdsql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open single-connection database: %v", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	singleClient := ent.NewClient(ent.Driver(entsql.OpenDB("postgres", db)))
	t.Cleanup(func() {
		if err := singleClient.Close(); err != nil {
			t.Errorf("close single-connection Ent client: %v", err)
		}
	})
	provider := &fakeQuotaResetProvider{subscriptions: []relay.UserSubscription{activeQuotaResetSubscription(42, "Group Alpha")}}
	service := NewService(singleClient, fakeProviderResolver(providerRow.ID, provider), NewApproverResolver(singleClient), nil)

	callCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	_, err = service.createWorkflowRequest(callCtx, requester, providerRow, activeQuotaResetSubscription(42, "Group Alpha"), CreateRequestInput{
		RequesterUserID: requester.ID,
		GroupID:         "42",
		Reason:          "Duplicate active request",
	})
	if !errors.Is(err, ErrActiveRequestExists) {
		t.Fatalf("createWorkflowRequest() error = %v, want ErrActiveRequestExists", err)
	}
	if callCtx.Err() != nil {
		t.Fatalf("duplicate lookup exhausted context instead of returning promptly: %v", callCtx.Err())
	}
}

func TestWorkflowApproveRequiresComment(t *testing.T) {
	fixture := newWorkflowDecisionFixture(t, []workflowNodeFixture{{approverIDs: []int{1}}})
	fixture.replaceApproverIDs(t, 0, fixture.actorA.ID)

	_, err := fixture.service.Approve(fixture.ctx, DecisionInput{
		ActorUserID:    fixture.actorA.ID,
		RequestID:      fixture.request.ID,
		RequestNodeID:  fixture.nodes[0].ID,
		DecisionReason: "  ",
	})
	if !errors.Is(err, ErrDecisionRequired) {
		t.Fatalf("Approve() error = %v, want ErrDecisionRequired", err)
	}
	if count := fixture.client.QuotaResetRequestDecision.Query().CountX(fixture.ctx); count != 0 {
		t.Fatalf("decision count = %d, want 0", count)
	}
}

func TestWorkflowApproveSatisfiesEveryLaterNodeContainingActor(t *testing.T) {
	fixture := newWorkflowDecisionFixture(t, []workflowNodeFixture{
		{}, {}, {}, {},
	})
	fixture.replaceApproverIDs(t, 0, fixture.actorA.ID)
	fixture.replaceApproverIDs(t, 1, fixture.actorB.ID)
	fixture.replaceApproverIDs(t, 2, fixture.actorA.ID)
	fixture.replaceApproverIDs(t, 3, fixture.actorA.ID)

	updated, err := fixture.service.Approve(fixture.ctx, DecisionInput{
		ActorUserID:    fixture.actorA.ID,
		RequestID:      fixture.request.ID,
		RequestNodeID:  fixture.nodes[0].ID,
		DecisionReason: "Approved at the first review",
	})
	if err != nil {
		t.Fatalf("Approve() error = %v", err)
	}
	if updated.Status != quotaresetrequest.StatusPending {
		t.Fatalf("status = %s, want pending", updated.Status)
	}
	if got := fixture.client.QuotaResetRequestNode.GetX(fixture.ctx, fixture.nodes[1].ID).Status; got != quotaresetrequestnode.StatusActive {
		t.Fatalf("node 1 status = %s, want active", got)
	}
	for _, id := range []int{fixture.nodes[2].ID, fixture.nodes[3].ID} {
		if got := fixture.client.QuotaResetRequestNode.GetX(fixture.ctx, id).Status; got != quotaresetrequestnode.StatusSatisfiedByPriorApproval {
			t.Fatalf("node %d status = %s, want satisfied_by_prior_approval", id, got)
		}
	}
	if fixture.provider.resetCalls != 0 {
		t.Fatalf("reset calls = %d, want 0", fixture.provider.resetCalls)
	}

	updated, err = fixture.service.Approve(fixture.ctx, DecisionInput{
		ActorUserID:    fixture.actorB.ID,
		RequestID:      fixture.request.ID,
		RequestNodeID:  fixture.nodes[1].ID,
		DecisionReason: "Approved the remaining review",
	})
	if err != nil {
		t.Fatalf("second Approve() error = %v", err)
	}
	if updated.Status != quotaresetrequest.StatusApprovedResetSucceeded || fixture.provider.resetCalls != 1 {
		t.Fatalf("status/reset calls = %s/%d, want success/1", updated.Status, fixture.provider.resetCalls)
	}
	if count := fixture.client.QuotaResetRequestDecision.Query().CountX(fixture.ctx); count != 2 {
		t.Fatalf("decision count = %d, want 2", count)
	}
}

func TestWorkflowApproveLeavesUnmatchedIntermediateNodeActive(t *testing.T) {
	fixture := newWorkflowDecisionFixture(t, []workflowNodeFixture{{}, {}, {}})
	fixture.replaceApproverIDs(t, 0, fixture.actorA.ID)
	fixture.replaceApproverIDs(t, 1, fixture.actorB.ID)
	fixture.replaceApproverIDs(t, 2, fixture.actorA.ID)

	updated, err := fixture.service.Approve(fixture.ctx, DecisionInput{
		ActorUserID:    fixture.actorA.ID,
		RequestID:      fixture.request.ID,
		RequestNodeID:  fixture.nodes[0].ID,
		DecisionReason: "Approved at the first review",
	})
	if err != nil {
		t.Fatalf("Approve() error = %v", err)
	}
	updated = fixture.client.QuotaResetRequest.GetX(fixture.ctx, updated.ID)
	if updated.CurrentNodeID == nil || *updated.CurrentNodeID != fixture.nodes[1].ID {
		t.Fatalf("current node = %v, want intermediate node %d", updated.CurrentNodeID, fixture.nodes[1].ID)
	}
	if got := fixture.client.QuotaResetRequestNode.GetX(fixture.ctx, fixture.nodes[2].ID).Status; got != quotaresetrequestnode.StatusSatisfiedByPriorApproval {
		t.Fatalf("later matching node status = %s", got)
	}
	if fixture.provider.resetCalls != 0 {
		t.Fatalf("reset calls = %d, want 0", fixture.provider.resetCalls)
	}
}

func TestWorkflowAdminOverrideCompletesCurrentNodeOnly(t *testing.T) {
	fixture := newWorkflowDecisionFixture(t, []workflowNodeFixture{{}, {}})
	fixture.replaceApproverIDs(t, 0, fixture.actorA.ID)
	fixture.replaceApproverIDs(t, 1, fixture.actorB.ID)

	updated, err := fixture.service.Approve(fixture.ctx, DecisionInput{
		ActorUserID:    fixture.admin.ID,
		RequestID:      fixture.request.ID,
		RequestNodeID:  fixture.nodes[0].ID,
		DecisionReason: "Approved through admin fallback",
		Admin:          true,
	})
	if err != nil {
		t.Fatalf("Approve() error = %v", err)
	}
	updated = fixture.client.QuotaResetRequest.GetX(fixture.ctx, updated.ID)
	if updated.CurrentNodeID == nil || *updated.CurrentNodeID != fixture.nodes[1].ID {
		t.Fatalf("current node = %v, want later node %d", updated.CurrentNodeID, fixture.nodes[1].ID)
	}
	decision := fixture.client.QuotaResetRequestDecision.Query().OnlyX(fixture.ctx)
	if !decision.AdminOverride || decision.Comment != "Approved through admin fallback" {
		t.Fatalf("decision = %+v, want durable admin override", decision)
	}
	if fixture.provider.resetCalls != 0 {
		t.Fatalf("reset calls = %d, want 0", fixture.provider.resetCalls)
	}
}

func TestWorkflowRejectsRequesterSelfApprovalEvenForAdmin(t *testing.T) {
	fixture := newWorkflowDecisionFixture(t, []workflowNodeFixture{{}})
	fixture.client.User.UpdateOneID(fixture.requester.ID).SetRole(entuser.RoleAdmin).SaveX(fixture.ctx)
	fixture.replaceApproverIDs(t, 0, fixture.requester.ID)

	_, err := fixture.service.Approve(fixture.ctx, DecisionInput{
		ActorUserID:    fixture.requester.ID,
		RequestID:      fixture.request.ID,
		RequestNodeID:  fixture.nodes[0].ID,
		DecisionReason: "Trying to approve my own request",
		Admin:          true,
	})
	if !errors.Is(err, ErrSelfApprovalForbidden) {
		t.Fatalf("Approve() error = %v, want ErrSelfApprovalForbidden", err)
	}
}

func TestWorkflowAdminFallbackWritesActivationEvent(t *testing.T) {
	fixture := newWorkflowCreationFixture(t, false, true)
	fixture.client.QuotaResetApproverConfig.Delete().ExecX(fixture.ctx)

	request, err := fixture.service.CreateRequest(fixture.ctx, CreateRequestInput{
		RequesterUserID: fixture.requester.ID,
		GroupID:         "42",
		Reason:          "Need a reset for a build investigation",
	})
	if err != nil {
		t.Fatalf("CreateRequest() error = %v", err)
	}
	nodes := workflowRequestNodes(t, fixture.ctx, fixture.client, request.ID)
	if len(nodes) != 2 || !nodes[1].AdminFallbackRequired || nodes[1].Status != quotaresetrequestnode.StatusActive {
		t.Fatalf("nodes = %#v, want active admin fallback node", nodes)
	}
	assertWorkflowEventCount(t, fixture.ctx, fixture.client, request.ID, quotaresetrequestevent.EventTypeNodeActivated, 1)
	assertWorkflowEventCount(t, fixture.ctx, fixture.client, request.ID, quotaresetrequestevent.EventTypeAdminFallbackActivated, 1)
}

func TestWorkflowRejectTerminatesRequest(t *testing.T) {
	fixture := newWorkflowDecisionFixture(t, []workflowNodeFixture{{}, {}})
	fixture.replaceApproverIDs(t, 0, fixture.actorA.ID)
	fixture.replaceApproverIDs(t, 1, fixture.actorB.ID)

	updated, err := fixture.service.Reject(fixture.ctx, DecisionInput{
		ActorUserID:    fixture.actorA.ID,
		RequestID:      fixture.request.ID,
		RequestNodeID:  fixture.nodes[0].ID,
		DecisionReason: "Insufficient justification",
	})
	if err != nil {
		t.Fatalf("Reject() error = %v", err)
	}
	if updated.Status != quotaresetrequest.StatusRejected || updated.CurrentNodeID != nil {
		t.Fatalf("request = %+v, want terminal rejection", updated)
	}
	if got := fixture.client.QuotaResetRequestNode.GetX(fixture.ctx, fixture.nodes[0].ID).Status; got != quotaresetrequestnode.StatusRejected {
		t.Fatalf("current node status = %s, want rejected", got)
	}
	if got := fixture.client.QuotaResetRequestNode.GetX(fixture.ctx, fixture.nodes[1].ID).Status; got != quotaresetrequestnode.StatusQueued {
		t.Fatalf("future node status = %s, want queued evidence", got)
	}
	decision := fixture.client.QuotaResetRequestDecision.Query().OnlyX(fixture.ctx)
	if decision.Decision != quotaresetrequestdecision.DecisionReject || decision.Comment != "Insufficient justification" {
		t.Fatalf("decision = %+v", decision)
	}
	if fixture.provider.resetCalls != 0 {
		t.Fatalf("reset calls = %d, want 0", fixture.provider.resetCalls)
	}
}

func TestWorkflowDecisionRejectsStaleNode(t *testing.T) {
	fixture := newWorkflowDecisionFixture(t, []workflowNodeFixture{{}, {}})
	fixture.replaceApproverIDs(t, 0, fixture.actorA.ID, fixture.actorB.ID)
	fixture.replaceApproverIDs(t, 1, fixture.actorB.ID)

	ctx, cancel := context.WithTimeout(fixture.ctx, 10*time.Second)
	defer cancel()
	gateTx, err := fixture.client.Tx(ctx)
	if err != nil {
		t.Fatalf("begin gate transaction: %v", err)
	}
	gateCommitted := false
	defer func() {
		if !gateCommitted {
			_ = gateTx.Rollback()
		}
	}()
	if _, err := gateTx.QuotaResetRequest.Query().
		Where(
			quotaresetrequest.IDEQ(fixture.request.ID),
			func(selector *entsql.Selector) { selector.ForUpdate() },
		).
		Only(ctx); err != nil {
		t.Fatalf("lock gate request: %v", err)
	}

	type callResult struct {
		operation string
		err       error
	}
	reachedRequestLockQuery := make(chan string, 2)
	releaseRequestLockQuery := make(chan struct{})
	releasedRequestLockQuery := make(chan string, 2)
	callerContext := func(operation string) context.Context {
		return context.WithValue(ctx, workflowRequestLockQueryHookContextKey{}, workflowRequestLockQueryHook(func() {
			reachedRequestLockQuery <- operation
			select {
			case <-releaseRequestLockQuery:
				releasedRequestLockQuery <- operation
			case <-ctx.Done():
			}
		}))
	}
	results := make(chan callResult, 2)
	go func() {
		_, err := fixture.service.Approve(callerContext("approve"), DecisionInput{
			ActorUserID:    fixture.actorA.ID,
			RequestID:      fixture.request.ID,
			RequestNodeID:  fixture.nodes[0].ID,
			DecisionReason: "Approved the current node",
		})
		results <- callResult{operation: "approve", err: err}
	}()
	go func() {
		_, err := fixture.service.Reject(callerContext("reject"), DecisionInput{
			ActorUserID:    fixture.actorB.ID,
			RequestID:      fixture.request.ID,
			RequestNodeID:  fixture.nodes[0].ID,
			DecisionReason: "Rejected the current node",
		})
		results <- callResult{operation: "reject", err: err}
	}()
	reached := make(map[string]bool, 2)
	for i := 0; i < 2; i++ {
		select {
		case operation := <-reachedRequestLockQuery:
			reached[operation] = true
		case <-ctx.Done():
			t.Fatalf("waiting for decision caller %d to reach request lock query: %v", i+1, ctx.Err())
		}
	}
	if !reached["approve"] || !reached["reject"] {
		t.Fatalf("request lock query callers = %#v, want approve and reject", reached)
	}
	close(releaseRequestLockQuery)
	released := make(map[string]bool, 2)
	for i := 0; i < 2; i++ {
		select {
		case operation := <-releasedRequestLockQuery:
			released[operation] = true
		case <-ctx.Done():
			t.Fatalf("waiting for decision caller %d request lock hook release: %v", i+1, ctx.Err())
		}
	}
	if !released["approve"] || !released["reject"] {
		t.Fatalf("released request lock query callers = %#v, want approve and reject", released)
	}
	if err := gateTx.Commit(); err != nil {
		t.Fatalf("release request lock gate: %v", err)
	}
	gateCommitted = true

	completed := make([]callResult, 0, 2)
	resultDeadline := time.NewTimer(5 * time.Second)
	defer resultDeadline.Stop()
	for len(completed) < 2 {
		select {
		case result := <-results:
			completed = append(completed, result)
		case <-resultDeadline.C:
			cancel()
			t.Fatalf("timed out waiting for concurrent decisions; received %d of 2 results", len(completed))
		}
	}

	var winner string
	advancedCount := 0
	for _, result := range completed {
		if result.err == nil {
			if winner != "" {
				t.Fatalf("both concurrent decisions succeeded: first=%s second=%s", winner, result.operation)
			}
			winner = result.operation
			continue
		}
		var advanced *WorkflowAdvancedError
		if !errors.As(result.err, &advanced) || advanced.RequestID != fixture.request.ID || !errors.Is(result.err, ErrWorkflowAdvanced) {
			t.Fatalf("%s error = %v, want WorkflowAdvancedError for request %d", result.operation, result.err, fixture.request.ID)
		}
		if latest := requireWorkflowAdvancedLatest(t, advanced); latest.ID != fixture.request.ID {
			t.Fatalf("%s latest summary id = %d, want %d", result.operation, latest.ID, fixture.request.ID)
		}
		advancedCount++
	}
	if winner == "" || advancedCount != 1 {
		t.Fatalf("winner/advanced count = %q/%d, want one success and one workflow_advanced", winner, advancedCount)
	}
	if count := fixture.client.QuotaResetRequestDecision.Query().CountX(fixture.ctx); count != 1 {
		t.Fatalf("decision count = %d, want 1", count)
	}
	decision := fixture.client.QuotaResetRequestDecision.Query().OnlyX(fixture.ctx)
	latest := fixture.client.QuotaResetRequest.GetX(fixture.ctx, fixture.request.ID)
	current := fixture.client.QuotaResetRequestNode.GetX(fixture.ctx, fixture.nodes[0].ID)
	future := fixture.client.QuotaResetRequestNode.GetX(fixture.ctx, fixture.nodes[1].ID)
	switch winner {
	case "approve":
		if decision.Decision != quotaresetrequestdecision.DecisionApprove || latest.Status != quotaresetrequest.StatusPending || latest.CurrentNodeID == nil || *latest.CurrentNodeID != future.ID || current.Status != quotaresetrequestnode.StatusApproved || future.Status != quotaresetrequestnode.StatusActive {
			t.Fatalf("approve winner state = decision %s request %s/%v nodes %s/%s", decision.Decision, latest.Status, latest.CurrentNodeID, current.Status, future.Status)
		}
	case "reject":
		if decision.Decision != quotaresetrequestdecision.DecisionReject || latest.Status != quotaresetrequest.StatusRejected || latest.CurrentNodeID != nil || current.Status != quotaresetrequestnode.StatusRejected || future.Status != quotaresetrequestnode.StatusQueued {
			t.Fatalf("reject winner state = decision %s request %s/%v nodes %s/%s", decision.Decision, latest.Status, latest.CurrentNodeID, current.Status, future.Status)
		}
	default:
		t.Fatalf("unexpected winner %q", winner)
	}
	if fixture.provider.resetCalls != 0 {
		t.Fatalf("reset calls = %d, want 0", fixture.provider.resetCalls)
	}
}

func TestWorkflowSnapshotDoesNotChangeAfterConfigMutation(t *testing.T) {
	fixture := newWorkflowCreationFixture(t, true, true)
	request, err := fixture.service.CreateRequest(fixture.ctx, CreateRequestInput{
		RequesterUserID: fixture.requester.ID,
		GroupID:         "42",
		Reason:          "Need a reset for a build investigation",
	})
	if err != nil {
		t.Fatalf("CreateRequest() error = %v", err)
	}
	nodesBefore := workflowRequestNodes(t, fixture.ctx, fixture.client, request.ID)
	approversBefore := workflowApproverSnapshots(t, fixture.ctx, fixture.client, request.ID)

	fixture.client.QuotaResetApproverConfig.Delete().ExecX(fixture.ctx)
	createQuotaResetApproverConfig(t, fixture.ctx, fixture.client, fixture.source.ID, fixture.initialDepartment.ExternalID, fixture.initialDepartment.Name, fixture.actorB.ID)
	fixture.client.User.UpdateOneID(fixture.actorA.ID).SetUsername("changed-after-snapshot").SaveX(fixture.ctx)

	nodesAfter := workflowRequestNodes(t, fixture.ctx, fixture.client, request.ID)
	approversAfter := workflowApproverSnapshots(t, fixture.ctx, fixture.client, request.ID)
	if mustWorkflowTestJSON(t, nodesBefore) != mustWorkflowTestJSON(t, nodesAfter) || mustWorkflowTestJSON(t, approversBefore) != mustWorkflowTestJSON(t, approversAfter) {
		t.Fatalf("persisted workflow changed after config mutation\nbefore nodes=%#v\nafter nodes=%#v\nbefore approvers=%#v\nafter approvers=%#v", nodesBefore, nodesAfter, approversBefore, approversAfter)
	}
}

func TestWorkflowRetryAllowsCompletionActorAndAdminOnly(t *testing.T) {
	t.Run("completion actor", func(t *testing.T) {
		fixture := newFailedWorkflowRetryFixture(t)
		updated, err := fixture.service.RetryReset(fixture.ctx, DecisionInput{ActorUserID: fixture.actorA.ID, RequestID: fixture.request.ID})
		if err != nil || updated.Status != quotaresetrequest.StatusApprovedResetSucceeded {
			t.Fatalf("RetryReset(completion actor) = %+v, %v", updated, err)
		}
	})

	t.Run("unrelated actor", func(t *testing.T) {
		fixture := newFailedWorkflowRetryFixture(t)
		_, err := fixture.service.RetryReset(fixture.ctx, DecisionInput{ActorUserID: fixture.actorB.ID, RequestID: fixture.request.ID})
		if !errors.Is(err, ErrNotApprover) || fixture.provider.resetCalls != 0 {
			t.Fatalf("RetryReset(unrelated) error/reset calls = %v/%d, want ErrNotApprover/0", err, fixture.provider.resetCalls)
		}
	})

	t.Run("admin", func(t *testing.T) {
		fixture := newFailedWorkflowRetryFixture(t)
		updated, err := fixture.service.RetryReset(fixture.ctx, DecisionInput{ActorUserID: fixture.admin.ID, RequestID: fixture.request.ID, Admin: true})
		if err != nil || updated.Status != quotaresetrequest.StatusApprovedResetSucceeded {
			t.Fatalf("RetryReset(admin) = %+v, %v", updated, err)
		}
	})

	t.Run("admin flag requires current admin", func(t *testing.T) {
		fixture := newFailedWorkflowRetryFixture(t)
		_, err := fixture.service.RetryReset(fixture.ctx, DecisionInput{ActorUserID: fixture.actorB.ID, RequestID: fixture.request.ID, Admin: true})
		if !errors.Is(err, ErrNotApprover) || fixture.provider.resetCalls != 0 {
			t.Fatalf("RetryReset(non-admin flag) error/reset calls = %v/%d, want ErrNotApprover/0", err, fixture.provider.resetCalls)
		}
	})
}

func TestCancelDispatchPreservesV1AndV2Behavior(t *testing.T) {
	for _, workflowVersion := range []int{1, WorkflowVersionV2} {
		t.Run(quotaresetWorkflowVersionName(workflowVersion), func(t *testing.T) {
			ctx := context.Background()
			client := testdb.Open(t)
			requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
			provider := createQuotaResetRelayProvider(t, ctx, client)
			create := client.QuotaResetRequest.Create().
				SetRequesterUserID(requester.ID).
				SetRequesterRelayUserID(1001).
				SetProviderID(provider.ID).
				SetGroupID("42").
				SetGroupName("Group Alpha").
				SetGroupPlatform("openai").
				SetReason("Need a reset for a build investigation").
				SetResolvedApproverUserIds([]int{}).
				SetMatchedDepartmentPaths([]map[string]any{})
			if workflowVersion == WorkflowVersionV2 {
				create.SetWorkflowVersion(WorkflowVersionV2)
			}
			request := create.SaveX(ctx)
			service := NewService(client, nil, nil, nil)
			updated, err := service.Cancel(ctx, requester.ID, request.ID)
			if err != nil || updated.Status != quotaresetrequest.StatusCancelled {
				t.Fatalf("Cancel(v%d) = %+v, %v", workflowVersion, updated, err)
			}
			if count := client.QuotaResetRequestEvent.Query().
				Where(
					quotaresetrequestevent.RequestIDEQ(request.ID),
					quotaresetrequestevent.EventTypeEQ(quotaresetrequestevent.EventTypeCancelled),
				).
				CountX(ctx); count != 1 {
				t.Fatalf("Cancel(v%d) cancelled event count = %d, want 1", workflowVersion, count)
			}
		})
	}
}

func TestCancelRollsBackWhenAuditEventFails(t *testing.T) {
	fixture := newWorkflowDecisionFixture(t, []workflowNodeFixture{{}})
	injectedErr := errors.New("injected cancelled event failure")
	fixture.client.QuotaResetRequestEvent.Use(func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, mutation ent.Mutation) (ent.Value, error) {
			eventMutation, ok := mutation.(*ent.QuotaResetRequestEventMutation)
			if ok && mutation.Op().Is(ent.OpCreate) {
				if eventType, exists := eventMutation.EventType(); exists && eventType == quotaresetrequestevent.EventTypeCancelled {
					return nil, injectedErr
				}
			}
			return next.Mutate(ctx, mutation)
		})
	})

	_, err := fixture.service.Cancel(fixture.ctx, fixture.requester.ID, fixture.request.ID)
	if !errors.Is(err, injectedErr) {
		t.Fatalf("Cancel() error = %v, want injected event failure", err)
	}
	request := fixture.client.QuotaResetRequest.GetX(fixture.ctx, fixture.request.ID)
	if request.Status != quotaresetrequest.StatusPending || request.CurrentNodeID == nil || *request.CurrentNodeID != fixture.nodes[0].ID {
		t.Fatalf("request after failed cancellation = %s/%v, want pending/current node %d", request.Status, request.CurrentNodeID, fixture.nodes[0].ID)
	}
	if node := fixture.client.QuotaResetRequestNode.GetX(fixture.ctx, fixture.nodes[0].ID); node.Status != quotaresetrequestnode.StatusActive {
		t.Fatalf("active node status after failed cancellation = %s, want active", node.Status)
	}
	if count := fixture.client.QuotaResetRequestEvent.Query().
		Where(
			quotaresetrequestevent.RequestIDEQ(fixture.request.ID),
			quotaresetrequestevent.EventTypeEQ(quotaresetrequestevent.EventTypeCancelled),
		).
		CountX(fixture.ctx); count != 0 {
		t.Fatalf("cancelled event count after failed cancellation = %d, want 0", count)
	}
}

func TestCancelPreservesActiveNodeEvidence(t *testing.T) {
	fixture := newWorkflowDecisionFixture(t, []workflowNodeFixture{{}})

	updated, err := fixture.service.Cancel(fixture.ctx, fixture.requester.ID, fixture.request.ID)
	if err != nil {
		t.Fatalf("Cancel() error = %v", err)
	}
	if updated.Status != quotaresetrequest.StatusCancelled || updated.CurrentNodeID == nil || *updated.CurrentNodeID != fixture.nodes[0].ID {
		t.Fatalf("cancelled request = %s/%v, want cancelled/current node %d", updated.Status, updated.CurrentNodeID, fixture.nodes[0].ID)
	}
	if node := fixture.client.QuotaResetRequestNode.GetX(fixture.ctx, fixture.nodes[0].ID); node.Status != quotaresetrequestnode.StatusActive {
		t.Fatalf("cancelled request active-node evidence = %s, want active", node.Status)
	}
}

func TestCancelReturnsVersionedStaleError(t *testing.T) {
	for _, workflowVersion := range []int{1, WorkflowVersionV2} {
		t.Run(quotaresetWorkflowVersionName(workflowVersion), func(t *testing.T) {
			ctx := context.Background()
			client := testdb.Open(t)
			requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
			provider := createQuotaResetRelayProvider(t, ctx, client)
			create := client.QuotaResetRequest.Create().
				SetRequesterUserID(requester.ID).
				SetRequesterRelayUserID(1001).
				SetProviderID(provider.ID).
				SetGroupID("42").
				SetGroupName("Group Alpha").
				SetGroupPlatform("openai").
				SetReason("Need a reset for a build investigation").
				SetStatus(quotaresetrequest.StatusRejected).
				SetResolvedApproverUserIds([]int{}).
				SetMatchedDepartmentPaths([]map[string]any{})
			if workflowVersion == WorkflowVersionV2 {
				create.SetWorkflowVersion(WorkflowVersionV2)
			}
			request := create.SaveX(ctx)
			service := NewService(client, nil, nil, nil)

			_, err := service.Cancel(ctx, requester.ID, request.ID)
			if workflowVersion == WorkflowVersionV2 {
				var advanced *WorkflowAdvancedError
				if !errors.As(err, &advanced) || advanced.RequestID != request.ID || !errors.Is(err, ErrWorkflowAdvanced) {
					t.Fatalf("Cancel(v2 stale) error = %v, want WorkflowAdvancedError for request %d", err, request.ID)
				}
				latest := requireWorkflowAdvancedLatest(t, advanced)
				if latest.ID != request.ID || latest.Status != quotaresetrequest.StatusRejected.String() || latest.Workflow == nil || latest.Workflow.CanCancel {
					t.Fatalf("Cancel(v2 stale) latest summary = %+v", latest)
				}
				return
			}
			if !errors.Is(err, ErrInvalidStatus) {
				t.Fatalf("Cancel(v1 stale) error = %v, want ErrInvalidStatus", err)
			}
		})
	}
}

func TestWorkflowAdvancedSummaryUsesAdminViewer(t *testing.T) {
	fixture := newWorkflowDecisionFixture(t, []workflowNodeFixture{{}, {}})

	_, err := fixture.service.Approve(fixture.ctx, DecisionInput{
		ActorUserID:    fixture.admin.ID,
		RequestID:      fixture.request.ID,
		RequestNodeID:  fixture.nodes[1].ID,
		DecisionReason: "Approved the wrong node",
		Admin:          true,
	})
	var advanced *WorkflowAdvancedError
	if !errors.As(err, &advanced) || !errors.Is(err, ErrWorkflowAdvanced) {
		t.Fatalf("Approve(stale admin) error = %v, want WorkflowAdvancedError", err)
	}
	latest := requireWorkflowAdvancedLatest(t, advanced)
	if latest.Workflow == nil || latest.Workflow.CurrentNode == nil || latest.Workflow.CurrentNode.ID != fixture.nodes[0].ID || !latest.Workflow.CanApprove || !latest.Workflow.CanReject {
		t.Fatalf("Approve(stale admin) latest summary = %+v", latest)
	}
}

func requireWorkflowAdvancedLatest(t *testing.T, advanced *WorkflowAdvancedError) *RequestSummary {
	t.Helper()
	latestField := reflect.ValueOf(advanced).Elem().FieldByName("Latest")
	if !latestField.IsValid() {
		t.Fatal("WorkflowAdvancedError does not expose Latest")
	}
	if latestField.IsNil() {
		t.Fatal("WorkflowAdvancedError.Latest is nil")
	}
	latest, ok := latestField.Interface().(*RequestSummary)
	if !ok {
		t.Fatalf("WorkflowAdvancedError.Latest type = %T, want *RequestSummary", latestField.Interface())
	}
	return latest
}

type workflowCreationFixture struct {
	ctx               context.Context
	client            *ent.Client
	service           *Service
	provider          *fakeQuotaResetProvider
	providerRow       *ent.RelayProvider
	source            *ent.DirectorySource
	requester         *ent.User
	actorA            *ent.User
	actorB            *ent.User
	initialDepartment *ent.DirectoryDepartment
}

func newWorkflowCreationFixture(t *testing.T, initialApprover, configuredNode bool) *workflowCreationFixture {
	t.Helper()
	ctx := context.Background()
	client := testdb.Open(t)
	source := createQuotaResetDirectorySource(t, ctx, client)
	initialDepartment := createQuotaResetDepartment(t, ctx, client, source.ID, "department-alpha", "Department Alpha", nil)
	chainDepartment := createQuotaResetDepartment(t, ctx, client, source.ID, "department-beta", "Department Beta", nil)
	requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
	actorA := createQuotaResetUser(t, ctx, client, "approver-alpha", "approver-alpha@example.com", nil, "user")
	actorB := createQuotaResetUser(t, ctx, client, "approver-beta", "approver-beta@example.org", nil, "user")
	requesterMember := createQuotaResetMember(t, ctx, client, source.ID, "member-alice", requester.Email, initialDepartment.ExternalID, &requester.ID)
	actorAMember := createQuotaResetMember(t, ctx, client, source.ID, "member-approver-alpha", actorA.Email, initialDepartment.ExternalID, &actorA.ID)
	actorBMember := createQuotaResetMember(t, ctx, client, source.ID, "member-approver-beta", actorB.Email, chainDepartment.ExternalID, &actorB.ID)
	createQuotaResetMemberDepartment(t, ctx, client, source.ID, requesterMember, initialDepartment.ExternalID)
	createQuotaResetMemberDepartment(t, ctx, client, source.ID, actorAMember, initialDepartment.ExternalID)
	createQuotaResetMemberDepartment(t, ctx, client, source.ID, actorBMember, chainDepartment.ExternalID)
	if initialApprover {
		createQuotaResetApproverConfig(t, ctx, client, source.ID, initialDepartment.ExternalID, initialDepartment.Name, actorA.ID)
	}
	providerRow := createQuotaResetRelayProvider(t, ctx, client)
	if configuredNode {
		createQuotaResetApproverConfig(t, ctx, client, source.ID, chainDepartment.ExternalID, chainDepartment.Name, actorB.ID)
		createWorkflowChain(t, ctx, client, providerRow.ID, "42", source.ID, chainDepartment)
	}
	provider := &fakeQuotaResetProvider{subscriptions: []relay.UserSubscription{activeQuotaResetSubscription(42, "Group Alpha")}}
	return &workflowCreationFixture{
		ctx:               ctx,
		client:            client,
		service:           NewService(client, fakeProviderResolver(providerRow.ID, provider), NewApproverResolver(client), nil),
		provider:          provider,
		providerRow:       providerRow,
		source:            source,
		requester:         requester,
		actorA:            actorA,
		actorB:            actorB,
		initialDepartment: initialDepartment,
	}
}

type workflowNodeFixture struct {
	approverIDs   []int
	adminFallback bool
}

type workflowDecisionFixture struct {
	ctx       context.Context
	client    *ent.Client
	service   *Service
	provider  *fakeQuotaResetProvider
	request   *ent.QuotaResetRequest
	nodes     []*ent.QuotaResetRequestNode
	requester *ent.User
	actorA    *ent.User
	actorB    *ent.User
	admin     *ent.User
}

func newWorkflowDecisionFixture(t *testing.T, specs []workflowNodeFixture) *workflowDecisionFixture {
	t.Helper()
	ctx := context.Background()
	client := testdb.Open(t)
	requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
	actorA := createQuotaResetUser(t, ctx, client, "approver-alpha", "approver-alpha@example.com", nil, "user")
	actorB := createQuotaResetUser(t, ctx, client, "approver-beta", "approver-beta@example.org", nil, "user")
	admin := createQuotaResetUser(t, ctx, client, "admin", "admin@example.com", nil, "admin")
	providerRow := createQuotaResetRelayProvider(t, ctx, client)
	request := client.QuotaResetRequest.Create().
		SetRequesterUserID(requester.ID).
		SetRequesterRelayUserID(1001).
		SetProviderID(providerRow.ID).
		SetGroupID("42").
		SetGroupName("Group Alpha").
		SetGroupPlatform("openai").
		SetReason("Need a reset for a build investigation").
		SetWorkflowVersion(WorkflowVersionV2).
		SetRequesterDisplayNameSnapshot("Alice Example").
		SetRequesterEmailSnapshot(requester.Email).
		SetRequesterDepartmentPaths([]string{"Department Alpha"}).
		SetRequesterNotificationIds(map[string]string{}).
		SetResolvedApproverUserIds([]int{}).
		SetMatchedDepartmentPaths([]map[string]any{}).
		SaveX(ctx)
	nodes := make([]*ent.QuotaResetRequestNode, 0, len(specs))
	for position, spec := range specs {
		status := quotaresetrequestnode.StatusQueued
		if position == 0 {
			status = quotaresetrequestnode.StatusActive
		}
		node := client.QuotaResetRequestNode.Create().
			SetRequestID(request.ID).
			SetPosition(position).
			SetNodeType(quotaresetrequestnode.NodeTypeConfiguredDepartment).
			SetLabel("Department review").
			SetDepartmentSnapshots([]map[string]any{{"external_id": "department-review", "display_path": "Department Review", "resolution": "configured"}}).
			SetStatus(status).
			SetAdminFallbackRequired(spec.adminFallback).
			SaveX(ctx)
		nodes = append(nodes, node)
		for _, approverID := range spec.approverIDs {
			createWorkflowNodeApprover(t, ctx, client, node.ID, approverID)
		}
	}
	request = client.QuotaResetRequest.UpdateOneID(request.ID).SetCurrentNodeID(nodes[0].ID).SaveX(ctx)
	provider := &fakeQuotaResetProvider{subscriptions: []relay.UserSubscription{activeQuotaResetSubscription(42, "Group Alpha")}}
	return &workflowDecisionFixture{
		ctx:       ctx,
		client:    client,
		service:   NewService(client, fakeProviderResolver(providerRow.ID, provider), NewApproverResolver(client), nil),
		provider:  provider,
		request:   request,
		nodes:     nodes,
		requester: requester,
		actorA:    actorA,
		actorB:    actorB,
		admin:     admin,
	}
}

func (f *workflowDecisionFixture) replaceApproverIDs(t *testing.T, nodeIndex int, approverIDs ...int) {
	t.Helper()
	f.client.QuotaResetRequestNodeApprover.Delete().
		Where(quotaresetrequestnodeapprover.RequestNodeIDEQ(f.nodes[nodeIndex].ID)).
		ExecX(f.ctx)
	for _, approverID := range approverIDs {
		createWorkflowNodeApprover(t, f.ctx, f.client, f.nodes[nodeIndex].ID, approverID)
	}
}

func newFailedWorkflowRetryFixture(t *testing.T) *workflowDecisionFixture {
	t.Helper()
	fixture := newWorkflowDecisionFixture(t, []workflowNodeFixture{{}})
	fixture.replaceApproverIDs(t, 0, fixture.actorA.ID)
	decision := fixture.client.QuotaResetRequestDecision.Create().
		SetRequestID(fixture.request.ID).
		SetRequestNodeID(fixture.nodes[0].ID).
		SetActorUserID(fixture.actorA.ID).
		SetActorDisplayName(fixture.actorA.Username).
		SetDecision(quotaresetrequestdecision.DecisionApprove).
		SetComment("Approved before reset failure").
		SaveX(fixture.ctx)
	fixture.client.QuotaResetRequestNode.UpdateOneID(fixture.nodes[0].ID).
		SetStatus(quotaresetrequestnode.StatusApproved).
		SetSatisfiedByDecisionID(decision.ID).
		SaveX(fixture.ctx)
	fixture.request = fixture.client.QuotaResetRequest.UpdateOneID(fixture.request.ID).
		SetStatus(quotaresetrequest.StatusApprovedResetFailed).
		SetWorkflowCompletedByDecisionID(decision.ID).
		ClearCurrentNodeID().
		SaveX(fixture.ctx)
	return fixture
}

func createWorkflowNodeApprover(t *testing.T, ctx context.Context, client *ent.Client, nodeID, userID int) {
	t.Helper()
	user := client.User.GetX(ctx, userID)
	client.QuotaResetRequestNodeApprover.Create().
		SetRequestNodeID(nodeID).
		SetUserID(user.ID).
		SetDisplayName(user.Username).
		SetEmail(user.Email).
		SetSource(quotaresetrequestnodeapprover.SourceConfigured).
		SetSourceDepartmentExternalIds([]string{"department-review"}).
		SetNotificationIds(map[string]string{}).
		SaveX(ctx)
}

func workflowRequestNodes(t *testing.T, ctx context.Context, client *ent.Client, requestID int) []*ent.QuotaResetRequestNode {
	t.Helper()
	return client.QuotaResetRequestNode.Query().
		Where(quotaresetrequestnode.RequestIDEQ(requestID)).
		Order(ent.Asc(quotaresetrequestnode.FieldPosition)).
		AllX(ctx)
}

func workflowApproverSnapshots(t *testing.T, ctx context.Context, client *ent.Client, requestID int) []*ent.QuotaResetRequestNodeApprover {
	t.Helper()
	nodes := workflowRequestNodes(t, ctx, client, requestID)
	nodeIDs := make([]int, 0, len(nodes))
	for _, node := range nodes {
		nodeIDs = append(nodeIDs, node.ID)
	}
	return client.QuotaResetRequestNodeApprover.Query().
		Where(quotaresetrequestnodeapprover.RequestNodeIDIn(nodeIDs...)).
		Order(ent.Asc(quotaresetrequestnodeapprover.FieldRequestNodeID), ent.Asc(quotaresetrequestnodeapprover.FieldUserID)).
		AllX(ctx)
}

func assertWorkflowNodeApprovers(t *testing.T, ctx context.Context, client *ent.Client, nodeID int, want ...int) {
	t.Helper()
	rows := client.QuotaResetRequestNodeApprover.Query().
		Where(quotaresetrequestnodeapprover.RequestNodeIDEQ(nodeID)).
		Order(ent.Asc(quotaresetrequestnodeapprover.FieldUserID)).
		AllX(ctx)
	got := make([]int, 0, len(rows))
	for _, row := range rows {
		got = append(got, row.UserID)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("node %d approvers = %#v, want %#v", nodeID, got, want)
	}
}

func assertWorkflowEventCount(t *testing.T, ctx context.Context, client *ent.Client, requestID int, eventType quotaresetrequestevent.EventType, want int) {
	t.Helper()
	got := client.QuotaResetRequestEvent.Query().
		Where(quotaresetrequestevent.RequestIDEQ(requestID), quotaresetrequestevent.EventTypeEQ(eventType)).
		CountX(ctx)
	if got != want {
		t.Fatalf("%s event count = %d, want %d", eventType, got, want)
	}
}

func quotaresetWorkflowVersionName(version int) string {
	if version == WorkflowVersionV2 {
		return "v2"
	}
	return "v1"
}

func mustWorkflowTestJSON(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal workflow test value: %v", err)
	}
	return string(data)
}
