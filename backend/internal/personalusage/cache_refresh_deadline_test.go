package personalusage

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/internal/readcache"
	"github.com/ai-efficiency/backend/internal/relay"
)

// TestCacheServesStaleWhenRefreshDeadlineExpires reproduces the production
// incident where the usage dashboard returned 502 "context deadline exceeded"
// even though an eligible stale generation still existed in the read model.
//
// The refresh flight owns a bounded context. When that internal deadline
// expires before the relay origin surfaces its own transient error, the cache
// must still prefer the eligible stale generation instead of returning the
// flight deadline as a hard failure. Only caller cancellation is allowed to
// bypass stale-if-error.
func TestCacheServesStaleWhenRefreshDeadlineExpires(t *testing.T) {
	now := time.Date(2026, 7, 15, 8, 0, 0, 0, time.UTC)
	store := newFakeUsageStore(func() time.Time { return now })
	cache := newDeadlineCache(t, store, func() time.Time { return now }, 40*time.Millisecond)
	key := testCacheKey()

	if _, err := cache.GetOrLoad(context.Background(), key, false, func(context.Context, bool) (OriginLoadResult, error) {
		return OriginLoadResult{Usage: testUsageSnapshot(1)}, nil
	}); err != nil {
		t.Fatalf("cold GetOrLoad() error = %v", err)
	}

	// Past the fresh window but still inside the hard stale deadline.
	now = now.Add(30 * time.Second)

	result, err := cache.GetOrLoad(context.Background(), key, false, func(loadCtx context.Context, _ bool) (OriginLoadResult, error) {
		<-loadCtx.Done()
		return OriginLoadResult{UsageErr: loadCtx.Err()}, nil
	})
	if err != nil {
		t.Fatalf("GetOrLoad() error = %v, want an eligible stale generation", err)
	}
	if result == nil || result.Usage == nil || result.Usage.Stats.TotalRequests != 1 {
		t.Fatalf("GetOrLoad() result = %+v, want the seeded stale generation", result)
	}
	if result.UsageFreshness.CacheStatus != "stale" || result.UsageFreshness.SourceStatus != "error" {
		t.Fatalf("GetOrLoad() freshness = %+v, want cache_status=stale source_status=error", result.UsageFreshness)
	}
}

// TestCacheServesStaleOnTransientOriginFailure covers transient authoritative
// failures that reach the cache as a hard loader error rather than a usage
// branch error: a relay login deadline or transport failure has no usable
// generation, but the previous eligible generation must still answer.
func TestCacheServesStaleOnTransientOriginFailure(t *testing.T) {
	tests := []struct {
		name string
		load OriginLoader
	}{
		{
			name: "relay login deadline",
			load: func(context.Context, bool) (OriginLoadResult, error) {
				return OriginLoadResult{}, fmt.Errorf("relay: login for usage origin: %w", context.DeadlineExceeded)
			},
		},
		{
			name: "relay login transport failure",
			load: func(context.Context, bool) (OriginLoadResult, error) {
				return OriginLoadResult{}, errors.New("relay: login for usage origin: connection reset by peer")
			},
		},
		{
			name: "usage branch transient error",
			load: func(context.Context, bool) (OriginLoadResult, error) {
				return OriginLoadResult{UsageErr: errors.New("synthetic Relay outage")}, nil
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			now := time.Date(2026, 7, 15, 8, 0, 0, 0, time.UTC)
			store := newFakeUsageStore(func() time.Time { return now })
			cache := testCache(t, store, func() time.Time { return now }, 0)
			if _, err := cache.GetOrLoad(context.Background(), testCacheKey(), false, func(context.Context, bool) (OriginLoadResult, error) {
				return OriginLoadResult{Usage: testUsageSnapshot(2)}, nil
			}); err != nil {
				t.Fatalf("seed cache: %v", err)
			}
			store.mu.Lock()
			seedWrites := store.setCalls
			store.mu.Unlock()

			now = now.Add(30 * time.Second)
			result, err := cache.GetOrLoad(context.Background(), testCacheKey(), false, tt.load)
			if err != nil {
				t.Fatalf("GetOrLoad() error = %v, want an eligible stale generation", err)
			}
			if result == nil || result.UsageFreshness.CacheStatus != "stale" || result.UsageFreshness.SourceStatus != "error" {
				t.Fatalf("GetOrLoad() result = %+v, want cache_status=stale source_status=error", result)
			}
			store.mu.Lock()
			writes := store.setCalls
			store.mu.Unlock()
			if writes != seedWrites {
				t.Fatalf("cache writes = %d, want %d; origin errors must not be stored", writes, seedWrites)
			}
		})
	}
}

// TestCacheCallerCancellationDuringRefreshNeverServesStale proves that caller
// cancellation is excluded from stale-if-error: when every waiter leaves, the
// shared flight is canceled and the caller sees cancellation, not stale data.
func TestCacheCallerCancellationDuringRefreshNeverServesStale(t *testing.T) {
	now := time.Date(2026, 7, 15, 8, 0, 0, 0, time.UTC)
	store := newFakeUsageStore(func() time.Time { return now })
	cache := testCache(t, store, func() time.Time { return now }, 0)
	if _, err := cache.GetOrLoad(context.Background(), testCacheKey(), false, func(context.Context, bool) (OriginLoadResult, error) {
		return OriginLoadResult{Usage: testUsageSnapshot(3)}, nil
	}); err != nil {
		t.Fatalf("seed cache: %v", err)
	}
	now = now.Add(30 * time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{})
	results := make(chan *CacheResult, 1)
	errs := make(chan error, 1)
	go func() {
		result, err := cache.GetOrLoad(ctx, testCacheKey(), false, func(loadCtx context.Context, _ bool) (OriginLoadResult, error) {
			close(started)
			<-loadCtx.Done()
			return OriginLoadResult{UsageErr: loadCtx.Err()}, nil
		})
		results <- result
		errs <- err
	}()
	<-started
	cancel()

	if err := <-errs; !errors.Is(err, context.Canceled) {
		t.Fatalf("GetOrLoad() error = %v, want context cancellation", err)
	}
	if result := <-results; result != nil {
		t.Fatalf("GetOrLoad() result = %+v, want no stale data for a canceled caller", result)
	}
}

// TestCacheColdMissRefreshDeadlineReturnsError keeps the cold-miss contract:
// with no eligible stale generation, a refresh-flight deadline is still an
// error rather than an invented empty success.
func TestCacheColdMissRefreshDeadlineReturnsError(t *testing.T) {
	now := time.Date(2026, 7, 15, 8, 0, 0, 0, time.UTC)
	store := newFakeUsageStore(func() time.Time { return now })
	cache := newDeadlineCache(t, store, func() time.Time { return now }, 40*time.Millisecond)

	_, err := cache.GetOrLoad(context.Background(), testCacheKey(), false, func(loadCtx context.Context, _ bool) (OriginLoadResult, error) {
		<-loadCtx.Done()
		return OriginLoadResult{UsageErr: loadCtx.Err()}, nil
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("GetOrLoad() error = %v, want the refresh deadline", err)
	}
}

// TestCacheExpiredGenerationRefreshDeadlineReturnsError proves the hard stale
// deadline is not extended: flight deadline expiry after the deadline fails.
func TestCacheExpiredGenerationRefreshDeadlineReturnsError(t *testing.T) {
	now := time.Date(2026, 7, 15, 8, 0, 0, 0, time.UTC)
	store := newFakeUsageStore(func() time.Time { return now })
	cache := newDeadlineCache(t, store, func() time.Time { return now }, 40*time.Millisecond)
	seeded, err := cache.GetOrLoad(context.Background(), testCacheKey(), false, func(context.Context, bool) (OriginLoadResult, error) {
		return OriginLoadResult{Usage: testUsageSnapshot(4)}, nil
	})
	if err != nil {
		t.Fatalf("seed cache: %v", err)
	}
	now = seeded.UsageFreshness.StaleUntil.Add(time.Nanosecond)

	_, err = cache.GetOrLoad(context.Background(), testCacheKey(), false, func(loadCtx context.Context, _ bool) (OriginLoadResult, error) {
		<-loadCtx.Done()
		return OriginLoadResult{UsageErr: loadCtx.Err()}, nil
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("GetOrLoad() error = %v, want the refresh deadline past the hard stale deadline", err)
	}
}

// TestCacheInvalidCredentialsNeverServesStale covers the usage branch form of
// the credential error: it stays a 409-class failure and never stale.
func TestCacheInvalidCredentialsNeverServesStale(t *testing.T) {
	now := time.Date(2026, 7, 15, 8, 0, 0, 0, time.UTC)
	store := newFakeUsageStore(func() time.Time { return now })
	cache := testCache(t, store, func() time.Time { return now }, 0)
	if _, err := cache.GetOrLoad(context.Background(), testCacheKey(), false, func(context.Context, bool) (OriginLoadResult, error) {
		return OriginLoadResult{Usage: testUsageSnapshot(5)}, nil
	}); err != nil {
		t.Fatalf("seed cache: %v", err)
	}
	now = now.Add(30 * time.Second)

	if _, err := cache.GetOrLoad(context.Background(), testCacheKey(), false, func(context.Context, bool) (OriginLoadResult, error) {
		return OriginLoadResult{UsageErr: relay.ErrInvalidCredentials}, nil
	}); !errors.Is(err, relay.ErrInvalidCredentials) {
		t.Fatalf("GetOrLoad() error = %v, want invalid credentials", err)
	}
}

// TestRefreshTimeoutOutlivesRelayUsageOriginTimeout asserts the timeout
// relationship that keeps a flight deadline from preempting the origin's own
// transient failure: relay origin < refresh flight (with a margin) and the
// implicit cache default keeps the same relationship.
func TestRefreshTimeoutOutlivesRelayUsageOriginTimeout(t *testing.T) {
	if relay.UserUsageOriginTimeout <= 0 {
		t.Fatalf("relay.UserUsageOriginTimeout = %s, want a positive bounded origin deadline", relay.UserUsageOriginTimeout)
	}
	if DefaultRefreshTimeout <= relay.UserUsageOriginTimeout {
		t.Fatalf("DefaultRefreshTimeout = %s, must be strictly longer than relay origin timeout %s", DefaultRefreshTimeout, relay.UserUsageOriginTimeout)
	}
	if margin := DefaultRefreshTimeout - relay.UserUsageOriginTimeout; margin < time.Second {
		t.Fatalf("refresh margin = %s, want a non-trivial margin above the relay origin deadline", margin)
	}

	var options CacheOptions
	applyCacheDefaults(&options)
	if options.RefreshTimeout != DefaultRefreshTimeout {
		t.Fatalf("implicit RefreshTimeout = %s, want %s", options.RefreshTimeout, DefaultRefreshTimeout)
	}
	if options.LeaseTTL != DefaultLeaseTTL {
		t.Fatalf("implicit LeaseTTL = %s, want %s", options.LeaseTTL, DefaultLeaseTTL)
	}
	if options.LeaseTTL <= options.RefreshTimeout {
		t.Fatalf("implicit LeaseTTL = %s, must stay strictly longer than the refresh flight %s", options.LeaseTTL, options.RefreshTimeout)
	}
}

// TestCacheHardLoaderCredentialAndConfigurationErrorsNeverServeStale keeps the
// production service.go shape for non-transient hard loader errors: invalid
// credentials and configuration failures must stay hard failures even when a
// stale generation exists.
func TestCacheHardLoaderCredentialAndConfigurationErrorsNeverServeStale(t *testing.T) {
	tests := []struct {
		name  string
		error error
	}{
		{name: "invalid credentials", error: fmt.Errorf("relay: login for usage origin: %w", relay.ErrInvalidCredentials)},
		{name: "configuration failure", error: fmt.Errorf("personal usage: %w", ErrConfiguration)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			now := time.Date(2026, 7, 15, 8, 0, 0, 0, time.UTC)
			store := newFakeUsageStore(func() time.Time { return now })
			cache := testCache(t, store, func() time.Time { return now }, 0)
			if _, err := cache.GetOrLoad(context.Background(), testCacheKey(), false, func(context.Context, bool) (OriginLoadResult, error) {
				return OriginLoadResult{Usage: testUsageSnapshot(7)}, nil
			}); err != nil {
				t.Fatalf("seed cache: %v", err)
			}
			now = now.Add(30 * time.Second)

			_, err := cache.GetOrLoad(context.Background(), testCacheKey(), false, func(context.Context, bool) (OriginLoadResult, error) {
				return OriginLoadResult{}, tt.error
			})
			if !errors.Is(err, tt.error) {
				t.Fatalf("GetOrLoad() error = %v, want %v", err, tt.error)
			}
		})
	}
}

// TestCacheServesStaleWhenProductionLoaderReturnsFlightDeadline covers the
// exact production incident shape. backend/internal/personalusage/service.go
// converts an observed flight deadline into a bare hard loader error (no
// UsageErr), so the cache's loader-error path must still prefer eligible stale.
func TestCacheServesStaleWhenProductionLoaderReturnsFlightDeadline(t *testing.T) {
	now := time.Date(2026, 7, 15, 8, 0, 0, 0, time.UTC)
	store := newFakeUsageStore(func() time.Time { return now })
	cache := newDeadlineCache(t, store, func() time.Time { return now }, 40*time.Millisecond)
	if _, err := cache.GetOrLoad(context.Background(), testCacheKey(), false, func(context.Context, bool) (OriginLoadResult, error) {
		return OriginLoadResult{Usage: testUsageSnapshot(6)}, nil
	}); err != nil {
		t.Fatalf("cold GetOrLoad() error = %v", err)
	}
	now = now.Add(30 * time.Second)

	result, err := cache.GetOrLoad(context.Background(), testCacheKey(), false, func(loadCtx context.Context, _ bool) (OriginLoadResult, error) {
		<-loadCtx.Done()
		return OriginLoadResult{}, loadCtx.Err()
	})
	if err != nil {
		t.Fatalf("GetOrLoad() error = %v, want an eligible stale generation", err)
	}
	if result == nil || result.UsageFreshness.CacheStatus != "stale" || result.UsageFreshness.SourceStatus != "error" {
		t.Fatalf("GetOrLoad() result = %+v, want cache_status=stale source_status=error", result)
	}
}

// TestCacheRelayIdentityMismatchNeverServesStale keeps a Relay binding
// integrity failure out of stale-if-error: it is not a transient failure.
func TestCacheRelayIdentityMismatchNeverServesStale(t *testing.T) {
	now := time.Date(2026, 7, 15, 8, 0, 0, 0, time.UTC)
	store := newFakeUsageStore(func() time.Time { return now })
	cache := testCache(t, store, func() time.Time { return now }, 0)
	if _, err := cache.GetOrLoad(context.Background(), testCacheKey(), false, func(context.Context, bool) (OriginLoadResult, error) {
		return OriginLoadResult{Usage: testUsageSnapshot(7)}, nil
	}); err != nil {
		t.Fatalf("seed cache: %v", err)
	}
	now = now.Add(30 * time.Second)

	mismatch := fmt.Errorf("%w: authenticated user 99 does not match requested user 7", relay.ErrUsageOriginIdentityMismatch)
	if _, err := cache.GetOrLoad(context.Background(), testCacheKey(), false, func(context.Context, bool) (OriginLoadResult, error) {
		return OriginLoadResult{}, mismatch
	}); !errors.Is(err, relay.ErrUsageOriginIdentityMismatch) {
		t.Fatalf("GetOrLoad() error = %v, want the Relay identity mismatch", err)
	}
}

func newDeadlineCache(t *testing.T, store readcache.Store, now func() time.Time, refreshTimeout time.Duration) *Cache {
	t.Helper()
	cache, err := NewCache(store, CacheOptions{
		Namespace:      "test-blue",
		CommandTimeout: time.Second,
		RefreshTimeout: refreshTimeout,
		LeaseTTL:       time.Second,
		PollInterval:   time.Millisecond,
		ReleaseTimeout: time.Second,
		Now:            now,
		RandFloat64:    func() float64 { return 0 },
		NewToken:       func() string { return "token-1" },
		Sleep:          readcache.Sleep,
	})
	if err != nil {
		t.Fatalf("NewCache() error = %v", err)
	}
	return cache
}
