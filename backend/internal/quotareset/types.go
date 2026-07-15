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
	DecisionReason string
	Admin          bool
}

type ListParams struct {
	Page     int
	PageSize int
	Status   string
}

type NotificationSettings struct {
	Enabled      bool   `json:"enabled"`
	Channel      string `json:"channel"`
	URL          string `json:"url"`
	AuthType     string `json:"auth_type"`
	CredentialID *int   `json:"credential_id,omitempty"`
	UpdatedAt    string `json:"updated_at,omitempty"`
}

type ApproverConfigListResponse struct {
	Items []ApproverConfig `json:"items"`
}

type ApproverCandidateListResponse struct {
	Items                    []ApproverCandidate               `json:"items"`
	UnmatchedRepresentatives []UnmatchedApproverRepresentative `json:"unmatched_representatives,omitempty"`
}

type ApproverCandidate struct {
	UserID                    int    `json:"user_id"`
	Username                  string `json:"username"`
	Email                     string `json:"email"`
	DisplayName               string `json:"display_name"`
	DirectoryMemberExternalID string `json:"directory_member_external_id"`
	Representative            bool   `json:"representative"`
	HasWeComUserID            bool   `json:"has_wecom_userid"`
}

type UnmatchedApproverRepresentative struct {
	DirectoryMemberExternalID string `json:"directory_member_external_id"`
	DisplayName               string `json:"display_name,omitempty"`
	Email                     string `json:"email,omitempty"`
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

type ChainDepartmentInput struct {
	DirectorySourceID     int    `json:"directory_source_id"`
	DepartmentExternalID  string `json:"department_external_id"`
	DepartmentDisplayPath string `json:"department_display_path"`
}

type ApprovalChainInput struct {
	ProviderID  int                    `json:"provider_id"`
	GroupID     string                 `json:"group_id"`
	GroupName   string                 `json:"group_name"`
	Enabled     bool                   `json:"enabled"`
	Departments []ChainDepartmentInput `json:"departments"`
}

type ApprovalChain struct {
	ID          int                    `json:"id"`
	ProviderID  int                    `json:"provider_id"`
	GroupID     string                 `json:"group_id"`
	GroupName   string                 `json:"group_name"`
	Enabled     bool                   `json:"enabled"`
	Departments []ChainDepartmentInput `json:"departments"`
	UpdatedAt   time.Time              `json:"updated_at"`
}

type ApprovalChainGroupOption struct {
	ProviderID   int    `json:"provider_id"`
	ProviderName string `json:"provider_name"`
	GroupID      string `json:"group_id"`
	GroupName    string `json:"group_name"`
	Platform     string `json:"platform"`
}

type ApprovalChainListResponse struct {
	Items  []ApprovalChain            `json:"items"`
	Groups []ApprovalChainGroupOption `json:"groups"`
}

type UpdateNotificationSettingsInput struct {
	ActorUserID  int
	Enabled      bool
	Channel      string
	URL          string
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
	ID                      int                      `json:"id"`
	RequesterUserID         int                      `json:"requester_user_id"`
	RequesterDisplayName    string                   `json:"requester_display_name"`
	RequesterEmail          string                   `json:"requester_email"`
	ProviderID              int                      `json:"provider_id"`
	GroupID                 string                   `json:"group_id"`
	GroupName               string                   `json:"group_name"`
	GroupPlatform           string                   `json:"group_platform"`
	Reason                  string                   `json:"reason"`
	Status                  string                   `json:"status"`
	WorkflowVersion         int                      `json:"workflow_version"`
	CurrentStep             int                      `json:"current_step,omitempty"`
	WorkflowSteps           []WorkflowStep           `json:"workflow_steps,omitempty"`
	ResolvedApproverUserIDs []int                    `json:"resolved_approver_user_ids"`
	MatchedDepartmentPaths  []DepartmentPathEvidence `json:"matched_department_paths"`
	ApprovedByUserID        *int                     `json:"approved_by_user_id,omitempty"`
	RejectedByUserID        *int                     `json:"rejected_by_user_id,omitempty"`
	DecisionReason          string                   `json:"decision_reason,omitempty"`
	ResetError              string                   `json:"reset_error,omitempty"`
	CreatedAt               time.Time                `json:"created_at"`
	UpdatedAt               time.Time                `json:"updated_at"`
	Events                  []RequestEvent           `json:"events,omitempty"`
}
