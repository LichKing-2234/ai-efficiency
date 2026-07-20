package relay

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"

	"go.uber.org/zap"
)

func TestUsersTrendBatchUsesOnlyAggregateRouteAndFiltersAuthorizedRows(t *testing.T) {
	var mu sync.Mutex
	requested := make([]string, 0, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requested = append(requested, r.URL.RequestURI())
		mu.Unlock()

		switch r.URL.Path {
		case "/api/v1/admin/dashboard/users-trend":
			wantQuery := url.Values{
				"start_date":  {"2026-07-01"},
				"end_date":    {"2026-07-20"},
				"granularity": {"day"},
				"timezone":    {"Asia/Shanghai"},
				"limit":       {"500"},
			}
			if !reflect.DeepEqual(r.URL.Query(), wantQuery) {
				t.Errorf("query = %#v, want %#v", r.URL.Query(), wantQuery)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"success": true,
				"data": map[string]any{"trend": []map[string]any{
					{"date": "2026-07-02", "user_id": 101, "tokens": 22, "actual_cost": 2.25},
					{"date": "2026-07-01", "user_id": 999, "tokens": 99, "actual_cost": 9.99},
					{"date": "2026-07-01", "user_id": 101, "tokens": 11, "actual_cost": 1.25},
				}},
			})
		case "/api/v1/admin/dashboard/trend":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"success": true,
				"data":    []map[string]any{{"date": "2026-07-01", "actual_cost": 1, "total_tokens": 1}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	provider := &sub2apiRelay{
		client: server.Client(), adminURL: server.URL, baseURL: server.URL + "/v1",
		apiKey: "test-admin-key", model: "test-model", logger: zap.NewNop(),
	}

	got, err := provider.GetUsageTrendForUsers(context.Background(), []int64{101, 102}, TeamMemberTrendParams{
		StartDate: "2026-07-01", EndDate: "2026-07-20", Granularity: "day", Timezone: "Asia/Shanghai",
	})
	if err != nil {
		t.Fatalf("GetUsageTrendForUsers() error = %v", err)
	}
	mu.Lock()
	requests := append([]string(nil), requested...)
	mu.Unlock()
	if len(requests) != 1 || !strings.HasPrefix(requests[0], "/api/v1/admin/dashboard/users-trend?") {
		t.Fatalf("trend requests = %v, want one users-trend request", requests)
	}
	if _, exists := got[999]; exists {
		t.Fatalf("trend map contains unauthorized user 999: %#v", got)
	}
	if len(got[101]) != 2 || got[101][0].Date != "2026-07-01" || got[101][1].Date != "2026-07-02" {
		t.Fatalf("authorized user 101 points = %#v, want sorted aggregate rows", got[101])
	}
	if points, exists := got[102]; !exists || len(points) != 0 {
		t.Fatalf("zero-usage authorized user 102 = %#v, exists %v, want explicit empty trend", points, exists)
	}
}

func TestUsersTrendBatchRejectsPossiblyTruncatedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/admin/dashboard/trend" {
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": []any{}})
			return
		}
		rows := make([]map[string]any, 500)
		for index := range rows {
			rows[index] = map[string]any{
				"date": "2026-07-01", "user_id": index + 1, "tokens": index, "actual_cost": float64(index),
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": map[string]any{"trend": rows}})
	}))
	t.Cleanup(server.Close)
	provider := &sub2apiRelay{client: server.Client(), adminURL: server.URL, apiKey: "test-admin-key", logger: zap.NewNop()}

	if _, err := provider.GetUsageTrendForUsers(context.Background(), []int64{101}, TeamMemberTrendParams{}); err == nil {
		t.Fatal("GetUsageTrendForUsers() error = nil, want possibly truncated users-trend rejection")
	}
}

func TestUsersTrendBatchRejectsMalformedAggregateRows(t *testing.T) {
	tests := []struct {
		name string
		row  map[string]any
	}{
		{name: "non-positive user", row: map[string]any{"date": "2026-07-01", "user_id": 0, "tokens": 1, "actual_cost": 1}},
		{name: "blank date", row: map[string]any{"date": " ", "user_id": 101, "tokens": 1, "actual_cost": 1}},
		{name: "missing tokens", row: map[string]any{"date": "2026-07-01", "user_id": 101, "actual_cost": 1}},
		{name: "negative tokens", row: map[string]any{"date": "2026-07-01", "user_id": 101, "tokens": -1, "actual_cost": 1}},
		{name: "missing actual cost", row: map[string]any{"date": "2026-07-01", "user_id": 101, "tokens": 1}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v1/admin/dashboard/trend" {
					_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": []any{}})
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{
					"success": true, "data": map[string]any{"trend": []map[string]any{test.row}},
				})
			}))
			t.Cleanup(server.Close)
			provider := &sub2apiRelay{client: server.Client(), adminURL: server.URL, apiKey: "test-admin-key", logger: zap.NewNop()}
			if _, err := provider.GetUsageTrendForUsers(context.Background(), []int64{101}, TeamMemberTrendParams{}); err == nil {
				t.Fatalf("GetUsageTrendForUsers() error = nil, want %s rejection", test.name)
			}
		})
	}
}

func TestNoPodTrendCacheOnSub2APIRelay(t *testing.T) {
	relayType := reflect.TypeOf(sub2apiRelay{})
	if field, exists := relayType.FieldByName("teamTrends"); exists {
		t.Fatalf("sub2apiRelay still contains completed trend cache field %s of type %s", field.Name, field.Type)
	}
	for index := 0; index < relayType.NumField(); index++ {
		field := relayType.Field(index)
		if field.Type.Kind() == reflect.Map {
			t.Fatalf("sub2apiRelay contains direct map field %s of type %s", field.Name, field.Type)
		}
	}
}

func TestUsersTrendBatchRejectsDuplicateUserDate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/admin/dashboard/trend" {
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": []any{}})
			return
		}
		row := map[string]any{"date": "2026-07-01", "user_id": 101, "tokens": 1, "actual_cost": 1}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": true, "data": map[string]any{"trend": []map[string]any{row, row}},
		})
	}))
	t.Cleanup(server.Close)
	provider := &sub2apiRelay{client: server.Client(), adminURL: server.URL, apiKey: "test-admin-key", logger: zap.NewNop()}
	if _, err := provider.GetUsageTrendForUsers(context.Background(), []int64{101}, TeamMemberTrendParams{}); err == nil {
		t.Fatal("GetUsageTrendForUsers() error = nil, want duplicate user/date rejection")
	}
}

func TestUsersTrendBatchRejectsInvalidRequestedUserID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": []any{}})
	}))
	t.Cleanup(server.Close)
	provider := &sub2apiRelay{client: server.Client(), adminURL: server.URL, apiKey: "test-admin-key", logger: zap.NewNop()}
	if _, err := provider.GetUsageTrendForUsers(context.Background(), []int64{101, 0}, TeamMemberTrendParams{}); err == nil {
		t.Fatal("GetUsageTrendForUsers() error = nil, want invalid requested user ID rejection")
	}
}
