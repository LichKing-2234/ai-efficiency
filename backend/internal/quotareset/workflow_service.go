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
		if rollbackErr := tx.Rollback(); rollbackErr != nil {
			return nil, fmt.Errorf("rollback failed quota reset request after create error %v: %w", err, rollbackErr)
		}
		if activeRequestCreateWasDuplicate(ctx, s.client, err, requester.ID, providerRow.ID, input.GroupID) {
			return nil, ErrActiveRequestExists
		}
		return nil, fmt.Errorf("create quota reset request: %w", err)
	}
	events := []*ent.QuotaResetRequestEventCreate{
		newWorkflowEvent(tx, request.ID, &requester.ID, quotaresetrequestevent.EventTypeCreated, map[string]any{"group_id": input.GroupID}),
		newWorkflowEvent(tx, request.ID, nil, quotaresetrequestevent.EventTypeApproverResolved, map[string]any{
			"approver_user_ids": workflow.ActiveApproverUserIDs(),
			"path_count":        len(paths),
		}),
		newWorkflowEvent(tx, request.ID, nil, quotaresetrequestevent.EventTypeWorkflowCreated, map[string]any{
			"workflow_version": workflowVersionV2,
			"step_count":       len(workflow.Steps),
		}),
	}
	if _, err := tx.QuotaResetRequestEvent.CreateBulk(events...).Save(ctx); err != nil {
		return nil, fmt.Errorf("write quota reset workflow events: %w", err)
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
	satisfiedSteps, err := workflow.Decide(WorkflowDecisionInput{
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
	terminal := workflow.CurrentStep == len(workflow.Steps)
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
	if terminal {
		update.SetDecisionReason(comment).SetDecidedAt(now)
		if approve {
			update.SetStatus(quotaresetrequest.StatusApprovedResetting).SetApprovedByUserID(input.ActorUserID)
		} else {
			update.SetStatus(quotaresetrequest.StatusRejected).SetRejectedByUserID(input.ActorUserID)
		}
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
	events := []*ent.QuotaResetRequestEventCreate{
		newWorkflowEvent(tx, request.ID, &input.ActorUserID, decisionEvent, decisionMetadata),
	}
	for _, satisfiedStep := range satisfiedSteps {
		sourceStep := workflow.Steps[satisfiedStep].SatisfiedByStep
		sourceDecision := workflow.Steps[*sourceStep].Decision
		metadata := map[string]any{
			"step_index":        satisfiedStep,
			"satisfied_by_step": *sourceStep,
			"actor_user_id":     sourceDecision.ActorUserID,
			"workflow_revision": request.WorkflowRevision + 1,
		}
		events = append(events, newWorkflowEvent(tx, request.ID, &sourceDecision.ActorUserID, quotaresetrequestevent.EventTypeStepSatisfied, metadata))
	}
	if !terminal {
		active := workflow.Steps[workflow.CurrentStep]
		metadata := map[string]any{
			"step_index":        workflow.CurrentStep,
			"step_label":        active.Label,
			"approver_user_ids": workflow.ActiveApproverUserIDs(),
			"workflow_revision": request.WorkflowRevision + 1,
		}
		events = append(events, newWorkflowEvent(tx, request.ID, nil, quotaresetrequestevent.EventTypeStepActivated, metadata))
		if active.AdminFallback {
			events = append(events, newWorkflowEvent(tx, request.ID, nil, quotaresetrequestevent.EventTypeAdminFallbackActivated, metadata))
		}
	}
	if terminal && approve {
		events = append(events, newWorkflowEvent(tx, request.ID, &input.ActorUserID, quotaresetrequestevent.EventTypeApproved, decisionMetadata))
	}
	if _, err := tx.QuotaResetRequestEvent.CreateBulk(events...).Save(ctx); err != nil {
		return nil, fmt.Errorf("write quota reset workflow events: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit quota reset workflow decision: %w", err)
	}
	if terminal && approve {
		return s.executeReset(ctx, updated.ID, input.ActorUserID, false, input.Admin)
	}
	if terminal {
		_ = s.notify(ctx, "quota_reset_request_rejected", updated)
		return updated, nil
	}
	_ = s.notify(ctx, "quota_reset_step_activated", updated)
	return updated, nil
}

func newWorkflowEvent(tx *ent.Tx, requestID int, actorUserID *int, eventType quotaresetrequestevent.EventType, metadata map[string]any) *ent.QuotaResetRequestEventCreate {
	return tx.QuotaResetRequestEvent.Create().
		SetRequestID(requestID).
		SetNillableActorUserID(actorUserID).
		SetEventType(eventType).
		SetMetadata(metadata)
}
