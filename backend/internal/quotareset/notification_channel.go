package quotareset

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type NotificationEvent string

const (
	NotificationNodeActivated  NotificationEvent = "quota_reset_approval_node_activated"
	NotificationRejected       NotificationEvent = "quota_reset_request_rejected"
	NotificationCancelled      NotificationEvent = "quota_reset_request_cancelled"
	NotificationResetSucceeded NotificationEvent = "quota_reset_request_reset_succeeded"
	NotificationResetFailed    NotificationEvent = "quota_reset_request_reset_failed"
	NotificationTest           NotificationEvent = "quota_reset_notification_test"
)

type NotificationPerson struct {
	UserID          int               `json:"user_id"`
	DisplayName     string            `json:"display_name"`
	Email           string            `json:"email,omitempty"`
	NotificationIDs map[string]string `json:"-"`
}

type NotificationDecision struct {
	ActorDisplayName string    `json:"actor_display_name"`
	Decision         string    `json:"decision"`
	Comment          string    `json:"comment"`
	CreatedAt        time.Time `json:"created_at"`
}

type NotificationNode struct {
	ID            int                  `json:"id"`
	Position      int                  `json:"position"`
	Total         int                  `json:"total"`
	Label         string               `json:"label"`
	Approvers     []NotificationPerson `json:"approvers"`
	AdminFallback bool                 `json:"admin_fallback"`
}

type NotificationContext struct {
	Event           NotificationEvent
	OccurredAt      time.Time
	RequestID       int
	Status          string
	Requester       NotificationPerson
	Recipients      []NotificationPerson
	DepartmentPaths []string
	GroupID         string
	GroupName       string
	GroupPlatform   string
	Reason          string
	CurrentNode     *NotificationNode
	ApprovalHistory []NotificationDecision
	ActionURL       string
}

type RenderedNotification struct {
	Body                    []byte
	Headers                 http.Header
	RecipientCount          int
	MissingRecipientUserIDs []int
}

type notificationAdapter interface {
	Render(NotificationContext) (RenderedNotification, error)
	ValidateResponse(statusCode int, body []byte) error
}

func notificationAdapterFor(channelType string) (notificationAdapter, error) {
	switch channelType {
	case "generic_webhook":
		return genericWebhookAdapter{}, nil
	case "wecom_group_robot":
		return weComGroupRobotAdapter{maxBytes: 4096}, nil
	default:
		return nil, fmt.Errorf("%w: unsupported channel_type %q", ErrInvalidNotification, channelType)
	}
}

func marshalNotificationPayload(value any) ([]byte, error) {
	body, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("marshal notification payload: %w", err)
	}
	return body, nil
}
