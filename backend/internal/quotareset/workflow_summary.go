package quotareset

import (
	"context"
	"encoding/json"
	"fmt"

	"entgo.io/ent/dialect/sql"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/predicate"
	"github.com/ai-efficiency/backend/ent/quotaresetrequest"
	"github.com/ai-efficiency/backend/ent/quotaresetrequestdecision"
	"github.com/ai-efficiency/backend/ent/quotaresetrequestnode"
	"github.com/ai-efficiency/backend/ent/quotaresetrequestnodeapprover"
)

type summaryViewer struct {
	UserID    int
	Admin     bool
	Requester bool
}

func (s *Service) loadWorkflowSummary(ctx context.Context, request *ent.QuotaResetRequest, viewer summaryViewer) (*WorkflowSummary, error) {
	summaries, err := s.loadWorkflowSummaries(ctx, []*ent.QuotaResetRequest{request}, viewer)
	if err != nil {
		return nil, err
	}
	return summaries[request.ID], nil
}

func (s *Service) loadWorkflowSummaries(ctx context.Context, requests []*ent.QuotaResetRequest, viewer summaryViewer) (map[int]*WorkflowSummary, error) {
	requestIDs := make([]int, 0, len(requests))
	workflowRequests := make(map[int]*ent.QuotaResetRequest, len(requests))
	for _, request := range requests {
		if request != nil && request.WorkflowVersion >= WorkflowVersionV2 {
			requestIDs = append(requestIDs, request.ID)
			workflowRequests[request.ID] = request
		}
	}
	if len(requestIDs) == 0 {
		return map[int]*WorkflowSummary{}, nil
	}

	nodes, err := s.client.QuotaResetRequestNode.Query().
		Where(quotaresetrequestnode.RequestIDIn(requestIDs...)).
		Order(ent.Asc(quotaresetrequestnode.FieldRequestID), ent.Asc(quotaresetrequestnode.FieldPosition), ent.Asc(quotaresetrequestnode.FieldID)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("load quota reset workflow nodes: %w", err)
	}
	nodeIDs := make([]int, 0, len(nodes))
	for _, node := range nodes {
		nodeIDs = append(nodeIDs, node.ID)
	}
	approvers := []*ent.QuotaResetRequestNodeApprover{}
	if len(nodeIDs) > 0 {
		approvers, err = s.client.QuotaResetRequestNodeApprover.Query().
			Where(quotaresetrequestnodeapprover.RequestNodeIDIn(nodeIDs...)).
			Order(ent.Asc(quotaresetrequestnodeapprover.FieldRequestNodeID), ent.Asc(quotaresetrequestnodeapprover.FieldUserID), ent.Asc(quotaresetrequestnodeapprover.FieldID)).
			All(ctx)
		if err != nil {
			return nil, fmt.Errorf("load quota reset workflow approvers: %w", err)
		}
	}
	decisions, err := s.client.QuotaResetRequestDecision.Query().
		Where(quotaresetrequestdecision.RequestIDIn(requestIDs...)).
		Order(ent.Asc(quotaresetrequestdecision.FieldRequestID), ent.Asc(quotaresetrequestdecision.FieldCreatedAt), ent.Asc(quotaresetrequestdecision.FieldID)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("load quota reset workflow decisions: %w", err)
	}

	nodesByRequest := make(map[int][]*ent.QuotaResetRequestNode, len(workflowRequests))
	for _, node := range nodes {
		nodesByRequest[node.RequestID] = append(nodesByRequest[node.RequestID], node)
	}
	approversByNode := make(map[int][]*ent.QuotaResetRequestNodeApprover, len(nodeIDs))
	for _, approver := range approvers {
		approversByNode[approver.RequestNodeID] = append(approversByNode[approver.RequestNodeID], approver)
	}
	decisionsByRequest := make(map[int][]*ent.QuotaResetRequestDecision, len(workflowRequests))
	for _, decision := range decisions {
		decisionsByRequest[decision.RequestID] = append(decisionsByRequest[decision.RequestID], decision)
	}

	result := make(map[int]*WorkflowSummary, len(workflowRequests))
	for requestID, request := range workflowRequests {
		summary, err := buildWorkflowSummary(request, viewer, nodesByRequest[requestID], approversByNode, decisionsByRequest[requestID])
		if err != nil {
			return nil, err
		}
		result[requestID] = summary
	}
	return result, nil
}

func buildWorkflowSummary(request *ent.QuotaResetRequest, viewer summaryViewer, nodes []*ent.QuotaResetRequestNode, approversByNode map[int][]*ent.QuotaResetRequestNodeApprover, decisions []*ent.QuotaResetRequestDecision) (*WorkflowSummary, error) {
	nodeSummaries := make([]WorkflowNodeSummary, 0, len(nodes))
	nodeIndexes := make(map[int]int, len(nodes))
	for _, node := range nodes {
		departments, err := workflowDepartmentSnapshots(node.DepartmentSnapshots)
		if err != nil {
			return nil, fmt.Errorf("decode quota reset workflow node %d departments: %w", node.ID, err)
		}
		approverSummaries := make([]WorkflowNodeApproverSummary, 0, len(approversByNode[node.ID]))
		for _, approver := range approversByNode[node.ID] {
			approverSummaries = append(approverSummaries, WorkflowNodeApproverSummary{
				UserID:      approver.UserID,
				DisplayName: approver.DisplayName,
				Email:       approver.Email,
				Source:      approver.Source.String(),
			})
		}
		nodeIndexes[node.ID] = len(nodeSummaries)
		nodeSummaries = append(nodeSummaries, WorkflowNodeSummary{
			ID:                    node.ID,
			Position:              node.Position,
			NodeType:              node.NodeType.String(),
			Label:                 node.Label,
			Departments:           departments,
			Status:                node.Status.String(),
			AdminFallbackRequired: node.AdminFallbackRequired,
			Approvers:             approverSummaries,
			SatisfiedByDecisionID: node.SatisfiedByDecisionID,
		})
	}

	decisionSummaries := make([]WorkflowDecisionSummary, 0, len(decisions))
	viewerHasDecision := false
	completionDecisionActorID := 0
	for _, decision := range decisions {
		decisionSummaries = append(decisionSummaries, WorkflowDecisionSummary{
			ID:               decision.ID,
			NodeID:           decision.RequestNodeID,
			ActorUserID:      decision.ActorUserID,
			ActorDisplayName: decision.ActorDisplayName,
			Decision:         decision.Decision.String(),
			Comment:          decision.Comment,
			AdminOverride:    decision.AdminOverride,
			CreatedAt:        decision.CreatedAt,
		})
		if decision.ActorUserID == viewer.UserID {
			viewerHasDecision = true
		}
		if request.WorkflowCompletedByDecisionID != nil && decision.ID == *request.WorkflowCompletedByDecisionID {
			completionDecisionActorID = decision.ActorUserID
		}
	}

	var currentNode *WorkflowNodeSummary
	if request.CurrentNodeID != nil {
		if index, ok := nodeIndexes[*request.CurrentNodeID]; ok {
			currentNode = &nodeSummaries[index]
		}
	}
	requester := request.RequesterUserID == viewer.UserID
	activeCandidate := currentNode != nil && currentNode.Status == quotaresetrequestnode.StatusActive.String() && workflowNodeHasApprover(*currentNode, viewer.UserID)
	if !requester && !viewer.Admin && !activeCandidate && !viewerHasDecision {
		return nil, nil
	}
	canDecide := request.Status == quotaresetrequest.StatusPending &&
		currentNode != nil &&
		currentNode.Status == quotaresetrequestnode.StatusActive.String() &&
		request.RequesterUserID != viewer.UserID &&
		(viewer.Admin || activeCandidate)
	canCancel := request.Status == quotaresetrequest.StatusPending && requester
	canRetry := request.Status == quotaresetrequest.StatusApprovedResetFailed &&
		(viewer.Admin || completionDecisionActorID == viewer.UserID)

	return &WorkflowSummary{
		Version:     request.WorkflowVersion,
		CurrentNode: currentNode,
		Nodes:       nodeSummaries,
		Decisions:   decisionSummaries,
		CanApprove:  canDecide,
		CanReject:   canDecide,
		CanCancel:   canCancel,
		CanRetry:    canRetry,
	}, nil
}

func workflowDepartmentSnapshots(raw []map[string]any) ([]DepartmentSnapshot, error) {
	if len(raw) == 0 {
		return []DepartmentSnapshot{}, nil
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var snapshots []DepartmentSnapshot
	if err := json.Unmarshal(encoded, &snapshots); err != nil {
		return nil, err
	}
	return snapshots, nil
}

func workflowNodeHasApprover(node WorkflowNodeSummary, userID int) bool {
	for _, approver := range node.Approvers {
		if approver.UserID == userID {
			return true
		}
	}
	return false
}

func legacyApproverJSONPredicate(userID int) predicate.QuotaResetRequest {
	encoded, _ := json.Marshal([]int{userID})
	return func(selector *sql.Selector) {
		selector.Where(sql.P(func(builder *sql.Builder) {
			builder.WriteString(selector.C(quotaresetrequest.FieldResolvedApproverUserIds)).
				WriteString("::jsonb @> ").
				Arg(string(encoded))
		}))
	}
}

func v2ActiveApproverPredicate(userID int) predicate.QuotaResetRequest {
	return func(requests *sql.Selector) {
		nodes := sql.Table(quotaresetrequestnode.Table)
		approvers := sql.Table(quotaresetrequestnodeapprover.Table)
		approverExists := sql.SelectExpr(sql.Expr("1")).
			From(approvers).
			Where(sql.And(
				sql.ColumnsEQ(approvers.C(quotaresetrequestnodeapprover.FieldRequestNodeID), nodes.C(quotaresetrequestnode.FieldID)),
				sql.EQ(approvers.C(quotaresetrequestnodeapprover.FieldUserID), userID),
			))
		nodeExists := sql.SelectExpr(sql.Expr("1")).
			From(nodes).
			Where(sql.And(
				sql.ColumnsEQ(nodes.C(quotaresetrequestnode.FieldRequestID), requests.C(quotaresetrequest.FieldID)),
				sql.ColumnsEQ(nodes.C(quotaresetrequestnode.FieldID), requests.C(quotaresetrequest.FieldCurrentNodeID)),
				sql.EQ(nodes.C(quotaresetrequestnode.FieldStatus), string(quotaresetrequestnode.StatusActive)),
				sql.Exists(approverExists),
			))
		requests.Where(sql.Exists(nodeExists))
	}
}

func v2CompletionActorPredicate(userID int) predicate.QuotaResetRequest {
	return func(requests *sql.Selector) {
		decisions := sql.Table(quotaresetrequestdecision.Table)
		decisionExists := sql.SelectExpr(sql.Expr("1")).
			From(decisions).
			Where(sql.And(
				sql.ColumnsEQ(decisions.C(quotaresetrequestdecision.FieldRequestID), requests.C(quotaresetrequest.FieldID)),
				sql.ColumnsEQ(decisions.C(quotaresetrequestdecision.FieldID), requests.C(quotaresetrequest.FieldWorkflowCompletedByDecisionID)),
				sql.EQ(decisions.C(quotaresetrequestdecision.FieldActorUserID), userID),
			))
		requests.Where(sql.Exists(decisionExists))
	}
}

func v2DecisionActorPredicate(userID int) predicate.QuotaResetRequest {
	return func(requests *sql.Selector) {
		decisions := sql.Table(quotaresetrequestdecision.Table)
		decisionExists := sql.SelectExpr(sql.Expr("1")).
			From(decisions).
			Where(sql.And(
				sql.ColumnsEQ(decisions.C(quotaresetrequestdecision.FieldRequestID), requests.C(quotaresetrequest.FieldID)),
				sql.EQ(decisions.C(quotaresetrequestdecision.FieldActorUserID), userID),
			))
		requests.Where(sql.Exists(decisionExists))
	}
}
