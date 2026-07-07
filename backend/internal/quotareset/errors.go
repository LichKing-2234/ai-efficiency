package quotareset

import "errors"

var (
	ErrNoRelayMapping        = errors.New("no_relay_mapping")
	ErrProviderUnsupported   = errors.New("provider_unsupported")
	ErrInactiveSubscription  = errors.New("inactive_subscription")
	ErrActiveRequestExists   = errors.New("active_request_exists")
	ErrNotApprover           = errors.New("not_approver")
	ErrSelfApprovalForbidden = errors.New("self_approval_forbidden")
	ErrInvalidStatus         = errors.New("invalid_status")
	ErrReasonRequired        = errors.New("reason_required")
	ErrDecisionRequired      = errors.New("decision_reason_required")
)
