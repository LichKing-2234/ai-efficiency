package personalusage

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/internal/readcache"
)

type recordingUsageCacheMetrics struct {
	mu     sync.Mutex
	counts map[string]int
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
