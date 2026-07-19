package teamusage

import (
	"context"
	"errors"
	"math"
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

func TestTeamUsageCacheMetricsRecordEncodeError(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	recorder := newRecordingTeamCacheMetrics()
	cache, err := NewSnapshotCache(readcache.NewRedisStore(client), SnapshotCacheOptions{
		Namespace: "test", SummaryMetrics: recorder, Now: time.Now,
		CommandTimeout: time.Second, RefreshTimeout: 2 * time.Second, LeaseTTL: time.Second,
		PollInterval: time.Millisecond, ReleaseTimeout: time.Second,
		RandFloat64: func() float64 { return 0 }, NewToken: func() string { return "encode-token" },
	})
	if err != nil {
		t.Fatalf("NewSnapshotCache() error = %v", err)
	}
	_, err = cache.GetSummaryOrLoad(context.Background(), testSnapshotCacheKey(), func(context.Context) (SummaryOriginLoadResult, error) {
		snapshot := testSummarySnapshot(math.NaN())
		return SummaryOriginLoadResult{Snapshot: snapshot}, nil
	})
	if err == nil {
		t.Fatal("GetSummaryOrLoad() error = nil, want JSON encoding error")
	}
	if recorder.count("error") != 1 {
		t.Fatalf("error outcomes = %d, want 1", recorder.count("error"))
	}
}

type retryingTeamMetricsStore struct {
	mu           sync.Mutex
	acquireCalls int
	ttlErr       error
}

func (s *retryingTeamMetricsStore) Get(context.Context, string) ([]byte, error) {
	return nil, readcache.ErrMiss
}
func (s *retryingTeamMetricsStore) Set(context.Context, string, []byte, time.Duration) error {
	return nil
}
func (s *retryingTeamMetricsStore) TryAcquireLease(context.Context, string, string, time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.acquireCalls++
	return s.acquireCalls > 1, nil
}
func (s *retryingTeamMetricsStore) LeaseTTL(context.Context, string) (time.Duration, error) {
	if s.ttlErr != nil {
		return 0, s.ttlErr
	}
	return 0, readcache.ErrMiss
}
func (s *retryingTeamMetricsStore) ReleaseLease(context.Context, string, string) (bool, error) {
	return true, nil
}

func TestTeamUsageCacheMetricsRecordOneMissAcrossLeaseRetry(t *testing.T) {
	recorder := newRecordingTeamCacheMetrics()
	cache, err := NewSnapshotCache(&retryingTeamMetricsStore{}, SnapshotCacheOptions{
		Namespace: "test", SummaryMetrics: recorder,
		CommandTimeout: time.Second, RefreshTimeout: 2 * time.Second, LeaseTTL: time.Second,
		PollInterval: time.Millisecond, ReleaseTimeout: time.Second,
		Now: time.Now, RandFloat64: func() float64 { return 0 }, NewToken: func() string { return "retry-token" },
	})
	if err != nil {
		t.Fatalf("NewSnapshotCache() error = %v", err)
	}
	if _, err := cache.GetSummaryOrLoad(context.Background(), testSnapshotCacheKey(), func(context.Context) (SummaryOriginLoadResult, error) {
		return SummaryOriginLoadResult{Snapshot: testSummarySnapshot(1)}, nil
	}); err != nil {
		t.Fatalf("GetSummaryOrLoad() error = %v", err)
	}
	if recorder.count("miss") != 1 {
		t.Fatalf("miss outcomes = %d, want one logical miss", recorder.count("miss"))
	}
}

func TestTeamUsageCacheMetricsLeaseTTLErrorFallsBackAuthoritatively(t *testing.T) {
	recorder := newRecordingTeamCacheMetrics()
	store := &retryingTeamMetricsStore{ttlErr: errors.New("Redis TTL unavailable")}
	cache, err := NewSnapshotCache(store, SnapshotCacheOptions{
		Namespace: "test", SummaryMetrics: recorder,
		CommandTimeout: time.Second, RefreshTimeout: 2 * time.Second, LeaseTTL: time.Second,
		PollInterval: time.Millisecond, ReleaseTimeout: time.Second,
		Now: time.Now, RandFloat64: func() float64 { return 0 }, NewToken: func() string { return "ttl-token" },
	})
	if err != nil {
		t.Fatalf("NewSnapshotCache() error = %v", err)
	}
	result, err := cache.GetSummaryOrLoad(context.Background(), testSnapshotCacheKey(), func(context.Context) (SummaryOriginLoadResult, error) {
		return SummaryOriginLoadResult{Snapshot: testSummarySnapshot(2)}, nil
	})
	if err != nil || result.Snapshot.Summary.RangeActualCost == nil || *result.Snapshot.Summary.RangeActualCost != 2 {
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
	now = now.Add(2*time.Minute + 43*time.Second)
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

func TestTeamUsageCacheMetricsKeepSummaryTrendMembersAndOrganizationSeparate(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	now := time.Date(2026, 7, 18, 8, 0, 0, 0, time.UTC)
	summaryMetrics := newRecordingTeamCacheMetrics()
	trendMetrics := newRecordingTeamCacheMetrics()
	membersMetrics := newRecordingTeamCacheMetrics()
	organizationMetrics := newRecordingTeamCacheMetrics()
	cache, err := NewSnapshotCache(readcache.NewRedisStore(client), SnapshotCacheOptions{
		Namespace: "test", Now: func() time.Time { return now },
		SummaryMetrics: summaryMetrics, TrendMetrics: trendMetrics, MembersMetrics: membersMetrics, OrganizationMetrics: organizationMetrics,
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
		if _, err := cache.GetTrendOrLoad(context.Background(), key, func(context.Context) (TrendOriginLoadResult, error) {
			return TrendOriginLoadResult{Snapshot: testTrendSnapshot()}, nil
		}); err != nil {
			t.Fatalf("trend read %d error = %v", index, err)
		}
		if _, err := cache.GetMembersOrLoad(context.Background(), key, func(context.Context) (MembersOriginLoadResult, error) {
			return MembersOriginLoadResult{Snapshot: testMembersSnapshot()}, nil
		}); err != nil {
			t.Fatalf("members read %d error = %v", index, err)
		}
		if _, err := cache.GetOrganizationOrLoad(context.Background(), OrganizationCacheKey{SnapshotCacheKey: key}, func(context.Context) (OrganizationOriginLoadResult, error) {
			return OrganizationOriginLoadResult{Snapshot: &OrganizationSnapshot{
				Window: testTrendSnapshot().Window, Departments: []OrganizationDepartment{}, Members: []OverviewMember{},
			}}, nil
		}); err != nil {
			t.Fatalf("organization read %d error = %v", index, err)
		}
	}
	for name, recorder := range map[string]*recordingTeamCacheMetrics{"summary": summaryMetrics, "trend": trendMetrics, "members": membersMetrics, "organization": organizationMetrics} {
		for outcome, want := range map[string]int{"miss": 1, "refresh": 1, "lease_acquired": 1, "fresh": 1} {
			if got := recorder.count(outcome); got != want {
				t.Fatalf("%s outcome %s = %d, want %d", name, outcome, got, want)
			}
		}
	}
}
