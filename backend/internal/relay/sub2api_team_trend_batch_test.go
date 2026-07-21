package relay

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"

	"go.uber.org/zap"
)

func paddedTrendJSONBody(t *testing.T, raw string, size int) []byte {
	t.Helper()
	if len(raw) > size {
		t.Fatalf("JSON fixture size = %d, exceeds requested size %d", len(raw), size)
	}
	body := make([]byte, size)
	copy(body, raw)
	for index := len(raw); index < len(body); index++ {
		body[index] = ' '
	}
	return body
}

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
				"limit":       {"5000"},
			}
			if !reflect.DeepEqual(r.URL.Query(), wantQuery) {
				t.Errorf("query = %#v, want %#v", r.URL.Query(), wantQuery)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"success": true,
				"data": map[string]any{
					"start_date": "2026-07-01", "end_date": "2026-07-20", "granularity": "day",
					"trend": []map[string]any{
						{"date": "2026-07-01", "user_id": 101, "tokens": 11, "actual_cost": 1.25},
						{"date": "2026-07-01", "user_id": 999, "tokens": 99, "actual_cost": 9.99},
						{"date": "2026-07-02", "user_id": 101, "tokens": 22, "actual_cost": 2.25},
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
		t.Fatalf("authorized user 101 points = %#v, want source-ordered aggregate rows", got[101])
	}
	if points, exists := got[102]; !exists || len(points) != 0 {
		t.Fatalf("zero-usage authorized user 102 = %#v, exists %v, want explicit empty trend", points, exists)
	}
}

func TestProviderWideTeamTrendBatchReturnsRawRowsAndExactMetadata(t *testing.T) {
	response := []byte(`{"success":true,"data":{"trend":[{"date":"2026-07-01","user_id":101,"tokens":11,"actual_cost":1.25},{"date":"2026-07-01","user_id":999,"actual_cost":9.99},{"date":"2026-07-02","user_id":101,"tokens":22,"actual_cost":2.25}],"start_date":"2026-07-01","end_date":"2026-07-02","granularity":"day"}}`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wantQuery := url.Values{
			"start_date": {"2026-07-01"}, "end_date": {"2026-07-02"},
			"granularity": {"day"}, "timezone": {"Asia/Shanghai"}, "limit": {"5000"},
		}
		if !reflect.DeepEqual(r.URL.Query(), wantQuery) {
			t.Fatalf("query = %#v, want %#v", r.URL.Query(), wantQuery)
		}
		_, _ = w.Write(response)
	}))
	t.Cleanup(server.Close)
	provider := &sub2apiRelay{client: server.Client(), adminURL: server.URL, apiKey: "test-admin-key", logger: zap.NewNop()}

	wide, ok := any(provider).(ProviderWideTeamTrendProvider)
	if !ok {
		t.Fatal("provider does not implement ProviderWideTeamTrendProvider")
	}
	got, err := wide.GetProviderUsageTrend(context.Background(), TeamMemberTrendParams{
		StartDate: " 2026-07-01 ", EndDate: "2026-07-02", Granularity: " day ", Timezone: " Asia/Shanghai ",
	}, 5000)
	if err != nil {
		t.Fatalf("GetProviderUsageTrend() error = %v", err)
	}
	if got.ResponseBytes != int64(len(response)) || got.PointCount != 3 || got.UniqueUserCount != 2 || !got.Complete {
		t.Fatalf("metadata = bytes:%d points:%d users:%d complete:%v", got.ResponseBytes, got.PointCount, got.UniqueUserCount, got.Complete)
	}
	wantCoverage := TeamMemberTrendParams{
		StartDate: "2026-07-01", EndDate: "2026-07-02", Granularity: "day", Timezone: "Asia/Shanghai",
	}
	if !reflect.DeepEqual(got.Coverage, wantCoverage) {
		t.Fatalf("coverage = %#v, want %#v", got.Coverage, wantCoverage)
	}
	if len(got.Points) != 3 || got.Points[0].UserID != 101 || got.Points[1].UserID != 999 || got.Points[2].Date != "2026-07-02" {
		t.Fatalf("raw provider-wide points = %#v, want all source-ordered rows", got.Points)
	}
	if got.Points[1].TotalTokens != nil {
		t.Fatalf("unknown tokens = %#v, want nil", got.Points[1].TotalTokens)
	}
}

func TestProviderWideTeamTrendBatchRejectsInvalidRowsBeforeFiltering(t *testing.T) {
	tests := []struct {
		name   string
		params TeamMemberTrendParams
		data   map[string]any
	}{
		{
			name:   "negative cost on unrelated user",
			params: TeamMemberTrendParams{StartDate: "2026-07-01", EndDate: "2026-07-01", Granularity: "day", Timezone: "UTC"},
			data: map[string]any{
				"start_date": "2026-07-01", "end_date": "2026-07-01", "granularity": "day",
				"trend": []map[string]any{{"date": "2026-07-01", "user_id": 999, "actual_cost": -1}},
			},
		},
		{
			name:   "negative optional tokens on unrelated user",
			params: TeamMemberTrendParams{StartDate: "2026-07-01", EndDate: "2026-07-01", Granularity: "day", Timezone: "UTC"},
			data: map[string]any{
				"start_date": "2026-07-01", "end_date": "2026-07-01", "granularity": "day",
				"trend": []map[string]any{{"date": "2026-07-01", "user_id": 999, "tokens": -1, "actual_cost": 1}},
			},
		},
		{
			name:   "out-of-order labels",
			params: TeamMemberTrendParams{StartDate: "2026-07-01", EndDate: "2026-07-02", Granularity: "day", Timezone: "UTC"},
			data: map[string]any{
				"start_date": "2026-07-01", "end_date": "2026-07-02", "granularity": "day",
				"trend": []map[string]any{
					{"date": "2026-07-02", "user_id": 101, "actual_cost": 1},
					{"date": "2026-07-01", "user_id": 101, "actual_cost": 1},
				},
			},
		},
		{
			name:   "invalid day label",
			params: TeamMemberTrendParams{StartDate: "2026-07-01", EndDate: "2026-07-01", Granularity: "day", Timezone: "UTC"},
			data: map[string]any{
				"start_date": "2026-07-01", "end_date": "2026-07-01", "granularity": "day",
				"trend": []map[string]any{{"date": "2026-07-01 00:00", "user_id": 101, "actual_cost": 1}},
			},
		},
		{
			name:   "mismatched coverage",
			params: TeamMemberTrendParams{StartDate: "2026-07-01", EndDate: "2026-07-01", Granularity: "day", Timezone: "UTC"},
			data: map[string]any{
				"start_date": "2026-06-30", "end_date": "2026-07-01", "granularity": "day", "trend": []map[string]any{},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": test.data})
			}))
			t.Cleanup(server.Close)
			provider := &sub2apiRelay{client: server.Client(), adminURL: server.URL, apiKey: "test-admin-key", logger: zap.NewNop()}
			if _, err := provider.GetProviderUsageTrend(context.Background(), test.params, 5000); err == nil {
				t.Fatalf("GetProviderUsageTrend() error = nil, want %s rejection", test.name)
			}
		})
	}
}

func TestProviderWideTeamTrendBatchRejectsExact5000Users(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		rows := make([]map[string]any, 5000)
		for index := range rows {
			rows[index] = map[string]any{"date": "2026-07-01", "user_id": index + 1, "actual_cost": 1}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"data": map[string]any{
				"trend": rows, "start_date": "2026-07-01", "end_date": "2026-07-01", "granularity": "day",
			},
		})
	}))
	t.Cleanup(server.Close)
	provider := &sub2apiRelay{client: server.Client(), adminURL: server.URL, apiKey: "test-admin-key", logger: zap.NewNop()}
	_, err := provider.GetProviderUsageTrend(context.Background(), TeamMemberTrendParams{
		StartDate: "2026-07-01", EndDate: "2026-07-01", Granularity: "day", Timezone: "UTC",
	}, 5000)
	if err == nil {
		t.Fatal("GetProviderUsageTrend() error = nil, want exact-5000 rejection")
	}
	requireProviderWideRejectionKind(t, err, ProviderSourceRejectionRawTrendLimit)
}

func TestProviderWideTeamTrendBatchEnforcesBodyLimitBeforeDecode(t *testing.T) {
	const trendBodyLimit = 32 << 20
	const validTrend = `{"success":true,"data":{"trend":[],"start_date":"","end_date":"","granularity":""}}`
	for _, test := range []struct {
		name    string
		size    int
		wantErr bool
	}{
		{name: "below 32 MiB", size: trendBodyLimit - 1},
		{name: "exactly 32 MiB", size: trendBodyLimit, wantErr: true},
		{name: "over 32 MiB", size: trendBodyLimit + 1, wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			body := paddedTrendJSONBody(t, validTrend, test.size)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write(body)
			}))
			t.Cleanup(server.Close)
			provider := &sub2apiRelay{client: server.Client(), adminURL: server.URL, apiKey: "test-admin-key", logger: zap.NewNop()}
			got, err := provider.GetProviderUsageTrend(context.Background(), TeamMemberTrendParams{}, 5000)
			if test.wantErr {
				if err == nil || !strings.Contains(err.Error(), "33554432-byte limit") {
					t.Fatalf("GetProviderUsageTrend() error = %v, want pre-decode body limit rejection", err)
				}
				requireProviderWideRejectionKind(t, err, ProviderSourceRejectionRawTrendLimit)
				return
			}
			if err != nil {
				t.Fatalf("GetProviderUsageTrend() error = %v, want below-limit acceptance", err)
			}
			if got.ResponseBytes != int64(test.size) || got.PointCount != 0 {
				t.Fatalf("trend result = %#v, want accepted %d-byte empty response", got, test.size)
			}
		})
	}
}

func TestProviderWideTeamTrendHTTPRejectsTypedPointCoverageAndCompleteness(t *testing.T) {
	params := TeamMemberTrendParams{StartDate: "2026-07-01", EndDate: "2026-07-01", Granularity: "day", Timezone: "UTC"}
	tests := []struct {
		name       string
		limit      int
		pointLimit int
		data       map[string]any
		want       ProviderSourceRejectionKind
	}{
		{
			name: "point exact limit", limit: 5000, pointLimit: 2, want: ProviderSourceRejectionRawTrendLimit,
			data: map[string]any{"trend": []map[string]any{
				{"date": "2026-07-01", "user_id": 101, "actual_cost": 1},
				{"date": "2026-07-01", "user_id": 102, "actual_cost": 1},
			}, "start_date": "2026-07-01", "end_date": "2026-07-01", "granularity": "day"},
		},
		{
			name: "coverage", limit: 5000, want: ProviderSourceRejectionRawTrendCoverage,
			data: map[string]any{"trend": []map[string]any{}, "start_date": "2026-06-30", "end_date": "2026-07-01", "granularity": "day"},
		},
		{
			name: "completeness", limit: 2, want: ProviderSourceRejectionRawTrendCompleteness,
			data: map[string]any{"trend": []map[string]any{
				{"date": "2026-07-01", "user_id": 101, "actual_cost": 1},
				{"date": "2026-07-01", "user_id": 102, "actual_cost": 1},
			}, "start_date": "2026-07-01", "end_date": "2026-07-01", "granularity": "day"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": test.data})
			}))
			t.Cleanup(server.Close)
			provider := &sub2apiRelay{
				client: server.Client(), adminURL: server.URL, apiKey: "test-admin-key", logger: zap.NewNop(),
				providerWideTrendPointLimit: test.pointLimit,
			}
			_, err := provider.GetProviderUsageTrend(context.Background(), params, test.limit)
			if err == nil {
				t.Fatalf("GetProviderUsageTrend() error = nil, want %s rejection", test.want)
			}
			requireProviderWideRejectionKind(t, err, test.want)
		})
	}
}

func requireProviderWideRejectionKind(t *testing.T, err error, want ProviderSourceRejectionKind) {
	t.Helper()
	got, ok := ProviderSourceRejectionKindOf(err)
	if !ok || got != want {
		t.Fatalf("ProviderSourceRejectionKindOf(%v) = %q/%v, want %q/true", err, got, ok, want)
	}
}

func TestProviderWideTeamTrendDecodedPointCountBoundaries(t *testing.T) {
	for _, test := range []struct {
		name    string
		count   int
		wantErr bool
	}{
		{name: "below limit", count: teamTrendBatchPointLimit - 1},
		{name: "exactly limit", count: teamTrendBatchPointLimit, wantErr: true},
		{name: "over limit", count: teamTrendBatchPointLimit + 1, wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := validateProviderWideTrendPointCount(make([]teamTrendBatchPoint, test.count))
			if (err != nil) != test.wantErr {
				t.Fatalf("validateProviderWideTrendPointCount(%d) error = %v, wantErr %v", test.count, err, test.wantErr)
			}
		})
	}
}

func TestProviderWideTeamTrendPointRejectsNonFiniteCost(t *testing.T) {
	for _, test := range []struct {
		name string
		cost float64
	}{
		{name: "NaN", cost: math.NaN()},
		{name: "positive infinity", cost: math.Inf(1)},
		{name: "negative infinity", cost: math.Inf(-1)},
	} {
		t.Run(test.name, func(t *testing.T) {
			cost := test.cost
			err := validateProviderWideTrendPoint(teamTrendBatchPoint{
				Date: "2026-07-01", UserID: 101, ActualCost: &cost,
			}, 0, "day")
			if err == nil {
				t.Fatalf("validateProviderWideTrendPoint() error = nil, want %s rejection", test.name)
			}
		})
	}
}

func TestProviderWideCurrentStatRejectsNonFiniteCosts(t *testing.T) {
	for _, test := range []struct {
		name string
		stat TeamUserUsageStats
	}{
		{name: "today NaN", stat: TeamUserUsageStats{TodayActualCost: math.NaN()}},
		{name: "today positive infinity", stat: TeamUserUsageStats{TodayActualCost: math.Inf(1)}},
		{name: "today negative infinity", stat: TeamUserUsageStats{TodayActualCost: math.Inf(-1)}},
		{name: "total NaN", stat: TeamUserUsageStats{TotalActualCost: math.NaN()}},
		{name: "total positive infinity", stat: TeamUserUsageStats{TotalActualCost: math.Inf(1)}},
		{name: "total negative infinity", stat: TeamUserUsageStats{TotalActualCost: math.Inf(-1)}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := validateProviderCurrentStat(test.stat); err == nil {
				t.Fatalf("validateProviderCurrentStat() error = nil, want %s rejection", test.name)
			}
		})
	}
}

func TestProviderWideTeamTrendDoesNotExposeUpstreamIdentityMessages(t *testing.T) {
	const upstreamMessage = "alice@example.com secret response text"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"success": false, "message": upstreamMessage})
	}))
	t.Cleanup(server.Close)
	provider := &sub2apiRelay{client: server.Client(), adminURL: server.URL, apiKey: "test-admin-key", logger: zap.NewNop()}
	_, err := provider.GetProviderUsageTrend(context.Background(), TeamMemberTrendParams{}, 5000)
	if err == nil {
		t.Fatal("GetProviderUsageTrend() error = nil, want failed envelope rejection")
	}
	if strings.Contains(err.Error(), upstreamMessage) || strings.Contains(err.Error(), "alice@example.com") {
		t.Fatalf("GetProviderUsageTrend() error exposed upstream identity text: %v", err)
	}
}

func TestProviderWideResultTypesContainNoIdentityOrRawBodyFields(t *testing.T) {
	for _, resultType := range []reflect.Type{
		reflect.TypeOf(ProviderDirectoryResult{}),
		reflect.TypeOf(ProviderCurrentStatsResult{}),
		reflect.TypeOf(ProviderWideTrendResult{}),
		reflect.TypeOf(ProviderWideTrendPoint{}),
	} {
		for index := 0; index < resultType.NumField(); index++ {
			name := strings.ToLower(resultType.Field(index).Name)
			for _, forbidden := range []string{"email", "username", "body"} {
				if strings.Contains(name, forbidden) {
					t.Fatalf("provider-wide result %s exposes forbidden field %s", resultType.Name(), resultType.Field(index).Name)
				}
			}
		}
	}
}

func TestUsersTrendBatchRejectsPossiblyTruncatedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/admin/dashboard/trend" {
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": []any{}})
			return
		}
		rows := make([]map[string]any, 5000)
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

func TestUsersTrendBatchPreservesInvalidCredentialsClassification(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		response   map[string]any
	}{
		{name: "http unauthorized", statusCode: http.StatusUnauthorized, response: map[string]any{"success": false}},
		{name: "http forbidden", statusCode: http.StatusForbidden, response: map[string]any{"success": false}},
		{name: "envelope unauthorized", statusCode: http.StatusOK, response: map[string]any{"code": http.StatusUnauthorized}},
		{name: "envelope forbidden", statusCode: http.StatusOK, response: map[string]any{"code": http.StatusForbidden}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(test.statusCode)
				_ = json.NewEncoder(w).Encode(test.response)
			}))
			t.Cleanup(server.Close)
			provider := &sub2apiRelay{client: server.Client(), adminURL: server.URL, apiKey: "test-admin-key", logger: zap.NewNop()}

			_, err := provider.GetUsageTrendForUsers(context.Background(), []int64{101}, TeamMemberTrendParams{})
			if !errors.Is(err, ErrInvalidCredentials) {
				t.Fatalf("GetUsageTrendForUsers() error = %v, want ErrInvalidCredentials", err)
			}
		})
	}
}

func TestUsersTrendBatchRejectsMalformedAggregateRows(t *testing.T) {
	tests := []struct {
		name string
		row  map[string]any
	}{
		{name: "non-positive user", row: map[string]any{"date": "2026-07-01", "user_id": 0, "tokens": 1, "actual_cost": 1}},
		{name: "blank date", row: map[string]any{"date": " ", "user_id": 101, "tokens": 1, "actual_cost": 1}},
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

func TestUsersTrendBatchPreservesUnknownTokens(t *testing.T) {
	tests := []struct {
		name string
		row  map[string]any
	}{
		{name: "omitted", row: map[string]any{"date": "2026-07-01", "user_id": 101, "actual_cost": 1.25}},
		{name: "null", row: map[string]any{"date": "2026-07-01", "user_id": 101, "tokens": nil, "actual_cost": 1.25}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"success": true, "data": map[string]any{"trend": []map[string]any{test.row}},
				})
			}))
			t.Cleanup(server.Close)
			provider := &sub2apiRelay{client: server.Client(), adminURL: server.URL, apiKey: "test-admin-key", logger: zap.NewNop()}

			got, err := provider.GetUsageTrendForUsers(context.Background(), []int64{101}, TeamMemberTrendParams{})
			if err != nil {
				t.Fatalf("GetUsageTrendForUsers() error = %v", err)
			}
			if len(got[101]) != 1 {
				t.Fatalf("user 101 points = %#v, want one point", got[101])
			}
			if got[101][0].TotalTokens != nil {
				t.Fatalf("user 101 total tokens = %#v, want nil for unknown tokens", got[101][0].TotalTokens)
			}
		})
	}
}

func TestNoPodTrendCacheOnSub2APIRelay(t *testing.T) {
	var mu sync.Mutex
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/admin/dashboard/users-trend" {
			http.NotFound(w, r)
			return
		}
		mu.Lock()
		requestCount++
		tokens := requestCount
		mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"data": map[string]any{"trend": []map[string]any{
				{"date": "2026-07-01", "user_id": 101, "tokens": tokens, "actual_cost": 1},
			}},
		})
	}))
	t.Cleanup(server.Close)
	provider := &sub2apiRelay{client: server.Client(), adminURL: server.URL, apiKey: "test-admin-key", logger: zap.NewNop()}

	first, err := provider.GetUsageTrendForUsers(context.Background(), []int64{101}, TeamMemberTrendParams{})
	if err != nil {
		t.Fatalf("first GetUsageTrendForUsers() error = %v", err)
	}
	second, err := provider.GetUsageTrendForUsers(context.Background(), []int64{101}, TeamMemberTrendParams{})
	if err != nil {
		t.Fatalf("second GetUsageTrendForUsers() error = %v", err)
	}
	mu.Lock()
	gotRequestCount := requestCount
	mu.Unlock()
	if gotRequestCount != 2 {
		t.Fatalf("users-trend request count = %d, want 2", gotRequestCount)
	}
	if first[101][0].TotalTokens == nil || *first[101][0].TotalTokens != 1 {
		t.Fatalf("first total tokens = %#v, want 1", first[101][0].TotalTokens)
	}
	if second[101][0].TotalTokens == nil || *second[101][0].TotalTokens != 2 {
		t.Fatalf("second total tokens = %#v, want 2 from second origin response", second[101][0].TotalTokens)
	}

	relayType := reflect.TypeOf(sub2apiRelay{})
	if field, exists := relayType.FieldByName("teamTrends"); exists {
		t.Fatalf("sub2apiRelay still contains completed trend cache field %s of type %s", field.Name, field.Type)
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
