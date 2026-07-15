package quotareset

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/ai-efficiency/backend/ent"
	entcredential "github.com/ai-efficiency/backend/ent/credential"
	"github.com/ai-efficiency/backend/ent/quotaresetnotificationsetting"
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
	createPendingWorkflowDirectoryFixture(t, ctx, client, requester)
	provider := createQuotaResetRelayProvider(t, ctx, client)
	fake := &fakeQuotaResetProvider{subscriptions: []relay.UserSubscription{activeQuotaResetSubscription(42, "Group Alpha")}}
	svc := NewService(client, fakeProviderResolver(provider.ID, fake), NewApproverResolver(client), nil)

	req, err := svc.CreateRequest(ctx, CreateRequestInput{RequesterUserID: requester.ID, GroupID: "42", Reason: "Need reset for a build investigation"})
	if err != nil {
		t.Fatalf("CreateRequest() error = %v", err)
	}
	if req.Status.String() != "pending" || req.RequesterRelayUserID != 1001 || req.GroupID != "42" {
		t.Fatalf("request = %+v", req)
	}
	if count := client.QuotaResetRequestEvent.Query().CountX(ctx); count != 2 {
		t.Fatalf("event count = %d, want workflow snapshot and activation", count)
	}
}

func TestCreateRequestRejectsDuplicateActiveRequest(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
	createPendingWorkflowDirectoryFixture(t, ctx, client, requester)
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

	updated, err := svc.Approve(ctx, DecisionInput{ActorUserID: approver.ID, RequestID: request.ID})
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

	updated, err := svc.Approve(ctx, DecisionInput{ActorUserID: admin.ID, RequestID: request.ID, Admin: true})
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

	updated, err := svc.Approve(ctx, DecisionInput{ActorUserID: admin.ID, RequestID: request.ID, Admin: true})
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

func TestResetTerminalOutcomeSurvivesCallerCancellation(t *testing.T) {
	type contextKey string
	const resetContextValue contextKey = "reset-context-value"

	for _, tc := range quotaResetTerminalFailureCases() {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newQuotaResetTerminalFailureFixture(t, tc.resetErr)
			providerStarted := make(chan context.Context, 1)
			providerRelease := make(chan struct{})
			fixture.provider.resetFn = func(ctx context.Context, _, _ int64) error {
				providerStarted <- ctx
				<-providerRelease
				return tc.resetErr
			}

			callerCtx, cancel := context.WithCancel(context.WithValue(fixture.ctx, resetContextValue, "preserved"))
			defer cancel()
			type resetResult struct {
				request *ent.QuotaResetRequest
				err     error
			}
			resultCh := make(chan resetResult, 1)
			go func() {
				request, err := fixture.service.executeReset(callerCtx, fixture.request.ID, fixture.actor.ID, false, false)
				resultCh <- resetResult{request: request, err: err}
			}()

			providerCtx := <-providerStarted
			_, hasDeadline := providerCtx.Deadline()
			preservedValue := providerCtx.Value(resetContextValue)
			cancel()
			<-callerCtx.Done()
			providerCancelled := false
			select {
			case <-providerCtx.Done():
				providerCancelled = true
			default:
			}
			close(providerRelease)
			result := <-resultCh

			if result.err != nil {
				t.Fatalf("executeReset() error = %v, want terminal outcome persisted", result.err)
			}
			if !hasDeadline {
				t.Fatal("provider context has no deadline")
			}
			if preservedValue != "preserved" {
				t.Fatalf("provider context value = %v, want preserved", preservedValue)
			}
			if providerCancelled {
				t.Fatal("provider context was cancelled with the original caller")
			}
			if !errors.Is(callerCtx.Err(), context.Canceled) {
				t.Fatalf("caller context error = %v, want context.Canceled", callerCtx.Err())
			}
			if result.request == nil || result.request.Status != tc.status || result.request.ResetCompletedAt == nil {
				t.Fatalf("terminal request = %+v, want status %s with completion time", result.request, tc.status)
			}
			if tc.resetErr != nil && !strings.Contains(result.request.ResetError, tc.resetErr.Error()) {
				t.Fatalf("reset error = %q, want %q", result.request.ResetError, tc.resetErr)
			}
			if count := fixture.client.QuotaResetRequestEvent.Query().
				Where(
					quotaresetrequestevent.RequestIDEQ(fixture.request.ID),
					quotaresetrequestevent.EventTypeEQ(tc.eventType),
				).
				CountX(fixture.ctx); count != 1 {
				t.Fatalf("%s event count = %d, want 1", tc.eventType, count)
			}
		})
	}
}

func TestAlreadyRunningResetSurvivesCallerCancellationBeforeProvider(t *testing.T) {
	fixture := newQuotaResetTerminalFailureFixture(t, nil)
	callerCtx, cancel := context.WithCancel(fixture.ctx)
	cancel()

	updated, err := fixture.service.executeReset(callerCtx, fixture.request.ID, fixture.actor.ID, false, false)
	if err != nil {
		t.Fatalf("executeReset() error = %v, want already-running reset to complete", err)
	}
	if updated.Status != quotaresetrequest.StatusApprovedResetSucceeded || updated.ResetCompletedAt == nil {
		t.Fatalf("terminal request = %+v, want succeeded with completion time", updated)
	}
	if fixture.provider.resetCalls != 1 {
		t.Fatalf("reset calls = %d, want 1", fixture.provider.resetCalls)
	}
	if count := fixture.client.QuotaResetRequestEvent.Query().
		Where(
			quotaresetrequestevent.RequestIDEQ(fixture.request.ID),
			quotaresetrequestevent.EventTypeEQ(quotaresetrequestevent.EventTypeResetSucceeded),
		).
		CountX(fixture.ctx); count != 1 {
		t.Fatalf("reset_succeeded event count = %d, want 1", count)
	}
}

func TestResetSuccessAuditFailureRollsBackTerminalOutcome(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
	actor := createQuotaResetUser(t, ctx, client, "approver", "approver@example.com", nil, "user")
	provider := createQuotaResetRelayProvider(t, ctx, client)
	request := createPendingQuotaResetRequest(t, ctx, client, requester.ID, 1001, provider.ID, "42", []int{actor.ID})
	client.QuotaResetRequest.UpdateOneID(request.ID).
		SetStatus(quotaresetrequest.StatusApprovedResetting).
		SaveX(ctx)
	createEnabledQuotaResetNotificationSetting(t, ctx, client)
	notifier := &countingQuotaResetNotifier{}
	fake := &fakeQuotaResetProvider{}
	svc := NewService(client, fakeProviderResolver(provider.ID, fake), NewApproverResolver(client), notifier)
	injectedErr := errors.New("injected reset_succeeded event failure")
	injectQuotaResetEventFailure(client, quotaresetrequestevent.EventTypeResetSucceeded, injectedErr)

	updated, err := svc.executeReset(ctx, request.ID, actor.ID, false, false)
	if updated != nil || !errors.Is(err, injectedErr) || !strings.Contains(err.Error(), "persist reset outcome") {
		t.Fatalf("executeReset() = %+v, %v, want nil summary with injected persistence error", updated, err)
	}
	assertQuotaResetOutcomeRolledBack(t, ctx, client, request.ID, quotaresetrequestevent.EventTypeResetSucceeded)
	if fake.resetCalls != 1 {
		t.Fatalf("reset calls = %d, want 1", fake.resetCalls)
	}
	if notifier.calls != 0 {
		t.Fatalf("result notification calls = %d, want 0", notifier.calls)
	}
}

func TestResetFailureAuditFailureRollsBackTerminalOutcome(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
	actor := createQuotaResetUser(t, ctx, client, "approver", "approver@example.com", nil, "user")
	provider := createQuotaResetRelayProvider(t, ctx, client)
	request := createPendingQuotaResetRequest(t, ctx, client, requester.ID, 1001, provider.ID, "42", []int{actor.ID})
	client.QuotaResetRequest.UpdateOneID(request.ID).
		SetStatus(quotaresetrequest.StatusApprovedResetting).
		SaveX(ctx)
	createEnabledQuotaResetNotificationSetting(t, ctx, client)
	notifier := &countingQuotaResetNotifier{}
	fake := &fakeQuotaResetProvider{resetErr: errors.New("relay timeout")}
	svc := NewService(client, fakeProviderResolver(provider.ID, fake), NewApproverResolver(client), notifier)
	injectedErr := errors.New("injected reset_failed event failure")
	injectQuotaResetEventFailure(client, quotaresetrequestevent.EventTypeResetFailed, injectedErr)

	updated, err := svc.executeReset(ctx, request.ID, actor.ID, false, false)
	if updated != nil || !errors.Is(err, injectedErr) || !strings.Contains(err.Error(), "persist reset outcome") {
		t.Fatalf("executeReset() = %+v, %v, want nil summary with injected persistence error", updated, err)
	}
	assertQuotaResetOutcomeRolledBack(t, ctx, client, request.ID, quotaresetrequestevent.EventTypeResetFailed)
	if fake.resetCalls != 1 {
		t.Fatalf("reset calls = %d, want 1", fake.resetCalls)
	}
	if notifier.calls != 0 {
		t.Fatalf("result notification calls = %d, want 0", notifier.calls)
	}
}

func TestResetTerminalUpdateFailureRollsBackOutcome(t *testing.T) {
	for _, tc := range quotaResetTerminalFailureCases() {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newQuotaResetTerminalFailureFixture(t, tc.resetErr)
			injectedErr := errors.New("injected terminal request update failure")
			fixture.client.QuotaResetRequest.Use(func(next ent.Mutator) ent.Mutator {
				return ent.MutateFunc(func(ctx context.Context, mutation ent.Mutation) (ent.Value, error) {
					requestMutation, ok := mutation.(*ent.QuotaResetRequestMutation)
					if ok && mutation.Op().Is(ent.OpUpdateOne) {
						if status, exists := requestMutation.Status(); exists && status == tc.status {
							return nil, injectedErr
						}
					}
					return next.Mutate(ctx, mutation)
				})
			})

			updated, err := fixture.service.executeReset(fixture.ctx, fixture.request.ID, fixture.actor.ID, false, false)
			if updated != nil || !errors.Is(err, injectedErr) || !strings.Contains(err.Error(), "persist reset outcome: update request") {
				t.Fatalf("executeReset() = %+v, %v, want nil summary with wrapped update persistence error", updated, err)
			}
			assertQuotaResetTerminalFailureRolledBack(t, fixture, tc.eventType)
		})
	}
}

func TestResetCommitFailureRollsBackUpdatedOutcomeAndCreatedEvent(t *testing.T) {
	for _, tc := range quotaResetTerminalFailureCases() {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newQuotaResetTerminalFailureFixture(t, tc.resetErr)
			txCtx, cancel := context.WithCancel(fixture.ctx)
			defer cancel()
			eventCreated := false
			fixture.client.QuotaResetRequestEvent.Use(func(next ent.Mutator) ent.Mutator {
				return ent.MutateFunc(func(ctx context.Context, mutation ent.Mutation) (ent.Value, error) {
					eventMutation, ok := mutation.(*ent.QuotaResetRequestEventMutation)
					if !ok || !mutation.Op().Is(ent.OpCreate) {
						return next.Mutate(ctx, mutation)
					}
					eventType, exists := eventMutation.EventType()
					if !exists || eventType != tc.eventType {
						return next.Mutate(ctx, mutation)
					}
					value, err := next.Mutate(ctx, mutation)
					if err != nil {
						return nil, err
					}
					eventCreated = true
					cancel()
					return value, nil
				})
			})

			errorMessage := ""
			if tc.resetErr != nil {
				errorMessage = tc.resetErr.Error()
			}
			updated, err := fixture.service.persistResetOutcome(
				txCtx,
				fixture.request.ID,
				fixture.actor.ID,
				tc.status,
				tc.eventType,
				errorMessage,
			)
			if !eventCreated {
				t.Fatal("terminal event mutation did not succeed before commit failure")
			}
			commitFailureWrapped := errors.Is(err, context.Canceled) || errors.Is(err, sql.ErrTxDone)
			if updated != nil || !commitFailureWrapped || !strings.Contains(err.Error(), "persist reset outcome: commit transaction") {
				t.Fatalf("executeReset() = %+v, %v, want nil summary with wrapped commit persistence error", updated, err)
			}
			assertQuotaResetOutcomeRolledBack(t, fixture.ctx, fixture.client, fixture.request.ID, tc.eventType)
			if fixture.provider.resetCalls != 0 {
				t.Fatalf("reset calls = %d, want 0 for direct persistence test", fixture.provider.resetCalls)
			}
			if fixture.notifier.calls != 0 {
				t.Fatalf("result notification calls = %d, want 0", fixture.notifier.calls)
			}
		})
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

	updated, err := svc.Approve(ctx, DecisionInput{ActorUserID: admin.ID, RequestID: request.ID, Admin: true})
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
	_, err := svc.Approve(ctx, DecisionInput{ActorUserID: admin.ID, RequestID: request.ID, Admin: true})
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

func TestUpdateNotificationSettingsValidatesEnabledURLAndBearerCredential(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	svc := NewService(client, nil, nil, nil)
	invalidURL := "ftp://hooks.example.com/quota-reset"

	_, err := svc.UpdateNotificationSettings(ctx, UpdateNotificationSettingsInput{
		ActorUserID: 1,
		Enabled:     true,
		ChannelType: "generic_webhook",
		URL:         &invalidURL,
		AuthType:    "none",
	})
	if err == nil || !strings.Contains(err.Error(), "invalid webhook URL") {
		t.Fatalf("invalid URL error = %v, want invalid webhook URL", err)
	}

	endpoint := "https://hooks.example.com/quota-reset"
	_, err = svc.UpdateNotificationSettings(ctx, UpdateNotificationSettingsInput{
		ActorUserID: 1,
		Enabled:     true,
		ChannelType: "generic_webhook",
		URL:         &endpoint,
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
		ChannelType:  "generic_webhook",
		URL:          &endpoint,
		AuthType:     "bearer_token",
		CredentialID: &wrongKind.ID,
	})
	if err == nil || !strings.Contains(err.Error(), "must be secret_text") {
		t.Fatalf("wrong credential kind error = %v, want secret_text error", err)
	}
}

func TestNotificationSettingsRequiresExplicitChannelType(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	svc := NewService(client, nil, nil, nil)
	endpoint := "https://hooks.example.com/quota-reset"

	_, err := svc.UpdateNotificationSettings(ctx, UpdateNotificationSettingsInput{
		ActorUserID: 1,
		Enabled:     false,
		URL:         &endpoint,
		AuthType:    "none",
	})
	if !errors.Is(err, ErrInvalidNotification) || !strings.Contains(err.Error(), "channel_type is required") {
		t.Fatalf("UpdateNotificationSettings() error = %v, want explicit channel_type error", err)
	}
}

func TestNotificationSettingsValidatesWeComEndpointAndDisallowsBearer(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	svc := NewService(client, nil, nil, nil)
	wrongEndpoint := "https://qyapi.weixin.qq.com/cgi-bin/webhook/other?key=synthetic-key"

	_, err := svc.UpdateNotificationSettings(ctx, UpdateNotificationSettingsInput{
		ActorUserID: 1,
		Enabled:     false,
		ChannelType: "wecom_group_robot",
		URL:         &wrongEndpoint,
		AuthType:    "none",
	})
	if !errors.Is(err, ErrInvalidNotification) || !strings.Contains(err.Error(), "Enterprise WeChat") {
		t.Fatalf("wrong WeCom endpoint error = %v, want Enterprise WeChat endpoint error", err)
	}

	robotURL := "https://qyapi.weixin.qq.com/cgi-bin/webhook/send" + "?" + "key=test-secret"
	_, err = svc.UpdateNotificationSettings(ctx, UpdateNotificationSettingsInput{
		ActorUserID: 1,
		Enabled:     false,
		ChannelType: "wecom_group_robot",
		URL:         &robotURL,
		AuthType:    "bearer_token",
	})
	if !errors.Is(err, ErrInvalidNotification) || !strings.Contains(err.Error(), "does not use bearer auth") {
		t.Fatalf("WeCom bearer error = %v, want bearer prohibition", err)
	}
}

func TestNotificationSettingsRejectsHTTPWeComRobotEndpoint(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	svc := NewService(client, nil, nil, nil)
	robotURL := "http://qyapi.weixin.qq.com/cgi-bin/webhook/send" + "?" + "key=test-secret"

	_, err := svc.UpdateNotificationSettings(ctx, UpdateNotificationSettingsInput{
		ActorUserID: 1,
		Enabled:     false,
		ChannelType: "wecom_group_robot",
		URL:         &robotURL,
		AuthType:    "none",
	})
	if !errors.Is(err, ErrInvalidNotification) || !strings.Contains(err.Error(), "invalid Enterprise WeChat") {
		t.Fatalf("HTTP WeCom endpoint error = %v, want invalid Enterprise WeChat endpoint", err)
	}
}

func TestNotificationSettingsRejectsWeComRobotEndpointForGenericWebhook(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	svc := NewService(client, nil, nil, nil)
	robotURL := "https://qyapi.weixin.qq.com/cgi-bin/webhook/send" + "?" + "key=test-secret"

	_, err := svc.UpdateNotificationSettings(ctx, UpdateNotificationSettingsInput{
		ActorUserID: 1,
		Enabled:     true,
		ChannelType: "generic_webhook",
		URL:         &robotURL,
		AuthType:    "none",
	})
	if !errors.Is(err, ErrInvalidNotification) || !strings.Contains(err.Error(), "use the Enterprise WeChat group robot preset") {
		t.Fatalf("generic WeCom endpoint error = %v, want WeCom preset guidance", err)
	}
}

func TestNotificationSettingsReadRedactsRobotKey(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	robotURL := "https://qyapi.weixin.qq.com/cgi-bin/webhook/send" + "?" + "key=test-secret"
	client.QuotaResetNotificationSetting.Create().
		SetEnabled(true).
		SetChannelType("wecom_group_robot").
		SetChannelTypeConfigured(true).
		SetURL(robotURL).
		SetAuthType("none").
		SetCreatedByUserID(1).
		SetUpdatedByUserID(1).
		SaveX(ctx)

	settings, err := NewService(client, nil, nil, nil).GetNotificationSettings(ctx)
	if err != nil {
		t.Fatalf("GetNotificationSettings() error = %v", err)
	}
	if !settings.URLConfigured || settings.URLPreview != "https://qyapi.weixin.qq.com/cgi-bin/webhook/send" {
		t.Fatalf("settings = %+v, want redacted configured robot URL", settings)
	}
	if strings.Contains(fmt.Sprintf("%+v", settings), "test-secret") {
		t.Fatalf("settings leaked robot key: %+v", settings)
	}
}

func TestNotificationSettingsReadRedactsGenericWebhookPath(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	endpoint := "https://hooks.example.com/services/synthetic-id/synthetic-token"
	client.QuotaResetNotificationSetting.Create().
		SetEnabled(true).
		SetChannelType("generic_webhook").
		SetChannelTypeConfigured(true).
		SetURL(endpoint).
		SetAuthType("none").
		SetCreatedByUserID(1).
		SetUpdatedByUserID(1).
		SaveX(ctx)

	settings, err := NewService(client, nil, nil, nil).GetNotificationSettings(ctx)
	if err != nil {
		t.Fatalf("GetNotificationSettings() error = %v", err)
	}
	if settings.URLPreview != "https://hooks.example.com" {
		t.Fatalf("URL preview = %q, want host-only generic preview", settings.URLPreview)
	}
	if strings.Contains(fmt.Sprintf("%+v", settings), "synthetic-token") {
		t.Fatalf("settings leaked generic webhook path token: %+v", settings)
	}
}

func TestNotificationSettingsChannelSwitchRequiresReplacementURL(t *testing.T) {
	tests := []struct {
		name        string
		fromChannel string
		fromURL     string
		toChannel   string
	}{
		{
			name:        "Enterprise WeChat to generic",
			fromChannel: "wecom_group_robot",
			fromURL:     "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=synthetic-old-key",
			toChannel:   "generic_webhook",
		},
		{
			name:        "generic to Enterprise WeChat",
			fromChannel: "generic_webhook",
			fromURL:     "https://hooks.example.com/quota-reset?token=synthetic-old-token",
			toChannel:   "wecom_group_robot",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			client := testdb.Open(t)
			svc := NewService(client, nil, nil, nil)
			if _, err := svc.UpdateNotificationSettings(ctx, UpdateNotificationSettingsInput{
				ActorUserID: 1,
				Enabled:     true,
				ChannelType: tt.fromChannel,
				URL:         &tt.fromURL,
				AuthType:    "none",
			}); err != nil {
				t.Fatalf("initial UpdateNotificationSettings() error = %v", err)
			}

			_, err := svc.UpdateNotificationSettings(ctx, UpdateNotificationSettingsInput{
				ActorUserID: 2,
				Enabled:     true,
				ChannelType: tt.toChannel,
				AuthType:    "none",
			})
			if !errors.Is(err, ErrInvalidNotification) || !strings.Contains(err.Error(), "replacement webhook URL is required when channel_type changes") {
				t.Fatalf("UpdateNotificationSettings(channel switch) error = %v, want replacement URL requirement", err)
			}
			row := client.QuotaResetNotificationSetting.Query().OnlyX(ctx)
			if row.ChannelType.String() != tt.fromChannel || row.URL != tt.fromURL {
				t.Fatalf("stored settings after rejected switch = %s/%q, want %s/%q", row.ChannelType, row.URL, tt.fromChannel, tt.fromURL)
			}
		})
	}
}

func TestNotificationSettingsChannelSwitchAcceptsReplacementURL(t *testing.T) {
	tests := []struct {
		name        string
		fromChannel string
		fromURL     string
		toChannel   string
		toURL       string
	}{
		{
			name:        "Enterprise WeChat to generic",
			fromChannel: "wecom_group_robot",
			fromURL:     "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=synthetic-old-key",
			toChannel:   "generic_webhook",
			toURL:       "https://hooks.example.com/quota-reset?token=synthetic-new-token",
		},
		{
			name:        "generic to Enterprise WeChat",
			fromChannel: "generic_webhook",
			fromURL:     "https://hooks.example.com/quota-reset?token=synthetic-old-token",
			toChannel:   "wecom_group_robot",
			toURL:       "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=synthetic-new-key",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			client := testdb.Open(t)
			svc := NewService(client, nil, nil, nil)
			if _, err := svc.UpdateNotificationSettings(ctx, UpdateNotificationSettingsInput{
				ActorUserID: 1,
				Enabled:     true,
				ChannelType: tt.fromChannel,
				URL:         &tt.fromURL,
				AuthType:    "none",
			}); err != nil {
				t.Fatalf("initial UpdateNotificationSettings() error = %v", err)
			}

			settings, err := svc.UpdateNotificationSettings(ctx, UpdateNotificationSettingsInput{
				ActorUserID: 2,
				Enabled:     true,
				ChannelType: tt.toChannel,
				URL:         &tt.toURL,
				AuthType:    "none",
			})
			if err != nil {
				t.Fatalf("UpdateNotificationSettings(channel switch) error = %v", err)
			}
			row := client.QuotaResetNotificationSetting.Query().OnlyX(ctx)
			if settings.ChannelType != tt.toChannel || row.ChannelType.String() != tt.toChannel || row.URL != tt.toURL {
				t.Fatalf("saved switched settings = %+v/%s/%q, want %s/%q", settings, row.ChannelType, row.URL, tt.toChannel, tt.toURL)
			}
		})
	}
}

func TestNotificationSettingsOmittedURLPreservesExistingSecret(t *testing.T) {
	tests := []struct {
		name    string
		channel string
		url     string
	}{
		{
			name:    "generic webhook",
			channel: "generic_webhook",
			url:     "https://hooks.example.com/quota-reset?token=synthetic-secret",
		},
		{
			name:    "Enterprise WeChat",
			channel: "wecom_group_robot",
			url:     "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=synthetic-secret",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			client := testdb.Open(t)
			svc := NewService(client, nil, nil, nil)
			if _, err := svc.UpdateNotificationSettings(ctx, UpdateNotificationSettingsInput{
				ActorUserID: 1,
				Enabled:     true,
				ChannelType: tt.channel,
				URL:         &tt.url,
				AuthType:    "none",
			}); err != nil {
				t.Fatalf("initial UpdateNotificationSettings() error = %v", err)
			}
			settings, err := svc.UpdateNotificationSettings(ctx, UpdateNotificationSettingsInput{
				ActorUserID: 2,
				Enabled:     true,
				ChannelType: tt.channel,
				AuthType:    "none",
			})
			if err != nil {
				t.Fatalf("UpdateNotificationSettings(omitted URL) error = %v", err)
			}
			if !settings.URLConfigured {
				t.Fatalf("settings = %+v, want configured URL", settings)
			}
			row := client.QuotaResetNotificationSetting.Query().OnlyX(ctx)
			if row.URL != tt.url {
				t.Fatalf("stored URL = %q, want preserved secret URL", row.URL)
			}
		})
	}
}

func TestNotificationSettingsOmittedURLPreservesExplicitGenericChannelOnWeComEndpoint(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	robotURL := "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=synthetic-existing-key"
	client.QuotaResetNotificationSetting.Create().
		SetEnabled(true).
		SetChannelType(quotaresetnotificationsetting.ChannelTypeGenericWebhook).
		SetChannelTypeConfigured(true).
		SetURL(robotURL).
		SetAuthType(quotaresetnotificationsetting.AuthTypeNone).
		SetCreatedByUserID(1).
		SetUpdatedByUserID(1).
		SaveX(ctx)

	settings, err := NewService(client, nil, nil, nil).UpdateNotificationSettings(ctx, UpdateNotificationSettingsInput{
		ActorUserID: 2,
		Enabled:     true,
		ChannelType: quotaresetnotificationsetting.ChannelTypeGenericWebhook.String(),
		AuthType:    quotaresetnotificationsetting.AuthTypeNone.String(),
	})
	if err != nil {
		t.Fatalf("UpdateNotificationSettings(omitted compatibility URL) error = %v", err)
	}
	row := client.QuotaResetNotificationSetting.Query().OnlyX(ctx)
	if settings.ChannelType != quotaresetnotificationsetting.ChannelTypeGenericWebhook.String() || row.URL != robotURL {
		t.Fatalf("preserved explicit generic settings = %+v/%q, want generic/%q", settings, row.URL, robotURL)
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
	endpoint := "https://hooks.example.com/quota-reset"

	updated, err := svc.UpdateNotificationSettings(ctx, UpdateNotificationSettingsInput{
		ActorUserID: 7,
		Enabled:     true,
		ChannelType: "generic_webhook",
		URL:         &endpoint,
		AuthType:    "none",
	})
	if err != nil {
		t.Fatalf("UpdateNotificationSettings() error = %v", err)
	}
	if updated.URLPreview != "https://hooks.example.com" || !updated.URLConfigured || !updated.Enabled {
		t.Fatalf("updated settings = %+v, want new enabled redacted URL", updated)
	}
	rows := client.QuotaResetNotificationSetting.Query().AllX(ctx)
	if len(rows) != 1 {
		t.Fatalf("notification setting row count = %d, want 1", len(rows))
	}
	if rows[0].URL != "https://hooks.example.com/quota-reset" || rows[0].UpdatedByUserID != 7 {
		t.Fatalf("remaining row = %+v, want updated canonical row", rows[0])
	}
}

func TestNotificationSettingsTestRequiresEnabledSavedWebhook(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	svc := NewService(client, nil, nil, NewWebhookNotifier(client, "", ""))

	_, err := svc.TestNotificationSettings(ctx, 7)
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
	_, err = svc.TestNotificationSettings(ctx, 7)
	if !errors.Is(err, ErrInvalidNotification) {
		t.Fatalf("TestNotificationSettings(disabled setting) error = %v, want ErrInvalidNotification", err)
	}
}

func TestListApproverCandidatesReturnsDirectoryMatchedUsers(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	source := createQuotaResetDirectorySource(t, ctx, client)
	alpha := createQuotaResetDepartment(t, ctx, client, source.ID, "department-alpha", "Department Alpha", nil)
	alphaLead := createQuotaResetUser(t, ctx, client, "lead-alpha", "lead-alpha@example.com", nil, "user")
	peer := createQuotaResetUser(t, ctx, client, "peer-alpha", "peer-alpha@example.com", nil, "user")
	client.DirectoryDepartment.UpdateOneID(alpha.ID).
		SetMetadata(map[string]any{"representative_external_ids": []any{"member-alpha-lead"}}).
		SaveX(ctx)
	leadMember := createQuotaResetMember(t, ctx, client, source.ID, "member-alpha-lead", alphaLead.Email, alpha.ExternalID, &alphaLead.ID)
	peerMember := createQuotaResetMember(t, ctx, client, source.ID, "member-alpha-peer", peer.Email, alpha.ExternalID, &peer.ID)
	createQuotaResetMemberDepartment(t, ctx, client, source.ID, leadMember, alpha.ExternalID)
	createQuotaResetMemberDepartment(t, ctx, client, source.ID, peerMember, alpha.ExternalID)
	svc := NewService(client, nil, nil, nil)

	resp, err := svc.ListApproverCandidates(ctx, ApproverCandidateParams{
		SourceID: source.ID,
		Query:    "peer-alpha",
		Page:     1,
		PageSize: 20,
	})
	if err != nil {
		t.Fatalf("ListApproverCandidates() error = %v", err)
	}
	if len(resp.Items) != 1 || resp.Items[0].UserID != peer.ID {
		t.Fatalf("candidates = %#v, want matched non-representative peer", resp.Items)
	}
	if resp.Page != 1 || resp.PageSize != 20 || resp.Total != 1 {
		t.Fatalf("pagination = page %d size %d total %d, want 1/20/1", resp.Page, resp.PageSize, resp.Total)
	}
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
	if resp.Items[0].DirectorySourceID != currentSource.ID || resp.Items[0].ApproverUserID != currentApprover.ID {
		t.Fatalf("configs = %#v, want current source config only", resp.Items)
	}
}

func TestSaveApproverConfigsAcceptsMatchedNonRepresentative(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	source := createQuotaResetDirectorySource(t, ctx, client)
	department := createQuotaResetDepartment(t, ctx, client, source.ID, "department-alpha", "Department Alpha", nil)
	lead := createQuotaResetUser(t, ctx, client, "lead-alpha", "lead-alpha@example.com", nil, "user")
	peer := createQuotaResetUser(t, ctx, client, "peer-alpha", "peer-alpha@example.com", nil, "user")
	client.DirectoryDepartment.UpdateOneID(department.ID).
		SetMetadata(map[string]any{"representative_external_ids": []any{"member-alpha-lead"}}).
		SaveX(ctx)
	leadMember := createQuotaResetMember(t, ctx, client, source.ID, "member-alpha-lead", lead.Email, department.ExternalID, &lead.ID)
	peerMember := createQuotaResetMember(t, ctx, client, source.ID, "member-alpha-peer", peer.Email, department.ExternalID, &peer.ID)
	createQuotaResetMemberDepartment(t, ctx, client, source.ID, leadMember, department.ExternalID)
	createQuotaResetMemberDepartment(t, ctx, client, source.ID, peerMember, department.ExternalID)
	svc := NewService(client, nil, nil, nil)

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
		t.Fatalf("SaveApproverConfigs(non representative) error = %v", err)
	}
	if len(resp.Items) != 1 || resp.Items[0].ApproverUserID != peer.ID {
		t.Fatalf("saved configs = %#v, want non-representative peer", resp.Items)
	}
}

type providerResolverFunc func(context.Context, int) (relay.Provider, error)

func (f providerResolverFunc) Resolve(ctx context.Context, providerID int) (relay.Provider, error) {
	return f(ctx, providerID)
}

type countingQuotaResetNotifier struct {
	calls int
}

func (n *countingQuotaResetNotifier) Notify(context.Context, NotificationContext) (*NotificationDeliveryResult, error) {
	n.calls++
	return &NotificationDeliveryResult{Delivered: true}, nil
}

type quotaResetTerminalFailureCase struct {
	name      string
	resetErr  error
	status    quotaresetrequest.Status
	eventType quotaresetrequestevent.EventType
}

func quotaResetTerminalFailureCases() []quotaResetTerminalFailureCase {
	return []quotaResetTerminalFailureCase{
		{
			name:      "success outcome",
			status:    quotaresetrequest.StatusApprovedResetSucceeded,
			eventType: quotaresetrequestevent.EventTypeResetSucceeded,
		},
		{
			name:      "provider failure outcome",
			resetErr:  errors.New("relay timeout"),
			status:    quotaresetrequest.StatusApprovedResetFailed,
			eventType: quotaresetrequestevent.EventTypeResetFailed,
		},
	}
}

type quotaResetTerminalFailureFixture struct {
	ctx      context.Context
	client   *ent.Client
	request  *ent.QuotaResetRequest
	actor    *ent.User
	provider *fakeQuotaResetProvider
	notifier *countingQuotaResetNotifier
	service  *Service
}

func newQuotaResetTerminalFailureFixture(t *testing.T, resetErr error) quotaResetTerminalFailureFixture {
	t.Helper()
	ctx := context.Background()
	client := testdb.Open(t)
	requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
	actor := createQuotaResetUser(t, ctx, client, "approver", "approver@example.com", nil, "user")
	providerRow := createQuotaResetRelayProvider(t, ctx, client)
	request := createPendingQuotaResetRequest(t, ctx, client, requester.ID, 1001, providerRow.ID, "42", []int{actor.ID})
	client.QuotaResetRequest.UpdateOneID(request.ID).
		SetStatus(quotaresetrequest.StatusApprovedResetting).
		SaveX(ctx)
	createEnabledQuotaResetNotificationSetting(t, ctx, client)
	provider := &fakeQuotaResetProvider{resetErr: resetErr}
	notifier := &countingQuotaResetNotifier{}
	return quotaResetTerminalFailureFixture{
		ctx:      ctx,
		client:   client,
		request:  request,
		actor:    actor,
		provider: provider,
		notifier: notifier,
		service:  NewService(client, fakeProviderResolver(providerRow.ID, provider), NewApproverResolver(client), notifier),
	}
}

func assertQuotaResetTerminalFailureRolledBack(t *testing.T, fixture quotaResetTerminalFailureFixture, eventType quotaresetrequestevent.EventType) {
	t.Helper()
	assertQuotaResetOutcomeRolledBack(t, fixture.ctx, fixture.client, fixture.request.ID, eventType)
	if fixture.provider.resetCalls != 1 {
		t.Fatalf("reset calls = %d, want 1", fixture.provider.resetCalls)
	}
	if fixture.notifier.calls != 0 {
		t.Fatalf("result notification calls = %d, want 0", fixture.notifier.calls)
	}
}

func injectQuotaResetEventFailure(client *ent.Client, eventType quotaresetrequestevent.EventType, injectedErr error) {
	client.QuotaResetRequestEvent.Use(func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, mutation ent.Mutation) (ent.Value, error) {
			eventMutation, ok := mutation.(*ent.QuotaResetRequestEventMutation)
			if ok && mutation.Op().Is(ent.OpCreate) {
				if got, exists := eventMutation.EventType(); exists && got == eventType {
					return nil, injectedErr
				}
			}
			return next.Mutate(ctx, mutation)
		})
	})
}

func assertQuotaResetOutcomeRolledBack(t *testing.T, ctx context.Context, client *ent.Client, requestID int, eventType quotaresetrequestevent.EventType) {
	t.Helper()
	request := client.QuotaResetRequest.GetX(ctx, requestID)
	if request.Status != quotaresetrequest.StatusApprovedResetting || request.ResetCompletedAt != nil || request.ResetError != "" {
		t.Fatalf("request after failed terminal persistence = %s/%v/%q, want approved_resetting/nil/empty", request.Status, request.ResetCompletedAt, request.ResetError)
	}
	if count := client.QuotaResetRequestEvent.Query().
		Where(
			quotaresetrequestevent.RequestIDEQ(requestID),
			quotaresetrequestevent.EventTypeEQ(eventType),
		).
		CountX(ctx); count != 0 {
		t.Fatalf("%s event count after rollback = %d, want 0", eventType, count)
	}
}

func createEnabledQuotaResetNotificationSetting(t *testing.T, ctx context.Context, client *ent.Client) {
	t.Helper()
	client.QuotaResetNotificationSetting.Create().
		SetEnabled(true).
		SetChannelType("generic_webhook").
		SetChannelTypeConfigured(true).
		SetURL("https://hooks.example.com/quota-reset").
		SetAuthType("none").
		SetCreatedByUserID(1).
		SetUpdatedByUserID(1).
		SaveX(ctx)
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
	subscriptions           []relay.UserSubscription
	listUserSubscriptionsFn func(context.Context, int64) ([]relay.UserSubscription, error)
	listedRelayUserIDs      []int64
	groups                  []relay.Group
	listPlatformGroupsFn    func(context.Context) ([]relay.Group, error)
	resetErr                error
	resetFn                 func(context.Context, int64, int64) error
	resetUserID             int64
	resetGroupID            int64
	resetCalls              int
}

func (f *fakeQuotaResetProvider) ListPlatformGroups(ctx context.Context) ([]relay.Group, error) {
	if f.listPlatformGroupsFn != nil {
		return f.listPlatformGroupsFn(ctx)
	}
	return append([]relay.Group(nil), f.groups...), nil
}

func (f *fakeQuotaResetProvider) ListUserSubscriptions(ctx context.Context, relayUserID int64) ([]relay.UserSubscription, error) {
	f.listedRelayUserIDs = append(f.listedRelayUserIDs, relayUserID)
	if f.listUserSubscriptionsFn != nil {
		return f.listUserSubscriptionsFn(ctx, relayUserID)
	}
	return append([]relay.UserSubscription(nil), f.subscriptions...), nil
}

func (f *fakeQuotaResetProvider) ResetSubscriptionQuotaForUser(ctx context.Context, relayUserID, groupID int64) error {
	f.resetCalls++
	f.resetUserID = relayUserID
	f.resetGroupID = groupID
	if f.resetFn != nil {
		return f.resetFn(ctx, relayUserID, groupID)
	}
	return f.resetErr
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

func createPendingWorkflowDirectoryFixture(t *testing.T, ctx context.Context, client *ent.Client, requester *ent.User) {
	t.Helper()
	source := createQuotaResetDirectorySource(t, ctx, client)
	department := createQuotaResetDepartment(t, ctx, client, source.ID, "department-alpha", "Department Alpha", nil)
	approver := createQuotaResetUser(t, ctx, client, "approver-alpha", "approver-alpha@example.com", nil, "user")
	requesterMember := createQuotaResetMember(t, ctx, client, source.ID, "member-alice", requester.Email, department.ExternalID, &requester.ID)
	approverMember := createQuotaResetMember(t, ctx, client, source.ID, "member-approver-alpha", approver.Email, department.ExternalID, &approver.ID)
	createQuotaResetMemberDepartment(t, ctx, client, source.ID, requesterMember, department.ExternalID)
	createQuotaResetMemberDepartment(t, ctx, client, source.ID, approverMember, department.ExternalID)
	createQuotaResetApproverConfig(t, ctx, client, source.ID, department.ExternalID, department.Name, approver.ID)
}

func createPendingQuotaResetRequest(t *testing.T, ctx context.Context, client *ent.Client, requesterUserID int, requesterRelayUserID int64, providerID int, groupID string, approverIDs []int) *ent.QuotaResetRequest {
	t.Helper()
	create := client.QuotaResetRequest.Create().
		SetRequesterUserID(requesterUserID).
		SetRequesterRelayUserID(requesterRelayUserID).
		SetProviderID(providerID).
		SetGroupID(groupID).
		SetGroupName("Group Alpha").
		SetGroupPlatform("openai").
		SetReason("Need reset for a build investigation").
		SetMatchedDepartmentPaths([]map[string]any{})
	if approverIDs != nil {
		create.SetResolvedApproverUserIds(approverIDs)
	}
	request, err := create.Save(ctx)
	if err != nil {
		t.Fatalf("create pending request: %v", err)
	}
	return request
}
