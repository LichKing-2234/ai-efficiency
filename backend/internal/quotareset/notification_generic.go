package quotareset

import (
	"fmt"
	"net/http"
	"time"
)

const (
	genericNotificationMaxBodyBytes            = 64 * 1024
	genericNotificationEventMaxBytes           = 128
	genericNotificationStatusMaxBytes          = 128
	genericNotificationDisplayNameMaxBytes     = 256
	genericNotificationEmailMaxBytes           = 320
	genericNotificationDepartmentPathMaxBytes  = 512
	genericNotificationDepartmentPathLimit     = 16
	genericNotificationGroupIDMaxBytes         = 512
	genericNotificationGroupNameMaxBytes       = 512
	genericNotificationGroupPlatformMaxBytes   = 128
	genericNotificationReasonMaxBytes          = 4096
	genericNotificationNodeLabelMaxBytes       = 512
	genericNotificationNodeApproverLimit       = 20
	genericNotificationDecisionMaxBytes        = 128
	genericNotificationDecisionCommentMaxBytes = 512
	genericNotificationApprovalHistoryLimit    = 16
	genericNotificationActionURLMaxBytes       = 4096
)

type genericWebhookAdapter struct{}

func (genericWebhookAdapter) Render(ctx NotificationContext) (RenderedNotification, error) {
	payload := genericNotificationPayload{
		SchemaVersion: 2,
		Event:         NotificationEvent(truncateUTF8(string(ctx.Event), genericNotificationEventMaxBytes)),
		Request: genericNotificationRequest{
			ID:     ctx.RequestID,
			Status: truncateUTF8(ctx.Status, genericNotificationStatusMaxBytes),
			Requester: genericNotificationRequester{
				DisplayName: truncateUTF8(ctx.Requester.DisplayName, genericNotificationDisplayNameMaxBytes),
				Email:       truncateUTF8(ctx.Requester.Email, genericNotificationEmailMaxBytes),
				Departments: boundedGenericStrings(ctx.DepartmentPaths, genericNotificationDepartmentPathLimit, genericNotificationDepartmentPathMaxBytes),
			},
			SubscriptionGroup: genericNotificationGroup{
				ID:       truncateUTF8(ctx.GroupID, genericNotificationGroupIDMaxBytes),
				Name:     truncateUTF8(ctx.GroupName, genericNotificationGroupNameMaxBytes),
				Platform: truncateUTF8(ctx.GroupPlatform, genericNotificationGroupPlatformMaxBytes),
			},
			Reason: truncateUTF8(ctx.Reason, genericNotificationReasonMaxBytes),
		},
		CurrentNode:     boundedGenericNode(ctx.CurrentNode),
		ApprovalHistory: boundedGenericApprovalHistory(ctx.ApprovalHistory),
		ActionURL:       truncateUTF8(ctx.ActionURL, genericNotificationActionURLMaxBytes),
		OccurredAt:      ctx.OccurredAt.UTC().Format(time.RFC3339),
	}
	body, err := marshalNotificationPayload(payload)
	if err != nil {
		return RenderedNotification{}, err
	}
	if len(body) > genericNotificationMaxBodyBytes {
		return RenderedNotification{}, fmt.Errorf("render generic notification: payload exceeds %d bytes", genericNotificationMaxBodyBytes)
	}
	return RenderedNotification{
		Body:    body,
		Headers: http.Header{"Content-Type": []string{"application/json"}},
	}, nil
}

type genericNotificationPayload struct {
	SchemaVersion   int                        `json:"schema_version"`
	Event           NotificationEvent          `json:"event"`
	Request         genericNotificationRequest `json:"request"`
	CurrentNode     *NotificationNode          `json:"current_node"`
	ApprovalHistory []NotificationDecision     `json:"approval_history"`
	ActionURL       string                     `json:"action_url"`
	OccurredAt      string                     `json:"occurred_at"`
}

type genericNotificationRequest struct {
	ID                int                          `json:"id"`
	Status            string                       `json:"status"`
	Requester         genericNotificationRequester `json:"requester"`
	SubscriptionGroup genericNotificationGroup     `json:"subscription_group"`
	Reason            string                       `json:"reason"`
}

type genericNotificationRequester struct {
	DisplayName string   `json:"display_name"`
	Email       string   `json:"email"`
	Departments []string `json:"departments"`
}

type genericNotificationGroup struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Platform string `json:"platform"`
}

func boundedGenericNode(node *NotificationNode) *NotificationNode {
	if node == nil {
		return nil
	}
	return &NotificationNode{
		ID:            node.ID,
		Position:      node.Position,
		Total:         node.Total,
		Label:         truncateUTF8(node.Label, genericNotificationNodeLabelMaxBytes),
		Approvers:     boundedGenericPeople(node.Approvers, genericNotificationNodeApproverLimit),
		AdminFallback: node.AdminFallback,
	}
}

func boundedGenericPeople(people []NotificationPerson, limit int) []NotificationPerson {
	if people == nil {
		return nil
	}
	if len(people) < limit {
		limit = len(people)
	}
	bounded := make([]NotificationPerson, 0, limit)
	for _, person := range people[:limit] {
		bounded = append(bounded, NotificationPerson{
			UserID:      person.UserID,
			DisplayName: truncateUTF8(person.DisplayName, genericNotificationDisplayNameMaxBytes),
			Email:       truncateUTF8(person.Email, genericNotificationEmailMaxBytes),
		})
	}
	return bounded
}

func boundedGenericStrings(values []string, limit, maxBytes int) []string {
	if values == nil {
		return nil
	}
	if len(values) < limit {
		limit = len(values)
	}
	bounded := make([]string, 0, limit)
	for _, value := range values[:limit] {
		bounded = append(bounded, truncateUTF8(value, maxBytes))
	}
	return bounded
}

func boundedGenericApprovalHistory(history []NotificationDecision) []NotificationDecision {
	if history == nil {
		return nil
	}
	start := 0
	if len(history) > genericNotificationApprovalHistoryLimit {
		start = len(history) - genericNotificationApprovalHistoryLimit
	}
	bounded := make([]NotificationDecision, 0, len(history)-start)
	for _, decision := range history[start:] {
		bounded = append(bounded, NotificationDecision{
			ActorDisplayName: truncateUTF8(decision.ActorDisplayName, genericNotificationDisplayNameMaxBytes),
			Decision:         truncateUTF8(decision.Decision, genericNotificationDecisionMaxBytes),
			Comment:          truncateUTF8(decision.Comment, genericNotificationDecisionCommentMaxBytes),
			CreatedAt:        decision.CreatedAt,
		})
	}
	return bounded
}

func (genericWebhookAdapter) ValidateResponse(statusCode int, _ []byte) error {
	if statusCode < http.StatusOK || statusCode > 299 {
		return fmt.Errorf("webhook returned %d", statusCode)
	}
	return nil
}
