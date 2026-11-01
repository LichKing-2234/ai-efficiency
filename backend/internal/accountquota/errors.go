package accountquota

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/ai-efficiency/backend/internal/relay"
)

// Stable API error codes exposed through details.error_code. They are a closed
// set: the browser renders them, so they carry no upstream error text.
const (
	CodeInvalidRequest        = "invalid-request"
	CodeProviderNotFound      = "provider-not-found"
	CodeCapabilityUnsupported = "capability-unsupported"
	CodeComplianceAckRequired = "compliance-ack-required"
	CodeSweepInFlight         = "sweep-in-flight"
	CodeSweepTooSoon          = "sweep-too-soon"
	CodeSweepNotFound         = "sweep-not-found"
	CodeNoTargets             = "no-targets"
	CodeTargetNotEligible     = "target-not-eligible"
	CodeUpstreamUnavailable   = "upstream-unavailable"
	CodeInternalError         = "internal-error"
)

// Stable per-account reason codes stored in a sweep result. They are sanitized:
// only the code and a fixed message reach the page, never upstream error text.
const (
	ReasonReauthRequired         = "reauth-required"
	ReasonRateLimited            = "rate-limited"
	ReasonUpstreamUnavailable    = "upstream-unavailable"
	ReasonTimeout                = "timeout"
	ReasonCapabilityUnsupported  = "capability-unsupported"
	ReasonComplianceAckRequired  = "compliance-ack-required"
	ReasonUpstreamOutcomeUnknown = "upstream-outcome-unknown"
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
	return fmt.Sprintf("account quota: %s: %s", e.Code, e.Message)
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

func errInvalidRequest(message string) *Error {
	return newError(http.StatusBadRequest, CodeInvalidRequest, message)
}

func errProviderNotFound() *Error {
	return newError(http.StatusNotFound, CodeProviderNotFound, "relay provider not found")
}

func errSweepNotFound() *Error {
	return newError(http.StatusNotFound, CodeSweepNotFound, "account quota sweep not found")
}

func errSweepInFlight() *Error {
	return newError(http.StatusConflict, CodeSweepInFlight, "another account quota sweep is already running")
}

func errSweepTooSoon(retryAfterSeconds int) *Error {
	return newError(http.StatusConflict, CodeSweepTooSoon, "account quota sweeps are rate limited").
		withDetail("retry_after_seconds", retryAfterSeconds)
}

func errNoTargets(message string) *Error {
	return newError(http.StatusBadRequest, CodeNoTargets, message)
}

func errTargetNotEligible(message string) *Error {
	return newError(http.StatusBadRequest, CodeTargetNotEligible, message)
}

func errCapabilityUnsupported(message string) *Error {
	return newError(http.StatusNotImplemented, CodeCapabilityUnsupported, message)
}

func errComplianceAckRequired() *Error {
	return newError(http.StatusLocked, CodeComplianceAckRequired, "the relay provider requires administrator compliance acknowledgement")
}

func errUpstreamUnavailable() *Error {
	return newError(http.StatusBadGateway, CodeUpstreamUnavailable, "the relay provider is unavailable")
}

// errInternal is the only failure the API reports without detail: unexpected
// errors may embed upstream or database text.
func errInternal() *Error {
	return newError(http.StatusInternalServerError, CodeInternalError, "account quota request failed")
}

// classifyAccountFailure maps one account's relay failure to a stable reason code
// and a sanitized message. It never returns upstream error text.
func classifyAccountFailure(err error) (string, string) {
	switch {
	case errors.Is(err, relay.ErrCapabilityUnsupported):
		return ReasonCapabilityUnsupported, "the relay provider does not support this capability"
	case errors.Is(err, relay.ErrComplianceAcknowledgementRequired):
		return ReasonComplianceAckRequired, "the relay provider requires administrator compliance acknowledgement"
	case errors.Is(err, relay.ErrUpstreamOutcomeUnknown):
		return ReasonUpstreamOutcomeUnknown, "the relay provider did not report a definitive outcome"
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return ReasonTimeout, "the upstream quota refresh timed out"
	}
	if status, ok := relay.UpstreamHTTPStatus(err); ok {
		switch {
		case status == http.StatusUnauthorized || status == http.StatusForbidden:
			// The relay refreshes the account's OAuth token before calling, so a 401
			// means a freshly minted token was still rejected: the account needs
			// re-authorization. A wrong relay admin key also surfaces as 401, in
			// which case every account fails the same way.
			return ReasonReauthRequired, "the account authorization was rejected; re-authorize the account"
		case status == http.StatusTooManyRequests:
			return ReasonRateLimited, "the upstream rate limited the request"
		default:
			return ReasonUpstreamUnavailable, "the upstream quota refresh failed"
		}
	}
	return ReasonUpstreamUnavailable, "the upstream quota refresh failed"
}
