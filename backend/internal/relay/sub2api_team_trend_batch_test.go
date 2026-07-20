package relay

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"go.uber.org/zap"
)

func TestTeamTrendBatchLimit(t *testing.T) {
	tests := []struct {
		totalRequested int
		want           int
	}{
		{totalRequested: 0, want: 500},
		{totalRequested: 2, want: 500},
		{totalRequested: 235, want: 500},
		{totalRequested: 400, want: 650},
		{totalRequested: 5000, want: 5000},
		{totalRequested: 9000, want: 5000},
	}

	for _, test := range tests {
		if got := teamTrendBatchLimit(test.totalRequested); got != test.want {
			t.Errorf("teamTrendBatchLimit(%d) = %d, want %d", test.totalRequested, got, test.want)
		}
	}
}

func TestTeamTrendBatchMapsFiltersAndSortsResponse(t *testing.T) {
	wantQuery := url.Values{
		"start_date":  {"2026-07-01"},
		"end_date":    {"2026-07-20"},
		"granularity": {"day"},
		"timezone":    {"Asia/Shanghai"},
		"limit":       {"500"},
	}
	provider := newTeamTrendBatchTestRelay(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/api/v1/admin/dashboard/users-trend" {
			t.Errorf("path = %q, want users-trend path", r.URL.Path)
		}
		if !reflect.DeepEqual(r.URL.Query(), wantQuery) {
			t.Errorf("query = %#v, want %#v", r.URL.Query(), wantQuery)
		}
		if got := r.Header.Get("X-API-Key"); got != "test-admin-key" {
			t.Errorf("X-API-Key = %q, want test-admin-key", got)
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"data": map[string]any{
				"trend": []map[string]any{
					{"date": "2026-07-02", "user_id": 101, "tokens": 22, "actual_cost": 2.25, "email": "ignored@example.com"},
					{"date": "2026-07-01", "user_id": 999, "tokens": 99, "actual_cost": 9.99},
					{"date": "2026-07-01", "user_id": 102, "tokens": 33, "actual_cost": 3.50},
					{"date": "2026-07-01", "user_id": 101, "tokens": 11, "actual_cost": 1.25},
				},
			},
		}); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))

	result, err := provider.getTeamTrendBatch(context.Background(), []int64{101, 102}, TeamMemberTrendParams{
		StartDate: "2026-07-01", EndDate: "2026-07-20", Granularity: "day", Timezone: "Asia/Shanghai",
	}, 500)
	if err != nil {
		t.Fatalf("getTeamTrendBatch() error = %v", err)
	}
	if result.UniqueUserCount != 3 || !result.Complete {
		t.Fatalf("unique count/complete = %d/%v, want 3/true", result.UniqueUserCount, result.Complete)
	}
	if len(result.PointsByUser) != 2 {
		t.Fatalf("PointsByUser keys = %v, want only requested users 101 and 102", result.PointsByUser)
	}
	if _, ok := result.PointsByUser[999]; ok {
		t.Fatal("PointsByUser contains out-of-scope user 999")
	}
	points101 := result.PointsByUser[101]
	if len(points101) != 2 || points101[0].Date != "2026-07-01" || points101[1].Date != "2026-07-02" {
		t.Fatalf("user 101 points = %#v, want date-sorted points", points101)
	}
	if points101[0].ActualCost != 1.25 || points101[1].ActualCost != 2.25 {
		t.Fatalf("user 101 costs = %#v", points101)
	}
	if points101[0].TotalTokens == nil || *points101[0].TotalTokens != 11 ||
		points101[1].TotalTokens == nil || *points101[1].TotalTokens != 22 {
		t.Fatalf("user 101 token pointers = %#v", points101)
	}
	if points101[0].TotalTokens == points101[1].TotalTokens {
		t.Fatal("user 101 token pointers share storage")
	}
	*points101[0].TotalTokens = 111
	if *points101[1].TotalTokens != 22 {
		t.Fatalf("mutating first token pointer changed second to %d", *points101[1].TotalTokens)
	}
	points102 := result.PointsByUser[102]
	if len(points102) != 1 || points102[0].TotalTokens == nil || *points102[0].TotalTokens != 33 {
		t.Fatalf("user 102 points = %#v", points102)
	}
}

func TestTeamTrendBatchClassifiesCompletenessBeforeFiltering(t *testing.T) {
	tests := []struct {
		name         string
		limit        int
		wantComplete bool
	}{
		{name: "exactly the limit is possibly truncated", limit: 2, wantComplete: false},
		{name: "fewer than the limit is complete", limit: 3, wantComplete: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			provider := newTeamTrendBatchResponseRelay(t, http.StatusOK, teamTrendBatchResponseJSON(t, []map[string]any{
				{"date": "2026-07-01", "user_id": 101, "tokens": 11, "actual_cost": 1.25},
				{"date": "2026-07-01", "user_id": 999, "tokens": 99, "actual_cost": 9.99},
			}))
			result, err := provider.getTeamTrendBatch(context.Background(), []int64{101}, TeamMemberTrendParams{}, test.limit)
			if err != nil {
				t.Fatalf("getTeamTrendBatch() error = %v", err)
			}
			if result.UniqueUserCount != 2 || result.Complete != test.wantComplete {
				t.Fatalf("unique count/complete = %d/%v, want 2/%v", result.UniqueUserCount, result.Complete, test.wantComplete)
			}
			if len(result.PointsByUser) != 1 || len(result.PointsByUser[101]) != 1 {
				t.Fatalf("PointsByUser = %#v, want only requested user 101", result.PointsByUser)
			}
			if _, ok := result.PointsByUser[999]; ok {
				t.Fatal("PointsByUser contains out-of-scope user 999")
			}
		})
	}
}

func TestTeamTrendBatchRejectsInvalidRowsAndCardinality(t *testing.T) {
	tests := []struct {
		name  string
		body  string
		limit int
	}{
		{
			name:  "non-positive user ID",
			body:  `{"success":true,"data":{"trend":[{"date":"2026-07-01","user_id":0,"tokens":1,"actual_cost":1}]}}`,
			limit: 500,
		},
		{
			name:  "blank date",
			body:  `{"success":true,"data":{"trend":[{"date":"  ","user_id":101,"tokens":1,"actual_cost":1}]}}`,
			limit: 500,
		},
		{
			name:  "negative tokens",
			body:  `{"success":true,"data":{"trend":[{"date":"2026-07-01","user_id":101,"tokens":-1,"actual_cost":1}]}}`,
			limit: 500,
		},
		{
			name:  "non-finite actual cost",
			body:  `{"success":true,"data":{"trend":[{"date":"2026-07-01","user_id":101,"tokens":1,"actual_cost":1e1000}]}}`,
			limit: 500,
		},
		{
			name:  "duplicate out-of-scope user date",
			body:  `{"success":true,"data":{"trend":[{"date":"2026-07-01","user_id":999,"tokens":1,"actual_cost":1},{"date":"2026-07-01","user_id":999,"tokens":2,"actual_cost":2}]}}`,
			limit: 500,
		},
		{
			name:  "more unique users than limit",
			body:  `{"success":true,"data":{"trend":[{"date":"2026-07-01","user_id":101,"tokens":1,"actual_cost":1},{"date":"2026-07-01","user_id":102,"tokens":2,"actual_cost":2}]}}`,
			limit: 1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			provider := newTeamTrendBatchResponseRelay(t, http.StatusOK, test.body)
			if _, err := provider.getTeamTrendBatch(context.Background(), []int64{101}, TeamMemberTrendParams{}, test.limit); err == nil {
				t.Fatal("getTeamTrendBatch() error = nil, want rejection")
			}
		})
	}
}

func TestTeamTrendBatchRejectsStatusEnvelopeAndJSONFailures(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
	}{
		{name: "non-200 status", statusCode: http.StatusBadGateway, body: `{"success":false,"message":"upstream failed"}`},
		{name: "unsuccessful envelope", statusCode: http.StatusOK, body: `{"success":false,"message":"upstream failed","data":{"trend":[]}}`},
		{name: "missing success status", statusCode: http.StatusOK, body: `{"data":{"trend":[]}}`},
		{name: "malformed JSON", statusCode: http.StatusOK, body: `{"success":true,"data":`},
		{name: "malformed data shape", statusCode: http.StatusOK, body: `{"success":true,"data":[]}`},
		{name: "trailing JSON", statusCode: http.StatusOK, body: `{"success":true,"data":{"trend":[]}}{}`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			provider := newTeamTrendBatchResponseRelay(t, test.statusCode, test.body)
			if _, err := provider.getTeamTrendBatch(context.Background(), []int64{101}, TeamMemberTrendParams{}, 500); err == nil {
				t.Fatal("getTeamTrendBatch() error = nil, want rejection")
			}
		})
	}
}

func TestTeamTrendBatchRejectsBodyThatReachesBound(t *testing.T) {
	const responseLimit = int64(64 << 20)
	prefix := `{"success":true,"data":{"trend":[]}}`
	client := &http.Client{Transport: teamTrendBatchRoundTripFunc(func(*http.Request) (*http.Response, error) {
		body := io.MultiReader(strings.NewReader(prefix), &teamTrendBatchSizedReader{
			remaining: responseLimit - int64(len(prefix)),
		})
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(body),
		}, nil
	})}
	provider := &sub2apiRelay{client: client, adminURL: "http://relay.example", apiKey: "test-admin-key", logger: zap.NewNop()}

	if _, err := provider.getTeamTrendBatch(context.Background(), []int64{101}, TeamMemberTrendParams{}, 500); err == nil {
		t.Fatal("getTeamTrendBatch() error = nil, want body-bound rejection")
	}
}

func TestTeamTrendBatchCancellationAttemptsOnlyOneRequest(t *testing.T) {
	var attempts atomic.Int32
	entered := make(chan struct{})
	client := &http.Client{Transport: teamTrendBatchRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		attempts.Add(1)
		close(entered)
		<-r.Context().Done()
		return nil, r.Context().Err()
	})}
	provider := &sub2apiRelay{client: client, adminURL: "http://relay.example", apiKey: "test-admin-key", logger: zap.NewNop()}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := provider.getTeamTrendBatch(ctx, []int64{101}, TeamMemberTrendParams{}, 500)
		done <- err
	}()

	<-entered
	cancel()
	err := <-done
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("getTeamTrendBatch() error = %v, want context.Canceled", err)
	}
	if attempts.Load() != 1 {
		t.Fatalf("request attempts = %d, want 1", attempts.Load())
	}
}

func newTeamTrendBatchResponseRelay(t *testing.T, statusCode int, body string) *sub2apiRelay {
	t.Helper()
	return newTeamTrendBatchTestRelay(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		if _, err := io.WriteString(w, body); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
}

func teamTrendBatchResponseJSON(t *testing.T, rows []map[string]any) string {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"success": true,
		"data":    map[string]any{"trend": rows},
	})
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	return string(body)
}

type teamTrendBatchRoundTripFunc func(*http.Request) (*http.Response, error)

func (f teamTrendBatchRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

type teamTrendBatchSizedReader struct {
	remaining int64
}

func (r *teamTrendBatchSizedReader) Read(buffer []byte) (int, error) {
	if r.remaining == 0 {
		return 0, io.EOF
	}
	if int64(len(buffer)) > r.remaining {
		buffer = buffer[:r.remaining]
	}
	for index := range buffer {
		buffer[index] = ' '
	}
	r.remaining -= int64(len(buffer))
	return len(buffer), nil
}

func newTeamTrendBatchTestRelay(t *testing.T, handler http.Handler) *sub2apiRelay {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return &sub2apiRelay{
		client: server.Client(), adminURL: server.URL, baseURL: server.URL + "/v1",
		apiKey: "test-admin-key", model: "test-model", logger: zap.NewNop(),
	}
}
