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

type workflowEvent struct {
	typeName quotaresetrequestevent.EventType
	actorID  *int
	metadata map[string]any
}

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
	defer tx.Rollback()
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
		if activeRequestCreateWasDuplicate(ctx, s.client, err, requester.ID, providerRow.ID, input.GroupID) {
			return nil, ErrActiveRequestExists
		}
		return nil, fmt.Errorf("create quota reset request: %w", err)
	}
	events := []workflowEvent{
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
	if err := writeWorkflowEventsTx(ctx, tx, request.ID, events); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit quota reset request: %w", err)
	}
	_ = s.notify(ctx, "quota_reset_request_created", request)
	return request, nil
}

func (s *Service) decideWorkflowRequest(ctx context.Context, request *ent.QuotaResetRequest, input DecisionInput, approve bool) (*ent.QuotaResetRequest, error) {
	comment := strings.TrimSpace(input.DecisionReason)
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
	transition, err := workflow.Decide(request.RequesterUserID, input, actorDisplayName, approve, now)
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
	defer tx.Rollback()
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
		return nil, ErrInvalidStatus
	}
	if err != nil {
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
	decisionEvent := quotaresetrequestevent.EventTypeRejected
	if approve {
		decisionEvent = quotaresetrequestevent.EventTypeStepApproved
	}
	events := []workflowEvent{{decisionEvent, &input.ActorUserID, decisionMetadata}}
	for _, satisfiedStep := range transition.SatisfiedSteps {
		sourceStep := workflow.Steps[satisfiedStep].SatisfiedByStep
		sourceDecision := workflow.Steps[*sourceStep].Decision
		metadata := map[string]any{
			"step_index":        satisfiedStep,
			"satisfied_by_step": *sourceStep,
			"actor_user_id":     sourceDecision.ActorUserID,
			"workflow_revision": request.WorkflowRevision + 1,
		}
		events = append(events, workflowEvent{quotaresetrequestevent.EventTypeStepSatisfied, &sourceDecision.ActorUserID, metadata})
	}
	if transition.ActivatedStep != nil {
		active := workflow.Steps[*transition.ActivatedStep]
		metadata := map[string]any{
			"step_index":        *transition.ActivatedStep,
			"step_label":        active.Label,
			"approver_user_ids": workflow.ActiveApproverUserIDs(),
			"workflow_revision": request.WorkflowRevision + 1,
		}
		events = append(events, workflowEvent{quotaresetrequestevent.EventTypeStepActivated, nil, metadata})
		if active.AdminFallback {
			events = append(events, workflowEvent{quotaresetrequestevent.EventTypeAdminFallbackActivated, nil, metadata})
		}
	}
	if transition.TerminalApproved {
		events = append(events, workflowEvent{quotaresetrequestevent.EventTypeApproved, &input.ActorUserID, decisionMetadata})
	}
	if err := writeWorkflowEventsTx(ctx, tx, request.ID, events); err != nil {
		return nil, err
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

func writeWorkflowEventsTx(ctx context.Context, tx *ent.Tx, requestID int, events []workflowEvent) error {
	for _, event := range events {
		if err := writeWorkflowEventTx(ctx, tx, requestID, event.actorID, event.typeName, event.metadata); err != nil {
			return err
		}
	}
	return nil
}

func writeWorkflowEventTx(ctx context.Context, tx *ent.Tx, requestID int, actorUserID *int, eventType quotaresetrequestevent.EventType, metadata map[string]any) error {
	if metadata == nil {
		metadata = map[string]any{}
	}
	create := tx.QuotaResetRequestEvent.Create().
		SetRequestID(requestID).
		SetEventType(eventType).
		SetMetadata(metadata)
	if actorUserID != nil {
		create.SetActorUserID(*actorUserID)
	}
	if _, err := create.Save(ctx); err != nil {
		return fmt.Errorf("write quota reset workflow event: %w", err)
	}
	return nil
}
