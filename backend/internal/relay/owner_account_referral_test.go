package relay_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/google/go-cmp/cmp"
)

func TestRefreshOpenAIReferralProjectsEligibility(t *testing.T) {
	var gotMethod, gotPath string
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/admin/openai/accounts/11/referrals/refresh", func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		writeOriginEnvelope(w, map[string]any{
			"eligibility": map[string]any{
				"should_show":                    true,
				"program_id":                     "program-1",
				"available_invites":              4,
				"remaining_send_capacity":        6,
				"requires_explicit_confirmation": true,
				"fetched_at":                     1760000000,
				"rules":                          []any{"must-not-surface"},
			},
			"cache_persisted": true,
		})
	})

	manager := newTestProvider(t, mux).(relay.OpenAIReferralManager)
	got, err := manager.RefreshOpenAIReferral(context.Background(), 11)
	if err != nil {
		t.Fatalf("RefreshOpenAIReferral() error = %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/v1/admin/openai/accounts/11/referrals/refresh" {
		t.Fatalf("request = %s %s, want POST /api/v1/admin/openai/accounts/11/referrals/refresh", gotMethod, gotPath)
	}
	observedAt := time.Date(2025, 10, 9, 8, 53, 20, 0, time.UTC)
	availableInvites := 4
	requiresConfirmation := true
	want := &relay.OpenAIReferral{
		ShouldShow:                   true,
		ProgramID:                    "program-1",
		AvailableInvites:             &availableInvites,
		RequiresExplicitConfirmation: &requiresConfirmation,
		ObservedAt:                   &observedAt,
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("referral projection mismatch (-want +got):\n%s", diff)
	}
}

// TestSendOpenAIReferralInviteSendsTheObservationAndTreatsAnUnsentResultAsUnknown
// asserts the invite gate inputs reach upstream unchanged, a sent invitation
// returns the refreshed eligibility, and every ambiguous answer is reported as an
// unknown outcome instead of a definitive failure.
func TestSendOpenAIReferralInviteSendsTheObservationAndTreatsAnUnsentResultAsUnknown(t *testing.T) {
	t.Run("sent invitation", func(t *testing.T) {
		var gotPayload map[string]json.RawMessage
		mux := http.NewServeMux()
		mux.HandleFunc("/api/v1/admin/openai/accounts/11/referrals/invite", func(w http.ResponseWriter, r *http.Request) {
			if err := json.NewDecoder(r.Body).Decode(&gotPayload); err != nil {
				t.Fatalf("decode invite payload: %v", err)
			}
			writeOriginEnvelope(w, map[string]any{
				"email": "bob@example.org",
				"sent":  true,
				"eligibility": map[string]any{
					"should_show":       true,
					"program_id":        "program-1",
					"available_invites": 3,
					"fetched_at":        1760000000,
				},
			})
		})

		manager := newTestProvider(t, mux).(relay.OpenAIReferralManager)
		got, err := manager.SendOpenAIReferralInvite(context.Background(), 11, relay.OpenAIReferralInviteRequest{
			Email:     "bob@example.org",
			ProgramID: "program-1",
			Confirmed: true,
		})
		if err != nil {
			t.Fatalf("SendOpenAIReferralInvite() error = %v", err)
		}
		for key, wantValue := range map[string]string{
			"email":      `"bob@example.org"`,
			"program_id": `"program-1"`,
			"confirmed":  `true`,
		} {
			if string(gotPayload[key]) != wantValue {
				t.Fatalf("invite payload %s = %s, want %s", key, gotPayload[key], wantValue)
			}
		}
		if got.Email != "bob@example.org" {
			t.Fatalf("result email = %q, want bob@example.org", got.Email)
		}
		if got.Referral == nil || got.Referral.AvailableInvites == nil || *got.Referral.AvailableInvites != 3 {
			t.Fatalf("refreshed referral = %+v, want available_invites 3", got.Referral)
		}
	})

	t.Run("unsent result is unknown", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("/api/v1/admin/openai/accounts/11/referrals/invite", func(w http.ResponseWriter, _ *http.Request) {
			writeOriginEnvelope(w, map[string]any{"email": "bob@example.org", "sent": false})
		})
		manager := newTestProvider(t, mux).(relay.OpenAIReferralManager)
		_, err := manager.SendOpenAIReferralInvite(context.Background(), 11, relay.OpenAIReferralInviteRequest{
			Email: "bob@example.org", ProgramID: "program-1", Confirmed: true,
		})
		if err == nil || !errors.Is(err, relay.ErrUpstreamOutcomeUnknown) {
			t.Fatalf("error = %v, want ErrUpstreamOutcomeUnknown", err)
		}
	})

	t.Run("bad gateway is unknown and leaks no upstream text", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("/api/v1/admin/openai/accounts/11/referrals/invite", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusBadGateway)
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 502, "message": "must-not-surface"})
		})
		manager := newTestProvider(t, mux).(relay.OpenAIReferralManager)
		_, err := manager.SendOpenAIReferralInvite(context.Background(), 11, relay.OpenAIReferralInviteRequest{
			Email: "bob@example.org", ProgramID: "program-1", Confirmed: true,
		})
		if err == nil || !errors.Is(err, relay.ErrUpstreamOutcomeUnknown) {
			t.Fatalf("error = %v, want ErrUpstreamOutcomeUnknown", err)
		}
		if strings.Contains(err.Error(), "must-not-surface") {
			t.Fatalf("unknown-outcome error leaks upstream text: %v", err)
		}
	})
}
