package teamusage

import (
	"context"
	"crypto/sha256"
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

func TestPrewarmMovingSharesCurrentStatsAndPublishesTimezonesIndependently(t *testing.T) {
	now := time.Date(2026, 7, 21, 8, 0, 0, 0, time.UTC)
	cache, _ := newRedisPrewarmCache(t, func() time.Time { return now })
	timezones := []string{"UTC", "Asia/Shanghai"}
	old := make(map[string]PrewarmManifest, len(timezones))
	for _, timezone := range timezones {
		old[timezone] = seedPrewarmManifest(t, cache, PrewarmCacheIdentity{ProviderID: 7, ProviderVersion: 11, Timezone: timezone, AnchorDate: "2026-07-21"}, now.Add(-30*time.Second), timezone[:1])
	}

	provider := newLifecycleProvider(ascendingIDs(3))
	prewarmer := mustPrewarmer(t, staticBindingResolver{binding: prewarmBinding(provider)}, cache, lifecycleOptions(timezones, func() time.Time { return now }))
	if err := prewarmer.RunMoving(context.Background()); err != nil {
		t.Fatalf("RunMoving() error = %v", err)
	}
	if got := provider.directoryCount(); got != 1 {
		t.Fatalf("directory calls = %d, want one shared current-stats generation", got)
	}
	if got := provider.statsCount(); got != 1 {
		t.Fatalf("stats calls = %d, want one chunk", got)
	}

	var sharedGeneration string
	for _, timezone := range timezones {
		result := readPrewarmResult(t, cache, PrewarmCacheIdentity{ProviderID: 7, ProviderVersion: 11, Timezone: timezone, AnchorDate: "2026-07-21"})
		if result.Manifest.TodayHour.GenerationID == old[timezone].TodayHour.GenerationID {
			t.Fatalf("%s today generation was not refreshed", timezone)
		}
		if result.Manifest.History29d.GenerationID != old[timezone].History29d.GenerationID || result.Manifest.History6d.GenerationID != old[timezone].History6d.GenerationID {
			t.Fatalf("%s moving cycle replaced historical references", timezone)
		}
		if sharedGeneration == "" {
			sharedGeneration = result.Manifest.CurrentStats.GenerationID
		} else if result.Manifest.CurrentStats.GenerationID != sharedGeneration {
			t.Fatalf("current generation = %q, want shared %q", result.Manifest.CurrentStats.GenerationID, sharedGeneration)
		}
	}
}

func TestPrewarmMovingTimezoneFailureDoesNotInvalidateAnother(t *testing.T) {
	now := time.Date(2026, 7, 21, 8, 0, 0, 0, time.UTC)
	cache, _ := newRedisPrewarmCache(t, func() time.Time { return now })
	timezones := []string{"UTC", "Asia/Shanghai"}
	old := make(map[string]PrewarmManifest, len(timezones))
	for index, timezone := range timezones {
		old[timezone] = seedPrewarmManifest(t, cache, PrewarmCacheIdentity{ProviderID: 7, ProviderVersion: 11, Timezone: timezone, AnchorDate: "2026-07-21"}, now.Add(-30*time.Second), string(rune('a'+index*4)))
	}
	provider := newLifecycleProvider([]int64{101})
	provider.trendErrorTimezone = "Asia/Shanghai"
	prewarmer := mustPrewarmer(t, staticBindingResolver{binding: prewarmBinding(provider)}, cache, lifecycleOptions(timezones, func() time.Time { return now }))
	if err := prewarmer.RunMoving(context.Background()); err == nil {
		t.Fatal("RunMoving() error = nil, want bounded timezone failure")
	}
	utc := readPrewarmResult(t, cache, PrewarmCacheIdentity{ProviderID: 7, ProviderVersion: 11, Timezone: "UTC", AnchorDate: "2026-07-21"})
	shanghai := readPrewarmResult(t, cache, PrewarmCacheIdentity{ProviderID: 7, ProviderVersion: 11, Timezone: "Asia/Shanghai", AnchorDate: "2026-07-21"})
	if utc.Manifest.TodayHour.GenerationID == old["UTC"].TodayHour.GenerationID {
		t.Fatal("healthy timezone was not published")
	}
	if shanghai.Manifest.TodayHour.GenerationID != old["Asia/Shanghai"].TodayHour.GenerationID {
		t.Fatal("failed timezone replaced its prior manifest")
	}
}

func TestPrewarmHistoricalRefreshesIndependentSegmentsAndSkipsSplitUnsafe(t *testing.T) {
	now := time.Date(2026, 7, 21, 8, 0, 0, 0, time.UTC)
	cache, _ := newRedisPrewarmCache(t, func() time.Time { return now })
	identity := PrewarmCacheIdentity{ProviderID: 7, ProviderVersion: 11, Timezone: "UTC", AnchorDate: "2026-07-21"}
	old := seedMixedAgePrewarmManifest(t, cache, identity, now)
	provider := newLifecycleProvider([]int64{101})
	provider.trendErrorClass = SegmentHistory6d
	prewarmer := mustPrewarmer(t, staticBindingResolver{binding: prewarmBinding(provider)}, cache, lifecycleOptions([]string{"UTC"}, func() time.Time { return now }))
	if err := prewarmer.RunHistorical(context.Background(), "UTC", now); err == nil {
		t.Fatal("RunHistorical() error = nil, want independent history_6d failure")
	}
	result := readPrewarmResult(t, cache, identity)
	if result.Manifest.History29d.GenerationID == old.History29d.GenerationID {
		t.Fatal("successful history_29d was not independently published")
	}
	if result.Manifest.History6d.GenerationID != old.History6d.GenerationID {
		t.Fatal("failed history_6d replaced its prior reference")
	}
	if result.Manifest.CurrentStats.GenerationID != old.CurrentStats.GenerationID || result.Manifest.TodayHour.GenerationID != old.TodayHour.GenerationID {
		t.Fatal("historical correction replaced moving references")
	}

	before := provider.trendCount()
	unsafeAnchor := time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC)
	if err := prewarmer.RunHistorical(context.Background(), "America/Los_Angeles", unsafeAnchor); err != nil {
		t.Fatalf("RunHistorical(split unsafe) error = %v", err)
	}
	if got := provider.trendCount(); got != before {
		t.Fatalf("split-unsafe historical run made %d source calls, want none", got-before)
	}
}

func TestPrewarmHistoricalJitterIsDeterministicAndBounded(t *testing.T) {
	binding := ProviderBinding{ProviderID: 7, ProviderVersion: 11}
	base := prewarmHistoricalJitter(binding, "UTC", "2026-07-21", SegmentHistory29d)
	if base < 0 || base >= 30*time.Minute {
		t.Fatalf("jitter = %v, want [0,30m)", base)
	}
	if got := prewarmHistoricalJitter(binding, "UTC", "2026-07-21", SegmentHistory29d); got != base {
		t.Fatalf("second jitter = %v, want deterministic %v", got, base)
	}
	variants := []time.Duration{
		prewarmHistoricalJitter(ProviderBinding{ProviderID: 8, ProviderVersion: 11}, "UTC", "2026-07-21", SegmentHistory29d),
		prewarmHistoricalJitter(ProviderBinding{ProviderID: 7, ProviderVersion: 12}, "UTC", "2026-07-21", SegmentHistory29d),
		prewarmHistoricalJitter(binding, "Asia/Shanghai", "2026-07-21", SegmentHistory29d),
		prewarmHistoricalJitter(binding, "UTC", "2026-07-22", SegmentHistory29d),
		prewarmHistoricalJitter(binding, "UTC", "2026-07-21", SegmentHistory6d),
	}
	allSame := true
	for _, got := range variants {
		if got < 0 || got >= 30*time.Minute {
			t.Fatalf("variant jitter = %v, want [0,30m)", got)
		}
		allSame = allSame && got == base
	}
	if allSame {
		t.Fatal("all dimension variants produced the same deterministic jitter")
	}
}

func TestPrewarmStartupRecoversOnlyMissingOrHardExpiredValues(t *testing.T) {
	base := time.Date(2026, 7, 21, 8, 0, 0, 0, time.UTC)
	now := base
	cache, server := newRedisPrewarmCache(t, func() time.Time { return now })
	identity := PrewarmCacheIdentity{ProviderID: 7, ProviderVersion: 11, Timezone: "UTC", AnchorDate: "2026-07-21"}
	manifest := seedPrewarmManifest(t, cache, identity, base, "a")
	provider := newLifecycleProvider([]int64{101})
	prewarmer := mustPrewarmer(t, staticBindingResolver{binding: prewarmBinding(provider)}, cache, lifecycleOptions([]string{"UTC"}, func() time.Time { return now }))

	now = base.Add(30 * time.Second)
	server.Del(manifest.History6d.Key)
	if err := prewarmer.runStartup(context.Background()); err != nil {
		t.Fatalf("runStartup(missing history) error = %v", err)
	}
	if got := provider.classCount(SegmentHistory6d); got != 1 {
		t.Fatalf("history_6d calls = %d, want one missing-value recovery", got)
	}
	if got := provider.classCount(SegmentHistory29d) + provider.classCount(SegmentTodayHour); got != 0 {
		t.Fatalf("unneeded segment recovery calls = %d, want zero", got)
	}
	if provider.directoryCount() != 0 || provider.statsCount() != 0 {
		t.Fatal("missing history recovery rebuilt current stats")
	}

	provider.resetCounts()
	now = base.Add(2 * time.Minute)
	if err := prewarmer.runStartup(context.Background()); err != nil {
		t.Fatalf("runStartup(stale hard-valid) error = %v", err)
	}
	if provider.totalSourceCalls() != 0 {
		t.Fatalf("startup rebuilt stale-but-hard-valid values with %d calls", provider.totalSourceCalls())
	}

	provider.resetCounts()
	now = base.Add(5 * time.Minute)
	if err := prewarmer.runStartup(context.Background()); err != nil {
		t.Fatalf("runStartup(hard expired moving values) error = %v", err)
	}
	if provider.directoryCount() != 1 || provider.statsCount() != 1 || provider.classCount(SegmentTodayHour) != 1 {
		t.Fatalf("hard-expired recovery counts directory=%d stats=%d today=%d, want 1/1/1", provider.directoryCount(), provider.statsCount(), provider.classCount(SegmentTodayHour))
	}
	if provider.classCount(SegmentHistory29d)+provider.classCount(SegmentHistory6d) != 0 {
		t.Fatal("hard-expired moving recovery rebuilt hard-valid histories")
	}
}

func TestPrewarmStartupLostSegmentLeaseCannotPublishRecovery(t *testing.T) {
	base := time.Date(2026, 7, 21, 8, 0, 0, 0, time.UTC)
	now := base.Add(30 * time.Second)
	store := &lostSegmentLeasePrewarmStore{
		recordingPrewarmStore: newRecordingPrewarmStore(),
		leaseTTLs:             make(map[string]time.Duration),
	}
	cache := mustNewPrewarmCache(t, store, func() time.Time { return now })
	identity := PrewarmCacheIdentity{ProviderID: 7, ProviderVersion: 11, Timezone: "UTC", AnchorDate: "2026-07-21"}
	old := seedPrewarmManifest(t, cache, identity, base, "a")
	store.mu.Lock()
	delete(store.values, old.TodayHour.Key)
	store.mu.Unlock()
	store.loseClass = SegmentTodayHour
	provider := newLifecycleProvider([]int64{101})
	prewarmer := mustPrewarmer(t, staticBindingResolver{binding: prewarmBinding(provider)}, cache, lifecycleOptions([]string{"UTC"}, func() time.Time { return now }))
	if err := prewarmer.runStartup(context.Background()); err == nil {
		t.Fatal("runStartup(lost today lease) error = nil, want lost-lease result")
	}
	result := readPrewarmResult(t, cache, identity)
	if result.Manifest.TodayHour.GenerationID != old.TodayHour.GenerationID {
		t.Fatal("startup published recovery after losing its segment lease")
	}
}

func TestPrewarmLeaseLostOrProviderVersionChangedCannotPublish(t *testing.T) {
	t.Run("lost segment", func(t *testing.T) {
		now := time.Date(2026, 7, 21, 8, 0, 0, 0, time.UTC)
		store := &lostSegmentLeasePrewarmStore{
			recordingPrewarmStore: newRecordingPrewarmStore(),
			leaseTTLs:             make(map[string]time.Duration),
			loseClass:             SegmentTodayHour,
		}
		cache := mustNewPrewarmCache(t, store, func() time.Time { return now })
		identity := PrewarmCacheIdentity{ProviderID: 7, ProviderVersion: 11, Timezone: "UTC", AnchorDate: "2026-07-21"}
		old := seedPrewarmManifest(t, cache, identity, now.Add(-30*time.Second), "a")
		provider := newLifecycleProvider([]int64{101})
		prewarmer := mustPrewarmer(t, staticBindingResolver{binding: prewarmBinding(provider)}, cache, lifecycleOptions([]string{"UTC"}, func() time.Time { return now }))
		if err := prewarmer.RunMoving(context.Background()); err == nil {
			t.Fatal("RunMoving(lost segment) error = nil, want lost-lease result")
		}
		got := readPrewarmResult(t, cache, identity)
		if got.Manifest.TodayHour.GenerationID != old.TodayHour.GenerationID {
			t.Fatal("lost segment lease published a new manifest")
		}
	})

	t.Run("lost historical segment", func(t *testing.T) {
		now := time.Date(2026, 7, 21, 8, 0, 0, 0, time.UTC)
		store := &lostSegmentLeasePrewarmStore{
			recordingPrewarmStore: newRecordingPrewarmStore(),
			leaseTTLs:             make(map[string]time.Duration),
		}
		cache := mustNewPrewarmCache(t, store, func() time.Time { return now })
		identity := PrewarmCacheIdentity{ProviderID: 7, ProviderVersion: 11, Timezone: "UTC", AnchorDate: "2026-07-21"}
		old := seedMixedAgePrewarmManifest(t, cache, identity, now)
		store.loseClass = SegmentHistory29d
		provider := newLifecycleProvider([]int64{101})
		prewarmer := mustPrewarmer(t, staticBindingResolver{binding: prewarmBinding(provider)}, cache, lifecycleOptions([]string{"UTC"}, func() time.Time { return now }))
		if err := prewarmer.RunHistorical(context.Background(), "UTC", now); err == nil {
			t.Fatal("RunHistorical(lost history_29d) error = nil, want lost-lease result")
		}
		got := readPrewarmResult(t, cache, identity)
		if got.Manifest.History29d.GenerationID != old.History29d.GenerationID {
			t.Fatal("lost history_29d lease was published by another segment worker")
		}
		if got.Manifest.History6d.GenerationID == old.History6d.GenerationID {
			t.Fatal("healthy history_6d worker did not publish independently")
		}
	})

	t.Run("provider version", func(t *testing.T) {
		now := time.Date(2026, 7, 21, 8, 0, 0, 0, time.UTC)
		cache, _ := newRedisPrewarmCache(t, func() time.Time { return now })
		identity := PrewarmCacheIdentity{ProviderID: 7, ProviderVersion: 11, Timezone: "UTC", AnchorDate: "2026-07-21"}
		old := seedPrewarmManifest(t, cache, identity, now.Add(-30*time.Second), "a")
		provider := newLifecycleProvider([]int64{101})
		resolver := &changingBindingResolver{first: prewarmBinding(provider), later: ProviderBinding{ProviderID: 7, ProviderVersion: 12, Provider: provider}}
		prewarmer := mustPrewarmer(t, resolver, cache, lifecycleOptions([]string{"UTC"}, func() time.Time { return now }))
		if err := prewarmer.RunMoving(context.Background()); err == nil {
			t.Fatal("RunMoving(provider changed) error = nil, want cancellation")
		}
		got := readPrewarmResult(t, cache, identity)
		if got.Manifest.TodayHour.GenerationID != old.TodayHour.GenerationID {
			t.Fatal("provider-version race published a new manifest")
		}
	})
}

func TestPrewarmConcurrencyUsesTwoDistributedSlotsAndTwoProcessPositions(t *testing.T) {
	now := time.Date(2026, 7, 21, 8, 0, 0, 0, time.UTC)
	cache, server := newRedisPrewarmCache(t, func() time.Time { return now })
	options := lifecycleOptions([]string{"UTC"}, func() time.Time { return now })
	left := mustPrewarmer(t, staticBindingResolver{}, cache, options).SourceCallLimiter()
	right := mustPrewarmer(t, staticBindingResolver{}, cache, options).SourceCallLimiter()

	var active, maximum atomic.Int64
	release := make(chan struct{})
	entered := make(chan struct{}, 8)
	var wg sync.WaitGroup
	for index := 0; index < 8; index++ {
		limiter := left
		if index%2 == 1 {
			limiter = right
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := limiter.Do(context.Background(), func(context.Context) error {
				current := active.Add(1)
				for {
					observed := maximum.Load()
					if current <= observed || maximum.CompareAndSwap(observed, current) {
						break
					}
				}
				entered <- struct{}{}
				<-release
				active.Add(-1)
				return nil
			}); err != nil {
				t.Errorf("SourceCallLimiter.Do() error = %v", err)
			}
		}()
	}
	for index := 0; index < 2; index++ {
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatal("two distributed source slots did not admit work")
		}
	}
	time.Sleep(40 * time.Millisecond)
	if got := active.Load(); got != 2 {
		t.Fatalf("deployment-wide active calls = %d, want exactly two", got)
	}
	if got := maximum.Load(); got != 2 {
		t.Fatalf("maximum source concurrency = %d, want two", got)
	}
	ownedSlots := 0
	for _, key := range server.Keys() {
		if strings.Contains(key, ":lease:") && server.TTL(key) > 80*time.Second && server.TTL(key) <= 90*time.Second {
			ownedSlots++
		}
	}
	if ownedSlots != 2 {
		t.Fatalf("owned 90-second source slots = %d, want two", ownedSlots)
	}
	close(release)
	wg.Wait()
}

func TestPrewarmLeaseDurationsUseCoordinatorAndWorkerContracts(t *testing.T) {
	now := time.Date(2026, 7, 21, 8, 0, 0, 0, time.UTC)
	store := &ttlRecordingPrewarmStore{recordingPrewarmStore: newRecordingPrewarmStore()}
	cache := mustNewPrewarmCache(t, store, func() time.Time { return now })
	identity := PrewarmCacheIdentity{ProviderID: 7, ProviderVersion: 11, Timezone: "UTC", AnchorDate: "2026-07-21"}
	seedMixedAgePrewarmManifest(t, cache, identity, now)
	store.resetTTLs()
	provider := newLifecycleProvider([]int64{101})
	prewarmer := mustPrewarmer(t, staticBindingResolver{binding: prewarmBinding(provider)}, cache, lifecycleOptions([]string{"UTC"}, func() time.Time { return now }))
	if err := prewarmer.RunMoving(context.Background()); err != nil {
		t.Fatalf("RunMoving() error = %v", err)
	}
	if !store.sawTTL(3*time.Minute) || !store.sawTTL(90*time.Second) {
		t.Fatalf("moving lease TTLs = %v, want coordinator 3m and worker/slot 90s", store.ttlsCopy())
	}
	store.resetTTLs()
	if err := prewarmer.RunHistorical(context.Background(), "UTC", now); err != nil {
		t.Fatalf("RunHistorical() error = %v", err)
	}
	if !store.sawTTL(6*time.Minute) || !store.sawTTL(90*time.Second) {
		t.Fatalf("historical lease TTLs = %v, want coordinator 6m and worker/slot 90s", store.ttlsCopy())
	}
}

func TestPrewarmLeaseCoordinatorUsesTokenCheckedCompareDelete(t *testing.T) {
	now := time.Date(2026, 7, 21, 8, 0, 0, 0, time.UTC)
	cache, _ := newRedisPrewarmCache(t, func() time.Time { return now })
	provider := newLifecycleProvider([]int64{101})
	provider.directoryErr = errors.New("synthetic directory failure")
	options := lifecycleOptions([]string{"UTC"}, func() time.Time { return now })
	left := mustPrewarmer(t, staticBindingResolver{binding: prewarmBinding(provider)}, cache, options)
	right := mustPrewarmer(t, staticBindingResolver{binding: prewarmBinding(provider)}, cache, options)
	if err := left.RunMoving(context.Background()); err == nil {
		t.Fatal("first RunMoving() error = nil, want source failure")
	}
	if err := right.RunMoving(context.Background()); err == nil {
		t.Fatal("second RunMoving() error = nil, want retry after coordinator release")
	}
	if got := provider.directoryCount(); got != 2 {
		t.Fatalf("directory calls after compare-delete release = %d, want two", got)
	}
}

func TestPrewarmConcurrencyCancellationFromSlotAcquisitionNeverCallsSource(t *testing.T) {
	store := newRecordingPrewarmStore()
	store.leaseErr = context.Canceled
	cache := mustNewPrewarmCache(t, store, time.Now)
	limiter := mustPrewarmer(t, staticBindingResolver{}, cache, lifecycleOptions([]string{"UTC"}, time.Now)).SourceCallLimiter()
	called := false
	err := limiter.Do(context.Background(), func(context.Context) error {
		called = true
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("SourceCallLimiter.Do() error = %v, want context.Canceled", err)
	}
	if called {
		t.Fatal("source callback ran after slot acquisition cancellation")
	}
}

func TestPrewarmConcurrencyRedisSlotErrorNeverCallsSource(t *testing.T) {
	store := newRecordingPrewarmStore()
	synthetic := errors.New("synthetic Redis lease failure")
	store.leaseErr = synthetic
	cache := mustNewPrewarmCache(t, store, time.Now)
	limiter := mustPrewarmer(t, staticBindingResolver{}, cache, lifecycleOptions([]string{"UTC"}, time.Now)).SourceCallLimiter()
	called := false
	err := limiter.Do(context.Background(), func(context.Context) error {
		called = true
		return nil
	})
	if !errors.Is(err, synthetic) {
		t.Fatalf("SourceCallLimiter.Do() error = %v, want Redis lease failure", err)
	}
	if called {
		t.Fatal("source callback ran without a distributed slot")
	}
}

func TestPrewarmConcurrencySourceCallGetsDeadlineBelowSlotTTL(t *testing.T) {
	cache, _ := newRedisPrewarmCache(t, time.Now)
	options := lifecycleOptions([]string{"UTC"}, time.Now)
	options.sourceCallTimeout = 40 * time.Millisecond
	limiter := mustPrewarmer(t, staticBindingResolver{}, cache, options).SourceCallLimiter()
	started := time.Now()
	err := limiter.Do(context.Background(), func(ctx context.Context) error {
		deadline, ok := ctx.Deadline()
		if !ok {
			t.Fatal("source callback context has no deadline")
		}
		remaining := time.Until(deadline)
		if remaining <= 0 || remaining >= prewarmWorkerLeaseTTL {
			t.Fatalf("source callback deadline remaining = %v, want (0,90s)", remaining)
		}
		<-ctx.Done()
		return ctx.Err()
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("SourceCallLimiter.Do() error = %v, want context deadline", err)
	}
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		t.Fatalf("bounded source call elapsed = %v, want prompt deadline", elapsed)
	}
}

func TestPrewarmMovingStartSkipsOverlappingSixtySecondTicksAndStopWaits(t *testing.T) {
	if prewarmMovingInterval != 60*time.Second {
		t.Fatalf("moving interval = %v, want 60s", prewarmMovingInterval)
	}
	now := time.Date(2026, 7, 21, 8, 0, 0, 0, time.UTC)
	cache, _ := newRedisPrewarmCache(t, func() time.Time { return now })
	seedPrewarmManifest(t, cache, PrewarmCacheIdentity{ProviderID: 7, ProviderVersion: 11, Timezone: "UTC", AnchorDate: "2026-07-21"}, now, "a")
	provider := newLifecycleProvider([]int64{101})
	provider.directoryEntered = make(chan struct{}, 2)
	provider.directoryRelease = make(chan struct{})
	ticks := make(chan time.Time, 2)
	options := lifecycleOptions([]string{"UTC"}, func() time.Time { return now })
	options.tick = ticks
	prewarmer := mustPrewarmer(t, staticBindingResolver{binding: prewarmBinding(provider)}, cache, options)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	prewarmer.Start(ctx)
	ticks <- now
	select {
	case <-provider.directoryEntered:
	case <-time.After(time.Second):
		t.Fatal("first moving tick did not start")
	}
	ticks <- now.Add(time.Minute)
	time.Sleep(40 * time.Millisecond)
	if got := provider.directoryCount(); got != 1 {
		t.Fatalf("overlapping ticks started %d cycles, want one", got)
	}
	close(provider.directoryRelease)
	prewarmer.Stop()
	if got := provider.directoryCount(); got != 1 {
		t.Fatalf("Stop() returned with %d cycles, want one completed cycle", got)
	}
}

type staticBindingResolver struct {
	binding ProviderBinding
	err     error
}

func (r staticBindingResolver) ResolvePrimaryProviderBinding(context.Context) (ProviderBinding, error) {
	return r.binding, r.err
}

type changingBindingResolver struct {
	mu    sync.Mutex
	first ProviderBinding
	later ProviderBinding
	calls int
}

func (r *changingBindingResolver) ResolvePrimaryProviderBinding(context.Context) (ProviderBinding, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	if r.calls == 1 {
		return r.first, nil
	}
	return r.later, nil
}

type lifecycleProvider struct {
	relay.Provider

	mu                 sync.Mutex
	ids                []int64
	directoryCalls     int
	directoryErr       error
	statsCalls         int
	trendCalls         map[PrewarmSegmentClass]int
	trendErrorTimezone string
	trendErrorClass    PrewarmSegmentClass
	beforeTrend        func()
	directoryEntered   chan struct{}
	directoryRelease   chan struct{}
}

func newLifecycleProvider(ids []int64) *lifecycleProvider {
	return &lifecycleProvider{ids: append([]int64(nil), ids...), trendCalls: make(map[PrewarmSegmentClass]int)}
}

func (p *lifecycleProvider) GetProviderUserIDs(ctx context.Context) (relay.ProviderDirectoryResult, error) {
	p.mu.Lock()
	p.directoryCalls++
	entered, release := p.directoryEntered, p.directoryRelease
	ids := append([]int64(nil), p.ids...)
	p.mu.Unlock()
	if entered != nil {
		entered <- struct{}{}
	}
	if release != nil {
		select {
		case <-release:
		case <-ctx.Done():
			return relay.ProviderDirectoryResult{}, ctx.Err()
		}
	}
	return relay.ProviderDirectoryResult{UserIDs: ids, PageCount: 1}, p.directoryErr
}

func (p *lifecycleProvider) GetProviderCurrentUsageStats(_ context.Context, ids []int64) (relay.ProviderCurrentStatsResult, error) {
	p.mu.Lock()
	p.statsCalls++
	p.mu.Unlock()
	stats := make(map[int64]relay.TeamUserUsageStats, len(ids))
	for _, id := range ids {
		tokens := id * 10
		stats[id] = relay.TeamUserUsageStats{UserID: id, TodayActualCost: 1, TotalActualCost: 2, TotalTokens: &tokens}
	}
	return relay.ProviderCurrentStatsResult{Stats: stats, ResponseBytes: 64}, nil
}

func (p *lifecycleProvider) GetProviderUsageTrend(_ context.Context, params relay.TeamMemberTrendParams, _ int) (relay.ProviderWideTrendResult, error) {
	class := classForCoverage(params)
	p.mu.Lock()
	p.trendCalls[class]++
	before := p.beforeTrend
	failTimezone := p.trendErrorTimezone
	failClass := p.trendErrorClass
	p.mu.Unlock()
	if before != nil {
		before()
	}
	if params.Timezone == failTimezone || class == failClass {
		return relay.ProviderWideTrendResult{}, fmt.Errorf("synthetic %s source failure", class)
	}
	label := params.StartDate
	if params.Granularity == "hour" {
		label += " 00:00"
	}
	tokens := int64(5)
	return relay.ProviderWideTrendResult{
		Points:   []relay.ProviderWideTrendPoint{{UserID: 101, Date: label, ActualCost: 1, TotalTokens: &tokens}},
		Coverage: params, ResponseBytes: 64, PointCount: 1, UniqueUserCount: 1, Complete: true,
	}, nil
}

func classForCoverage(params relay.TeamMemberTrendParams) PrewarmSegmentClass {
	if params.Granularity == "hour" {
		return SegmentTodayHour
	}
	start, _ := time.Parse(time.DateOnly, params.StartDate)
	end, _ := time.Parse(time.DateOnly, params.EndDate)
	if int(end.Sub(start).Hours()/24) >= 28 {
		return SegmentHistory29d
	}
	return SegmentHistory6d
}

func (p *lifecycleProvider) directoryCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.directoryCalls
}

func (p *lifecycleProvider) statsCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.statsCalls
}

func (p *lifecycleProvider) classCount(class PrewarmSegmentClass) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.trendCalls[class]
}

func (p *lifecycleProvider) trendCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	var total int
	for _, count := range p.trendCalls {
		total += count
	}
	return total
}

func (p *lifecycleProvider) totalSourceCalls() int {
	return p.directoryCount() + p.statsCount() + p.trendCount()
}

func (p *lifecycleProvider) resetCounts() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.directoryCalls = 0
	p.statsCalls = 0
	p.trendCalls = make(map[PrewarmSegmentClass]int)
}

func lifecycleOptions(timezones []string, now func() time.Time) PrewarmerOptions {
	var sequence atomic.Uint64
	return PrewarmerOptions{
		Timezones: timezones,
		Now:       now,
		NewToken: func() string {
			value := sha256.Sum256([]byte(fmt.Sprintf("token-%d", sequence.Add(1))))
			return fmt.Sprintf("%x", value[:])
		},
		NewGenerationID: func() string {
			value := sha256.Sum256([]byte(fmt.Sprintf("generation-%d", sequence.Add(1))))
			return fmt.Sprintf("%x", value[:])
		},
	}
}

func mustPrewarmer(t *testing.T, resolver PrimaryProviderBindingResolver, cache *PrewarmCache, options PrewarmerOptions) *Prewarmer {
	t.Helper()
	prewarmer, err := NewPrewarmer(resolver, cache, options)
	if err != nil {
		t.Fatalf("NewPrewarmer() error = %v", err)
	}
	return prewarmer
}

func seedPrewarmManifest(t *testing.T, cache *PrewarmCache, identity PrewarmCacheIdentity, generatedAt time.Time, seed string) PrewarmManifest {
	t.Helper()
	manifest := testPrewarmManifestWithGeneration(t, cache, identity, generatedAt, seed)
	publishSeedManifest(t, cache, manifest)
	return manifest
}

func seedMixedAgePrewarmManifest(t *testing.T, cache *PrewarmCache, identity PrewarmCacheIdentity, now time.Time) PrewarmManifest {
	t.Helper()
	current, err := cache.WriteCurrentStats(context.Background(), testPrewarmCurrentStats(now, "a"))
	if err != nil {
		t.Fatalf("WriteCurrentStats() error = %v", err)
	}
	today, err := cache.WriteSegment(context.Background(), testPrewarmSegment(t, identity, now, SegmentTodayHour, "b"))
	if err != nil {
		t.Fatalf("WriteSegment(today) error = %v", err)
	}
	history29, err := cache.WriteSegment(context.Background(), testPrewarmSegment(t, identity, now.Add(-26*time.Hour), SegmentHistory29d, "c"))
	if err != nil {
		t.Fatalf("WriteSegment(history29) error = %v", err)
	}
	history6, err := cache.WriteSegment(context.Background(), testPrewarmSegment(t, identity, now.Add(-26*time.Hour), SegmentHistory6d, "d"))
	if err != nil {
		t.Fatalf("WriteSegment(history6) error = %v", err)
	}
	manifest := PrewarmManifest{
		SchemaVersion: prewarmCacheSchemaVersion, ProviderID: identity.ProviderID, ProviderVersion: identity.ProviderVersion,
		Timezone: identity.Timezone, TimezoneDigest: prewarmTimezoneDigest(identity.Timezone), AnchorDate: identity.AnchorDate,
		CreatedAt: now, CurrentStats: current, History29d: history29, History6d: history6, TodayHour: today,
	}
	publishSeedManifest(t, cache, manifest)
	return manifest
}

func publishSeedManifest(t *testing.T, cache *PrewarmCache, manifest PrewarmManifest) {
	t.Helper()
	leaseKey := cache.LeaseKey("test-publish", manifest.Timezone, manifest.AnchorDate, manifest.History29d.GenerationID)
	token := strings.Repeat("e", 64)
	acquired, err := cache.TryAcquireLease(context.Background(), leaseKey, token, time.Minute)
	if err != nil || !acquired {
		t.Fatalf("TryAcquireLease(seed) = %v, %v", acquired, err)
	}
	published, err := cache.PublishManifest(context.Background(), leaseKey, token, manifest)
	if err != nil || !published {
		t.Fatalf("PublishManifest(seed) = %v, %v", published, err)
	}
}

func readPrewarmResult(t *testing.T, cache *PrewarmCache, identity PrewarmCacheIdentity) *PrewarmCacheResult {
	t.Helper()
	result, ok, err := cache.Read(context.Background(), identity)
	if err != nil || !ok {
		t.Fatalf("PrewarmCache.Read() = %v, %v, %v", result, ok, err)
	}
	return result
}

type ttlRecordingPrewarmStore struct {
	*recordingPrewarmStore
	ttlMu sync.Mutex
	ttls  []time.Duration
}

func (s *ttlRecordingPrewarmStore) TryAcquireLease(ctx context.Context, key, token string, ttl time.Duration) (bool, error) {
	s.ttlMu.Lock()
	s.ttls = append(s.ttls, ttl)
	s.ttlMu.Unlock()
	return s.recordingPrewarmStore.TryAcquireLease(ctx, key, token, ttl)
}

func (s *ttlRecordingPrewarmStore) resetTTLs() {
	s.ttlMu.Lock()
	defer s.ttlMu.Unlock()
	s.ttls = nil
}

func (s *ttlRecordingPrewarmStore) ttlsCopy() []time.Duration {
	s.ttlMu.Lock()
	defer s.ttlMu.Unlock()
	return append([]time.Duration(nil), s.ttls...)
}

func (s *ttlRecordingPrewarmStore) sawTTL(want time.Duration) bool {
	for _, got := range s.ttlsCopy() {
		if got == want {
			return true
		}
	}
	return false
}

var _ readcache.BatchStore = (*ttlRecordingPrewarmStore)(nil)

type lostSegmentLeasePrewarmStore struct {
	*recordingPrewarmStore

	ttlMu     sync.Mutex
	leaseTTLs map[string]time.Duration
	loseClass PrewarmSegmentClass
}

func (s *lostSegmentLeasePrewarmStore) TryAcquireLease(ctx context.Context, key, token string, ttl time.Duration) (bool, error) {
	s.ttlMu.Lock()
	s.leaseTTLs[key] = ttl
	s.ttlMu.Unlock()
	return s.recordingPrewarmStore.TryAcquireLease(ctx, key, token, ttl)
}

func (s *lostSegmentLeasePrewarmStore) SetIfLeaseOwned(
	ctx context.Context,
	leaseKey, token, key string,
	value []byte,
	ttl time.Duration,
) (bool, error) {
	published, err := s.recordingPrewarmStore.SetIfLeaseOwned(ctx, leaseKey, token, key, value, ttl)
	if err != nil || !published || leaseKey != key || s.loseClass == "" || !strings.Contains(string(value), `"class":"`+string(s.loseClass)+`"`) {
		return published, err
	}
	s.ttlMu.Lock()
	s.recordingPrewarmStore.mu.Lock()
	for candidate, candidateTTL := range s.leaseTTLs {
		if candidate != key && candidateTTL == prewarmWorkerLeaseTTL {
			if _, owned := s.recordingPrewarmStore.leases[candidate]; owned {
				delete(s.recordingPrewarmStore.leases, candidate)
				break
			}
		}
	}
	s.recordingPrewarmStore.mu.Unlock()
	s.ttlMu.Unlock()
	return published, err
}

var _ readcache.BatchStore = (*lostSegmentLeasePrewarmStore)(nil)
var _ relay.ProviderWideTeamUsageProvider = (*lifecycleProvider)(nil)
var _ relay.ProviderWideTeamTrendProvider = (*lifecycleProvider)(nil)
var _ = errors.Is
