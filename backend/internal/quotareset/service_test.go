package quotareset

import (
	"context"
	"database/sql"
	"encoding/json"
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
	"github.com/ai-efficiency/backend/internal/workitems"
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
	if count := client.QuotaResetRequestEvent.Query().CountX(ctx); count != 5 {
		t.Fatalf("event count = %d, want initial request, workflow, activation, and fallback audit", count)
	}
}

func TestCreateRequestInvalidatesWorkItemCountsWithRequestAndEvents(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	revisions := workitems.NewRevisionStore(client)
	if err := revisions.Ensure(ctx); err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	before, err := revisions.Current(ctx)
	if err != nil {
		t.Fatalf("Current() before error = %v", err)
	}
	requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
	provider := createQuotaResetRelayProvider(t, ctx, client)
	fake := &fakeQuotaResetProvider{subscriptions: []relay.UserSubscription{activeQuotaResetSubscription(42, "Group Alpha")}}
	svc := NewService(client, fakeProviderResolver(provider.ID, fake), NewApproverResolver(client), nil, revisions)

	req, err := svc.CreateRequest(ctx, CreateRequestInput{
		RequesterUserID: requester.ID,
		GroupID:         "42",
		Reason:          "Need reset for a build investigation",
	})
	if err != nil {
		t.Fatalf("CreateRequest() error = %v", err)
	}
	after, err := revisions.Current(ctx)
	if err != nil {
		t.Fatalf("Current() after error = %v", err)
	}
	if after == before {
		t.Fatalf("revision after CreateRequest() = %q, want a new revision", after)
	}
	eventTypes := client.QuotaResetRequestEvent.Query().
		Where(quotaresetrequestevent.RequestIDEQ(req.ID)).
		Order(ent.Asc(quotaresetrequestevent.FieldID)).
		Select(quotaresetrequestevent.FieldEventType).
		StringsX(ctx)
	wantEventTypes := []string{
		quotaresetrequestevent.EventTypeCreated.String(),
		quotaresetrequestevent.EventTypeApproverResolved.String(),
		quotaresetrequestevent.EventTypeWorkflowCreated.String(),
		quotaresetrequestevent.EventTypeStepActivated.String(),
		quotaresetrequestevent.EventTypeAdminFallbackActivated.String(),
	}
	if !reflect.DeepEqual(eventTypes, wantEventTypes) {
		t.Fatalf("event types = %#v, want %#v", eventTypes, wantEventTypes)
	}
}

func TestCreateRequestRollsBackRequestAndEventsWhenRevisionInvalidationFails(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	revisions, before := initializeQuotaResetRevisions(t, ctx, client)
	requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
	provider := createQuotaResetRelayProvider(t, ctx, client)
	fake := &fakeQuotaResetProvider{subscriptions: []relay.UserSubscription{activeQuotaResetSubscription(42, "Group Alpha")}}
	failQuotaResetRevisionUpdates(client, func() bool { return true })
	svc := NewService(client, fakeProviderResolver(provider.ID, fake), NewApproverResolver(client), nil, revisions)

	_, err := svc.CreateRequest(ctx, CreateRequestInput{
		RequesterUserID: requester.ID,
		GroupID:         "42",
		Reason:          "Need reset for a build investigation",
	})
	if err == nil || !strings.Contains(err.Error(), "injected revision failure") {
		t.Fatalf("CreateRequest() error = %v, want injected revision failure", err)
	}
	if count := client.QuotaResetRequest.Query().CountX(ctx); count != 0 {
		t.Fatalf("request count after rollback = %d, want 0", count)
	}
	if count := client.QuotaResetRequestEvent.Query().CountX(ctx); count != 0 {
		t.Fatalf("event count after rollback = %d, want 0", count)
	}
	if after := currentQuotaResetRevision(t, ctx, revisions); after != before {
		t.Fatalf("revision after rollback = %q, want %q", after, before)
	}
}

func TestCreateRequestClassifiesConcurrentDuplicateAfterTransactionRollback(t *testing.T) {
	ctx := context.Background()
	client, dsn := testdb.OpenWithDSN(t)
	competingClient, err := ent.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open competing ent client: %v", err)
	}
	t.Cleanup(func() { _ = competingClient.Close() })
	revisions, before := initializeQuotaResetRevisions(t, ctx, client)
	requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
	provider := createQuotaResetRelayProvider(t, ctx, client)
	fake := &fakeQuotaResetProvider{subscriptions: []relay.UserSubscription{activeQuotaResetSubscription(42, "Group Alpha")}}
	injected := false
	client.QuotaResetRequest.Use(func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, mutation ent.Mutation) (ent.Value, error) {
			if mutation.Op().Is(ent.OpCreate) && !injected {
				injected = true
				if _, err := competingClient.QuotaResetRequest.Create().
					SetRequesterUserID(requester.ID).
					SetRequesterRelayUserID(1001).
					SetProviderID(provider.ID).
					SetGroupID("42").
					SetGroupName("Group Alpha").
					SetGroupPlatform("openai").
					SetReason("Concurrent synthetic request").
					SetResolvedApproverUserIds([]int{}).
					SetMatchedDepartmentPaths([]map[string]any{}).
					Save(ctx); err != nil {
					return nil, fmt.Errorf("create competing request: %w", err)
				}
			}
			return next.Mutate(ctx, mutation)
		})
	})
	svc := NewService(client, fakeProviderResolver(provider.ID, fake), NewApproverResolver(client), nil, revisions)

	_, err = svc.CreateRequest(ctx, CreateRequestInput{
		RequesterUserID: requester.ID,
		GroupID:         "42",
		Reason:          "Need reset for a build investigation",
	})
	if !errors.Is(err, ErrActiveRequestExists) {
		t.Fatalf("CreateRequest() error = %v, want ErrActiveRequestExists", err)
	}
	if count := client.QuotaResetRequest.Query().CountX(ctx); count != 1 {
		t.Fatalf("request count = %d, want only the competing request", count)
	}
	if count := client.QuotaResetRequestEvent.Query().CountX(ctx); count != 0 {
		t.Fatalf("event count after duplicate rollback = %d, want 0", count)
	}
	if after := currentQuotaResetRevision(t, ctx, revisions); after != before {
		t.Fatalf("revision after duplicate rollback = %q, want %q", after, before)
	}
}

func TestPendingExitInvalidatesWorkItemCountsWithStatusAndEvent(t *testing.T) {
	tests := []struct {
		name       string
		wantStatus quotaresetrequest.Status
		wantEvent  quotaresetrequestevent.EventType
		mutate     func(context.Context, *Service, int, int) (*ent.QuotaResetRequest, error)
	}{
		{
			name:       "cancel",
			wantStatus: quotaresetrequest.StatusCancelled,
			wantEvent:  quotaresetrequestevent.EventTypeCancelled,
			mutate: func(ctx context.Context, svc *Service, requesterID, requestID int) (*ent.QuotaResetRequest, error) {
				return svc.Cancel(ctx, requesterID, requestID)
			},
		},
		{
			name:       "reject",
			wantStatus: quotaresetrequest.StatusRejected,
			wantEvent:  quotaresetrequestevent.EventTypeRejected,
			mutate: func(ctx context.Context, svc *Service, actorID, requestID int) (*ent.QuotaResetRequest, error) {
				return svc.Reject(ctx, DecisionInput{ActorUserID: actorID, RequestID: requestID, DecisionReason: "Synthetic rejection reason", Admin: true})
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			client := testdb.Open(t)
			revisions, before := initializeQuotaResetRevisions(t, ctx, client)
			requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
			actor := requester
			if test.name == "reject" {
				actor = createQuotaResetUser(t, ctx, client, "admin", "admin@example.com", nil, "admin")
			}
			provider := createQuotaResetRelayProvider(t, ctx, client)
			request := createPendingQuotaResetRequest(t, ctx, client, requester.ID, 1001, provider.ID, "42", nil)
			svc := NewService(client, nil, nil, nil, revisions)

			updated, err := test.mutate(ctx, svc, actor.ID, request.ID)
			if err != nil {
				t.Fatalf("mutation error = %v", err)
			}
			if updated.Status != test.wantStatus {
				t.Fatalf("status = %s, want %s", updated.Status, test.wantStatus)
			}
			if after := currentQuotaResetRevision(t, ctx, revisions); after == before {
				t.Fatalf("revision after mutation = %q, want a new revision", after)
			}
			if got := quotaResetEventTypes(t, ctx, client, request.ID); !reflect.DeepEqual(got, []string{test.wantEvent.String()}) {
				t.Fatalf("event types = %#v, want %s", got, test.wantEvent)
			}
		})
	}
}

func TestPendingExitRollsBackStatusAndEventWhenRevisionInvalidationFails(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(context.Context, *Service, int, int) (*ent.QuotaResetRequest, error)
	}{
		{
			name: "cancel",
			mutate: func(ctx context.Context, svc *Service, requesterID, requestID int) (*ent.QuotaResetRequest, error) {
				return svc.Cancel(ctx, requesterID, requestID)
			},
		},
		{
			name: "reject",
			mutate: func(ctx context.Context, svc *Service, actorID, requestID int) (*ent.QuotaResetRequest, error) {
				return svc.Reject(ctx, DecisionInput{ActorUserID: actorID, RequestID: requestID, DecisionReason: "Synthetic rejection reason", Admin: true})
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			client := testdb.Open(t)
			revisions, before := initializeQuotaResetRevisions(t, ctx, client)
			requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
			actor := requester
			if test.name == "reject" {
				actor = createQuotaResetUser(t, ctx, client, "admin", "admin@example.com", nil, "admin")
			}
			provider := createQuotaResetRelayProvider(t, ctx, client)
			request := createPendingQuotaResetRequest(t, ctx, client, requester.ID, 1001, provider.ID, "42", nil)
			failQuotaResetRevisionUpdates(client, func() bool { return true })
			svc := NewService(client, nil, nil, nil, revisions)

			_, err := test.mutate(ctx, svc, actor.ID, request.ID)
			if err == nil || !strings.Contains(err.Error(), "injected revision failure") {
				t.Fatalf("mutation error = %v, want injected revision failure", err)
			}
			if status := client.QuotaResetRequest.GetX(ctx, request.ID).Status; status != quotaresetrequest.StatusPending {
				t.Fatalf("status after rollback = %s, want pending", status)
			}
			if got := quotaResetEventTypes(t, ctx, client, request.ID); len(got) != 0 {
				t.Fatalf("event types after rollback = %#v, want none", got)
			}
			if after := currentQuotaResetRevision(t, ctx, revisions); after != before {
				t.Fatalf("revision after rollback = %q, want %q", after, before)
			}
		})
	}
}

func TestActionableTransitionPredicateMissMapsToInvalidStatus(t *testing.T) {
	tests := []struct {
		name           string
		initialStatus  quotaresetrequest.Status
		mutationStatus quotaresetrequest.Status
		racedStatus    quotaresetrequest.Status
		mutate         func(context.Context, *Service, int, int) (*ent.QuotaResetRequest, error)
	}{
		{
			name:           "cancel",
			initialStatus:  quotaresetrequest.StatusPending,
			mutationStatus: quotaresetrequest.StatusCancelled,
			racedStatus:    quotaresetrequest.StatusRejected,
			mutate: func(ctx context.Context, svc *Service, requesterID, requestID int) (*ent.QuotaResetRequest, error) {
				return svc.Cancel(ctx, requesterID, requestID)
			},
		},
		{
			name:           "reject",
			initialStatus:  quotaresetrequest.StatusPending,
			mutationStatus: quotaresetrequest.StatusRejected,
			racedStatus:    quotaresetrequest.StatusCancelled,
			mutate: func(ctx context.Context, svc *Service, actorID, requestID int) (*ent.QuotaResetRequest, error) {
				return svc.Reject(ctx, DecisionInput{ActorUserID: actorID, RequestID: requestID, DecisionReason: "Synthetic rejection reason", Admin: true})
			},
		},
		{
			name:           "approve",
			initialStatus:  quotaresetrequest.StatusPending,
			mutationStatus: quotaresetrequest.StatusApprovedResetting,
			racedStatus:    quotaresetrequest.StatusRejected,
			mutate: func(ctx context.Context, svc *Service, actorID, requestID int) (*ent.QuotaResetRequest, error) {
				return svc.Approve(ctx, DecisionInput{ActorUserID: actorID, RequestID: requestID, DecisionReason: "Synthetic approval reason", Admin: true})
			},
		},
		{
			name:           "retry",
			initialStatus:  quotaresetrequest.StatusApprovedResetFailed,
			mutationStatus: quotaresetrequest.StatusApprovedResetting,
			racedStatus:    quotaresetrequest.StatusApprovedResetting,
			mutate: func(ctx context.Context, svc *Service, actorID, requestID int) (*ent.QuotaResetRequest, error) {
				return svc.RetryReset(ctx, DecisionInput{ActorUserID: actorID, RequestID: requestID, Admin: true})
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			client, dsn := testdb.OpenWithDSN(t)
			competingClient, err := ent.Open("postgres", dsn)
			if err != nil {
				t.Fatalf("open competing ent client: %v", err)
			}
			t.Cleanup(func() { _ = competingClient.Close() })
			revisions, before := initializeQuotaResetRevisions(t, ctx, client)
			requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
			admin := createQuotaResetUser(t, ctx, client, "admin", "admin@example.com", nil, "admin")
			actorID := admin.ID
			if test.name == "cancel" {
				actorID = requester.ID
			}
			provider := createQuotaResetRelayProvider(t, ctx, client)
			request := createPendingQuotaResetRequest(t, ctx, client, requester.ID, 1001, provider.ID, "42", nil)
			if test.initialStatus != quotaresetrequest.StatusPending {
				client.QuotaResetRequest.UpdateOneID(request.ID).SetStatus(test.initialStatus).SaveX(ctx)
			}
			injected := false
			client.QuotaResetRequest.Use(func(next ent.Mutator) ent.Mutator {
				return ent.MutateFunc(func(ctx context.Context, mutation ent.Mutation) (ent.Value, error) {
					requestMutation, ok := mutation.(*ent.QuotaResetRequestMutation)
					if ok && mutation.Op().Is(ent.OpUpdateOne) && !injected {
						if status, changed := requestMutation.Status(); changed && status == test.mutationStatus {
							injected = true
							if _, err := competingClient.QuotaResetRequest.UpdateOneID(request.ID).SetStatus(test.racedStatus).Save(ctx); err != nil {
								return nil, fmt.Errorf("commit competing status: %w", err)
							}
						}
					}
					return next.Mutate(ctx, mutation)
				})
			})
			fake := &fakeQuotaResetProvider{}
			svc := NewService(client, fakeProviderResolver(provider.ID, fake), NewApproverResolver(client), nil, revisions)

			_, err = test.mutate(ctx, svc, actorID, request.ID)
			if !errors.Is(err, ErrInvalidStatus) {
				t.Fatalf("mutation error = %v, want ErrInvalidStatus", err)
			}
			if !injected {
				t.Fatal("competing status mutation was not injected")
			}
			if status := client.QuotaResetRequest.GetX(ctx, request.ID).Status; status != test.racedStatus {
				t.Fatalf("status = %s, want competing status %s", status, test.racedStatus)
			}
			if got := quotaResetEventTypes(t, ctx, client, request.ID); len(got) != 0 {
				t.Fatalf("events = %#v, want none from predicate miss", got)
			}
			if fake.resetCalls != 0 {
				t.Fatalf("Relay reset calls = %d, want 0", fake.resetCalls)
			}
			if after := currentQuotaResetRevision(t, ctx, revisions); after != before {
				t.Fatalf("revision after predicate miss = %q, want %q", after, before)
			}
		})
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

	fallback := newWorkflowStep(WorkflowStepConfiguredDepartment, nil)
	fallback.Label = "Admin fallback"
	fallback.AdminFallback = true
	fallback.Status = WorkflowStepActive
	workflow := &Workflow{
		Version:     workflowVersionV2,
		CurrentStep: 0,
		Requester:   WorkflowPerson{UserID: requester.ID},
		Steps:       []WorkflowStep{fallback},
	}
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

func TestApproveCallerCancellationBeforeRelayStillStoresResetFailure(t *testing.T) {
	setupCtx := context.Background()
	client := testdb.Open(t)
	requestCtx, cancel := context.WithCancel(setupCtx)
	requester := createQuotaResetUser(t, setupCtx, client, "alice", "alice@example.com", intPtr(1001), "user")
	approver := createQuotaResetUser(t, setupCtx, client, "lead", "lead@example.com", nil, "user")
	provider := createQuotaResetRelayProvider(t, setupCtx, client)
	request := createPendingQuotaResetRequest(t, setupCtx, client, requester.ID, 1001, provider.ID, "42", []int{approver.ID})
	resolver := providerResolverFunc(func(ctx context.Context, providerID int) (relay.Provider, error) {
		if providerID != provider.ID {
			t.Fatalf("provider id = %d, want %d", providerID, provider.ID)
		}
		cancel()
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return nil, errors.New("synthetic provider resolution failure after caller cancellation")
	})
	svc := NewService(client, resolver, NewApproverResolver(client), nil)

	updated, err := svc.Approve(requestCtx, DecisionInput{
		ActorUserID:    approver.ID,
		RequestID:      request.ID,
		DecisionReason: "Approved",
	})
	reloaded := client.QuotaResetRequest.GetX(setupCtx, request.ID)
	events := quotaResetEventTypes(t, setupCtx, client, request.ID)
	if err != nil || updated == nil || updated.Status != quotaresetrequest.StatusApprovedResetFailed || reloaded.Status != quotaresetrequest.StatusApprovedResetFailed {
		t.Fatalf("Approve() error = %v, returned = %+v, stored status = %s, events = %#v; want stored approved_reset_failed after caller cancellation", err, updated, reloaded.Status, events)
	}
	if !reflect.DeepEqual(events, []string{
		quotaresetrequestevent.EventTypeApproved.String(),
		quotaresetrequestevent.EventTypeResetStarted.String(),
		quotaresetrequestevent.EventTypeResetFailed.String(),
	}) {
		t.Fatalf("event types = %#v, want approved, reset_started, reset_failed", events)
	}
}

func TestRetryResetCallerCancellationDuringRelayStillStoresResetFailure(t *testing.T) {
	setupCtx := context.Background()
	client := testdb.Open(t)
	revisions, before := initializeQuotaResetRevisions(t, setupCtx, client)
	requestCtx, cancel := context.WithCancel(setupCtx)
	admin := createQuotaResetUser(t, setupCtx, client, "admin", "admin@example.com", nil, "admin")
	requester := createQuotaResetUser(t, setupCtx, client, "alice", "alice@example.com", intPtr(1001), "user")
	provider := createQuotaResetRelayProvider(t, setupCtx, client)
	request := createPendingQuotaResetRequest(t, setupCtx, client, requester.ID, 1001, provider.ID, "42", nil)
	client.QuotaResetRequest.UpdateOneID(request.ID).
		SetStatus(quotaresetrequest.StatusApprovedResetFailed).
		SetResetError("Synthetic prior reset failure").
		SaveX(setupCtx)
	var relayContextDone <-chan struct{}
	var relayContextBounded bool
	var relayContextCancelled bool
	var failureContextBounded bool
	var failureContextUncancelled bool
	var failureContextIndependent bool
	client.QuotaResetRequest.Use(func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, mutation ent.Mutation) (ent.Value, error) {
			requestMutation, ok := mutation.(*ent.QuotaResetRequestMutation)
			if ok {
				status, exists := requestMutation.Status()
				if exists && status == quotaresetrequest.StatusApprovedResetFailed {
					_, failureContextBounded = ctx.Deadline()
					failureContextUncancelled = ctx.Err() == nil
					failureContextIndependent = relayContextDone != nil && ctx.Done() != relayContextDone
				}
			}
			return next.Mutate(ctx, mutation)
		})
	})
	var revisionAtRelay string
	fake := &fakeQuotaResetProvider{}
	fake.resetFunc = func(ctx context.Context, _, _ int64) error {
		revisionAtRelay = currentQuotaResetRevision(t, setupCtx, revisions)
		relayContextDone = ctx.Done()
		_, relayContextBounded = ctx.Deadline()
		cancel()
		relayContextCancelled = ctx.Err() != nil
		return errors.New("synthetic relay failure after caller cancellation")
	}
	svc := NewService(client, fakeProviderResolver(provider.ID, fake), NewApproverResolver(client), nil, revisions)

	updated, err := svc.RetryReset(requestCtx, DecisionInput{ActorUserID: admin.ID, RequestID: request.ID, Admin: true})
	reloaded := client.QuotaResetRequest.GetX(setupCtx, request.ID)
	events := quotaResetEventTypes(t, setupCtx, client, request.ID)
	after := currentQuotaResetRevision(t, setupCtx, revisions)
	if err != nil || updated == nil || updated.Status != quotaresetrequest.StatusApprovedResetFailed || reloaded.Status != quotaresetrequest.StatusApprovedResetFailed {
		t.Fatalf("RetryReset() error = %v, returned = %+v, stored status = %s, events = %#v; want stored approved_reset_failed after caller cancellation", err, updated, reloaded.Status, events)
	}
	if !relayContextBounded || relayContextCancelled || !failureContextBounded || !failureContextUncancelled || !failureContextIndependent {
		t.Fatalf("contexts: relay_bounded=%v relay_cancelled=%v failure_bounded=%v failure_uncancelled=%v failure_independent=%v; want true/false/true/true/true", relayContextBounded, relayContextCancelled, failureContextBounded, failureContextUncancelled, failureContextIndependent)
	}
	if revisionAtRelay == "" || revisionAtRelay == before || after == revisionAtRelay {
		t.Fatalf("revisions before=%q at_relay=%q after=%q, want retry and failure commits to advance independently", before, revisionAtRelay, after)
	}
	if !reflect.DeepEqual(events, []string{
		quotaresetrequestevent.EventTypeResetRetried.String(),
		quotaresetrequestevent.EventTypeResetStarted.String(),
		quotaresetrequestevent.EventTypeResetFailed.String(),
	}) {
		t.Fatalf("event types = %#v, want reset_retried, reset_started, reset_failed", events)
	}
}

func TestApproveCallerCancellationAfterRelaySuccessStillStoresResetSuccess(t *testing.T) {
	setupCtx := context.Background()
	client := testdb.Open(t)
	requestCtx, cancel := context.WithCancel(setupCtx)
	requester := createQuotaResetUser(t, setupCtx, client, "alice", "alice@example.com", intPtr(1001), "user")
	approver := createQuotaResetUser(t, setupCtx, client, "lead", "lead@example.com", nil, "user")
	provider := createQuotaResetRelayProvider(t, setupCtx, client)
	request := createPendingQuotaResetRequest(t, setupCtx, client, requester.ID, 1001, provider.ID, "42", []int{approver.ID})
	var relayContextDone <-chan struct{}
	var relayContextBounded bool
	var relayContextCancelled bool
	var successContextBounded bool
	var successContextUncancelled bool
	var successContextIndependent bool
	client.QuotaResetRequest.Use(func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, mutation ent.Mutation) (ent.Value, error) {
			requestMutation, ok := mutation.(*ent.QuotaResetRequestMutation)
			if ok {
				status, exists := requestMutation.Status()
				if exists && status == quotaresetrequest.StatusApprovedResetSucceeded {
					_, successContextBounded = ctx.Deadline()
					successContextUncancelled = ctx.Err() == nil
					successContextIndependent = relayContextDone != nil && ctx.Done() != relayContextDone
				}
			}
			return next.Mutate(ctx, mutation)
		})
	})
	fake := &fakeQuotaResetProvider{}
	fake.resetFunc = func(ctx context.Context, _, _ int64) error {
		relayContextDone = ctx.Done()
		_, relayContextBounded = ctx.Deadline()
		cancel()
		relayContextCancelled = ctx.Err() != nil
		return nil
	}
	svc := NewService(client, fakeProviderResolver(provider.ID, fake), NewApproverResolver(client), nil)

	updated, err := svc.Approve(requestCtx, DecisionInput{
		ActorUserID:    approver.ID,
		RequestID:      request.ID,
		DecisionReason: "Approved",
	})
	reloaded := client.QuotaResetRequest.GetX(setupCtx, request.ID)
	events := quotaResetEventTypes(t, setupCtx, client, request.ID)
	if err != nil || updated == nil || updated.Status != quotaresetrequest.StatusApprovedResetSucceeded || reloaded.Status != quotaresetrequest.StatusApprovedResetSucceeded {
		t.Fatalf("Approve() error = %v, returned = %+v, stored status = %s, events = %#v; want stored approved_reset_succeeded after caller cancellation", err, updated, reloaded.Status, events)
	}
	if !relayContextBounded || relayContextCancelled || !successContextBounded || !successContextUncancelled || !successContextIndependent {
		t.Fatalf("contexts: relay_bounded=%v relay_cancelled=%v success_bounded=%v success_uncancelled=%v success_independent=%v; want true/false/true/true/true", relayContextBounded, relayContextCancelled, successContextBounded, successContextUncancelled, successContextIndependent)
	}
	if !reflect.DeepEqual(events, []string{
		quotaresetrequestevent.EventTypeApproved.String(),
		quotaresetrequestevent.EventTypeResetStarted.String(),
		quotaresetrequestevent.EventTypeResetSucceeded.String(),
	}) {
		t.Fatalf("event types = %#v, want approved, reset_started, reset_succeeded", events)
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
	assertQuotaResetEventTypes(t, ctx, client, request.ID,
		quotaresetrequestevent.EventTypeCreated,
		quotaresetrequestevent.EventTypeApproverResolved,
		quotaresetrequestevent.EventTypeWorkflowCreated,
		quotaresetrequestevent.EventTypeStepActivated,
		quotaresetrequestevent.EventTypeAdminFallbackActivated,
	)
}

func TestCreateRequestV2RecordsInitialActiveStepEvent(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	source := createQuotaResetDirectorySource(t, ctx, client)
	exact := createQuotaResetDepartment(t, ctx, client, source.ID, "dept-exact", "Exact", nil)
	requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
	approver := createQuotaResetUser(t, ctx, client, "bob", "bob@example.org", nil, "user")
	requesterMember := createQuotaResetMember(t, ctx, client, source.ID, "member-alice", requester.Email, exact.ExternalID, &requester.ID)
	createQuotaResetMemberDepartment(t, ctx, client, source.ID, requesterMember, exact.ExternalID)
	createQuotaResetMemberInDepartment(t, ctx, client, source.ID, "member-bob", approver, exact.ExternalID)
	createQuotaResetApproverConfig(t, ctx, client, source.ID, exact.ExternalID, exact.Name, approver.ID)
	provider := createQuotaResetRelayProvider(t, ctx, client)
	fake := &fakeQuotaResetProvider{subscriptions: []relay.UserSubscription{activeQuotaResetSubscription(42, "Group Alpha")}}
	svc := NewService(client, fakeProviderResolver(provider.ID, fake), nil, nil)

	request, err := svc.CreateRequest(ctx, CreateRequestInput{RequesterUserID: requester.ID, GroupID: "42", Reason: "Need reset for a build investigation"})
	if err != nil {
		t.Fatalf("CreateRequest() error = %v", err)
	}
	assertQuotaResetEventTypes(t, ctx, client, request.ID,
		quotaresetrequestevent.EventTypeCreated,
		quotaresetrequestevent.EventTypeApproverResolved,
		quotaresetrequestevent.EventTypeWorkflowCreated,
		quotaresetrequestevent.EventTypeStepActivated,
	)
	activation := client.QuotaResetRequestEvent.Query().Where(
		quotaresetrequestevent.RequestIDEQ(request.ID),
		quotaresetrequestevent.EventTypeEQ(quotaresetrequestevent.EventTypeStepActivated),
	).OnlyX(ctx)
	if activation.Metadata["step_index"] != float64(0) || activation.Metadata["step_label"] != "Exact" {
		t.Fatalf("activation metadata = %+v", activation.Metadata)
	}
}

func TestCreateRequestV2RollsBackWhenInitialActivationEventFails(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
	provider := createQuotaResetRelayProvider(t, ctx, client)
	client.QuotaResetRequestEvent.Use(func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, mutation ent.Mutation) (ent.Value, error) {
			if eventMutation, ok := mutation.(*ent.QuotaResetRequestEventMutation); ok {
				if eventType, exists := eventMutation.EventType(); exists && eventType == quotaresetrequestevent.EventTypeStepActivated {
					return nil, errors.New("injected activation audit failure")
				}
			}
			return next.Mutate(ctx, mutation)
		})
	})
	fake := &fakeQuotaResetProvider{subscriptions: []relay.UserSubscription{activeQuotaResetSubscription(42, "Group Alpha")}}
	svc := NewService(client, fakeProviderResolver(provider.ID, fake), nil, nil)

	_, err := svc.CreateRequest(ctx, CreateRequestInput{RequesterUserID: requester.ID, GroupID: "42", Reason: "Need reset for a build investigation"})
	if err == nil || !strings.Contains(err.Error(), "injected activation audit failure") {
		t.Fatalf("CreateRequest() error = %v, want injected activation audit failure", err)
	}
	if requests := client.QuotaResetRequest.Query().CountX(ctx); requests != 0 {
		t.Fatalf("request count = %d, want transaction rollback", requests)
	}
	if events := client.QuotaResetRequestEvent.Query().CountX(ctx); events != 0 {
		t.Fatalf("event count = %d, want transaction rollback", events)
	}
}

func TestCancelV2RollsBackStatusWhenAuditInsertIsCancelled(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
	approver := createQuotaResetUser(t, ctx, client, "bob", "bob@example.org", nil, "user")
	provider := createQuotaResetRelayProvider(t, ctx, client)
	workflow := workflowFixtureForUsers(requester.ID, approver.ID, approver.ID, true)
	workflow.Steps = workflow.Steps[:1]
	request := createPendingWorkflowRequest(t, ctx, client, requester, provider, workflow)
	cancelCtx, cancel := context.WithCancel(ctx)
	client.QuotaResetRequestEvent.Use(func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, mutation ent.Mutation) (ent.Value, error) {
			if eventMutation, ok := mutation.(*ent.QuotaResetRequestEventMutation); ok {
				if eventType, exists := eventMutation.EventType(); exists && eventType == quotaresetrequestevent.EventTypeCancelled {
					cancel()
				}
			}
			return next.Mutate(ctx, mutation)
		})
	})

	_, err := NewService(client, nil, nil, nil).Cancel(cancelCtx, requester.ID, request.ID)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Cancel() error = %v, want context cancellation", err)
	}
	stored := client.QuotaResetRequest.GetX(context.Background(), request.ID)
	if stored.Status != quotaresetrequest.StatusWorkflowPending {
		t.Fatalf("stored status = %s, want workflow_pending after audit rollback", stored.Status)
	}
	if events := client.QuotaResetRequestEvent.Query().Where(
		quotaresetrequestevent.RequestIDEQ(request.ID),
		quotaresetrequestevent.EventTypeEQ(quotaresetrequestevent.EventTypeCancelled),
	).CountX(context.Background()); events != 0 {
		t.Fatalf("cancellation event count = %d, want 0", events)
	}
}

func TestCancelV1PreservesLegacyEventAndPostCommitNotification(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
	provider := createQuotaResetRelayProvider(t, ctx, client)
	request := createPendingQuotaResetRequest(t, ctx, client, requester.ID, 1001, provider.ID, "42", nil)
	recorder := &recordingQuotaResetNotifier{}

	updated, err := NewService(client, nil, nil, recorder).Cancel(ctx, requester.ID, request.ID)
	if err != nil {
		t.Fatalf("Cancel() error = %v", err)
	}
	if updated.Status != quotaresetrequest.StatusCancelled || !containsString(recorder.events, "quota_reset_request_cancelled") {
		t.Fatalf("status/notifications = %s/%v, want cancelled notification", updated.Status, recorder.events)
	}
	event := client.QuotaResetRequestEvent.Query().Where(
		quotaresetrequestevent.RequestIDEQ(request.ID),
		quotaresetrequestevent.EventTypeEQ(quotaresetrequestevent.EventTypeCancelled),
	).OnlyX(ctx)
	if len(event.Metadata) != 0 {
		t.Fatalf("legacy cancellation metadata = %+v, want empty", event.Metadata)
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
		Approvers:             []WorkflowApprover{{UserID: bob.ID, DisplayName: "bob", Email: bob.Email}},
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
	workflow.Steps[0].Approvers = append(workflow.Steps[0].Approvers, WorkflowApprover{UserID: second.ID, DisplayName: second.Username, Email: second.Email})
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

func TestApproveInvalidatesWorkItemCountsBeforeRelayAndResetSuccessDoesNotInvalidateAgain(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	revisions, before := initializeQuotaResetRevisions(t, ctx, client)
	requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
	approver := createQuotaResetUser(t, ctx, client, "lead", "lead@example.com", nil, "user")
	provider := createQuotaResetRelayProvider(t, ctx, client)
	request := createPendingQuotaResetRequest(t, ctx, client, requester.ID, 1001, provider.ID, "42", []int{approver.ID})
	var revisionAtRelay string
	var statusAtRelay quotaresetrequest.Status
	var eventsAtRelay []string
	fake := &fakeQuotaResetProvider{}
	fake.resetFunc = func(_ context.Context, _, _ int64) error {
		revisionAtRelay = currentQuotaResetRevision(t, ctx, revisions)
		statusAtRelay = client.QuotaResetRequest.GetX(ctx, request.ID).Status
		eventsAtRelay = quotaResetEventTypes(t, ctx, client, request.ID)
		return nil
	}
	svc := NewService(client, fakeProviderResolver(provider.ID, fake), NewApproverResolver(client), nil, revisions)

	updated, err := svc.Approve(ctx, DecisionInput{ActorUserID: approver.ID, RequestID: request.ID, DecisionReason: "Approved"})
	if err != nil {
		t.Fatalf("Approve() error = %v", err)
	}
	if updated.Status != quotaresetrequest.StatusApprovedResetSucceeded {
		t.Fatalf("status = %s, want approved_reset_succeeded", updated.Status)
	}
	if revisionAtRelay == "" || revisionAtRelay == before {
		t.Fatalf("revision at Relay = %q, want a revision after %q", revisionAtRelay, before)
	}
	if statusAtRelay != quotaresetrequest.StatusApprovedResetting {
		t.Fatalf("status at Relay = %s, want approved_resetting", statusAtRelay)
	}
	wantAtRelay := []string{
		quotaresetrequestevent.EventTypeApproved.String(),
		quotaresetrequestevent.EventTypeResetStarted.String(),
	}
	if !reflect.DeepEqual(eventsAtRelay, wantAtRelay) {
		t.Fatalf("events at Relay = %#v, want %#v", eventsAtRelay, wantAtRelay)
	}
	if after := currentQuotaResetRevision(t, ctx, revisions); after != revisionAtRelay {
		t.Fatalf("revision after reset success = %q, want unchanged from Relay %q", after, revisionAtRelay)
	}
}

func TestApproveRollsBackPendingExitAndPreventsRelayWhenRevisionInvalidationFails(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	revisions, before := initializeQuotaResetRevisions(t, ctx, client)
	requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
	approver := createQuotaResetUser(t, ctx, client, "lead", "lead@example.com", nil, "user")
	provider := createQuotaResetRelayProvider(t, ctx, client)
	request := createPendingQuotaResetRequest(t, ctx, client, requester.ID, 1001, provider.ID, "42", []int{approver.ID})
	failQuotaResetRevisionUpdates(client, func() bool { return true })
	fake := &fakeQuotaResetProvider{}
	svc := NewService(client, fakeProviderResolver(provider.ID, fake), NewApproverResolver(client), nil, revisions)

	_, err := svc.Approve(ctx, DecisionInput{ActorUserID: approver.ID, RequestID: request.ID, DecisionReason: "Approved"})
	if err == nil || !strings.Contains(err.Error(), "injected revision failure") {
		t.Fatalf("Approve() error = %v, want injected revision failure", err)
	}
	reloaded := client.QuotaResetRequest.GetX(ctx, request.ID)
	if reloaded.Status != quotaresetrequest.StatusPending || reloaded.ApprovedByUserID != nil || reloaded.DecidedAt != nil {
		t.Fatalf("request after rollback = %+v, want unchanged pending decision", reloaded)
	}
	if got := quotaResetEventTypes(t, ctx, client, request.ID); len(got) != 0 {
		t.Fatalf("events after rollback = %#v, want none", got)
	}
	if fake.resetCalls != 0 {
		t.Fatalf("Relay reset calls = %d, want 0", fake.resetCalls)
	}
	if after := currentQuotaResetRevision(t, ctx, revisions); after != before {
		t.Fatalf("revision after rollback = %q, want %q", after, before)
	}
}

func TestRetryResetInvalidatesWorkItemCountsWithEventsBeforeRelay(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	revisions, before := initializeQuotaResetRevisions(t, ctx, client)
	admin := createQuotaResetUser(t, ctx, client, "admin", "admin@example.com", nil, "admin")
	requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
	provider := createQuotaResetRelayProvider(t, ctx, client)
	request := createPendingQuotaResetRequest(t, ctx, client, requester.ID, 1001, provider.ID, "42", nil)
	client.QuotaResetRequest.UpdateOneID(request.ID).
		SetStatus(quotaresetrequest.StatusApprovedResetFailed).
		SetResetError("Synthetic prior reset failure").
		SaveX(ctx)
	var revisionAtRelay string
	var statusAtRelay quotaresetrequest.Status
	var eventsAtRelay []string
	fake := &fakeQuotaResetProvider{}
	fake.resetFunc = func(_ context.Context, _, _ int64) error {
		revisionAtRelay = currentQuotaResetRevision(t, ctx, revisions)
		statusAtRelay = client.QuotaResetRequest.GetX(ctx, request.ID).Status
		eventsAtRelay = quotaResetEventTypes(t, ctx, client, request.ID)
		return nil
	}
	svc := NewService(client, fakeProviderResolver(provider.ID, fake), NewApproverResolver(client), nil, revisions)

	updated, err := svc.RetryReset(ctx, DecisionInput{ActorUserID: admin.ID, RequestID: request.ID, Admin: true})
	if err != nil {
		t.Fatalf("RetryReset() error = %v", err)
	}
	if updated.Status != quotaresetrequest.StatusApprovedResetSucceeded {
		t.Fatalf("status = %s, want approved_reset_succeeded", updated.Status)
	}
	if revisionAtRelay == "" || revisionAtRelay == before {
		t.Fatalf("revision at Relay = %q, want a revision after %q", revisionAtRelay, before)
	}
	if statusAtRelay != quotaresetrequest.StatusApprovedResetting {
		t.Fatalf("status at Relay = %s, want approved_resetting", statusAtRelay)
	}
	wantEvents := []string{
		quotaresetrequestevent.EventTypeResetRetried.String(),
		quotaresetrequestevent.EventTypeResetStarted.String(),
	}
	if !reflect.DeepEqual(eventsAtRelay, wantEvents) {
		t.Fatalf("events at Relay = %#v, want %#v", eventsAtRelay, wantEvents)
	}
	if after := currentQuotaResetRevision(t, ctx, revisions); after != revisionAtRelay {
		t.Fatalf("revision after reset success = %q, want unchanged from Relay %q", after, revisionAtRelay)
	}
}

func TestRetryResetRollsBackFailedExitAndPreventsRelayWhenRevisionInvalidationFails(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	revisions, before := initializeQuotaResetRevisions(t, ctx, client)
	admin := createQuotaResetUser(t, ctx, client, "admin", "admin@example.com", nil, "admin")
	requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
	provider := createQuotaResetRelayProvider(t, ctx, client)
	request := createPendingQuotaResetRequest(t, ctx, client, requester.ID, 1001, provider.ID, "42", nil)
	client.QuotaResetRequest.UpdateOneID(request.ID).
		SetStatus(quotaresetrequest.StatusApprovedResetFailed).
		SetResetError("Synthetic prior reset failure").
		SaveX(ctx)
	failQuotaResetRevisionUpdates(client, func() bool { return true })
	fake := &fakeQuotaResetProvider{}
	svc := NewService(client, fakeProviderResolver(provider.ID, fake), NewApproverResolver(client), nil, revisions)

	_, err := svc.RetryReset(ctx, DecisionInput{ActorUserID: admin.ID, RequestID: request.ID, Admin: true})
	if err == nil || !strings.Contains(err.Error(), "injected revision failure") {
		t.Fatalf("RetryReset() error = %v, want injected revision failure", err)
	}
	reloaded := client.QuotaResetRequest.GetX(ctx, request.ID)
	if reloaded.Status != quotaresetrequest.StatusApprovedResetFailed || reloaded.ResetError != "Synthetic prior reset failure" {
		t.Fatalf("request after rollback = %+v, want prior failed state", reloaded)
	}
	if got := quotaResetEventTypes(t, ctx, client, request.ID); len(got) != 0 {
		t.Fatalf("events after rollback = %#v, want none", got)
	}
	if fake.resetCalls != 0 {
		t.Fatalf("Relay reset calls = %d, want 0", fake.resetCalls)
	}
	if after := currentQuotaResetRevision(t, ctx, revisions); after != before {
		t.Fatalf("revision after rollback = %q, want %q", after, before)
	}
}

func TestStoreResetFailureInvalidatesWorkItemCountsWithFailureEvent(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	revisions, before := initializeQuotaResetRevisions(t, ctx, client)
	admin := createQuotaResetUser(t, ctx, client, "admin", "admin@example.com", nil, "admin")
	requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
	provider := createQuotaResetRelayProvider(t, ctx, client)
	request := createPendingQuotaResetRequest(t, ctx, client, requester.ID, 1001, provider.ID, "42", nil)
	var revisionAtRelay string
	fake := &fakeQuotaResetProvider{}
	fake.resetFunc = func(_ context.Context, _, _ int64) error {
		revisionAtRelay = currentQuotaResetRevision(t, ctx, revisions)
		return errors.New("synthetic relay failure")
	}
	svc := NewService(client, fakeProviderResolver(provider.ID, fake), NewApproverResolver(client), nil, revisions)

	updated, err := svc.Approve(ctx, DecisionInput{ActorUserID: admin.ID, RequestID: request.ID, DecisionReason: "Approved", Admin: true})
	if err != nil {
		t.Fatalf("Approve() error = %v, want stored reset failure", err)
	}
	if updated.Status != quotaresetrequest.StatusApprovedResetFailed {
		t.Fatalf("status = %s, want approved_reset_failed", updated.Status)
	}
	if revisionAtRelay == "" || revisionAtRelay == before {
		t.Fatalf("revision at Relay = %q, want approval revision after %q", revisionAtRelay, before)
	}
	if after := currentQuotaResetRevision(t, ctx, revisions); after == revisionAtRelay {
		t.Fatalf("revision after reset failure = %q, want a second actionable-entry revision", after)
	}
	wantEvents := []string{
		quotaresetrequestevent.EventTypeApproved.String(),
		quotaresetrequestevent.EventTypeResetStarted.String(),
		quotaresetrequestevent.EventTypeResetFailed.String(),
	}
	if got := quotaResetEventTypes(t, ctx, client, request.ID); !reflect.DeepEqual(got, wantEvents) {
		t.Fatalf("event types = %#v, want %#v", got, wantEvents)
	}
}

func TestStoreResetFailureRollsBackFailedEntryAndEventWhenRevisionInvalidationFails(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	revisions, before := initializeQuotaResetRevisions(t, ctx, client)
	admin := createQuotaResetUser(t, ctx, client, "admin", "admin@example.com", nil, "admin")
	requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
	provider := createQuotaResetRelayProvider(t, ctx, client)
	request := createPendingQuotaResetRequest(t, ctx, client, requester.ID, 1001, provider.ID, "42", nil)
	var revisionAtRelay string
	fake := &fakeQuotaResetProvider{}
	fake.resetFunc = func(_ context.Context, _, _ int64) error {
		revisionAtRelay = currentQuotaResetRevision(t, ctx, revisions)
		return errors.New("synthetic relay failure")
	}
	revisionUpdates := failQuotaResetRevisionUpdateNumber(client, 2)
	svc := NewService(client, fakeProviderResolver(provider.ID, fake), NewApproverResolver(client), nil, revisions)

	_, err := svc.Approve(ctx, DecisionInput{ActorUserID: admin.ID, RequestID: request.ID, DecisionReason: "Approved", Admin: true})
	if err == nil || !strings.Contains(err.Error(), "injected revision failure") {
		t.Fatalf("Approve() error = %v, want injected failure while storing reset failure", err)
	}
	if *revisionUpdates != 2 {
		t.Fatalf("revision update attempts = %d, want approval plus failed-entry attempts", *revisionUpdates)
	}
	if revisionAtRelay == "" || revisionAtRelay == before {
		t.Fatalf("revision at Relay = %q, want approval revision after %q", revisionAtRelay, before)
	}
	reloaded := client.QuotaResetRequest.GetX(ctx, request.ID)
	if reloaded.Status != quotaresetrequest.StatusApprovedResetting || reloaded.ResetError != "" {
		t.Fatalf("request after failed failure-store tx = %+v, want approved_resetting", reloaded)
	}
	wantEvents := []string{
		quotaresetrequestevent.EventTypeApproved.String(),
		quotaresetrequestevent.EventTypeResetStarted.String(),
	}
	if got := quotaResetEventTypes(t, ctx, client, request.ID); !reflect.DeepEqual(got, wantEvents) {
		t.Fatalf("event types after rollback = %#v, want %#v", got, wantEvents)
	}
	if after := currentQuotaResetRevision(t, ctx, revisions); after != revisionAtRelay {
		t.Fatalf("revision after rollback = %q, want Relay revision %q", after, revisionAtRelay)
	}
}

func TestStoreResetFailureRollsBackFailedEntryWhenRequiredEventWriteFails(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	revisions, before := initializeQuotaResetRevisions(t, ctx, client)
	admin := createQuotaResetUser(t, ctx, client, "admin", "admin@example.com", nil, "admin")
	requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
	provider := createQuotaResetRelayProvider(t, ctx, client)
	request := createPendingQuotaResetRequest(t, ctx, client, requester.ID, 1001, provider.ID, "42", nil)
	var revisionAtRelay string
	fake := &fakeQuotaResetProvider{}
	fake.resetFunc = func(_ context.Context, _, _ int64) error {
		revisionAtRelay = currentQuotaResetRevision(t, ctx, revisions)
		return errors.New("synthetic relay failure")
	}
	client.QuotaResetRequestEvent.Use(func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, mutation ent.Mutation) (ent.Value, error) {
			eventMutation, ok := mutation.(*ent.QuotaResetRequestEventMutation)
			if ok && mutation.Op().Is(ent.OpCreate) {
				if eventType, exists := eventMutation.EventType(); exists && eventType == quotaresetrequestevent.EventTypeResetFailed {
					return nil, fmt.Errorf("injected reset_failed event failure")
				}
			}
			return next.Mutate(ctx, mutation)
		})
	})
	svc := NewService(client, fakeProviderResolver(provider.ID, fake), NewApproverResolver(client), nil, revisions)

	_, err := svc.Approve(ctx, DecisionInput{ActorUserID: admin.ID, RequestID: request.ID, DecisionReason: "Approved", Admin: true})
	if err == nil || !strings.Contains(err.Error(), "injected reset_failed event failure") {
		t.Fatalf("Approve() error = %v, want required reset_failed event error", err)
	}
	if revisionAtRelay == "" || revisionAtRelay == before {
		t.Fatalf("revision at Relay = %q, want approval revision after %q", revisionAtRelay, before)
	}
	reloaded := client.QuotaResetRequest.GetX(ctx, request.ID)
	if reloaded.Status != quotaresetrequest.StatusApprovedResetting || reloaded.ResetError != "" {
		t.Fatalf("request after failed event tx = %+v, want approved_resetting", reloaded)
	}
	wantEvents := []string{
		quotaresetrequestevent.EventTypeApproved.String(),
		quotaresetrequestevent.EventTypeResetStarted.String(),
	}
	if got := quotaResetEventTypes(t, ctx, client, request.ID); !reflect.DeepEqual(got, wantEvents) {
		t.Fatalf("event types after rollback = %#v, want %#v", got, wantEvents)
	}
	if after := currentQuotaResetRevision(t, ctx, revisions); after != revisionAtRelay {
		t.Fatalf("revision after event rollback = %q, want Relay revision %q", after, revisionAtRelay)
	}
}

func TestResetSucceededEventFailureRollsBackSucceededStateWithoutAnotherInvalidation(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	revisions, before := initializeQuotaResetRevisions(t, ctx, client)
	admin := createQuotaResetUser(t, ctx, client, "admin", "admin@example.com", nil, "admin")
	requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
	provider := createQuotaResetRelayProvider(t, ctx, client)
	request := createPendingQuotaResetRequest(t, ctx, client, requester.ID, 1001, provider.ID, "42", nil)
	var revisionAtRelay string
	fake := &fakeQuotaResetProvider{}
	fake.resetFunc = func(_ context.Context, _, _ int64) error {
		revisionAtRelay = currentQuotaResetRevision(t, ctx, revisions)
		return nil
	}
	client.QuotaResetRequestEvent.Use(func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, mutation ent.Mutation) (ent.Value, error) {
			eventMutation, ok := mutation.(*ent.QuotaResetRequestEventMutation)
			if ok && mutation.Op().Is(ent.OpCreate) {
				if eventType, exists := eventMutation.EventType(); exists && eventType == quotaresetrequestevent.EventTypeResetSucceeded {
					return nil, fmt.Errorf("injected reset_succeeded event failure")
				}
			}
			return next.Mutate(ctx, mutation)
		})
	})
	svc := NewService(client, fakeProviderResolver(provider.ID, fake), NewApproverResolver(client), nil, revisions)

	_, err := svc.Approve(ctx, DecisionInput{ActorUserID: admin.ID, RequestID: request.ID, DecisionReason: "Approved", Admin: true})
	if err == nil || !strings.Contains(err.Error(), "injected reset_succeeded event failure") {
		t.Fatalf("Approve() error = %v, want injected reset_succeeded event failure", err)
	}
	if status := client.QuotaResetRequest.GetX(ctx, request.ID).Status; status != quotaresetrequest.StatusApprovedResetting {
		t.Fatalf("status after event failure = %s, want approved_resetting", status)
	}
	if revisionAtRelay == "" || revisionAtRelay == before {
		t.Fatalf("revision at Relay = %q, want approval revision after %q", revisionAtRelay, before)
	}
	if after := currentQuotaResetRevision(t, ctx, revisions); after != revisionAtRelay {
		t.Fatalf("revision after success event failure = %q, want Relay revision %q", after, revisionAtRelay)
	}
	if got := quotaResetEventTypes(t, ctx, client, request.ID); !reflect.DeepEqual(got, []string{
		quotaresetrequestevent.EventTypeApproved.String(),
		quotaresetrequestevent.EventTypeResetStarted.String(),
	}) {
		t.Fatalf("event types = %#v, want approved and reset_started only", got)
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

func TestAuthenticatedRequestListsProjectPublicWorkflowFields(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
	firstApprover := createQuotaResetUser(t, ctx, client, "bob", "bob@example.org", nil, "user")
	futureApprover := createQuotaResetUser(t, ctx, client, "carol", "carol@example.net", nil, "user")
	provider := createQuotaResetRelayProvider(t, ctx, client)
	workflow := workflowFixtureForUsers(requester.ID, firstApprover.ID, firstApprover.ID, true)
	workflow.Requester.NotificationIDs = map[string]string{"wecom": "private-requester-wecom"}
	workflow.Steps[0].Approvers[0].NotificationIDs = map[string]string{"wecom": "private-current-wecom"}
	workflow.Steps = append(workflow.Steps, WorkflowStep{
		Kind:                  WorkflowStepConfiguredDepartment,
		Label:                 "Company / Security",
		DepartmentExternalIDs: []string{"dept-security"},
		Approvers: []WorkflowApprover{{
			UserID:          futureApprover.ID,
			DisplayName:     "carol",
			Email:           futureApprover.Email,
			NotificationIDs: map[string]string{"wecom": "private-future-wecom"},
		}},
		Status: WorkflowStepQueued,
	})
	if _, err := workflow.Decide(WorkflowDecision{
		ActorUserID:      firstApprover.ID,
		ActorDisplayName: "bob",
		Comment:          "Approved exact department",
		Approve:          true,
		Admin:            true,
		DecidedAt:        time.Date(2026, 7, 16, 8, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("Decide() error = %v", err)
	}
	request := createPendingWorkflowRequest(t, ctx, client, requester, provider, workflow)
	svc := NewService(client, nil, nil, nil)

	responses := map[string]*RequestListResponse{}
	var err error
	responses["mine"], err = svc.ListMine(ctx, requester.ID, ListParams{})
	if err != nil {
		t.Fatalf("ListMine() error = %v", err)
	}
	responses["approvals"], err = svc.ListApprovals(ctx, firstApprover.ID, ListParams{})
	if err != nil {
		t.Fatalf("ListApprovals() error = %v", err)
	}
	responses["admin"], err = svc.ListAdmin(ctx, ListParams{})
	if err != nil {
		t.Fatalf("ListAdmin() error = %v", err)
	}

	for name, response := range responses {
		if response.Total != 1 || len(response.Items) != 1 || response.Items[0].ID != request.ID {
			t.Fatalf("%s response = %+v, want request %d", name, response, request.ID)
		}
		item := response.Items[0]
		if item.CurrentStep == nil || *item.CurrentStep != 2 || len(item.WorkflowSteps) != 3 {
			t.Fatalf("%s workflow progress = %v/%d, want current step 2 of 3", name, item.CurrentStep, len(item.WorkflowSteps))
		}
		if item.WorkflowSteps[2]["label"] != "Company / Security" || item.WorkflowSteps[2]["step_number"] != 3 {
			t.Fatalf("%s future step = %+v", name, item.WorkflowSteps[2])
		}
		if item.WorkflowSteps[1]["satisfied_by_step_number"] != 1 {
			t.Fatalf("%s satisfied_by_step_number = %v, want 1", name, item.WorkflowSteps[1]["satisfied_by_step_number"])
		}

		raw, err := json.Marshal(response)
		if err != nil {
			t.Fatalf("marshal %s response: %v", name, err)
		}
		for _, privateValue := range []string{"private-requester-wecom", "private-current-wecom", "private-future-wecom"} {
			if strings.Contains(string(raw), privateValue) {
				t.Fatalf("%s response leaked %q: %s", name, privateValue, raw)
			}
		}
		var payload map[string]any
		if err := json.Unmarshal(raw, &payload); err != nil {
			t.Fatalf("decode %s response: %v", name, err)
		}
		assertNoJSONFields(t, payload, "notification_ids")
		steps := payload["items"].([]any)[0].(map[string]any)["workflow_steps"].([]any)
		assertJSONKeys(t, steps[2].(map[string]any), "admin_fallback", "label", "status", "step_number")
		assertJSONKeys(t, steps[0].(map[string]any)["decision"].(map[string]any), "actor_display_name", "actor_user_id", "comment", "decided_at")
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

func TestV2ResetFailureRemainsAWorkItemForFinalApprover(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	source := createQuotaResetDirectorySource(t, ctx, client)
	exact := createQuotaResetDepartment(t, ctx, client, source.ID, "dept-exact", "Exact", nil)
	requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
	approver := createQuotaResetUser(t, ctx, client, "bob", "bob@example.org", nil, "user")
	requesterMember := createQuotaResetMember(t, ctx, client, source.ID, "member-alice", requester.Email, exact.ExternalID, &requester.ID)
	createQuotaResetMemberDepartment(t, ctx, client, source.ID, requesterMember, exact.ExternalID)
	createQuotaResetMemberInDepartment(t, ctx, client, source.ID, "member-bob", approver, exact.ExternalID)
	createQuotaResetApproverConfig(t, ctx, client, source.ID, exact.ExternalID, exact.Name, approver.ID)
	provider := createQuotaResetRelayProvider(t, ctx, client)
	fake := &fakeQuotaResetProvider{
		subscriptions: []relay.UserSubscription{activeQuotaResetSubscription(42, "Group Alpha")},
		resetErr:      errors.New("relay timeout"),
	}
	svc := NewService(client, fakeProviderResolver(provider.ID, fake), nil, nil)

	request, err := svc.CreateRequest(ctx, CreateRequestInput{RequesterUserID: requester.ID, GroupID: "42", Reason: "Need reset for a build investigation"})
	if err != nil {
		t.Fatalf("CreateRequest() error = %v", err)
	}
	failed, err := svc.Approve(ctx, DecisionInput{ActorUserID: approver.ID, RequestID: request.ID, DecisionReason: "Approved"})
	if err != nil {
		t.Fatalf("Approve() error = %v", err)
	}
	if failed.Status != quotaresetrequest.StatusApprovedResetFailed || len(failed.ResolvedApproverUserIds) != 0 {
		t.Fatalf("failed request = status %s approvers %v, want failed with cleared active approvers", failed.Status, failed.ResolvedApproverUserIds)
	}
	if failed.ApprovedByUserID == nil || *failed.ApprovedByUserID != approver.ID {
		t.Fatalf("approved_by_user_id = %v, want %d", failed.ApprovedByUserID, approver.ID)
	}

	counts, err := workitems.NewService(client, nil).Counts(ctx, approver.ID, false)
	if err != nil {
		t.Fatalf("Counts() error = %v", err)
	}
	if counts.QuotaResetApprovalCount != 1 || counts.TotalCount != 1 {
		t.Fatalf("counts = %+v, want one actionable retry", counts)
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
	fake := &fakeQuotaResetProvider{resetBlockFor: 2 * time.Second}
	svc := NewService(client, fakeProviderResolver(provider.ID, fake), nil, nil)
	svc.resetExecutionTimeout = time.Second

	started := time.Now()
	updated, err := svc.executeReset(ctx, request.ID, approver.ID, true, false)
	if err != nil {
		t.Fatalf("executeReset() error = %v", err)
	}
	if elapsed := time.Since(started); elapsed >= 1500*time.Millisecond {
		t.Fatalf("executeReset() elapsed = %s, want relay deadline", elapsed)
	}
	if updated.Status != quotaresetrequest.StatusApprovedResetFailed || !strings.Contains(updated.ResetError, context.DeadlineExceeded.Error()) {
		t.Fatalf("status/reset error = %s/%q, want bounded reset failure", updated.Status, updated.ResetError)
	}
}

func TestExecuteResetRollsBackStartWhenEventInsertFails(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
	approver := createQuotaResetUser(t, ctx, client, "bob", "bob@example.org", nil, "user")
	provider := createQuotaResetRelayProvider(t, ctx, client)
	startedAt := time.Date(2026, 7, 15, 8, 0, 0, 0, time.UTC)
	completedAt := startedAt.Add(time.Minute)
	request := createPendingQuotaResetRequest(t, ctx, client, requester.ID, 1001, provider.ID, "42", []int{approver.ID})
	request = client.QuotaResetRequest.UpdateOneID(request.ID).
		SetStatus(quotaresetrequest.StatusApprovedResetFailed).
		SetResetError("previous relay failure").
		SetResetStartedAt(startedAt).
		SetResetCompletedAt(completedAt).
		SaveX(ctx)
	failQuotaResetEventInsert(client, quotaresetrequestevent.EventTypeResetStarted, "injected reset-started audit failure")
	fake := &fakeQuotaResetProvider{}

	_, err := NewService(client, fakeProviderResolver(provider.ID, fake), nil, nil).
		executeReset(ctx, request.ID, approver.ID, true, false)
	if err == nil || !strings.Contains(err.Error(), "injected reset-started audit failure") {
		t.Fatalf("executeReset() error = %v, want reset-started audit failure", err)
	}
	stored := client.QuotaResetRequest.GetX(ctx, request.ID)
	if stored.Status != request.Status || stored.ResetError != request.ResetError ||
		!stored.ResetStartedAt.Equal(*request.ResetStartedAt) || !stored.ResetCompletedAt.Equal(*request.ResetCompletedAt) {
		t.Fatalf("stored reset state = %+v, want unchanged %+v", stored, request)
	}
	if fake.resetCalls != 0 || quotaResetEventCount(ctx, client, request.ID, quotaresetrequestevent.EventTypeResetRetried) != 0 {
		t.Fatalf("reset calls/retry events = %d/%d, want 0/0", fake.resetCalls, quotaResetEventCount(ctx, client, request.ID, quotaresetrequestevent.EventTypeResetRetried))
	}
}

func TestExecuteResetStoresFailureWhenInitialStartAuditFails(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
	approver := createQuotaResetUser(t, ctx, client, "bob", "bob@example.org", nil, "user")
	provider := createQuotaResetRelayProvider(t, ctx, client)
	request := createPendingQuotaResetRequest(t, ctx, client, requester.ID, 1001, provider.ID, "42", []int{approver.ID})
	request = client.QuotaResetRequest.UpdateOneID(request.ID).SetStatus(quotaresetrequest.StatusApprovedResetting).SaveX(ctx)
	failQuotaResetEventInsert(client, quotaresetrequestevent.EventTypeResetStarted, "injected reset-started audit failure")
	fake := &fakeQuotaResetProvider{}

	updated, err := NewService(client, fakeProviderResolver(provider.ID, fake), nil, nil).
		executeReset(ctx, request.ID, approver.ID, false, false)
	if err != nil {
		t.Fatalf("executeReset() error = %v", err)
	}
	if updated.Status != quotaresetrequest.StatusApprovedResetFailed || !strings.Contains(updated.ResetError, "injected reset-started audit failure") {
		t.Fatalf("status/reset error = %s/%q, want failed/start audit error", updated.Status, updated.ResetError)
	}
	if fake.resetCalls != 0 || quotaResetEventCount(ctx, client, request.ID, quotaresetrequestevent.EventTypeResetFailed) != 1 {
		t.Fatalf("reset calls/failure events = %d/%d, want 0/1", fake.resetCalls, quotaResetEventCount(ctx, client, request.ID, quotaresetrequestevent.EventTypeResetFailed))
	}
}

func TestExecuteResetRollsBackSuccessWhenEventInsertFails(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
	approver := createQuotaResetUser(t, ctx, client, "bob", "bob@example.org", nil, "user")
	provider := createQuotaResetRelayProvider(t, ctx, client)
	request := createPendingQuotaResetRequest(t, ctx, client, requester.ID, 1001, provider.ID, "42", []int{approver.ID})
	request = client.QuotaResetRequest.UpdateOneID(request.ID).SetStatus(quotaresetrequest.StatusApprovedResetting).SaveX(ctx)
	failQuotaResetEventInsert(client, quotaresetrequestevent.EventTypeResetSucceeded, "injected reset-succeeded audit failure")
	fake := &fakeQuotaResetProvider{}
	notifier := &recordingQuotaResetNotifier{}

	_, err := NewService(client, fakeProviderResolver(provider.ID, fake), nil, notifier).
		executeReset(ctx, request.ID, approver.ID, false, false)
	if err == nil || !strings.Contains(err.Error(), "injected reset-succeeded audit failure") {
		t.Fatalf("executeReset() error = %v, want reset-succeeded audit failure", err)
	}
	stored := client.QuotaResetRequest.GetX(ctx, request.ID)
	if stored.Status != quotaresetrequest.StatusApprovedResetting || stored.ResetCompletedAt != nil {
		t.Fatalf("stored status/completed_at = %s/%v, want resetting/nil", stored.Status, stored.ResetCompletedAt)
	}
	if fake.resetCalls != 1 || len(notifier.events) != 0 || quotaResetEventCount(ctx, client, request.ID, quotaresetrequestevent.EventTypeResetSucceeded) != 0 {
		t.Fatalf("reset calls/notifications/success events = %d/%v/%d, want 1/[]/0", fake.resetCalls, notifier.events, quotaResetEventCount(ctx, client, request.ID, quotaresetrequestevent.EventTypeResetSucceeded))
	}
}

func TestExecuteResetRollsBackFailureWhenEventInsertFails(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
	approver := createQuotaResetUser(t, ctx, client, "bob", "bob@example.org", nil, "user")
	provider := createQuotaResetRelayProvider(t, ctx, client)
	request := createPendingQuotaResetRequest(t, ctx, client, requester.ID, 1001, provider.ID, "42", []int{approver.ID})
	request = client.QuotaResetRequest.UpdateOneID(request.ID).SetStatus(quotaresetrequest.StatusApprovedResetting).SaveX(ctx)
	failQuotaResetEventInsert(client, quotaresetrequestevent.EventTypeResetFailed, "injected reset-failed audit failure")
	fake := &fakeQuotaResetProvider{resetErr: errors.New("relay timeout")}
	notifier := &recordingQuotaResetNotifier{}

	_, err := NewService(client, fakeProviderResolver(provider.ID, fake), nil, notifier).
		executeReset(ctx, request.ID, approver.ID, false, false)
	if err == nil || !strings.Contains(err.Error(), "injected reset-failed audit failure") {
		t.Fatalf("executeReset() error = %v, want reset-failed audit failure", err)
	}
	stored := client.QuotaResetRequest.GetX(ctx, request.ID)
	if stored.Status != quotaresetrequest.StatusApprovedResetting || stored.ResetError != "" || stored.ResetCompletedAt != nil {
		t.Fatalf("stored status/error/completed_at = %s/%q/%v, want resetting/empty/nil", stored.Status, stored.ResetError, stored.ResetCompletedAt)
	}
	if fake.resetCalls != 1 || len(notifier.events) != 0 || quotaResetEventCount(ctx, client, request.ID, quotaresetrequestevent.EventTypeResetFailed) != 0 {
		t.Fatalf("reset calls/notifications/failure events = %d/%v/%d, want 1/[]/0", fake.resetCalls, notifier.events, quotaResetEventCount(ctx, client, request.ID, quotaresetrequestevent.EventTypeResetFailed))
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
	resetFunc     func(context.Context, int64, int64) error
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
	resetFunc := f.resetFunc
	f.mu.Unlock()
	if resetFunc != nil {
		return resetFunc(ctx, relayUserID, groupID)
	}
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
		},
		Steps: []WorkflowStep{
			{
				Kind:                  WorkflowStepRequesterDepartments,
				Label:                 "Company / Group Alpha",
				DepartmentExternalIDs: []string{"dept-alpha"},
				Approvers:             []WorkflowApprover{{UserID: firstApproverID, DisplayName: "bob", Email: "bob@example.org"}},
				Status:                WorkflowStepActive,
			},
			{
				Kind:                  WorkflowStepConfiguredDepartment,
				Label:                 "Company / Group Beta",
				DepartmentExternalIDs: []string{"dept-beta"},
				Approvers:             []WorkflowApprover{{UserID: secondApproverID, DisplayName: "carol", Email: "carol@example.org"}},
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

func assertJSONKeys(t *testing.T, value map[string]any, want ...string) {
	t.Helper()
	got := make([]string, 0, len(value))
	for key := range value {
		got = append(got, key)
	}
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("JSON keys = %v, want %v", got, want)
	}
}

func assertQuotaResetEventTypes(t *testing.T, ctx context.Context, client *ent.Client, requestID int, want ...quotaresetrequestevent.EventType) {
	t.Helper()
	events := client.QuotaResetRequestEvent.Query().Where(quotaresetrequestevent.RequestIDEQ(requestID)).AllX(ctx)
	got := make(map[quotaresetrequestevent.EventType]int, len(events))
	for _, event := range events {
		got[event.EventType]++
	}
	wantCounts := make(map[quotaresetrequestevent.EventType]int, len(want))
	for _, eventType := range want {
		wantCounts[eventType]++
	}
	if !reflect.DeepEqual(got, wantCounts) {
		t.Fatalf("event types = %v, want %v", got, wantCounts)
	}
}

func failQuotaResetEventInsert(client *ent.Client, eventType quotaresetrequestevent.EventType, message string) {
	client.QuotaResetRequestEvent.Use(func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, mutation ent.Mutation) (ent.Value, error) {
			if eventMutation, ok := mutation.(*ent.QuotaResetRequestEventMutation); ok {
				if got, exists := eventMutation.EventType(); exists && got == eventType {
					return nil, errors.New(message)
				}
			}
			return next.Mutate(ctx, mutation)
		})
	})
}

func initializeQuotaResetRevisions(t *testing.T, ctx context.Context, client *ent.Client) (*workitems.RevisionStore, string) {
	t.Helper()
	revisions := workitems.NewRevisionStore(client)
	if err := revisions.Ensure(ctx); err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	return revisions, currentQuotaResetRevision(t, ctx, revisions)
}

func currentQuotaResetRevision(t *testing.T, ctx context.Context, revisions *workitems.RevisionStore) string {
	t.Helper()
	revision, err := revisions.Current(ctx)
	if err != nil {
		t.Fatalf("Current() error = %v", err)
	}
	return revision
}

func failQuotaResetRevisionUpdates(client *ent.Client, enabled func() bool) {
	client.SystemSetting.Use(func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, mutation ent.Mutation) (ent.Value, error) {
			settingMutation, ok := mutation.(*ent.SystemSettingMutation)
			if ok {
				if _, valueChanged := settingMutation.Value(); mutation.Op().Is(ent.OpUpdate) && valueChanged && enabled() {
					return nil, fmt.Errorf("injected revision failure")
				}
			}
			return next.Mutate(ctx, mutation)
		})
	})
}

func quotaResetEventCount(ctx context.Context, client *ent.Client, requestID int, eventType quotaresetrequestevent.EventType) int {
	return client.QuotaResetRequestEvent.Query().Where(
		quotaresetrequestevent.RequestIDEQ(requestID),
		quotaresetrequestevent.EventTypeEQ(eventType),
	).CountX(ctx)
}

func failQuotaResetRevisionUpdateNumber(client *ent.Client, failAt int) *int {
	updates := 0
	client.SystemSetting.Use(func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, mutation ent.Mutation) (ent.Value, error) {
			settingMutation, ok := mutation.(*ent.SystemSettingMutation)
			if ok {
				if _, valueChanged := settingMutation.Value(); mutation.Op().Is(ent.OpUpdate) && valueChanged {
					updates++
					if updates == failAt {
						return nil, fmt.Errorf("injected revision failure on update %d", updates)
					}
				}
			}
			return next.Mutate(ctx, mutation)
		})
	})
	return &updates
}

func quotaResetEventTypes(t *testing.T, ctx context.Context, client *ent.Client, requestID int) []string {
	t.Helper()
	return client.QuotaResetRequestEvent.Query().
		Where(quotaresetrequestevent.RequestIDEQ(requestID)).
		Order(ent.Asc(quotaresetrequestevent.FieldID)).
		Select(quotaresetrequestevent.FieldEventType).
		StringsX(ctx)
}
