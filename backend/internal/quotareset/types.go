package quotareset

import (
	"context"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/internal/relay"
)

type ApproverResolution struct {
	ApproverUserIDs []int                    `json:"approver_user_ids"`
	Paths           []DepartmentPathEvidence `json:"paths"`
}

type DepartmentPathEvidence struct {
	StartDepartmentExternalID   string               `json:"start_department_external_id"`
	Path                        []DepartmentPathNode `json:"path"`
	MatchedDepartmentExternalID string               `json:"matched_department_external_id,omitempty"`
	MatchedApproverUserIDs      []int                `json:"matched_approver_user_ids,omitempty"`
	Resolution                  string               `json:"resolution"`
}

type DepartmentPathNode struct {
	ExternalID  string `json:"external_id"`
	DisplayPath string `json:"display_path"`
}

type SubscriptionOption struct {
	GroupID         string   `json:"group_id"`
	GroupName       string   `json:"group_name"`
	Platform        string   `json:"platform"`
	DailyUsageUSD   float64  `json:"daily_usage_usd"`
	WeeklyUsageUSD  float64  `json:"weekly_usage_usd"`
	MonthlyUsageUSD float64  `json:"monthly_usage_usd"`
	DailyLimitUSD   *float64 `json:"daily_limit_usd,omitempty"`
	WeeklyLimitUSD  *float64 `json:"weekly_limit_usd,omitempty"`
	MonthlyLimitUSD *float64 `json:"monthly_limit_usd,omitempty"`
}

type OptionsResponse struct {
	ProviderID int                  `json:"provider_id"`
	Groups     []SubscriptionOption `json:"groups"`
}

type CreateRequestInput struct {
	RequesterUserID int
	GroupID         string
	Reason          string
}

type DecisionInput struct {
	ActorUserID    int
	RequestID      int
	RequestNodeID  int
	DecisionReason string
	Admin          bool
}

type ListParams struct {
	Page     int
	PageSize int
	Status   string
}

type NotificationSettings struct {
	Enabled         bool   `json:"enabled"`
	ChannelType     string `json:"channel_type"`
	TemplateVersion int    `json:"template_version"`
	URLConfigured   bool   `json:"url_configured"`
	URLPreview      string `json:"url_preview"`
	AuthType        string `json:"auth_type"`
	CredentialID    *int   `json:"credential_id,omitempty"`
	UpdatedAt       string `json:"updated_at,omitempty"`
}

type ApproverConfigListResponse struct {
	Items []ApproverConfig `json:"items"`
}

type ApproverCandidateParams struct {
	SourceID int
	Query    string
	Page     int
	PageSize int
}

type ApproverCandidate struct {
	UserID                    int      `json:"user_id"`
	Username                  string   `json:"username"`
	Email                     string   `json:"email"`
	DisplayName               string   `json:"display_name"`
	DirectoryMemberExternalID string   `json:"directory_member_external_id"`
	DepartmentPaths           []string `json:"department_paths"`
	WeComMentionAvailable     bool     `json:"wecom_mention_available"`
}

type ApproverCandidateListResponse struct {
	Items    []ApproverCandidate `json:"items"`
	Page     int                 `json:"page"`
	PageSize int                 `json:"page_size"`
	Total    int                 `json:"total"`
}

type ApproverConfig struct {
	ID                    int       `json:"id"`
	DirectorySourceID     int       `json:"directory_source_id"`
	DepartmentExternalID  string    `json:"department_external_id"`
	DepartmentDisplayPath string    `json:"department_display_path"`
	ApproverUserID        int       `json:"approver_user_id"`
	ApproverUsername      string    `json:"approver_username"`
	ApproverEmail         string    `json:"approver_email"`
	Enabled               bool      `json:"enabled"`
	CreatedAt             time.Time `json:"created_at"`
	UpdatedAt             time.Time `json:"updated_at"`
}

type SaveApproverConfigsInput struct {
	ActorUserID int
	Mode        string
	Items       []ApproverConfigInput
}

type ApproverConfigInput struct {
	DepartmentExternalID  string `json:"department_external_id"`
	DepartmentDisplayPath string `json:"department_display_path"`
	ApproverUserID        int    `json:"approver_user_id"`
	Enabled               bool   `json:"enabled"`
}

type ApprovalChainNodeInput struct {
	DirectorySourceID     int    `json:"directory_source_id"`
	DepartmentExternalID  string `json:"department_external_id"`
	DepartmentDisplayPath string `json:"department_display_path"`
}

type ApprovalChainInput struct {
	ProviderID int                      `json:"provider_id"`
	GroupID    string                   `json:"group_id"`
	GroupName  string                   `json:"group_name"`
	Enabled    bool                     `json:"enabled"`
	Nodes      []ApprovalChainNodeInput `json:"nodes"`
}

type SaveApprovalChainsInput struct {
	ActorUserID int
	Items       []ApprovalChainInput
}

type ApprovalChain struct {
	ID         int                      `json:"id"`
	ProviderID int                      `json:"provider_id"`
	GroupID    string                   `json:"group_id"`
	GroupName  string                   `json:"group_name"`
	Enabled    bool                     `json:"enabled"`
	Nodes      []ApprovalChainNodeInput `json:"nodes"`
}

type ApprovalChainListResponse struct {
	Items []ApprovalChain `json:"items"`
}

type ApprovalChainGroupOption struct {
	ProviderID int    `json:"provider_id"`
	GroupID    string `json:"group_id"`
	GroupName  string `json:"group_name"`
	Platform   string `json:"platform"`
}

type ApprovalChainDepartmentOption struct {
	DirectorySourceID     int    `json:"directory_source_id"`
	DepartmentExternalID  string `json:"department_external_id"`
	DepartmentDisplayPath string `json:"department_display_path"`
	ApproverCount         int    `json:"approver_count"`
}

type ApprovalChainOptionsResponse struct {
	Groups      []ApprovalChainGroupOption      `json:"groups"`
	Departments []ApprovalChainDepartmentOption `json:"departments"`
}

type UpdateNotificationSettingsInput struct {
	ActorUserID  int
	Enabled      bool
	ChannelType  string
	URL          *string
	AuthType     string
	CredentialID *int
}

type RequestEvent struct {
	ID           int            `json:"id"`
	RequestID    int            `json:"request_id"`
	ActorUserID  *int           `json:"actor_user_id,omitempty"`
	EventType    string         `json:"event_type"`
	Metadata     map[string]any `json:"metadata"`
	ErrorMessage string         `json:"error_message,omitempty"`
	CreatedAt    time.Time      `json:"created_at"`
}

type ProviderResolver interface {
	Resolve(ctx context.Context, providerID int) (relay.Provider, error)
}

type Notifier interface {
	NotifyRequestEvent(ctx context.Context, event string, req *ent.QuotaResetRequest) error
}

type RequestListResponse struct {
	Items    []RequestSummary `json:"items"`
	Page     int              `json:"page"`
	PageSize int              `json:"page_size"`
	Total    int              `json:"total"`
}

type RequestSummary struct {
	ID                       int                      `json:"id"`
	RequesterUserID          int                      `json:"requester_user_id"`
	RequesterDisplayName     string                   `json:"requester_display_name"`
	RequesterEmail           string                   `json:"requester_email"`
	RequesterDepartmentPaths []string                 `json:"requester_department_paths"`
	ProviderID               int                      `json:"provider_id"`
	GroupID                  string                   `json:"group_id"`
	GroupName                string                   `json:"group_name"`
	GroupPlatform            string                   `json:"group_platform"`
	Reason                   string                   `json:"reason"`
	Status                   string                   `json:"status"`
	ResolvedApproverUserIDs  []int                    `json:"resolved_approver_user_ids"`
	MatchedDepartmentPaths   []DepartmentPathEvidence `json:"matched_department_paths"`
	ApprovedByUserID         *int                     `json:"approved_by_user_id,omitempty"`
	RejectedByUserID         *int                     `json:"rejected_by_user_id,omitempty"`
	DecisionReason           string                   `json:"decision_reason,omitempty"`
	ResetError               string                   `json:"reset_error,omitempty"`
	CreatedAt                time.Time                `json:"created_at"`
	UpdatedAt                time.Time                `json:"updated_at"`
	Events                   []RequestEvent           `json:"events,omitempty"`
	Workflow                 *WorkflowSummary         `json:"workflow,omitempty"`
}
