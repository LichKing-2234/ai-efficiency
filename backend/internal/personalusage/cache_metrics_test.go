package personalusage

import (
	"context"
	"errors"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/internal/readcache"
)

type recordingUsageCacheMetrics struct {
	mu     sync.Mutex
	counts map[string]int
}

func TestPersonalUsageCacheMetricsRecordEncodeError(t *testing.T) {
	now := time.Date(2026, 7, 18, 8, 0, 0, 0, time.UTC)
	recorder := newRecordingUsageCacheMetrics()
	cache, err := NewCache(newFakeUsageStore(func() time.Time { return now }), CacheOptions{
		Namespace: "test", Metrics: recorder, Now: func() time.Time { return now },
		CommandTimeout: time.Second, RefreshTimeout: 2 * time.Second, LeaseTTL: time.Second,
		PollInterval: time.Millisecond, ReleaseTimeout: time.Second,
		RandFloat64: func() float64 { return 0 }, NewToken: func() string { return "encode-token" }, Sleep: readcache.Sleep,
	})
	if err != nil {
		t.Fatalf("NewCache() error = %v", err)
	}
	_, err = cache.GetOrLoad(context.Background(), testCacheKey(), false, func(context.Context, bool) (OriginLoadResult, error) {
		usage := testUsageSnapshot(1)
		usage.Stats.TotalActualCost = math.NaN()
		return OriginLoadResult{Usage: usage}, nil
	})
	if err == nil {
		t.Fatal("GetOrLoad() error = nil, want JSON encoding error")
	}
	if recorder.count("error") != 1 {
		t.Fatalf("error outcomes = %d, want 1", recorder.count("error"))
	}
}

type retryingUsageMetricsStore struct {
	mu           sync.Mutex
	acquireCalls int
	ttlErr       error
}

func (s *retryingUsageMetricsStore) Get(context.Context, string) ([]byte, error) {
	return nil, readcache.ErrMiss
}
func (s *retryingUsageMetricsStore) Set(context.Context, string, []byte, time.Duration) error {
	return nil
}
func (s *retryingUsageMetricsStore) TryAcquireLease(context.Context, string, string, time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.acquireCalls++
	return s.acquireCalls > 1, nil
}
func (s *retryingUsageMetricsStore) LeaseTTL(context.Context, string) (time.Duration, error) {
	if s.ttlErr != nil {
		return 0, s.ttlErr
	}
	return 0, readcache.ErrMiss
}
func (s *retryingUsageMetricsStore) ReleaseLease(context.Context, string, string) (bool, error) {
	return true, nil
}

func TestPersonalUsageCacheMetricsRecordOneMissAcrossLeaseRetry(t *testing.T) {
	recorder := newRecordingUsageCacheMetrics()
	cache, err := NewCache(&retryingUsageMetricsStore{}, CacheOptions{
		Namespace: "test", Metrics: recorder,
		CommandTimeout: time.Second, RefreshTimeout: 2 * time.Second, LeaseTTL: time.Second,
		PollInterval: time.Millisecond, ReleaseTimeout: time.Second,
		Now: time.Now, RandFloat64: func() float64 { return 0 }, NewToken: func() string { return "retry-token" }, Sleep: readcache.Sleep,
	})
	if err != nil {
		t.Fatalf("NewCache() error = %v", err)
	}
	if _, err := cache.GetOrLoad(context.Background(), testCacheKey(), false, func(context.Context, bool) (OriginLoadResult, error) {
		return OriginLoadResult{Usage: testUsageSnapshot(1)}, nil
	}); err != nil {
		t.Fatalf("GetOrLoad() error = %v", err)
	}
	if recorder.count("miss") != 1 {
		t.Fatalf("miss outcomes = %d, want one logical miss", recorder.count("miss"))
	}
}

func TestPersonalUsageCacheMetricsLeaseTTLErrorFallsBackAuthoritatively(t *testing.T) {
	recorder := newRecordingUsageCacheMetrics()
	store := &retryingUsageMetricsStore{ttlErr: errors.New("Redis TTL unavailable")}
	cache, err := NewCache(store, CacheOptions{
		Namespace: "test", Metrics: recorder,
		CommandTimeout: time.Second, RefreshTimeout: 2 * time.Second, LeaseTTL: time.Second,
		PollInterval: time.Millisecond, ReleaseTimeout: time.Second,
		Now: time.Now, RandFloat64: func() float64 { return 0 }, NewToken: func() string { return "ttl-token" }, Sleep: readcache.Sleep,
	})
	if err != nil {
		t.Fatalf("NewCache() error = %v", err)
	}
	result, err := cache.GetOrLoad(context.Background(), testCacheKey(), false, func(context.Context, bool) (OriginLoadResult, error) {
		return OriginLoadResult{Usage: testUsageSnapshot(2)}, nil
	})
	if err != nil || result.Usage.Stats.TotalRequests != 2 {
		t.Fatalf("fallback result = %+v, error = %v", result, err)
	}
	for outcome, want := range map[string]int{"miss": 1, "lease_wait": 1, "error": 1, "lease_failed": 1, "refresh": 1} {
		if got := recorder.count(outcome); got != want {
			t.Fatalf("outcome %s = %d, want %d", outcome, got, want)
		}
	}
	if recorder.count("lease_acquired") != 0 {
		t.Fatalf("lease_acquired = %d, want immediate fallback", recorder.count("lease_acquired"))
	}
}

func TestPersonalUsageCacheMetricsRecordStaleAndRedisFallback(t *testing.T) {
	t.Run("eligible stale", func(t *testing.T) {
		now := time.Date(2026, 7, 18, 8, 0, 0, 0, time.UTC)
		store := newFakeUsageStore(func() time.Time { return now })
		recorder := newRecordingUsageCacheMetrics()
		cache, err := NewCache(store, CacheOptions{
			Namespace: "test", Now: func() time.Time { return now }, Metrics: recorder,
			CommandTimeout: time.Second, RefreshTimeout: 2 * time.Second, LeaseTTL: time.Second,
			PollInterval: time.Millisecond, ReleaseTimeout: time.Second,
			RandFloat64: func() float64 { return 0 }, NewToken: func() string { return "stale-token" }, Sleep: readcache.Sleep,
		})
		if err != nil {
			t.Fatalf("NewCache() error = %v", err)
		}
		if _, err := cache.GetOrLoad(context.Background(), testCacheKey(), false, func(context.Context, bool) (OriginLoadResult, error) {
			return OriginLoadResult{Usage: testUsageSnapshot(1)}, nil
		}); err != nil {
			t.Fatalf("prime cache: %v", err)
		}
		now = now.Add(28 * time.Second)
		transient := errors.New("relay unavailable")
		result, err := cache.GetOrLoad(context.Background(), testCacheKey(), false, func(context.Context, bool) (OriginLoadResult, error) {
			return OriginLoadResult{UsageErr: transient}, nil
		})
		if err != nil || result.UsageFreshness.CacheStatus != "stale" {
			t.Fatalf("stale result = %+v, error = %v", result, err)
		}
		if recorder.count("stale") != 1 || recorder.count("error") != 1 {
			t.Fatalf("stale/error outcomes = %d/%d, want 1/1", recorder.count("stale"), recorder.count("error"))
		}
	})

	t.Run("Redis read and release failures", func(t *testing.T) {
		now := time.Date(2026, 7, 18, 8, 0, 0, 0, time.UTC)
		store := newFakeUsageStore(func() time.Time { return now })
		store.getErr = errors.New("redis read unavailable")
		recorder := newRecordingUsageCacheMetrics()
		cache, err := NewCache(store, CacheOptions{
			Namespace: "test", Now: func() time.Time { return now }, Metrics: recorder,
			CommandTimeout: time.Second, RefreshTimeout: 2 * time.Second, LeaseTTL: time.Second,
			PollInterval: time.Millisecond, ReleaseTimeout: time.Second,
			RandFloat64: func() float64 { return 0 }, NewToken: func() string { return "fallback-token" }, Sleep: readcache.Sleep,
		})
		if err != nil {
			t.Fatalf("NewCache() error = %v", err)
		}
		result, err := cache.GetOrLoad(context.Background(), testCacheKey(), false, func(context.Context, bool) (OriginLoadResult, error) {
			return OriginLoadResult{Usage: testUsageSnapshot(2)}, nil
		})
		if err != nil || result.Usage.Stats.TotalRequests != 2 {
			t.Fatalf("fallback result = %+v, error = %v", result, err)
		}
		if recorder.count("error") != 1 || recorder.count("refresh") != 1 {
			t.Fatalf("fallback error/refresh = %d/%d, want 1/1", recorder.count("error"), recorder.count("refresh"))
		}

		store.getErr = nil
		store.releaseErr = errors.New("redis release unavailable")
		secondRecorder := newRecordingUsageCacheMetrics()
		second, err := NewCache(store, CacheOptions{
			Namespace: "release", Now: func() time.Time { return now }, Metrics: secondRecorder,
			CommandTimeout: time.Second, RefreshTimeout: 2 * time.Second, LeaseTTL: time.Second,
			PollInterval: time.Millisecond, ReleaseTimeout: time.Second,
			RandFloat64: func() float64 { return 0 }, NewToken: func() string { return "release-token" }, Sleep: readcache.Sleep,
		})
		if err != nil {
			t.Fatalf("NewCache() release case error = %v", err)
		}
		if _, err := second.GetOrLoad(context.Background(), testCacheKey(), false, func(context.Context, bool) (OriginLoadResult, error) {
			return OriginLoadResult{Usage: testUsageSnapshot(3)}, nil
		}); err != nil {
			t.Fatalf("release fallback result: %v", err)
		}
		if secondRecorder.count("lease_failed") != 1 || secondRecorder.count("error") != 1 {
			t.Fatalf("release failure/error = %d/%d, want 1/1", secondRecorder.count("lease_failed"), secondRecorder.count("error"))
		}
	})
}

func newRecordingUsageCacheMetrics() *recordingUsageCacheMetrics {
	return &recordingUsageCacheMetrics{counts: make(map[string]int)}
}

func (r *recordingUsageCacheMetrics) Record(outcome string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.counts[outcome]++
}

func (r *recordingUsageCacheMetrics) count(outcome string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.counts[outcome]
}

func TestPersonalUsageCacheMetricsRecordColdAndWarmReads(t *testing.T) {
	now := time.Date(2026, 7, 18, 8, 0, 0, 0, time.UTC)
	store := newFakeUsageStore(func() time.Time { return now })
	recorder := newRecordingUsageCacheMetrics()
	cache, err := NewCache(store, CacheOptions{
		Namespace: "test", Now: func() time.Time { return now }, Metrics: recorder,
		CommandTimeout: time.Second, RefreshTimeout: 2 * time.Second, LeaseTTL: time.Second,
		PollInterval: time.Millisecond, ReleaseTimeout: time.Second,
		RandFloat64: func() float64 { return 0 }, NewToken: func() string { return "metrics-token" }, Sleep: readcache.Sleep,
	})
	if err != nil {
		t.Fatalf("NewCache() error = %v", err)
	}
	loader := func(context.Context, bool) (OriginLoadResult, error) {
		return OriginLoadResult{Usage: testUsageSnapshot(1)}, nil
	}
	if _, err := cache.GetOrLoad(context.Background(), testCacheKey(), false, loader); err != nil {
		t.Fatalf("cold GetOrLoad() error = %v", err)
	}
	if _, err := cache.GetOrLoad(context.Background(), testCacheKey(), false, loader); err != nil {
		t.Fatalf("warm GetOrLoad() error = %v", err)
	}
	for outcome, want := range map[string]int{"miss": 1, "refresh": 1, "lease_acquired": 1, "fresh": 1} {
		if got := recorder.count(outcome); got != want {
			t.Fatalf("outcome %s = %d, want %d", outcome, got, want)
		}
	}
}
