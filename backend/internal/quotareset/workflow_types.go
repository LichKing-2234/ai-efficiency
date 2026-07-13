package quotareset

import "time"

const WorkflowVersionV2 = 2

type RequesterIdentitySnapshot struct {
	DisplayName     string
	Email           string
	DepartmentPaths []string
	NotificationIDs map[string]string
}

type DepartmentSnapshot struct {
	ExternalID  string `json:"external_id"`
	DisplayPath string `json:"display_path"`
	Resolution  string `json:"resolution"`
}

type ResolvedNodeApprover struct {
	UserID                      int
	DisplayName                 string
	Email                       string
	Source                      string
	SourceDepartmentExternalIDs []string
	NotificationIDs             map[string]string
}

type ResolvedWorkflowNode struct {
	Position              int
	NodeType              string
	Label                 string
	Departments           []DepartmentSnapshot
	Approvers             []ResolvedNodeApprover
	InitialStatus         string
	AdminFallbackRequired bool
}

type WorkflowSnapshot struct {
	Requester RequesterIdentitySnapshot
	Nodes     []ResolvedWorkflowNode
}

type WorkflowNodeApproverSummary struct {
	UserID      int    `json:"user_id"`
	DisplayName string `json:"display_name"`
	Email       string `json:"email"`
	Source      string `json:"source"`
}

type WorkflowDecisionSummary struct {
	ID               int       `json:"id"`
	NodeID           int       `json:"node_id"`
	ActorUserID      int       `json:"actor_user_id"`
	ActorDisplayName string    `json:"actor_display_name"`
	Decision         string    `json:"decision"`
	Comment          string    `json:"comment"`
	AdminOverride    bool      `json:"admin_override"`
	CreatedAt        time.Time `json:"created_at"`
}

type WorkflowNodeSummary struct {
	ID                    int                           `json:"id"`
	Position              int                           `json:"position"`
	NodeType              string                        `json:"node_type"`
	Label                 string                        `json:"label"`
	Departments           []DepartmentSnapshot          `json:"departments"`
	Status                string                        `json:"status"`
	AdminFallbackRequired bool                          `json:"admin_fallback_required"`
	Approvers             []WorkflowNodeApproverSummary `json:"approvers"`
	SatisfiedByDecisionID *int                          `json:"satisfied_by_decision_id,omitempty"`
}

type WorkflowSummary struct {
	Version     int                       `json:"version"`
	CurrentNode *WorkflowNodeSummary      `json:"current_node,omitempty"`
	Nodes       []WorkflowNodeSummary     `json:"nodes"`
	Decisions   []WorkflowDecisionSummary `json:"decisions"`
	CanApprove  bool                      `json:"can_approve"`
	CanReject   bool                      `json:"can_reject"`
	CanCancel   bool                      `json:"can_cancel"`
	CanRetry    bool                      `json:"can_retry"`
}
