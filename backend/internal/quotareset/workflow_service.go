package quotareset

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/quotaresetrequest"
	"github.com/ai-efficiency/backend/ent/quotaresetrequestevent"
	"github.com/ai-efficiency/backend/internal/relay"
)

func (s *Service) createWorkflowRequest(
	ctx context.Context,
	requester *ent.User,
	providerRow *ent.RelayProvider,
	subscription relay.UserSubscription,
	input CreateRequestInput,
	workflow *Workflow,
	paths []DepartmentPathEvidence,
) (*ent.QuotaResetRequest, error) {
	rawWorkflow, err := EncodeWorkflow(workflow)
	if err != nil {
		return nil, err
	}
	pathMaps, err := departmentPathEvidenceToMaps(paths)
	if err != nil {
		return nil, err
	}
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return nil, fmt.Errorf("start quota reset request transaction: %w", err)
	}
	request, err := tx.QuotaResetRequest.Create().
		SetRequesterUserID(requester.ID).
		SetRequesterRelayUserID(int64(*requester.RelayUserID)).
		SetProviderID(providerRow.ID).
		SetGroupID(input.GroupID).
		SetGroupName(subscriptionGroupName(subscription)).
		SetGroupPlatform(subscriptionGroupPlatform(subscription)).
		SetReason(input.Reason).
		SetStatus(quotaresetrequest.StatusWorkflowPending).
		SetWorkflowVersion(workflowVersionV2).
		SetWorkflow(rawWorkflow).
		SetWorkflowRevision(0).
		SetResolvedApproverUserIds(workflow.ActiveApproverUserIDs()).
		SetMatchedDepartmentPaths(pathMaps).
		Save(ctx)
	if err != nil {
		_ = tx.Rollback()
		if activeRequestCreateWasDuplicate(ctx, s.client, err, requester.ID, providerRow.ID, input.GroupID) {
			return nil, ErrActiveRequestExists
		}
		return nil, fmt.Errorf("create quota reset request: %w", err)
	}
	events := []struct {
		typeName quotaresetrequestevent.EventType
		actorID  *int
		metadata map[string]any
	}{
		{quotaresetrequestevent.EventTypeCreated, &requester.ID, map[string]any{"group_id": input.GroupID}},
		{quotaresetrequestevent.EventTypeApproverResolved, nil, map[string]any{
			"approver_user_ids": workflow.ActiveApproverUserIDs(),
			"path_count":        len(paths),
		}},
		{quotaresetrequestevent.EventTypeWorkflowCreated, nil, map[string]any{
			"workflow_version": workflowVersionV2,
			"step_count":       len(workflow.Steps),
		}},
	}
	for _, event := range events {
		if err := writeWorkflowEventTx(ctx, tx, request.ID, event.actorID, event.typeName, event.metadata, ""); err != nil {
			_ = tx.Rollback()
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit quota reset request: %w", err)
	}
	_ = s.notify(ctx, "quota_reset_request_created", request)
	return request, nil
}

func (s *Service) decideWorkflowRequest(ctx context.Context, request *ent.QuotaResetRequest, input DecisionInput, approve bool) (*ent.QuotaResetRequest, error) {
	comment := strings.TrimSpace(input.DecisionReason)
	if comment == "" {
		return nil, ErrDecisionRequired
	}
	if request.Status != quotaresetrequest.StatusWorkflowPending {
		return nil, ErrInvalidStatus
	}
	workflow, err := DecodeWorkflow(request.Workflow)
	if err != nil {
		return nil, err
	}
	if workflow.CurrentStep < 0 || workflow.CurrentStep >= len(workflow.Steps) {
		return nil, ErrInvalidStatus
	}
	actor, err := s.client.User.Get(ctx, input.ActorUserID)
	if err != nil {
		return nil, fmt.Errorf("load quota reset decision actor: %w", err)
	}
	stepIndex := workflow.CurrentStep
	stepLabel := workflow.Steps[stepIndex].Label
	actorDisplayName := strings.TrimSpace(actor.Username)
	for _, approver := range workflow.Steps[stepIndex].Approvers {
		if approver.UserID == actor.ID {
			actorDisplayName = firstWorkflowValue(approver.DisplayName, actorDisplayName)
			break
		}
	}
	now := time.Now().UTC()
	transition, err := workflow.Decide(WorkflowDecisionInput{
		RequesterUserID:  request.RequesterUserID,
		ActorUserID:      input.ActorUserID,
		ActorDisplayName: actorDisplayName,
		Comment:          comment,
		Approve:          approve,
		Admin:            input.Admin,
		DecidedAt:        now,
	})
	if err != nil {
		return nil, err
	}
	rawWorkflow, err := EncodeWorkflow(workflow)
	if err != nil {
		return nil, err
	}
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return nil, fmt.Errorf("start quota reset decision transaction: %w", err)
	}
	update := tx.QuotaResetRequest.UpdateOneID(request.ID).
		Where(
			quotaresetrequest.StatusEQ(quotaresetrequest.StatusWorkflowPending),
			quotaresetrequest.WorkflowRevisionEQ(request.WorkflowRevision),
		).
		SetWorkflow(rawWorkflow).
		SetWorkflowRevision(request.WorkflowRevision + 1).
		SetResolvedApproverUserIds(workflow.ActiveApproverUserIDs())
	if transition.TerminalApproved {
		update.
			SetStatus(quotaresetrequest.StatusApprovedResetting).
			SetApprovedByUserID(input.ActorUserID).
			SetDecisionReason(comment).
			SetDecidedAt(now)
	}
	if transition.TerminalRejected {
		update.
			SetStatus(quotaresetrequest.StatusRejected).
			SetRejectedByUserID(input.ActorUserID).
			SetDecisionReason(comment).
			SetDecidedAt(now)
	}
	updated, err := update.Save(ctx)
	if ent.IsNotFound(err) {
		_ = tx.Rollback()
		return nil, ErrInvalidStatus
	}
	if err != nil {
		_ = tx.Rollback()
		return nil, fmt.Errorf("store quota reset workflow decision: %w", err)
	}
	decisionMetadata := map[string]any{
		"step_index":         stepIndex,
		"step_label":         stepLabel,
		"actor_display_name": actorDisplayName,
		"comment":            comment,
		"admin":              input.Admin,
		"workflow_revision":  request.WorkflowRevision + 1,
	}
	if approve {
		if err := writeWorkflowEventTx(ctx, tx, request.ID, &input.ActorUserID, quotaresetrequestevent.EventTypeStepApproved, decisionMetadata, ""); err != nil {
			_ = tx.Rollback()
			return nil, err
		}
	} else {
		if err := writeWorkflowEventTx(ctx, tx, request.ID, &input.ActorUserID, quotaresetrequestevent.EventTypeRejected, decisionMetadata, ""); err != nil {
			_ = tx.Rollback()
			return nil, err
		}
	}
	for _, satisfiedStep := range transition.SatisfiedSteps {
		sourceStep := workflow.Steps[satisfiedStep].SatisfiedByStep
		if sourceStep == nil || *sourceStep < 0 || *sourceStep >= satisfiedStep {
			_ = tx.Rollback()
			return nil, fmt.Errorf("%w: satisfied step %d has no source", ErrInvalidWorkflow, satisfiedStep)
		}
		sourceDecision := workflow.Steps[*sourceStep].Decision
		if sourceDecision == nil || !sourceDecision.Approve {
			_ = tx.Rollback()
			return nil, fmt.Errorf("%w: satisfied step %d source has no approval", ErrInvalidWorkflow, satisfiedStep)
		}
		metadata := map[string]any{
			"step_index":        satisfiedStep,
			"satisfied_by_step": *sourceStep,
			"actor_user_id":     sourceDecision.ActorUserID,
			"workflow_revision": request.WorkflowRevision + 1,
		}
		if err := writeWorkflowEventTx(ctx, tx, request.ID, &sourceDecision.ActorUserID, quotaresetrequestevent.EventTypeStepSatisfied, metadata, ""); err != nil {
			_ = tx.Rollback()
			return nil, err
		}
	}
	if transition.ActivatedStep != nil {
		active := workflow.Steps[*transition.ActivatedStep]
		metadata := map[string]any{
			"step_index":        *transition.ActivatedStep,
			"step_label":        active.Label,
			"approver_user_ids": workflow.ActiveApproverUserIDs(),
			"workflow_revision": request.WorkflowRevision + 1,
		}
		if err := writeWorkflowEventTx(ctx, tx, request.ID, nil, quotaresetrequestevent.EventTypeStepActivated, metadata, ""); err != nil {
			_ = tx.Rollback()
			return nil, err
		}
		if active.AdminFallback {
			if err := writeWorkflowEventTx(ctx, tx, request.ID, nil, quotaresetrequestevent.EventTypeAdminFallbackActivated, metadata, ""); err != nil {
				_ = tx.Rollback()
				return nil, err
			}
		}
	}
	if transition.TerminalApproved {
		if err := writeWorkflowEventTx(ctx, tx, request.ID, &input.ActorUserID, quotaresetrequestevent.EventTypeApproved, decisionMetadata, ""); err != nil {
			_ = tx.Rollback()
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit quota reset workflow decision: %w", err)
	}
	if transition.TerminalApproved {
		return s.executeReset(ctx, updated.ID, input.ActorUserID, false, input.Admin)
	}
	if transition.TerminalRejected {
		_ = s.notify(ctx, "quota_reset_request_rejected", updated)
		return updated, nil
	}
	_ = s.notify(ctx, "quota_reset_step_activated", updated)
	return updated, nil
}

func writeWorkflowEventTx(ctx context.Context, tx *ent.Tx, requestID int, actorUserID *int, eventType quotaresetrequestevent.EventType, metadata map[string]any, errorMessage string) error {
	if metadata == nil {
		metadata = map[string]any{}
	}
	create := tx.QuotaResetRequestEvent.Create().
		SetRequestID(requestID).
		SetEventType(eventType).
		SetMetadata(metadata).
		SetErrorMessage(strings.TrimSpace(errorMessage))
	if actorUserID != nil {
		create.SetActorUserID(*actorUserID)
	}
	if _, err := create.Save(ctx); err != nil {
		return fmt.Errorf("write quota reset workflow event: %w", err)
	}
	return nil
}
