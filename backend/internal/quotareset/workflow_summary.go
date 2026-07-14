package quotareset

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"entgo.io/ent/dialect/sql"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/directorymember"
	"github.com/ai-efficiency/backend/ent/predicate"
	"github.com/ai-efficiency/backend/ent/quotaresetrequest"
	"github.com/ai-efficiency/backend/ent/quotaresetrequestdecision"
	"github.com/ai-efficiency/backend/ent/quotaresetrequestnode"
	"github.com/ai-efficiency/backend/ent/quotaresetrequestnodeapprover"
	entuser "github.com/ai-efficiency/backend/ent/user"
	"github.com/ai-efficiency/backend/internal/directorysync"
)

type summaryViewer struct {
	UserID    int
	Admin     bool
	Requester bool
}

func (s *Service) GetRequestSummary(ctx context.Context, requestID, viewerUserID int, admin bool) (*RequestSummary, error) {
	request, err := s.client.QuotaResetRequest.Get(ctx, requestID)
	if err != nil {
		return nil, fmt.Errorf("load quota reset request summary: %w", err)
	}
	viewer, err := s.authorizeRequestSummaryViewer(ctx, request, viewerUserID, admin)
	if err != nil {
		return nil, err
	}
	items, err := s.summaries(ctx, []*ent.QuotaResetRequest{request}, viewer)
	if err != nil {
		return nil, err
	}
	if len(items) != 1 {
		return nil, fmt.Errorf("load quota reset request summary: request %d produced %d summaries", requestID, len(items))
	}
	if request.WorkflowVersion >= WorkflowVersionV2 && items[0].Workflow == nil {
		return nil, ErrNotApprover
	}
	return &items[0], nil
}

func (s *Service) authorizeRequestSummaryViewer(ctx context.Context, request *ent.QuotaResetRequest, viewerUserID int, adminRoute bool) (summaryViewer, error) {
	viewer := summaryViewer{
		UserID:    viewerUserID,
		Requester: request.RequesterUserID == viewerUserID,
	}
	if adminRoute {
		actor, err := s.client.User.Get(ctx, viewerUserID)
		if err != nil && !ent.IsNotFound(err) {
			return summaryViewer{}, fmt.Errorf("load quota reset summary viewer: %w", err)
		}
		if err == nil && actor.Role == entuser.RoleAdmin {
			viewer.Admin = true
		}
	}
	if viewer.Requester || viewer.Admin {
		return viewer, nil
	}
	if request.WorkflowVersion >= WorkflowVersionV2 {
		visible, err := s.v2RequestSummaryVisible(ctx, request, viewerUserID)
		if err != nil {
			return summaryViewer{}, err
		}
		if visible {
			return viewer, nil
		}
		return summaryViewer{}, ErrNotApprover
	}
	if isResolvedApprover(request, viewerUserID) ||
		(request.ApprovedByUserID != nil && *request.ApprovedByUserID == viewerUserID) ||
		(request.RejectedByUserID != nil && *request.RejectedByUserID == viewerUserID) {
		return viewer, nil
	}
	return summaryViewer{}, ErrNotApprover
}

func (s *Service) v2RequestSummaryVisible(ctx context.Context, request *ent.QuotaResetRequest, viewerUserID int) (bool, error) {
	decisionActor, err := s.client.QuotaResetRequestDecision.Query().
		Where(
			quotaresetrequestdecision.RequestIDEQ(request.ID),
			quotaresetrequestdecision.ActorUserIDEQ(viewerUserID),
		).
		Exist(ctx)
	if err != nil {
		return false, fmt.Errorf("authorize quota reset decision viewer: %w", err)
	}
	if decisionActor {
		return true, nil
	}
	nodes, err := s.client.QuotaResetRequestNode.Query().
		Where(quotaresetrequestnode.RequestIDEQ(request.ID)).
		All(ctx)
	if err != nil {
		return false, fmt.Errorf("authorize quota reset node viewer: %w", err)
	}
	visibleNodeIDs := make([]int, 0, len(nodes))
	for _, node := range nodes {
		if workflowNodeIsCurrent(request, node) || workflowNodeIsHistorical(node) {
			visibleNodeIDs = append(visibleNodeIDs, node.ID)
		}
	}
	if len(visibleNodeIDs) == 0 {
		return false, nil
	}
	visible, err := s.client.QuotaResetRequestNodeApprover.Query().
		Where(
			quotaresetrequestnodeapprover.RequestNodeIDIn(visibleNodeIDs...),
			quotaresetrequestnodeapprover.UserIDEQ(viewerUserID),
		).
		Exist(ctx)
	if err != nil {
		return false, fmt.Errorf("authorize quota reset approver viewer: %w", err)
	}
	return visible, nil
}

func (s *Service) enrichWorkflowAdvancedError(ctx context.Context, err error, viewerUserID int, admin bool) error {
	var advanced *WorkflowAdvancedError
	if !errors.As(err, &advanced) || advanced == nil {
		return err
	}
	latest, latestErr := s.GetRequestSummary(ctx, advanced.RequestID, viewerUserID, admin)
	if latestErr == nil {
		advanced.Latest = latest
	} else if errors.Is(latestErr, ErrNotApprover) {
		return ErrNotApprover
	}
	return err
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
	viewerWasApprover := false
	for _, node := range nodes {
		departments, err := workflowDepartmentSnapshots(node.DepartmentSnapshots)
		if err != nil {
			return nil, fmt.Errorf("decode quota reset workflow node %d departments: %w", node.ID, err)
		}
		approverSummaries := make([]WorkflowNodeApproverSummary, 0, len(approversByNode[node.ID]))
		historicalNode := workflowNodeIsHistorical(node)
		for _, approver := range approversByNode[node.ID] {
			if historicalNode && approver.UserID == viewer.UserID {
				viewerWasApprover = true
			}
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
	if !requester && !viewer.Admin && !activeCandidate && !viewerHasDecision && !viewerWasApprover {
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

func workflowNodeIsCurrent(request *ent.QuotaResetRequest, node *ent.QuotaResetRequestNode) bool {
	return request.CurrentNodeID != nil && *request.CurrentNodeID == node.ID && node.Status == quotaresetrequestnode.StatusActive
}

func workflowNodeIsHistorical(node *ent.QuotaResetRequestNode) bool {
	return node.Status != quotaresetrequestnode.StatusQueued && node.Status != quotaresetrequestnode.StatusActive
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

func (s *Service) notificationContextForRequest(ctx context.Context, requestID, nodeID int, event NotificationEvent) (NotificationContext, error) {
	request, err := s.client.QuotaResetRequest.Get(ctx, requestID)
	if err != nil {
		return NotificationContext{}, fmt.Errorf("load quota reset request for notification: %w", err)
	}
	requester, err := s.notificationRequester(ctx, request)
	if err != nil {
		return NotificationContext{}, err
	}
	nodes, err := s.client.QuotaResetRequestNode.Query().
		Where(quotaresetrequestnode.RequestIDEQ(request.ID)).
		Order(ent.Asc(quotaresetrequestnode.FieldPosition), ent.Asc(quotaresetrequestnode.FieldID)).
		All(ctx)
	if err != nil {
		return NotificationContext{}, fmt.Errorf("load quota reset nodes for notification: %w", err)
	}
	nodeIDs := make([]int, 0, len(nodes))
	for _, node := range nodes {
		nodeIDs = append(nodeIDs, node.ID)
	}
	approvers := make([]*ent.QuotaResetRequestNodeApprover, 0)
	if len(nodeIDs) > 0 {
		approvers, err = s.client.QuotaResetRequestNodeApprover.Query().
			Where(quotaresetrequestnodeapprover.RequestNodeIDIn(nodeIDs...)).
			Order(ent.Asc(quotaresetrequestnodeapprover.FieldRequestNodeID), ent.Asc(quotaresetrequestnodeapprover.FieldUserID), ent.Asc(quotaresetrequestnodeapprover.FieldID)).
			All(ctx)
		if err != nil {
			return NotificationContext{}, fmt.Errorf("load quota reset approvers for notification: %w", err)
		}
	}
	decisions, err := s.client.QuotaResetRequestDecision.Query().
		Where(quotaresetrequestdecision.RequestIDEQ(request.ID)).
		Order(ent.Asc(quotaresetrequestdecision.FieldCreatedAt), ent.Asc(quotaresetrequestdecision.FieldID)).
		All(ctx)
	if err != nil {
		return NotificationContext{}, fmt.Errorf("load quota reset decisions for notification: %w", err)
	}

	approversByNode := make(map[int][]NotificationPerson, len(nodes))
	approversByUser := make(map[int]NotificationPerson)
	for _, approver := range approvers {
		person := notificationPersonFromApprover(approver)
		approversByNode[approver.RequestNodeID] = append(approversByNode[approver.RequestNodeID], person)
		if _, exists := approversByUser[person.UserID]; !exists {
			approversByUser[person.UserID] = person
		}
	}
	if nodeID <= 0 && request.CurrentNodeID != nil {
		nodeID = *request.CurrentNodeID
	}
	completionDecision := workflowCompletionDecision(request, decisions)
	if nodeID <= 0 && completionDecision != nil {
		nodeID = completionDecision.RequestNodeID
	}
	var currentNode *NotificationNode
	var currentNodeRow *ent.QuotaResetRequestNode
	for _, node := range nodes {
		if node.ID != nodeID {
			continue
		}
		currentNodeRow = node
		currentNode = &NotificationNode{
			ID:            node.ID,
			Position:      node.Position,
			Total:         len(nodes),
			Label:         node.Label,
			Approvers:     append([]NotificationPerson(nil), approversByNode[node.ID]...),
			AdminFallback: node.AdminFallbackRequired,
		}
		break
	}
	if event == NotificationNodeActivated && (request.Status != quotaresetrequest.StatusPending ||
		request.CurrentNodeID == nil ||
		*request.CurrentNodeID != nodeID ||
		currentNodeRow == nil ||
		currentNodeRow.Status != quotaresetrequestnode.StatusActive) {
		return NotificationContext{}, fmt.Errorf("build node activation notification: node %d is not the active pending request node", nodeID)
	}

	history := make([]NotificationDecision, 0, len(decisions))
	for _, decision := range decisions {
		history = append(history, NotificationDecision{
			ActorDisplayName: decision.ActorDisplayName,
			Decision:         decision.Decision.String(),
			Comment:          decision.Comment,
			CreatedAt:        decision.CreatedAt.UTC(),
		})
	}
	recipients, err := s.notificationRecipients(ctx, request, event, currentNode, completionDecision, approversByUser, requester)
	if err != nil {
		return NotificationContext{}, err
	}
	return NotificationContext{
		Event:           event,
		OccurredAt:      time.Now().UTC(),
		RequestID:       request.ID,
		Status:          request.Status.String(),
		Requester:       requester,
		Recipients:      uniqueNotificationPeople(recipients),
		DepartmentPaths: append([]string(nil), request.RequesterDepartmentPaths...),
		GroupID:         request.GroupID,
		GroupName:       request.GroupName,
		GroupPlatform:   request.GroupPlatform,
		Reason:          request.Reason,
		CurrentNode:     currentNode,
		ApprovalHistory: history,
		ActionURL:       s.notificationActionURL(request.ID),
	}, nil
}

func (s *Service) notificationRequester(ctx context.Context, request *ent.QuotaResetRequest) (NotificationPerson, error) {
	if request.WorkflowVersion >= WorkflowVersionV2 {
		return NotificationPerson{
			UserID:          request.RequesterUserID,
			DisplayName:     request.RequesterDisplayNameSnapshot,
			Email:           request.RequesterEmailSnapshot,
			NotificationIDs: cloneStringMap(request.RequesterNotificationIds),
		}, nil
	}
	people, err := s.currentNotificationPeopleForUserIDs(ctx, []int{request.RequesterUserID})
	if err != nil {
		return NotificationPerson{}, fmt.Errorf("resolve legacy requester notification identity: %w", err)
	}
	if len(people) == 0 {
		return NotificationPerson{UserID: request.RequesterUserID, NotificationIDs: map[string]string{}}, nil
	}
	return people[0], nil
}

func (s *Service) notificationRecipients(ctx context.Context, request *ent.QuotaResetRequest, event NotificationEvent, currentNode *NotificationNode, completionDecision *ent.QuotaResetRequestDecision, approversByUser map[int]NotificationPerson, requester NotificationPerson) ([]NotificationPerson, error) {
	switch event {
	case NotificationNodeActivated:
		if currentNode != nil && currentNode.AdminFallback {
			return s.currentAdminNotificationPeople(ctx)
		}
		if currentNode != nil {
			return append([]NotificationPerson(nil), currentNode.Approvers...), nil
		}
	case NotificationRejected, NotificationResetSucceeded:
		return []NotificationPerson{requester}, nil
	case NotificationCancelled:
		if currentNode != nil && currentNode.AdminFallback {
			return s.currentAdminNotificationPeople(ctx)
		}
		if currentNode != nil {
			return append([]NotificationPerson(nil), currentNode.Approvers...), nil
		}
		return s.currentNotificationPeopleForUserIDs(ctx, request.ResolvedApproverUserIds)
	case NotificationResetFailed:
		recipients := []NotificationPerson{requester}
		if completionDecision != nil {
			actor := NotificationPerson{
				UserID:          completionDecision.ActorUserID,
				DisplayName:     completionDecision.ActorDisplayName,
				NotificationIDs: map[string]string{},
			}
			if snapshot, exists := approversByUser[actor.UserID]; exists {
				mergeNotificationPerson(&actor, snapshot)
			}
			recipients = append(recipients, actor)
		} else if request.ApprovedByUserID != nil {
			legacyActor, err := s.currentNotificationPeopleForUserIDs(ctx, []int{*request.ApprovedByUserID})
			if err != nil {
				return nil, err
			}
			recipients = append(recipients, legacyActor...)
		}
		admins, err := s.currentAdminNotificationPeople(ctx)
		if err != nil {
			return nil, err
		}
		return append(recipients, admins...), nil
	case NotificationTest:
		return nil, nil
	}
	return []NotificationPerson{}, nil
}

func workflowCompletionDecision(request *ent.QuotaResetRequest, decisions []*ent.QuotaResetRequestDecision) *ent.QuotaResetRequestDecision {
	if request.WorkflowCompletedByDecisionID == nil {
		return nil
	}
	for _, decision := range decisions {
		if decision.ID == *request.WorkflowCompletedByDecisionID {
			return decision
		}
	}
	return nil
}

func notificationPersonFromApprover(approver *ent.QuotaResetRequestNodeApprover) NotificationPerson {
	return NotificationPerson{
		UserID:          approver.UserID,
		DisplayName:     approver.DisplayName,
		Email:           approver.Email,
		NotificationIDs: cloneStringMap(approver.NotificationIds),
	}
}

func (s *Service) currentAdminNotificationPeople(ctx context.Context) ([]NotificationPerson, error) {
	admins, err := s.client.User.Query().
		Where(entuser.RoleEQ(entuser.RoleAdmin)).
		Order(ent.Asc(entuser.FieldID)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("load current admins for notification: %w", err)
	}
	return s.currentNotificationPeople(ctx, admins)
}

func (s *Service) currentNotificationPeopleForUserIDs(ctx context.Context, userIDs []int) ([]NotificationPerson, error) {
	orderedIDs := make([]int, 0, len(userIDs))
	seen := make(map[int]struct{}, len(userIDs))
	for _, userID := range userIDs {
		if userID <= 0 {
			continue
		}
		if _, exists := seen[userID]; exists {
			continue
		}
		seen[userID] = struct{}{}
		orderedIDs = append(orderedIDs, userID)
	}
	if len(orderedIDs) == 0 {
		return []NotificationPerson{}, nil
	}
	users, err := s.client.User.Query().Where(entuser.IDIn(orderedIDs...)).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("load notification users: %w", err)
	}
	usersByID := make(map[int]*ent.User, len(users))
	for _, user := range users {
		usersByID[user.ID] = user
	}
	orderedUsers := make([]*ent.User, 0, len(users))
	for _, userID := range orderedIDs {
		if user := usersByID[userID]; user != nil {
			orderedUsers = append(orderedUsers, user)
		}
	}
	return s.currentNotificationPeople(ctx, orderedUsers)
}

func (s *Service) currentNotificationPeople(ctx context.Context, users []*ent.User) ([]NotificationPerson, error) {
	people := make([]NotificationPerson, 0, len(users))
	usersByID := make(map[int]*ent.User, len(users))
	usersByEmail := make(map[string]*ent.User, len(users))
	requestedUserIDs := make([]int, 0, len(users))
	requestedEmails := make([]string, 0, len(users))
	for _, user := range users {
		if user == nil {
			continue
		}
		if _, exists := usersByID[user.ID]; !exists {
			requestedUserIDs = append(requestedUserIDs, user.ID)
		}
		usersByID[user.ID] = user
		if email := strings.ToLower(strings.TrimSpace(user.Email)); email != "" {
			if _, exists := usersByEmail[email]; !exists {
				requestedEmails = append(requestedEmails, email)
			}
			usersByEmail[email] = user
		}
	}
	membersByUserID := make(map[int]*ent.DirectoryMember)
	sourceID, ok, err := directorysync.CurrentSourceID(ctx, s.client)
	if err != nil {
		return nil, fmt.Errorf("resolve current directory for notification: %w", err)
	}
	if ok && len(usersByID) > 0 {
		matches := make([]predicate.DirectoryMember, 0, 2)
		if len(requestedUserIDs) > 0 {
			matches = append(matches, directorymember.MatchedUserIDIn(requestedUserIDs...))
		}
		if len(requestedEmails) > 0 {
			matches = append(matches, directorymember.EmailNormalizedIn(requestedEmails...))
		}
		members, err := s.client.DirectoryMember.Query().
			Where(
				directorymember.SourceIDEQ(sourceID),
				directorymember.Or(matches...),
			).
			Order(ent.Asc(directorymember.FieldID)).
			All(ctx)
		if err != nil {
			return nil, fmt.Errorf("load current directory notification identities: %w", err)
		}
		for _, member := range members {
			if !workflowMemberIsActive(member) {
				continue
			}
			user := workflowMemberUser(member, usersByID, usersByEmail)
			if user == nil || membersByUserID[user.ID] != nil {
				continue
			}
			membersByUserID[user.ID] = member
		}
	}
	for _, user := range users {
		if user == nil {
			continue
		}
		member := membersByUserID[user.ID]
		people = append(people, NotificationPerson{
			UserID:          user.ID,
			DisplayName:     firstNonEmptyQuotaReset(requesterMemberDisplayName(member), user.Username),
			Email:           user.Email,
			NotificationIDs: notificationIDsForMember(member),
		})
	}
	return people, nil
}

func (s *Service) notificationActionURL(requestID int) string {
	if provider, ok := s.notifier.(interface{ notificationActionURL(int) string }); ok {
		return provider.notificationActionURL(requestID)
	}
	return fmt.Sprintf("/usage/quota-reset?request_id=%d", requestID)
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
