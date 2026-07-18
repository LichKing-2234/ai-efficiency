package representativescope

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

type recordingScopeCacheMetrics struct {
	mu     sync.Mutex
	counts map[string]int
}

type scopeLeaseFailureStore struct{}

func (scopeLeaseFailureStore) Get(context.Context, string) ([]byte, error) {
	return nil, readcache.ErrMiss
}

type scopeTTLFailureStore struct {
	mu           sync.Mutex
	acquireCalls int
}

func (s *scopeTTLFailureStore) Get(context.Context, string) ([]byte, error) {
	return nil, readcache.ErrMiss
}
func (s *scopeTTLFailureStore) Set(context.Context, string, []byte, time.Duration) error { return nil }
func (s *scopeTTLFailureStore) TryAcquireLease(context.Context, string, string, time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.acquireCalls++
	return s.acquireCalls > 1, nil
}
func (s *scopeTTLFailureStore) LeaseTTL(context.Context, string) (time.Duration, error) {
	return 0, errors.New("Redis TTL unavailable")
}
func (s *scopeTTLFailureStore) ReleaseLease(context.Context, string, string) (bool, error) {
	return true, nil
}

func TestRepresentativeScopeCacheMetricsLeaseTTLErrorFallsBackAuthoritatively(t *testing.T) {
	recorder := &recordingScopeCacheMetrics{}
	cache, err := NewCache(&scopeTTLFailureStore{}, CacheOptions{
		Namespace: "test", Metrics: recorder,
		CommandTimeout: time.Second, RefreshTimeout: 2 * time.Second, LeaseTTL: time.Second,
		PollInterval: time.Millisecond, ReleaseTimeout: time.Second,
		RandFloat64: func() float64 { return 0 }, NewToken: func() string { return "ttl-token" },
	})
	if err != nil {
		t.Fatalf("NewCache() error = %v", err)
	}
	guard := scopeGuard{ActorUserID: 7, ActorRole: "user", DirectorySourceID: 3, DirectoryRunID: 11}
	result, err := cache.GetOrLoad(context.Background(), guard, func(context.Context) (*Scope, error) {
		return testCachedScope(guard.ActorUserID, "department-alpha"), nil
	})
	if err != nil || result == nil {
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
func (scopeLeaseFailureStore) Set(context.Context, string, []byte, time.Duration) error { return nil }
func (scopeLeaseFailureStore) TryAcquireLease(context.Context, string, string, time.Duration) (bool, error) {
	return false, errors.New("redis lease unavailable")
}
func (scopeLeaseFailureStore) LeaseTTL(context.Context, string) (time.Duration, error) {
	return 0, readcache.ErrMiss
}
func (scopeLeaseFailureStore) ReleaseLease(context.Context, string, string) (bool, error) {
	return true, nil
}

func TestRepresentativeScopeCacheMetricsRecordLeaseFailureAndFallback(t *testing.T) {
	recorder := &recordingScopeCacheMetrics{}
	cache, err := NewCache(scopeLeaseFailureStore{}, CacheOptions{
		Namespace: "test", Metrics: recorder,
		CommandTimeout: time.Second, RefreshTimeout: 2 * time.Second, LeaseTTL: time.Second,
		PollInterval: time.Millisecond, ReleaseTimeout: time.Second,
		RandFloat64: func() float64 { return 0 }, NewToken: func() string { return "metrics-token" },
	})
	if err != nil {
		t.Fatalf("NewCache() error = %v", err)
	}
	guard := scopeGuard{ActorUserID: 7, ActorRole: "user", DirectorySourceID: 3, DirectoryRunID: 11}
	result, err := cache.GetOrLoad(context.Background(), guard, func(context.Context) (*Scope, error) {
		return testCachedScope(guard.ActorUserID, "department-alpha"), nil
	})
	if err != nil || result == nil {
		t.Fatalf("fallback result = %+v, error = %v", result, err)
	}
	for outcome, want := range map[string]int{"miss": 1, "refresh": 1, "error": 1, "lease_failed": 1} {
		if got := recorder.count(outcome); got != want {
			t.Fatalf("outcome %s = %d, want %d", outcome, got, want)
		}
	}
}

func (r *recordingScopeCacheMetrics) Record(outcome string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.counts == nil {
		r.counts = make(map[string]int)
	}
	r.counts[outcome]++
}

func (r *recordingScopeCacheMetrics) count(outcome string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.counts[outcome]
}

func TestRepresentativeScopeCacheMetricsRecordColdAndWarmReads(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	recorder := &recordingScopeCacheMetrics{}
	cache, err := NewCache(readcache.NewRedisStore(client), CacheOptions{
		Namespace: "test", Metrics: recorder,
		CommandTimeout: time.Second, RefreshTimeout: 2 * time.Second, LeaseTTL: time.Second,
		PollInterval: time.Millisecond, ReleaseTimeout: time.Second,
		RandFloat64: func() float64 { return 0 }, NewToken: func() string { return "metrics-token" },
	})
	if err != nil {
		t.Fatalf("NewCache() error = %v", err)
	}
	guard := scopeGuard{ActorUserID: 7, ActorRole: "user", DirectorySourceID: 3, DirectoryRunID: 11}
	loader := func(context.Context) (*Scope, error) {
		return testCachedScope(guard.ActorUserID, "department-alpha"), nil
	}
	for index := 0; index < 2; index++ {
		if _, err := cache.GetOrLoad(context.Background(), guard, loader); err != nil {
			t.Fatalf("read %d error = %v", index, err)
		}
	}
	for outcome, want := range map[string]int{"miss": 1, "refresh": 1, "lease_acquired": 1, "fresh": 1} {
		if got := recorder.count(outcome); got != want {
			t.Fatalf("outcome %s = %d, want %d", outcome, got, want)
		}
	}
}
