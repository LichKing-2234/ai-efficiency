package personalusage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/internal/readcache"
	"github.com/ai-efficiency/backend/internal/relay"
)

type fakeStoreValue struct {
	value []byte
	ttl   time.Duration
}

type fakeLease struct {
	token     string
	expiresAt time.Time
}

type fakeUsageStore struct {
	mu         sync.Mutex
	now        func() time.Time
	values     map[string]fakeStoreValue
	leases     map[string]fakeLease
	getErr     error
	setErr     error
	acquireErr error
	releaseErr error
	setCalls   int
	getCalls   int
	leaseCalls int
}

func newFakeUsageStore(now func() time.Time) *fakeUsageStore {
	return &fakeUsageStore{
		now:    now,
		values: make(map[string]fakeStoreValue),
		leases: make(map[string]fakeLease),
	}
}

func (s *fakeUsageStore) Get(_ context.Context, key string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.getCalls++
	if s.getErr != nil {
		return nil, s.getErr
	}
	value, ok := s.values[key]
	if !ok {
		return nil, readcache.ErrMiss
	}
	return append([]byte(nil), value.value...), nil
}

func (s *fakeUsageStore) Set(_ context.Context, key string, value []byte, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.setCalls++
	if s.setErr != nil {
		return s.setErr
	}
	s.values[key] = fakeStoreValue{value: append([]byte(nil), value...), ttl: ttl}
	return nil
}

func (s *fakeUsageStore) TryAcquireLease(_ context.Context, key, token string, ttl time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.acquireErr != nil {
		return false, s.acquireErr
	}
	now := s.now()
	if lease, ok := s.leases[key]; ok && lease.expiresAt.After(now) {
		return false, nil
	}
	s.leases[key] = fakeLease{token: token, expiresAt: now.Add(ttl)}
	return true, nil
}

func (s *fakeUsageStore) LeaseTTL(_ context.Context, key string) (time.Duration, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.leaseCalls++
	lease, ok := s.leases[key]
	if !ok || !lease.expiresAt.After(s.now()) {
		delete(s.leases, key)
		return 0, readcache.ErrMiss
	}
	return lease.expiresAt.Sub(s.now()), nil
}

func (s *fakeUsageStore) leaseCallCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.leaseCalls
}

type injectAfterAcquireStore struct {
	*fakeUsageStore
	once  sync.Once
	value []byte
}

func (s *injectAfterAcquireStore) TryAcquireLease(ctx context.Context, key, token string, ttl time.Duration) (bool, error) {
	acquired, err := s.fakeUsageStore.TryAcquireLease(ctx, key, token, ttl)
	if err != nil || !acquired {
		return acquired, err
	}
	s.once.Do(func() {
		valueKey := strings.TrimSuffix(key, ":lease")
		s.fakeUsageStore.mu.Lock()
		s.fakeUsageStore.values[valueKey] = fakeStoreValue{value: append([]byte(nil), s.value...), ttl: time.Minute}
		s.fakeUsageStore.mu.Unlock()
	})
	return true, nil
}

func (s *fakeUsageStore) ReleaseLease(_ context.Context, key, token string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.releaseErr != nil {
		return false, s.releaseErr
	}
	lease, ok := s.leases[key]
	if !ok || lease.token != token {
		return false, nil
	}
	delete(s.leases, key)
	return true, nil
}

func testCache(t *testing.T, store readcache.Store, now func() time.Time, random float64) *Cache {
	t.Helper()
	var token atomic.Int64
	cache, err := NewCache(store, CacheOptions{
		Namespace:      "test-blue",
		CommandTimeout: time.Second,
		RefreshTimeout: 2 * time.Second,
		LeaseTTL:       time.Second,
		PollInterval:   time.Millisecond,
		ReleaseTimeout: time.Second,
		Now:            now,
		RandFloat64:    func() float64 { return random },
		NewToken:       func() string { return fmt.Sprintf("token-%d", token.Add(1)) },
		Sleep:          readcache.Sleep,
	})
	if err != nil {
		t.Fatalf("NewCache() error = %v", err)
	}
	return cache
}

func testCacheKey() CacheKey {
	return CacheKey{
		ProviderID:      3,
		ProviderVersion: 4,
		ActorID:         5,
		RelayUserID:     7,
		BindingVersion:  9,
		Params: relay.UserUsageDashboardParams{
			StartDate:   "2026-07-01",
			EndDate:     "2026-07-15",
			Granularity: "day",
			Timezone:    "Asia/Shanghai",
		},
	}
}

func testUsageSnapshot(requests int64) *relay.UserUsageDashboardResponse {
	return &relay.UserUsageDashboardResponse{
		Configured: true,
		Range: relay.UserUsageDashboardRange{
			StartDate: "2026-07-01", EndDate: "2026-07-15", Granularity: "day", Timezone: "Asia/Shanghai",
		},
		Stats:  &relay.UserUsageDashboardStats{TotalRequests: requests},
		Trend:  []relay.UserUsageTrendPoint{},
		Models: []relay.UserUsageModelStat{},
	}
}

func TestCacheColdFreshStaleAndHardExpiry(t *testing.T) {
	now := time.Date(2026, 7, 15, 8, 0, 0, 0, time.UTC)
	store := newFakeUsageStore(func() time.Time { return now })
	cache := testCache(t, store, func() time.Time { return now }, 0)
	key := testCacheKey()
	var calls atomic.Int32
	loadSuccess := func(context.Context, bool) (OriginLoadResult, error) {
		calls.Add(1)
		return OriginLoadResult{Usage: testUsageSnapshot(1)}, nil
	}

	cold, err := cache.GetOrLoad(context.Background(), key, false, loadSuccess)
	if err != nil {
		t.Fatalf("cold GetOrLoad() error = %v", err)
	}
	if cold.UsageFreshness.CacheStatus != "miss" || cold.UsageFreshness.SourceStatus != "ok" {
		t.Fatalf("cold freshness = %+v", cold.UsageFreshness)
	}
	if got := cold.UsageFreshness.FreshUntil.Sub(now); got != 27*time.Second {
		t.Fatalf("fresh window = %s, want 27s", got)
	}
	if got := cold.UsageFreshness.StaleUntil.Sub(now); got != 108*time.Second {
		t.Fatalf("stale deadline = %s, want 108s", got)
	}

	now = now.Add(10 * time.Second)
	unexpectedFreshLoad := errors.New("fresh cache hit loaded origin")
	fresh, err := cache.GetOrLoad(context.Background(), key, false, func(context.Context, bool) (OriginLoadResult, error) {
		return OriginLoadResult{}, unexpectedFreshLoad
	})
	if errors.Is(err, unexpectedFreshLoad) {
		store.mu.Lock()
		values := fmt.Sprintf("%+v", store.values)
		store.mu.Unlock()
		t.Fatalf("fresh cache hit loaded origin; stored values=%s", values)
	}
	if err != nil || fresh.UsageFreshness.CacheStatus != "fresh" {
		t.Fatalf("fresh result=%+v err=%v", fresh, err)
	}

	now = now.Add(18 * time.Second)
	transient := errors.New("synthetic Relay outage")
	stale, err := cache.GetOrLoad(context.Background(), key, true, func(context.Context, bool) (OriginLoadResult, error) {
		calls.Add(1)
		return OriginLoadResult{
			UsageErr: transient,
			Quota: relay.UserUsageGroupQuotaState{
				Status: "unavailable", Groups: []relay.UserUsageGroupQuotaGroupItem{},
			},
			QuotaFreshness: QuotaFreshness{CacheStatus: "uncached", SourceStatus: "error"},
			QuotaLoaded:    true,
		}, nil
	})
	if err != nil {
		t.Fatalf("stale GetOrLoad() error = %v", err)
	}
	if stale.UsageFreshness.CacheStatus != "stale" || stale.UsageFreshness.SourceStatus != "error" {
		t.Fatalf("stale freshness = %+v", stale.UsageFreshness)
	}
	if !stale.QuotaLoaded || stale.Quota.Status != "unavailable" {
		t.Fatalf("stale quota result = %+v", stale)
	}

	now = cold.UsageFreshness.StaleUntil.Add(time.Nanosecond)
	if _, err := cache.GetOrLoad(context.Background(), key, false, func(context.Context, bool) (OriginLoadResult, error) {
		return OriginLoadResult{UsageErr: transient}, nil
	}); !errors.Is(err, transient) {
		t.Fatalf("hard-expired error = %v, want transient origin error", err)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("origin calls = %d, want 2 before hard-expiry loader", got)
	}
}

func TestCacheNeverUsesStaleForInvalidCredentials(t *testing.T) {
	now := time.Date(2026, 7, 15, 8, 0, 0, 0, time.UTC)
	store := newFakeUsageStore(func() time.Time { return now })
	cache := testCache(t, store, func() time.Time { return now }, 0)
	key := testCacheKey()
	if _, err := cache.GetOrLoad(context.Background(), key, false, func(context.Context, bool) (OriginLoadResult, error) {
		return OriginLoadResult{Usage: testUsageSnapshot(1)}, nil
	}); err != nil {
		t.Fatalf("seed cache: %v", err)
	}
	now = now.Add(28 * time.Second)
	if _, err := cache.GetOrLoad(context.Background(), key, false, func(context.Context, bool) (OriginLoadResult, error) {
		return OriginLoadResult{}, relay.ErrInvalidCredentials
	}); !errors.Is(err, relay.ErrInvalidCredentials) {
		t.Fatalf("invalid-credential refresh error = %v", err)
	}
}

func TestCacheRedisFailureStillCollapsesAuthoritativeLoad(t *testing.T) {
	now := time.Date(2026, 7, 15, 8, 0, 0, 0, time.UTC)
	store := newFakeUsageStore(func() time.Time { return now })
	store.getErr = errors.New("synthetic Redis outage")
	cache := testCache(t, store, func() time.Time { return now }, 0)
	var calls atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	loader := func(ctx context.Context, _ bool) (OriginLoadResult, error) {
		if calls.Add(1) == 1 {
			close(started)
		}
		select {
		case <-release:
			return OriginLoadResult{Usage: testUsageSnapshot(2)}, nil
		case <-ctx.Done():
			return OriginLoadResult{}, ctx.Err()
		}
	}

	const callers = 50
	start := make(chan struct{})
	results := make(chan error, callers)
	for i := 0; i < callers; i++ {
		go func() {
			<-start
			result, err := cache.GetOrLoad(context.Background(), testCacheKey(), false, loader)
			if err == nil && (result == nil || result.Usage == nil || result.Usage.Stats.TotalRequests != 2) {
				err = fmt.Errorf("unexpected result: %+v", result)
			}
			results <- err
		}()
	}
	close(start)
	<-started
	time.Sleep(5 * time.Millisecond)
	close(release)
	for i := 0; i < callers; i++ {
		if err := <-results; err != nil {
			t.Fatalf("caller error = %v", err)
		}
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("origin calls = %d, want 1", got)
	}
}

func TestCacheKeyIsolatesAllDimensionsAndNeverSerializesQuota(t *testing.T) {
	base := testCacheKey()
	keys := map[string]struct{}{}
	add := func(key CacheKey) {
		encoded, err := cacheKey("test-blue", key)
		if err != nil {
			t.Fatalf("cacheKey() error = %v", err)
		}
		keys[encoded] = struct{}{}
		if strings.Contains(encoded, "Asia/Shanghai") || strings.Contains(encoded, "2026-07-01") {
			t.Fatalf("cache key exposes raw dimensions: %q", encoded)
		}
	}
	add(base)
	variants := []CacheKey{base, base, base, base, base, base, base, base, base}
	variants[0].ProviderID++
	variants[1].ProviderVersion++
	variants[2].ActorID++
	variants[3].RelayUserID++
	variants[4].BindingVersion++
	variants[5].Params.StartDate = "2026-07-02"
	variants[6].Params.EndDate = "2026-07-14"
	variants[7].Params.Granularity = "hour"
	variants[8].Params.Timezone = "UTC"
	for _, variant := range variants {
		add(variant)
	}
	if len(keys) != 10 {
		t.Fatalf("unique keys = %d, want 10", len(keys))
	}

	now := time.Date(2026, 7, 15, 8, 0, 0, 0, time.UTC)
	store := newFakeUsageStore(func() time.Time { return now })
	cache := testCache(t, store, func() time.Time { return now }, 0)
	result, err := cache.GetOrLoad(context.Background(), base, true, func(context.Context, bool) (OriginLoadResult, error) {
		asOf := now
		return OriginLoadResult{
			Usage: testUsageSnapshot(3),
			Quota: relay.UserUsageGroupQuotaState{
				Status: "ok",
				Groups: []relay.UserUsageGroupQuotaGroupItem{{GroupID: "42", GroupName: "Group Alpha"}},
			},
			QuotaFreshness: QuotaFreshness{AsOf: &asOf, CacheStatus: "uncached", SourceStatus: "ok"},
			QuotaLoaded:    true,
		}, nil
	})
	if err != nil || result == nil || !result.QuotaLoaded {
		t.Fatalf("GetOrLoad() result=%+v err=%v", result, err)
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.values) != 1 {
		t.Fatalf("stored values = %d, want 1", len(store.values))
	}
	for _, value := range store.values {
		encoded := string(value.value)
		if strings.Contains(encoded, "Group Alpha") || strings.Contains(encoded, "group_quotas") || strings.Contains(encoded, "quota_freshness") {
			t.Fatalf("Redis value contains fresh-only quota data: %s", encoded)
		}
		if value.ttl != 108*time.Second {
			t.Fatalf("Redis TTL = %s, want 108s", value.ttl)
		}
	}
}

func TestCacheRefreshesMalformedAndSchemaMismatchedValues(t *testing.T) {
	now := time.Date(2026, 7, 15, 8, 0, 0, 0, time.UTC)
	key := testCacheKey()
	encodedKey, err := cacheKey("test-blue", key)
	if err != nil {
		t.Fatalf("cacheKey() error = %v", err)
	}
	tests := map[string][]byte{
		"malformed":       []byte(`{"schema_version":`),
		"schema mismatch": []byte(`{"schema_version":2}`),
		"unknown field":   []byte(`{"schema_version":1,"unknown":true}`),
	}
	for name, stored := range tests {
		t.Run(name, func(t *testing.T) {
			store := newFakeUsageStore(func() time.Time { return now })
			store.values[encodedKey] = fakeStoreValue{value: stored, ttl: time.Minute}
			cache := testCache(t, store, func() time.Time { return now }, 0)
			var calls atomic.Int32
			result, err := cache.GetOrLoad(context.Background(), key, false, func(context.Context, bool) (OriginLoadResult, error) {
				calls.Add(1)
				return OriginLoadResult{Usage: testUsageSnapshot(17)}, nil
			})
			if err != nil {
				t.Fatalf("GetOrLoad() error = %v", err)
			}
			if calls.Load() != 1 || result.Usage.Stats.TotalRequests != 17 || result.UsageFreshness.CacheStatus != "miss" {
				t.Fatalf("refresh result=%+v calls=%d", result, calls.Load())
			}
		})
	}
}

func TestCacheUsesBothJitterEndpoints(t *testing.T) {
	now := time.Date(2026, 7, 15, 8, 0, 0, 0, time.UTC)
	tests := []struct {
		name        string
		random      float64
		freshWindow time.Duration
		staleWindow time.Duration
	}{
		{name: "minimum jitter", random: 0, freshWindow: 27 * time.Second, staleWindow: 108 * time.Second},
		{name: "maximum jitter", random: 1, freshWindow: 24 * time.Second, staleWindow: 96 * time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newFakeUsageStore(func() time.Time { return now })
			cache := testCache(t, store, func() time.Time { return now }, tt.random)
			result, err := cache.GetOrLoad(context.Background(), testCacheKey(), false, func(context.Context, bool) (OriginLoadResult, error) {
				return OriginLoadResult{Usage: testUsageSnapshot(1)}, nil
			})
			if err != nil {
				t.Fatalf("GetOrLoad() error = %v", err)
			}
			if got := result.UsageFreshness.FreshUntil.Sub(now); got != tt.freshWindow {
				t.Fatalf("fresh window = %s, want %s", got, tt.freshWindow)
			}
			if got := result.UsageFreshness.StaleUntil.Sub(now); got != tt.staleWindow {
				t.Fatalf("stale window = %s, want %s", got, tt.staleWindow)
			}
			store.mu.Lock()
			for _, value := range store.values {
				if value.ttl != tt.staleWindow {
					store.mu.Unlock()
					t.Fatalf("stored TTL = %s, want %s", value.ttl, tt.staleWindow)
				}
			}
			store.mu.Unlock()
		})
	}
}

func TestCacheToleratesSetAndReleaseErrors(t *testing.T) {
	now := time.Date(2026, 7, 15, 8, 0, 0, 0, time.UTC)
	store := newFakeUsageStore(func() time.Time { return now })
	store.setErr = errors.New("synthetic set failure")
	store.releaseErr = errors.New("synthetic release failure")
	cache := testCache(t, store, func() time.Time { return now }, 0)

	result, err := cache.GetOrLoad(context.Background(), testCacheKey(), false, func(context.Context, bool) (OriginLoadResult, error) {
		return OriginLoadResult{Usage: testUsageSnapshot(23)}, nil
	})
	if err != nil {
		t.Fatalf("GetOrLoad() error = %v", err)
	}
	if result.Usage.Stats.TotalRequests != 23 || result.UsageFreshness.CacheStatus != "miss" {
		t.Fatalf("result = %+v", result)
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.setCalls != 1 {
		t.Fatalf("Set calls = %d, want 1", store.setCalls)
	}
}

func TestCacheDoubleChecksAfterLeaseAcquisition(t *testing.T) {
	now := time.Date(2026, 7, 15, 8, 0, 0, 0, time.UTC)
	envelope := usageValueEnvelope{
		SchemaVersion: usageCacheSchemaVersion,
		GeneratedAt:   now,
		FreshUntil:    now.Add(27 * time.Second),
		StaleUntil:    now.Add(108 * time.Second),
		Usage: usagePayload{
			Range: testUsageSnapshot(31).Range, Stats: testUsageSnapshot(31).Stats,
			Trend: []relay.UserUsageTrendPoint{}, Models: []relay.UserUsageModelStat{},
		},
	}
	value, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	store := &injectAfterAcquireStore{fakeUsageStore: newFakeUsageStore(func() time.Time { return now }), value: value}
	cache := testCache(t, store, func() time.Time { return now }, 0)
	result, err := cache.GetOrLoad(context.Background(), testCacheKey(), false, func(context.Context, bool) (OriginLoadResult, error) {
		return OriginLoadResult{}, errors.New("origin must not run after double-check hit")
	})
	if err != nil {
		t.Fatalf("GetOrLoad() error = %v", err)
	}
	if result.Usage.Stats.TotalRequests != 31 || result.UsageFreshness.CacheStatus != "fresh" {
		t.Fatalf("result = %+v", result)
	}
}

func TestCacheCollapsesLoadsAcrossTwoInstances(t *testing.T) {
	now := time.Date(2026, 7, 15, 8, 0, 0, 0, time.UTC)
	store := newFakeUsageStore(func() time.Time { return now })
	first := testCache(t, store, func() time.Time { return now }, 0)
	second := testCache(t, store, func() time.Time { return now }, 0)
	var calls atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	loader := func(ctx context.Context, _ bool) (OriginLoadResult, error) {
		if calls.Add(1) == 1 {
			close(started)
		}
		select {
		case <-release:
			return OriginLoadResult{Usage: testUsageSnapshot(41)}, nil
		case <-ctx.Done():
			return OriginLoadResult{}, ctx.Err()
		}
	}
	results := make(chan *CacheResult, 2)
	errs := make(chan error, 2)
	go func() {
		result, err := first.GetOrLoad(context.Background(), testCacheKey(), false, loader)
		results <- result
		errs <- err
	}()
	<-started
	go func() {
		result, err := second.GetOrLoad(context.Background(), testCacheKey(), false, loader)
		results <- result
		errs <- err
	}()
	waitForCondition(t, time.Second, func() bool { return store.leaseCallCount() > 0 }, "second cache to poll the shared lease")
	close(release)
	for i := 0; i < 2; i++ {
		if err := <-errs; err != nil {
			t.Fatalf("GetOrLoad() error = %v", err)
		}
		result := <-results
		if result.Usage.Stats.TotalRequests != 41 {
			t.Fatalf("result = %+v", result)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("origin calls = %d, want 1", calls.Load())
	}
}

func TestCacheLeaseHolderCancellationLetsAnotherInstanceRefresh(t *testing.T) {
	now := time.Date(2026, 7, 15, 8, 0, 0, 0, time.UTC)
	store := newFakeUsageStore(func() time.Time { return now })
	first := testCache(t, store, func() time.Time { return now }, 0)
	second := testCache(t, store, func() time.Time { return now }, 0)
	firstStarted := make(chan struct{})
	var calls atomic.Int32
	loader := func(ctx context.Context, _ bool) (OriginLoadResult, error) {
		if calls.Add(1) == 1 {
			close(firstStarted)
			<-ctx.Done()
			return OriginLoadResult{}, ctx.Err()
		}
		return OriginLoadResult{Usage: testUsageSnapshot(53)}, nil
	}
	firstCtx, cancelFirst := context.WithCancel(context.Background())
	firstDone := make(chan error, 1)
	go func() {
		_, err := first.GetOrLoad(firstCtx, testCacheKey(), false, loader)
		firstDone <- err
	}()
	<-firstStarted
	secondDone := make(chan error, 1)
	secondResult := make(chan *CacheResult, 1)
	go func() {
		result, err := second.GetOrLoad(context.Background(), testCacheKey(), false, loader)
		secondResult <- result
		secondDone <- err
	}()
	waitForCondition(t, time.Second, func() bool { return store.leaseCallCount() > 0 }, "second cache to wait on the first lease")
	cancelFirst()
	if err := <-firstDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("first error = %v, want canceled", err)
	}
	select {
	case err := <-secondDone:
		if err != nil {
			t.Fatalf("second error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("second cache remained blocked after holder cancellation")
	}
	if result := <-secondResult; result.Usage.Stats.TotalRequests != 53 {
		t.Fatalf("second result = %+v", result)
	}
	if calls.Load() != 2 {
		t.Fatalf("origin calls = %d, want 2", calls.Load())
	}
}

func TestCacheCallerCancellationNeverReturnsStale(t *testing.T) {
	now := time.Date(2026, 7, 15, 8, 0, 0, 0, time.UTC)
	store := newFakeUsageStore(func() time.Time { return now })
	cache := testCache(t, store, func() time.Time { return now }, 0)
	if _, err := cache.GetOrLoad(context.Background(), testCacheKey(), false, func(context.Context, bool) (OriginLoadResult, error) {
		return OriginLoadResult{Usage: testUsageSnapshot(61)}, nil
	}); err != nil {
		t.Fatalf("seed cache: %v", err)
	}
	now = now.Add(28 * time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var calls atomic.Int32
	if _, err := cache.GetOrLoad(ctx, testCacheKey(), false, func(context.Context, bool) (OriginLoadResult, error) {
		calls.Add(1)
		return OriginLoadResult{UsageErr: errors.New("synthetic outage")}, nil
	}); !errors.Is(err, context.Canceled) {
		t.Fatalf("GetOrLoad() error = %v, want canceled", err)
	}
	if calls.Load() != 0 {
		t.Fatalf("origin calls = %d, want 0", calls.Load())
	}
}

func waitForCondition(t *testing.T, timeout time.Duration, condition func() bool, description string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", description)
		}
		time.Sleep(time.Millisecond)
	}
}
