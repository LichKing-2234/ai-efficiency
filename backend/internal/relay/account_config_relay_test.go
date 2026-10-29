package relay_test

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"testing"

	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/google/go-cmp/cmp"
)

func sortedRawKeys(m map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func intPtr(value int) *int {
	return &value
}

func TestReadAccountConfigurationsProjectsOnlyAllowListedKeys(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/admin/accounts", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("platform"); got != "openai" {
			t.Errorf("platform filter = %q, want openai", got)
		}
		writeOriginEnvelope(w, map[string]any{
			"items": []any{
				map[string]any{
					"id": 11, "name": "Account Alpha", "platform": "openai", "type": "oauth",
					"status": "active", "schedulable": true,
					"credentials": map[string]any{
						"access_token":       "must-not-escape",
						"refresh_token":      "must-not-escape",
						"id_token":           "must-not-escape",
						"chatgpt_account_id": "must-not-escape",
						"_token_version":     7,
						"model_mapping":      map[string]string{"gpt-5.5": "gpt-6.1-sol"},
					},
					"extra": map[string]any{
						"privacy_mode":                          "training_off",
						"openai_compact_mode":                   "auto",
						"codex_cli_only":                        true,
						"auto_reset_credit_7d_threshold":        1,
						"auto_reset_credit_expiry_enabled":      true,
						"auto_reset_credit_expiry_lead_minutes": 30,
						"codex_auto_reset_credit_expiry_at":     "must-not-escape",
						"codex_fingerprint_seed":                "must-not-escape",
						"codex_5h_used_percent":                 42,
						"upstream_model_metadata":               map[string]any{"private": "must-not-escape"},
						// Two live credits out of three: the expired one is not counted, and
						// the display-only count must not leak into the configuration.
						"codex_reset_credit_snapshot": map[string]any{
							"available_count": 3,
							"credits": []any{
								map[string]any{"expires_at": "2020-01-01T00:00:00Z"},
								map[string]any{"expires_at": "2999-01-01T00:00:00Z"},
								map[string]any{"expires_at": "2999-01-02T00:00:00Z"},
							},
						},
					},
				},
				map[string]any{
					"id": 12, "name": "Account Beta", "platform": "openai", "type": "apikey",
					"status": "active", "schedulable": true,
					"credentials": map[string]any{"api_key": "must-not-escape"},
					"extra": map[string]any{
						"codex_fingerprint_seed": "must-not-escape",
					},
				},
				map[string]any{
					"id": 13, "name": "Account Gamma", "platform": "openai", "type": "oauth",
					"status": "active", "schedulable": true,
					"credentials": map[string]any{"access_token": "must-not-escape"},
					"extra": map[string]any{
						// A positive count without expiry details cannot be proven live, so it
						// clamps to zero instead of reporting credits that may already be gone.
						"codex_reset_credit_snapshot": map[string]any{"available_count": 2},
					},
				},
			},
			"page": 1, "pages": 1,
		})
	})

	reader := newTestProvider(t, mux).(relay.AccountConfigurationReader)
	got, err := reader.ReadAccountConfigurations(context.Background(), "openai")
	if err != nil {
		t.Fatalf("ReadAccountConfigurations() error = %v", err)
	}
	// A whole-value diff: anything not on the allow-list can only surface here.
	want := map[int64]relay.AccountConfigurationRecord{
		11: {
			Configuration: relay.AccountConfiguration{
				ModelMapping: map[string]string{"gpt-5.5": "gpt-6.1-sol"},
				Extra: map[string]any{
					"privacy_mode":                          "training_off",
					"openai_compact_mode":                   "auto",
					"codex_cli_only":                        true,
					"auto_reset_credit_7d_threshold":        float64(1),
					"auto_reset_credit_expiry_enabled":      true,
					"auto_reset_credit_expiry_lead_minutes": float64(30),
				},
			},
			ResetCreditsAvailable: intPtr(2),
		},
		12: {},
		13: {
			ResetCreditsAvailable: intPtr(0),
		},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("account configurations mismatch (-want +got):\n%s", diff)
	}
}

func TestApplyAccountConfigurationSendsOnlyTheMergeableAllowList(t *testing.T) {
	var gotMethod, gotPath string
	var payload map[string]json.RawMessage

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/admin/accounts/bulk-update", func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode bulk-update payload: %v", err)
		}
		writeOriginEnvelope(w, map[string]any{"updated": 2})
	})

	applier := newTestProvider(t, mux).(relay.AccountConfigurationApplier)
	err := applier.ApplyAccountConfiguration(context.Background(), []int64{11, 12}, relay.AccountConfiguration{
		ModelMapping: map[string]string{"gpt-5.5": "gpt-6.1-sol"},
		Extra: map[string]any{
			"privacy_mode":           "training_off",
			"codex_fingerprint_seed": "must-not-be-sent",
			// The reset-credit snapshot is a display-only observation, not
			// configuration; a caller that somehow carries it still cannot write it.
			"codex_reset_credit_snapshot": map[string]any{"available_count": 2},
		},
	})
	if err != nil {
		t.Fatalf("ApplyAccountConfiguration() error = %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/v1/admin/accounts/bulk-update" {
		t.Fatalf("request = %s %s, want POST /api/v1/admin/accounts/bulk-update", gotMethod, gotPath)
	}
	if diff := cmp.Diff([]string{"account_ids", "credentials", "extra"}, sortedRawKeys(payload)); diff != "" {
		t.Fatalf("payload keys mismatch (-want +got):\n%s", diff)
	}

	var accountIDs []int64
	if err := json.Unmarshal(payload["account_ids"], &accountIDs); err != nil {
		t.Fatalf("decode account_ids: %v", err)
	}
	if diff := cmp.Diff([]int64{11, 12}, accountIDs); diff != "" {
		t.Fatalf("account_ids mismatch (-want +got):\n%s", diff)
	}

	var credentials map[string]json.RawMessage
	if err := json.Unmarshal(payload["credentials"], &credentials); err != nil {
		t.Fatalf("decode credentials: %v", err)
	}
	if diff := cmp.Diff([]string{"model_mapping"}, sortedRawKeys(credentials)); diff != "" {
		t.Fatalf("credentials keys mismatch (-want +got):\n%s", diff)
	}

	var extra map[string]any
	if err := json.Unmarshal(payload["extra"], &extra); err != nil {
		t.Fatalf("decode extra: %v", err)
	}
	if diff := cmp.Diff(map[string]any{"privacy_mode": "training_off"}, extra); diff != "" {
		t.Fatalf("extra mismatch (-want +got):\n%s", diff)
	}
}

// A null section would be merged as `column || NULL`, which Postgres resolves to
// NULL and would wipe the stored column. An empty section must be absent from the
// payload, never present as null.
func TestApplyAccountConfigurationOmitsEmptySections(t *testing.T) {
	var payload map[string]json.RawMessage

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/admin/accounts/bulk-update", func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode bulk-update payload: %v", err)
		}
		writeOriginEnvelope(w, map[string]any{"updated": 1})
	})

	applier := newTestProvider(t, mux).(relay.AccountConfigurationApplier)
	if err := applier.ApplyAccountConfiguration(context.Background(), []int64{11}, relay.AccountConfiguration{}); err != nil {
		t.Fatalf("ApplyAccountConfiguration() error = %v", err)
	}
	if diff := cmp.Diff([]string{"account_ids"}, sortedRawKeys(payload)); diff != "" {
		t.Fatalf("payload keys mismatch (-want +got):\n%s", diff)
	}
}
