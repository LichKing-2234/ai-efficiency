package relay_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

// TestReadOwnerAccountUsageReportsWindowsHealthAndPerAccountFailures asserts the
// batch read: the explicit force flag, both windows with their window stats, the
// health facts derived by the upstream usage read, and a per-account failure that
// does not fail the whole batch.
func TestReadOwnerAccountUsageReportsWindowsHealthAndPerAccountFailures(t *testing.T) {
	var gotPath string
	var gotPayload map[string]json.RawMessage
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/admin/accounts/usage/batch", func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&gotPayload); err != nil {
			t.Fatalf("decode batch usage payload: %v", err)
		}
		writeOriginEnvelope(w, map[string]any{
			"usage": map[string]any{
				"11": map[string]any{
					"updated_at":   "2026-10-23T04:00:00Z",
					"needs_reauth": true,
					"is_banned":    true,
					"is_forbidden": true,
					"error_code":   "forbidden",
					"five_hour": map[string]any{
						"utilization": 12.5,
						"resets_at":   "2026-10-23T09:00:00Z",
						"window_stats": map[string]any{
							"requests": 3, "tokens": 1200, "cost": 0.75, "standard_cost": 1.25, "user_cost": 0.5,
						},
					},
					"seven_day": map[string]any{
						"utilization": 62.5,
						"resets_at":   "2026-10-29T09:00:00Z",
					},
				},
			},
			"errors": map[string]string{"12": "upstream returned 401"},
		})
	})

	reader := newTestProvider(t, mux).(relay.OwnerAccountUsageReader)
	got, err := reader.ReadOwnerAccountUsage(context.Background(), []int64{12, 11}, true)
	if err != nil {
		t.Fatalf("ReadOwnerAccountUsage() error = %v", err)
	}
	if gotPath != "/api/v1/admin/accounts/usage/batch" {
		t.Fatalf("path = %q, want /api/v1/admin/accounts/usage/batch", gotPath)
	}
	force, ok := gotPayload["force"]
	if !ok || string(force) != "true" {
		t.Fatalf("force payload = %s, want true", force)
	}

	updatedAt := time.Date(2026, 10, 23, 4, 0, 0, 0, time.UTC)
	fiveHourResetAt := time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)
	sevenDayResetAt := time.Date(2026, 10, 29, 9, 0, 0, 0, time.UTC)
	want := []relay.OwnerAccountUsage{
		{
			AccountID: 11,
			UpdatedAt: &updatedAt,
			FiveHour: &relay.OwnerAccountUsageWindow{
				Utilization:  12.5,
				ResetsAt:     &fiveHourResetAt,
				Requests:     3,
				Tokens:       1200,
				Cost:         float64Ptr(0.75),
				StandardCost: 1.25,
				UserCost:     0.5,
			},
			SevenDay: &relay.OwnerAccountUsageWindow{
				Utilization: 62.5,
				ResetsAt:    &sevenDayResetAt,
			},
			Health: relay.OwnerAccountUsageHealth{
				NeedsReauth: true,
				IsBanned:    true,
				IsForbidden: true,
				ErrorCode:   "forbidden",
			},
		},
		{AccountID: 12},
	}
	if diff := cmp.Diff(want, got, cmpopts.IgnoreFields(relay.OwnerAccountUsage{}, "Err")); diff != "" {
		t.Fatalf("owner account usage mismatch (-want +got):\n%s", diff)
	}
	if got[1].Err == nil {
		t.Fatal("account 12 has an upstream usage error but the batch projection reports none")
	}
}

func float64Ptr(value float64) *float64 { return &value }
