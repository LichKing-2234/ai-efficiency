package owneraccount

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/ai-efficiency/backend/internal/relay"
)

// Stable reason codes exposed through details.error_code. They are a closed set:
// the browser renders them, so they must stay descriptive without carrying any
// upstream error text.
const (
	CodeAccountNotFound       = "account-not-found"
	CodeOperationNotFound     = "operation-not-found"
	CodeOwnerInactive         = "owner-inactive"
	CodeCapabilityUnsupported = "capability-unsupported"
	CodeComplianceAckRequired = "compliance-ack-required"
	CodeUpstreamUnavailable   = "upstream-unavailable"
	CodeActionsDisabled       = "actions-disabled"
	CodeResetFreshness        = "reset-freshness-required"
	CodeInviteFreshness       = "invite-freshness-required"
	CodeCreditsExpired        = "credits-expired"
	CodeMutationInFlight      = "mutation-in-flight"
	CodeRefreshCooldown       = "refresh-cooldown"
	CodeProgramChanged        = "program-changed"
	CodeReferralUnavailable   = "referral-unavailable"
	CodeInvalidEmail          = "invalid-email"
	CodeConsentRequired       = "consent-required"
	CodeBusy                  = "busy"
	CodeMutationRateLimited   = "mutation-rate-limited"
	CodeInvalidPagination     = "invalid-pagination"
	CodeInvalidFilter         = "invalid-filter"
	CodeUsageUnavailable      = "usage-unavailable"
	CodeUsageBatchTooLarge    = "usage-batch-too-large"
	CodeInvalidRequest        = "invalid-request"
	CodeInternalError         = "internal-error"
)

// Error is a classified service failure. The HTTP layer translates it into the
// platform envelope and never forwards upstream text.
type Error struct {
	Status  int
	Code    string
	Message string
	Details map[string]any
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("owner account: %s: %s", e.Code, e.Message)
}

func newError(status int, code, message string) *Error {
	return &Error{Status: status, Code: code, Message: message}
}

func (e *Error) withDetail(key string, value any) *Error {
	if e.Details == nil {
		e.Details = make(map[string]any, 2)
	}
	e.Details[key] = value
	return e
}

func errAccountNotFound() *Error {
	return newError(http.StatusNotFound, CodeAccountNotFound, "account not found")
}

func errOperationNotFound() *Error {
	return newError(http.StatusNotFound, CodeOperationNotFound, "operation not found")
}

func errOwnerInactive() *Error {
	return newError(http.StatusForbidden, CodeOwnerInactive, "account owner is not active")
}

func errActionsDisabled() *Error {
	return newError(http.StatusForbidden, CodeActionsDisabled, "owner account actions are disabled")
}

func errConsentRequired() *Error {
	return newError(http.StatusBadRequest, CodeConsentRequired, "explicit confirmation is required")
}

func errInvalidEmail() *Error {
	return newError(http.StatusBadRequest, CodeInvalidEmail, "invitation email is invalid")
}

func errResetFreshness() *Error {
	return newError(http.StatusConflict, CodeResetFreshness, "a fresh quota observation is required")
}

func errInviteFreshness() *Error {
	return newError(http.StatusConflict, CodeInviteFreshness, "a fresh referral observation is required")
}

func errCreditsExpired() *Error {
	return newError(http.StatusConflict, CodeCreditsExpired, "no unexpired reset credit is available")
}

func errMutationInFlight(operationID int) *Error {
	return newError(http.StatusConflict, CodeMutationInFlight, "another account operation is in flight").
		withDetail("operation_id", operationID)
}

func errBusy() *Error {
	return newError(http.StatusConflict, CodeBusy, "account operation is busy")
}

func errProgramChanged() *Error {
	return newError(http.StatusConflict, CodeProgramChanged, "the referral program changed; refresh and try again")
}

func errReferralUnavailable() *Error {
	return newError(http.StatusConflict, CodeReferralUnavailable, "referral invitations are unavailable")
}

func errInvalidPagination() *Error {
	return newError(http.StatusBadRequest, CodeInvalidPagination, "page and page_size are invalid")
}

func errInvalidFilter(name string) *Error {
	return newError(http.StatusBadRequest, CodeInvalidFilter, "invalid filter: "+name)
}

func errUsageBatchTooLarge() *Error {
	return newError(http.StatusBadRequest, CodeUsageBatchTooLarge, "too many accounts were requested")
}

func errMutationRateLimited(retryAfterSeconds int) *Error {
	return newError(http.StatusTooManyRequests, CodeMutationRateLimited, "account mutation rate limit reached").
		withDetail("retry_after_seconds", retryAfterSeconds)
}

func errRefreshCooldown(retryAfterSeconds int, cooldownUntil string) *Error {
	e := newError(http.StatusTooManyRequests, CodeRefreshCooldown, "account refresh is cooling down").
		withDetail("retry_after_seconds", retryAfterSeconds)
	if cooldownUntil != "" {
		e.withDetail("cooldown_until", cooldownUntil)
	}
	return e
}

// upstreamClassification is the one place a relay capability sentinel is mapped
// onto an HTTP reason code, a sanitized stored operation code and a log-safe
// class. Callers never see upstream error text.
type upstreamClassification struct {
	Status        int
	Code          string
	OperationCode string
	LogClass      string
	Message       string
}

func classifyRelayError(err error) upstreamClassification {
	switch {
	case errors.Is(err, relay.ErrCapabilityUnsupported):
		return upstreamClassification{
			Status:        http.StatusNotImplemented,
			Code:          CodeCapabilityUnsupported,
			OperationCode: CodeCapabilityUnsupported,
			LogClass:      "capability_unsupported",
			Message:       "the relay provider does not support this capability",
		}
	case errors.Is(err, relay.ErrComplianceAcknowledgementRequired):
		return upstreamClassification{
			Status:        http.StatusLocked,
			Code:          CodeComplianceAckRequired,
			OperationCode: CodeComplianceAckRequired,
			LogClass:      "compliance_ack_required",
			Message:       "the relay provider requires administrator compliance acknowledgement",
		}
	case errors.Is(err, relay.ErrUpstreamOutcomeUnknown):
		return upstreamClassification{
			Status:        http.StatusBadGateway,
			Code:          CodeUpstreamUnavailable,
			OperationCode: "upstream-outcome-unknown",
			LogClass:      "upstream_outcome_unknown",
			Message:       "the relay provider is unavailable",
		}
	default:
		return upstreamClassification{
			Status:        http.StatusBadGateway,
			Code:          CodeUpstreamUnavailable,
			OperationCode: CodeUpstreamUnavailable,
			LogClass:      "upstream_or_internal",
			Message:       "the relay provider is unavailable",
		}
	}
}

// classifyUpstream maps a relay failure to the stable reason code the browser
// understands.
func classifyUpstream(err error) *Error {
	classification := classifyRelayError(err)
	return newError(classification.Status, classification.Code, classification.Message)
}

// classifyUpstreamCode is the sanitized operation code stored for an upstream
// failure. It deliberately drops all upstream error text.
func classifyUpstreamCode(err error) string {
	return classifyRelayError(err).OperationCode
}

// logClassify keeps logs free of upstream or database text.
func logClassify(err error) string {
	if err == nil {
		return ""
	}
	return classifyRelayError(err).LogClass
}
