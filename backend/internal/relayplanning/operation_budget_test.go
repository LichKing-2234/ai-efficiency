package relayplanning

import (
	"context"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/ent/relationshipoperation"
	"github.com/ai-efficiency/backend/ent/relationshipoperationattempt"
	"github.com/ai-efficiency/backend/internal/testdb"
)

type operationContextKey string

func TestOperationContextOutlivesCallerDeadlineAndCancellation(t *testing.T) {
	parent, cancelParent := context.WithCancel(context.WithValue(context.Background(), operationContextKey("request_id"), "request-1"))
	cancelParent()

	ctx, cancel := OperationContext(parent)
	defer cancel()

	if err := ctx.Err(); err != nil {
		t.Fatalf("OperationContext error = %v, want in-flight context despite caller cancellation", err)
	}
	if got := ctx.Value(operationContextKey("request_id")); got != "request-1" {
		t.Fatalf("OperationContext request value = %v, want preserved request scope", got)
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("OperationContext has no deadline, want the operation execution budget")
	}
	remaining := time.Until(deadline)
	if remaining <= 0 || remaining > OperationExecutionBudget {
		t.Fatalf("OperationContext remaining budget = %s, want within (0, %s]", remaining, OperationExecutionBudget)
	}
}

func TestSettleAbandonedOperationsInterruptsStaleOnly(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	providerRow := client.RelayProvider.Create().
		SetName("relay-operation-budget-test").
		SetDisplayName("Relay Operation Budget Test").
		SetBaseURL("https://relay.example.com").
		SetAdminAPIKey("test-admin-key").
		SaveX(ctx)

	createOperation := func(key string, lifecycle relationshipoperation.Lifecycle, startedAt time.Time) (int, int) {
		operation := client.RelationshipOperation.Create().
			SetOperationKey(key).
			SetProviderID(providerRow.ID).
			SetPlatform("openai").
			SetLifecycle(lifecycle).
			SetBaselineSnapshot(map[string]any{}).
			SetTargetSnapshot(map[string]any{}).
			SetBaselineFingerprint("baseline").
			SetTargetFingerprint("target").
			SetSupportedDirections([]string{"resume"}).
			SetInitiatedByUserID(1).
			SaveX(ctx)
		attempt := client.RelationshipOperationAttempt.Create().
			SetOperationID(operation.ID).
			SetAttemptNumber(1).
			SetDirection(relationshipoperationattempt.DirectionResume).
			SetInitiatedByUserID(1).
			SetStatus(relationshipoperationattempt.StatusRunning).
			SetStartedAt(startedAt).
			SaveX(ctx)
		return operation.ID, attempt.ID
	}

	staleOperationID, staleAttemptID := createOperation("stale-operation", relationshipoperation.LifecycleResuming, time.Now().UTC().Add(-2*time.Hour))
	freshOperationID, freshAttemptID := createOperation("fresh-operation", relationshipoperation.LifecycleApplying, time.Now().UTC())

	settled, err := SettleAbandonedOperations(ctx, client, time.Minute)
	if err != nil {
		t.Fatalf("settle abandoned operations: %v", err)
	}
	if settled != 1 {
		t.Fatalf("settled operations = %d, want exactly the abandoned operation", settled)
	}
	if got := client.RelationshipOperation.GetX(ctx, staleOperationID).Lifecycle; got != relationshipoperation.LifecycleInterrupted {
		t.Fatalf("abandoned operation lifecycle = %q, want interrupted", got)
	}
	staleAttempt := client.RelationshipOperationAttempt.GetX(ctx, staleAttemptID)
	if staleAttempt.Status != relationshipoperationattempt.StatusInterrupted || staleAttempt.CompletedAt == nil {
		t.Fatalf("abandoned attempt = %q completed=%v, want interrupted with completion time", staleAttempt.Status, staleAttempt.CompletedAt)
	}
	if got := client.RelationshipOperation.GetX(ctx, freshOperationID).Lifecycle; got != relationshipoperation.LifecycleApplying {
		t.Fatalf("in-flight operation lifecycle = %q, want untouched applying", got)
	}
	if got := client.RelationshipOperationAttempt.GetX(ctx, freshAttemptID).Status; got != relationshipoperationattempt.StatusRunning {
		t.Fatalf("in-flight attempt status = %q, want untouched running", got)
	}
}

func TestConfirmRecoveryIgnoresExpiredRequestDeadline(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	service, _, _, operation := createRecoveryFixture(t, ctx, client, false)

	preview, err := service.PreviewRecovery(ctx, operation.ID, RecoveryResume)
	if err != nil {
		t.Fatalf("preview recovery: %v", err)
	}
	expired, cancel := context.WithDeadline(ctx, time.Now().Add(-time.Second))
	defer cancel()
	if expired.Err() == nil {
		t.Fatal("request context is not expired")
	}

	result, err := service.ConfirmRecovery(expired, RecoveryConfirmRequest{
		OperationID:                     operation.ID,
		Direction:                       RecoveryResume,
		ExpectedBaselineRevisions:       preview.BaselineRevisions,
		ExpectedRelationshipFingerprint: preview.RelationshipFingerprint,
		InitiatedByUserID:               1,
	})
	if err != nil {
		t.Fatalf("ConfirmRecovery with an expired request context: %v", err)
	}
	if result == nil || result.Lifecycle != string(relationshipoperation.LifecycleApplied) {
		t.Fatalf("recovery result = %+v, want applied lifecycle", result)
	}
	if got := client.RelationshipOperation.GetX(ctx, operation.ID).Lifecycle; got != relationshipoperation.LifecycleApplied {
		t.Fatalf("operation lifecycle = %q, want applied despite the expired request context", got)
	}
}
