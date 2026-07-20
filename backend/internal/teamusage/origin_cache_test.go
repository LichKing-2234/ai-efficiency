package teamusage

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/internal/readcache"
	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/alicebob/miniredis/v2"
	redis "github.com/redis/go-redis/v9"
)

func TestOriginCacheKeyIsolatesEveryScopeAndRangeDimension(t *testing.T) {
	base := testOriginCacheKey()
	baseEncoded, err := originCacheKey("test", base)
	if err != nil {
		t.Fatalf("originCacheKey() error = %v", err)
	}
	variants := []OriginCacheKey{
		func() OriginCacheKey { key := base; key.ProviderID++; return key }(),
		func() OriginCacheKey { key := base; key.ProviderVersion++; return key }(),
		func() OriginCacheKey { key := base; key.ScopeVersion = "scope-v2"; return key }(),
		func() OriginCacheKey { key := base; key.ScopeHash = "scope-hash-v2"; return key }(),
		func() OriginCacheKey { key := base; key.Params.StartDate = "2026-07-02"; return key }(),
		func() OriginCacheKey { key := base; key.Params.EndDate = "2026-07-08"; return key }(),
		func() OriginCacheKey { key := base; key.Params.Granularity = "hour"; return key }(),
		func() OriginCacheKey { key := base; key.Params.Timezone = "UTC"; return key }(),
	}
	for _, variant := range variants {
		encoded, keyErr := originCacheKey("test", variant)
		if keyErr != nil {
			t.Fatalf("originCacheKey(%+v) error = %v", variant, keyErr)
		}
		if encoded == baseEncoded {
			t.Fatalf("originCacheKey(%+v) reused %q", variant, baseEncoded)
		}
	}
	otherNamespace, err := originCacheKey("other", base)
	if err != nil || otherNamespace == baseEncoded {
		t.Fatalf("namespace key = %q, %v, want isolated from %q", otherNamespace, err, baseEncoded)
	}
}

func TestOriginCachePayloadIsPrivateAndExpiresAfterSixtySeconds(t *testing.T) {
	now := time.Date(2026, 7, 20, 8, 0, 0, 0, time.UTC)
	cache, server := testOriginCache(t, func() time.Time { return now }, "privacy-token")
	key := testOriginCacheKey()
	var loads atomic.Int32
	loader := func(context.Context) (*teamUsageScopeOrigin, error) {
		loads.Add(1)
		return testScopeOrigin(), nil
	}
	if _, err := cache.GetOrLoad(context.Background(), key, loader); err != nil {
		t.Fatalf("cold GetOrLoad() error = %v", err)
	}
	if _, err := cache.GetOrLoad(context.Background(), key, loader); err != nil {
		t.Fatalf("warm GetOrLoad() error = %v", err)
	}
	if loads.Load() != 1 {
		t.Fatalf("origin loads = %d, want 1", loads.Load())
	}
	encodedKey, _ := originCacheKey("test", key)
	stored, err := server.Get(encodedKey)
	if err != nil {
		t.Fatalf("read stored scope origin: %v", err)
	}
	for _, required := range []string{`"relay_user_ids"`, `"stats_by_relay_user_id"`, `"points_by_user"`} {
		if !strings.Contains(stored, required) {
			t.Fatalf("scope origin payload missing %s: %s", required, stored)
		}
	}
	for _, forbidden := range []string{
		`"email"`, `"username"`, `"password"`, `"credential"`, `"display_name"`,
		`"summary"`, `"top_members"`, `"department_trend"`, `"departments"`, `"members"`,
	} {
		if strings.Contains(stored, forbidden) {
			t.Fatalf("scope origin payload contains forbidden field %s: %s", forbidden, stored)
		}
	}
	if ttl := server.TTL(encodedKey); ttl != time.Minute {
		t.Fatalf("scope origin TTL = %s, want 1m", ttl)
	}

	now = now.Add(time.Minute)
	if _, err := cache.GetOrLoad(context.Background(), key, loader); err != nil {
		t.Fatalf("expired GetOrLoad() error = %v", err)
	}
	if loads.Load() != 2 {
		t.Fatalf("origin loads after exactly 60 seconds = %d, want 2", loads.Load())
	}
}

func TestOriginCacheMalformedAndOversizedValuesReload(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{name: "malformed", value: `{"schema_version":1`},
		{name: "oversized", value: strings.Repeat("x", scopeOriginPayloadMaxBytes+1)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cache, server := testOriginCache(t, time.Now, test.name+"-token")
			key := testOriginCacheKey()
			encodedKey, _ := originCacheKey("test", key)
			server.Set(encodedKey, test.value)
			server.SetTTL(encodedKey, time.Minute)
			var loads atomic.Int32
			result, err := cache.GetOrLoad(context.Background(), key, func(context.Context) (*teamUsageScopeOrigin, error) {
				loads.Add(1)
				return testScopeOrigin(), nil
			})
			if err != nil || result == nil {
				t.Fatalf("GetOrLoad() result=%#v error=%v", result, err)
			}
			if loads.Load() != 1 {
				t.Fatalf("reloads = %d, want 1", loads.Load())
			}
			stored, getErr := server.Get(encodedKey)
			if getErr != nil || len(stored) > scopeOriginPayloadMaxBytes || !strings.Contains(stored, `"relay_user_ids"`) {
				t.Fatalf("replacement bytes=%d error=%v payload=%q", len(stored), getErr, stored)
			}
		})
	}
}

func TestOriginPayloadBoundSkipsOversizedWrites(t *testing.T) {
	if scopeOriginPayloadMaxBytes != 2<<20 {
		t.Fatalf("scopeOriginPayloadMaxBytes = %d, want repository-aligned 2 MiB bound", scopeOriginPayloadMaxBytes)
	}
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	store := &countingOriginSetStore{Store: readcache.NewRedisStore(client)}
	cache := newTestOriginCache(t, store, time.Now, "bound-token")
	origin := testScopeOrigin()
	tokens := int64(1)
	points := make([]relay.UsageTrendPoint, 0, 40000)
	for index := 0; index < cap(points); index++ {
		points = append(points, relay.UsageTrendPoint{
			Date: fmt.Sprintf("2026-07-%06d", index), ActualCost: 1, TotalTokens: &tokens,
		})
	}
	origin.PointsByUser[101] = points
	var loads atomic.Int32
	loader := func(context.Context) (*teamUsageScopeOrigin, error) {
		loads.Add(1)
		return origin, nil
	}
	for attempt := 0; attempt < 2; attempt++ {
		if _, err := cache.GetOrLoad(context.Background(), testOriginCacheKey(), loader); err != nil {
			t.Fatalf("oversized GetOrLoad(%d) error = %v", attempt, err)
		}
	}
	if store.setCalls.Load() != 0 || loads.Load() != 2 {
		t.Fatalf("oversized writes/loads = %d/%d, want 0/2", store.setCalls.Load(), loads.Load())
	}
}

func TestOriginCacheRedisErrorsFailOpenAndSourceErrorsAreNotCached(t *testing.T) {
	t.Run("Redis failure", func(t *testing.T) {
		cache := newTestOriginCache(t, failingSnapshotStore{err: errors.New("Redis unavailable")}, time.Now, "fail-open-token")
		var loads atomic.Int32
		result, err := cache.GetOrLoad(context.Background(), testOriginCacheKey(), func(context.Context) (*teamUsageScopeOrigin, error) {
			loads.Add(1)
			return testScopeOrigin(), nil
		})
		if err != nil || result == nil || loads.Load() != 1 {
			t.Fatalf("fail-open result=%#v loads=%d error=%v", result, loads.Load(), err)
		}
	})

	t.Run("soft source failure", func(t *testing.T) {
		cache, server := testOriginCache(t, time.Now, "source-error-token")
		origin := testScopeOrigin()
		origin.sourceErr = errors.New("synthetic users-trend outage")
		var loads atomic.Int32
		for attempt := 0; attempt < 2; attempt++ {
			result, err := cache.GetOrLoad(context.Background(), testOriginCacheKey(), func(context.Context) (*teamUsageScopeOrigin, error) {
				loads.Add(1)
				return origin, nil
			})
			if err != nil || result == nil {
				t.Fatalf("soft source GetOrLoad(%d) result=%#v error=%v", attempt, result, err)
			}
		}
		if loads.Load() != 2 || len(server.Keys()) != 0 {
			t.Fatalf("soft source loads/keys = %d/%v, want 2/no cached value", loads.Load(), server.Keys())
		}
	})
}

func TestOriginCacheCrossPodLeaseCollapsesOneLoad(t *testing.T) {
	server := miniredis.RunT(t)
	clientA := redis.NewClient(&redis.Options{Addr: server.Addr()})
	clientB := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = clientA.Close(); _ = clientB.Close() })
	cacheA := newTestOriginCache(t, readcache.NewRedisStore(clientA), time.Now, "pod-a-token")
	cacheB := newTestOriginCache(t, readcache.NewRedisStore(clientB), time.Now, "pod-b-token")

	started := make(chan struct{})
	release := make(chan struct{})
	var loads atomic.Int32
	loader := func(ctx context.Context) (*teamUsageScopeOrigin, error) {
		if loads.Add(1) == 1 {
			close(started)
		}
		select {
		case <-release:
			return testScopeOrigin(), nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	results := make(chan error, 2)
	go func() { _, err := cacheA.GetOrLoad(context.Background(), testOriginCacheKey(), loader); results <- err }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("first Pod did not start the origin")
	}
	go func() { _, err := cacheB.GetOrLoad(context.Background(), testOriginCacheKey(), loader); results <- err }()
	time.Sleep(25 * time.Millisecond)
	close(release)
	for index := 0; index < 2; index++ {
		if err := <-results; err != nil {
			t.Fatalf("cross-Pod GetOrLoad() error = %v", err)
		}
	}
	if loads.Load() != 1 {
		t.Fatalf("cross-Pod origin loads = %d, want 1", loads.Load())
	}
}

func testOriginCache(t *testing.T, now func() time.Time, token string) (*OriginCache, *miniredis.Miniredis) {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return newTestOriginCache(t, readcache.NewRedisStore(client), now, token), server
}

func newTestOriginCache(t *testing.T, store readcache.Store, now func() time.Time, token string) *OriginCache {
	t.Helper()
	cache, err := NewOriginCache(store, OriginCacheOptions{
		Namespace: "test", CommandTimeout: time.Second, RefreshTimeout: 2 * time.Second,
		LeaseTTL: time.Second, PollInterval: 5 * time.Millisecond, ReleaseTimeout: time.Second,
		Now: now, NewToken: func() string { return token },
	})
	if err != nil {
		t.Fatalf("NewOriginCache() error = %v", err)
	}
	return cache
}

func testOriginCacheKey() OriginCacheKey {
	return OriginCacheKey{
		ProviderID: 3, ProviderVersion: 7, ScopeVersion: "scope-v1", ScopeHash: "scope-hash-v1",
		Params: OverviewParams{StartDate: "2026-07-01", EndDate: "2026-07-07", Granularity: "day", Timezone: "Asia/Shanghai"},
	}
}

func testScopeOrigin() *teamUsageScopeOrigin {
	tokens101, tokens102 := int64(10), int64(20)
	cost101, cost102 := 1.0, 2.0
	return &teamUsageScopeOrigin{
		RelayUserIDs: []int64{101, 102},
		StatsByRelayUserID: map[int64]relay.TeamUserUsageStats{
			101: {UserID: 101, RangeActualCost: &cost101, RangeTotalTokens: &tokens101},
			102: {UserID: 102, RangeActualCost: &cost102, RangeTotalTokens: &tokens102},
		},
		PointsByUser: map[int64][]relay.UsageTrendPoint{
			101: {{Date: "2026-07-01", ActualCost: 1, TotalTokens: &tokens101}},
			102: {{Date: "2026-07-01", ActualCost: 2, TotalTokens: &tokens102}},
		},
	}
}

type countingOriginSetStore struct {
	readcache.Store
	setCalls atomic.Int32
}

func (s *countingOriginSetStore) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	s.setCalls.Add(1)
	return s.Store.Set(ctx, key, value, ttl)
}
