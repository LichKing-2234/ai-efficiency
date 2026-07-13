package quotareset

import (
	"fmt"
	"net/http"
	"time"
)

type genericWebhookAdapter struct{}

func (genericWebhookAdapter) Render(ctx NotificationContext) (RenderedNotification, error) {
	payload := map[string]any{
		"schema_version": 2,
		"event":          ctx.Event,
		"request": map[string]any{
			"id":     ctx.RequestID,
			"status": ctx.Status,
			"requester": map[string]any{
				"display_name": ctx.Requester.DisplayName,
				"email":        ctx.Requester.Email,
				"departments":  ctx.DepartmentPaths,
			},
			"subscription_group": map[string]any{
				"id":       ctx.GroupID,
				"name":     ctx.GroupName,
				"platform": ctx.GroupPlatform,
			},
			"reason": ctx.Reason,
		},
		"current_node":     ctx.CurrentNode,
		"approval_history": ctx.ApprovalHistory,
		"action_url":       ctx.ActionURL,
		"occurred_at":      ctx.OccurredAt.UTC().Format(time.RFC3339),
	}
	body, err := marshalNotificationPayload(payload)
	if err != nil {
		return RenderedNotification{}, err
	}
	return RenderedNotification{
		Body:    body,
		Headers: http.Header{"Content-Type": []string{"application/json"}},
	}, nil
}

func (genericWebhookAdapter) ValidateResponse(statusCode int, _ []byte) error {
	if statusCode < http.StatusOK || statusCode > 299 {
		return fmt.Errorf("webhook returned %d", statusCode)
	}
	return nil
}
