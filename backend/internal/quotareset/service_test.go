package quotareset

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	entsql "entgo.io/ent/dialect/sql"

	"github.com/ai-efficiency/backend/ent"
	entcredential "github.com/ai-efficiency/backend/ent/credential"
	"github.com/ai-efficiency/backend/ent/quotaresetrequest"
	"github.com/ai-efficiency/backend/ent/quotaresetrequestevent"
	entuser "github.com/ai-efficiency/backend/ent/user"
	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/ai-efficiency/backend/internal/testdb"
)

func TestCreateRequestRequiresActiveSubscriptionAndReason(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
	provider := createQuotaResetRelayProvider(t, ctx, client)
	fake := &fakeQuotaResetProvider{subscriptions: []relay.UserSubscription{activeQuotaResetSubscription(42, "Group Alpha")}}
	svc := NewService(client, fakeProviderResolver(provider.ID, fake), NewApproverResolver(client), nil)

	req, err := svc.CreateRequest(ctx, CreateRequestInput{RequesterUserID: requester.ID, GroupID: "42", Reason: "Need reset for a build investigation"})
	if err != nil {
		t.Fatalf("CreateRequest() error = %v", err)
	}
	if req.Status.String() != "workflow_pending" || req.RequesterRelayUserID != 1001 || req.GroupID != "42" {
		t.Fatalf("request = %+v", req)
	}
	if count := client.QuotaResetRequestEvent.Query().CountX(ctx); count != 3 {
		t.Fatalf("event count = %d, want created, approver_resolved, and workflow_created", count)
	}
}

func TestCreateRequestRejectsDuplicateActiveRequest(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
	provider := createQuotaResetRelayProvider(t, ctx, client)
	fake := &fakeQuotaResetProvider{subscriptions: []relay.UserSubscription{activeQuotaResetSubscription(42, "Group Alpha")}}
	svc := NewService(client, fakeProviderResolver(provider.ID, fake), NewApproverResolver(client), nil)
	_, err := svc.CreateRequest(ctx, CreateRequestInput{RequesterUserID: requester.ID, GroupID: "42", Reason: "First request"})
	if err != nil {
		t.Fatalf("first CreateRequest() error = %v", err)
	}
	_, err = svc.CreateRequest(ctx, CreateRequestInput{RequesterUserID: requester.ID, GroupID: "42", Reason: "Second request"})
	if !errors.Is(err, ErrActiveRequestExists) {
		t.Fatalf("second CreateRequest() error = %v, want ErrActiveRequestExists", err)
	}
}

func TestCreateWorkflowRequestRollsBackBeforeDuplicateDetection(t *testing.T) {
	ctx := context.Background()
	seedClient, dsn := testdb.OpenWithDSN(t)
	requester := createQuotaResetUser(t, ctx, seedClient, "alice", "alice@example.com", intPtr(1001), "user")
	provider := createQuotaResetRelayProvider(t, ctx, seedClient)
	createPendingQuotaResetRequest(t, ctx, seedClient, requester.ID, 1001, provider.ID, "42", nil)

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open single-connection database: %v", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	singleConnectionClient := ent.NewClient(ent.Driver(entsql.OpenDB("postgres", db)))
	t.Cleanup(func() { _ = singleConnectionClient.Close() })

	workflow := &Workflow{
		Version:     workflowVersionV2,
		CurrentStep: 0,
		Requester:   WorkflowPerson{UserID: requester.ID},
		Steps:       []WorkflowStep{adminFallbackWorkflowStep()},
	}
	workflow.Steps[0].Status = WorkflowStepActive
	requestCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()

	_, err = NewService(singleConnectionClient, nil, nil, nil).createWorkflowRequest(
		requestCtx,
		requester,
		provider,
		activeQuotaResetSubscription(42, "Group Alpha"),
		CreateRequestInput{RequesterUserID: requester.ID, GroupID: "42", Reason: "Duplicate request"},
		workflow,
		nil,
	)
	if !errors.Is(err, ErrActiveRequestExists) {
		t.Fatalf("createWorkflowRequest() error = %v, want ErrActiveRequestExists", err)
	}
}

func TestQuotaResetRequestActiveUniquenessIsEnforcedByDatabase(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
	provider := createQuotaResetRelayProvider(t, ctx, client)
	createPendingQuotaResetRequest(t, ctx, client, requester.ID, 1001, provider.ID, "42", nil)

	_, err := client.QuotaResetRequest.Create().
		SetRequesterUserID(requester.ID).
		SetRequesterRelayUserID(1001).
		SetProviderID(provider.ID).
		SetGroupID("42").
		SetGroupName("Group Alpha").
		SetGroupPlatform("openai").
		SetReason("Duplicate active request").
		SetResolvedApproverUserIds([]int{}).
		SetMatchedDepartmentPaths([]map[string]any{}).
		Save(ctx)
	if !ent.IsConstraintError(err) {
		t.Fatalf("duplicate active request error = %v, want constraint error", err)
	}

	for _, status := range []quotaresetrequest.Status{
		quotaresetrequest.StatusApprovedResetSucceeded,
		quotaresetrequest.StatusRejected,
	} {
		if _, err := client.QuotaResetRequest.Create().
			SetRequesterUserID(requester.ID).
			SetRequesterRelayUserID(1001).
			SetProviderID(provider.ID).
			SetGroupID("42").
			SetGroupName("Group Alpha").
			SetGroupPlatform("openai").
			SetReason("Historical terminal request").
			SetStatus(status).
			SetResolvedApproverUserIds([]int{}).
			SetMatchedDepartmentPaths([]map[string]any{}).
			Save(ctx); err != nil {
			t.Fatalf("terminal duplicate status %s error = %v, want nil", status, err)
		}
	}
}

func TestApproverApproveExecutesResetAndWritesEvents(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
	approver := createQuotaResetUser(t, ctx, client, "lead", "lead@example.com", nil, "user")
	provider := createQuotaResetRelayProvider(t, ctx, client)
	request := createPendingQuotaResetRequest(t, ctx, client, requester.ID, 1001, provider.ID, "42", []int{approver.ID})
	fake := &fakeQuotaResetProvider{subscriptions: []relay.UserSubscription{activeQuotaResetSubscription(42, "Group Alpha")}}
	svc := NewService(client, fakeProviderResolver(provider.ID, fake), NewApproverResolver(client), nil)

	updated, err := svc.Approve(ctx, DecisionInput{ActorUserID: approver.ID, RequestID: request.ID, DecisionReason: "Approved"})
	if err != nil {
		t.Fatalf("Approve() error = %v", err)
	}
	if updated.Status.String() != "approved_reset_succeeded" {
		t.Fatalf("status = %s, want approved_reset_succeeded", updated.Status)
	}
	if fake.resetUserID != 1001 || fake.resetGroupID != 42 {
		t.Fatalf("reset call = %d/%d, want 1001/42", fake.resetUserID, fake.resetGroupID)
	}
}

func TestApproveV1RequiresComment(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
	approver := createQuotaResetUser(t, ctx, client, "lead", "lead@example.org", nil, "user")
	provider := createQuotaResetRelayProvider(t, ctx, client)
	request := createPendingQuotaResetRequest(t, ctx, client, requester.ID, 1001, provider.ID, "42", []int{approver.ID})

	_, err := NewService(client, nil, nil, nil).Approve(ctx, DecisionInput{ActorUserID: approver.ID, RequestID: request.ID})
	if !errors.Is(err, ErrDecisionRequired) {
		t.Fatalf("Approve() error = %v, want ErrDecisionRequired", err)
	}
}

func TestCreateRequestV2SnapshotsWorkflowAndEvents(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
	provider := createQuotaResetRelayProvider(t, ctx, client)
	fake := &fakeQuotaResetProvider{subscriptions: []relay.UserSubscription{activeQuotaResetSubscription(42, "Group Alpha")}}
	svc := NewService(client, fakeProviderResolver(provider.ID, fake), NewApproverResolver(client), nil)

	request, err := svc.CreateRequest(ctx, CreateRequestInput{RequesterUserID: requester.ID, GroupID: "42", Reason: "Need reset for a build investigation"})
	if err != nil {
		t.Fatalf("CreateRequest() error = %v", err)
	}
	if request.WorkflowVersion != workflowVersionV2 || request.WorkflowRevision != 0 {
		t.Fatalf("workflow version/revision = %d/%d", request.WorkflowVersion, request.WorkflowRevision)
	}
	if request.Status.String() != "workflow_pending" {
		t.Fatalf("status = %s, want rollout-safe workflow_pending", request.Status)
	}
	workflow, err := DecodeWorkflow(request.Workflow)
	if err != nil {
		t.Fatalf("DecodeWorkflow() error = %v", err)
	}
	if len(workflow.Steps) != 1 || !workflow.Steps[0].AdminFallback || workflow.Steps[0].Status != WorkflowStepActive {
		t.Fatalf("workflow steps = %#v, want active admin fallback", workflow.Steps)
	}
	if count := client.QuotaResetRequestEvent.Query().Where(quotaresetrequestevent.RequestIDEQ(request.ID)).CountX(ctx); count != 3 {
		t.Fatalf("event count = %d, want created, approver_resolved, workflow_created", count)
	}
}

func TestCreateRequestV2IgnoresGroupForRouting(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	source := createQuotaResetDirectorySource(t, ctx, client)
	parent := createQuotaResetDepartment(t, ctx, client, source.ID, "dept-parent", "Parent", nil)
	exact := createQuotaResetDepartment(t, ctx, client, source.ID, "dept-exact", "Exact", &parent.ExternalID)
	requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
	approver := createQuotaResetUser(t, ctx, client, "bob", "bob@example.org", nil, "user")
	requesterMember := createQuotaResetMember(t, ctx, client, source.ID, "member-alice", requester.Email, exact.ExternalID, &requester.ID)
	createQuotaResetMemberDepartment(t, ctx, client, source.ID, requesterMember, exact.ExternalID)
	approverMember := createQuotaResetMemberInDepartment(t, ctx, client, source.ID, "member-bob", approver, exact.ExternalID)
	createQuotaResetMemberDepartment(t, ctx, client, source.ID, approverMember, parent.ExternalID)
	createQuotaResetApproverConfig(t, ctx, client, source.ID, exact.ExternalID, exact.Name, approver.ID)
	createQuotaResetApproverConfig(t, ctx, client, source.ID, parent.ExternalID, parent.Name, approver.ID)
	provider := createQuotaResetRelayProvider(t, ctx, client)
	fake := &fakeQuotaResetProvider{subscriptions: []relay.UserSubscription{
		activeQuotaResetSubscription(42, "Group Alpha"),
		activeQuotaResetSubscription(43, "Group Beta"),
	}}
	svc := NewService(client, fakeProviderResolver(provider.ID, fake), NewApproverResolver(client), nil)

	alphaRequest, err := svc.CreateRequest(ctx, CreateRequestInput{RequesterUserID: requester.ID, GroupID: "42", Reason: "Need reset for Group Alpha"})
	if err != nil {
		t.Fatalf("CreateRequest(Group Alpha) error = %v", err)
	}
	betaRequest, err := svc.CreateRequest(ctx, CreateRequestInput{RequesterUserID: requester.ID, GroupID: "43", Reason: "Need reset for Group Beta"})
	if err != nil {
		t.Fatalf("CreateRequest(Group Beta) error = %v", err)
	}
	alphaWorkflow, err := DecodeWorkflow(alphaRequest.Workflow)
	if err != nil {
		t.Fatalf("DecodeWorkflow(Group Alpha) error = %v", err)
	}
	betaWorkflow, err := DecodeWorkflow(betaRequest.Workflow)
	if err != nil {
		t.Fatalf("DecodeWorkflow(Group Beta) error = %v", err)
	}
	if !reflect.DeepEqual(alphaWorkflow, betaWorkflow) {
		t.Fatalf("group-specific workflows differ:\nalpha=%#v\nbeta=%#v", alphaWorkflow, betaWorkflow)
	}
}

func TestApproveV2SatisfiesDerivedLaterStep(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	source := createQuotaResetDirectorySource(t, ctx, client)
	parent := createQuotaResetDepartment(t, ctx, client, source.ID, "dept-parent", "Parent", nil)
	exact := createQuotaResetDepartment(t, ctx, client, source.ID, "dept-exact", "Exact", &parent.ExternalID)
	requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
	approver := createQuotaResetUser(t, ctx, client, "bob", "bob@example.org", nil, "user")
	requesterMember := createQuotaResetMember(t, ctx, client, source.ID, "member-alice", requester.Email, exact.ExternalID, &requester.ID)
	createQuotaResetMemberDepartment(t, ctx, client, source.ID, requesterMember, exact.ExternalID)
	approverMember := createQuotaResetMemberInDepartment(t, ctx, client, source.ID, "member-bob", approver, exact.ExternalID)
	createQuotaResetMemberDepartment(t, ctx, client, source.ID, approverMember, parent.ExternalID)
	createQuotaResetApproverConfig(t, ctx, client, source.ID, exact.ExternalID, exact.Name, approver.ID)
	createQuotaResetApproverConfig(t, ctx, client, source.ID, parent.ExternalID, parent.Name, approver.ID)
	provider := createQuotaResetRelayProvider(t, ctx, client)
	fake := &fakeQuotaResetProvider{subscriptions: []relay.UserSubscription{activeQuotaResetSubscription(42, "Group Alpha")}}
	recorder := &recordingQuotaResetNotifier{}
	svc := NewService(client, fakeProviderResolver(provider.ID, fake), NewApproverResolver(client), recorder)
	request, err := svc.CreateRequest(ctx, CreateRequestInput{RequesterUserID: requester.ID, GroupID: "42", Reason: "Need reset for a build investigation"})
	if err != nil {
		t.Fatalf("CreateRequest() error = %v", err)
	}

	completed, err := svc.Approve(ctx, DecisionInput{ActorUserID: approver.ID, RequestID: request.ID, DecisionReason: "Approved for reset"})
	if err != nil {
		t.Fatalf("Approve() error = %v", err)
	}
	if completed.Status != quotaresetrequest.StatusApprovedResetSucceeded || fake.resetCalls != 1 {
		t.Fatalf("status/reset calls = %s/%d, want approved_reset_succeeded/1", completed.Status, fake.resetCalls)
	}
	if containsString(recorder.events, "quota_reset_step_activated") {
		t.Fatalf("notification events = %v, auto-satisfied step must not activate", recorder.events)
	}
	event := client.QuotaResetRequestEvent.Query().Where(
		quotaresetrequestevent.RequestIDEQ(request.ID),
		quotaresetrequestevent.EventTypeEQ(quotaresetrequestevent.EventTypeStepSatisfied),
	).OnlyX(ctx)
	if event.ActorUserID == nil || *event.ActorUserID != approver.ID {
		t.Fatalf("step_satisfied actor = %v, want %d", event.ActorUserID, approver.ID)
	}
	if got := int(event.Metadata["satisfied_by_step"].(float64)); got != 0 {
		t.Fatalf("satisfied_by_step = %d, want 0", got)
	}
}

func TestApproveV2AdvancesThenFinalApprovalResetsOnce(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
	firstApprover := createQuotaResetUser(t, ctx, client, "bob", "bob@example.org", nil, "user")
	finalApprover := createQuotaResetUser(t, ctx, client, "carol", "carol@example.org", nil, "user")
	provider := createQuotaResetRelayProvider(t, ctx, client)
	request := createPendingWorkflowRequest(t, ctx, client, requester, provider, workflowFixtureForUsers(requester.ID, firstApprover.ID, finalApprover.ID, false))
	fake := &fakeQuotaResetProvider{subscriptions: []relay.UserSubscription{activeQuotaResetSubscription(42, "Group Alpha")}}
	svc := NewService(client, fakeProviderResolver(provider.ID, fake), nil, nil)

	intermediate, err := svc.Approve(ctx, DecisionInput{ActorUserID: firstApprover.ID, RequestID: request.ID, DecisionReason: "额度异常，确认重置"})
	if err != nil {
		t.Fatalf("first Approve() error = %v", err)
	}
	if intermediate.Status != quotaresetrequest.StatusWorkflowPending || intermediate.WorkflowRevision != 1 {
		t.Fatalf("intermediate status/revision = %s/%d", intermediate.Status, intermediate.WorkflowRevision)
	}
	if got, want := intermediate.ResolvedApproverUserIds, []int{finalApprover.ID}; !reflect.DeepEqual(got, want) {
		t.Fatalf("current approvers = %v, want %v", got, want)
	}
	if fake.resetCalls != 0 {
		t.Fatalf("reset calls after intermediate approval = %d", fake.resetCalls)
	}
	history, err := svc.ListApprovals(ctx, firstApprover.ID, ListParams{})
	if err != nil || history.Total != 1 || history.Items[0].ID != request.ID {
		t.Fatalf("processed history = %+v, error %v", history, err)
	}

	completed, err := svc.Approve(ctx, DecisionInput{ActorUserID: finalApprover.ID, RequestID: request.ID, DecisionReason: "同意最终重置"})
	if err != nil {
		t.Fatalf("final Approve() error = %v", err)
	}
	if completed.Status != quotaresetrequest.StatusApprovedResetSucceeded || fake.resetCalls != 1 {
		t.Fatalf("final status/reset calls = %s/%d", completed.Status, fake.resetCalls)
	}
}

func TestApproveV2ReusesPriorActorAndResetsWithoutDuplicateDecision(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
	approver := createQuotaResetUser(t, ctx, client, "bob", "bob@example.org", nil, "user")
	other := createQuotaResetUser(t, ctx, client, "carol", "carol@example.org", nil, "user")
	provider := createQuotaResetRelayProvider(t, ctx, client)
	request := createPendingWorkflowRequest(t, ctx, client, requester, provider, workflowFixtureForUsers(requester.ID, approver.ID, other.ID, true))
	fake := &fakeQuotaResetProvider{subscriptions: []relay.UserSubscription{activeQuotaResetSubscription(42, "Group Alpha")}}
	recorder := &recordingQuotaResetNotifier{}
	svc := NewService(client, fakeProviderResolver(provider.ID, fake), nil, recorder)

	completed, err := svc.Approve(ctx, DecisionInput{ActorUserID: approver.ID, RequestID: request.ID, DecisionReason: "同意全部匹配节点"})
	if err != nil {
		t.Fatalf("Approve() error = %v", err)
	}
	if completed.Status != quotaresetrequest.StatusApprovedResetSucceeded || fake.resetCalls != 1 {
		t.Fatalf("status/reset calls = %s/%d", completed.Status, fake.resetCalls)
	}
	stored := client.QuotaResetRequest.GetX(ctx, request.ID)
	workflow, err := DecodeWorkflow(stored.Workflow)
	if err != nil {
		t.Fatalf("DecodeWorkflow() error = %v", err)
	}
	if workflow.Steps[1].Status != WorkflowStepSatisfied || workflow.Steps[1].Decision != nil {
		t.Fatalf("reused step = %+v", workflow.Steps[1])
	}
	if containsString(recorder.events, "quota_reset_step_activated") {
		t.Fatalf("notification events = %v, auto-satisfied step must not activate", recorder.events)
	}
}

func TestApproveV2RecordsTheOriginalDecisionThatSatisfiedALaterStep(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
	bob := createQuotaResetUser(t, ctx, client, "bob", "bob@example.org", nil, "user")
	carol := createQuotaResetUser(t, ctx, client, "carol", "carol@example.org", nil, "user")
	provider := createQuotaResetRelayProvider(t, ctx, client)
	workflow := workflowFixtureForUsers(requester.ID, bob.ID, carol.ID, false)
	workflow.Steps = append(workflow.Steps, WorkflowStep{
		Kind:                  WorkflowStepConfiguredDepartment,
		Label:                 "Company / Security",
		DepartmentExternalIDs: []string{"dept-security"},
		Approvers:             []WorkflowApprover{{UserID: bob.ID, DisplayName: "bob", Email: bob.Email, Source: "configured"}},
		Status:                WorkflowStepQueued,
	})
	request := createPendingWorkflowRequest(t, ctx, client, requester, provider, workflow)
	fake := &fakeQuotaResetProvider{subscriptions: []relay.UserSubscription{activeQuotaResetSubscription(42, "Group Alpha")}}
	svc := NewService(client, fakeProviderResolver(provider.ID, fake), nil, nil)

	if _, err := svc.Approve(ctx, DecisionInput{ActorUserID: bob.ID, RequestID: request.ID, DecisionReason: "Initial approval"}); err != nil {
		t.Fatalf("first Approve() error = %v", err)
	}
	if _, err := svc.Approve(ctx, DecisionInput{ActorUserID: carol.ID, RequestID: request.ID, DecisionReason: "Second approval"}); err != nil {
		t.Fatalf("second Approve() error = %v", err)
	}
	event := client.QuotaResetRequestEvent.Query().Where(
		quotaresetrequestevent.RequestIDEQ(request.ID),
		quotaresetrequestevent.EventTypeEQ(quotaresetrequestevent.EventTypeStepSatisfied),
	).OnlyX(ctx)
	if event.ActorUserID == nil || *event.ActorUserID != bob.ID {
		t.Fatalf("step_satisfied actor = %v, want bob %d", event.ActorUserID, bob.ID)
	}
	if got := int(event.Metadata["satisfied_by_step"].(float64)); got != 0 {
		t.Fatalf("satisfied_by_step = %d, want 0", got)
	}
}

func TestApproveV2RequiresComment(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
	approver := createQuotaResetUser(t, ctx, client, "bob", "bob@example.org", nil, "user")
	other := createQuotaResetUser(t, ctx, client, "carol", "carol@example.org", nil, "user")
	provider := createQuotaResetRelayProvider(t, ctx, client)
	request := createPendingWorkflowRequest(t, ctx, client, requester, provider, workflowFixtureForUsers(requester.ID, approver.ID, other.ID, false))

	_, err := NewService(client, nil, nil, nil).Approve(ctx, DecisionInput{ActorUserID: approver.ID, RequestID: request.ID})
	if !errors.Is(err, ErrDecisionRequired) {
		t.Fatalf("Approve() error = %v, want ErrDecisionRequired", err)
	}
}

func TestApproveRejectsUnknownWorkflowVersion(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
	admin := createQuotaResetUser(t, ctx, client, "admin", "admin@example.com", nil, "admin")
	provider := createQuotaResetRelayProvider(t, ctx, client)
	request := createPendingQuotaResetRequest(t, ctx, client, requester.ID, 1001, provider.ID, "42", []int{admin.ID})
	request = client.QuotaResetRequest.UpdateOneID(request.ID).SetWorkflowVersion(3).SaveX(ctx)
	fake := &fakeQuotaResetProvider{subscriptions: []relay.UserSubscription{activeQuotaResetSubscription(42, "Group Alpha")}}

	_, err := NewService(client, fakeProviderResolver(provider.ID, fake), nil, nil).Approve(ctx, DecisionInput{
		ActorUserID: admin.ID, RequestID: request.ID, DecisionReason: "Approved", Admin: true,
	})
	if !errors.Is(err, ErrInvalidWorkflow) {
		t.Fatalf("Approve() error = %v, want ErrInvalidWorkflow", err)
	}
	if fake.resetCalls != 0 {
		t.Fatalf("reset calls = %d, want 0", fake.resetCalls)
	}
}

func TestApproveV2RejectsTerminalWorkflowState(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
	approver := createQuotaResetUser(t, ctx, client, "bob", "bob@example.org", nil, "user")
	provider := createQuotaResetRelayProvider(t, ctx, client)
	workflow := workflowFixtureForUsers(requester.ID, approver.ID, approver.ID, false)
	workflow.CurrentStep = len(workflow.Steps)
	for index := range workflow.Steps {
		workflow.Steps[index].Status = WorkflowStepApproved
		workflow.Steps[index].Decision = &WorkflowDecision{
			ActorUserID: approver.ID,
			Comment:     "already approved",
			Approve:     true,
			DecidedAt:   time.Now().UTC(),
		}
	}
	request := createPendingWorkflowRequest(t, ctx, client, requester, provider, workflow)

	_, err := NewService(client, nil, nil, nil).Approve(ctx, DecisionInput{
		ActorUserID: approver.ID, RequestID: request.ID, DecisionReason: "approve",
	})
	if !errors.Is(err, ErrInvalidStatus) {
		t.Fatalf("Approve() error = %v, want ErrInvalidStatus", err)
	}
}

func TestRejectV2StoresCommentAndDoesNotReset(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
	approver := createQuotaResetUser(t, ctx, client, "bob", "bob@example.org", nil, "user")
	other := createQuotaResetUser(t, ctx, client, "carol", "carol@example.org", nil, "user")
	provider := createQuotaResetRelayProvider(t, ctx, client)
	request := createPendingWorkflowRequest(t, ctx, client, requester, provider, workflowFixtureForUsers(requester.ID, approver.ID, other.ID, false))
	fake := &fakeQuotaResetProvider{subscriptions: []relay.UserSubscription{activeQuotaResetSubscription(42, "Group Alpha")}}
	svc := NewService(client, fakeProviderResolver(provider.ID, fake), nil, nil)

	rejected, err := svc.Reject(ctx, DecisionInput{ActorUserID: approver.ID, RequestID: request.ID, DecisionReason: "申请信息不足"})
	if err != nil {
		t.Fatalf("Reject() error = %v", err)
	}
	if rejected.Status != quotaresetrequest.StatusRejected || rejected.DecisionReason != "申请信息不足" || fake.resetCalls != 0 {
		t.Fatalf("status/comment/reset calls = %s/%q/%d", rejected.Status, rejected.DecisionReason, fake.resetCalls)
	}
	workflow, err := DecodeWorkflow(rejected.Workflow)
	if err != nil {
		t.Fatalf("DecodeWorkflow() error = %v", err)
	}
	if workflow.Steps[0].Status != WorkflowStepRejected || workflow.Steps[0].Decision == nil || workflow.Steps[0].Decision.Comment != "申请信息不足" {
		t.Fatalf("rejected step = %+v", workflow.Steps[0])
	}
}

func TestConcurrentDecisionV2HasOneWinner(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
	first := createQuotaResetUser(t, ctx, client, "bob", "bob@example.org", nil, "user")
	second := createQuotaResetUser(t, ctx, client, "carol", "carol@example.org", nil, "user")
	provider := createQuotaResetRelayProvider(t, ctx, client)
	workflow := workflowFixtureForUsers(requester.ID, first.ID, second.ID, true)
	workflow.Steps = workflow.Steps[:1]
	workflow.Steps[0].Approvers = append(workflow.Steps[0].Approvers, WorkflowApprover{UserID: second.ID, DisplayName: second.Username, Email: second.Email, Source: "configured"})
	request := createPendingWorkflowRequest(t, ctx, client, requester, provider, workflow)
	fake := &fakeQuotaResetProvider{subscriptions: []relay.UserSubscription{activeQuotaResetSubscription(42, "Group Alpha")}}
	svc := NewService(client, fakeProviderResolver(provider.ID, fake), nil, nil)

	start := make(chan struct{})
	errorsByActor := make(chan error, 2)
	var wait sync.WaitGroup
	for _, actor := range []*ent.User{first, second} {
		wait.Add(1)
		go func(actor *ent.User) {
			defer wait.Done()
			<-start
			_, err := svc.Approve(ctx, DecisionInput{ActorUserID: actor.ID, RequestID: request.ID, DecisionReason: "concurrent approval"})
			errorsByActor <- err
		}(actor)
	}
	close(start)
	wait.Wait()
	close(errorsByActor)
	winners := 0
	losers := 0
	for err := range errorsByActor {
		if err == nil {
			winners++
		} else if errors.Is(err, ErrInvalidStatus) {
			losers++
		} else {
			t.Fatalf("concurrent Approve() error = %v", err)
		}
	}
	if winners != 1 || losers != 1 || fake.resetCalls != 1 {
		t.Fatalf("winners/losers/reset calls = %d/%d/%d", winners, losers, fake.resetCalls)
	}
}

func TestApproveStoresProviderResolutionFailureAsResetFailed(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	admin := client.User.Create().SetUsername("admin").SetEmail("admin@example.com").SetAuthSource("ldap").SetRole(entuser.RoleAdmin).SaveX(ctx)
	requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
	provider := createQuotaResetRelayProvider(t, ctx, client)
	request := createPendingQuotaResetRequest(t, ctx, client, requester.ID, 1001, provider.ID, "42", nil)
	svc := NewService(client, providerResolverFunc(func(context.Context, int) (relay.Provider, error) {
		return nil, errors.New("resolver unavailable")
	}), NewApproverResolver(client), nil)

	updated, err := svc.Approve(ctx, DecisionInput{ActorUserID: admin.ID, RequestID: request.ID, DecisionReason: "Approved", Admin: true})
	if err != nil {
		t.Fatalf("Approve() error = %v, want stored reset failure", err)
	}
	if updated.Status != quotaresetrequest.StatusApprovedResetFailed {
		t.Fatalf("status = %s, want approved_reset_failed", updated.Status)
	}
	if !strings.Contains(updated.ResetError, "resolver unavailable") {
		t.Fatalf("reset_error = %q, want resolver error", updated.ResetError)
	}
	if count := client.QuotaResetRequestEvent.Query().
		Where(quotaresetrequestevent.RequestIDEQ(request.ID), quotaresetrequestevent.EventTypeEQ(quotaresetrequestevent.EventTypeResetFailed)).
		CountX(ctx); count != 1 {
		t.Fatalf("reset_failed event count = %d, want 1", count)
	}
}

func TestApproveStoresUnsupportedResetterAsResetFailed(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	admin := client.User.Create().SetUsername("admin").SetEmail("admin@example.com").SetAuthSource("ldap").SetRole(entuser.RoleAdmin).SaveX(ctx)
	requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
	provider := createQuotaResetRelayProvider(t, ctx, client)
	request := createPendingQuotaResetRequest(t, ctx, client, requester.ID, 1001, provider.ID, "42", nil)
	svc := NewService(client, fakeProviderResolver(provider.ID, &listOnlyQuotaResetProvider{}), NewApproverResolver(client), nil)

	updated, err := svc.Approve(ctx, DecisionInput{ActorUserID: admin.ID, RequestID: request.ID, DecisionReason: "Approved", Admin: true})
	if err != nil {
		t.Fatalf("Approve() error = %v, want stored reset failure", err)
	}
	if updated.Status != quotaresetrequest.StatusApprovedResetFailed {
		t.Fatalf("status = %s, want approved_reset_failed", updated.Status)
	}
	if !strings.Contains(updated.ResetError, ErrProviderUnsupported.Error()) {
		t.Fatalf("reset_error = %q, want provider unsupported", updated.ResetError)
	}
}

func TestAdminCanApproveOwnRequestThroughAdminFallback(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	admin := createQuotaResetUser(t, ctx, client, "admin", "admin@example.com", intPtr(1001), "admin")
	provider := createQuotaResetRelayProvider(t, ctx, client)
	request := createPendingQuotaResetRequest(t, ctx, client, admin.ID, 1001, provider.ID, "42", []int{admin.ID})
	fake := &fakeQuotaResetProvider{subscriptions: []relay.UserSubscription{activeQuotaResetSubscription(42, "Group Alpha")}}
	svc := NewService(client, fakeProviderResolver(provider.ID, fake), NewApproverResolver(client), nil)

	updated, err := svc.Approve(ctx, DecisionInput{ActorUserID: admin.ID, RequestID: request.ID, DecisionReason: "Approved", Admin: true})
	if err != nil {
		t.Fatalf("admin self Approve() error = %v", err)
	}
	if updated.Status != quotaresetrequest.StatusApprovedResetSucceeded {
		t.Fatalf("status = %s, want approved_reset_succeeded", updated.Status)
	}
}

func TestListApprovalsFiltersByResolvedApprover(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	approver := createQuotaResetUser(t, ctx, client, "lead", "lead@example.com", nil, "user")
	otherApprover := createQuotaResetUser(t, ctx, client, "other-lead", "other-lead@example.com", nil, "user")
	requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
	provider := createQuotaResetRelayProvider(t, ctx, client)
	want := createPendingQuotaResetRequest(t, ctx, client, requester.ID, 1001, provider.ID, "42", []int{approver.ID})
	createPendingQuotaResetRequest(t, ctx, client, requester.ID, 1001, provider.ID, "43", []int{otherApprover.ID})
	svc := NewService(client, nil, nil, nil)

	resp, err := svc.ListApprovals(ctx, approver.ID, ListParams{})
	if err != nil {
		t.Fatalf("ListApprovals() error = %v", err)
	}
	if resp.Total != 1 || len(resp.Items) != 1 || resp.Items[0].ID != want.ID {
		t.Fatalf("ListApprovals() = total %d items %+v, want request %d only", resp.Total, resp.Items, want.ID)
	}
}

func TestResetFailureCanBeRetriedByAdmin(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	admin := client.User.Create().SetUsername("admin").SetEmail("admin@example.com").SetAuthSource("ldap").SetRole(entuser.RoleAdmin).SaveX(ctx)
	requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
	provider := createQuotaResetRelayProvider(t, ctx, client)
	request := createPendingQuotaResetRequest(t, ctx, client, requester.ID, 1001, provider.ID, "42", nil)
	fake := &fakeQuotaResetProvider{resetErr: errors.New("relay timeout")}
	svc := NewService(client, fakeProviderResolver(provider.ID, fake), NewApproverResolver(client), nil)
	_, err := svc.Approve(ctx, DecisionInput{ActorUserID: admin.ID, RequestID: request.ID, DecisionReason: "Approved", Admin: true})
	if err != nil {
		t.Fatalf("admin Approve() should store reset failure without returning error: %v", err)
	}
	fake.resetErr = nil
	updated, err := svc.RetryReset(ctx, DecisionInput{ActorUserID: admin.ID, RequestID: request.ID, Admin: true})
	if err != nil {
		t.Fatalf("RetryReset() error = %v", err)
	}
	if updated.Status.String() != "approved_reset_succeeded" {
		t.Fatalf("status = %s, want approved_reset_succeeded", updated.Status)
	}
}

func TestResetFailureCanBeRetriedByFinalV2Approver(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
	approver := createQuotaResetUser(t, ctx, client, "bob", "bob@example.org", nil, "user")
	provider := createQuotaResetRelayProvider(t, ctx, client)
	workflow := workflowFixtureForUsers(requester.ID, approver.ID, approver.ID, true)
	workflow.Steps = workflow.Steps[:1]
	request := createPendingWorkflowRequest(t, ctx, client, requester, provider, workflow)
	fake := &fakeQuotaResetProvider{
		subscriptions: []relay.UserSubscription{activeQuotaResetSubscription(42, "Group Alpha")},
		resetErr:      errors.New("relay timeout"),
	}
	svc := NewService(client, fakeProviderResolver(provider.ID, fake), nil, nil)

	failed, err := svc.Approve(ctx, DecisionInput{ActorUserID: approver.ID, RequestID: request.ID, DecisionReason: "Approved"})
	if err != nil || failed.Status != quotaresetrequest.StatusApprovedResetFailed {
		t.Fatalf("Approve() = status %v error %v, want reset failure", failed.Status, err)
	}
	fake.resetErr = nil
	updated, err := svc.RetryReset(ctx, DecisionInput{ActorUserID: approver.ID, RequestID: request.ID})
	if err != nil {
		t.Fatalf("RetryReset() error = %v", err)
	}
	if updated.Status != quotaresetrequest.StatusApprovedResetSucceeded || fake.resetCalls != 2 {
		t.Fatalf("status/reset calls = %s/%d, want succeeded/2", updated.Status, fake.resetCalls)
	}
}

func TestRetryResetRejectsRequestAlreadyResettingBeforeCallingProvider(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	admin := client.User.Create().SetUsername("admin").SetEmail("admin@example.com").SetAuthSource("ldap").SetRole(entuser.RoleAdmin).SaveX(ctx)
	requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
	provider := createQuotaResetRelayProvider(t, ctx, client)
	request := createPendingQuotaResetRequest(t, ctx, client, requester.ID, 1001, provider.ID, "42", nil)
	client.QuotaResetRequest.UpdateOneID(request.ID).
		SetStatus(quotaresetrequest.StatusApprovedResetting).
		SaveX(ctx)
	fake := &fakeQuotaResetProvider{subscriptions: []relay.UserSubscription{activeQuotaResetSubscription(42, "Group Alpha")}}
	svc := NewService(client, fakeProviderResolver(provider.ID, fake), NewApproverResolver(client), nil)

	_, err := svc.executeReset(ctx, request.ID, admin.ID, true, true)
	if !errors.Is(err, ErrInvalidStatus) {
		t.Fatalf("executeReset(retry while resetting) error = %v, want ErrInvalidStatus", err)
	}
	if fake.resetCalls != 0 {
		t.Fatalf("reset calls = %d, want 0", fake.resetCalls)
	}
}

func TestExecuteResetSurvivesCallerCancellationAfterApprovalCommit(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
	approver := createQuotaResetUser(t, ctx, client, "bob", "bob@example.org", nil, "user")
	provider := createQuotaResetRelayProvider(t, ctx, client)
	request := createPendingQuotaResetRequest(t, ctx, client, requester.ID, 1001, provider.ID, "42", []int{approver.ID})
	request = client.QuotaResetRequest.UpdateOneID(request.ID).SetStatus(quotaresetrequest.StatusApprovedResetFailed).SaveX(ctx)
	fake := &fakeQuotaResetProvider{}
	svc := NewService(client, fakeProviderResolver(provider.ID, fake), nil, nil)
	cancelled, cancel := context.WithCancel(ctx)
	cancel()

	updated, err := svc.executeReset(cancelled, request.ID, approver.ID, true, false)
	if err != nil {
		t.Fatalf("executeReset() error = %v", err)
	}
	if updated.Status != quotaresetrequest.StatusApprovedResetSucceeded || fake.resetCalls != 1 {
		t.Fatalf("status/reset calls = %s/%d, want succeeded/1", updated.Status, fake.resetCalls)
	}
}

func TestExecuteResetBoundsDetachedRelayCall(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
	approver := createQuotaResetUser(t, ctx, client, "bob", "bob@example.org", nil, "user")
	provider := createQuotaResetRelayProvider(t, ctx, client)
	request := createPendingQuotaResetRequest(t, ctx, client, requester.ID, 1001, provider.ID, "42", []int{approver.ID})
	request = client.QuotaResetRequest.UpdateOneID(request.ID).SetStatus(quotaresetrequest.StatusApprovedResetFailed).SaveX(ctx)
	fake := &fakeQuotaResetProvider{resetBlockFor: 500 * time.Millisecond}
	svc := NewService(client, fakeProviderResolver(provider.ID, fake), nil, nil)
	svc.resetExecutionTimeout = 20 * time.Millisecond

	started := time.Now()
	updated, err := svc.executeReset(ctx, request.ID, approver.ID, true, false)
	if err != nil {
		t.Fatalf("executeReset() error = %v", err)
	}
	if elapsed := time.Since(started); elapsed >= 250*time.Millisecond {
		t.Fatalf("executeReset() elapsed = %s, want relay deadline", elapsed)
	}
	if updated.Status != quotaresetrequest.StatusApprovedResetFailed || !strings.Contains(updated.ResetError, context.DeadlineExceeded.Error()) {
		t.Fatalf("status/reset error = %s/%q, want bounded reset failure", updated.Status, updated.ResetError)
	}
}

func TestUpdateNotificationSettingsValidatesEnabledURLAndBearerCredential(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	svc := NewService(client, nil, nil, nil)
	_, err := svc.UpdateNotificationSettings(ctx, UpdateNotificationSettingsInput{
		ActorUserID: 1,
		Enabled:     true,
		Channel:     "legacy_auto",
		URL:         "https://hooks.example.com/quota-reset",
		AuthType:    "none",
	})
	if !errors.Is(err, ErrInvalidNotification) {
		t.Fatalf("legacy channel error = %v, want ErrInvalidNotification", err)
	}

	_, err = svc.UpdateNotificationSettings(ctx, UpdateNotificationSettingsInput{
		ActorUserID: 1,
		Enabled:     true,
		URL:         "ftp://hooks.example.com/quota-reset",
		AuthType:    "none",
	})
	if err == nil || !strings.Contains(err.Error(), "invalid webhook url") {
		t.Fatalf("invalid URL error = %v, want invalid webhook url", err)
	}

	_, err = svc.UpdateNotificationSettings(ctx, UpdateNotificationSettingsInput{
		ActorUserID: 1,
		Enabled:     true,
		URL:         "https://hooks.example.com/quota-reset",
		AuthType:    "bearer_token",
	})
	if err == nil || !strings.Contains(err.Error(), "credential is required") {
		t.Fatalf("missing credential error = %v, want credential required", err)
	}

	wrongKind := client.Credential.Create().
		SetName("Wrong webhook credential").
		SetKind(entcredential.KindUsernamePassword).
		SetPayload("encrypted-payload").
		SaveX(ctx)
	_, err = svc.UpdateNotificationSettings(ctx, UpdateNotificationSettingsInput{
		ActorUserID:  1,
		Enabled:      true,
		URL:          "https://hooks.example.com/quota-reset",
		AuthType:     "bearer_token",
		CredentialID: &wrongKind.ID,
	})
	if err == nil || !strings.Contains(err.Error(), "must be secret_text") {
		t.Fatalf("wrong credential kind error = %v, want secret_text error", err)
	}
}

func TestUpdateNotificationSettingsCollapsesDuplicateRows(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	client.QuotaResetNotificationSetting.Create().
		SetEnabled(false).
		SetURL("https://hooks.example.com/old-a").
		SetAuthType("none").
		SetCreatedByUserID(1).
		SetUpdatedByUserID(1).
		SaveX(ctx)
	client.QuotaResetNotificationSetting.Create().
		SetEnabled(true).
		SetURL("https://hooks.example.com/old-b").
		SetAuthType("none").
		SetCreatedByUserID(1).
		SetUpdatedByUserID(1).
		SaveX(ctx)
	svc := NewService(client, nil, nil, nil)

	updated, err := svc.UpdateNotificationSettings(ctx, UpdateNotificationSettingsInput{
		ActorUserID: 7,
		Enabled:     true,
		Channel:     "wecom_group_robot",
		URL:         "https://hooks.example.com/quota-reset",
		AuthType:    "none",
	})
	if err != nil {
		t.Fatalf("UpdateNotificationSettings() error = %v", err)
	}
	if updated.URL != "https://hooks.example.com/quota-reset" || updated.Channel != "wecom_group_robot" || !updated.Enabled {
		t.Fatalf("updated settings = %+v, want new enabled URL", updated)
	}
	rows := client.QuotaResetNotificationSetting.Query().AllX(ctx)
	if len(rows) != 1 {
		t.Fatalf("notification setting row count = %d, want 1", len(rows))
	}
	if rows[0].URL != "https://hooks.example.com/quota-reset" || rows[0].Channel.String() != "wecom_group_robot" || rows[0].UpdatedByUserID != 7 {
		t.Fatalf("remaining row = %+v, want updated canonical row", rows[0])
	}
}

func TestNotificationSettingsTestRequiresEnabledSavedWebhook(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	svc := NewService(client, nil, nil, NewWebhookNotifier(client, "", ""))

	err := svc.TestNotificationSettings(ctx, 7)
	if !errors.Is(err, ErrInvalidNotification) {
		t.Fatalf("TestNotificationSettings(no setting) error = %v, want ErrInvalidNotification", err)
	}

	client.QuotaResetNotificationSetting.Create().
		SetEnabled(false).
		SetURL("https://hooks.example.com/quota-reset").
		SetAuthType("none").
		SetCreatedByUserID(1).
		SetUpdatedByUserID(1).
		SaveX(ctx)
	err = svc.TestNotificationSettings(ctx, 7)
	if !errors.Is(err, ErrInvalidNotification) {
		t.Fatalf("TestNotificationSettings(disabled setting) error = %v, want ErrInvalidNotification", err)
	}
}

func TestNotificationSettingsBackfillsLegacyWeComChannel(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	row := client.QuotaResetNotificationSetting.Create().
		SetEnabled(true).
		SetChannel("legacy_auto").
		SetURL("https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=redacted-test-key").
		SetAuthType("none").
		SaveX(ctx)
	svc := NewService(client, nil, nil, nil)

	settings, err := svc.GetNotificationSettings(ctx)
	if err != nil {
		t.Fatalf("GetNotificationSettings() error = %v", err)
	}
	if settings.Channel != "wecom_group_robot" {
		t.Fatalf("channel = %q, want wecom_group_robot", settings.Channel)
	}
	stored := client.QuotaResetNotificationSetting.GetX(ctx, row.ID)
	if stored.Channel.String() != "wecom_group_robot" {
		t.Fatalf("stored channel = %q, want backfilled wecom_group_robot", stored.Channel)
	}
}

func TestListApproverCandidatesReturnsActiveMembersOfSelectedDepartment(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	source := createQuotaResetDirectorySource(t, ctx, client)
	alpha := createQuotaResetDepartment(t, ctx, client, source.ID, "department-alpha", "Department Alpha", nil)
	beta := createQuotaResetDepartment(t, ctx, client, source.ID, "department-beta", "Department Beta", nil)
	alphaLead := createQuotaResetUser(t, ctx, client, "lead-alpha", "lead-alpha@example.com", nil, "user")
	staleMatchedLead := createQuotaResetUser(t, ctx, client, "lead-alpha-stale", "lead-alpha-stale@example.com", nil, "user")
	betaLead := createQuotaResetUser(t, ctx, client, "lead-beta", "lead-beta@example.com", nil, "user")
	peer := createQuotaResetUser(t, ctx, client, "peer-alpha", "peer-alpha@example.com", nil, "user")
	client.DirectoryDepartment.UpdateOneID(alpha.ID).
		SetMetadata(map[string]any{"representative_external_ids": []any{"member-alpha-lead", "member-alpha-stale", "member-alpha-unmatched"}}).
		SaveX(ctx)
	createQuotaResetMember(t, ctx, client, source.ID, "member-alpha-lead", alphaLead.Email, alpha.ExternalID, &alphaLead.ID)
	createQuotaResetMember(t, ctx, client, source.ID, "member-alpha-stale", staleMatchedLead.Email, alpha.ExternalID, nil)
	createQuotaResetMember(t, ctx, client, source.ID, "member-alpha-unmatched", "lead-alpha-unmatched@example.com", alpha.ExternalID, nil)
	createQuotaResetMember(t, ctx, client, source.ID, "member-alpha-peer", peer.Email, alpha.ExternalID, &peer.ID)
	client.DirectoryMember.UpdateOneID(createQuotaResetMember(t, ctx, client, source.ID, "member-beta-lead", betaLead.Email, beta.ExternalID, &betaLead.ID).ID).
		SetMetadata(map[string]any{"leader_department_ids": []any{beta.ExternalID}}).
		SaveX(ctx)
	svc := NewService(client, nil, nil, nil)

	resp, err := svc.ListApproverCandidates(ctx, source.ID, alpha.ExternalID)
	if err != nil {
		t.Fatalf("ListApproverCandidates() error = %v", err)
	}
	if got, want := approverCandidateUserIDs(resp.Items), []int{alphaLead.ID, staleMatchedLead.ID, peer.ID}; !reflect.DeepEqual(got, want) {
		t.Fatalf("alpha candidate ids = %#v, want all mapped department members %#v", got, want)
	}
	if resp.Items[0].Email != alphaLead.Email || resp.Items[0].DirectoryMemberExternalID != "member-alpha-lead" {
		t.Fatalf("alpha candidate detail = %#v", resp.Items[0])
	}
	if len(resp.UnmatchedRepresentatives) != 1 || resp.UnmatchedRepresentatives[0].DirectoryMemberExternalID != "member-alpha-unmatched" {
		t.Fatalf("alpha unmatched representatives = %#v, want member-alpha-unmatched", resp.UnmatchedRepresentatives)
	}

	resp, err = svc.ListApproverCandidates(ctx, source.ID, beta.ExternalID)
	if err != nil {
		t.Fatalf("ListApproverCandidates(beta) error = %v", err)
	}
	if len(resp.Items) != 1 || resp.Items[0].UserID != betaLead.ID {
		t.Fatalf("beta candidates = %#v, want beta representative from member metadata", resp.Items)
	}
}

func approverCandidateUserIDs(items []ApproverCandidate) []int {
	ids := make([]int, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.UserID)
	}
	sort.Ints(ids)
	return ids
}

func TestListApproverConfigsOnlyReturnsCurrentDirectorySource(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	staleSource := createQuotaResetDirectorySource(t, ctx, client)
	staleApprover := createQuotaResetUser(t, ctx, client, "lead-stale", "lead-stale@example.com", nil, "user")
	createQuotaResetApproverConfig(t, ctx, client, staleSource.ID, "department-stale", "Department Stale", staleApprover.ID)
	currentSource := createQuotaResetDirectorySource(t, ctx, client)
	currentApprover := createQuotaResetUser(t, ctx, client, "lead-current", "lead-current@example.com", nil, "user")
	createQuotaResetApproverConfig(t, ctx, client, currentSource.ID, "department-current", "Department Current", currentApprover.ID)
	svc := NewService(client, nil, nil, nil)

	resp, err := svc.ListApproverConfigs(ctx)
	if err != nil {
		t.Fatalf("ListApproverConfigs() error = %v", err)
	}
	if len(resp.Items) != 1 {
		t.Fatalf("config count = %d, want current source only", len(resp.Items))
	}
	if resp.CurrentDirectorySourceID == nil || *resp.CurrentDirectorySourceID != currentSource.ID {
		t.Fatalf("current directory source = %v, want %d", resp.CurrentDirectorySourceID, currentSource.ID)
	}
	if resp.Items[0].DirectorySourceID != currentSource.ID || resp.Items[0].ApproverUserID != currentApprover.ID {
		t.Fatalf("configs = %#v, want current source config only", resp.Items)
	}
}

func TestSaveApproverConfigsAllowsDepartmentMemberAndRejectsOutsider(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	source := createQuotaResetDirectorySource(t, ctx, client)
	department := createQuotaResetDepartment(t, ctx, client, source.ID, "department-alpha", "Department Alpha", nil)
	lead := createQuotaResetUser(t, ctx, client, "lead-alpha", "lead-alpha@example.com", nil, "user")
	peer := createQuotaResetUser(t, ctx, client, "peer-alpha", "peer-alpha@example.com", nil, "user")
	outsider := createQuotaResetUser(t, ctx, client, "outsider", "outsider@example.com", nil, "user")
	client.DirectoryDepartment.UpdateOneID(department.ID).
		SetMetadata(map[string]any{"representative_external_ids": []any{"member-alpha-lead"}}).
		SaveX(ctx)
	createQuotaResetMember(t, ctx, client, source.ID, "member-alpha-lead", lead.Email, department.ExternalID, &lead.ID)
	createQuotaResetMember(t, ctx, client, source.ID, "member-alpha-peer", peer.Email, department.ExternalID, &peer.ID)
	createQuotaResetMember(t, ctx, client, source.ID, "member-outsider", outsider.Email, "department-other", &outsider.ID)
	svc := NewService(client, nil, nil, nil)

	_, err := svc.SaveApproverConfigs(ctx, SaveApproverConfigsInput{
		ActorUserID: 1,
		Mode:        ApproverConfigSaveModeReplaceAll,
		Items: []ApproverConfigInput{{
			DepartmentExternalID:  department.ExternalID,
			DepartmentDisplayPath: department.Name,
			ApproverUserID:        outsider.ID,
			Enabled:               true,
		}},
	})
	if !errors.Is(err, ErrInvalidApproverConfig) {
		t.Fatalf("SaveApproverConfigs(outsider) error = %v, want ErrInvalidApproverConfig", err)
	}

	resp, err := svc.SaveApproverConfigs(ctx, SaveApproverConfigsInput{
		ActorUserID: 1,
		Mode:        ApproverConfigSaveModeReplaceAll,
		Items: []ApproverConfigInput{{
			DepartmentExternalID:  department.ExternalID,
			DepartmentDisplayPath: department.Name,
			ApproverUserID:        peer.ID,
			Enabled:               true,
		}},
	})
	if err != nil {
		t.Fatalf("SaveApproverConfigs(representative) error = %v", err)
	}
	if len(resp.Items) != 1 || resp.Items[0].ApproverUserID != peer.ID {
		t.Fatalf("saved configs = %#v, want non-representative department member", resp.Items)
	}
}

type providerResolverFunc func(context.Context, int) (relay.Provider, error)

func (f providerResolverFunc) Resolve(ctx context.Context, providerID int) (relay.Provider, error) {
	return f(ctx, providerID)
}

func fakeProviderResolver(wantID int, provider relay.Provider) ProviderResolver {
	return providerResolverFunc(func(_ context.Context, providerID int) (relay.Provider, error) {
		if providerID != wantID {
			return nil, fmt.Errorf("provider id = %d, want %d", providerID, wantID)
		}
		return provider, nil
	})
}

type fakeQuotaResetProvider struct {
	relay.Provider
	mu            sync.Mutex
	subscriptions []relay.UserSubscription
	resetErr      error
	resetBlockFor time.Duration
	resetUserID   int64
	resetGroupID  int64
	resetCalls    int
}

type recordingQuotaResetNotifier struct {
	events []string
}

func (n *recordingQuotaResetNotifier) NotifyRequestEvent(_ context.Context, event string, _ *ent.QuotaResetRequest) error {
	n.events = append(n.events, event)
	return nil
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func (f *fakeQuotaResetProvider) ListUserSubscriptions(_ context.Context, relayUserID int64) ([]relay.UserSubscription, error) {
	return f.subscriptions, nil
}

func (f *fakeQuotaResetProvider) ResetSubscriptionQuotaForUser(ctx context.Context, relayUserID, groupID int64) error {
	f.mu.Lock()
	f.resetCalls++
	f.resetUserID = relayUserID
	f.resetGroupID = groupID
	blockFor := f.resetBlockFor
	resetErr := f.resetErr
	f.mu.Unlock()
	if blockFor > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(blockFor):
			return errors.New("test reset did not receive deadline")
		}
	}
	return resetErr
}

func workflowFixtureForUsers(requesterID, firstApproverID, finalApproverID int, reuseFirst bool) *Workflow {
	secondApproverID := finalApproverID
	if reuseFirst {
		secondApproverID = firstApproverID
	}
	return &Workflow{
		Version:     workflowVersionV2,
		CurrentStep: 0,
		Requester: WorkflowPerson{
			UserID:          requesterID,
			DisplayName:     "alice",
			Email:           "alice@example.com",
			DepartmentPaths: []string{"Company / Group Alpha"},
			NotificationIDs: map[string]string{"wecom": "alice"},
		},
		Steps: []WorkflowStep{
			{
				Kind:                  WorkflowStepRequesterDepartments,
				Label:                 "Company / Group Alpha",
				DepartmentExternalIDs: []string{"dept-alpha"},
				Approvers:             []WorkflowApprover{{UserID: firstApproverID, DisplayName: "bob", Email: "bob@example.org", Source: "configured"}},
				Status:                WorkflowStepActive,
			},
			{
				Kind:                  WorkflowStepConfiguredDepartment,
				Label:                 "Company / Group Beta",
				DepartmentExternalIDs: []string{"dept-beta"},
				Approvers:             []WorkflowApprover{{UserID: secondApproverID, DisplayName: "carol", Email: "carol@example.org", Source: "configured"}},
				Status:                WorkflowStepQueued,
			},
		},
	}
}

func createPendingWorkflowRequest(t *testing.T, ctx context.Context, client *ent.Client, requester *ent.User, provider *ent.RelayProvider, workflow *Workflow) *ent.QuotaResetRequest {
	t.Helper()
	raw, err := EncodeWorkflow(workflow)
	if err != nil {
		t.Fatalf("EncodeWorkflow() error = %v", err)
	}
	request, err := client.QuotaResetRequest.Create().
		SetRequesterUserID(requester.ID).
		SetRequesterRelayUserID(int64(*requester.RelayUserID)).
		SetProviderID(provider.ID).
		SetGroupID("42").
		SetGroupName("Group Alpha").
		SetGroupPlatform("openai").
		SetReason("Need reset for a build investigation").
		SetStatus(quotaresetrequest.StatusWorkflowPending).
		SetWorkflowVersion(workflowVersionV2).
		SetWorkflow(raw).
		SetWorkflowRevision(0).
		SetResolvedApproverUserIds(workflow.ActiveApproverUserIDs()).
		SetMatchedDepartmentPaths([]map[string]any{}).
		Save(ctx)
	if err != nil {
		t.Fatalf("create workflow request: %v", err)
	}
	return request
}

type listOnlyQuotaResetProvider struct {
	relay.Provider
}

func (f *listOnlyQuotaResetProvider) ListUserSubscriptions(_ context.Context, relayUserID int64) ([]relay.UserSubscription, error) {
	return []relay.UserSubscription{activeQuotaResetSubscription(42, "Group Alpha")}, nil
}

func activeQuotaResetSubscription(groupID int64, groupName string) relay.UserSubscription {
	return relay.UserSubscription{
		ID:              groupID * 10,
		UserID:          1001,
		GroupID:         groupID,
		Status:          "active",
		DailyUsageUSD:   10,
		WeeklyUsageUSD:  20,
		MonthlyUsageUSD: 30,
		Group:           &relay.Group{ID: groupID, Name: groupName, Platform: "openai"},
	}
}

func createQuotaResetRelayProvider(t *testing.T, ctx context.Context, client *ent.Client) *ent.RelayProvider {
	t.Helper()
	provider, err := client.RelayProvider.Create().
		SetName("primary-relay").
		SetDisplayName("Primary Relay").
		SetBaseURL("https://relay.example.com").
		SetAdminAPIKey("test-admin-api-key").
		SetRelayType("sub2api").
		SetIsPrimary(true).
		SetEnabled(true).
		Save(ctx)
	if err != nil {
		t.Fatalf("create relay provider: %v", err)
	}
	return provider
}

func createPendingQuotaResetRequest(t *testing.T, ctx context.Context, client *ent.Client, requesterUserID int, requesterRelayUserID int64, providerID int, groupID string, approverIDs []int) *ent.QuotaResetRequest {
	t.Helper()
	request, err := client.QuotaResetRequest.Create().
		SetRequesterUserID(requesterUserID).
		SetRequesterRelayUserID(requesterRelayUserID).
		SetProviderID(providerID).
		SetGroupID(groupID).
		SetGroupName("Group Alpha").
		SetGroupPlatform("openai").
		SetReason("Need reset for a build investigation").
		SetResolvedApproverUserIds(approverIDs).
		SetMatchedDepartmentPaths([]map[string]any{}).
		Save(ctx)
	if err != nil {
		t.Fatalf("create pending request: %v", err)
	}
	return request
}
