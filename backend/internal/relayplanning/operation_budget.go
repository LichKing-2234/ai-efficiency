package relayplanning

import (
	"context"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/relationshipoperation"
	"github.com/ai-efficiency/backend/ent/relationshipoperationattempt"
)

// OperationExecutionBudget bounds one durable Relay relationship mutation:
// initial apply, Replan apply, Mapping Renewal, and Recovery.
//
// Durable Relationship Operation work must not inherit the inbound HTTP
// request deadline. A browser or reverse-proxy timeout that fires mid-flight
// would otherwise cancel external Relay writes after their local step was
// already marked dispatched, and a twenty-plus step Replan legitimately needs
// more wall clock than one request context is allowed to hold open. The
// operation itself stays bounded, durable, and resumable: when this budget is
// exhausted the operation settles as interrupted and an explicit Recovery
// resumes it from verified readback instead of repeating external writes.
const OperationExecutionBudget = 5 * time.Minute

// OperationSettlementMargin is added to OperationExecutionBudget before an
// unfinished operation is considered abandoned rather than in flight.
const OperationSettlementMargin = 2 * time.Minute

// OperationContext derives the context durable Relay mutation work runs on. It
// keeps request-scoped values (telemetry correlation, actor) while replacing the
// caller's cancellation and deadline with OperationExecutionBudget.
func OperationContext(parent context.Context) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	return context.WithTimeout(context.WithoutCancel(parent), OperationExecutionBudget)
}

// SettleAbandonedOperations marks Relationship Operations that never settled and
// outlived OperationExecutionBudget as interrupted, together with their running
// attempts. Without it a process restart during a detached execution would leave
// the Mapping permanently `operating`, because Recovery only accepts an
// interrupted operation. Operations younger than staleAfter are left untouched so
// a rolling deploy never settles work another instance is still executing.
func SettleAbandonedOperations(ctx context.Context, client *ent.Client, staleAfter time.Duration) (int, error) {
	if client == nil {
		return 0, nil
	}
	if staleAfter <= 0 {
		staleAfter = OperationExecutionBudget + OperationSettlementMargin
	}
	cutoff := time.Now().UTC().Add(-staleAfter)
	operations, err := client.RelationshipOperation.Query().Where(
		relationshipoperation.LifecycleIn(
			relationshipoperation.LifecycleApplying,
			relationshipoperation.LifecycleResuming,
			relationshipoperation.LifecycleRestoring,
		),
	).All(ctx)
	if err != nil {
		return 0, err
	}
	settled := 0
	for _, operation := range operations {
		attempts, err := client.RelationshipOperationAttempt.Query().
			Where(relationshipoperationattempt.OperationIDEQ(operation.ID)).
			Order(ent.Desc(relationshipoperationattempt.FieldID)).
			All(ctx)
		if err != nil {
			return settled, err
		}
		startedAt := operation.UpdatedAt
		if len(attempts) > 0 && attempts[0].StartedAt != nil {
			startedAt = *attempts[0].StartedAt
		}
		if startedAt.After(cutoff) {
			continue
		}
		if err := settleAbandonedOperation(ctx, client, operation.ID, attempts); err != nil {
			return settled, err
		}
		settled++
	}
	return settled, nil
}

func settleAbandonedOperation(ctx context.Context, client *ent.Client, operationID int, attempts []*ent.RelationshipOperationAttempt) error {
	dbCtx := context.WithoutCancel(ctx)
	now := time.Now().UTC()
	tx, err := client.Tx(dbCtx)
	if err != nil {
		return err
	}
	updated, err := tx.RelationshipOperation.Update().Where(
		relationshipoperation.IDEQ(operationID),
		relationshipoperation.LifecycleIn(
			relationshipoperation.LifecycleApplying,
			relationshipoperation.LifecycleResuming,
			relationshipoperation.LifecycleRestoring,
		),
	).SetLifecycle(relationshipoperation.LifecycleInterrupted).Save(dbCtx)
	if err == nil && updated == 0 {
		_ = tx.Rollback()
		return nil
	}
	for _, attempt := range attempts {
		if attempt.Status != relationshipoperationattempt.StatusRunning {
			continue
		}
		if _, err = tx.RelationshipOperationAttempt.UpdateOneID(attempt.ID).
			SetStatus(relationshipoperationattempt.StatusInterrupted).
			SetCompletedAt(now).
			Save(dbCtx); err != nil {
			break
		}
	}
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}
