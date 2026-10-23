package owneraccount

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/ai-efficiency/backend/internal/relay"
	"go.uber.org/zap"
)

// Terminal operation states.
const (
	statusSucceeded = "succeeded"
	statusFailed    = "failed"
	statusUnknown   = "unknown"
)

// workerRequest is everything the detached worker needs. It is deliberately plain
// data: the worker must never touch a gin.Context.
type workerRequest struct {
	requestCtx  context.Context
	operationID int
	providerID  int
	accountID   int64
	actorUserID int
	action      string
	programID   string
	inviteEmail string
}

// startOperationWorker starts the detached worker for one accepted mutation. The
// request context is detached from cancellation so a client disconnect after the
// 202 cannot abort an irreversible upstream call.
func (s *Service) startOperationWorker(request workerRequest) {
	request.requestCtx = context.WithoutCancel(request.requestCtx)
	go s.runOperationWorker(request)
}

// runOperationWorker performs exactly one upstream call and writes one terminal
// state. Terminal writes retry a bounded number of times; the upstream call never
// repeats.
func (s *Service) runOperationWorker(request workerRequest) {
	startedAt := time.Now()
	ctx, cancel := context.WithTimeout(request.requestCtx, s.options.OperationTimeout)
	defer cancel()

	operationID := request.operationID
	upstreamCode := ""
	status := statusUnknown
	var result map[string]any
	var warningCode string

	defer func() {
		recovered := recover()
		if recovered != nil {
			status = statusUnknown
			upstreamCode = "worker-panic"
			result = nil
			warningCode = ""
			s.logger.Error("owner account operation worker panicked",
				zap.Int("operation_id", operationID),
				zap.String("action", request.action),
				zap.Int("provider_id", request.providerID),
				zap.Int64("account_id", request.accountID),
				zap.Int("actor_user_id", request.actorUserID),
				zap.String("status", status),
			)
		}
		if writeErr := s.finishOperation(operationID, status, upstreamCode, warningCode, result); writeErr != nil {
			s.logger.Error("owner account operation terminal write failed",
				zap.Int("operation_id", operationID),
				zap.String("action", request.action),
				zap.Int("provider_id", request.providerID),
				zap.Int64("account_id", request.accountID),
				zap.Int("actor_user_id", request.actorUserID),
				zap.String("status", status),
				zap.String("error_class", "terminal_write"),
			)
		}
		s.logger.Info("owner account operation finished",
			zap.Int("operation_id", operationID),
			zap.String("action", request.action),
			zap.Int("provider_id", request.providerID),
			zap.Int64("account_id", request.accountID),
			zap.Int("actor_user_id", request.actorUserID),
			zap.String("status", status),
			zap.String("upstream_code", upstreamCode),
			zap.Duration("duration", time.Since(startedAt)),
		)
	}()

	stopHeartbeat := s.startHeartbeat(ctx, operationID)
	defer stopHeartbeat()

	succeeded, outcomeStatus, outcomeCode, outcomeWarning, outcomeResult := s.performOperation(ctx, request)
	status = outcomeStatus
	upstreamCode = outcomeCode
	warningCode = outcomeWarning
	result = outcomeResult
	if succeeded {
		s.runtime.InvalidateOwnerAccounts(request.providerID, platformOpenAI)
	}
}

// startHeartbeat renews the running row every heartbeat interval until the worker
// finishes. A heartbeat that cannot be written is not fatal: the row becomes
// unknown only after the stale window, which is the intended fail-safe.
func (s *Service) startHeartbeat(ctx context.Context, operationID int) func() {
	heartbeatCtx, cancel := context.WithCancel(ctx)
	go func() {
		ticker := time.NewTicker(s.options.HeartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-heartbeatCtx.Done():
				return
			case <-ticker.C:
				touchCtx, touchCancel := context.WithTimeout(heartbeatCtx, 3*time.Second)
				_ = s.touchHeartbeat(touchCtx, operationID)
				touchCancel()
			}
		}
	}()
	return cancel
}

// performOperation resolves the long-running provider and performs exactly one
// upstream call. It returns the terminal state classification.
func (s *Service) performOperation(ctx context.Context, request workerRequest) (bool, string, string, string, map[string]any) {
	row, err := s.client.RelayProvider.Get(ctx, request.providerID)
	if err != nil {
		return false, statusFailed, CodeUpstreamUnavailable, "", nil
	}
	provider, err := s.runtime.ResolveLongRunningEntity(row)
	if err != nil {
		return false, statusFailed, CodeUpstreamUnavailable, "", nil
	}
	switch request.action {
	case "reset_credit":
		manager, ok := provider.(relay.OpenAIQuotaManager)
		if !ok {
			return false, statusFailed, CodeCapabilityUnsupported, "", nil
		}
		reset, err := manager.ConsumeOpenAIResetCredit(ctx, request.accountID)
		if err != nil {
			return false, mutationFailureStatus(err), classifyUpstreamCode(err), "", nil
		}
		windowsReset := 0
		warningCode := ""
		upstreamCode := ""
		if reset != nil {
			windowsReset = reset.WindowsReset
			warningCode = sanitizeCode(reset.WarningCode)
			upstreamCode = sanitizeCode(reset.Code)
		}
		return true, statusSucceeded, upstreamCode, warningCode, map[string]any{"windows_reset": windowsReset}
	case "send_invitation":
		manager, ok := provider.(relay.OpenAIReferralManager)
		if !ok {
			return false, statusFailed, CodeCapabilityUnsupported, "", nil
		}
		invite, err := manager.SendOpenAIReferralInvite(ctx, request.accountID, relay.OpenAIReferralInviteRequest{
			Email:     request.inviteEmail,
			ProgramID: request.programID,
			Confirmed: true,
		})
		if err != nil {
			return false, mutationFailureStatus(err), classifyUpstreamCode(err), "", nil
		}
		if invite == nil {
			return false, statusUnknown, "upstream-outcome-unknown", "", nil
		}
		return true, statusSucceeded, "", "", map[string]any{"sent": true}
	default:
		return false, statusFailed, CodeUpstreamUnavailable, "", nil
	}
}

// mutationFailureStatus classifies an upstream mutation failure. A compliance lock
// or a missing capability is a definitive failure; an ambiguous outcome, a
// timeout or a cancelled detached context is unknown and must block a repeat.
func mutationFailureStatus(err error) string {
	switch {
	case errors.Is(err, relay.ErrUpstreamOutcomeUnknown):
		return statusUnknown
	case errors.Is(err, relay.ErrCapabilityUnsupported), errors.Is(err, relay.ErrComplianceAcknowledgementRequired):
		return statusFailed
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return statusUnknown
	default:
		return statusFailed
	}
}

// sanitizeCode keeps an upstream code only when it is a short stable identifier.
// Anything else is dropped rather than stored or rendered.
func sanitizeCode(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 64 {
		return ""
	}
	for _, char := range value {
		switch {
		case char >= 'a' && char <= 'z':
		case char >= 'A' && char <= 'Z':
		case char >= '0' && char <= '9':
		case char == '_' || char == '-' || char == '.':
		default:
			return ""
		}
	}
	return value
}
