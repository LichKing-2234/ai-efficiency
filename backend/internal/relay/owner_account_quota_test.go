package relay_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/google/go-cmp/cmp"
)

// TestRefreshOpenAIQuotaProjectsCreditsResetCreditsAndUnixTimestamps asserts the
// quota refresh projection: unix-second window reset times and the observation
// time become RFC3339-valued times, reset credits are expiry-clamped, and the
// spendable credit balance keeps its upstream decimal string.
func TestRefreshOpenAIQuotaProjectsCreditsResetCreditsAndUnixTimestamps(t *testing.T) {
	var gotMethod, gotPath string
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/admin/openai/accounts/11/quota/refresh", func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		writeOriginEnvelope(w, map[string]any{
			"user_id":    "user-must-not-surface",
			"account_id": "account-must-not-surface",
			"email":      "alice@example.com",
			"plan_type":  "plus",
			"rate_limit": map[string]any{
				"primary_window": map[string]any{
					"used_percent":         42.5,
					"limit_window_seconds": 18000,
					"reset_at":             1760003600,
				},
				"secondary_window": map[string]any{
					"used_percent":         77,
					"limit_window_seconds": 604800,
					"reset_at":             1760500000,
				},
			},
			"rate_limit_reset_credits": map[string]any{
				"available_count": 3,
				"credits": []any{
					map[string]any{"expires_at": "2000-01-01T00:00:00Z"},
					map[string]any{"expires_at": "2999-01-01T00:00:00Z"},
					map[string]any{"expires_at": "not-a-time"},
				},
			},
			"credits":    map[string]any{"has_credits": true, "unlimited": true, "balance": nil},
			"fetched_at": 1760000000,
		})
	})

	manager := newTestProvider(t, mux).(relay.OpenAIQuotaManager)
	got, err := manager.RefreshOpenAIQuota(context.Background(), 11)
	if err != nil {
		t.Fatalf("RefreshOpenAIQuota() error = %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/v1/admin/openai/accounts/11/quota/refresh" {
		t.Fatalf("request = %s %s, want POST /api/v1/admin/openai/accounts/11/quota/refresh", gotMethod, gotPath)
	}

	observedAt := time.Date(2025, 10, 9, 8, 53, 20, 0, time.UTC)
	primaryResetAt := time.Date(2025, 10, 9, 9, 53, 20, 0, time.UTC)
	secondaryResetAt := time.Date(2025, 10, 15, 3, 46, 40, 0, time.UTC)
	want := &relay.OpenAIQuota{
		PlanType: "plus",
		PrimaryWindow: &relay.OpenAIQuotaWindow{
			Utilization:   42.5,
			WindowSeconds: 18000,
			ResetsAt:      &primaryResetAt,
		},
		SecondaryWindow: &relay.OpenAIQuotaWindow{
			Utilization:   77,
			WindowSeconds: 604800,
			ResetsAt:      &secondaryResetAt,
		},
		ResetCredits: &relay.OwnerAccountResetCredits{
			Available: 1,
			ExpiresAt: []time.Time{time.Date(2999, 1, 1, 0, 0, 0, 0, time.UTC)},
		},
		Credits: &relay.OwnerAccountCredits{
			HasCredits: true,
			Unlimited:  true,
			ObservedAt: &observedAt,
		},
		ObservedAt: &observedAt,
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("quota projection mismatch (-want +got):\n%s", diff)
	}
}

// TestConsumeOpenAIResetCreditReportsWindowsResetAsInt asserts that a successful
// reset reports the consumed credit, the number of windows reset as an integer,
// any post-reset warning code, and the post-reset quota observation.
func TestConsumeOpenAIResetCreditReportsWindowsResetAsInt(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/admin/openai/accounts/11/reset-quota", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		writeOriginEnvelope(w, map[string]any{
			"code": "ok",
			"credit": map[string]any{
				"id":         "credit-must-not-surface",
				"expires_at": "2027-01-01T00:00:00Z",
				"status":     "redeemed",
			},
			"windows_reset": 2,
			"warning_code":  "account_state_refresh_failed",
			"quota": map[string]any{
				"plan_type":  "plus",
				"fetched_at": 1760000000,
			},
		})
	})

	manager := newTestProvider(t, mux).(relay.OpenAIQuotaManager)
	got, err := manager.ConsumeOpenAIResetCredit(context.Background(), 11)
	if err != nil {
		t.Fatalf("ConsumeOpenAIResetCredit() error = %v", err)
	}
	var windowsReset int = got.WindowsReset
	if windowsReset != 2 {
		t.Fatalf("WindowsReset = %d (%T), want 2 as an integer", got.WindowsReset, got.WindowsReset)
	}
	if got.Code != "ok" {
		t.Fatalf("Code = %q, want ok", got.Code)
	}
	if got.WarningCode != "account_state_refresh_failed" {
		t.Fatalf("WarningCode = %q, want account_state_refresh_failed", got.WarningCode)
	}
	creditExpiresAt := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
	if diff := cmp.Diff(&creditExpiresAt, got.CreditExpiresAt); diff != "" {
		t.Fatalf("credit expiry mismatch (-want +got):\n%s", diff)
	}
	observedAt := time.Date(2025, 10, 9, 8, 53, 20, 0, time.UTC)
	wantQuota := &relay.OpenAIQuota{PlanType: "plus", ObservedAt: &observedAt}
	if diff := cmp.Diff(wantQuota, got.Quota); diff != "" {
		t.Fatalf("post-reset quota mismatch (-want +got):\n%s", diff)
	}
}

// TestOwnerAccountCapabilitiesMapUpstreamLocksToStableErrors asserts the failure
// mapping the service layer keys off: a compliance lock is an explicit
// acknowledgement failure, and 404/405 on an endpoint this adapter knows how to
// call means the upstream is older than the verified baseline.
func TestOwnerAccountCapabilitiesMapUpstreamLocksToStableErrors(t *testing.T) {
	writeStatus := func(status int) http.Handler {
		mux := http.NewServeMux()
		mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
			_ = json.NewEncoder(w).Encode(map[string]any{"code": status, "message": "must-not-surface"})
		})
		return mux
	}

	t.Run("quota refresh compliance lock", func(t *testing.T) {
		manager := newTestProvider(t, writeStatus(http.StatusLocked)).(relay.OpenAIQuotaManager)
		_, err := manager.RefreshOpenAIQuota(context.Background(), 11)
		if err == nil || !errors.Is(err, relay.ErrComplianceAcknowledgementRequired) {
			t.Fatalf("error = %v, want ErrComplianceAcknowledgementRequired", err)
		}
	})

	t.Run("reset compliance lock", func(t *testing.T) {
		manager := newTestProvider(t, writeStatus(http.StatusLocked)).(relay.OpenAIQuotaManager)
		_, err := manager.ConsumeOpenAIResetCredit(context.Background(), 11)
		if err == nil || !errors.Is(err, relay.ErrComplianceAcknowledgementRequired) {
			t.Fatalf("error = %v, want ErrComplianceAcknowledgementRequired", err)
		}
	})

	t.Run("quota refresh endpoint missing", func(t *testing.T) {
		manager := newTestProvider(t, writeStatus(http.StatusNotFound)).(relay.OpenAIQuotaManager)
		_, err := manager.RefreshOpenAIQuota(context.Background(), 11)
		if err == nil || !errors.Is(err, relay.ErrCapabilityUnsupported) {
			t.Fatalf("error = %v, want ErrCapabilityUnsupported", err)
		}
	})

	t.Run("referral refresh method not allowed", func(t *testing.T) {
		manager := newTestProvider(t, writeStatus(http.StatusMethodNotAllowed)).(relay.OpenAIReferralManager)
		_, err := manager.RefreshOpenAIReferral(context.Background(), 11)
		if err == nil || !errors.Is(err, relay.ErrCapabilityUnsupported) {
			t.Fatalf("error = %v, want ErrCapabilityUnsupported", err)
		}
	})
}
