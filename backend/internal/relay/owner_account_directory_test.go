package relay_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/google/go-cmp/cmp"
)

// TestListOwnerAccountsProjectsOnlyOwnerSafeFacts asserts the whole projected
// entry, so anything the projection must not carry — parent identity, notes,
// proxies, credential material beyond the email, the fingerprint seed, and
// arbitrary extra keys — can only surface as a diff.
func TestListOwnerAccountsProjectsOnlyOwnerSafeFacts(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/admin/accounts", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("platform"); got != "openai" {
			t.Errorf("platform filter = %q, want openai", got)
		}
		if got := r.URL.Query().Get("lite"); got != "false" {
			t.Errorf("lite filter = %q, want false; the lite projection has no group names", got)
		}
		writeOriginEnvelope(w, map[string]any{
			"items": []any{map[string]any{
				"id": 11, "name": "alice@example.com", "platform": "openai", "type": "oauth",
				"status": "active", "schedulable": true,
				"rate_limit_reset_at":       "2026-10-23T05:00:00Z",
				"parent_account_id":         99,
				"parent_email":              "must-not-escape@example.net",
				"parent_plan_type":          "must-not-escape",
				"parent_chatgpt_account_id": "must-not-escape",
				"notes":                     "must-not-escape",
				"proxy":                     map[string]any{"id": 3, "host": "must-not-escape"},
				"credentials": map[string]any{
					"email":                  " alice@example.com ",
					"plan_type":              "must-not-escape",
					"access_token":           "must-not-escape",
					"refresh_token":          "must-not-escape",
					"id_token":               "must-not-escape",
					"chatgpt_account_id":     "must-not-escape",
					"codex_fingerprint_seed": "must-not-escape",
				},
				"groups": []any{
					map[string]any{"id": 21, "name": "Group Alpha", "platform": "openai"},
					map[string]any{"id": 22, "name": "Group Beta", "platform": "openai"},
				},
				"account_groups": []any{
					map[string]any{"group_id": 22, "priority": 1},
					map[string]any{"group_id": 21, "priority": 2},
					map[string]any{"group_id": 23, "priority": 3},
				},
				"group_ids": []any{21, 22, 23},
				"extra": map[string]any{
					"codex_fingerprint_seed":    "must-not-escape",
					"codex_5h_used_percent":     42,
					"upstream_model_metadata":   map[string]any{"private": "must-not-escape"},
					"auto_reset_credit_enabled": true,
				},
			}},
			"page": 1, "pages": 1,
		})
	})

	reader := newTestProvider(t, mux).(relay.OwnerAccountDirectoryReader)
	got, err := reader.ListOwnerAccounts(context.Background(), "openai")
	if err != nil {
		t.Fatalf("ListOwnerAccounts() error = %v", err)
	}
	rateLimitedUntil := time.Date(2026, 10, 23, 5, 0, 0, 0, time.UTC)
	want := []relay.OwnerAccount{{
		ID:       11,
		Name:     "alice@example.com",
		Email:    "alice@example.com",
		Platform: "openai",
		Type:     "oauth",
		Status:   "active",
		// Only the derived shadow fact survives; the parent identity itself must
		// not appear anywhere in the projection.
		Shadow:           true,
		Schedulable:      true,
		RateLimitedUntil: &rateLimitedUntil,
		// Group 23 has no name in either embedded group object, so it is omitted
		// instead of being rendered as a numeric ID.
		Groups: []relay.OwnerAccountGroup{
			{ID: 22, Name: "Group Beta", Priority: 1},
			{ID: 21, Name: "Group Alpha", Priority: 2},
		},
		AutoResetCreditEnabled: true,
	}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("owner accounts mismatch (-want +got):\n%s", diff)
	}
}

// TestListOwnerAccountsProjectsTheFiveAllowListedExtraKeys asserts the typed
// snapshots: unix-second timestamps become RFC3339-valued times, reset credits
// are expiry-clamped, and nothing else in `extra` reaches the projection.
func TestListOwnerAccountsProjectsTheFiveAllowListedExtraKeys(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/admin/accounts", func(w http.ResponseWriter, _ *http.Request) {
		writeOriginEnvelope(w, map[string]any{
			"items": []any{map[string]any{
				"id": 11, "name": "bob", "platform": "openai", "type": "oauth",
				"status": "active", "schedulable": true,
				"credentials": map[string]any{"email": "bob@example.org"},
				"extra": map[string]any{
					"codex_credits_snapshot": map[string]any{
						"credits":    map[string]any{"has_credits": true, "unlimited": false, "balance": "12.5"},
						"fetched_at": 1760000000,
					},
					"codex_reset_credit_snapshot": map[string]any{
						"available_count": 2,
						"credits": []any{
							map[string]any{"expires_at": "2000-01-01T00:00:00Z"},
							map[string]any{"expires_at": "2999-01-01T00:00:00Z"},
						},
					},
					"codex_referral_snapshot": map[string]any{
						"should_show":                    true,
						"program_id":                     "program-1",
						"available_invites":              3,
						"requires_explicit_confirmation": false,
						"fetched_at":                     1760000123,
					},
					"codex_usage_updated_at":    "2026-10-22T10:00:00Z",
					"auto_reset_credit_enabled": true,
					"codex_5h_used_percent":     42,
				},
			}},
			"page": 1, "pages": 1,
		})
	})

	reader := newTestProvider(t, mux).(relay.OwnerAccountDirectoryReader)
	got, err := reader.ListOwnerAccounts(context.Background(), "openai")
	if err != nil {
		t.Fatalf("ListOwnerAccounts() error = %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("account count = %d, want 1", len(got))
	}
	account := got[0]

	balance := "12.5"
	usageUpdatedAt := time.Date(2026, 10, 22, 10, 0, 0, 0, time.UTC)
	creditsObservedAt := time.Date(2025, 10, 9, 8, 53, 20, 0, time.UTC)
	referralObservedAt := time.Date(2025, 10, 9, 8, 55, 23, 0, time.UTC)
	availableInvites := 3
	requiresConfirmation := false
	want := relay.OwnerAccount{
		ID: 11, Name: "bob", Email: "bob@example.org", Platform: "openai", Type: "oauth",
		Status: "active", Schedulable: true,
		UsageUpdatedAt: &usageUpdatedAt,
		Credits: &relay.OwnerAccountCredits{
			HasCredits: true,
			Unlimited:  false,
			Balance:    &balance,
			ObservedAt: &creditsObservedAt,
		},
		// The expired credit is dropped; only the live one is reported.
		ResetCredits: &relay.OwnerAccountResetCredits{
			Available: 1,
			ExpiresAt: []time.Time{time.Date(2999, 1, 1, 0, 0, 0, 0, time.UTC)},
		},
		Referral: &relay.OwnerAccountReferral{
			ShouldShow:                   true,
			ProgramID:                    "program-1",
			AvailableInvites:             &availableInvites,
			RequiresExplicitConfirmation: &requiresConfirmation,
			ObservedAt:                   &referralObservedAt,
		},
		AutoResetCreditEnabled: true,
	}
	if diff := cmp.Diff(want, account); diff != "" {
		t.Fatalf("owner account mismatch (-want +got):\n%s", diff)
	}
}
