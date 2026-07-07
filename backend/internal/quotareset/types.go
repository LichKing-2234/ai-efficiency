package quotareset

import "time"

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
	URL          string `json:"url"`
	AuthType     string `json:"auth_type"`
	CredentialID *int   `json:"credential_id,omitempty"`
	UpdatedAt    string `json:"updated_at,omitempty"`
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
