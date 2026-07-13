package quotareset

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"entgo.io/ent/dialect/sql"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/quotaresetrequest"
	"github.com/ai-efficiency/backend/ent/quotaresetrequestdecision"
	"github.com/ai-efficiency/backend/ent/quotaresetrequestevent"
	"github.com/ai-efficiency/backend/ent/quotaresetrequestnode"
	"github.com/ai-efficiency/backend/ent/quotaresetrequestnodeapprover"
	entuser "github.com/ai-efficiency/backend/ent/user"
	"github.com/ai-efficiency/backend/internal/relay"
)

func (s *Service) createWorkflowRequest(
	ctx context.Context,
	requester *ent.User,
	providerRow *ent.RelayProvider,
	subscription relay.UserSubscription,
	input CreateRequestInput,
) (*ent.QuotaResetRequest, error) {
	snapshot, err := s.workflowResolver.Resolve(ctx, requester.ID, providerRow.ID, input.GroupID)
	if err != nil {
		return nil, err
	}
	departmentPaths := cloneStringSlice(snapshot.Requester.DepartmentPaths)
	notificationIDs := cloneStringMap(snapshot.Requester.NotificationIDs)
	now := time.Now()
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin workflow snapshot transaction: %w", err)
	}
	txClosed := false
	defer func() {
		if !txClosed {
			_ = tx.Rollback()
		}
	}()

	request, err := tx.QuotaResetRequest.Create().
		SetRequesterUserID(requester.ID).
		SetRequesterRelayUserID(int64(*requester.RelayUserID)).
		SetProviderID(providerRow.ID).
		SetGroupID(input.GroupID).
		SetGroupName(subscriptionGroupName(subscription)).
		SetGroupPlatform(subscriptionGroupPlatform(subscription)).
		SetReason(strings.TrimSpace(input.Reason)).
		SetWorkflowVersion(WorkflowVersionV2).
		SetRequesterDisplayNameSnapshot(snapshot.Requester.DisplayName).
		SetRequesterEmailSnapshot(snapshot.Requester.Email).
		SetRequesterDepartmentPaths(departmentPaths).
		SetRequesterNotificationIds(notificationIDs).
		SetResolvedApproverUserIds([]int{}).
		SetMatchedDepartmentPaths([]map[string]any{}).
		Save(ctx)
	if err != nil {
		createErr := err
		rollbackErr := tx.Rollback()
		txClosed = true
		if rollbackErr != nil {
			return nil, fmt.Errorf("rollback workflow snapshot after request create failed: %v (create error: %w)", rollbackErr, createErr)
		}
		if activeRequestCreateWasDuplicate(ctx, s.client, createErr, requester.ID, providerRow.ID, input.GroupID) {
			return nil, ErrActiveRequestExists
		}
		return nil, fmt.Errorf("create v2 quota reset request: %w", createErr)
	}

	var activeNodeID *int
	storedNodes := make([]*ent.QuotaResetRequestNode, 0, len(snapshot.Nodes))
	for _, resolved := range snapshot.Nodes {
		departmentSnapshots, err := departmentSnapshotsToMaps(resolved.Departments)
		if err != nil {
			return nil, err
		}
		status := quotaresetrequestnode.Status(resolved.InitialStatus)
		if status == quotaresetrequestnode.StatusQueued && activeNodeID == nil {
			status = quotaresetrequestnode.StatusActive
		}
		create := tx.QuotaResetRequestNode.Create().
			SetRequestID(request.ID).
			SetPosition(resolved.Position).
			SetNodeType(quotaresetrequestnode.NodeType(resolved.NodeType)).
			SetLabel(resolved.Label).
			SetDepartmentSnapshots(departmentSnapshots).
			SetStatus(status).
			SetAdminFallbackRequired(resolved.AdminFallbackRequired)
		if status == quotaresetrequestnode.StatusActive {
			create.SetActivatedAt(now)
		}
		node, err := create.Save(ctx)
		if err != nil {
			return nil, fmt.Errorf("create request node: %w", err)
		}
		storedNodes = append(storedNodes, node)
		if status == quotaresetrequestnode.StatusActive {
			id := node.ID
			activeNodeID = &id
		}
		for _, approver := range resolved.Approvers {
			if _, err := tx.QuotaResetRequestNodeApprover.Create().
				SetRequestNodeID(node.ID).
				SetUserID(approver.UserID).
				SetDisplayName(approver.DisplayName).
				SetEmail(approver.Email).
				SetSource(quotaresetrequestnodeapprover.Source(approver.Source)).
				SetSourceDepartmentExternalIds(cloneStringSlice(approver.SourceDepartmentExternalIDs)).
				SetNotificationIds(cloneStringMap(approver.NotificationIDs)).
				Save(ctx); err != nil {
				return nil, fmt.Errorf("create request node approver: %w", err)
			}
		}
	}

	update := tx.QuotaResetRequest.UpdateOneID(request.ID)
	if activeNodeID != nil {
		update.SetCurrentNodeID(*activeNodeID)
	} else {
		update.SetStatus(quotaresetrequest.StatusApprovedResetting).ClearCurrentNodeID()
	}
	request, err = update.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("activate workflow: %w", err)
	}
	if err := writeWorkflowEvent(ctx, tx, request.ID, nil, quotaresetrequestevent.EventTypeWorkflowSnapshotted, map[string]any{
		"node_count": len(snapshot.Nodes),
	}); err != nil {
		return nil, err
	}
	for _, node := range storedNodes {
		switch node.Status {
		case quotaresetrequestnode.StatusSkippedNoApprover:
			if err := writeWorkflowEvent(ctx, tx, request.ID, nil, quotaresetrequestevent.EventTypeNodeSkippedNoApprover, map[string]any{
				"node_id":  node.ID,
				"position": node.Position,
			}); err != nil {
				return nil, err
			}
		case quotaresetrequestnode.StatusActive:
			if err := writeNodeActivationEvents(ctx, tx, request.ID, node); err != nil {
				return nil, err
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit workflow snapshot: %w", err)
	}
	txClosed = true
	if activeNodeID == nil {
		return s.executeReset(ctx, request.ID, requester.ID, false, false)
	}
	_ = s.notifyActiveNode(ctx, request.ID, *activeNodeID)
	return request, nil
}

func (s *Service) approveWorkflow(ctx context.Context, input DecisionInput) (*ent.QuotaResetRequest, error) {
	input.DecisionReason = strings.TrimSpace(input.DecisionReason)
	if input.DecisionReason == "" {
		return nil, ErrDecisionRequired
	}
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin workflow approval: %w", err)
	}
	defer tx.Rollback()
	request, node, actor, adminOverride, err := lockAndAuthorizeCurrentNode(ctx, tx, input)
	if err != nil {
		return nil, err
	}
	decision, err := tx.QuotaResetRequestDecision.Create().
		SetRequestID(request.ID).
		SetRequestNodeID(node.ID).
		SetActorUserID(actor.ID).
		SetActorDisplayName(actor.Username).
		SetDecision(quotaresetrequestdecision.DecisionApprove).
		SetComment(input.DecisionReason).
		SetAdminOverride(adminOverride).
		Save(ctx)
	if ent.IsConstraintError(err) {
		return nil, &WorkflowAdvancedError{RequestID: request.ID}
	}
	if err != nil {
		return nil, fmt.Errorf("store workflow approval: %w", err)
	}
	now := time.Now()
	if _, err := tx.QuotaResetRequestNode.UpdateOneID(node.ID).
		SetStatus(quotaresetrequestnode.StatusApproved).
		SetSatisfiedByDecisionID(decision.ID).
		SetCompletedAt(now).
		Save(ctx); err != nil {
		return nil, fmt.Errorf("complete current node: %w", err)
	}
	if err := writeWorkflowEvent(ctx, tx, request.ID, &actor.ID, quotaresetrequestevent.EventTypeNodeApproved, map[string]any{
		"node_id":        node.ID,
		"position":       node.Position,
		"admin_override": adminOverride,
	}); err != nil {
		return nil, err
	}

	later, err := tx.QuotaResetRequestNode.Query().
		Where(
			quotaresetrequestnode.RequestIDEQ(request.ID),
			quotaresetrequestnode.PositionGT(node.Position),
			quotaresetrequestnode.StatusEQ(quotaresetrequestnode.StatusQueued),
		).
		Order(ent.Asc(quotaresetrequestnode.FieldPosition)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("load later workflow nodes: %w", err)
	}
	for _, future := range later {
		eligible, err := tx.QuotaResetRequestNodeApprover.Query().
			Where(
				quotaresetrequestnodeapprover.RequestNodeIDEQ(future.ID),
				quotaresetrequestnodeapprover.UserIDEQ(actor.ID),
			).
			Exist(ctx)
		if err != nil {
			return nil, fmt.Errorf("check reusable approval: %w", err)
		}
		if !eligible {
			continue
		}
		if _, err := tx.QuotaResetRequestNode.UpdateOneID(future.ID).
			SetStatus(quotaresetrequestnode.StatusSatisfiedByPriorApproval).
			SetSatisfiedByDecisionID(decision.ID).
			SetCompletedAt(now).
			Save(ctx); err != nil {
			return nil, fmt.Errorf("reuse workflow approval: %w", err)
		}
		if err := writeWorkflowEvent(ctx, tx, request.ID, &actor.ID, quotaresetrequestevent.EventTypeNodeSatisfiedByPriorApproval, map[string]any{
			"node_id":     future.ID,
			"position":    future.Position,
			"decision_id": decision.ID,
		}); err != nil {
			return nil, err
		}
	}
	request, nextNodeID, err := advanceWorkflowAfterDecision(ctx, tx, request.ID, decision.ID, now)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit workflow approval: %w", err)
	}
	if nextNodeID != nil {
		_ = s.notifyActiveNode(ctx, request.ID, *nextNodeID)
		return request, nil
	}
	return s.executeReset(ctx, request.ID, actor.ID, false, input.Admin)
}

func (s *Service) rejectWorkflow(ctx context.Context, input DecisionInput) (*ent.QuotaResetRequest, error) {
	input.DecisionReason = strings.TrimSpace(input.DecisionReason)
	if input.DecisionReason == "" {
		return nil, ErrDecisionRequired
	}
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin workflow rejection: %w", err)
	}
	defer tx.Rollback()
	request, node, actor, adminOverride, err := lockAndAuthorizeCurrentNode(ctx, tx, input)
	if err != nil {
		return nil, err
	}
	decision, err := tx.QuotaResetRequestDecision.Create().
		SetRequestID(request.ID).
		SetRequestNodeID(node.ID).
		SetActorUserID(actor.ID).
		SetActorDisplayName(actor.Username).
		SetDecision(quotaresetrequestdecision.DecisionReject).
		SetComment(input.DecisionReason).
		SetAdminOverride(adminOverride).
		Save(ctx)
	if ent.IsConstraintError(err) {
		return nil, &WorkflowAdvancedError{RequestID: request.ID}
	}
	if err != nil {
		return nil, fmt.Errorf("store workflow rejection: %w", err)
	}
	now := time.Now()
	if _, err := tx.QuotaResetRequestNode.UpdateOneID(node.ID).
		SetStatus(quotaresetrequestnode.StatusRejected).
		SetSatisfiedByDecisionID(decision.ID).
		SetCompletedAt(now).
		Save(ctx); err != nil {
		return nil, fmt.Errorf("reject current workflow node: %w", err)
	}
	request, err = tx.QuotaResetRequest.UpdateOneID(request.ID).
		SetStatus(quotaresetrequest.StatusRejected).
		ClearCurrentNodeID().
		Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("reject workflow request: %w", err)
	}
	if err := writeWorkflowEvent(ctx, tx, request.ID, &actor.ID, quotaresetrequestevent.EventTypeRejected, map[string]any{
		"node_id":        node.ID,
		"position":       node.Position,
		"decision_id":    decision.ID,
		"admin_override": adminOverride,
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit workflow rejection: %w", err)
	}
	_ = s.notify(ctx, "quota_reset_request_rejected", request)
	return request, nil
}

func (s *Service) retryResetWorkflow(ctx context.Context, input DecisionInput, request *ent.QuotaResetRequest) (*ent.QuotaResetRequest, error) {
	if request.Status != quotaresetrequest.StatusApprovedResetFailed {
		return nil, ErrInvalidStatus
	}
	actor, err := s.client.User.Get(ctx, input.ActorUserID)
	if err != nil {
		return nil, err
	}
	if input.Admin {
		if actor.Role != entuser.RoleAdmin {
			return nil, ErrNotApprover
		}
	} else {
		decisionID := request.WorkflowCompletedByDecisionID
		if decisionID == nil {
			return nil, ErrNotApprover
		}
		decision, err := s.client.QuotaResetRequestDecision.Get(ctx, *decisionID)
		if err != nil {
			return nil, err
		}
		if decision.ActorUserID != input.ActorUserID {
			return nil, ErrNotApprover
		}
	}
	return s.executeReset(ctx, request.ID, input.ActorUserID, true, input.Admin)
}

type workflowRequestLockQueryHookContextKey struct{}

type workflowRequestLockQueryHook func()

func lockAndAuthorizeCurrentNode(ctx context.Context, tx *ent.Tx, input DecisionInput) (*ent.QuotaResetRequest, *ent.QuotaResetRequestNode, *ent.User, bool, error) {
	request, err := tx.QuotaResetRequest.Query().
		Where(
			quotaresetrequest.IDEQ(input.RequestID),
			func(selector *sql.Selector) {
				selector.ForUpdate()
				if hook, ok := ctx.Value(workflowRequestLockQueryHookContextKey{}).(workflowRequestLockQueryHook); ok && hook != nil {
					hook()
				}
			},
		).
		Only(ctx)
	if err != nil {
		return nil, nil, nil, false, fmt.Errorf("lock workflow request: %w", err)
	}
	advanced := func() (*ent.QuotaResetRequest, *ent.QuotaResetRequestNode, *ent.User, bool, error) {
		return nil, nil, nil, false, &WorkflowAdvancedError{RequestID: request.ID}
	}
	if request.WorkflowVersion < WorkflowVersionV2 || request.Status != quotaresetrequest.StatusPending || request.CurrentNodeID == nil || input.RequestNodeID != *request.CurrentNodeID {
		return advanced()
	}
	node, err := tx.QuotaResetRequestNode.Query().
		Where(
			quotaresetrequestnode.IDEQ(input.RequestNodeID),
			quotaresetrequestnode.RequestIDEQ(request.ID),
			func(selector *sql.Selector) { selector.ForUpdate() },
		).
		Only(ctx)
	if ent.IsNotFound(err) {
		return advanced()
	}
	if err != nil {
		return nil, nil, nil, false, fmt.Errorf("lock current workflow node: %w", err)
	}
	if node.Status != quotaresetrequestnode.StatusActive {
		return advanced()
	}
	if request.RequesterUserID == input.ActorUserID {
		return nil, nil, nil, false, ErrSelfApprovalForbidden
	}
	actor, err := tx.User.Get(ctx, input.ActorUserID)
	if err != nil {
		return nil, nil, nil, false, fmt.Errorf("load workflow actor: %w", err)
	}
	normalApprover, err := tx.QuotaResetRequestNodeApprover.Query().
		Where(
			quotaresetrequestnodeapprover.RequestNodeIDEQ(node.ID),
			quotaresetrequestnodeapprover.UserIDEQ(actor.ID),
		).
		Exist(ctx)
	if err != nil {
		return nil, nil, nil, false, fmt.Errorf("authorize workflow approver: %w", err)
	}
	currentAdmin := input.Admin && actor.Role == entuser.RoleAdmin
	if !normalApprover && !currentAdmin {
		return nil, nil, nil, false, ErrNotApprover
	}
	return request, node, actor, currentAdmin && !normalApprover, nil
}

func advanceWorkflowAfterDecision(ctx context.Context, tx *ent.Tx, requestID, decisionID int, now time.Time) (*ent.QuotaResetRequest, *int, error) {
	next, err := tx.QuotaResetRequestNode.Query().
		Where(
			quotaresetrequestnode.RequestIDEQ(requestID),
			quotaresetrequestnode.StatusEQ(quotaresetrequestnode.StatusQueued),
		).
		Order(ent.Asc(quotaresetrequestnode.FieldPosition)).
		First(ctx)
	update := tx.QuotaResetRequest.UpdateOneID(requestID)
	if err == nil {
		next, err = tx.QuotaResetRequestNode.UpdateOneID(next.ID).
			SetStatus(quotaresetrequestnode.StatusActive).
			SetActivatedAt(now).
			Save(ctx)
		if err != nil {
			return nil, nil, fmt.Errorf("activate next workflow node: %w", err)
		}
		if err := writeNodeActivationEvents(ctx, tx, requestID, next); err != nil {
			return nil, nil, err
		}
		request, err := update.SetCurrentNodeID(next.ID).Save(ctx)
		if err != nil {
			return nil, nil, fmt.Errorf("advance workflow request: %w", err)
		}
		nextNodeID := next.ID
		return request, &nextNodeID, nil
	}
	if !ent.IsNotFound(err) {
		return nil, nil, fmt.Errorf("load next workflow node: %w", err)
	}
	request, err := update.
		SetStatus(quotaresetrequest.StatusApprovedResetting).
		SetWorkflowCompletedByDecisionID(decisionID).
		ClearCurrentNodeID().
		Save(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("complete workflow request: %w", err)
	}
	return request, nil, nil
}

func writeNodeActivationEvents(ctx context.Context, tx *ent.Tx, requestID int, node *ent.QuotaResetRequestNode) error {
	if err := writeWorkflowEvent(ctx, tx, requestID, nil, quotaresetrequestevent.EventTypeNodeActivated, map[string]any{
		"node_id":        node.ID,
		"position":       node.Position,
		"admin_fallback": node.AdminFallbackRequired,
	}); err != nil {
		return err
	}
	if node.AdminFallbackRequired {
		if err := writeWorkflowEvent(ctx, tx, requestID, nil, quotaresetrequestevent.EventTypeAdminFallbackActivated, map[string]any{
			"node_id":  node.ID,
			"position": node.Position,
		}); err != nil {
			return err
		}
	}
	return nil
}

func writeWorkflowEvent(ctx context.Context, tx *ent.Tx, requestID int, actorUserID *int, eventType quotaresetrequestevent.EventType, metadata map[string]any) error {
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
		return fmt.Errorf("write workflow event: %w", err)
	}
	return nil
}

func departmentSnapshotsToMaps(snapshots []DepartmentSnapshot) ([]map[string]any, error) {
	data, err := json.Marshal(snapshots)
	if err != nil {
		return nil, fmt.Errorf("marshal department snapshots: %w", err)
	}
	result := []map[string]any{}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("convert department snapshots: %w", err)
	}
	return result, nil
}

func cloneStringMap(values map[string]string) map[string]string {
	cloned := make(map[string]string, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func cloneStringSlice(values []string) []string {
	cloned := make([]string, len(values))
	copy(cloned, values)
	return cloned
}

func (s *Service) notifyActiveNode(ctx context.Context, requestID, requestNodeID int) error {
	if requestNodeID <= 0 {
		return nil
	}
	request, err := s.client.QuotaResetRequest.Get(ctx, requestID)
	if err != nil {
		return err
	}
	return s.notify(ctx, "quota_reset_approval_node_activated", request)
}
