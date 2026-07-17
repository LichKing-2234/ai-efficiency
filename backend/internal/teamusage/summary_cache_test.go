package teamusage

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/internal/readcache"
	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/ai-efficiency/backend/internal/representativescope"
	"github.com/alicebob/miniredis/v2"
	redis "github.com/redis/go-redis/v9"
)

func TestSnapshotCacheKeyIsolatesEveryAuthoritativeDimension(t *testing.T) {
	base := testSnapshotCacheKey()
	baseEncoded, err := snapshotCacheKey("test", base)
	if err != nil {
		t.Fatalf("snapshotCacheKey() error = %v", err)
	}
	variants := []SnapshotCacheKey{
		func() SnapshotCacheKey { key := base; key.ProviderID++; return key }(),
		func() SnapshotCacheKey { key := base; key.ProviderVersion++; return key }(),
		func() SnapshotCacheKey { key := base; key.ActorID++; return key }(),
		func() SnapshotCacheKey { key := base; key.ScopeVersion = "scope-v2"; return key }(),
		func() SnapshotCacheKey { key := base; key.ScopeHash = "scope-hash-v2"; return key }(),
		func() SnapshotCacheKey { key := base; key.Params.StartDate = "2026-07-02"; return key }(),
		func() SnapshotCacheKey { key := base; key.Params.EndDate = "2026-07-08"; return key }(),
		func() SnapshotCacheKey { key := base; key.Params.Granularity = "hour"; return key }(),
		func() SnapshotCacheKey { key := base; key.Params.Timezone = "UTC"; return key }(),
	}
	for _, variant := range variants {
		encoded, keyErr := snapshotCacheKey("test", variant)
		if keyErr != nil {
			t.Fatalf("snapshotCacheKey(%+v) error = %v", variant, keyErr)
		}
		if encoded == baseEncoded {
			t.Fatalf("snapshotCacheKey(%+v) reused %q", variant, baseEncoded)
		}
	}

	pageVariant := base
	pageVariant.Params.Page = 9
	pageVariant.Params.PageSize = 99
	pageEncoded, err := snapshotCacheKey("test", pageVariant)
	if err != nil {
		t.Fatalf("page snapshotCacheKey() error = %v", err)
	}
	if pageEncoded != baseEncoded {
		t.Fatalf("legacy ineffective page params changed snapshot key: %q != %q", pageEncoded, baseEncoded)
	}
}

func TestEffectiveScopeHashIsDeterministicAndContentSensitive(t *testing.T) {
	first := testEffectiveScope()
	second := testEffectiveScope()
	second.RepresentedSubtreeIDs = map[string]map[string]struct{}{
		"department-beta":  {"department-beta": {}},
		"department-alpha": {"department-alpha": {}, "department-beta": {}},
	}
	firstHash, err := effectiveScopeHash(first)
	if err != nil {
		t.Fatalf("effectiveScopeHash(first) error = %v", err)
	}
	secondHash, err := effectiveScopeHash(second)
	if err != nil {
		t.Fatalf("effectiveScopeHash(second) error = %v", err)
	}
	if firstHash == "" || firstHash != secondHash {
		t.Fatalf("equivalent scope hashes = %q/%q, want same non-empty hash", firstHash, secondHash)
	}

	second.OverviewSubjects[0].RelayUserID = intPtr(2002)
	changedHash, err := effectiveScopeHash(second)
	if err != nil {
		t.Fatalf("effectiveScopeHash(changed) error = %v", err)
	}
	if changedHash == firstHash {
		t.Fatalf("changed effective scope reused hash %q", firstHash)
	}
}

func TestSnapshotCacheColdMissWarmHitAndJitterBounds(t *testing.T) {
	now := time.Date(2026, 7, 16, 8, 0, 0, 0, time.UTC)
	tests := []struct {
		name        string
		random      float64
		freshWindow time.Duration
		staleWindow time.Duration
	}{
		{name: "minimum jitter", random: 0, freshWindow: 54 * time.Second, staleWindow: 4*time.Minute + 30*time.Second},
		{name: "maximum jitter", random: 1, freshWindow: 48 * time.Second, staleWindow: 4 * time.Minute},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cache, server := testSnapshotCache(t, now, tt.random)
			var loads atomic.Int32
			loader := func(context.Context) (SnapshotOriginLoadResult, error) {
				loads.Add(1)
				return SnapshotOriginLoadResult{Snapshot: testOverviewSnapshot(12.5)}, nil
			}
			first, err := cache.GetOrLoad(context.Background(), testSnapshotCacheKey(), loader)
			if err != nil {
				t.Fatalf("cold GetOrLoad() error = %v", err)
			}
			second, err := cache.GetOrLoad(context.Background(), testSnapshotCacheKey(), loader)
			if err != nil {
				t.Fatalf("warm GetOrLoad() error = %v", err)
			}
			if loads.Load() != 1 || first.Freshness.CacheStatus != "miss" || second.Freshness.CacheStatus != "fresh" {
				t.Fatalf("loads/status = %d %q/%q", loads.Load(), first.Freshness.CacheStatus, second.Freshness.CacheStatus)
			}
			if got := first.Freshness.FreshUntil.Sub(now); got != tt.freshWindow {
				t.Fatalf("fresh window = %s, want %s", got, tt.freshWindow)
			}
			if got := first.Freshness.StaleUntil.Sub(now); got != tt.staleWindow {
				t.Fatalf("stale window = %s, want %s", got, tt.staleWindow)
			}
			keys := server.Keys()
			if len(keys) != 1 || server.TTL(keys[0]) != tt.staleWindow {
				t.Fatalf("stored keys/TTL = %v/%s, want one value with %s", keys, server.TTL(keys[0]), tt.staleWindow)
			}
			stored, getErr := server.Get(keys[0])
			if getErr != nil {
				t.Fatalf("read stored snapshot: %v", getErr)
			}
			if strings.Contains(stored, "request_id") || strings.Contains(stored, "scope_version") {
				t.Fatalf("request-local metadata leaked into cached snapshot: %s", stored)
			}
		})
	}
}

func TestSnapshotCacheUsesEligibleStaleButRejectsHardFailures(t *testing.T) {
	now := time.Date(2026, 7, 16, 8, 0, 0, 0, time.UTC)
	cache, _ := testSnapshotCacheWithClock(t, func() time.Time { return now }, 0)
	key := testSnapshotCacheKey()
	if _, err := cache.GetOrLoad(context.Background(), key, func(context.Context) (SnapshotOriginLoadResult, error) {
		return SnapshotOriginLoadResult{Snapshot: testOverviewSnapshot(7.5)}, nil
	}); err != nil {
		t.Fatalf("prime cache: %v", err)
	}

	now = now.Add(55 * time.Second)
	transient := errors.New("synthetic Relay outage")
	stale, err := cache.GetOrLoad(context.Background(), key, func(context.Context) (SnapshotOriginLoadResult, error) {
		return SnapshotOriginLoadResult{SnapshotErr: transient}, nil
	})
	if err != nil {
		t.Fatalf("eligible stale GetOrLoad() error = %v", err)
	}
	if stale.Snapshot.Summary.RangeActualCost == nil || *stale.Snapshot.Summary.RangeActualCost != 7.5 || stale.Freshness.CacheStatus != "stale" || stale.Freshness.SourceStatus != "error" {
		t.Fatalf("stale result = %+v", stale)
	}

	if _, err := cache.GetOrLoad(context.Background(), key, func(context.Context) (SnapshotOriginLoadResult, error) {
		return SnapshotOriginLoadResult{}, relay.ErrInvalidCredentials
	}); !errors.Is(err, relay.ErrInvalidCredentials) {
		t.Fatalf("hard error = %v, want ErrInvalidCredentials", err)
	}

	now = now.Add(4*time.Minute + 16*time.Second)
	if _, err := cache.GetOrLoad(context.Background(), key, func(context.Context) (SnapshotOriginLoadResult, error) {
		return SnapshotOriginLoadResult{SnapshotErr: transient}, nil
	}); !errors.Is(err, transient) {
		t.Fatalf("expired stale error = %v, want transient origin error", err)
	}
}

func TestSnapshotCacheRedisFailureFallsBackAndStillCollapsesLocally(t *testing.T) {
	store := failingSnapshotStore{err: errors.New("synthetic Redis outage")}
	cache, err := NewSnapshotCache(store, SnapshotCacheOptions{Namespace: "test"})
	if err != nil {
		t.Fatalf("NewSnapshotCache() error = %v", err)
	}
	var loads atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	loader := func(ctx context.Context) (SnapshotOriginLoadResult, error) {
		if loads.Add(1) == 1 {
			close(started)
		}
		select {
		case <-release:
			return SnapshotOriginLoadResult{Snapshot: testOverviewSnapshot(9)}, nil
		case <-ctx.Done():
			return SnapshotOriginLoadResult{}, ctx.Err()
		}
	}

	const callers = 50
	var wg sync.WaitGroup
	errs := make(chan error, callers)
	start := make(chan struct{})
	for index := 0; index < callers; index++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			result, loadErr := cache.GetOrLoad(context.Background(), testSnapshotCacheKey(), loader)
			if loadErr == nil && (result == nil || result.Snapshot == nil || result.Freshness.CacheStatus != "miss") {
				loadErr = errors.New("unexpected authoritative fallback result")
			}
			errs <- loadErr
		}()
	}
	close(start)
	<-started
	time.Sleep(5 * time.Millisecond)
	close(release)
	wg.Wait()
	close(errs)
	for loadErr := range errs {
		if loadErr != nil {
			t.Fatalf("GetOrLoad() error = %v", loadErr)
		}
	}
	if loads.Load() != 1 {
		t.Fatalf("authoritative loads = %d, want 1", loads.Load())
	}
}

func TestSnapshotCacheCollapsesRefreshAcrossInstances(t *testing.T) {
	now := time.Date(2026, 7, 16, 8, 0, 0, 0, time.UTC)
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	store := readcache.NewRedisStore(client)
	first := newTestSnapshotCache(t, store, func() time.Time { return now }, 0)
	second := newTestSnapshotCache(t, store, func() time.Time { return now }, 0)

	var loads atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	loader := func(ctx context.Context) (SnapshotOriginLoadResult, error) {
		if loads.Add(1) == 1 {
			close(started)
		}
		select {
		case <-release:
			return SnapshotOriginLoadResult{Snapshot: testOverviewSnapshot(11)}, nil
		case <-ctx.Done():
			return SnapshotOriginLoadResult{}, ctx.Err()
		}
	}

	type outcome struct {
		result *SnapshotCacheResult
		err    error
	}
	outcomes := make(chan outcome, 2)
	go func() {
		result, loadErr := first.GetOrLoad(context.Background(), testSnapshotCacheKey(), loader)
		outcomes <- outcome{result: result, err: loadErr}
	}()
	<-started
	go func() {
		result, loadErr := second.GetOrLoad(context.Background(), testSnapshotCacheKey(), loader)
		outcomes <- outcome{result: result, err: loadErr}
	}()
	time.Sleep(50 * time.Millisecond)
	close(release)
	for index := 0; index < 2; index++ {
		got := <-outcomes
		if got.err != nil || got.result == nil || got.result.Snapshot == nil {
			t.Fatalf("outcome = %+v", got)
		}
	}
	if loads.Load() != 1 {
		t.Fatalf("authoritative loads = %d, want 1", loads.Load())
	}
}

func TestSnapshotCacheTreatsMalformedValueAsMiss(t *testing.T) {
	now := time.Date(2026, 7, 16, 8, 0, 0, 0, time.UTC)
	cache, server := testSnapshotCache(t, now, 0)
	key, err := snapshotCacheKey("test", testSnapshotCacheKey())
	if err != nil {
		t.Fatalf("snapshotCacheKey() error = %v", err)
	}
	server.Set(key, `{"schema_version":999,"snapshot":null}`)
	var loads atomic.Int32
	result, err := cache.GetOrLoad(context.Background(), testSnapshotCacheKey(), func(context.Context) (SnapshotOriginLoadResult, error) {
		loads.Add(1)
		return SnapshotOriginLoadResult{Snapshot: testOverviewSnapshot(13)}, nil
	})
	if err != nil {
		t.Fatalf("GetOrLoad() error = %v", err)
	}
	if loads.Load() != 1 || result.Freshness.CacheStatus != "miss" {
		t.Fatalf("loads/status = %d/%q, want 1/miss", loads.Load(), result.Freshness.CacheStatus)
	}
}

func TestSnapshotCacheRejectsPreviousOverviewSchema(t *testing.T) {
	now := time.Date(2026, 7, 16, 8, 0, 0, 0, time.UTC)
	cache, server := testSnapshotCache(t, now, 0)
	key, err := snapshotCacheKey("test", testSnapshotCacheKey())
	if err != nil {
		t.Fatalf("snapshotCacheKey() error = %v", err)
	}
	encoded, err := json.Marshal(snapshotValueEnvelope{
		SchemaVersion: 1,
		GeneratedAt:   now,
		FreshUntil:    now.Add(54 * time.Second),
		StaleUntil:    now.Add(4*time.Minute + 30*time.Second),
		Snapshot:      testOverviewSnapshot(12),
	})
	if err != nil {
		t.Fatalf("encode previous snapshot schema: %v", err)
	}
	server.Set(key, string(encoded))

	var loads atomic.Int32
	result, err := cache.GetOrLoad(context.Background(), testSnapshotCacheKey(), func(context.Context) (SnapshotOriginLoadResult, error) {
		loads.Add(1)
		return SnapshotOriginLoadResult{Snapshot: testOverviewSnapshot(13)}, nil
	})
	if err != nil {
		t.Fatalf("GetOrLoad() error = %v", err)
	}
	if loads.Load() != 1 || result.Freshness.CacheStatus != "miss" || *result.Snapshot.Summary.RangeActualCost != 13 {
		t.Fatalf("loads/status/range = %d/%q/%v, want 1/miss/13", loads.Load(), result.Freshness.CacheStatus, result.Snapshot.Summary.RangeActualCost)
	}
}

func TestSummaryCacheKeyIsolatedFromOverviewLane(t *testing.T) {
	overviewKey, err := snapshotCacheKey("test", testSnapshotCacheKey())
	if err != nil {
		t.Fatalf("snapshotCacheKey() error = %v", err)
	}
	summaryKey, err := summaryCacheKey("test", testSnapshotCacheKey())
	if err != nil {
		t.Fatalf("summaryCacheKey() error = %v", err)
	}
	if overviewKey == summaryKey {
		t.Fatal("summary and overview cache keys must be isolated")
	}
	if !strings.HasPrefix(overviewKey, "ae:test:team-usage-snapshot:v1:") {
		t.Fatalf("overview cache key = %q, want unchanged prefix", overviewKey)
	}
	if !strings.HasPrefix(summaryKey, "ae:test:team-usage-summary:v1:") {
		t.Fatalf("summary cache key = %q, want summary prefix", summaryKey)
	}
}

func TestSummaryCacheColdMissWarmHitAndStoresOnlySummarySnapshot(t *testing.T) {
	now := time.Date(2026, 7, 16, 8, 0, 0, 0, time.UTC)
	cache, server := testSnapshotCache(t, now, 0)
	var loads atomic.Int32
	loader := func(context.Context) (SummaryOriginLoadResult, error) {
		loads.Add(1)
		return SummaryOriginLoadResult{Snapshot: testSummarySnapshot(12.5)}, nil
	}

	first, err := cache.GetSummaryOrLoad(context.Background(), testSnapshotCacheKey(), loader)
	if err != nil {
		t.Fatalf("cold GetSummaryOrLoad() error = %v", err)
	}
	second, err := cache.GetSummaryOrLoad(context.Background(), testSnapshotCacheKey(), loader)
	if err != nil {
		t.Fatalf("warm GetSummaryOrLoad() error = %v", err)
	}
	if loads.Load() != 1 || first.Freshness.CacheStatus != "miss" || second.Freshness.CacheStatus != "fresh" {
		t.Fatalf("loads/status = %d %q/%q, want 1 miss/fresh", loads.Load(), first.Freshness.CacheStatus, second.Freshness.CacheStatus)
	}
	if first.Snapshot.Summary.RangeActualCost == nil || *first.Snapshot.Summary.RangeActualCost != 12.5 {
		t.Fatalf("summary snapshot = %+v, want range_actual_cost 12.5", first.Snapshot)
	}

	key, err := summaryCacheKey("test", testSnapshotCacheKey())
	if err != nil {
		t.Fatalf("summaryCacheKey() error = %v", err)
	}
	stored, err := server.Get(key)
	if err != nil {
		t.Fatalf("read stored summary snapshot: %v", err)
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal([]byte(stored), &envelope); err != nil {
		t.Fatalf("decode stored summary envelope: %v", err)
	}
	assertJSONFields(t, envelope, "schema_version", "generated_at", "fresh_until", "stale_until", "snapshot")
	var snapshot map[string]json.RawMessage
	if err := json.Unmarshal(envelope["snapshot"], &snapshot); err != nil {
		t.Fatalf("decode stored summary snapshot: %v", err)
	}
	assertJSONFields(t, snapshot, "window", "summary")
}

func TestSummaryCacheUsesEligibleStaleAndRejectsHardStale(t *testing.T) {
	now := time.Date(2026, 7, 16, 8, 0, 0, 0, time.UTC)
	cache, _ := testSnapshotCacheWithClock(t, func() time.Time { return now }, 0)
	key := testSnapshotCacheKey()
	if _, err := cache.GetSummaryOrLoad(context.Background(), key, func(context.Context) (SummaryOriginLoadResult, error) {
		return SummaryOriginLoadResult{Snapshot: testSummarySnapshot(7.5)}, nil
	}); err != nil {
		t.Fatalf("prime summary cache: %v", err)
	}

	now = now.Add(55 * time.Second)
	transient := errors.New("synthetic Relay outage")
	stale, err := cache.GetSummaryOrLoad(context.Background(), key, func(context.Context) (SummaryOriginLoadResult, error) {
		return SummaryOriginLoadResult{SnapshotErr: transient}, nil
	})
	if err != nil {
		t.Fatalf("eligible stale GetSummaryOrLoad() error = %v", err)
	}
	if stale.Snapshot.Summary.RangeActualCost == nil || *stale.Snapshot.Summary.RangeActualCost != 7.5 ||
		stale.Freshness.CacheStatus != "stale" || stale.Freshness.SourceStatus != "error" {
		t.Fatalf("stale summary result = %+v", stale)
	}

	if _, err := cache.GetSummaryOrLoad(context.Background(), key, func(context.Context) (SummaryOriginLoadResult, error) {
		return SummaryOriginLoadResult{}, relay.ErrInvalidCredentials
	}); !errors.Is(err, relay.ErrInvalidCredentials) {
		t.Fatalf("hard error = %v, want ErrInvalidCredentials", err)
	}

	now = now.Add(4*time.Minute + 16*time.Second)
	if _, err := cache.GetSummaryOrLoad(context.Background(), key, func(context.Context) (SummaryOriginLoadResult, error) {
		return SummaryOriginLoadResult{SnapshotErr: transient}, nil
	}); !errors.Is(err, transient) {
		t.Fatalf("expired stale error = %v, want transient origin error", err)
	}
}

func TestSummaryCacheRejectsMalformedAndOverviewShapedValues(t *testing.T) {
	now := time.Date(2026, 7, 16, 8, 0, 0, 0, time.UTC)
	tests := []struct {
		name  string
		value func(t *testing.T, cache *SnapshotCache, server *miniredis.Miniredis, key string) string
	}{
		{
			name: "malformed",
			value: func(*testing.T, *SnapshotCache, *miniredis.Miniredis, string) string {
				return `{"schema_version":999,"snapshot":null}`
			},
		},
		{
			name: "overview shaped",
			value: func(t *testing.T, cache *SnapshotCache, server *miniredis.Miniredis, key string) string {
				t.Helper()
				if _, err := cache.GetSummaryOrLoad(context.Background(), testSnapshotCacheKey(), func(context.Context) (SummaryOriginLoadResult, error) {
					return SummaryOriginLoadResult{Snapshot: testSummarySnapshot(12)}, nil
				}); err != nil {
					t.Fatalf("prime summary cache: %v", err)
				}
				stored, err := server.Get(key)
				if err != nil {
					t.Fatalf("read stored summary snapshot: %v", err)
				}
				var envelope map[string]json.RawMessage
				if err := json.Unmarshal([]byte(stored), &envelope); err != nil {
					t.Fatalf("decode stored summary envelope: %v", err)
				}
				overview, err := json.Marshal(testOverviewSnapshot(12))
				if err != nil {
					t.Fatalf("encode overview snapshot: %v", err)
				}
				envelope["snapshot"] = overview
				encoded, err := json.Marshal(envelope)
				if err != nil {
					t.Fatalf("encode overview-shaped envelope: %v", err)
				}
				return string(encoded)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cache, server := testSnapshotCache(t, now, 0)
			key, err := summaryCacheKey("test", testSnapshotCacheKey())
			if err != nil {
				t.Fatalf("summaryCacheKey() error = %v", err)
			}
			server.Set(key, tt.value(t, cache, server, key))
			var loads atomic.Int32
			result, err := cache.GetSummaryOrLoad(context.Background(), testSnapshotCacheKey(), func(context.Context) (SummaryOriginLoadResult, error) {
				loads.Add(1)
				return SummaryOriginLoadResult{Snapshot: testSummarySnapshot(13)}, nil
			})
			if err != nil {
				t.Fatalf("GetSummaryOrLoad() error = %v", err)
			}
			if loads.Load() != 1 || result.Freshness.CacheStatus != "miss" ||
				result.Snapshot.Summary.RangeActualCost == nil || *result.Snapshot.Summary.RangeActualCost != 13 {
				t.Fatalf("loads/result = %d/%+v, want one fresh authoritative load", loads.Load(), result)
			}
		})
	}
}

func TestSummaryCacheRedisOutageFallsBackAuthoritatively(t *testing.T) {
	cache := newTestSnapshotCache(t, failingSnapshotStore{err: errors.New("synthetic Redis outage")}, time.Now, 0)
	var loads atomic.Int32
	loader := func(context.Context) (SummaryOriginLoadResult, error) {
		loads.Add(1)
		return SummaryOriginLoadResult{Snapshot: testSummarySnapshot(9)}, nil
	}
	for index := 0; index < 2; index++ {
		result, err := cache.GetSummaryOrLoad(context.Background(), testSnapshotCacheKey(), loader)
		if err != nil {
			t.Fatalf("GetSummaryOrLoad() call %d error = %v", index+1, err)
		}
		if result.Freshness.CacheStatus != "miss" {
			t.Fatalf("GetSummaryOrLoad() call %d cache status = %q, want miss", index+1, result.Freshness.CacheStatus)
		}
	}
	if loads.Load() != 2 {
		t.Fatalf("authoritative loads = %d, want one per Redis-outage request", loads.Load())
	}
}

func TestSummaryCacheCallerCancellationStopsFinalLoader(t *testing.T) {
	cache, _ := testSnapshotCache(t, time.Now(), 0)
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	loaderCancelled := make(chan struct{})
	result := make(chan error, 1)
	go func() {
		_, err := cache.GetSummaryOrLoad(ctx, testSnapshotCacheKey(), func(ctx context.Context) (SummaryOriginLoadResult, error) {
			close(started)
			<-ctx.Done()
			close(loaderCancelled)
			return SummaryOriginLoadResult{}, ctx.Err()
		})
		result <- err
	}()
	<-started
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("GetSummaryOrLoad() error = %v, want context.Canceled", err)
	}
	select {
	case <-loaderCancelled:
	case <-time.After(time.Second):
		t.Fatal("final waiter cancellation did not stop the shared summary loader")
	}
}

func TestSummaryCacheCollapsesProcessLocalLoads(t *testing.T) {
	cache := newTestSnapshotCache(t, failingSnapshotStore{err: errors.New("synthetic Redis outage")}, time.Now, 0)
	var loads atomic.Int32
	started := make(chan struct{}, 20)
	release := make(chan struct{})
	loader := func(ctx context.Context) (SummaryOriginLoadResult, error) {
		loads.Add(1)
		started <- struct{}{}
		select {
		case <-release:
			return SummaryOriginLoadResult{Snapshot: testSummarySnapshot(11)}, nil
		case <-ctx.Done():
			return SummaryOriginLoadResult{}, ctx.Err()
		}
	}

	const callers = 20
	results := make(chan error, callers)
	var wg sync.WaitGroup
	call := func(ctx context.Context) {
		defer wg.Done()
		result, err := cache.GetSummaryOrLoad(ctx, testSnapshotCacheKey(), loader)
		if err == nil && (result == nil || result.Snapshot == nil) {
			err = errors.New("missing summary cache result")
		}
		results <- err
	}

	wg.Add(1)
	go call(context.Background())
	<-started
	for index := 1; index < callers; index++ {
		ctx := &observedWaitContext{Context: context.Background(), waitStarted: make(chan struct{})}
		wg.Add(1)
		go call(ctx)
		<-ctx.waitStarted
	}
	close(release)
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatalf("GetSummaryOrLoad() error = %v", err)
		}
	}
	if loads.Load() != 1 {
		t.Fatalf("authoritative loads = %d, want 1", loads.Load())
	}
}

type observedWaitContext struct {
	context.Context
	waitStarted chan struct{}
	once        sync.Once
}

func (c *observedWaitContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.waitStarted) })
	return c.Context.Done()
}

func assertJSONFields(t *testing.T, value map[string]json.RawMessage, fields ...string) {
	t.Helper()
	if len(value) != len(fields) {
		t.Fatalf("JSON fields = %v, want exactly %v", value, fields)
	}
	for _, field := range fields {
		if _, ok := value[field]; !ok {
			t.Fatalf("JSON fields = %v, missing %q", value, field)
		}
	}
}

func testSnapshotCache(t *testing.T, now time.Time, random float64) (*SnapshotCache, *miniredis.Miniredis) {
	t.Helper()
	return testSnapshotCacheWithClock(t, func() time.Time { return now }, random)
}

func testSnapshotCacheWithClock(t *testing.T, now func() time.Time, random float64) (*SnapshotCache, *miniredis.Miniredis) {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return newTestSnapshotCache(t, readcache.NewRedisStore(client), now, random), server
}

func newTestSnapshotCache(t *testing.T, store readcache.Store, now func() time.Time, random float64) *SnapshotCache {
	t.Helper()
	cache, err := NewSnapshotCache(store, SnapshotCacheOptions{
		Namespace: "test", CommandTimeout: time.Second, RefreshTimeout: 2 * time.Second,
		LeaseTTL: time.Second, PollInterval: 5 * time.Millisecond, ReleaseTimeout: time.Second,
		Now: now, RandFloat64: func() float64 { return random }, NewToken: func() string { return "test-token" },
	})
	if err != nil {
		t.Fatalf("NewSnapshotCache() error = %v", err)
	}
	return cache
}

func testSnapshotCacheKey() SnapshotCacheKey {
	return SnapshotCacheKey{
		ProviderID: 3, ProviderVersion: 7, ActorID: 11,
		ScopeVersion: "scope-v1", ScopeHash: "scope-hash-v1",
		Params: OverviewParams{StartDate: "2026-07-01", EndDate: "2026-07-07", Granularity: "day", Timezone: "Asia/Shanghai"},
	}
}

func testOverviewSnapshot(rangeCost float64) *OverviewResponse {
	tokens := int64(1234)
	today := 2.5
	total := 20.5
	return &OverviewResponse{
		Configured: true, IsRepresentative: true,
		Window: OverviewWindow{StartDate: "2026-07-01", EndDate: "2026-07-07", Granularity: "day", Today: "2026-07-07", RollingDays: 7, Timezone: "Asia/Shanghai"},
		Summary: OverviewSummary{
			MemberCount: 2, RelayMemberCount: 2, RangeActualCost: &rangeCost, RangeTotalTokens: &tokens,
			TodayActualCost: &today, TotalActualCost: &total, UnitLabel: "USD",
		},
		TopMembers:      []OverviewMember{},
		TopMemberTrend:  TopMemberTrendState{UnitLabel: "USD", RankBasis: "range_total_tokens", Series: []TopMemberTrendSeries{}},
		DepartmentTrend: DepartmentTrendState{UnitLabel: "USD", Series: []DepartmentTrendSeries{}},
		Members:         []OverviewMember{}, MemberTree: []OverviewMemberNode{},
	}
}

func testSummarySnapshot(rangeCost float64) *SummarySnapshot {
	overview := testOverviewSnapshot(rangeCost)
	return &SummarySnapshot{
		Window: overview.Window,
		Summary: SummaryAggregate{
			Unavailable: overview.Summary.Unavailable, UnavailableReason: overview.Summary.UnavailableReason,
			MemberCount: overview.Summary.MemberCount, RelayMemberCount: overview.Summary.RelayMemberCount,
			RangeActualCost: overview.Summary.RangeActualCost, RangeTotalTokens: overview.Summary.RangeTotalTokens,
			TodayActualCost: overview.Summary.TodayActualCost, TotalActualCost: overview.Summary.TotalActualCost,
			UnitLabel: overview.Summary.UnitLabel,
		},
	}
}

func testEffectiveScope() *representativescope.Scope {
	relayID := 1001
	subject := representativescope.Subject{
		SubjectType: "member", UserID: 101, DirectoryMemberExternalID: "member-alice",
		DisplayName: "Alice", Email: "alice@example.com", DepartmentExternalID: "department-alpha",
		DepartmentExternalIDs: []string{"department-alpha", "department-beta"}, RelayUserID: &relayID, Selectable: true,
	}
	return &representativescope.Scope{
		Version: "scope-v1", ActorUserID: 11, ActorMemberExternalID: "member-representative", IsRepresentative: true,
		RepresentedDepartmentIDs: []string{"department-alpha"},
		RepresentedSubtreeIDs: map[string]map[string]struct{}{
			"department-alpha": {"department-alpha": {}, "department-beta": {}},
			"department-beta":  {"department-beta": {}},
		},
		Departments:           []representativescope.DepartmentScope{{ExternalID: "department-alpha", Name: "Department Alpha"}},
		MemberTreeRootIDs:     []string{"department-alpha"},
		MemberTreeDepartments: []representativescope.DepartmentScope{{ExternalID: "department-alpha", Name: "Department Alpha"}},
		Subjects:              []representativescope.Subject{subject}, OverviewSubjects: []representativescope.Subject{subject},
		TargetRepresentedRoots: map[int][]string{101: {"department-alpha"}},
	}
}

type failingSnapshotStore struct {
	err error
}

func (s failingSnapshotStore) Get(context.Context, string) ([]byte, error) {
	return nil, s.err
}

func (s failingSnapshotStore) Set(context.Context, string, []byte, time.Duration) error {
	return s.err
}

func (s failingSnapshotStore) TryAcquireLease(context.Context, string, string, time.Duration) (bool, error) {
	return false, s.err
}

func (s failingSnapshotStore) LeaseTTL(context.Context, string) (time.Duration, error) {
	return 0, s.err
}

func (s failingSnapshotStore) ReleaseLease(context.Context, string, string) (bool, error) {
	return false, s.err
}
