package teamusage

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/internal/readcache"
	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/alicebob/miniredis/v2"
	redis "github.com/redis/go-redis/v9"
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
	metrics := &recordingPrewarmRequestMetrics{}
	options := lifecycleOptions(timezones, func() time.Time { return now })
	options.Metrics = metrics
	prewarmer := mustPrewarmer(t, staticBindingResolver{binding: prewarmBinding(provider)}, cache, options)
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
	currentBytes := 0
	timezoneBytes := 0
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
			currentBytes = result.Manifest.CurrentStats.SerializedBytes
		} else if result.Manifest.CurrentStats.GenerationID != sharedGeneration {
			t.Fatalf("current generation = %q, want shared %q", result.Manifest.CurrentStats.GenerationID, sharedGeneration)
		}
		encoded, encodeErr := encodePrewarmJSON(result.Manifest, prewarmManifestMaxBytes)
		if encodeErr != nil {
			t.Fatalf("encode manifest size: %v", encodeErr)
		}
		timezoneBytes += result.Manifest.History29d.SerializedBytes + result.Manifest.History6d.SerializedBytes +
			result.Manifest.TodayHour.SerializedBytes + len(encoded)
	}
	if len(metrics.generation) == 0 || metrics.generation[len(metrics.generation)-1] != currentBytes+timezoneBytes {
		t.Fatalf("generation bytes = %#v, want current once %d + timezones %d", metrics.generation, currentBytes, timezoneBytes)
	}
	for _, timezone := range timezones {
		if !containsPrewarmQuantity(metrics.quantities, PrewarmQuantitySegmentBytes, timezone) ||
			!containsPrewarmQuantity(metrics.quantities, PrewarmQuantityTimezoneBytes, timezone) {
			t.Fatalf("publication quantities for %s = %#v", timezone, metrics.quantities)
		}
	}
}

func containsPrewarmQuantity(metrics []prewarmQuantityMetric, kind PrewarmQuantityKind, timezone string) bool {
	for _, metric := range metrics {
		if metric.kind == kind && metric.timezone == timezone && metric.value > 0 {
			return true
		}
	}
	return false
}

func TestPrewarmGenerationGaugeWaitsForCompleteCurrentGenerationAndResets(t *testing.T) {
	now := time.Date(2026, 7, 21, 8, 0, 0, 0, time.UTC)
	metrics := &recordingPrewarmRequestMetrics{}
	cache := mustNewPrewarmCache(t, newRecordingPrewarmStore(), func() time.Time { return now })
	options := lifecycleOptions([]string{"UTC", "Asia/Shanghai"}, func() time.Time { return now })
	options.Metrics = metrics
	prewarmer := mustPrewarmer(t, staticBindingResolver{binding: prewarmBinding(newLifecycleProvider([]int64{101}))}, cache, options)

	firstUTC := publicationMetricsManifest(7, 11, strings.Repeat("a", 64), "UTC", "2026-07-21")
	firstShanghai := publicationMetricsManifest(7, 11, strings.Repeat("a", 64), "Asia/Shanghai", "2026-07-21")
	beginPublicationMetricsBatch(prewarmer, firstUTC, firstShanghai)
	prewarmer.recordPublishedGeneration(firstUTC)
	if len(metrics.generation) != 0 {
		t.Fatalf("partial generation gauge = %#v, want unchanged", metrics.generation)
	}
	prewarmer.recordPublishedGeneration(firstShanghai)
	if want := fullPublicationBytes(t, firstUTC, firstShanghai); !reflect.DeepEqual(metrics.generation, []int{want}) {
		t.Fatalf("first complete generation gauge = %#v, want %d", metrics.generation, want)
	}

	secondUTC := publicationMetricsManifest(7, 11, strings.Repeat("b", 64), "UTC", "2026-07-21")
	secondShanghai := publicationMetricsManifest(7, 11, strings.Repeat("b", 64), "Asia/Shanghai", "2026-07-21")
	beginPublicationMetricsBatch(prewarmer, secondUTC, secondShanghai)
	prewarmer.recordPublishedGeneration(secondUTC)
	if len(metrics.generation) != 1 {
		t.Fatalf("current-generation reset changed full gauge early: %#v", metrics.generation)
	}
	prewarmer.recordPublishedGeneration(secondShanghai)

	thirdUTC := publicationMetricsManifest(8, 1, strings.Repeat("c", 64), "UTC", "2026-07-21")
	thirdShanghai := publicationMetricsManifest(8, 1, strings.Repeat("c", 64), "Asia/Shanghai", "2026-07-21")
	beginPublicationMetricsBatch(prewarmer, thirdUTC, thirdShanghai)
	prewarmer.recordPublishedGeneration(thirdUTC)
	if len(metrics.generation) != 2 {
		t.Fatalf("provider-version reset changed full gauge early: %#v", metrics.generation)
	}
	prewarmer.recordPublishedGeneration(thirdShanghai)
	if len(metrics.generation) != 3 {
		t.Fatalf("completed generation gauges = %#v, want three complete values", metrics.generation)
	}
}

func TestPrewarmGenerationGaugeExcludesConfiguredSplitUnsafeLane(t *testing.T) {
	now := time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC)
	metrics := &recordingPrewarmRequestMetrics{}
	cache := mustNewPrewarmCache(t, newRecordingPrewarmStore(), func() time.Time { return now })
	options := lifecycleOptions([]string{"UTC", "America/Los_Angeles"}, func() time.Time { return now })
	options.Metrics = metrics
	prewarmer := mustPrewarmer(t, staticBindingResolver{binding: prewarmBinding(newLifecycleProvider([]int64{101}))}, cache, options)

	utc := publicationMetricsManifest(7, 11, strings.Repeat("d", 64), "UTC", "2026-03-09")
	utc.CreatedAt = now
	beginPublicationMetricsBatch(prewarmer, utc)
	prewarmer.recordPublishedGeneration(utc)
	if want := fullPublicationBytes(t, utc); !reflect.DeepEqual(metrics.generation, []int{want}) {
		t.Fatalf("split-safe generation gauge = %#v, want UTC-only complete value %d", metrics.generation, want)
	}
}

func TestPrewarmGenerationGaugeLocksExpectedLanesToManifestBatchTime(t *testing.T) {
	batchTime := time.Date(2026, 7, 21, 8, 0, 0, 0, time.UTC)
	currentTime := time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC)
	metrics := &recordingPrewarmRequestMetrics{}
	cache := mustNewPrewarmCache(t, newRecordingPrewarmStore(), func() time.Time { return currentTime })
	options := lifecycleOptions([]string{"UTC", "America/Los_Angeles"}, func() time.Time { return currentTime })
	options.Metrics = metrics
	prewarmer := mustPrewarmer(t, staticBindingResolver{binding: prewarmBinding(newLifecycleProvider([]int64{101}))}, cache, options)

	utc := publicationMetricsManifest(7, 11, strings.Repeat("f", 64), "UTC", "2026-07-21")
	losAngeles := publicationMetricsManifest(7, 11, strings.Repeat("f", 64), "America/Los_Angeles", "2026-07-21")
	utc.CreatedAt, losAngeles.CreatedAt = batchTime, batchTime
	beginPublicationMetricsBatch(prewarmer, utc, losAngeles)
	prewarmer.recordPublishedGeneration(utc)
	if len(metrics.generation) != 0 {
		t.Fatalf("fresh-clock DST state completed manifest-time generation early: %#v", metrics.generation)
	}
	prewarmer.recordPublishedGeneration(losAngeles)
	if want := fullPublicationBytes(t, utc, losAngeles); !reflect.DeepEqual(metrics.generation, []int{want}) {
		t.Fatalf("manifest-time complete generation gauge = %#v, want %d", metrics.generation, want)
	}
}

func TestPrewarmGenerationGaugeRejectsLateOldAnchorForSameCurrentID(t *testing.T) {
	batchTime := time.Date(2026, 7, 21, 8, 0, 0, 0, time.UTC)
	metrics := &recordingPrewarmRequestMetrics{}
	cache := mustNewPrewarmCache(t, newRecordingPrewarmStore(), func() time.Time { return batchTime })
	options := lifecycleOptions([]string{"UTC", "Asia/Shanghai"}, func() time.Time { return batchTime })
	options.Metrics = metrics
	prewarmer := mustPrewarmer(t, staticBindingResolver{binding: prewarmBinding(newLifecycleProvider([]int64{101}))}, cache, options)

	utc := publicationMetricsManifest(7, 11, strings.Repeat("g", 64), "UTC", "2026-07-21")
	lateShanghai := publicationMetricsManifest(7, 11, strings.Repeat("g", 64), "Asia/Shanghai", "2026-07-20")
	currentShanghai := publicationMetricsManifest(7, 11, strings.Repeat("g", 64), "Asia/Shanghai", "2026-07-21")
	utc.CreatedAt, lateShanghai.CreatedAt, currentShanghai.CreatedAt = batchTime, batchTime, batchTime
	beginPublicationMetricsBatch(prewarmer, utc, currentShanghai)
	prewarmer.recordPublishedGeneration(utc)
	prewarmer.recordPublishedGeneration(lateShanghai)
	if len(metrics.generation) != 0 {
		t.Fatalf("late old-anchor manifest completed current batch: %#v", metrics.generation)
	}
	prewarmer.recordPublishedGeneration(currentShanghai)
	if want := fullPublicationBytes(t, utc, currentShanghai); !reflect.DeepEqual(metrics.generation, []int{want}) {
		t.Fatalf("current-anchor complete generation gauge = %#v, want %d", metrics.generation, want)
	}
}

func publicationMetricsManifest(providerID int, providerVersion int64, currentID, timezone, anchor string) PrewarmManifest {
	ref := func(class PrewarmSegmentClass, size int) PrewarmValueReference {
		return PrewarmValueReference{Class: class, SerializedBytes: size, GenerationID: strings.Repeat(string(rune('e'+size%3)), 64)}
	}
	return PrewarmManifest{
		SchemaVersion: prewarmCacheSchemaVersion, ProviderID: providerID, ProviderVersion: providerVersion,
		Timezone: timezone, TimezoneDigest: prewarmTimezoneDigest(timezone), AnchorDate: anchor, CreatedAt: time.Date(2026, 7, 21, 8, 0, 0, 0, time.UTC),
		CurrentStats: PrewarmValueReference{GenerationID: currentID, SerializedBytes: 100},
		History29d:   ref(SegmentHistory29d, 10), History6d: ref(SegmentHistory6d, 20), TodayHour: ref(SegmentTodayHour, 30),
	}
}

func beginPublicationMetricsBatch(prewarmer *Prewarmer, manifests ...PrewarmManifest) {
	lanes := make([]prewarmTimezoneLane, 0, len(manifests))
	for _, manifest := range manifests {
		lanes = append(lanes, prewarmTimezoneLane{timezone: manifest.Timezone, anchorDate: manifest.AnchorDate})
	}
	prewarmer.beginPublicationBatch(ProviderBinding{
		ProviderID: manifests[0].ProviderID, ProviderVersion: manifests[0].ProviderVersion,
	}, manifests[0].CurrentStats, lanes)
}

func fullPublicationBytes(t *testing.T, manifests ...PrewarmManifest) int {
	t.Helper()
	total := manifests[0].CurrentStats.SerializedBytes
	for _, manifest := range manifests {
		encoded, err := encodePrewarmJSON(manifest, prewarmManifestMaxBytes)
		if err != nil {
			t.Fatalf("encodePrewarmJSON() error = %v", err)
		}
		total += manifest.History29d.SerializedBytes + manifest.History6d.SerializedBytes + manifest.TodayHour.SerializedBytes + len(encoded)
	}
	return total
}

func TestPrewarmMovingPreflightErrorDoesNotBlockHealthyTimezone(t *testing.T) {
	now := time.Date(2026, 7, 21, 8, 0, 0, 0, time.UTC)
	badIdentity := PrewarmCacheIdentity{ProviderID: 7, ProviderVersion: 11, Timezone: "Asia/Shanghai", AnchorDate: "2026-07-21"}
	goodIdentity := PrewarmCacheIdentity{ProviderID: 7, ProviderVersion: 11, Timezone: "UTC", AnchorDate: "2026-07-21"}
	baseStore := newRecordingPrewarmStore()
	seedCache := mustNewPrewarmCache(t, baseStore, func() time.Time { return now })
	bad := seedPrewarmManifest(t, seedCache, badIdentity, now.Add(-30*time.Second), "a")
	good := seedPrewarmManifest(t, seedCache, goodIdentity, now.Add(-30*time.Second), "e")
	store := &laneMGetErrorPrewarmStore{
		BatchStore: &leaseVisiblePrewarmStore{recordingPrewarmStore: baseStore},
		failKey:    bad.TodayHour.Key,
		err:        errors.New("synthetic timezone MGET failure"),
	}
	cache := mustNewPrewarmCache(t, store, func() time.Time { return now })
	provider := newLifecycleProvider([]int64{101})
	prewarmer := mustPrewarmer(t, staticBindingResolver{binding: prewarmBinding(provider)}, cache, lifecycleOptions([]string{"Asia/Shanghai", "UTC"}, func() time.Time { return now }))

	if err := prewarmer.RunMoving(context.Background()); err == nil {
		t.Fatal("RunMoving() error = nil, want bounded bad-timezone preflight error")
	}
	badResult := readPrewarmResult(t, seedCache, badIdentity)
	goodResult := readPrewarmResult(t, seedCache, goodIdentity)
	if badResult.Manifest.TodayHour.GenerationID != bad.TodayHour.GenerationID {
		t.Fatal("bad preflight timezone replaced its prior today generation")
	}
	if goodResult.Manifest.TodayHour.GenerationID == good.TodayHour.GenerationID {
		t.Fatal("healthy timezone was not refreshed after another timezone preflight failed")
	}
	if provider.directoryCount() != 1 || provider.statsCount() != 1 || provider.classCount(SegmentTodayHour) != 1 {
		t.Fatalf("healthy moving sources directory=%d stats=%d today=%d, want one shared current build and one today fetch",
			provider.directoryCount(), provider.statsCount(), provider.classCount(SegmentTodayHour))
	}
}

func TestPrewarmMovingPreflightUsesOneBatchInstantAcrossTimezones(t *testing.T) {
	batchTime := time.Date(2026, 7, 21, 15, 59, 0, 0, time.UTC)
	rolloverTime := batchTime.Add(2 * time.Minute)
	cache, _ := newRedisPrewarmCache(t, func() time.Time { return batchTime })
	for index, timezone := range []string{"UTC", "Asia/Shanghai"} {
		seedPrewarmManifest(t, cache, PrewarmCacheIdentity{
			ProviderID: 7, ProviderVersion: 11, Timezone: timezone, AnchorDate: "2026-07-21",
		}, batchTime.Add(-30*time.Second), string(rune('a'+index*4)))
	}
	var clockCalls atomic.Int32
	now := func() time.Time {
		if clockCalls.Add(1) == 1 {
			return batchTime
		}
		return rolloverTime
	}
	provider := newLifecycleProvider([]int64{101})
	prewarmer := mustPrewarmer(t, staticBindingResolver{binding: prewarmBinding(provider)}, cache, lifecycleOptions([]string{"UTC", "Asia/Shanghai"}, now))

	lanes, err := prewarmer.preflightMovingLanes(context.Background(), prewarmBinding(provider))
	if err != nil {
		t.Fatalf("preflightMovingLanes() error = %v", err)
	}
	if len(lanes) != 2 || lanes[0].anchorDate != "2026-07-21" || lanes[1].anchorDate != "2026-07-21" {
		t.Fatalf("batch-instant preflight lanes = %#v, want both 2026-07-21 anchors", lanes)
	}
	if calls := clockCalls.Load(); calls != 1 {
		t.Fatalf("preflight clock calls = %d, want one batch instant", calls)
	}
}

func TestPrewarmMovingDoesNotBootstrapMissingManifest(t *testing.T) {
	now := time.Date(2026, 7, 21, 8, 0, 0, 0, time.UTC)
	cache, _ := newRedisPrewarmCache(t, func() time.Time { return now })
	provider := newLifecycleProvider([]int64{101})
	prewarmer := mustPrewarmer(t, staticBindingResolver{binding: prewarmBinding(provider)}, cache, lifecycleOptions([]string{"UTC"}, func() time.Time { return now }))
	if err := prewarmer.RunMoving(context.Background()); err != nil {
		t.Fatalf("RunMoving(missing manifest) error = %v", err)
	}
	if got := provider.totalSourceCalls(); got != 0 {
		t.Fatalf("RunMoving(missing manifest) made %d recovery source calls, want zero", got)
	}
}

func TestPrewarmRecoveryRunningLifecycleBootstrapsNewAnchorAfterBothJitters(t *testing.T) {
	dayOne := time.Date(2026, 7, 21, 23, 59, 0, 0, time.UTC)
	dayTwo := time.Date(2026, 7, 22, 0, 0, 0, 0, time.UTC)
	var clock atomic.Value
	clock.Store(dayOne)
	now := func() time.Time { return clock.Load().(time.Time) }
	cache, server := newRedisPrewarmCache(t, now)
	seedPrewarmManifest(t, cache, PrewarmCacheIdentity{ProviderID: 7, ProviderVersion: 11, Timezone: "UTC", AnchorDate: "2026-07-21"}, dayOne, "a")
	provider := newLifecycleProvider([]int64{101})
	ticks := make(chan time.Time, 2)
	options := lifecycleOptions([]string{"UTC"}, now)
	options.tick = ticks
	prewarmer := mustPrewarmer(t, staticBindingResolver{binding: prewarmBinding(provider)}, cache, options)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	prewarmer.Start(ctx)
	select {
	case <-prewarmer.startupDone:
	case <-time.After(time.Second):
		t.Fatal("day-one startup did not complete")
	}

	binding := prewarmBinding(provider)
	jitter29 := prewarmHistoricalJitter(binding, "UTC", "2026-07-22", SegmentHistory29d)
	jitter6 := prewarmHistoricalJitter(binding, "UTC", "2026-07-22", SegmentHistory6d)
	minimumJitter := jitter29
	if jitter6 < minimumJitter {
		minimumJitter = jitter6
	}
	if minimumJitter <= 0 {
		t.Fatalf("test fixture minimum jitter = %v, want positive pre-due window", minimumJitter)
	}
	clock.Store(dayTwo.Add(minimumJitter / 2))
	ticks <- now()
	preJitterTick := now().Truncate(prewarmMovingInterval).UTC().Format(time.RFC3339)
	recoveryTickKey := cache.LeaseKey("recovery-tick", "7", "11", prewarmer.allowlistDigest, preJitterTick, "recovery")
	waitForPrewarmCondition(t, time.Second, func() bool {
		return server.Exists(recoveryTickKey) && !prewarmer.moving.Load() && !prewarmer.recovery.Load() && prewarmer.scheduledWorkers.Load() == 0
	})
	if got := provider.totalSourceCalls(); got != 0 {
		t.Fatalf("pre-jitter rollover made %d source calls, want zero", got)
	}

	maximumJitter := jitter29
	if jitter6 > maximumJitter {
		maximumJitter = jitter6
	}
	clock.Store(dayTwo.Add(maximumJitter + time.Second))
	ticks <- now()
	newIdentity := PrewarmCacheIdentity{ProviderID: 7, ProviderVersion: 11, Timezone: "UTC", AnchorDate: "2026-07-22"}
	waitForPrewarmCondition(t, 2*time.Second, func() bool {
		result, found, err := cache.Read(context.Background(), newIdentity)
		return err == nil && found && result != nil && result.Complete
	})
	if provider.directoryCount() != 1 || provider.statsCount() != 1 ||
		provider.classCount(SegmentHistory29d) != 1 || provider.classCount(SegmentHistory6d) != 1 ||
		provider.classCount(SegmentTodayHour) != 1 {
		t.Fatalf("rollover source calls directory=%d stats=%d history29=%d history6=%d today=%d, want 1 each",
			provider.directoryCount(), provider.statsCount(), provider.classCount(SegmentHistory29d),
			provider.classCount(SegmentHistory6d), provider.classCount(SegmentTodayHour))
	}
	prewarmer.Stop()
}

func TestPrewarmRecoverySharesOneCurrentGenerationAcrossMissingLanes(t *testing.T) {
	now := time.Date(2026, 7, 21, 8, 0, 0, 0, time.UTC)
	cache, _ := newRedisPrewarmCache(t, func() time.Time { return now })
	provider := newLifecycleProvider([]int64{101})
	timezones := []string{"Asia/Shanghai", "UTC"}
	prewarmer := mustPrewarmer(t, staticBindingResolver{binding: prewarmBinding(provider)}, cache, lifecycleOptions(timezones, func() time.Time { return now }))
	if err := prewarmer.runRecovery(context.Background()); err != nil {
		t.Fatalf("runRecovery() error = %v", err)
	}
	if provider.directoryCount() != 1 || provider.statsCount() != 1 ||
		provider.classCount(SegmentHistory29d) != 2 || provider.classCount(SegmentHistory6d) != 2 || provider.classCount(SegmentTodayHour) != 2 {
		t.Fatalf("recovery sources directory=%d stats=%d history29=%d history6=%d today=%d, want 1/1/2/2/2",
			provider.directoryCount(), provider.statsCount(), provider.classCount(SegmentHistory29d),
			provider.classCount(SegmentHistory6d), provider.classCount(SegmentTodayHour))
	}
	var currentGeneration string
	for _, timezone := range timezones {
		result := readPrewarmResult(t, cache, PrewarmCacheIdentity{ProviderID: 7, ProviderVersion: 11, Timezone: timezone, AnchorDate: "2026-07-21"})
		if !result.Complete {
			t.Fatalf("recovery timezone %s manifest is incomplete", timezone)
		}
		if currentGeneration == "" {
			currentGeneration = result.Manifest.CurrentStats.GenerationID
		} else if result.Manifest.CurrentStats.GenerationID != currentGeneration {
			t.Fatalf("recovery timezone %s current generation = %q, want shared %q", timezone, result.Manifest.CurrentStats.GenerationID, currentGeneration)
		}
	}
}

func TestPrewarmRecoveryLaneFailureDoesNotBlockHealthyLane(t *testing.T) {
	now := time.Date(2026, 7, 21, 8, 0, 0, 0, time.UTC)
	cache, server := newRedisPrewarmCache(t, func() time.Time { return now })
	provider := newLifecycleProvider([]int64{101})
	provider.trendErrorTimezone = "Asia/Shanghai"
	prewarmer := mustPrewarmer(t, staticBindingResolver{binding: prewarmBinding(provider)}, cache, lifecycleOptions([]string{"Asia/Shanghai", "UTC"}, func() time.Time { return now }))
	if err := prewarmer.runRecovery(context.Background()); err == nil {
		t.Fatal("runRecovery() error = nil, want bounded bad-lane failure")
	}
	healthyIdentity := PrewarmCacheIdentity{ProviderID: 7, ProviderVersion: 11, Timezone: "UTC", AnchorDate: "2026-07-21"}
	if result := readPrewarmResult(t, cache, healthyIdentity); !result.Complete {
		t.Fatal("healthy recovery lane did not publish a complete manifest")
	}
	badIdentity := PrewarmCacheIdentity{ProviderID: 7, ProviderVersion: 11, Timezone: "Asia/Shanghai", AnchorDate: "2026-07-21"}
	if _, found, err := cache.Read(context.Background(), badIdentity); err != nil || found {
		t.Fatalf("bad recovery lane Read() found=%v err=%v, want no manifest", found, err)
	}
	if provider.directoryCount() != 1 || provider.statsCount() != 1 {
		t.Fatalf("recovery current builds directory=%d stats=%d, want one shared build", provider.directoryCount(), provider.statsCount())
	}
	if got := countLeasesWithTTL(server, 80*time.Second, 91*time.Second); got != 0 {
		t.Fatalf("recovery worker/source leases after lane failure = %d, want zero", got)
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
	metrics := &recordingPrewarmRequestMetrics{}
	reporter := &recordingPrewarmReporter{events: make(chan PrewarmBackgroundEvent, 4)}
	options := lifecycleOptions(timezones, func() time.Time { return now })
	options.Metrics, options.Reporter = metrics, reporter
	prewarmer := mustPrewarmer(t, staticBindingResolver{binding: prewarmBinding(provider)}, cache, options)
	startedAt := time.Now()
	err := prewarmer.RunMoving(context.Background())
	if err == nil {
		t.Fatal("RunMoving() error = nil, want bounded timezone failure")
	}
	prewarmer.reportScheduledLifecycle(strings.Repeat("m", 32), []prewarmLifecycleTarget{
		{class: PrewarmCycleMoving, timezone: "UTC"},
		{class: PrewarmCycleMoving, timezone: "Asia/Shanghai"},
	}, startedAt, err)
	if got := countPrewarmCycleOutcome(metrics.cycles, PrewarmCycleMoving, "Asia/Shanghai", PrewarmCycleError); got != 1 {
		t.Fatalf("bad moving lane error metrics = %d, want one; all=%#v", got, metrics.cycles)
	}
	if got := countPrewarmCycleOutcome(metrics.cycles, PrewarmCycleMoving, "UTC", PrewarmCycleError); got != 0 {
		t.Fatalf("healthy moving lane error metrics = %d, want zero; all=%#v", got, metrics.cycles)
	}
	if events := drainPrewarmReporter(reporter); len(events) != 1 || events[0].Timezone != "Asia/Shanghai" || events[0].Class != PrewarmCycleMoving {
		t.Fatalf("moving failure events = %#v, want exact bad lane only", events)
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

func TestPrewarmMovingSharedFailureReportsOnlyPreflightSelectedLanes(t *testing.T) {
	now := time.Date(2026, 3, 9, 12, 0, 0, 0, time.UTC)
	cache, _ := newRedisPrewarmCache(t, func() time.Time { return now })
	seedPrewarmManifest(t, cache, PrewarmCacheIdentity{
		ProviderID: 7, ProviderVersion: 11, Timezone: "UTC", AnchorDate: "2026-03-09",
	}, now.Add(-30*time.Second), "a")
	provider := newLifecycleProvider([]int64{101})
	provider.directoryErr = errors.New("dynamic shared source detail")
	metrics := &recordingPrewarmRequestMetrics{}
	reporter := &recordingPrewarmReporter{events: make(chan PrewarmBackgroundEvent, 4)}
	options := lifecycleOptions([]string{"UTC", "America/Los_Angeles"}, func() time.Time { return now })
	options.Metrics, options.Reporter = metrics, reporter
	prewarmer := mustPrewarmer(t, staticBindingResolver{binding: prewarmBinding(provider)}, cache, options)

	err := prewarmer.runMoving(context.Background())
	if err == nil {
		t.Fatal("runMoving() error = nil, want shared source failure")
	}
	prewarmer.reportScheduledLifecycle(strings.Repeat("l", 32), prewarmLifecycleTargets(PrewarmCycleMoving, options.Timezones), time.Now(), err)

	if got := countPrewarmCycleOutcome(metrics.cycles, PrewarmCycleMoving, "UTC", PrewarmCycleError); got != 1 {
		t.Fatalf("selected UTC shared-failure metrics = %d, want one; all=%#v", got, metrics.cycles)
	}
	if got := countPrewarmCycleOutcome(metrics.cycles, PrewarmCycleMoving, "America/Los_Angeles", PrewarmCycleError); got != 0 {
		t.Fatalf("split-unsafe LA shared-failure metrics = %d, want zero; all=%#v", got, metrics.cycles)
	}
	if events := drainPrewarmReporter(reporter); len(events) != 1 || events[0].Timezone != "UTC" {
		t.Fatalf("selected-lane shared-failure events = %#v, want UTC only", events)
	}
}

func TestPrewarmMovingPostLaneFailureDoesNotDoubleRecordCycleOutcome(t *testing.T) {
	now := time.Date(2026, 7, 21, 8, 0, 0, 0, time.UTC)
	cache, server := newRedisPrewarmCache(t, func() time.Time { return now })
	seedPrewarmManifest(t, cache, PrewarmCacheIdentity{
		ProviderID: 7, ProviderVersion: 11, Timezone: "UTC", AnchorDate: "2026-07-21",
	}, now.Add(-30*time.Second), "a")
	provider := newLifecycleProvider([]int64{101})
	metrics := &recordingPrewarmRequestMetrics{}
	reporter := &recordingPrewarmReporter{events: make(chan PrewarmBackgroundEvent, 2)}
	options := lifecycleOptions([]string{"UTC"}, func() time.Time { return now })
	options.Metrics, options.Reporter = metrics, reporter
	prewarmer := mustPrewarmer(t, staticBindingResolver{binding: prewarmBinding(provider)}, cache, options)
	activeKey := cache.LeaseKey("moving-active", "7", "11", prewarmer.allowlistDigest)
	var deleteActive sync.Once
	metrics.cycleHook = func(class, timezone, outcome string) {
		if class == string(PrewarmCycleMoving) && timezone == "UTC" && outcome == "success" {
			deleteActive.Do(func() { server.Del(activeKey) })
		}
	}

	err := prewarmer.runMoving(context.Background())
	if err == nil {
		t.Fatal("runMoving() error = nil, want post-lane ownership failure")
	}
	prewarmer.reportScheduledLifecycle(strings.Repeat("o", 32), prewarmLifecycleTargets(PrewarmCycleMoving, options.Timezones), time.Now(), err)

	if got := countPrewarmCycleOutcome(metrics.cycles, PrewarmCycleMoving, "UTC", PrewarmCycleSuccess); got != 1 {
		t.Fatalf("lane success metrics = %d, want one; all=%#v", got, metrics.cycles)
	}
	if len(metrics.cycles) != 1 {
		t.Fatalf("post-lane ownership failure double-recorded cycle metrics: %#v", metrics.cycles)
	}
	if events := drainPrewarmReporter(reporter); len(events) != 1 || events[0].Timezone != "UTC" || events[0].Outcome != PrewarmCycleError {
		t.Fatalf("post-lane ownership failure events = %#v", events)
	}
}

func TestPrewarmRecoveryPostLaneFailureDoesNotDoubleRecordCycleOutcome(t *testing.T) {
	now := time.Date(2026, 7, 21, 8, 0, 0, 0, time.UTC)
	cache, server := newRedisPrewarmCache(t, func() time.Time { return now })
	provider := newLifecycleProvider([]int64{101})
	metrics := &recordingPrewarmRequestMetrics{}
	reporter := &recordingPrewarmReporter{events: make(chan PrewarmBackgroundEvent, 2)}
	options := lifecycleOptions([]string{"UTC"}, func() time.Time { return now })
	options.Metrics, options.Reporter = metrics, reporter
	prewarmer := mustPrewarmer(t, staticBindingResolver{binding: prewarmBinding(provider)}, cache, options)
	activeKey := cache.LeaseKey("recovery-active", "7", "11", prewarmer.allowlistDigest)
	var deleteActive sync.Once
	metrics.cycleHook = func(class, timezone, outcome string) {
		if class == string(PrewarmCycleRecovery) && timezone == "UTC" && outcome == "success" {
			deleteActive.Do(func() { server.Del(activeKey) })
		}
	}

	err := prewarmer.runRecovery(context.Background())
	if err == nil {
		t.Fatal("runRecovery() error = nil, want post-lane ownership failure")
	}
	prewarmer.reportScheduledLifecycle(strings.Repeat("q", 32), prewarmLifecycleTargets(PrewarmCycleRecovery, options.Timezones), time.Now(), err)

	if got := countPrewarmCycleOutcome(metrics.cycles, PrewarmCycleRecovery, "UTC", PrewarmCycleSuccess); got != 1 {
		t.Fatalf("recovery lane success metrics = %d, want one; all=%#v", got, metrics.cycles)
	}
	if len(metrics.cycles) != 1 {
		t.Fatalf("recovery post-lane ownership failure double-recorded cycle metrics: %#v", metrics.cycles)
	}
	if events := drainPrewarmReporter(reporter); len(events) != 1 || events[0].Timezone != "UTC" || events[0].Outcome != PrewarmCycleError {
		t.Fatalf("recovery post-lane ownership failure events = %#v", events)
	}
}

func TestPrewarmHistoricalRefreshesIndependentSegmentsAndSkipsSplitUnsafe(t *testing.T) {
	now := time.Date(2026, 7, 21, 8, 0, 0, 0, time.UTC)
	cache, _ := newRedisPrewarmCache(t, func() time.Time { return now })
	identity := PrewarmCacheIdentity{ProviderID: 7, ProviderVersion: 11, Timezone: "UTC", AnchorDate: "2026-07-21"}
	old := seedMixedAgePrewarmManifest(t, cache, identity, now)
	provider := newLifecycleProvider([]int64{101})
	provider.trendErrorClass = SegmentHistory6d
	metrics := &recordingPrewarmRequestMetrics{}
	reporter := &recordingPrewarmReporter{events: make(chan PrewarmBackgroundEvent, 4)}
	options := lifecycleOptions([]string{"UTC", "America/Los_Angeles"}, func() time.Time { return now })
	options.Metrics, options.Reporter = metrics, reporter
	prewarmer := mustPrewarmer(t, staticBindingResolver{binding: prewarmBinding(provider)}, cache, options)
	startedAt := time.Now()
	err := prewarmer.RunHistorical(context.Background(), "UTC", now)
	if err == nil {
		t.Fatal("RunHistorical() error = nil, want independent history_6d failure")
	}
	prewarmer.reportScheduledLifecycle(strings.Repeat("h", 32), []prewarmLifecycleTarget{
		{class: PrewarmCycleHistory29d, timezone: "UTC"},
		{class: PrewarmCycleHistory6d, timezone: "UTC"},
	}, startedAt, err)
	if got := countPrewarmCycleOutcome(metrics.cycles, PrewarmCycleHistory6d, "UTC", PrewarmCycleError); got != 1 {
		t.Fatalf("history_6d error metrics = %d, want one; all=%#v", got, metrics.cycles)
	}
	if got := countPrewarmCycleOutcome(metrics.cycles, PrewarmCycleHistory29d, "UTC", PrewarmCycleError); got != 0 {
		t.Fatalf("healthy history_29d error metrics = %d, want zero; all=%#v", got, metrics.cycles)
	}
	if events := drainPrewarmReporter(reporter); len(events) != 1 || events[0].Class != PrewarmCycleHistory6d || events[0].Timezone != "UTC" {
		t.Fatalf("historical failure events = %#v, want history_6d only", events)
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

	deleteLeasesWithTTL(server, 5*time.Minute, 7*time.Minute)
	provider.resetCounts()
	now = base.Add(2 * time.Minute)
	if err := prewarmer.runStartup(context.Background()); err != nil {
		t.Fatalf("runStartup(stale hard-valid) error = %v", err)
	}
	if provider.totalSourceCalls() != 0 {
		t.Fatalf("startup rebuilt stale-but-hard-valid values with %d calls", provider.totalSourceCalls())
	}

	deleteLeasesWithTTL(server, 5*time.Minute, 7*time.Minute)
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

func TestPrewarmStartupRetainsInvalidTodayWithoutSourceCall(t *testing.T) {
	base := time.Date(2026, 7, 21, 8, 0, 0, 0, time.UTC)
	now := base.Add(30 * time.Second)
	store := &leaseVisiblePrewarmStore{recordingPrewarmStore: newRecordingPrewarmStore()}
	cache := mustNewPrewarmCache(t, store, func() time.Time { return now })
	identity := PrewarmCacheIdentity{ProviderID: 7, ProviderVersion: 11, Timezone: "UTC", AnchorDate: "2026-07-21"}
	manifest := seedPrewarmManifest(t, cache, identity, base, "a")
	store.mu.Lock()
	store.values[manifest.TodayHour.Key] = []byte(`{"invalid":true}`)
	store.mu.Unlock()
	result, ok, err := cache.Read(context.Background(), identity)
	if err != nil || !ok || result.TodayHourStatus != PrewarmValueInvalid {
		t.Fatalf("Read(invalid today) = %v, %v, %v", result, ok, err)
	}
	provider := newLifecycleProvider([]int64{101})
	prewarmer := mustPrewarmer(t, staticBindingResolver{binding: prewarmBinding(provider)}, cache, lifecycleOptions([]string{"UTC"}, func() time.Time { return now }))
	if err := prewarmer.runStartup(context.Background()); err != nil {
		t.Fatalf("runStartup(invalid today) error = %v", err)
	}
	if got := provider.totalSourceCalls(); got != 0 {
		t.Fatalf("invalid hard-valid today triggered %d startup source calls, want zero", got)
	}
}

func TestPrewarmLeaseLostOrProviderVersionChangedCannotPublish(t *testing.T) {
	t.Run("lost moving coordinator", func(t *testing.T) {
		now := time.Date(2026, 7, 21, 8, 0, 0, 0, time.UTC)
		cache, server := newRedisPrewarmCache(t, func() time.Time { return now })
		identity := PrewarmCacheIdentity{ProviderID: 7, ProviderVersion: 11, Timezone: "UTC", AnchorDate: "2026-07-21"}
		old := seedPrewarmManifest(t, cache, identity, now.Add(-30*time.Second), "a")
		provider := newLifecycleProvider([]int64{101})
		provider.afterDirectory = func() {
			deleteLeasesWithTTL(server, 2*time.Minute, 4*time.Minute)
		}
		prewarmer := mustPrewarmer(t, staticBindingResolver{binding: prewarmBinding(provider)}, cache, lifecycleOptions([]string{"UTC"}, func() time.Time { return now }))
		if err := prewarmer.RunMoving(context.Background()); err == nil {
			t.Fatal("RunMoving(lost coordinator) error = nil, want ownership failure")
		}
		if got := provider.statsCount() + provider.trendCount(); got != 0 {
			t.Fatalf("lost coordinator allowed %d continued source calls, want zero", got)
		}
		result := readPrewarmResult(t, cache, identity)
		if result.Manifest.CurrentStats.GenerationID != old.CurrentStats.GenerationID || result.Manifest.TodayHour.GenerationID != old.TodayHour.GenerationID {
			t.Fatal("lost moving coordinator published a new manifest")
		}
	})

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

func TestPrewarmLeaseWorkerDeadlinesAreStrictlyBelowLeaseTTLs(t *testing.T) {
	if prewarmMovingWorkerTimeout <= 0 || prewarmMovingWorkerTimeout >= prewarmMovingCoordinatorTTL {
		t.Fatalf("moving worker timeout = %v, want (0,%v)", prewarmMovingWorkerTimeout, prewarmMovingCoordinatorTTL)
	}
	if prewarmHistoricalWorkerTimeout <= 0 || prewarmHistoricalWorkerTimeout >= prewarmHistoryCoordinatorTTL {
		t.Fatalf("historical worker timeout = %v, want (0,%v)", prewarmHistoricalWorkerTimeout, prewarmHistoryCoordinatorTTL)
	}
	if prewarmStartupWorkerTimeout <= 0 || prewarmStartupWorkerTimeout >= prewarmHistoryCoordinatorTTL {
		t.Fatalf("startup worker timeout = %v, want (0,%v)", prewarmStartupWorkerTimeout, prewarmHistoryCoordinatorTTL)
	}
	if prewarmSegmentWorkerTimeout <= 0 || prewarmSegmentWorkerTimeout >= prewarmWorkerLeaseTTL {
		t.Fatalf("segment worker timeout = %v, want (0,%v)", prewarmSegmentWorkerTimeout, prewarmWorkerLeaseTTL)
	}
}

func TestPrewarmLeaseQueuedSegmentExpiresWithoutSourceOrPublish(t *testing.T) {
	now := time.Date(2026, 7, 21, 8, 0, 0, 0, time.UTC)
	cache, _ := newRedisPrewarmCache(t, func() time.Time { return now })
	identity := PrewarmCacheIdentity{ProviderID: 7, ProviderVersion: 11, Timezone: "UTC", AnchorDate: "2026-07-21"}
	old := seedPrewarmManifest(t, cache, identity, now, "a")
	provider := newLifecycleProvider([]int64{101})
	options := lifecycleOptions([]string{"UTC"}, func() time.Time { return now })
	options.segmentWorkerTimeout = 40 * time.Millisecond
	options.sourceCallTimeout = time.Second
	prewarmer := mustPrewarmer(t, staticBindingResolver{binding: prewarmBinding(provider)}, cache, options)

	release := make(chan struct{})
	entered := make(chan struct{}, 2)
	var blockers sync.WaitGroup
	for index := 0; index < 2; index++ {
		blockers.Add(1)
		go func() {
			defer blockers.Done()
			_ = prewarmer.SourceCallLimiter().Do(context.Background(), func(context.Context) error {
				entered <- struct{}{}
				<-release
				return nil
			})
		}()
	}
	for index := 0; index < 2; index++ {
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatal("failed to occupy both process source positions")
		}
	}

	resultCh := make(chan error, 1)
	go func() {
		resultCh <- prewarmer.runMovingTimezone(context.Background(), prewarmBinding(provider), old.CurrentStats, "UTC")
	}()
	var runErr error
	returnedBeforeRelease := false
	select {
	case runErr = <-resultCh:
		returnedBeforeRelease = true
	case <-time.After(150 * time.Millisecond):
	}
	close(release)
	blockers.Wait()
	if !returnedBeforeRelease {
		select {
		case runErr = <-resultCh:
		case <-time.After(time.Second):
			t.Fatal("queued segment did not finish after blocker release")
		}
	}
	if !returnedBeforeRelease || !errors.Is(runErr, context.DeadlineExceeded) {
		t.Fatalf("queued segment returned before release=%v error=%v, want prompt deadline", returnedBeforeRelease, runErr)
	}
	if got := provider.trendCount(); got != 0 {
		t.Fatalf("expired queued segment made %d source calls, want zero", got)
	}
	result := readPrewarmResult(t, cache, identity)
	if result.Manifest.TodayHour.GenerationID != old.TodayHour.GenerationID {
		t.Fatal("expired queued segment published a manifest")
	}
}

func TestPrewarmLeaseMovingCoordinatorRetainsSuccessAndReleasesEarlyFailure(t *testing.T) {
	t.Run("successful tick retained", func(t *testing.T) {
		now := time.Date(2026, 7, 21, 8, 0, 0, 0, time.UTC)
		cache, server := newRedisPrewarmCache(t, func() time.Time { return now })
		seedPrewarmManifest(t, cache, PrewarmCacheIdentity{ProviderID: 7, ProviderVersion: 11, Timezone: "UTC", AnchorDate: "2026-07-21"}, now, "a")
		provider := newLifecycleProvider([]int64{101})
		options := lifecycleOptions([]string{"UTC"}, func() time.Time { return now })
		left := mustPrewarmer(t, staticBindingResolver{binding: prewarmBinding(provider)}, cache, options)
		right := mustPrewarmer(t, staticBindingResolver{binding: prewarmBinding(provider)}, cache, options)
		if err := left.RunMoving(context.Background()); err != nil {
			t.Fatalf("left RunMoving() error = %v", err)
		}
		if err := right.RunMoving(context.Background()); err != nil {
			t.Fatalf("right RunMoving() error = %v", err)
		}
		if got := provider.directoryCount(); got != 1 {
			t.Fatalf("same-tick directory calls = %d, want one", got)
		}
		if got := countLeasesWithTTL(server, 2*time.Minute, 4*time.Minute); got != 1 {
			t.Fatalf("retained moving coordinator count = %d, want one", got)
		}
	})

	t.Run("early failure released", func(t *testing.T) {
		now := time.Date(2026, 7, 21, 8, 0, 0, 0, time.UTC)
		cache, _ := newRedisPrewarmCache(t, func() time.Time { return now })
		seedPrewarmManifest(t, cache, PrewarmCacheIdentity{ProviderID: 7, ProviderVersion: 11, Timezone: "UTC", AnchorDate: "2026-07-21"}, now, "a")
		provider := newLifecycleProvider([]int64{101})
		provider.setDirectoryError(errors.New("synthetic directory failure"))
		options := lifecycleOptions([]string{"UTC"}, func() time.Time { return now })
		left := mustPrewarmer(t, staticBindingResolver{binding: prewarmBinding(provider)}, cache, options)
		right := mustPrewarmer(t, staticBindingResolver{binding: prewarmBinding(provider)}, cache, options)
		if err := left.RunMoving(context.Background()); err == nil {
			t.Fatal("first RunMoving() error = nil, want source failure")
		}
		provider.setDirectoryError(nil)
		if err := right.RunMoving(context.Background()); err != nil {
			t.Fatalf("retry RunMoving() error = %v", err)
		}
		if got := provider.directoryCount(); got != 2 {
			t.Fatalf("directory calls after early-failure release = %d, want two", got)
		}
	})
}

func TestPrewarmLeaseActiveMovingPreventsAdjacentTickOverlap(t *testing.T) {
	base := time.Date(2026, 7, 21, 8, 0, 0, 0, time.UTC)
	cache, _ := newRedisPrewarmCache(t, func() time.Time { return base })
	seedPrewarmManifest(t, cache, PrewarmCacheIdentity{ProviderID: 7, ProviderVersion: 11, Timezone: "UTC", AnchorDate: "2026-07-21"}, base, "a")
	provider := newLifecycleProvider([]int64{101})
	provider.directoryEntered = make(chan struct{}, 3)
	provider.directoryRelease = make(chan struct{})
	leftNow := base
	rightNow := base.Add(time.Minute)
	left := mustPrewarmer(t, staticBindingResolver{binding: prewarmBinding(provider)}, cache, lifecycleOptions([]string{"UTC"}, func() time.Time { return leftNow }))
	right := mustPrewarmer(t, staticBindingResolver{binding: prewarmBinding(provider)}, cache, lifecycleOptions([]string{"UTC"}, func() time.Time { return rightNow }))
	leftResult := make(chan error, 1)
	go func() { leftResult <- left.RunMoving(context.Background()) }()
	select {
	case <-provider.directoryEntered:
	case <-time.After(time.Second):
		t.Fatal("tick N did not enter source")
	}
	nextCtx, cancelNext := context.WithTimeout(context.Background(), 100*time.Millisecond)
	err := right.RunMoving(nextCtx)
	cancelNext()
	if err != nil {
		t.Fatalf("tick N+1 active-busy RunMoving() error = %v", err)
	}
	if got := provider.directoryCount(); got != 1 {
		t.Fatalf("tick N+1 source calls raised directory count to %d, want one", got)
	}
	close(provider.directoryRelease)
	if err := <-leftResult; err != nil {
		t.Fatalf("tick N RunMoving() error = %v", err)
	}
	if err := right.RunMoving(context.Background()); err != nil {
		t.Fatalf("tick N+1 replay RunMoving() error = %v", err)
	}
	if got := provider.directoryCount(); got != 1 {
		t.Fatalf("tick N+1 replay raised directory count to %d, want one", got)
	}
	rightNow = base.Add(2 * time.Minute)
	if err := right.RunMoving(context.Background()); err != nil {
		t.Fatalf("tick N+2 RunMoving() error = %v", err)
	}
	if got := provider.directoryCount(); got != 2 {
		t.Fatalf("tick N+2 directory count = %d, want two", got)
	}
}

func TestPrewarmRecoveryActiveLeasePreventsAdjacentTickOverlap(t *testing.T) {
	base := time.Date(2026, 7, 21, 8, 0, 0, 0, time.UTC)
	var cacheClock atomic.Value
	cacheClock.Store(base)
	cache, server := newRedisPrewarmCache(t, func() time.Time { return cacheClock.Load().(time.Time) })
	provider := newLifecycleProvider([]int64{101})
	provider.directoryEntered = make(chan struct{}, 3)
	provider.directoryRelease = make(chan struct{})
	leftNow := base
	rightNow := base.Add(time.Minute)
	sharedOptions := lifecycleOptions([]string{"UTC"}, func() time.Time { return base })
	leftOptions := sharedOptions
	leftOptions.Now = func() time.Time { return leftNow }
	rightOptions := sharedOptions
	rightOptions.Now = func() time.Time { return rightNow }
	left := mustPrewarmer(t, staticBindingResolver{binding: prewarmBinding(provider)}, cache, leftOptions)
	right := mustPrewarmer(t, staticBindingResolver{binding: prewarmBinding(provider)}, cache, rightOptions)
	leftResult := make(chan error, 1)
	go func() { leftResult <- left.runRecovery(context.Background()) }()
	select {
	case <-provider.directoryEntered:
	case <-time.After(time.Second):
		t.Fatal("recovery tick N did not enter source")
	}
	nextCtx, cancelNext := context.WithTimeout(context.Background(), 100*time.Millisecond)
	err := right.runRecovery(nextCtx)
	cancelNext()
	if err != nil {
		t.Fatalf("recovery tick N+1 active-busy error = %v", err)
	}
	if got := provider.directoryCount(); got != 1 {
		t.Fatalf("recovery tick N+1 raised directory count to %d, want one", got)
	}
	close(provider.directoryRelease)
	if err := <-leftResult; err != nil {
		t.Fatalf("recovery tick N error = %v", err)
	}
	identity := PrewarmCacheIdentity{ProviderID: 7, ProviderVersion: 11, Timezone: "UTC", AnchorDate: "2026-07-21"}
	manifestKey, _ := prewarmManifestKeyForIdentity("test", prewarmCacheSchemaVersion, identity)
	server.Del(manifestKey)
	if err := right.runRecovery(context.Background()); err != nil {
		t.Fatalf("recovery tick N+1 replay error = %v", err)
	}
	if got := provider.directoryCount(); got != 1 {
		t.Fatalf("recovery tick N+1 replay raised directory count to %d, want one", got)
	}
	rightNow = base.Add(2 * time.Minute)
	cacheClock.Store(rightNow)
	if err := right.runRecovery(context.Background()); err != nil {
		t.Fatalf("recovery tick N+2 error = %v", err)
	}
	if got := provider.directoryCount(); got != 2 {
		t.Fatalf("recovery tick N+2 directory count = %d, want two", got)
	}
}

func TestPrewarmLeaseAtomicPublicationRejectsCoordinatorLostAtCommit(t *testing.T) {
	now := time.Date(2026, 7, 21, 8, 0, 0, 0, time.UTC)
	store := &coordinatorDropMultiLeaseStore{recordingPrewarmStore: newRecordingPrewarmStore()}
	cache := mustNewPrewarmCache(t, store, func() time.Time { return now })
	identity := PrewarmCacheIdentity{ProviderID: 7, ProviderVersion: 11, Timezone: "UTC", AnchorDate: "2026-07-21"}
	old := seedPrewarmManifest(t, cache, identity, now, "a")
	store.dropCoordinator = true
	provider := newLifecycleProvider([]int64{101})
	prewarmer := mustPrewarmer(t, staticBindingResolver{binding: prewarmBinding(provider)}, cache, lifecycleOptions([]string{"UTC"}, func() time.Time { return now }))
	if err := prewarmer.RunMoving(context.Background()); err == nil {
		t.Fatal("RunMoving(commit-window coordinator loss) error = nil")
	}
	result := readPrewarmResult(t, cache, identity)
	if result.Manifest.TodayHour.GenerationID != old.TodayHour.GenerationID {
		t.Fatal("coordinator lost at atomic commit still published manifest")
	}
	if got := store.maximumClaims(); got < 3 {
		t.Fatalf("moving atomic publication checked %d leases, want tick+active+segment", got)
	}
}

func TestPrewarmRecoveryAtomicPublicationRejectsCoordinatorLostAtCommit(t *testing.T) {
	now := time.Date(2026, 7, 21, 8, 0, 0, 0, time.UTC)
	store := &coordinatorDropMultiLeaseStore{recordingPrewarmStore: newRecordingPrewarmStore(), dropCoordinator: true}
	cache := mustNewPrewarmCache(t, store, func() time.Time { return now })
	provider := newLifecycleProvider([]int64{101})
	prewarmer := mustPrewarmer(t, staticBindingResolver{binding: prewarmBinding(provider)}, cache, lifecycleOptions([]string{"UTC"}, func() time.Time { return now }))
	if err := prewarmer.runRecovery(context.Background()); err == nil {
		t.Fatal("runRecovery(commit-window coordinator loss) error = nil")
	}
	identity := PrewarmCacheIdentity{ProviderID: 7, ProviderVersion: 11, Timezone: "UTC", AnchorDate: "2026-07-21"}
	if _, found, err := cache.Read(context.Background(), identity); err != nil || found {
		t.Fatalf("recovery manifest after coordinator loss found=%v err=%v, want absent", found, err)
	}
	if got := store.maximumClaims(); got < 3 {
		t.Fatalf("recovery atomic publication checked %d leases, want tick+active+segment", got)
	}
}

func TestPrewarmRecoveryAtomicPublicationRejectsHistoryLeaseLostAtCommit(t *testing.T) {
	now := time.Date(2026, 7, 21, 8, 0, 0, 0, time.UTC)
	store := &coordinatorDropMultiLeaseStore{recordingPrewarmStore: newRecordingPrewarmStore()}
	cache := mustNewPrewarmCache(t, store, func() time.Time { return now })
	provider := newLifecycleProvider([]int64{101})
	binding := prewarmBinding(provider)
	store.replaceLeaseKey = prewarmSegmentLeaseKey(
		cache, binding, "UTC", "2026-07-21", string(SegmentHistory29d),
	)
	prewarmer := mustPrewarmer(t, staticBindingResolver{binding: binding}, cache, lifecycleOptions([]string{"UTC", "Asia/Shanghai"}, func() time.Time { return now }))

	if err := prewarmer.runRecovery(context.Background()); err == nil {
		t.Fatal("runRecovery(commit-window history lease loss) error = nil")
	}
	identity := PrewarmCacheIdentity{ProviderID: 7, ProviderVersion: 11, Timezone: "UTC", AnchorDate: "2026-07-21"}
	if _, found, err := cache.Read(context.Background(), identity); err != nil || found {
		t.Fatalf("recovery manifest after history lease loss found=%v err=%v, want absent", found, err)
	}
	healthyIdentity := PrewarmCacheIdentity{ProviderID: 7, ProviderVersion: 11, Timezone: "Asia/Shanghai", AnchorDate: "2026-07-21"}
	if result := readPrewarmResult(t, cache, healthyIdentity); !result.Complete {
		t.Fatal("healthy recovery lane did not publish after another lane lost a history lease")
	}
	if got := store.maximumClaims(); got < 5 {
		t.Fatalf("recovery atomic publication checked %d leases, want tick+active+three segments", got)
	}
}

func TestPrewarmHistoricalUsesIndependentRetainedClassCoordinators(t *testing.T) {
	now := time.Date(2026, 7, 21, 8, 0, 0, 0, time.UTC)
	cache, server := newRedisPrewarmCache(t, func() time.Time { return now })
	identity := PrewarmCacheIdentity{ProviderID: 7, ProviderVersion: 11, Timezone: "UTC", AnchorDate: "2026-07-21"}
	seedMixedAgePrewarmManifest(t, cache, identity, now)
	provider := newLifecycleProvider([]int64{101})
	provider.setTrendErrorClass(SegmentHistory29d)
	options := lifecycleOptions([]string{"UTC"}, func() time.Time { return now })
	left := mustPrewarmer(t, staticBindingResolver{binding: prewarmBinding(provider)}, cache, options)
	right := mustPrewarmer(t, staticBindingResolver{binding: prewarmBinding(provider)}, cache, options)
	if err := left.RunHistorical(context.Background(), "UTC", now); err == nil {
		t.Fatal("left RunHistorical() error = nil, want history_29d failure")
	}
	if provider.classCount(SegmentHistory29d) != 1 || provider.classCount(SegmentHistory6d) != 1 {
		t.Fatalf("first class calls history29=%d history6=%d, want 1/1", provider.classCount(SegmentHistory29d), provider.classCount(SegmentHistory6d))
	}
	if got := countLeasesWithTTL(server, 5*time.Minute, 7*time.Minute); got != 1 {
		t.Fatalf("retained class coordinators after partial success = %d, want one", got)
	}
	provider.setTrendErrorClass("")
	if err := right.RunHistorical(context.Background(), "UTC", now); err != nil {
		t.Fatalf("right RunHistorical() error = %v", err)
	}
	if provider.classCount(SegmentHistory29d) != 2 || provider.classCount(SegmentHistory6d) != 1 {
		t.Fatalf("retry class calls history29=%d history6=%d, want 2/1", provider.classCount(SegmentHistory29d), provider.classCount(SegmentHistory6d))
	}
	if got := countLeasesWithTTL(server, 5*time.Minute, 7*time.Minute); got != 2 {
		t.Fatalf("retained class coordinators after both successes = %d, want two", got)
	}
}

func TestPrewarmHistoricalCoordinatorKeyIncludesAllowlistIdentity(t *testing.T) {
	now := time.Date(2026, 7, 21, 8, 0, 0, 0, time.UTC)
	identity := PrewarmCacheIdentity{ProviderID: 7, ProviderVersion: 11, Timezone: "UTC", AnchorDate: "2026-07-21"}
	allowlists := [][]string{{"UTC"}, {"UTC", "Asia/Shanghai"}}
	coordinatorKeys := make([]string, 0, len(allowlists))

	for _, allowlist := range allowlists {
		cache, server := newRedisPrewarmCache(t, func() time.Time { return now })
		seedPrewarmManifest(t, cache, identity, now, "a")
		provider := newLifecycleProvider([]int64{101})
		prewarmer := mustPrewarmer(t, staticBindingResolver{binding: prewarmBinding(provider)}, cache, lifecycleOptions(allowlist, func() time.Time { return now }))
		if err := prewarmer.RunHistorical(context.Background(), "UTC", now); err != nil {
			t.Fatalf("RunHistorical(%v) error = %v", allowlist, err)
		}
		coordinatorKey := cache.LeaseKey(
			"historical-coordinator", "7", "11", prewarmer.allowlistDigest,
			prewarmTimezoneDigest("UTC"), "2026-07-21", string(SegmentHistory29d),
		)
		if !server.Exists(coordinatorKey) {
			t.Fatalf("historical coordinator for allowlist %v did not use allowlist-scoped key", allowlist)
		}
		coordinatorKeys = append(coordinatorKeys, coordinatorKey)
	}

	if coordinatorKeys[0] == coordinatorKeys[1] {
		t.Fatal("historical coordinator key did not change with the configured timezone allowlist")
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

func TestPrewarmConcurrencyCancellationReleasesOwnedSlotImmediately(t *testing.T) {
	cache, server := newRedisPrewarmCache(t, time.Now)
	limiter := mustPrewarmer(t, staticBindingResolver{}, cache, lifecycleOptions([]string{"UTC"}, time.Now)).SourceCallLimiter()
	ctx, cancel := context.WithCancel(context.Background())
	entered := make(chan struct{})
	resultCh := make(chan error, 1)
	go func() {
		resultCh <- limiter.Do(ctx, func(callCtx context.Context) error {
			close(entered)
			<-callCtx.Done()
			return callCtx.Err()
		})
	}()
	<-entered
	cancel()
	if err := <-resultCh; !errors.Is(err, context.Canceled) {
		t.Fatalf("SourceCallLimiter.Do() error = %v, want context canceled", err)
	}
	if got := countLeasesWithTTL(server, 80*time.Second, 91*time.Second); got != 0 {
		t.Fatalf("owned source slots after cancellation = %d, want zero", got)
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

func TestPrewarmerRecordsSchedulerTickBeforeWorkers(t *testing.T) {
	ticks := make(chan time.Time, 1)
	tickRecorded := make(chan struct{}, 1)
	metrics := &recordingPrewarmRequestMetrics{schedulerTickRecorded: tickRecorded}
	metrics.schedulerTickHook = func() {
		for _, cycle := range metrics.cyclesCopy() {
			switch PrewarmCycleClass(cycle.class) {
			case PrewarmCycleMoving, PrewarmCycleRecovery, PrewarmCycleHistory29d, PrewarmCycleHistory6d:
				t.Error("scheduled worker recorded before scheduler tick")
			}
		}
	}
	options := lifecycleOptions([]string{"UTC"}, time.Now)
	options.tick, options.Metrics = ticks, metrics
	prewarmer := mustPrewarmer(
		t,
		staticBindingResolver{binding: prewarmBinding(newLifecycleProvider([]int64{101}))},
		mustNewPrewarmCache(t, newRecordingPrewarmStore(), time.Now),
		options,
	)

	prewarmer.Start(context.Background())
	select {
	case <-prewarmer.startupDone:
	case <-time.After(2 * time.Second):
		t.Fatal("startup did not finish")
	}
	ticks <- time.Now()
	select {
	case <-tickRecorded:
	case <-time.After(2 * time.Second):
		t.Fatal("scheduler tick was not recorded")
	}
	prewarmer.Stop()
	metrics.mu.Lock()
	recordedTicks := metrics.schedulerTicks
	metrics.mu.Unlock()
	if recordedTicks != 1 {
		t.Fatalf("scheduler ticks = %d, want 1", recordedTicks)
	}
}

func TestPrewarmMovingStartDuringStopCannotJoinLifecycle(t *testing.T) {
	now := time.Date(2026, 7, 21, 8, 0, 0, 0, time.UTC)
	cache, _ := newRedisPrewarmCache(t, func() time.Time { return now })
	seedPrewarmManifest(t, cache, PrewarmCacheIdentity{ProviderID: 7, ProviderVersion: 11, Timezone: "UTC", AnchorDate: "2026-07-21"}, now, "a")
	provider := newLifecycleProvider([]int64{101})
	provider.directoryEntered = make(chan struct{}, 3)
	provider.directoryRelease = make(chan struct{})
	provider.directoryContextDone = make(chan struct{}, 1)
	provider.ignoreDirectoryCancel = true
	ticks := make(chan time.Time, 3)
	options := lifecycleOptions([]string{"UTC"}, func() time.Time { return now })
	options.tick = ticks
	prewarmer := mustPrewarmer(t, staticBindingResolver{binding: prewarmBinding(provider)}, cache, options)
	firstCtx, firstCancel := context.WithCancel(context.Background())
	defer firstCancel()
	prewarmer.Start(firstCtx)
	ticks <- now
	select {
	case <-provider.directoryEntered:
	case <-time.After(time.Second):
		t.Fatal("first lifecycle did not enter moving source")
	}
	stopDone := make(chan struct{})
	go func() {
		prewarmer.Stop()
		close(stopDone)
	}()
	select {
	case <-provider.directoryContextDone:
	case <-time.After(time.Second):
		t.Fatal("Stop() did not cancel the active lifecycle")
	}
	duringCtx, cancelDuring := context.WithCancel(context.Background())
	prewarmer.Start(duringCtx)
	cancelDuring()
	close(provider.directoryRelease)
	select {
	case <-stopDone:
	case <-time.After(time.Second):
		t.Fatal("Stop() did not return after active worker exited")
	}

	now = now.Add(time.Minute)
	postCtx, cancelPost := context.WithCancel(context.Background())
	defer cancelPost()
	prewarmer.Start(postCtx)
	ticks <- now
	select {
	case <-provider.directoryEntered:
	case <-time.After(300 * time.Millisecond):
		t.Fatal("Start() after completed Stop did not start a new lifecycle")
	}
	prewarmer.Stop()
}

func TestPrewarmerLifecycleReportsBoundedStartupProviderFailure(t *testing.T) {
	cache := mustNewPrewarmCache(t, newRecordingPrewarmStore(), time.Now)
	metrics := &recordingPrewarmRequestMetrics{}
	reporter := &recordingPrewarmReporter{events: make(chan PrewarmBackgroundEvent, 1)}
	options := lifecycleOptions([]string{"UTC"}, time.Now)
	options.Metrics = metrics
	options.Reporter = reporter
	options.NewOperationID = func() string { return strings.Repeat("a", 32) }
	prewarmer := mustPrewarmer(t, staticBindingResolver{err: errors.New("dynamic provider detail")}, cache, options)

	prewarmer.Start(context.Background())
	defer prewarmer.Stop()
	select {
	case event := <-reporter.events:
		if event.OperationID != strings.Repeat("a", 32) || event.Class != PrewarmCycleStartup ||
			event.Timezone != "UTC" || event.Outcome != PrewarmCycleError || event.ProviderID != 0 || event.ProviderVersion != 0 {
			t.Fatalf("startup failure event = %#v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("startup provider failure was discarded")
	}
	if !containsPrewarmCycleMetric(metrics.cycles, prewarmCycleMetric{
		class: string(PrewarmCycleStartup), timezone: "UTC", outcome: string(PrewarmCycleError),
	}) {
		t.Fatalf("startup failure cycle metrics = %#v", metrics.cycles)
	}
}

func TestPrewarmerStartupCollapsesConcurrentAndStaggeredInstances(t *testing.T) {
	now := time.Date(2026, 7, 21, 8, 0, 0, 0, time.UTC)
	server := miniredis.RunT(t)
	timezones := []string{"UTC", "Asia/Shanghai", "America/Los_Angeles", "Europe/Berlin"}

	newInstance := func(token string, provider *lifecycleProvider, metrics *recordingPrewarmRequestMetrics) *Prewarmer {
		client := redis.NewClient(&redis.Options{Addr: server.Addr()})
		t.Cleanup(func() { _ = client.Close() })
		cache, err := NewPrewarmCache(readcache.NewRedisStore(client), PrewarmCacheOptions{
			Namespace: "test", Now: func() time.Time { return now }, Metrics: metrics,
		})
		if err != nil {
			t.Fatalf("NewPrewarmCache() error = %v", err)
		}
		options := lifecycleOptions(timezones, func() time.Time { return now })
		options.Metrics = metrics
		options.NewToken = func() string { return strings.Repeat(token, 64) }
		return mustPrewarmer(t, staticBindingResolver{binding: prewarmBinding(provider)}, cache, options)
	}
	waitForStartup := func(name string, prewarmer *Prewarmer) {
		t.Helper()
		select {
		case <-prewarmer.startupDone:
		case <-time.After(2 * time.Second):
			t.Fatalf("%s startup did not finish", name)
		}
	}

	ownerProvider := newLifecycleProvider([]int64{101})
	ownerProvider.directoryEntered = make(chan struct{}, 1)
	ownerProvider.directoryRelease = make(chan struct{})
	ownerMetrics := &recordingPrewarmRequestMetrics{}
	owner := newInstance("a", ownerProvider, ownerMetrics)

	concurrentProvider := newLifecycleProvider([]int64{101})
	concurrentMetrics := &recordingPrewarmRequestMetrics{}
	concurrent := newInstance("b", concurrentProvider, concurrentMetrics)

	staggeredProvider := newLifecycleProvider([]int64{101})
	staggeredMetrics := &recordingPrewarmRequestMetrics{}
	staggered := newInstance("c", staggeredProvider, staggeredMetrics)

	owner.Start(context.Background())
	select {
	case <-ownerProvider.directoryEntered:
	case <-time.After(time.Second):
		t.Fatal("startup owner did not enter source work")
	}
	concurrent.Start(context.Background())
	waitForStartup("concurrent loser", concurrent)

	close(ownerProvider.directoryRelease)
	waitForStartup("owner", owner)
	staggered.Start(context.Background())
	waitForStartup("staggered loser", staggered)

	owner.Stop()
	concurrent.Stop()
	staggered.Stop()

	if got := concurrentProvider.totalSourceCalls(); got != 0 {
		t.Fatalf("concurrent startup loser source calls = %d, want zero", got)
	}
	if got := staggeredProvider.totalSourceCalls(); got != 0 {
		t.Fatalf("staggered startup loser source calls = %d, want zero", got)
	}
	for name, metrics := range map[string]*recordingPrewarmRequestMetrics{
		"concurrent": concurrentMetrics,
		"staggered":  staggeredMetrics,
	} {
		if got := countPrewarmCycleOutcome(metrics.cycles, PrewarmCycleStartup, "UTC", PrewarmCycleLeaseBusy); got != 1 {
			t.Fatalf("%s startup lease_busy metrics = %d, want one per timezone; all=%#v", name, got, metrics.cycles)
		}
		if got := countPrewarmCycleOutcome(metrics.cycles, PrewarmCycleStartup, "UTC", PrewarmCycleSuccess); got != 0 {
			t.Fatalf("%s startup success metrics = %d, want zero; all=%#v", name, got, metrics.cycles)
		}
	}
	if got := countPrewarmCycleOutcome(ownerMetrics.cycles, PrewarmCycleStartup, "UTC", PrewarmCycleSuccess); got != 1 {
		t.Fatalf("startup owner success metrics = %d, want one per timezone; all=%#v", got, ownerMetrics.cycles)
	}
}

func TestPrewarmerLifecycleReportsBoundedCoordinatorFailure(t *testing.T) {
	store := newRecordingPrewarmStore()
	store.leaseErr = errors.New("dynamic Redis detail")
	cache := mustNewPrewarmCache(t, store, time.Now)
	metrics := &recordingPrewarmRequestMetrics{}
	reporter := &recordingPrewarmReporter{events: make(chan PrewarmBackgroundEvent, 1)}
	provider := newLifecycleProvider([]int64{101})
	options := lifecycleOptions([]string{"UTC"}, time.Now)
	options.Metrics = metrics
	options.Reporter = reporter
	options.NewOperationID = func() string { return strings.Repeat("b", 32) }
	prewarmer := mustPrewarmer(t, staticBindingResolver{binding: prewarmBinding(provider)}, cache, options)

	prewarmer.Start(context.Background())
	defer prewarmer.Stop()
	select {
	case event := <-reporter.events:
		if event.Class != PrewarmCycleStartup || event.Outcome != PrewarmCycleError || event.OperationID != strings.Repeat("b", 32) {
			t.Fatalf("coordinator failure event = %#v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("startup coordinator failure was discarded")
	}
}

func TestPrewarmerScheduledLifecycleReportsEarlyGlobalFailureOnce(t *testing.T) {
	cache := mustNewPrewarmCache(t, newRecordingPrewarmStore(), time.Now)
	metrics := &recordingPrewarmRequestMetrics{}
	reporter := &recordingPrewarmReporter{events: make(chan PrewarmBackgroundEvent, 2)}
	options := lifecycleOptions([]string{"UTC"}, time.Now)
	options.Metrics, options.Reporter = metrics, reporter
	prewarmer := mustPrewarmer(t, staticBindingResolver{err: errors.New("dynamic provider detail")}, cache, options)

	startedAt := time.Now()
	err := prewarmer.runMoving(context.Background())
	if err == nil {
		t.Fatal("runMoving() error = nil, want provider resolution failure")
	}
	prewarmer.reportScheduledLifecycle(strings.Repeat("g", 32), []prewarmLifecycleTarget{
		{class: PrewarmCycleMoving, timezone: "UTC"},
	}, startedAt, err)
	if got := countPrewarmCycleOutcome(metrics.cycles, PrewarmCycleMoving, "UTC", PrewarmCycleError); got != 1 {
		t.Fatalf("global moving error metrics = %d, want one; all=%#v", got, metrics.cycles)
	}
	if events := drainPrewarmReporter(reporter); len(events) != 1 || events[0].Class != PrewarmCycleMoving || events[0].Timezone != "UTC" {
		t.Fatalf("global moving failure events = %#v", events)
	}
}

func TestPrewarmerScheduledLifecyclePreservesGlobalValidationOutcome(t *testing.T) {
	cache := mustNewPrewarmCache(t, newRecordingPrewarmStore(), time.Now)
	metrics := &recordingPrewarmRequestMetrics{}
	reporter := &recordingPrewarmReporter{events: make(chan PrewarmBackgroundEvent, 1)}
	options := lifecycleOptions([]string{"UTC"}, time.Now)
	options.Metrics, options.Reporter = metrics, reporter
	prewarmer := mustPrewarmer(t, staticBindingResolver{binding: prewarmBinding(newLifecycleProvider([]int64{101}))}, cache, options)
	err := fmt.Errorf("moving cycle: %w", wrapPrewarmSourceFailure(
		prewarmSourceFailureValidation,
		errors.New("dynamic validation detail"),
	))

	prewarmer.reportScheduledLifecycle(strings.Repeat("r", 32), []prewarmLifecycleTarget{
		{class: PrewarmCycleMoving, timezone: "UTC"},
	}, time.Now(), err)

	if got := countPrewarmCycleOutcome(metrics.cycles, PrewarmCycleMoving, "UTC", PrewarmCycleRejected); got != 1 {
		t.Fatalf("global moving rejected metrics = %d, want one; all=%#v", got, metrics.cycles)
	}
	if events := drainPrewarmReporter(reporter); len(events) != 1 || events[0].Outcome != PrewarmCycleRejected {
		t.Fatalf("global moving rejection events = %#v", events)
	}
}

func TestPrewarmerStartupLifecycleUsesValidationOutcome(t *testing.T) {
	cache := mustNewPrewarmCache(t, newRecordingPrewarmStore(), time.Now)
	metrics := &recordingPrewarmRequestMetrics{}
	reporter := &recordingPrewarmReporter{events: make(chan PrewarmBackgroundEvent, 1)}
	options := lifecycleOptions([]string{"UTC"}, time.Now)
	options.Metrics, options.Reporter = metrics, reporter
	prewarmer := mustPrewarmer(t, staticBindingResolver{binding: prewarmBinding(newLifecycleProvider([]int64{101}))}, cache, options)
	err := wrapPrewarmSourceFailure(prewarmSourceFailureValidation, errors.New("dynamic validation detail"))

	prewarmer.reportLifecycle(strings.Repeat("s", 32), PrewarmCycleStartup, []string{"UTC"}, time.Now(), err, true)

	if got := countPrewarmCycleOutcome(metrics.cycles, PrewarmCycleStartup, "UTC", PrewarmCycleRejected); got != 1 {
		t.Fatalf("startup rejected metrics = %d, want one; all=%#v", got, metrics.cycles)
	}
	if events := drainPrewarmReporter(reporter); len(events) != 1 || events[0].Outcome != PrewarmCycleRejected {
		t.Fatalf("startup rejection events = %#v", events)
	}
}

func TestPrewarmerScheduledLifecyclePartitionsExactAndGlobalFailures(t *testing.T) {
	cache := mustNewPrewarmCache(t, newRecordingPrewarmStore(), time.Now)
	metrics := &recordingPrewarmRequestMetrics{}
	reporter := &recordingPrewarmReporter{events: make(chan PrewarmBackgroundEvent, 2)}
	options := lifecycleOptions([]string{"UTC", "Asia/Shanghai"}, time.Now)
	options.Metrics, options.Reporter = metrics, reporter
	prewarmer := mustPrewarmer(t, staticBindingResolver{binding: prewarmBinding(newLifecycleProvider([]int64{101}))}, cache, options)
	err := errors.Join(
		newPrewarmLifecycleFailure(PrewarmCycleMoving, "Asia/Shanghai", false, errors.New("dynamic lane detail")),
		fmt.Errorf("moving cycle: %w", wrapPrewarmSourceFailure(
			prewarmSourceFailureValidation,
			errors.New("dynamic global validation detail"),
		)),
	)

	prewarmer.reportScheduledLifecycle(strings.Repeat("p", 32), []prewarmLifecycleTarget{
		{class: PrewarmCycleMoving, timezone: "UTC"},
		{class: PrewarmCycleMoving, timezone: "Asia/Shanghai"},
	}, time.Now(), err)

	if got := countPrewarmCycleOutcome(metrics.cycles, PrewarmCycleMoving, "UTC", PrewarmCycleRejected); got != 1 {
		t.Fatalf("UTC global rejected metrics = %d, want one; all=%#v", got, metrics.cycles)
	}
	if got := countPrewarmCycleOutcome(metrics.cycles, PrewarmCycleMoving, "Asia/Shanghai", PrewarmCycleError); got != 1 {
		t.Fatalf("Shanghai exact error metrics = %d, want one; all=%#v", got, metrics.cycles)
	}
	if len(metrics.cycles) != 2 {
		t.Fatalf("partitioned lifecycle metrics = %#v, want exactly two", metrics.cycles)
	}
	events := drainPrewarmReporter(reporter)
	if len(events) != 2 || events[0].Timezone != "UTC" || events[0].Outcome != PrewarmCycleRejected ||
		events[1].Timezone != "Asia/Shanghai" || events[1].Outcome != PrewarmCycleError {
		t.Fatalf("partitioned lifecycle events = %#v", events)
	}
}

type recordingPrewarmReporter struct {
	events chan PrewarmBackgroundEvent
}

func (r *recordingPrewarmReporter) ReportPrewarmBackground(event PrewarmBackgroundEvent) {
	r.events <- event
}

func drainPrewarmReporter(reporter *recordingPrewarmReporter) []PrewarmBackgroundEvent {
	var events []PrewarmBackgroundEvent
	for {
		select {
		case event := <-reporter.events:
			events = append(events, event)
		default:
			return events
		}
	}
}

func countPrewarmCycleOutcome(metrics []prewarmCycleMetric, class PrewarmCycleClass, timezone string, outcome PrewarmCycleOutcome) int {
	count := 0
	for _, metric := range metrics {
		if metric.class == string(class) && metric.timezone == timezone && metric.outcome == string(outcome) {
			count++
		}
	}
	return count
}

func containsPrewarmCycleMetric(got []prewarmCycleMetric, want prewarmCycleMetric) bool {
	for _, metric := range got {
		if metric.class == want.class && metric.timezone == want.timezone && metric.outcome == want.outcome {
			return true
		}
	}
	return false
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

	mu                    sync.Mutex
	ids                   []int64
	directoryCalls        int
	directoryErr          error
	afterDirectory        func()
	statsCalls            int
	trendCalls            map[PrewarmSegmentClass]int
	trendErrorTimezone    string
	trendErrorClass       PrewarmSegmentClass
	beforeTrend           func()
	directoryEntered      chan struct{}
	directoryRelease      chan struct{}
	directoryContextDone  chan struct{}
	ignoreDirectoryCancel bool
}

func newLifecycleProvider(ids []int64) *lifecycleProvider {
	return &lifecycleProvider{ids: append([]int64(nil), ids...), trendCalls: make(map[PrewarmSegmentClass]int)}
}

func (p *lifecycleProvider) GetProviderUserIDs(ctx context.Context) (relay.ProviderDirectoryResult, error) {
	p.mu.Lock()
	p.directoryCalls++
	entered, release := p.directoryEntered, p.directoryRelease
	contextDone := p.directoryContextDone
	ignoreCancel := p.ignoreDirectoryCancel
	directoryErr := p.directoryErr
	afterDirectory := p.afterDirectory
	ids := append([]int64(nil), p.ids...)
	p.mu.Unlock()
	if entered != nil {
		entered <- struct{}{}
	}
	if release != nil {
		if ignoreCancel {
			select {
			case <-release:
			case <-ctx.Done():
				if contextDone != nil {
					contextDone <- struct{}{}
				}
				<-release
			}
		} else {
			select {
			case <-release:
			case <-ctx.Done():
				return relay.ProviderDirectoryResult{}, ctx.Err()
			}
		}
	}
	if afterDirectory != nil {
		afterDirectory()
	}
	return relay.ProviderDirectoryResult{UserIDs: ids, PageCount: 1}, directoryErr
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

func (p *lifecycleProvider) setDirectoryError(err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.directoryErr = err
}

func (p *lifecycleProvider) setTrendErrorClass(class PrewarmSegmentClass) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.trendErrorClass = class
}

func countLeasesWithTTL(server *miniredis.Miniredis, minimum, maximum time.Duration) int {
	count := 0
	for _, key := range server.Keys() {
		if !strings.Contains(key, ":lease:") {
			continue
		}
		ttl := server.TTL(key)
		if ttl > minimum && ttl < maximum {
			count++
		}
	}
	return count
}

func deleteLeasesWithTTL(server *miniredis.Miniredis, minimum, maximum time.Duration) {
	for _, key := range server.Keys() {
		if !strings.Contains(key, ":lease:") {
			continue
		}
		ttl := server.TTL(key)
		if ttl > minimum && ttl < maximum {
			server.Del(key)
		}
	}
}

func waitForPrewarmCondition(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("timed out waiting for prewarm condition")
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

func (s *ttlRecordingPrewarmStore) Get(ctx context.Context, key string) ([]byte, error) {
	return getRecordingPrewarmValueOrLease(ctx, s.recordingPrewarmStore, key)
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

func (s *lostSegmentLeasePrewarmStore) Get(ctx context.Context, key string) ([]byte, error) {
	return getRecordingPrewarmValueOrLease(ctx, s.recordingPrewarmStore, key)
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
	if err != nil || !published || leaseKey != key || s.loseClass == "" {
		return published, err
	}
	var segment PrewarmTrendSegment
	if decodeErr := decodePrewarmStoredJSON(value, prewarmSegmentMaxBytes, &segment); decodeErr != nil || segment.Class != s.loseClass {
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

type leaseVisiblePrewarmStore struct {
	*recordingPrewarmStore
}

func (s *leaseVisiblePrewarmStore) Get(ctx context.Context, key string) ([]byte, error) {
	return getRecordingPrewarmValueOrLease(ctx, s.recordingPrewarmStore, key)
}

func getRecordingPrewarmValueOrLease(ctx context.Context, store *recordingPrewarmStore, key string) ([]byte, error) {
	value, err := store.Get(ctx, key)
	if !errors.Is(err, readcache.ErrMiss) {
		return value, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	token, ok := store.leases[key]
	if !ok {
		return nil, readcache.ErrMiss
	}
	return []byte(token), nil
}

var _ readcache.BatchStore = (*leaseVisiblePrewarmStore)(nil)

type laneMGetErrorPrewarmStore struct {
	readcache.BatchStore
	failKey string
	err     error
}

func (s *laneMGetErrorPrewarmStore) MGet(ctx context.Context, keys ...string) ([][]byte, error) {
	for _, key := range keys {
		if key == s.failKey {
			return nil, s.err
		}
	}
	return s.BatchStore.MGet(ctx, keys...)
}

var _ readcache.BatchStore = (*laneMGetErrorPrewarmStore)(nil)

type coordinatorDropMultiLeaseStore struct {
	*recordingPrewarmStore

	muDrop          sync.Mutex
	dropCoordinator bool
	replaceLeaseKey string
	maxClaims       int
}

func (s *coordinatorDropMultiLeaseStore) Get(ctx context.Context, key string) ([]byte, error) {
	return getRecordingPrewarmValueOrLease(ctx, s.recordingPrewarmStore, key)
}

func (s *coordinatorDropMultiLeaseStore) SetIfLeasesOwned(
	ctx context.Context,
	leaseKeys, tokens []string,
	key string,
	value []byte,
	ttl time.Duration,
) (bool, error) {
	s.muDrop.Lock()
	if len(leaseKeys) > s.maxClaims {
		s.maxClaims = len(leaseKeys)
	}
	drop := s.dropCoordinator
	replaceLeaseKey := s.replaceLeaseKey
	s.muDrop.Unlock()
	if drop && len(leaseKeys) > 0 {
		s.recordingPrewarmStore.mu.Lock()
		delete(s.recordingPrewarmStore.leases, leaseKeys[0])
		s.recordingPrewarmStore.mu.Unlock()
	}
	if replaceLeaseKey != "" {
		s.recordingPrewarmStore.mu.Lock()
		s.recordingPrewarmStore.leases[replaceLeaseKey] = "competitor-owner"
		s.recordingPrewarmStore.mu.Unlock()
	}
	return s.recordingPrewarmStore.SetIfLeasesOwned(ctx, leaseKeys, tokens, key, value, ttl)
}

func (s *coordinatorDropMultiLeaseStore) maximumClaims() int {
	s.muDrop.Lock()
	defer s.muDrop.Unlock()
	return s.maxClaims
}

var _ readcache.BatchStore = (*coordinatorDropMultiLeaseStore)(nil)
var _ relay.ProviderWideTeamUsageProvider = (*lifecycleProvider)(nil)
var _ relay.ProviderWideTeamTrendProvider = (*lifecycleProvider)(nil)
var _ = errors.Is
