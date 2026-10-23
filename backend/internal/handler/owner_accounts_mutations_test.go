package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/ent/owneraccountoperation"
	"github.com/ai-efficiency/backend/internal/owneraccount"
)

func prepareOwnerQuotaObservation(t *testing.T, env *ownerAccountTestEnv) {
	t.Helper()
	w := env.request(http.MethodPost, "/api/v1/user/owner-accounts/11/quota/refresh", nil, env.aliceAuth)
	if w.Code != http.StatusOK {
		t.Fatalf("quota refresh status = %d, body: %s", w.Code, w.Body.String())
	}
}

func prepareOwnerReferralObservation(t *testing.T, env *ownerAccountTestEnv) {
	t.Helper()
	w := env.request(http.MethodPost, "/api/v1/user/owner-accounts/11/referral/refresh", nil, env.aliceAuth)
	if w.Code != http.StatusOK {
		t.Fatalf("referral refresh status = %d, body: %s", w.Code, w.Body.String())
	}
}

func resetCredit(t *testing.T, env *ownerAccountTestEnv, confirm bool) *httptest.ResponseRecorder {
	t.Helper()
	return env.request(http.MethodPost, "/api/v1/user/owner-accounts/11/reset-credit", map[string]any{
		"confirm": confirm, "reason": "operator requested",
	}, env.aliceAuth)
}

func inviteOwner(t *testing.T, env *ownerAccountTestEnv, programID string) *httptest.ResponseRecorder {
	t.Helper()
	return env.request(http.MethodPost, "/api/v1/user/owner-accounts/11/invitations", map[string]any{
		"email": "bob@example.org", "program_id": programID, "consent": true, "reason": "team growth",
	}, env.aliceAuth)
}

func countOwnerOperations(t *testing.T, env *ownerAccountTestEnv, action string) int {
	t.Helper()
	count, err := env.client.OwnerAccountOperation.Query().
		Where(owneraccountoperation.AccountID(11), owneraccountoperation.ActionEQ(owneraccountoperation.Action(action))).
		Count(context.Background())
	if err != nil {
		t.Fatalf("count owner operations: %v", err)
	}
	return count
}

// TestOwnerAccountResetGates walks every server-side reset gate independently:
// deployment flag, consent, observation freshness/age/ordering, live credits,
// rolling cap and single flight.
func TestOwnerAccountResetGates(t *testing.T) {
	t.Run("deployment flag off", func(t *testing.T) {
		env := setupOwnerAccountEnv(t, func(options *owneraccount.Options) { options.MutationsEnabled = false })
		env.upstream.setAccounts(ownerAccountWire(11, "Alice", "alice@example.com", "openai", "oauth"))
		w := resetCredit(t, env, true)
		if w.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403, body: %s", w.Code, w.Body.String())
		}
		if got := env.errorCode(t, w); got != "actions-disabled" {
			t.Fatalf("error_code = %q", got)
		}
		if count := countOwnerOperations(t, env, "reset_credit"); count != 0 {
			t.Fatalf("flag-off wrote %d operation rows", count)
		}
	})

	t.Run("missing consent", func(t *testing.T) {
		env := setupOwnerAccountEnv(t, nil)
		env.upstream.setAccounts(ownerAccountWire(11, "Alice", "alice@example.com", "openai", "oauth"))
		prepareOwnerQuotaObservation(t, env)
		w := resetCredit(t, env, false)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400, body: %s", w.Code, w.Body.String())
		}
		if got := env.errorCode(t, w); got != "consent-required" {
			t.Fatalf("error_code = %q", got)
		}
		if count := countOwnerOperations(t, env, "reset_credit"); count != 0 {
			t.Fatalf("consent rejection wrote %d rows", count)
		}
	})

	t.Run("no quota observation", func(t *testing.T) {
		env := setupOwnerAccountEnv(t, nil)
		env.upstream.setAccounts(ownerAccountWire(11, "Alice", "alice@example.com", "openai", "oauth"))
		w := resetCredit(t, env, true)
		if w.Code != http.StatusConflict {
			t.Fatalf("status = %d, want 409, body: %s", w.Code, w.Body.String())
		}
		if got := env.errorCode(t, w); got != "reset-freshness-required" {
			t.Fatalf("error_code = %q", got)
		}
	})

	t.Run("stale quota observation", func(t *testing.T) {
		env := setupOwnerAccountEnv(t, nil)
		env.upstream.setAccounts(ownerAccountWire(11, "Alice", "alice@example.com", "openai", "oauth"))
		env.seedOperation(t, env.alice.ID, ownerAccountSeed{
			AccountID: 11, Action: owneraccountoperation.ActionRefreshQuota,
			Status:    owneraccountoperation.StatusSucceeded,
			CreatedAt: time.Now().UTC().Add(-16 * time.Minute),
			Result:    ownerAccountQuotaObservation(1, "2999-01-01T00:00:00Z"),
		})
		w := resetCredit(t, env, true)
		if w.Code != http.StatusConflict {
			t.Fatalf("status = %d, want 409, body: %s", w.Code, w.Body.String())
		}
		if got := env.errorCode(t, w); got != "reset-freshness-required" {
			t.Fatalf("error_code = %q", got)
		}
	})

	t.Run("observation older than the last attempt", func(t *testing.T) {
		env := setupOwnerAccountEnv(t, nil)
		env.upstream.setAccounts(ownerAccountWire(11, "Alice", "alice@example.com", "openai", "oauth"))
		now := time.Now().UTC()
		env.seedOperation(t, env.alice.ID, ownerAccountSeed{
			AccountID: 11, Action: owneraccountoperation.ActionRefreshQuota,
			Status: owneraccountoperation.StatusSucceeded, CreatedAt: now.Add(-5 * time.Minute),
			Result: ownerAccountQuotaObservation(1, "2999-01-01T00:00:00Z"),
		})
		env.seedOperation(t, env.alice.ID, ownerAccountSeed{
			AccountID: 11, Action: owneraccountoperation.ActionResetCredit,
			Status: owneraccountoperation.StatusFailed, CreatedAt: now.Add(-time.Minute),
		})
		w := resetCredit(t, env, true)
		if w.Code != http.StatusConflict {
			t.Fatalf("status = %d, want 409, body: %s", w.Code, w.Body.String())
		}
		if got := env.errorCode(t, w); got != "reset-freshness-required" {
			t.Fatalf("error_code = %q", got)
		}
	})

	t.Run("expired reset credit", func(t *testing.T) {
		env := setupOwnerAccountEnv(t, nil)
		// The cached directory claims one unexpired credit; the persisted
		// observation says none, and the gate must trust only the local row.
		account := ownerAccountResetCredits(
			ownerAccountWire(11, "Alice", "alice@example.com", "openai", "oauth"), 1, "2999-01-01T00:00:00Z")
		env.upstream.setAccounts(account)
		env.upstream.setQuota(11, ownerAccountQuotaPayload(0, "2999-01-01T00:00:00Z"))
		prepareOwnerQuotaObservation(t, env)
		w := resetCredit(t, env, true)
		if w.Code != http.StatusConflict {
			t.Fatalf("status = %d, want 409, body: %s", w.Code, w.Body.String())
		}
		if got := env.errorCode(t, w); got != "credits-expired" {
			t.Fatalf("error_code = %q", got)
		}
	})

	t.Run("fresh observation wins over a stale cached credit snapshot", func(t *testing.T) {
		env := setupOwnerAccountEnv(t, nil)
		// The cached directory has an expired credit only; the fresh persisted
		// observation reports a live one and must admit the mutation.
		account := ownerAccountResetCredits(
			ownerAccountWire(11, "Alice", "alice@example.com", "openai", "oauth"), 1, "2000-01-01T00:00:00Z")
		env.upstream.setAccounts(account)
		prepareOwnerQuotaObservation(t, env)
		w := resetCredit(t, env, true)
		if w.Code != http.StatusAccepted {
			t.Fatalf("status = %d, want 202, body: %s", w.Code, w.Body.String())
		}
		env.waitForOperation(t, env.aliceAuth, int(env.data(t, w)["operation_id"].(float64)), "succeeded")
	})

	t.Run("rolling daily cap", func(t *testing.T) {
		env := setupOwnerAccountEnv(t, nil)
		env.upstream.setAccounts(ownerAccountWire(11, "Alice", "alice@example.com", "openai", "oauth"))
		for index := 0; index < 5; index++ {
			env.seedOperation(t, env.alice.ID, ownerAccountSeed{
				AccountID: 11, Action: owneraccountoperation.ActionResetCredit,
				Status:    owneraccountoperation.StatusFailed,
				CreatedAt: time.Now().UTC().Add(-time.Hour),
			})
		}
		prepareOwnerQuotaObservation(t, env)
		w := resetCredit(t, env, true)
		if w.Code != http.StatusTooManyRequests {
			t.Fatalf("status = %d, want 429, body: %s", w.Code, w.Body.String())
		}
		if got := env.errorCode(t, w); got != "mutation-rate-limited" {
			t.Fatalf("error_code = %q", got)
		}
		details := env.errorDetails(t, w)
		if retry, ok := details["retry_after_seconds"].(float64); !ok || retry < 1 {
			t.Fatalf("retry_after_seconds = %v", details["retry_after_seconds"])
		}
	})

	t.Run("single flight", func(t *testing.T) {
		env := setupOwnerAccountEnv(t, nil)
		env.upstream.setAccounts(ownerAccountWire(11, "Alice", "alice@example.com", "openai", "oauth"))
		running := env.seedOperation(t, env.alice.ID, ownerAccountSeed{
			AccountID: 11, Action: owneraccountoperation.ActionResetCredit,
			Status:    owneraccountoperation.StatusRunning,
			CreatedAt: time.Now().UTC().Add(-time.Minute),
			HeartbeatAt: func() *time.Time {
				value := time.Now().UTC()
				return &value
			}(),
		})
		prepareOwnerQuotaObservation(t, env)
		w := resetCredit(t, env, true)
		if w.Code != http.StatusConflict {
			t.Fatalf("status = %d, want 409, body: %s", w.Code, w.Body.String())
		}
		if got := env.errorCode(t, w); got != "mutation-in-flight" {
			t.Fatalf("error_code = %q", got)
		}
		details := env.errorDetails(t, w)
		if got := details["operation_id"]; got != float64(running.ID) {
			t.Fatalf("operation_id = %v, want %d", got, running.ID)
		}
	})

	t.Run("accepted and polled to success", func(t *testing.T) {
		env := setupOwnerAccountEnv(t, nil)
		env.upstream.setAccounts(ownerAccountWire(11, "Alice", "alice@example.com", "openai", "oauth"))
		prepareOwnerQuotaObservation(t, env)
		w := resetCredit(t, env, true)
		if w.Code != http.StatusAccepted {
			t.Fatalf("status = %d, want 202, body: %s", w.Code, w.Body.String())
		}
		accepted := env.data(t, w)
		operationID := int(accepted["operation_id"].(float64))
		if operationID <= 0 {
			t.Fatalf("operation_id = %v", accepted["operation_id"])
		}
		operation := env.waitForOperation(t, env.aliceAuth, operationID, "succeeded")
		if got := operation["action"]; got != "reset_credit" {
			t.Fatalf("action = %v", got)
		}
		result := operation["result"].(map[string]any)
		if got := result["windows_reset"]; got != float64(2) {
			t.Fatalf("windows_reset = %v, want 2", got)
		}
		if got := operation["warning_code"]; got != "account_state_refresh_failed" {
			t.Fatalf("warning_code = %v", got)
		}
		if calls := env.upstream.resetCallCount(); calls != 1 {
			t.Fatalf("upstream reset calls = %d, want exactly 1", calls)
		}
		row, err := env.client.OwnerAccountOperation.Query().
			Where(owneraccountoperation.ActionEQ(owneraccountoperation.ActionResetCredit)).
			Only(context.Background())
		if err != nil {
			t.Fatalf("load operation: %v", err)
		}
		if row.ActorUserID != env.alice.ID {
			t.Fatalf("actor = %d, want %d", row.ActorUserID, env.alice.ID)
		}
	})

	t.Run("upstream failure becomes failed", func(t *testing.T) {
		env := setupOwnerAccountEnv(t, nil)
		env.upstream.setAccounts(ownerAccountWire(11, "Alice", "alice@example.com", "openai", "oauth"))
		env.upstream.setResetStatus(11, http.StatusInternalServerError)
		prepareOwnerQuotaObservation(t, env)
		w := resetCredit(t, env, true)
		if w.Code != http.StatusAccepted {
			t.Fatalf("status = %d, want 202, body: %s", w.Code, w.Body.String())
		}
		operationID := int(env.data(t, w)["operation_id"].(float64))
		operation := env.waitForOperation(t, env.aliceAuth, operationID, "failed")
		if got := operation["error_code"]; got != "upstream-unavailable" {
			t.Fatalf("error_code = %v, want upstream-unavailable", got)
		}
		if calls := env.upstream.resetCallCount(); calls != 1 {
			t.Fatalf("upstream reset calls = %d, want no retry", calls)
		}
	})

	t.Run("unknown outcome blocks until a newer observation", func(t *testing.T) {
		env := setupOwnerAccountEnv(t, nil)
		env.upstream.setAccounts(ownerAccountWire(11, "Alice", "alice@example.com", "openai", "oauth"))
		env.seedOperation(t, env.alice.ID, ownerAccountSeed{
			AccountID: 11, Action: owneraccountoperation.ActionResetCredit,
			Status:    owneraccountoperation.StatusUnknown,
			CreatedAt: time.Now().UTC().Add(-time.Minute),
			Upstream:  "upstream-outcome-unknown",
		})
		w := resetCredit(t, env, true)
		if w.Code != http.StatusConflict || env.errorCode(t, w) != "reset-freshness-required" {
			t.Fatalf("blocked reset = %d/%s, want 409/reset-freshness-required", w.Code, env.errorCode(t, w))
		}
		blocked := env.data(t, env.request(http.MethodGet, "/api/v1/user/owner-accounts", nil, env.aliceAuth))
		blockedActions := blocked["items"].([]any)[0].(map[string]any)["blocked_actions"].([]any)
		if len(blockedActions) != 1 {
			t.Fatalf("blocked_actions = %v, want one entry", blockedActions)
		}
		entry := blockedActions[0].(map[string]any)
		if entry["action"] != "reset_credit" || entry["reason"] != "unknown" {
			t.Fatalf("blocked action = %v", entry)
		}

		prepareOwnerQuotaObservation(t, env)
		accepted := resetCredit(t, env, true)
		if accepted.Code != http.StatusAccepted {
			t.Fatalf("reset after refresh = %d, want 202, body: %s", accepted.Code, accepted.Body.String())
		}
		env.waitForOperation(t, env.aliceAuth, int(env.data(t, accepted)["operation_id"].(float64)), "succeeded")
	})
}

// TestOwnerAccountInvitationGates covers consent, email validation, freshness,
// program binding, remaining capacity, daily caps and the unknown-outcome block.
func TestOwnerAccountInvitationGates(t *testing.T) {
	t.Run("missing consent", func(t *testing.T) {
		env := setupOwnerAccountEnv(t, nil)
		env.upstream.setAccounts(ownerAccountWire(11, "Alice", "alice@example.com", "openai", "oauth"))
		w := env.request(http.MethodPost, "/api/v1/user/owner-accounts/11/invitations", map[string]any{
			"email": "bob@example.org", "program_id": "program-1", "consent": false,
		}, env.aliceAuth)
		if w.Code != http.StatusBadRequest || env.errorCode(t, w) != "consent-required" {
			t.Fatalf("status = %d/%s, want 400/consent-required", w.Code, env.errorCode(t, w))
		}
	})

	t.Run("invalid email", func(t *testing.T) {
		env := setupOwnerAccountEnv(t, nil)
		env.upstream.setAccounts(ownerAccountWire(11, "Alice", "alice@example.com", "openai", "oauth"))
		w := env.request(http.MethodPost, "/api/v1/user/owner-accounts/11/invitations", map[string]any{
			"email": "not-an-email", "program_id": "program-1", "consent": true,
		}, env.aliceAuth)
		if w.Code != http.StatusBadRequest || env.errorCode(t, w) != "invalid-email" {
			t.Fatalf("status = %d/%s, want 400/invalid-email", w.Code, env.errorCode(t, w))
		}
	})

	t.Run("no referral observation", func(t *testing.T) {
		env := setupOwnerAccountEnv(t, nil)
		env.upstream.setAccounts(ownerAccountWire(11, "Alice", "alice@example.com", "openai", "oauth"))
		w := inviteOwner(t, env, "program-1")
		if w.Code != http.StatusConflict || env.errorCode(t, w) != "invite-freshness-required" {
			t.Fatalf("status = %d/%s, want 409/invite-freshness-required", w.Code, env.errorCode(t, w))
		}
	})

	t.Run("program changed", func(t *testing.T) {
		env := setupOwnerAccountEnv(t, nil)
		env.upstream.setAccounts(ownerAccountWire(11, "Alice", "alice@example.com", "openai", "oauth"))
		prepareOwnerReferralObservation(t, env)
		w := inviteOwner(t, env, "program-2")
		if w.Code != http.StatusConflict || env.errorCode(t, w) != "program-changed" {
			t.Fatalf("status = %d/%s, want 409/program-changed", w.Code, env.errorCode(t, w))
		}
	})

	t.Run("no remaining capacity", func(t *testing.T) {
		env := setupOwnerAccountEnv(t, nil)
		// The cached directory still offers three invitations; the persisted
		// observation reports none, and the gate must trust only the local row.
		env.upstream.setAccounts(ownerAccountReferral(ownerAccountWire(11, "Alice", "alice@example.com", "openai", "oauth"), "program-1", 3))
		env.upstream.setReferral(11, ownerAccountReferralSnapshot("program-1", 0, true))
		prepareOwnerReferralObservation(t, env)
		w := inviteOwner(t, env, "program-1")
		if w.Code != http.StatusConflict || env.errorCode(t, w) != "referral-unavailable" {
			t.Fatalf("status = %d/%s, want 409/referral-unavailable", w.Code, env.errorCode(t, w))
		}
	})

	t.Run("fresh observation wins over a stale cached eligibility snapshot", func(t *testing.T) {
		env := setupOwnerAccountEnv(t, nil)
		// The cached directory reports no capacity; the fresh persisted observation
		// reports three invitations and must admit the mutation.
		env.upstream.setAccounts(ownerAccountReferral(ownerAccountWire(11, "Alice", "alice@example.com", "openai", "oauth"), "program-1", 0))
		prepareOwnerReferralObservation(t, env)
		w := inviteOwner(t, env, "program-1")
		if w.Code != http.StatusAccepted {
			t.Fatalf("status = %d, want 202, body: %s", w.Code, w.Body.String())
		}
		env.waitForOperation(t, env.aliceAuth, int(env.data(t, w)["operation_id"].(float64)), "succeeded")
	})

	t.Run("rolling daily cap", func(t *testing.T) {
		env := setupOwnerAccountEnv(t, nil)
		env.upstream.setAccounts(ownerAccountWire(11, "Alice", "alice@example.com", "openai", "oauth"))
		for index := 0; index < 10; index++ {
			env.seedOperation(t, env.alice.ID, ownerAccountSeed{
				AccountID: 11, Action: owneraccountoperation.ActionSendInvitation,
				Status:      owneraccountoperation.StatusSucceeded,
				CreatedAt:   time.Now().UTC().Add(-time.Hour),
				InviteEmail: "alice@example.com",
			})
		}
		prepareOwnerReferralObservation(t, env)
		w := inviteOwner(t, env, "program-1")
		if w.Code != http.StatusTooManyRequests || env.errorCode(t, w) != "mutation-rate-limited" {
			t.Fatalf("status = %d/%s, want 429/mutation-rate-limited", w.Code, env.errorCode(t, w))
		}
	})

	t.Run("accepted and polled to success", func(t *testing.T) {
		env := setupOwnerAccountEnv(t, nil)
		env.upstream.setAccounts(ownerAccountWire(11, "Alice", "alice@example.com", "openai", "oauth"))
		prepareOwnerReferralObservation(t, env)
		w := inviteOwner(t, env, "program-1")
		if w.Code != http.StatusAccepted {
			t.Fatalf("status = %d, want 202, body: %s", w.Code, w.Body.String())
		}
		operationID := int(env.data(t, w)["operation_id"].(float64))
		operation := env.waitForOperation(t, env.aliceAuth, operationID, "succeeded")
		result := operation["result"].(map[string]any)
		if got := result["sent"]; got != true {
			t.Fatalf("result.sent = %v, want true", got)
		}
		if email := queryInvite(t, env, operationID); email != "bob@example.org" {
			t.Fatalf("stored invite_email = %q", email)
		}
	})

	t.Run("ambiguous upstream outcome blocks until referral refresh", func(t *testing.T) {
		env := setupOwnerAccountEnv(t, nil)
		env.upstream.setAccounts(ownerAccountWire(11, "Alice", "alice@example.com", "openai", "oauth"))
		prepareOwnerReferralObservation(t, env)
		env.upstream.setInviteStatus(http.StatusBadGateway)
		w := inviteOwner(t, env, "program-1")
		if w.Code != http.StatusAccepted {
			t.Fatalf("status = %d, want 202, body: %s", w.Code, w.Body.String())
		}
		operationID := int(env.data(t, w)["operation_id"].(float64))
		operation := env.waitForOperation(t, env.aliceAuth, operationID, "unknown")
		if got := operation["error_code"]; got != "upstream-outcome-unknown" {
			t.Fatalf("error_code = %v, want upstream-outcome-unknown", got)
		}
		if calls := env.upstream.resetCallCount(); calls != 0 {
			t.Fatalf("ambiguous invite retried upstream %d times", calls)
		}

		list := env.data(t, env.request(http.MethodGet, "/api/v1/user/owner-accounts", nil, env.aliceAuth))
		blocked := list["items"].([]any)[0].(map[string]any)["blocked_actions"].([]any)
		if len(blocked) != 1 {
			t.Fatalf("blocked_actions = %v, want the unknown invite", blocked)
		}
		if entry := blocked[0].(map[string]any); entry["action"] != "send_invitation" {
			t.Fatalf("blocked action = %v", entry)
		}

		// The same stale observation cannot be used to retry.
		retry := inviteOwner(t, env, "program-1")
		if retry.Code != http.StatusConflict || env.errorCode(t, retry) != "invite-freshness-required" {
			t.Fatalf("retry = %d/%s, want 409/invite-freshness-required", retry.Code, env.errorCode(t, retry))
		}

		// A newer referral observation unblocks the action once the shared refresh
		// cooldown has elapsed.
		env.upstream.setInviteStatus(0)
		env.backdateRefreshes(t, 2*time.Minute)
		prepareOwnerReferralObservation(t, env)
		accepted := inviteOwner(t, env, "program-1")
		if accepted.Code != http.StatusAccepted {
			t.Fatalf("invite after refresh = %d, want 202, body: %s", accepted.Code, accepted.Body.String())
		}
		env.waitForOperation(t, env.aliceAuth, int(env.data(t, accepted)["operation_id"].(float64)), "succeeded")
	})

	t.Run("unsent result is unknown", func(t *testing.T) {
		env := setupOwnerAccountEnv(t, nil)
		env.upstream.setAccounts(ownerAccountWire(11, "Alice", "alice@example.com", "openai", "oauth"))
		prepareOwnerReferralObservation(t, env)
		env.upstream.setInviteData(map[string]any{"email": "bob@example.org", "sent": false})
		w := inviteOwner(t, env, "program-1")
		if w.Code != http.StatusAccepted {
			t.Fatalf("status = %d, want 202, body: %s", w.Code, w.Body.String())
		}
		operationID := int(env.data(t, w)["operation_id"].(float64))
		env.waitForOperation(t, env.aliceAuth, operationID, "unknown")
	})

	t.Run("compliance failure becomes failed", func(t *testing.T) {
		env := setupOwnerAccountEnv(t, nil)
		env.upstream.setAccounts(ownerAccountWire(11, "Alice", "alice@example.com", "openai", "oauth"))
		prepareOwnerReferralObservation(t, env)
		env.upstream.setInviteStatus(http.StatusLocked)
		w := inviteOwner(t, env, "program-1")
		if w.Code != http.StatusAccepted {
			t.Fatalf("status = %d, want 202, body: %s", w.Code, w.Body.String())
		}
		operationID := int(env.data(t, w)["operation_id"].(float64))
		operation := env.waitForOperation(t, env.aliceAuth, operationID, "failed")
		if got := operation["error_code"]; got != "compliance-ack-required" {
			t.Fatalf("error_code = %v, want compliance-ack-required", got)
		}
	})
}

func queryInvite(t *testing.T, env *ownerAccountTestEnv, operationID int) string {
	t.Helper()
	row, err := env.client.OwnerAccountOperation.Get(context.Background(), operationID)
	if err != nil {
		t.Fatalf("load invite operation: %v", err)
	}
	if row.InviteEmail == nil {
		return ""
	}
	return *row.InviteEmail
}

// ownerAccountQuotaObservation is the sanitized result a succeeded quota refresh
// or reset persists; gates read only these fields.
func ownerAccountQuotaObservation(credits int, expiry string) map[string]any {
	expiries := make([]string, 0, credits)
	for index := 0; index < credits; index++ {
		expiries = append(expiries, expiry)
	}
	return map[string]any{
		"reset_credits_remaining": credits,
		"reset_credit_expiries":   expiries,
	}
}
