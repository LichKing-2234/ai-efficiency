package relay

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/internal/readcache"
	"github.com/alicebob/miniredis/v2"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestSub2APITeamTrendRedisCacheFirstAndBatchFirst(t *testing.T) {
	server := miniredis.RunT(t)
	store := newTeamTrendRedisTestStore(t, server)
	params := testSub2APITeamTrendParams()

	var batchCalls atomic.Int64
	var individualCalls atomic.Int64
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/admin/dashboard/users-trend":
			batchCalls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"success": true,
				"data": map[string]any{"trend": []map[string]any{
					{"date": "2026-07-19", "user_id": 201, "tokens": 21, "actual_cost": 2.1},
					{"date": "2026-07-19", "user_id": 999, "tokens": 99, "actual_cost": 9.9},
				}},
			})
		case "/api/v1/admin/dashboard/trend":
			individualCalls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"success": true,
				"data": []map[string]any{{
					"date": "2026-07-19", "actual_cost": 1.5, "total_tokens": 15,
				}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(httpServer.Close)

	cache := newTeamTrendRedisTestCache(t, store, teamTrendRedisCacheOptions{
		Namespace: "team-trend-orchestration", ProviderID: 7, ProviderVersion: 3,
	})
	provider := &sub2apiRelay{
		client: httpServer.Client(), adminURL: httpServer.URL, baseURL: httpServer.URL + "/v1",
		apiKey: "test-admin-key", model: "test-model", logger: zap.NewNop(), teamTrends: cache,
	}

	t.Run("all hits make no HTTP requests", func(t *testing.T) {
		seed := map[int64][]UsageTrendPoint{
			101: {{Date: "2026-07-19", ActualCost: 1.01}},
			102: {{Date: "2026-07-19", ActualCost: 1.02}},
		}
		if err := cache.Write(context.Background(), seed, params, "test_seed"); err != nil {
			t.Fatalf("seed Redis: %v", err)
		}

		got, err := provider.GetUsageTrendForUsers(context.Background(), []int64{101, 102}, params)
		if err != nil {
			t.Fatalf("GetUsageTrendForUsers() error = %v", err)
		}
		if batchCalls.Load() != 0 || individualCalls.Load() != 0 {
			t.Fatalf("batch/individual HTTP calls = %d/%d, want 0/0", batchCalls.Load(), individualCalls.Load())
		}
		if got[101][0].ActualCost != 1.01 || got[102][0].ActualCost != 1.02 {
			t.Fatalf("cached results = %#v", got)
		}
	})

	t.Run("one miss uses one individual request", func(t *testing.T) {
		batchCalls.Store(0)
		individualCalls.Store(0)
		if err := cache.Write(context.Background(), map[int64][]UsageTrendPoint{
			111: {{Date: "2026-07-19", ActualCost: 1.11}},
		}, params, "test_seed"); err != nil {
			t.Fatalf("seed Redis: %v", err)
		}

		got, err := provider.GetUsageTrendForUsers(context.Background(), []int64{111, 112}, params)
		if err != nil {
			t.Fatalf("GetUsageTrendForUsers() error = %v", err)
		}
		if batchCalls.Load() != 0 || individualCalls.Load() != 1 {
			t.Fatalf("batch/individual HTTP calls = %d/%d, want 0/1", batchCalls.Load(), individualCalls.Load())
		}
		if got[111][0].ActualCost != 1.11 || got[112][0].ActualCost != 1.5 {
			t.Fatalf("mixed cache/origin results = %#v", got)
		}
	})

	t.Run("two misses use one complete batch and cache absent requested user", func(t *testing.T) {
		batchCalls.Store(0)
		individualCalls.Store(0)

		got, err := provider.GetUsageTrendForUsers(context.Background(), []int64{201, 202}, params)
		if err != nil {
			t.Fatalf("GetUsageTrendForUsers() error = %v", err)
		}
		if batchCalls.Load() != 1 || individualCalls.Load() != 0 {
			t.Fatalf("batch/individual HTTP calls = %d/%d, want 1/0", batchCalls.Load(), individualCalls.Load())
		}
		if len(got[201]) != 1 || got[201][0].ActualCost != 2.1 {
			t.Fatalf("trend[201] = %#v", got[201])
		}
		if points, ok := got[202]; !ok || len(points) != 0 {
			t.Fatalf("trend[202] = %#v, present=%v; want successful empty slice", points, ok)
		}

		got, err = provider.GetUsageTrendForUsers(context.Background(), []int64{201, 202}, params)
		if err != nil {
			t.Fatalf("second GetUsageTrendForUsers() error = %v", err)
		}
		if batchCalls.Load() != 1 || individualCalls.Load() != 0 {
			t.Fatalf("second call batch/individual HTTP calls = %d/%d, want unchanged 1/0", batchCalls.Load(), individualCalls.Load())
		}
		if len(got[201]) != 1 {
			t.Fatalf("second trend[201] = %#v", got[201])
		}
		if points, ok := got[202]; !ok || len(points) != 0 {
			t.Fatalf("second trend[202] = %#v, present=%v", points, ok)
		}
	})
}

func TestSub2APITeamTrendRedisPossibleTruncationFallsBackOnlyForAbsentUsers(t *testing.T) {
	server := miniredis.RunT(t)
	store := newTeamTrendRedisTestStore(t, server)
	metrics := &teamTrendRedisTestMetrics{}
	params := testSub2APITeamTrendParams()
	var batchCalls atomic.Int64
	var individualCalls atomic.Int64
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v1/admin/dashboard/users-trend" {
			batchCalls.Add(1)
			rows := make([]map[string]any, 0, 500)
			rows = append(rows, map[string]any{
				"date": "2026-07-19", "user_id": 301, "tokens": 31, "actual_cost": 3.01,
			})
			for index := 0; index < 499; index++ {
				rows = append(rows, map[string]any{
					"date": "2026-07-19", "user_id": int64(1000 + index), "tokens": index, "actual_cost": float64(index),
				})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"success": true, "data": map[string]any{"trend": rows},
			})
			return
		}
		individualCalls.Add(1)
		if got := r.URL.Query().Get("user_id"); got != "302" {
			t.Errorf("individual user_id = %q, want 302", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"data": []map[string]any{{
				"date": "2026-07-19", "actual_cost": 3.02, "total_tokens": 32,
			}},
		})
	}))
	t.Cleanup(httpServer.Close)
	provider := newSub2APITeamTrendTestProvider(t, httpServer, store, metrics, "truncation")

	got, err := provider.GetUsageTrendForUsers(context.Background(), []int64{301, 302}, params)
	if err != nil {
		t.Fatalf("GetUsageTrendForUsers() error = %v", err)
	}
	if batchCalls.Load() != 1 || individualCalls.Load() != 1 {
		t.Fatalf("batch/individual calls = %d/%d, want 1/1", batchCalls.Load(), individualCalls.Load())
	}
	if got[301][0].ActualCost != 3.01 || got[302][0].ActualCost != 3.02 {
		t.Fatalf("trends = %#v", got)
	}
	if metrics.count("possible_truncation") != 1 || metrics.count("batch_origin") != 1 || metrics.count("individual_fallback") != 1 {
		t.Fatalf("metrics = %v", metrics.outcomesSnapshot())
	}

	if _, err := provider.GetUsageTrendForUsers(context.Background(), []int64{301, 302}, params); err != nil {
		t.Fatalf("warm GetUsageTrendForUsers() error = %v", err)
	}
	if batchCalls.Load() != 1 || individualCalls.Load() != 1 {
		t.Fatalf("warm batch/individual calls = %d/%d, want unchanged 1/1", batchCalls.Load(), individualCalls.Load())
	}
}

func TestSub2APITeamTrendRedisBatchResponseFailuresFallBackToEveryMiss(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
	}{
		{name: "status 500", statusCode: http.StatusInternalServerError, body: `{"success":false}`},
		{name: "decode failure", statusCode: http.StatusOK, body: `{"success":true,"data":`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := miniredis.RunT(t)
			store := newTeamTrendRedisTestStore(t, server)
			var batchCalls atomic.Int64
			var individualCalls atomic.Int64
			httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/api/v1/admin/dashboard/users-trend" {
					batchCalls.Add(1)
					w.WriteHeader(test.statusCode)
					_, _ = io.WriteString(w, test.body)
					return
				}
				individualCalls.Add(1)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"success": true,
					"data":    []map[string]any{{"date": "2026-07-19", "actual_cost": 4.5, "total_tokens": 45}},
				})
			}))
			t.Cleanup(httpServer.Close)
			provider := newSub2APITeamTrendTestProvider(t, httpServer, store, nil, "batch-failure")

			got, err := provider.GetUsageTrendForUsers(context.Background(), []int64{401, 402}, testSub2APITeamTrendParams())
			if err != nil {
				t.Fatalf("GetUsageTrendForUsers() error = %v", err)
			}
			if batchCalls.Load() != 1 || individualCalls.Load() != 2 || len(got) != 2 {
				t.Fatalf("batch/individual/results = %d/%d/%#v, want 1/2/two", batchCalls.Load(), individualCalls.Load(), got)
			}
		})
	}
}

func TestSub2APITeamTrendRedisBatchTransportFailureFallsBackToEveryMiss(t *testing.T) {
	server := miniredis.RunT(t)
	store := newTeamTrendRedisTestStore(t, server)
	var batchCalls atomic.Int64
	var individualCalls atomic.Int64
	client := &http.Client{Transport: teamTrendBatchRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path == "/api/v1/admin/dashboard/users-trend" {
			batchCalls.Add(1)
			return nil, errors.New("synthetic batch transport failure")
		}
		individualCalls.Add(1)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(
				`{"success":true,"data":[{"date":"2026-07-19","actual_cost":4.5,"total_tokens":45}]}`,
			)),
		}, nil
	})}
	cache := newTeamTrendRedisTestCache(t, store, teamTrendRedisCacheOptions{
		Namespace: "transport-failure", ProviderID: 7, ProviderVersion: 3,
	})
	provider := &sub2apiRelay{
		client: client, adminURL: "http://relay.example", baseURL: "http://relay.example/v1",
		apiKey: "test-admin-key", model: "test-model", logger: zap.NewNop(), teamTrends: cache,
	}
	got, err := provider.GetUsageTrendForUsers(context.Background(), []int64{411, 412}, testSub2APITeamTrendParams())
	if err != nil {
		t.Fatalf("GetUsageTrendForUsers() error = %v", err)
	}
	if batchCalls.Load() != 1 || individualCalls.Load() != 2 || len(got) != 2 {
		t.Fatalf("batch/individual/results = %d/%d/%#v, want 1/2/two", batchCalls.Load(), individualCalls.Load(), got)
	}
}

func TestSub2APITeamTrendRedisCommandFailuresAreFailOpen(t *testing.T) {
	tests := []struct {
		name            string
		configure       func(*teamTrendOrchestrationFaultStore)
		preAcquireLease bool
		wantLeaseFailed bool
		wantError       bool
	}{
		{name: "MGET", configure: func(store *teamTrendOrchestrationFaultStore) { store.mgetErr = errors.New("synthetic MGET failure") }},
		{name: "pipeline", configure: func(store *teamTrendOrchestrationFaultStore) {
			store.setManyErr = errors.New("synthetic pipeline failure")
		}},
		{name: "SETNX", configure: func(store *teamTrendOrchestrationFaultStore) {
			store.acquireErr = errors.New("synthetic SETNX failure")
		}, wantLeaseFailed: true},
		{name: "PTTL", configure: func(store *teamTrendOrchestrationFaultStore) { store.ttlErr = errors.New("synthetic PTTL failure") }, preAcquireLease: true, wantLeaseFailed: true, wantError: true},
		{name: "release", configure: func(store *teamTrendOrchestrationFaultStore) {
			store.releaseErr = errors.New("synthetic release failure")
		}, wantLeaseFailed: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := miniredis.RunT(t)
			baseStore := newTeamTrendRedisTestStore(t, server)
			faultStore := &teamTrendOrchestrationFaultStore{MultiStore: baseStore}
			test.configure(faultStore)
			metrics := &teamTrendRedisTestMetrics{}
			options := teamTrendRedisCacheOptions{
				Namespace: "command-failure-" + strings.ToLower(test.name), ProviderID: 7, ProviderVersion: 3, Metrics: metrics,
			}
			if test.preAcquireLease {
				baseCache := newTeamTrendRedisTestCache(t, baseStore, options)
				if _, _, acquired, err := baseCache.TryAcquireBatchLease(context.Background(), []int64{421, 422}, testSub2APITeamTrendParams(), 500); err != nil || !acquired {
					t.Fatalf("pre-acquire lease = %v, %v", acquired, err)
				}
			}
			var batchCalls atomic.Int64
			var individualCalls atomic.Int64
			httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/api/v1/admin/dashboard/users-trend" {
					batchCalls.Add(1)
					_ = json.NewEncoder(w).Encode(map[string]any{
						"success": true,
						"data": map[string]any{"trend": []map[string]any{
							{"date": "2026-07-19", "user_id": 421, "tokens": 41, "actual_cost": 4.21},
							{"date": "2026-07-19", "user_id": 422, "tokens": 42, "actual_cost": 4.22},
						}},
					})
					return
				}
				individualCalls.Add(1)
				http.Error(w, "unexpected individual fallback", http.StatusInternalServerError)
			}))
			t.Cleanup(httpServer.Close)
			cache := newTeamTrendRedisTestCache(t, faultStore, options)
			provider := &sub2apiRelay{
				client: httpServer.Client(), adminURL: httpServer.URL, baseURL: httpServer.URL + "/v1",
				apiKey: "test-admin-key", model: "test-model", logger: zap.NewNop(), teamTrends: cache,
			}

			got, err := provider.GetUsageTrendForUsers(context.Background(), []int64{421, 422}, testSub2APITeamTrendParams())
			if err != nil {
				t.Fatalf("GetUsageTrendForUsers() error = %v", err)
			}
			if batchCalls.Load() != 1 || individualCalls.Load() != 0 || len(got) != 2 {
				t.Fatalf("batch/individual/results = %d/%d/%#v, want 1/0/two", batchCalls.Load(), individualCalls.Load(), got)
			}
			if test.wantLeaseFailed && metrics.count("lease_failed") == 0 {
				t.Fatalf("metrics = %v, want lease_failed", metrics.outcomesSnapshot())
			}
			if test.wantError && metrics.count("error") == 0 {
				t.Fatalf("metrics = %v, want error", metrics.outcomesSnapshot())
			}
		})
	}
}

func TestSub2APITeamTrendRedisCancellationWhileWaitingStartsNoOrigin(t *testing.T) {
	server := miniredis.RunT(t)
	store := newTeamTrendRedisTestStore(t, server)
	params := testSub2APITeamTrendParams()
	baseOptions := teamTrendRedisCacheOptions{Namespace: "wait-cancel", ProviderID: 7, ProviderVersion: 3}
	baseCache := newTeamTrendRedisTestCache(t, store, baseOptions)
	if _, _, acquired, err := baseCache.TryAcquireBatchLease(context.Background(), []int64{431, 432}, params, 500); err != nil || !acquired {
		t.Fatalf("pre-acquire lease = %v, %v", acquired, err)
	}
	sleepEntered := make(chan struct{})
	var sleepOnce sync.Once
	baseOptions.Sleep = func(ctx context.Context, _ time.Duration) error {
		sleepOnce.Do(func() { close(sleepEntered) })
		<-ctx.Done()
		return ctx.Err()
	}
	cache := newTeamTrendRedisTestCache(t, store, baseOptions)
	var origins atomic.Int64
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		origins.Add(1)
		http.Error(w, "unexpected origin", http.StatusInternalServerError)
	}))
	t.Cleanup(httpServer.Close)
	provider := &sub2apiRelay{
		client: httpServer.Client(), adminURL: httpServer.URL, baseURL: httpServer.URL + "/v1",
		apiKey: "test-admin-key", model: "test-model", logger: zap.NewNop(), teamTrends: cache,
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := provider.GetUsageTrendForUsers(ctx, []int64{431, 432}, params)
		done <- err
	}()
	<-sleepEntered
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("GetUsageTrendForUsers() error = %v, want context.Canceled", err)
	}
	if origins.Load() != 0 {
		t.Fatalf("origins = %d, want zero", origins.Load())
	}
}

func TestSub2APITeamTrendRedisWaiterReacquiresAfterLeaseDisappears(t *testing.T) {
	server := miniredis.RunT(t)
	store := newTeamTrendRedisTestStore(t, server)
	params := testSub2APITeamTrendParams()
	baseOptions := teamTrendRedisCacheOptions{Namespace: "wait-reacquire", ProviderID: 7, ProviderVersion: 3}
	baseCache := newTeamTrendRedisTestCache(t, store, baseOptions)
	leaseKey, token, acquired, err := baseCache.TryAcquireBatchLease(context.Background(), []int64{435, 436}, params, 500)
	if err != nil || !acquired {
		t.Fatalf("pre-acquire lease = %v, %v", acquired, err)
	}
	metrics := &teamTrendRedisTestMetrics{}
	var releaseOnce sync.Once
	baseOptions.Metrics = metrics
	baseOptions.Sleep = func(context.Context, time.Duration) error {
		releaseOnce.Do(func() { baseCache.ReleaseBatchLease(leaseKey, token) })
		return nil
	}
	cache := newTeamTrendRedisTestCache(t, store, baseOptions)
	var batchCalls atomic.Int64
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/admin/dashboard/users-trend" {
			http.Error(w, "unexpected individual origin", http.StatusInternalServerError)
			return
		}
		batchCalls.Add(1)
		writeSub2APITeamTrendBatchRows(t, w, []map[string]any{
			{"date": "2026-07-19", "user_id": 435, "tokens": 35, "actual_cost": 4.35},
			{"date": "2026-07-19", "user_id": 436, "tokens": 36, "actual_cost": 4.36},
		})
	}))
	t.Cleanup(httpServer.Close)
	provider := &sub2apiRelay{
		client: httpServer.Client(), adminURL: httpServer.URL, baseURL: httpServer.URL + "/v1",
		apiKey: "test-admin-key", model: "test-model", logger: zap.NewNop(), teamTrends: cache,
	}

	got, err := provider.GetUsageTrendForUsers(context.Background(), []int64{435, 436}, params)
	if err != nil {
		t.Fatalf("GetUsageTrendForUsers() error = %v", err)
	}
	if batchCalls.Load() != 1 || len(got) != 2 {
		t.Fatalf("batch/results = %d/%#v, want 1/two", batchCalls.Load(), got)
	}
	if metrics.count("lease_wait") != 1 || metrics.count("lease_acquired") != 1 ||
		metrics.count("lease_failed") != 0 || metrics.count("error") != 0 {
		t.Fatalf("metrics = %v, want one wait, one reacquire, no lease_failed or error", metrics.outcomesSnapshot())
	}
}

func TestSub2APITeamTrendRedisNearDeadlineWaiterSkipsSecondBatch(t *testing.T) {
	server := miniredis.RunT(t)
	store := newTeamTrendRedisTestStore(t, server)
	params := testSub2APITeamTrendParams()
	baseOptions := teamTrendRedisCacheOptions{Namespace: "wait-near-deadline", ProviderID: 7, ProviderVersion: 3}
	baseCache := newTeamTrendRedisTestCache(t, store, baseOptions)
	leaseKey, token, acquired, err := baseCache.TryAcquireBatchLease(context.Background(), []int64{437, 438}, params, 500)
	if err != nil || !acquired {
		t.Fatalf("pre-acquire lease = %v, %v", acquired, err)
	}
	metrics := &teamTrendRedisTestMetrics{}
	var releaseOnce sync.Once
	baseOptions.Metrics = metrics
	baseOptions.Sleep = func(context.Context, time.Duration) error {
		releaseOnce.Do(func() { baseCache.ReleaseBatchLease(leaseKey, token) })
		return nil
	}
	cache := newTeamTrendRedisTestCache(t, store, baseOptions)
	var batchCalls atomic.Int64
	var individualCalls atomic.Int64
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v1/admin/dashboard/users-trend" {
			batchCalls.Add(1)
			writeSub2APITeamTrendBatchRows(t, w, []map[string]any{
				{"date": "2026-07-19", "user_id": 437, "tokens": 37, "actual_cost": 4.37},
				{"date": "2026-07-19", "user_id": 438, "tokens": 38, "actual_cost": 4.38},
			})
			return
		}
		individualCalls.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"data":    []map[string]any{{"date": "2026-07-19", "actual_cost": 4.5, "total_tokens": 45}},
		})
	}))
	t.Cleanup(httpServer.Close)
	provider := &sub2apiRelay{
		client: httpServer.Client(), adminURL: httpServer.URL, baseURL: httpServer.URL + "/v1",
		apiKey: "test-admin-key", model: "test-model", logger: zap.NewNop(), teamTrends: cache,
	}
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(teamTrendBatchOriginTimeout-time.Second))
	defer cancel()

	got, err := provider.GetUsageTrendForUsers(ctx, []int64{437, 438}, params)
	if err != nil {
		t.Fatalf("GetUsageTrendForUsers() error = %v", err)
	}
	if batchCalls.Load() != 0 || individualCalls.Load() != 2 || len(got) != 2 {
		t.Fatalf("batch/individual/results = %d/%d/%#v, want 0/2/two", batchCalls.Load(), individualCalls.Load(), got)
	}
	if metrics.count("lease_wait") != 1 || metrics.count("lease_acquired") != 0 {
		t.Fatalf("metrics = %v, want initial lease wait without reacquire", metrics.outcomesSnapshot())
	}
}

func TestSub2APITeamTrendRedisNearDeadlineInitialMissesStillUseBatch(t *testing.T) {
	server := miniredis.RunT(t)
	store := newTeamTrendRedisTestStore(t, server)
	metrics := &teamTrendRedisTestMetrics{}
	cache := newTeamTrendRedisTestCache(t, store, teamTrendRedisCacheOptions{
		Namespace: "initial-near-deadline", ProviderID: 7, ProviderVersion: 3, Metrics: metrics,
	})
	var batchCalls atomic.Int64
	var individualCalls atomic.Int64
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/admin/dashboard/users-trend" {
			individualCalls.Add(1)
			http.Error(w, "unexpected individual origin", http.StatusInternalServerError)
			return
		}
		batchCalls.Add(1)
		writeSub2APITeamTrendBatchRows(t, w, []map[string]any{
			{"date": "2026-07-19", "user_id": 439, "tokens": 39, "actual_cost": 4.39},
			{"date": "2026-07-19", "user_id": 440, "tokens": 40, "actual_cost": 4.40},
		})
	}))
	t.Cleanup(httpServer.Close)
	provider := &sub2apiRelay{
		client: httpServer.Client(), adminURL: httpServer.URL, baseURL: httpServer.URL + "/v1",
		apiKey: "test-admin-key", model: "test-model", logger: zap.NewNop(), teamTrends: cache,
	}
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(teamTrendBatchOriginTimeout-time.Second))
	defer cancel()

	got, err := provider.GetUsageTrendForUsers(ctx, []int64{439, 440}, testSub2APITeamTrendParams())
	if err != nil {
		t.Fatalf("GetUsageTrendForUsers() error = %v", err)
	}
	if batchCalls.Load() != 1 || individualCalls.Load() != 0 || len(got) != 2 {
		t.Fatalf("batch/individual/results = %d/%d/%#v, want 1/0/two", batchCalls.Load(), individualCalls.Load(), got)
	}
	if metrics.count("lease_acquired") != 1 || metrics.count("batch_origin") != 1 {
		t.Fatalf("metrics = %v, want initial lease and batch origin", metrics.outcomesSnapshot())
	}
}

func TestSub2APITeamTrendRedisCancellationDuringBatchStartsNoFallback(t *testing.T) {
	batchStarted := make(chan struct{})
	var batchCalls atomic.Int64
	var individualCalls atomic.Int64
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/admin/dashboard/users-trend" {
			batchCalls.Add(1)
			close(batchStarted)
			<-r.Context().Done()
			return
		}
		individualCalls.Add(1)
		http.Error(w, "unexpected fallback", http.StatusInternalServerError)
	}))
	t.Cleanup(httpServer.Close)
	provider := &sub2apiRelay{
		client: httpServer.Client(), adminURL: httpServer.URL, baseURL: httpServer.URL + "/v1",
		apiKey: "test-admin-key", model: "test-model", logger: zap.NewNop(),
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := provider.GetUsageTrendForUsers(ctx, []int64{441, 442}, testSub2APITeamTrendParams())
		done <- err
	}()
	<-batchStarted
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("GetUsageTrendForUsers() error = %v, want context.Canceled", err)
	}
	if batchCalls.Load() != 1 || individualCalls.Load() != 0 {
		t.Fatalf("batch/individual calls = %d/%d, want 1/0", batchCalls.Load(), individualCalls.Load())
	}
}

func TestSub2APITeamTrendRedisReturnedValuesAreDefensiveClones(t *testing.T) {
	server := miniredis.RunT(t)
	store := newTeamTrendRedisTestStore(t, server)
	var batchCalls atomic.Int64
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/admin/dashboard/users-trend" {
			http.Error(w, "unexpected individual origin", http.StatusInternalServerError)
			return
		}
		batchCalls.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"data": map[string]any{"trend": []map[string]any{
				{"date": "2026-07-19", "user_id": 451, "tokens": 51, "actual_cost": 4.51},
				{"date": "2026-07-19", "user_id": 452, "tokens": 52, "actual_cost": 4.52},
			}},
		})
	}))
	t.Cleanup(httpServer.Close)
	provider := newSub2APITeamTrendTestProvider(t, httpServer, store, nil, "defensive-clone")

	got, err := provider.GetUsageTrendForUsers(context.Background(), []int64{451, 452}, testSub2APITeamTrendParams())
	if err != nil {
		t.Fatal(err)
	}
	got[451][0].ActualCost = 99
	*got[451][0].TotalTokens = 999
	delete(got, 452)
	got[999] = []UsageTrendPoint{{Date: "mutated", ActualCost: 999}}

	warm, err := provider.GetUsageTrendForUsers(context.Background(), []int64{451, 452}, testSub2APITeamTrendParams())
	if err != nil {
		t.Fatal(err)
	}
	if batchCalls.Load() != 1 {
		t.Fatalf("batch calls = %d, want 1", batchCalls.Load())
	}
	if len(warm) != 2 || warm[451][0].ActualCost != 4.51 || *warm[451][0].TotalTokens != 51 || warm[452][0].ActualCost != 4.52 {
		t.Fatalf("warm values mutated through caller: %#v", warm)
	}
}

func TestSub2APITeamTrendRedisNonPositiveIDsBypassRedisAndBatch(t *testing.T) {
	server := miniredis.RunT(t)
	store := newTeamTrendRedisTestStore(t, server)
	var batchCalls atomic.Int64
	var individualCalls atomic.Int64
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v1/admin/dashboard/users-trend" {
			batchCalls.Add(1)
			http.Error(w, "unexpected batch", http.StatusInternalServerError)
			return
		}
		individualCalls.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"data":    []map[string]any{{"date": "2026-07-19", "actual_cost": 1.0}},
		})
	}))
	t.Cleanup(httpServer.Close)
	provider := newSub2APITeamTrendTestProvider(t, httpServer, store, nil, "non-positive")

	got, err := provider.GetUsageTrendForUsers(context.Background(), []int64{0, -1, 0}, testSub2APITeamTrendParams())
	if err != nil {
		t.Fatalf("GetUsageTrendForUsers() error = %v", err)
	}
	if batchCalls.Load() != 0 || individualCalls.Load() != 2 || len(got) != 2 {
		t.Fatalf("batch/individual/results = %d/%d/%#v, want 0/2/two deduplicated IDs", batchCalls.Load(), individualCalls.Load(), got)
	}
	if keys := server.Keys(); len(keys) != 0 {
		t.Fatalf("Redis keys = %v, want none for non-positive IDs", keys)
	}
}

func TestSub2APITeamTrendRedisIndividualFailureReturnsNoPartialResult(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/admin/dashboard/users-trend", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "synthetic batch failure", http.StatusInternalServerError)
	})
	mux.HandleFunc("/api/v1/admin/dashboard/trend", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("user_id") == "472" {
			http.Error(w, "synthetic individual failure", http.StatusBadGateway)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"data":    []map[string]any{{"date": "2026-07-19", "actual_cost": 4.71}},
		})
	})
	httpServer := httptest.NewServer(mux)
	t.Cleanup(httpServer.Close)
	provider := &sub2apiRelay{
		client: httpServer.Client(), adminURL: httpServer.URL, baseURL: httpServer.URL + "/v1",
		apiKey: "test-admin-key", model: "test-model", logger: zap.NewNop(),
	}

	got, err := provider.GetUsageTrendForUsers(context.Background(), []int64{471, 472}, testSub2APITeamTrendParams())
	if err == nil {
		t.Fatal("GetUsageTrendForUsers() error = nil")
	}
	if got != nil {
		t.Fatalf("GetUsageTrendForUsers() result = %#v, want nil on fallback failure", got)
	}
}

func TestSub2APITeamTrendRedisFailOpenLogsOnlySafeFields(t *testing.T) {
	server := miniredis.RunT(t)
	baseStore := newTeamTrendRedisTestStore(t, server)
	faultStore := &teamTrendOrchestrationFaultStore{
		MultiStore: baseStore, mgetErr: errors.New("synthetic MGET failure for test IDs"),
	}
	core, logs := observer.New(zap.WarnLevel)
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/admin/dashboard/users-trend" {
			http.Error(w, "unexpected individual", http.StatusInternalServerError)
			return
		}
		writeSub2APITeamTrendBatchRows(t, w, []map[string]any{
			{"date": "2026-07-19", "user_id": 481, "tokens": 81, "actual_cost": 4.81},
			{"date": "2026-07-19", "user_id": 482, "tokens": 82, "actual_cost": 4.82},
		})
	}))
	t.Cleanup(httpServer.Close)
	cache := newTeamTrendRedisTestCache(t, faultStore, teamTrendRedisCacheOptions{
		Namespace: "safe-log", ProviderID: 7, ProviderVersion: 3,
	})
	provider := &sub2apiRelay{
		client: httpServer.Client(), adminURL: httpServer.URL, baseURL: httpServer.URL + "/v1",
		apiKey: "test-admin-key", model: "test-model", logger: zap.New(core), teamTrends: cache,
	}

	if _, err := provider.GetUsageTrendForUsers(context.Background(), []int64{481, 482}, testSub2APITeamTrendParams()); err != nil {
		t.Fatal(err)
	}
	if logs.Len() != 2 {
		t.Fatalf("safe log count = %d, want two cache read failures", logs.Len())
	}
	for _, entry := range logs.All() {
		fields := entry.ContextMap()
		want := map[string]any{"provider_id": int64(7), "user_count": int64(2), "error_class": "redis"}
		if !reflect.DeepEqual(fields, want) {
			t.Fatalf("log fields = %#v, want only %#v", fields, want)
		}
	}
}

func TestSub2APITeamTrendRedisCrossPodLeaseCollapsesOnlyIdenticalMissingSets(t *testing.T) {
	server := miniredis.RunT(t)
	storeA := newTeamTrendRedisTestStore(t, server)
	storeB := newTeamTrendRedisTestStore(t, server)
	metricsA := &teamTrendRedisTestMetrics{}
	metricsB := &teamTrendRedisTestMetrics{}
	optionsA := teamTrendRedisCacheOptions{
		Namespace: "cross-pod", ProviderID: 7, ProviderVersion: 3, Metrics: metricsA,
	}
	optionsB := teamTrendRedisCacheOptions{
		Namespace: "cross-pod", ProviderID: 7, ProviderVersion: 3, Metrics: metricsB,
	}
	cacheA := newTeamTrendRedisTestCache(t, storeA, optionsA)
	cacheB := newTeamTrendRedisTestCache(t, storeB, optionsB)

	var phase atomic.Int32
	var batchCalls atomic.Int64
	var individualCalls atomic.Int64
	firstBatchStarted := make(chan struct{})
	releaseFirstBatch := make(chan struct{})
	twoDistinctBatchesStarted := make(chan struct{})
	var firstOnce sync.Once
	var distinctOnce sync.Once
	var distinctBatchCalls atomic.Int32
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/api/v1/admin/dashboard/users-trend" {
			individualCalls.Add(1)
			http.Error(w, "unexpected individual origin", http.StatusInternalServerError)
			return
		}
		batchCalls.Add(1)
		if phase.Load() == 0 {
			firstOnce.Do(func() { close(firstBatchStarted) })
			select {
			case <-releaseFirstBatch:
			case <-r.Context().Done():
				return
			}
			writeSub2APITeamTrendBatchRows(t, w, []map[string]any{
				{"date": "2026-07-19", "user_id": 501, "tokens": 51, "actual_cost": 5.01},
				{"date": "2026-07-19", "user_id": 502, "tokens": 52, "actual_cost": 5.02},
			})
			return
		}

		if distinctBatchCalls.Add(1) == 2 {
			distinctOnce.Do(func() { close(twoDistinctBatchesStarted) })
		}
		select {
		case <-twoDistinctBatchesStarted:
		case <-r.Context().Done():
			return
		}
		writeSub2APITeamTrendBatchRows(t, w, []map[string]any{
			{"date": "2026-07-19", "user_id": 601, "tokens": 61, "actual_cost": 6.01},
			{"date": "2026-07-19", "user_id": 602, "tokens": 62, "actual_cost": 6.02},
			{"date": "2026-07-19", "user_id": 603, "tokens": 63, "actual_cost": 6.03},
			{"date": "2026-07-19", "user_id": 604, "tokens": 64, "actual_cost": 6.04},
		})
	}))
	t.Cleanup(httpServer.Close)
	providerA := &sub2apiRelay{
		client: httpServer.Client(), adminURL: httpServer.URL, baseURL: httpServer.URL + "/v1",
		apiKey: "test-admin-key", model: "test-model", logger: zap.NewNop(), teamTrends: cacheA,
	}
	providerB := &sub2apiRelay{
		client: httpServer.Client(), adminURL: httpServer.URL, baseURL: httpServer.URL + "/v1",
		apiKey: "test-admin-key", model: "test-model", logger: zap.NewNop(), teamTrends: cacheB,
	}
	type trendResult struct {
		values map[int64][]UsageTrendPoint
		err    error
	}

	identicalResults := make(chan trendResult, 2)
	go func() {
		values, err := providerA.GetUsageTrendForUsers(context.Background(), []int64{501, 502}, testSub2APITeamTrendParams())
		identicalResults <- trendResult{values: values, err: err}
	}()
	<-firstBatchStarted
	go func() {
		values, err := providerB.GetUsageTrendForUsers(context.Background(), []int64{501, 502}, testSub2APITeamTrendParams())
		identicalResults <- trendResult{values: values, err: err}
	}()
	waitForTeamTrendMetric(t, metricsB, "lease_wait", 1)
	close(releaseFirstBatch)
	first := <-identicalResults
	second := <-identicalResults
	if first.err != nil || second.err != nil {
		t.Fatalf("identical calls errors = %v / %v", first.err, second.err)
	}
	if batchCalls.Load() != 1 || individualCalls.Load() != 0 {
		t.Fatalf("identical calls batch/individual = %d/%d, want 1/0", batchCalls.Load(), individualCalls.Load())
	}
	if !reflect.DeepEqual(first.values, second.values) {
		t.Fatalf("independent cross-Pod results differ: %#v / %#v", first.values, second.values)
	}
	first.values[501][0].ActualCost = 99
	if second.values[501][0].ActualCost != 5.01 {
		t.Fatal("cross-Pod callers share result storage")
	}

	phase.Store(1)
	distinctResults := make(chan trendResult, 2)
	go func() {
		values, err := providerA.GetUsageTrendForUsers(context.Background(), []int64{601, 602}, testSub2APITeamTrendParams())
		distinctResults <- trendResult{values: values, err: err}
	}()
	go func() {
		values, err := providerB.GetUsageTrendForUsers(context.Background(), []int64{603, 604}, testSub2APITeamTrendParams())
		distinctResults <- trendResult{values: values, err: err}
	}()
	distinctA := <-distinctResults
	distinctB := <-distinctResults
	if distinctA.err != nil || distinctB.err != nil {
		t.Fatalf("distinct calls errors = %v / %v", distinctA.err, distinctB.err)
	}
	if batchCalls.Load() != 3 || individualCalls.Load() != 0 {
		t.Fatalf("distinct calls total batch/individual = %d/%d, want 3/0", batchCalls.Load(), individualCalls.Load())
	}
	isFirstSet := func(values map[int64][]UsageTrendPoint) bool {
		return len(values) == 2 && len(values[601]) == 1 && len(values[602]) == 1
	}
	isSecondSet := func(values map[int64][]UsageTrendPoint) bool {
		return len(values) == 2 && len(values[603]) == 1 && len(values[604]) == 1
	}
	if !((isFirstSet(distinctA.values) && isSecondSet(distinctB.values)) ||
		(isSecondSet(distinctA.values) && isFirstSet(distinctB.values))) {
		t.Fatalf("distinct results contain unexpected IDs: %#v / %#v", distinctA.values, distinctB.values)
	}

	overlap, err := providerA.GetUsageTrendForUsers(context.Background(), []int64{601, 603}, testSub2APITeamTrendParams())
	if err != nil {
		t.Fatalf("overlap GetUsageTrendForUsers() error = %v", err)
	}
	if batchCalls.Load() != 3 || individualCalls.Load() != 0 {
		t.Fatalf("overlap total batch/individual = %d/%d, want unchanged 3/0", batchCalls.Load(), individualCalls.Load())
	}
	if overlap[601][0].ActualCost != 6.01 || overlap[603][0].ActualCost != 6.03 {
		t.Fatalf("overlap Redis values = %#v", overlap)
	}
}

func writeSub2APITeamTrendBatchRows(t *testing.T, w http.ResponseWriter, rows []map[string]any) {
	t.Helper()
	if err := json.NewEncoder(w).Encode(map[string]any{
		"success": true, "data": map[string]any{"trend": rows},
	}); err != nil {
		t.Errorf("encode batch response: %v", err)
	}
}

func waitForTeamTrendMetric(t *testing.T, metrics *teamTrendRedisTestMetrics, outcome string, count int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if metrics.count(outcome) >= count {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("metrics = %v, want %s count >= %d", metrics.outcomesSnapshot(), outcome, count)
}

type teamTrendOrchestrationFaultStore struct {
	readcache.MultiStore
	mgetErr    error
	setManyErr error
	acquireErr error
	ttlErr     error
	releaseErr error
}

func (s *teamTrendOrchestrationFaultStore) MGet(ctx context.Context, keys []string) ([][]byte, error) {
	if s.mgetErr != nil {
		return nil, s.mgetErr
	}
	return s.MultiStore.MGet(ctx, keys)
}

func (s *teamTrendOrchestrationFaultStore) SetMany(ctx context.Context, items []readcache.SetItem) error {
	if s.setManyErr != nil {
		return s.setManyErr
	}
	return s.MultiStore.SetMany(ctx, items)
}

func (s *teamTrendOrchestrationFaultStore) TryAcquireLease(ctx context.Context, key, token string, ttl time.Duration) (bool, error) {
	if s.acquireErr != nil {
		return false, s.acquireErr
	}
	return s.MultiStore.TryAcquireLease(ctx, key, token, ttl)
}

func (s *teamTrendOrchestrationFaultStore) LeaseTTL(ctx context.Context, key string) (time.Duration, error) {
	if s.ttlErr != nil {
		return 0, s.ttlErr
	}
	return s.MultiStore.LeaseTTL(ctx, key)
}

func (s *teamTrendOrchestrationFaultStore) ReleaseLease(ctx context.Context, key, token string) (bool, error) {
	if s.releaseErr != nil {
		return false, s.releaseErr
	}
	return s.MultiStore.ReleaseLease(ctx, key, token)
}

func newSub2APITeamTrendTestProvider(
	t *testing.T,
	httpServer *httptest.Server,
	store readcache.MultiStore,
	metrics readcache.Metrics,
	namespace string,
) *sub2apiRelay {
	t.Helper()
	cache := newTeamTrendRedisTestCache(t, store, teamTrendRedisCacheOptions{
		Namespace: namespace, ProviderID: 7, ProviderVersion: 3, Metrics: metrics,
	})
	return &sub2apiRelay{
		client: httpServer.Client(), adminURL: httpServer.URL, baseURL: httpServer.URL + "/v1",
		apiKey: "test-admin-key", model: "test-model", logger: zap.NewNop(), teamTrends: cache,
	}
}

func testSub2APITeamTrendParams() TeamMemberTrendParams {
	return TeamMemberTrendParams{
		StartDate: "2026-07-01", EndDate: "2026-07-20", Granularity: "day", Timezone: "Asia/Shanghai",
	}
}
