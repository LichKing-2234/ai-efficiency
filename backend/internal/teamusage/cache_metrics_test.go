package teamusage

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/internal/readcache"
	"github.com/alicebob/miniredis/v2"
	redis "github.com/redis/go-redis/v9"
)

type recordingTeamCacheMetrics struct {
	mu     sync.Mutex
	counts map[string]int
}

func TestTeamUsageCacheMetricsRecordEligibleStale(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	now := time.Date(2026, 7, 18, 8, 0, 0, 0, time.UTC)
	recorder := newRecordingTeamCacheMetrics()
	cache, err := NewSnapshotCache(readcache.NewRedisStore(client), SnapshotCacheOptions{
		Namespace: "test", Now: func() time.Time { return now }, SummaryMetrics: recorder,
		CommandTimeout: time.Second, RefreshTimeout: 2 * time.Second, LeaseTTL: time.Second,
		PollInterval: time.Millisecond, ReleaseTimeout: time.Second,
		RandFloat64: func() float64 { return 0 }, NewToken: func() string { return "metrics-token" },
	})
	if err != nil {
		t.Fatalf("NewSnapshotCache() error = %v", err)
	}
	key := testSnapshotCacheKey()
	if _, err := cache.GetSummaryOrLoad(context.Background(), key, func(context.Context) (SummaryOriginLoadResult, error) {
		return SummaryOriginLoadResult{Snapshot: testSummarySnapshot(1)}, nil
	}); err != nil {
		t.Fatalf("prime summary: %v", err)
	}
	now = now.Add(55 * time.Second)
	transient := errors.New("relay unavailable")
	result, err := cache.GetSummaryOrLoad(context.Background(), key, func(context.Context) (SummaryOriginLoadResult, error) {
		return SummaryOriginLoadResult{SnapshotErr: transient}, nil
	})
	if err != nil || result.Freshness.CacheStatus != "stale" {
		t.Fatalf("stale result = %+v, error = %v", result, err)
	}
	if recorder.count("stale") != 1 || recorder.count("error") != 1 {
		t.Fatalf("stale/error outcomes = %d/%d, want 1/1", recorder.count("stale"), recorder.count("error"))
	}
}

func newRecordingTeamCacheMetrics() *recordingTeamCacheMetrics {
	return &recordingTeamCacheMetrics{counts: make(map[string]int)}
}

func (r *recordingTeamCacheMetrics) Record(outcome string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.counts[outcome]++
}

func (r *recordingTeamCacheMetrics) count(outcome string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.counts[outcome]
}

func TestTeamUsageCacheMetricsKeepSummaryAndOverviewSeparate(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	now := time.Date(2026, 7, 18, 8, 0, 0, 0, time.UTC)
	summaryMetrics := newRecordingTeamCacheMetrics()
	overviewMetrics := newRecordingTeamCacheMetrics()
	cache, err := NewSnapshotCache(readcache.NewRedisStore(client), SnapshotCacheOptions{
		Namespace: "test", Now: func() time.Time { return now },
		SummaryMetrics: summaryMetrics, OverviewMetrics: overviewMetrics,
		CommandTimeout: time.Second, RefreshTimeout: 2 * time.Second, LeaseTTL: time.Second,
		PollInterval: time.Millisecond, ReleaseTimeout: time.Second,
		RandFloat64: func() float64 { return 0 }, NewToken: func() string { return "metrics-token" },
	})
	if err != nil {
		t.Fatalf("NewSnapshotCache() error = %v", err)
	}
	key := testSnapshotCacheKey()
	for index := 0; index < 2; index++ {
		if _, err := cache.GetSummaryOrLoad(context.Background(), key, func(context.Context) (SummaryOriginLoadResult, error) {
			return SummaryOriginLoadResult{Snapshot: testSummarySnapshot(1)}, nil
		}); err != nil {
			t.Fatalf("summary read %d error = %v", index, err)
		}
		if _, err := cache.GetOrLoad(context.Background(), key, func(context.Context) (SnapshotOriginLoadResult, error) {
			return SnapshotOriginLoadResult{Snapshot: testOverviewSnapshot(1)}, nil
		}); err != nil {
			t.Fatalf("overview read %d error = %v", index, err)
		}
	}
	for name, recorder := range map[string]*recordingTeamCacheMetrics{"summary": summaryMetrics, "overview": overviewMetrics} {
		for outcome, want := range map[string]int{"miss": 1, "refresh": 1, "lease_acquired": 1, "fresh": 1} {
			if got := recorder.count(outcome); got != want {
				t.Fatalf("%s outcome %s = %d, want %d", name, outcome, got, want)
			}
		}
	}
}
