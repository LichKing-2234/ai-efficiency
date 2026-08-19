package relay_test

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/internal/relay"
)

func TestReadGroupOAuthPoolUsageFiltersAccountsAndAveragesValidSevenDaySnapshots(t *testing.T) {
	var batchIDs []int64
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/admin/accounts", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("type"); got != "oauth" {
			t.Errorf("account type filter = %q, want oauth", got)
		}
		if got := r.URL.Query().Get("status"); got != "" {
			t.Errorf("account status filter = %q, want no upstream status filter", got)
		}
		if got := r.URL.Query().Get("page_size"); got != "1000" {
			t.Errorf("account page size = %q, want 1000", got)
		}
		groupID := r.URL.Query().Get("group")
		items := []map[string]any{}
		switch groupID {
		case "42":
			items = []map[string]any{
				{"id": 1, "type": "oauth", "status": "active"},
				{"id": 2, "type": "oauth", "status": "active"},
				{"id": 6, "type": "oauth", "status": "active", "rate_limit_reset_at": "2099-07-16T00:00:00Z"},
				{"id": 3, "type": "api_key", "status": "active"},
				{"id": 4, "type": "oauth", "status": "inactive"},
			}
		case "43":
			items = []map[string]any{
				{"id": 2, "type": "oauth", "status": "active"},
				{"id": 5, "type": "oauth", "status": "active"},
			}
		default:
			t.Fatalf("unexpected group filter %q", groupID)
		}
		writeOriginEnvelope(w, map[string]any{"items": items, "page": 1, "pages": 1})
	})
	mux.HandleFunc("/api/v1/admin/accounts/usage/batch", func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			AccountIDs []int64 `json:"account_ids"`
			Force      bool    `json:"force"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode batch payload: %v", err)
		}
		batchIDs = append(batchIDs, payload.AccountIDs...)
		if payload.Force {
			t.Error("batch usage force = true, want false")
		}
		writeOriginEnvelope(w, map[string]any{
			"usage": map[string]any{
				"1": map[string]any{
					"updated_at": "2099-07-15T01:00:00Z",
					"seven_day":  map[string]any{"utilization": 10, "resets_at": "2099-07-23T00:00:00Z"},
				},
				"2": map[string]any{
					"updated_at": "2099-07-15T02:00:00Z",
					"seven_day":  map[string]any{"utilization": 30, "resets_at": "2099-07-22T00:00:00Z"},
				},
				"6": map[string]any{
					"updated_at": "2099-07-15T04:00:00Z",
					"seven_day":  map[string]any{"utilization": 50, "resets_at": "2099-07-24T00:00:00Z"},
				},
				"5": map[string]any{"updated_at": "2099-07-15T03:00:00Z"},
			},
			"errors": map[string]string{},
		})
	})

	reader := newTestProvider(t, mux).(relay.GroupOAuthPoolUsageReader)
	state, err := reader.ReadGroupOAuthPoolUsage(context.Background(), []int64{43, 42, 42})
	if err != nil {
		t.Fatalf("ReadGroupOAuthPoolUsage() error = %v", err)
	}
	if got, want := batchIDs, []int64{1, 2, 5, 6}; !equalInt64s(got, want) {
		t.Fatalf("batch account IDs = %v, want %v", got, want)
	}
	if state.Status != "ok" || len(state.Groups) != 2 {
		t.Fatalf("pool state = %+v, want two groups", state)
	}
	sort.Slice(state.Groups, func(i, j int) bool { return state.Groups[i].GroupID < state.Groups[j].GroupID })
	group42 := state.Groups[0]
	if group42.GroupID != "42" || group42.Status != "ok" || group42.ValidOAuthAccounts != 3 || group42.TotalActiveOAuthAccounts != 3 || group42.AverageWeeklyUtilization != 30 {
		t.Fatalf("group 42 = %+v", group42)
	}
	if got := group42.NextResetAt.UTC(); !got.Equal(time.Date(2099, 7, 22, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("group 42 reset = %s, want 2099-07-22", got)
	}
	group43 := state.Groups[1]
	if group43.GroupID != "43" || group43.Status != "partial" || group43.ValidOAuthAccounts != 1 || group43.TotalActiveOAuthAccounts != 2 || group43.AverageWeeklyUtilization != 30 {
		t.Fatalf("group 43 = %+v", group43)
	}
}

func equalInt64s(left, right []int64) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
