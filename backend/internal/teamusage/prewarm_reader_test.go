package teamusage

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/ai-efficiency/backend/internal/representativescope"
	"github.com/ai-efficiency/backend/internal/testdb"
)

func TestPrewarmReaderFullHitFiltersAuthorizedRosterAndUsesSparseZero(t *testing.T) {
	now := time.Date(2026, 7, 21, 8, 0, 0, 0, time.UTC)
	cache, _ := newRedisPrewarmCache(t, func() time.Time { return now })
	identity := testPrewarmIdentity()
	seedAuthorizedPrewarmManifest(t, cache, identity, now, []int64{101, 102})
	limiter := &readerSourceLimiter{}
	metrics := &recordingPrewarmRequestMetrics{}
	reader := mustPrewarmReader(t, cache, limiter, PrewarmReaderOptions{
		Timezones: []string{"UTC"}, Now: func() time.Time { return now }, Metrics: metrics,
	})
	provider := &prewarmReaderProvider{fakeRelayProvider: &fakeRelayProvider{}}

	origin, outcome, err := reader.ReadAuthorizedOrigin(context.Background(), PrewarmReadRequest{
		ProviderID: 7, ActorUserID: 1, ProviderVersion: 11, ScopeVersion: "scope-v1",
		Params: prewarmReader7dParams(), AuthorizedRelayUserIDs: []int64{102, 101, 102}, Provider: provider,
	})
	if err != nil || outcome != PrewarmReadFullHit {
		t.Fatalf("ReadAuthorizedOrigin() outcome/error = %q/%v, want full hit", outcome, err)
	}
	if limiter.calls.Load() != 0 || provider.trendCalls.Load() != 0 {
		t.Fatalf("full hit source calls = limiter %d/provider %d, want zero", limiter.calls.Load(), provider.trendCalls.Load())
	}
	if !reflect.DeepEqual(origin.RelayUserIDs, []int64{101, 102}) {
		t.Fatalf("authorized Relay IDs = %#v, want sorted unique IDs", origin.RelayUserIDs)
	}
	if points, ok := origin.PointsByUser[102]; !ok || len(points) != 0 {
		t.Fatalf("sparse authorized points = %#v/%v, want dense empty row", points, ok)
	}
	if stat := origin.StatsByRelayUserID[102]; stat.RangeActualCost == nil || *stat.RangeActualCost != 0 ||
		stat.RangeTotalTokens == nil || *stat.RangeTotalTokens != 0 {
		t.Fatalf("sparse authorized range = %#v/%#v, want complete zero", stat.RangeActualCost, stat.RangeTotalTokens)
	}
	if !reflect.DeepEqual(metrics.quantities, []prewarmQuantityMetric{{kind: PrewarmQuantityUnionUsers, timezone: "UTC", value: 1}}) {
		t.Fatalf("composition quantities = %#v", metrics.quantities)
	}
}

func TestPrewarmReaderWindowSelectionUsesExactCacheReferences(t *testing.T) {
	now := time.Date(2026, 7, 21, 8, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		params OverviewParams
		want   func(PrewarmManifest) []string
	}{
		{
			name:   "today",
			params: OverviewParams{StartDate: "2026-07-21", EndDate: "2026-07-21", Granularity: "hour", Timezone: "UTC"},
			want: func(manifest PrewarmManifest) []string {
				return []string{manifest.CurrentStats.Key, manifest.TodayHour.Key}
			},
		},
		{
			name:   "7d",
			params: prewarmReader7dParams(),
			want: func(manifest PrewarmManifest) []string {
				return []string{manifest.CurrentStats.Key, manifest.History6d.Key, manifest.TodayHour.Key}
			},
		},
		{
			name:   "30d",
			params: OverviewParams{StartDate: "2026-06-22", EndDate: "2026-07-21", Granularity: "day", Timezone: "UTC"},
			want: func(manifest PrewarmManifest) []string {
				return []string{manifest.CurrentStats.Key, manifest.History29d.Key, manifest.TodayHour.Key}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := newRecordingPrewarmStore()
			cache := mustNewPrewarmCache(t, store, func() time.Time { return now })
			manifest := seedAuthorizedPrewarmManifest(t, cache, testPrewarmIdentity(), now, []int64{101})
			reader := mustPrewarmReader(t, cache, &readerSourceLimiter{}, PrewarmReaderOptions{
				Timezones: []string{"UTC"}, Now: func() time.Time { return now },
			})
			provider := &prewarmReaderProvider{fakeRelayProvider: &fakeRelayProvider{}}

			origin, outcome, err := reader.ReadAuthorizedOrigin(context.Background(), PrewarmReadRequest{
				ProviderID: 7, ActorUserID: 1, ProviderVersion: 11, ScopeVersion: "scope-v1",
				Params: test.params, AuthorizedRelayUserIDs: []int64{101}, Provider: provider,
			})
			if err != nil || outcome != PrewarmReadFullHit || origin == nil {
				t.Fatalf("ReadAuthorizedOrigin(%s) = %#v/%q/%v, want full hit", test.name, origin, outcome, err)
			}
			if got := store.LastMGet(); !reflect.DeepEqual(got, test.want(manifest)) {
				t.Fatalf("ReadAuthorizedOrigin(%s) MGET keys = %#v, want %#v", test.name, got, test.want(manifest))
			}
		})
	}
}

func TestPrewarmReaderWindowSelectionRecoveryUsesOnlySelectedStaticPrerequisites(t *testing.T) {
	current := &PrewarmCurrentStatsEnvelope{}
	history29d := &PrewarmTrendSegment{}
	history6d := &PrewarmTrendSegment{}
	tests := []struct {
		name   string
		class  PrewarmWindowClass
		result *PrewarmCacheResult
	}{
		{
			name:  "today",
			class: PrewarmWindowToday,
			result: &PrewarmCacheResult{
				CurrentStats: current, CurrentStatsStatus: PrewarmValueStale,
				TodayHourStatus: PrewarmValueMissing,
			},
		},
		{
			name:  "7d",
			class: PrewarmWindow7d,
			result: &PrewarmCacheResult{
				CurrentStats: current, CurrentStatsStatus: PrewarmValueStale,
				Segments: PrewarmSegmentSet{History6d: history6d}, History6dStatus: PrewarmValueStale,
				TodayHourStatus: PrewarmValueInvalid,
			},
		},
		{
			name:  "30d",
			class: PrewarmWindow30d,
			result: &PrewarmCacheResult{
				CurrentStats: current, CurrentStatsStatus: PrewarmValueStale,
				Segments: PrewarmSegmentSet{History29d: history29d}, History29dStatus: PrewarmValueStale,
				TodayHourStatus: PrewarmValueHardExpired,
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if !prewarmResultCanRecoverToday(test.result, test.class) {
				t.Fatalf("prewarmResultCanRecoverToday(%s) = false, want selected prerequisites only", test.class)
			}
			withoutCurrent := *test.result
			withoutCurrent.CurrentStats = nil
			if prewarmResultCanRecoverToday(&withoutCurrent, test.class) {
				t.Fatalf("prewarmResultCanRecoverToday(%s, missing current) = true", test.class)
			}
			withoutSelectedHistory := *test.result
			switch test.class {
			case PrewarmWindow7d:
				withoutSelectedHistory.Segments.History6d = nil
			case PrewarmWindow30d:
				withoutSelectedHistory.Segments.History29d = nil
			default:
				return
			}
			if prewarmResultCanRecoverToday(&withoutSelectedHistory, test.class) {
				t.Fatalf("prewarmResultCanRecoverToday(%s, missing selected history) = true", test.class)
			}
		})
	}
}

func TestPrewarmReaderTamperedRosterDigestSelectsExactFallback(t *testing.T) {
	now := time.Date(2026, 7, 21, 8, 0, 0, 0, time.UTC)
	cache, server := newRedisPrewarmCache(t, func() time.Time { return now })
	identity := testPrewarmIdentity()
	manifest := seedAuthorizedPrewarmManifest(t, cache, identity, now, []int64{101, 102})
	result := readPrewarmResult(t, cache, identity)
	tampered := *result.CurrentStats
	tampered.RosterDigest = strings.Repeat("f", 64)
	encodedCurrent, err := encodePrewarmStoredJSON(tampered, prewarmCurrentStatsMaxBytes, prewarmCurrentStatsMaxBytes)
	if err != nil {
		t.Fatalf("encodePrewarmStoredJSON(tampered current) error = %v", err)
	}
	server.Set(manifest.CurrentStats.Key, string(encodedCurrent))
	server.SetTTL(manifest.CurrentStats.Key, movingValueTTL)
	manifest.CurrentStats.RosterDigest = tampered.RosterDigest
	manifest.CurrentStats.SerializedBytes = len(encodedCurrent)
	encodedManifest, err := encodePrewarmJSON(manifest, prewarmManifestMaxBytes)
	if err != nil {
		t.Fatalf("encodePrewarmJSON(tampered manifest) error = %v", err)
	}
	manifestKey, err := prewarmManifestKeyForIdentity("test", prewarmCacheSchemaVersion, identity)
	if err != nil {
		t.Fatalf("prewarmManifestKeyForIdentity() error = %v", err)
	}
	server.Set(manifestKey, string(encodedManifest))
	server.SetTTL(manifestKey, manifestTTL)

	metrics := &recordingPrewarmRequestMetrics{}
	reader := mustPrewarmReader(t, cache, &readerSourceLimiter{}, PrewarmReaderOptions{
		Timezones: []string{"UTC"}, Now: func() time.Time { return now }, Metrics: metrics,
	})
	provider := &prewarmReaderProvider{fakeRelayProvider: &fakeRelayProvider{}}
	origin, outcome, err := reader.ReadAuthorizedOrigin(context.Background(), PrewarmReadRequest{
		ProviderID: 7, ActorUserID: 1, ProviderVersion: 11, ScopeVersion: "scope-v1",
		Params: prewarmReader7dParams(), AuthorizedRelayUserIDs: []int64{101}, Provider: provider,
	})
	if err == nil || origin != nil || outcome != PrewarmReadFallback {
		t.Fatalf("tampered roster digest = %#v/%q/%v, want exact fallback signal", origin, outcome, err)
	}
	if !strings.Contains(err.Error(), "prewarm current stats roster digest does not match stats") {
		t.Fatalf("tampered roster digest error = %v, want roster validator", err)
	}
}

func TestPrewarmReaderUnionMetricUsesProviderWideTrendBeforeAuthorization(t *testing.T) {
	now := time.Date(2026, 7, 21, 8, 0, 0, 0, time.UTC)
	cache, _ := newRedisPrewarmCache(t, func() time.Time { return now })
	identity := testPrewarmIdentity()
	manifest := seedAuthorizedPrewarmManifest(t, cache, identity, now, []int64{101, 102, 103})
	coverage, err := prewarmSegmentCoverage(SegmentHistory6d, identity.AnchorDate, identity.Timezone)
	if err != nil {
		t.Fatalf("prewarmSegmentCoverage() error = %v", err)
	}
	points := []relay.ProviderWideTrendPoint{
		{UserID: 101, Date: coverage.StartDate, ActualCost: 1},
		{UserID: 102, Date: coverage.StartDate, ActualCost: 1},
		{UserID: 103, Date: coverage.StartDate, ActualCost: 1},
	}
	manifest.History6d, err = cache.WriteSegment(context.Background(), PrewarmTrendSegment{
		SchemaVersion: prewarmCacheSchemaVersion, ProviderID: identity.ProviderID, ProviderVersion: identity.ProviderVersion,
		TimezoneDigest: prewarmTimezoneDigest(identity.Timezone), GenerationID: strings.Repeat("z", 64), GeneratedAt: now,
		Timezone: identity.Timezone, AnchorDate: identity.AnchorDate, Class: SegmentHistory6d, Coverage: coverage,
		Points: points, ResponseBytes: 96, PointCount: len(points), UniqueUserCount: len(points), Complete: true,
	})
	if err != nil {
		t.Fatalf("WriteSegment() error = %v", err)
	}
	leaseKey := cache.LeaseKey("union-metric", manifest.History6d.GenerationID)
	token := strings.Repeat("u", 64)
	acquired, err := cache.TryAcquireLease(context.Background(), leaseKey, token, time.Minute)
	if err != nil || !acquired {
		t.Fatalf("TryAcquireLease() = %v, %v", acquired, err)
	}
	published, err := cache.PublishManifest(context.Background(), leaseKey, token, manifest)
	if err != nil || !published {
		t.Fatalf("PublishManifest() = %v, %v", published, err)
	}

	metrics := &recordingPrewarmRequestMetrics{}
	reader := mustPrewarmReader(t, cache, &readerSourceLimiter{}, PrewarmReaderOptions{
		Timezones: []string{"UTC"}, Now: func() time.Time { return now }, Metrics: metrics,
	})
	provider := &prewarmReaderProvider{fakeRelayProvider: &fakeRelayProvider{}}
	for _, authorized := range [][]int64{{101}, {102}} {
		origin, outcome, readErr := reader.ReadAuthorizedOrigin(context.Background(), PrewarmReadRequest{
			ProviderID: 7, ActorUserID: 1, ProviderVersion: 11, ScopeVersion: "scope-v1",
			Params: prewarmReader7dParams(), AuthorizedRelayUserIDs: authorized, Provider: provider,
		})
		if readErr != nil || outcome != PrewarmReadFullHit || !reflect.DeepEqual(origin.RelayUserIDs, authorized) {
			t.Fatalf("ReadAuthorizedOrigin(%v) = %#v/%q/%v", authorized, origin, outcome, readErr)
		}
	}
	want := []prewarmQuantityMetric{
		{kind: PrewarmQuantityUnionUsers, timezone: "UTC", value: 3},
		{kind: PrewarmQuantityUnionUsers, timezone: "UTC", value: 3},
	}
	if !reflect.DeepEqual(metrics.quantities, want) {
		t.Fatalf("provider-wide union quantities = %#v, want %#v", metrics.quantities, want)
	}
}

func TestPrewarmReaderMetricsUseClosedFallbackReasons(t *testing.T) {
	now := time.Date(2026, 7, 21, 8, 0, 0, 0, time.UTC)
	cache, _ := newRedisPrewarmCache(t, func() time.Time { return now })
	metrics := &recordingPrewarmRequestMetrics{}
	reader := mustPrewarmReader(t, cache, &readerSourceLimiter{}, PrewarmReaderOptions{
		Timezones: []string{"UTC"}, Now: func() time.Time { return now }, Metrics: metrics,
	})
	provider := &prewarmReaderProvider{fakeRelayProvider: &fakeRelayProvider{}}

	requests := []struct {
		request PrewarmReadRequest
		want    prewarmRequestMetric
	}{
		{
			request: PrewarmReadRequest{Params: prewarmReader7dParams()},
			want:    prewarmRequestMetric{timezone: "UTC", outcome: "fallback", reason: "invalid_request"},
		},
		{
			request: PrewarmReadRequest{
				ProviderID: 7, ActorUserID: 1, ProviderVersion: 11, ScopeVersion: "scope-v1", Provider: provider,
				Params: OverviewParams{StartDate: "2026-07-01", EndDate: "2026-07-21", Granularity: "day", Timezone: "UTC"},
			},
			want: prewarmRequestMetric{timezone: "UTC", outcome: "ineligible", reason: "ineligible"},
		},
		{
			request: PrewarmReadRequest{
				ProviderID: 7, ActorUserID: 1, ProviderVersion: 11, ScopeVersion: "scope-v1", Provider: provider,
				Params: prewarmReader7dParams(), AuthorizedRelayUserIDs: []int64{101},
			},
			want: prewarmRequestMetric{timezone: "UTC", outcome: "miss", reason: "cache_miss"},
		},
	}
	for _, test := range requests {
		_, _, _ = reader.ReadAuthorizedOrigin(context.Background(), test.request)
	}
	if !reflect.DeepEqual(metrics.requests, []prewarmRequestMetric{requests[0].want, requests[1].want, requests[2].want}) {
		t.Fatalf("request metrics = %#v, want closed reasons %#v", metrics.requests, []prewarmRequestMetric{requests[0].want, requests[1].want, requests[2].want})
	}
}

func TestPrewarmReaderSlotUsesExactFallbackBeforeAtomicInstall(t *testing.T) {
	client := testdb.Open(t)
	createPrimaryRelayProvider(t, client)
	scope, provider := membersTestData(1)
	rangeCost, rangeTokens := 7.0, int64(70)
	provider.summaryStats[10001] = relay.TeamUserUsageStats{
		UserID: 10001, RangeActualCost: &rangeCost, RangeTotalTokens: &rangeTokens,
	}
	provider.trendPoints[10001] = []relay.UsageTrendPoint{{Date: "2026-07-15", ActualCost: 7, TotalTokens: &rangeTokens}}
	service := newUncachedServiceForTest(client, fakeScopeResolver{scope: scope}, fakeProviderResolver{provider: provider}, nil)
	service.prewarmReaderSource = NewPrewarmReaderSlot()
	service.originCache, _ = testOriginCache(t, time.Now, "empty-reader-slot-fallback-token")

	response, err := service.Summary(context.Background(), 1, prewarmReader7dParams())
	if err != nil {
		t.Fatalf("Summary() error = %v", err)
	}
	if response.Summary.RangeActualCost == nil || *response.Summary.RangeActualCost != 7 {
		t.Fatalf("empty-slot fallback range = %#v, want exact 7", response.Summary.RangeActualCost)
	}
	if len(provider.summaryRequestBatches) == 0 || provider.trendCalls == 0 {
		t.Fatalf("empty-slot exact fallback calls = stats %d trend %d", len(provider.summaryRequestBatches), provider.trendCalls)
	}
}

func TestPrewarmReaderAuthorizedRosterAbsenceAndEligibilityFailuresSelectFallback(t *testing.T) {
	now := time.Date(2026, 7, 21, 8, 0, 0, 0, time.UTC)
	store := newRecordingPrewarmStore()
	cache := mustNewPrewarmCache(t, store, func() time.Time { return now })
	seedAuthorizedPrewarmManifest(t, cache, testPrewarmIdentity(), now, []int64{101})
	reader := mustPrewarmReader(t, cache, &readerSourceLimiter{}, PrewarmReaderOptions{
		Timezones: []string{"UTC", "America/Los_Angeles"}, Now: func() time.Time { return now },
	})
	provider := &prewarmReaderProvider{fakeRelayProvider: &fakeRelayProvider{}}

	origin, outcome, err := reader.ReadAuthorizedOrigin(context.Background(), PrewarmReadRequest{
		ProviderID: 7, ActorUserID: 1, ProviderVersion: 11, ScopeVersion: "scope-v1",
		Params: prewarmReader7dParams(), AuthorizedRelayUserIDs: []int64{999}, Provider: provider,
	})
	if err != nil || origin != nil || outcome != PrewarmReadFallback {
		t.Fatalf("missing authorized roster = %#v/%q/%v, want exact fallback", origin, outcome, err)
	}

	for _, test := range []struct {
		name   string
		params OverviewParams
	}{
		{name: "custom", params: OverviewParams{StartDate: "2026-07-01", EndDate: "2026-07-21", Granularity: "day", Timezone: "UTC"}},
		{name: "unconfigured timezone", params: OverviewParams{StartDate: "2026-07-21", EndDate: "2026-07-21", Granularity: "hour", Timezone: "Asia/Shanghai"}},
		{name: "DST rollover", params: OverviewParams{StartDate: "2026-03-03", EndDate: "2026-03-09", Granularity: "day", Timezone: "America/Los_Angeles"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			before := store.GetCalls()
			origin, outcome, err := reader.ReadAuthorizedOrigin(context.Background(), PrewarmReadRequest{
				ProviderID: 7, ActorUserID: 1, ProviderVersion: 11, ScopeVersion: "scope-v1",
				Params: test.params, AuthorizedRelayUserIDs: []int64{101}, Provider: provider,
			})
			if err != nil || origin != nil || outcome != PrewarmReadIneligible {
				t.Fatalf("ReadAuthorizedOrigin() = %#v/%q/%v, want clean ineligible", origin, outcome, err)
			}
			if store.GetCalls() != before {
				t.Fatalf("ineligible request made %d manifest reads, want zero", store.GetCalls()-before)
			}
		})
	}
}

func TestPrewarmPartialTodayFetchesOnlyTodayThroughSharedLimiterAndPublishes(t *testing.T) {
	now := time.Date(2026, 7, 21, 8, 0, 0, 0, time.UTC)
	cache, server := newRedisPrewarmCache(t, func() time.Time { return now })
	identity := testPrewarmIdentity()
	manifest := seedAuthorizedPrewarmManifest(t, cache, identity, now, []int64{101})
	server.Del(manifest.TodayHour.Key)
	limiter := &readerSourceLimiter{}
	metrics := &recordingPrewarmRequestMetrics{}
	reader := mustPrewarmReader(t, cache, limiter, PrewarmReaderOptions{
		Timezones: []string{"UTC"}, Now: func() time.Time { return now },
		NewToken:        func() string { return strings.Repeat("a", 64) },
		NewGenerationID: func() string { return strings.Repeat("b", 64) },
		Metrics:         metrics,
	})
	provider := &prewarmReaderProvider{
		fakeRelayProvider: &fakeRelayProvider{},
		trendResult: relay.ProviderWideTrendResult{
			Coverage:      relay.TeamMemberTrendParams{StartDate: "2026-07-21", EndDate: "2026-07-21", Granularity: "hour", Timezone: "UTC"},
			Points:        []relay.ProviderWideTrendPoint{{UserID: 101, Date: "2026-07-21 08:00", ActualCost: 2, TotalTokens: int64Ptr(20)}},
			ResponseBytes: 64, PointCount: 1, UniqueUserCount: 1, Complete: true,
		},
	}

	request := PrewarmReadRequest{
		ProviderID: 7, ActorUserID: 1, ProviderVersion: 11, ScopeVersion: "scope-v1",
		Params: prewarmReader7dParams(), AuthorizedRelayUserIDs: []int64{101}, Provider: provider,
	}
	origin, outcome, err := reader.ReadAuthorizedOrigin(context.Background(), request)
	if err != nil || outcome != PrewarmReadPartialToday || origin == nil {
		t.Fatalf("ReadAuthorizedOrigin(partial) = %#v/%q/%v", origin, outcome, err)
	}
	if limiter.calls.Load() != 1 || provider.trendCalls.Load() != 1 {
		t.Fatalf("partial source calls = limiter %d/provider %d, want one today call", limiter.calls.Load(), provider.trendCalls.Load())
	}
	if !reflect.DeepEqual(metrics.sources, []prewarmSourceMetric{{
		class: "today_hour", timezone: "UTC", outcome: "success", bytes: 64, points: 1, users: 1,
	}}) {
		t.Fatalf("partial source metrics = %#v, want bounded today source evidence", metrics.sources)
	}
	if got := provider.params(); len(got) != 1 || got[0] != (relay.TeamMemberTrendParams{
		StartDate: "2026-07-21", EndDate: "2026-07-21", Granularity: "hour", Timezone: "UTC",
	}) {
		t.Fatalf("partial source params = %#v, want exact D..D/hour only", got)
	}
	result := readPrewarmResult(t, cache, identity)
	if !result.Complete || result.Manifest.TodayHour.GenerationID != strings.Repeat("b", 64) {
		t.Fatalf("published partial recovery = %#v, want complete new manifest", result)
	}

	if _, outcome, err = reader.ReadAuthorizedOrigin(context.Background(), request); err != nil || outcome != PrewarmReadFullHit {
		t.Fatalf("second ReadAuthorizedOrigin() outcome/error = %q/%v, want Redis full hit", outcome, err)
	}
	if limiter.calls.Load() != 1 {
		t.Fatalf("second request reused a completed Pod value/source: calls=%d, want Redis-only hit", limiter.calls.Load())
	}
}

func TestPrewarmPartialTodayComposesWhenUnselectedHistoryIsUnavailable(t *testing.T) {
	now := time.Date(2026, 7, 21, 8, 0, 0, 0, time.UTC)
	tests := []struct {
		name          string
		params        OverviewParams
		unselectedRef func(PrewarmManifest) PrewarmValueReference
	}{
		{
			name:   "today",
			params: OverviewParams{StartDate: "2026-07-21", EndDate: "2026-07-21", Granularity: "hour", Timezone: "UTC"},
			unselectedRef: func(manifest PrewarmManifest) PrewarmValueReference {
				return manifest.History29d
			},
		},
		{
			name:   "7d",
			params: prewarmReader7dParams(),
			unselectedRef: func(manifest PrewarmManifest) PrewarmValueReference {
				return manifest.History29d
			},
		},
		{
			name: "30d",
			params: OverviewParams{
				StartDate: "2026-06-22", EndDate: "2026-07-21", Granularity: "day", Timezone: "UTC",
			},
			unselectedRef: func(manifest PrewarmManifest) PrewarmValueReference {
				return manifest.History6d
			},
		},
	}
	for _, test := range tests {
		for _, damage := range []string{"deleted", "corrupt"} {
			t.Run(test.name+"/"+damage, func(t *testing.T) {
				cache, server := newRedisPrewarmCache(t, func() time.Time { return now })
				identity := testPrewarmIdentity()
				manifest := seedAuthorizedPrewarmManifest(t, cache, identity, now, []int64{101})
				server.Del(manifest.TodayHour.Key)
				unselected := test.unselectedRef(manifest)
				if damage == "deleted" {
					server.Del(unselected.Key)
				} else {
					server.Set(unselected.Key, "corrupt-zstd-frame")
					server.SetTTL(unselected.Key, historyValueTTL)
				}

				limiter := &readerSourceLimiter{}
				reader := mustPrewarmReader(t, cache, limiter, PrewarmReaderOptions{
					Timezones: []string{"UTC"}, Now: func() time.Time { return now },
					NewToken:        func() string { return strings.Repeat("a", 64) },
					NewGenerationID: func() string { return strings.Repeat("b", 64) },
				})
				provider := &prewarmReaderProvider{
					fakeRelayProvider: &fakeRelayProvider{},
					trendResult: relay.ProviderWideTrendResult{
						Coverage: relay.TeamMemberTrendParams{
							StartDate: "2026-07-21", EndDate: "2026-07-21", Granularity: "hour", Timezone: "UTC",
						},
						Points: []relay.ProviderWideTrendPoint{
							{UserID: 101, Date: "2026-07-21 08:00", ActualCost: 2, TotalTokens: int64Ptr(20)},
						},
						ResponseBytes: 64, PointCount: 1, UniqueUserCount: 1, Complete: true,
					},
				}

				origin, outcome, err := reader.ReadAuthorizedOrigin(context.Background(), PrewarmReadRequest{
					ProviderID: 7, ActorUserID: 1, ProviderVersion: 11, ScopeVersion: "scope-v1",
					Params: test.params, AuthorizedRelayUserIDs: []int64{101}, Provider: provider,
				})
				if err != nil || outcome != PrewarmReadPartialToday || origin == nil {
					t.Fatalf("ReadAuthorizedOrigin(%s, %s unselected history) = %#v/%q/%v, want request-scoped recovery", test.name, damage, origin, outcome, err)
				}
				if limiter.calls.Load() != 1 || provider.trendCalls.Load() != 1 {
					t.Fatalf("request-scoped source calls = limiter %d/provider %d, want one today call", limiter.calls.Load(), provider.trendCalls.Load())
				}
				manifestKey, keyErr := prewarmManifestKeyForIdentity("test", prewarmCacheSchemaVersion, identity)
				if keyErr != nil {
					t.Fatalf("prewarmManifestKeyForIdentity() error = %v", keyErr)
				}
				encodedManifest, getErr := server.Get(manifestKey)
				if getErr != nil {
					t.Fatalf("get stored manifest error = %v", getErr)
				}
				var storedManifest PrewarmManifest
				if decodeErr := decodePrewarmJSON([]byte(encodedManifest), prewarmManifestMaxBytes, &storedManifest); decodeErr != nil {
					t.Fatalf("decode stored manifest error = %v", decodeErr)
				}
				if storedManifest.TodayHour != manifest.TodayHour {
					t.Fatalf("incomplete full generation was published: today ref = %#v, want original %#v", storedManifest.TodayHour, manifest.TodayHour)
				}
			})
		}
	}
}

func TestPrewarmPartialTodayRelayFailureSelectsCompleteFallback(t *testing.T) {
	now := time.Date(2026, 7, 21, 8, 0, 0, 0, time.UTC)
	cache, server := newRedisPrewarmCache(t, func() time.Time { return now })
	manifest := seedAuthorizedPrewarmManifest(t, cache, testPrewarmIdentity(), now, []int64{101})
	server.Del(manifest.TodayHour.Key)
	metrics := &recordingPrewarmRequestMetrics{}
	reader := mustPrewarmReader(t, cache, &readerSourceLimiter{}, PrewarmReaderOptions{
		Timezones: []string{"UTC"}, Now: func() time.Time { return now }, Metrics: metrics,
	})
	provider := &prewarmReaderProvider{fakeRelayProvider: &fakeRelayProvider{}, trendErr: errors.New("synthetic today outage")}

	origin, outcome, err := reader.ReadAuthorizedOrigin(context.Background(), PrewarmReadRequest{
		ProviderID: 7, ActorUserID: 1, ProviderVersion: 11, ScopeVersion: "scope-v1",
		Params: prewarmReader7dParams(), AuthorizedRelayUserIDs: []int64{101}, Provider: provider,
	})
	if err == nil || origin != nil || outcome != PrewarmReadFallback {
		t.Fatalf("partial Relay failure = %#v/%q/%v, want exact fallback signal", origin, outcome, err)
	}
	last := metrics.requests[len(metrics.requests)-1]
	if last.reason != "source_error" {
		t.Fatalf("partial Relay fallback reason = %q, want source_error", last.reason)
	}
}

func TestPrewarmPartialTodayRedisLeaseFailureIsNotClassifiedAsSource(t *testing.T) {
	now := time.Date(2026, 7, 21, 8, 0, 0, 0, time.UTC)
	store := newRecordingPrewarmStore()
	cache := mustNewPrewarmCache(t, store, func() time.Time { return now })
	manifest := seedAuthorizedPrewarmManifest(t, cache, testPrewarmIdentity(), now, []int64{101})
	store.mu.Lock()
	delete(store.values, manifest.TodayHour.Key)
	store.mu.Unlock()
	store.leaseErr = errors.New("dynamic Redis lease detail")
	metrics := &recordingPrewarmRequestMetrics{}
	reader := mustPrewarmReader(t, cache, &readerSourceLimiter{}, PrewarmReaderOptions{
		Timezones: []string{"UTC"}, Now: func() time.Time { return now }, Metrics: metrics,
	})
	provider := &prewarmReaderProvider{fakeRelayProvider: &fakeRelayProvider{}}

	origin, outcome, err := reader.ReadAuthorizedOrigin(context.Background(), PrewarmReadRequest{
		ProviderID: 7, ActorUserID: 1, ProviderVersion: 11, ScopeVersion: "scope-v1",
		Params: prewarmReader7dParams(), AuthorizedRelayUserIDs: []int64{101}, Provider: provider,
	})
	if err == nil || origin != nil || outcome != PrewarmReadFallback {
		t.Fatalf("partial Redis failure = %#v/%q/%v, want exact fallback signal", origin, outcome, err)
	}
	last := metrics.requests[len(metrics.requests)-1]
	if last.reason != "redis_error" {
		t.Fatalf("partial Redis fallback reason = %q, want redis_error", last.reason)
	}
}

func TestPrewarmPartialTodayAuthorizedRosterAbsenceFallsBackBeforeSource(t *testing.T) {
	now := time.Date(2026, 7, 21, 8, 0, 0, 0, time.UTC)
	cache, server := newRedisPrewarmCache(t, func() time.Time { return now })
	manifest := seedAuthorizedPrewarmManifest(t, cache, testPrewarmIdentity(), now, []int64{101})
	server.Del(manifest.TodayHour.Key)
	limiter := &readerSourceLimiter{}
	reader := mustPrewarmReader(t, cache, limiter, PrewarmReaderOptions{
		Timezones: []string{"UTC"}, Now: func() time.Time { return now },
	})
	provider := &prewarmReaderProvider{fakeRelayProvider: &fakeRelayProvider{}}

	origin, outcome, err := reader.ReadAuthorizedOrigin(context.Background(), PrewarmReadRequest{
		ProviderID: 7, ActorUserID: 1, ProviderVersion: 11, ScopeVersion: "scope-v1",
		Params: prewarmReader7dParams(), AuthorizedRelayUserIDs: []int64{999}, Provider: provider,
	})
	if err != nil || origin != nil || outcome != PrewarmReadFallback {
		t.Fatalf("partial missing roster = %#v/%q/%v, want exact fallback", origin, outcome, err)
	}
	if limiter.calls.Load() != 0 || provider.trendCalls.Load() != 0 {
		t.Fatalf("partial missing roster source calls = %d/%d, want zero", limiter.calls.Load(), provider.trendCalls.Load())
	}
}

func TestPrewarmPartialTodayFlightComposesEachAuthorizedWaiterIndependently(t *testing.T) {
	now := time.Date(2026, 7, 21, 8, 0, 0, 0, time.UTC)
	cache, server := newRedisPrewarmCache(t, func() time.Time { return now })
	manifest := seedAuthorizedPrewarmManifest(t, cache, testPrewarmIdentity(), now, []int64{101, 102})
	server.Del(manifest.TodayHour.Key)
	reader := mustPrewarmReader(t, cache, &readerSourceLimiter{}, PrewarmReaderOptions{
		Timezones: []string{"UTC"}, Now: func() time.Time { return now },
	})
	started, release := make(chan struct{}), make(chan struct{})
	provider := &prewarmReaderProvider{
		fakeRelayProvider: &fakeRelayProvider{}, started: started, release: release,
		trendResult: relay.ProviderWideTrendResult{
			Coverage: relay.TeamMemberTrendParams{StartDate: "2026-07-21", EndDate: "2026-07-21", Granularity: "hour", Timezone: "UTC"},
			Points: []relay.ProviderWideTrendPoint{
				{UserID: 101, Date: "2026-07-21 08:00", ActualCost: 1},
				{UserID: 102, Date: "2026-07-21 08:00", ActualCost: 2},
			},
			ResponseBytes: 64, PointCount: 2, UniqueUserCount: 2, Complete: true,
		},
	}
	type readResult struct {
		origin  *teamUsageScopeOrigin
		outcome PrewarmReadOutcome
		err     error
	}
	results := make(chan readResult, 2)
	read := func(actor int, authorized int64) {
		origin, outcome, err := reader.ReadAuthorizedOrigin(context.Background(), PrewarmReadRequest{
			ProviderID: 7, ActorUserID: actor, ProviderVersion: 11, ScopeVersion: "scope-v1",
			Params: prewarmReader7dParams(), AuthorizedRelayUserIDs: []int64{authorized}, Provider: provider,
		})
		results <- readResult{origin: origin, outcome: outcome, err: err}
	}
	go read(1, 101)
	<-started
	go read(2, 102)
	time.Sleep(20 * time.Millisecond)
	close(release)

	seen := map[int64]bool{}
	for range 2 {
		result := <-results
		if result.err != nil || result.outcome != PrewarmReadPartialToday || result.origin == nil || len(result.origin.RelayUserIDs) != 1 {
			t.Fatalf("concurrent partial result = %#v/%q/%v", result.origin, result.outcome, result.err)
		}
		seen[result.origin.RelayUserIDs[0]] = true
	}
	if !seen[101] || !seen[102] || provider.trendCalls.Load() != 1 {
		t.Fatalf("authorized waiter projections/source calls = %#v/%d, want distinct 101+102 with one flight", seen, provider.trendCalls.Load())
	}
}

func TestPrewarmPartialTodayLifecycleMovingLeaseSelectsExactFallbackWithoutConcurrentSource(t *testing.T) {
	now := time.Date(2026, 7, 21, 8, 0, 0, 0, time.UTC)
	client := testdb.Open(t)
	providerRow := createPrimaryRelayProvider(t, client)
	scope, exactProvider := membersTestData(1)
	rangeCost, rangeTokens := 9.0, int64(90)
	exactProvider.summaryStats[10001] = relay.TeamUserUsageStats{
		UserID: 10001, TodayActualCost: 3, TotalActualCost: 30, RangeActualCost: &rangeCost, RangeTotalTokens: &rangeTokens,
	}
	exactProvider.trendPoints[10001] = []relay.UsageTrendPoint{{Date: "2026-07-15", ActualCost: 9, TotalTokens: &rangeTokens}}
	provider := &prewarmReaderProvider{
		fakeRelayProvider: exactProvider,
		trendResult: relay.ProviderWideTrendResult{
			Coverage:      relay.TeamMemberTrendParams{StartDate: "2026-07-21", EndDate: "2026-07-21", Granularity: "hour", Timezone: "UTC"},
			Points:        []relay.ProviderWideTrendPoint{{UserID: 10001, Date: "2026-07-21 08:00", ActualCost: 2, TotalTokens: int64Ptr(20)}},
			ResponseBytes: 64, PointCount: 1, UniqueUserCount: 1, Complete: true,
		},
	}
	cache, server := newRedisPrewarmCache(t, func() time.Time { return now })
	identity := PrewarmCacheIdentity{
		ProviderID: providerRow.ID, ProviderVersion: providerRow.ConfigurationVersion, Timezone: "UTC", AnchorDate: "2026-07-21",
	}
	manifest := seedAuthorizedPrewarmManifest(t, cache, identity, now, []int64{10001})
	server.Del(manifest.TodayHour.Key)
	lifecycleLeaseKey := cache.LeaseKey(
		"segment", strconv.Itoa(identity.ProviderID), strconv.FormatInt(identity.ProviderVersion, 10),
		prewarmTimezoneDigest(identity.Timezone), identity.AnchorDate, "moving",
	)
	lifecycleToken := strings.Repeat("f", 64)
	acquired, err := cache.TryAcquireLease(context.Background(), lifecycleLeaseKey, lifecycleToken, prewarmWorkerLeaseTTL)
	if err != nil || !acquired {
		t.Fatalf("TryAcquireLease(lifecycle moving) = %v/%v", acquired, err)
	}
	limiter := &readerSourceLimiter{}
	reader := mustPrewarmReader(t, cache, limiter, PrewarmReaderOptions{
		Timezones: []string{"UTC"}, Now: func() time.Time { return now }, NewToken: func() string { return strings.Repeat("e", 64) },
	})
	service := newUncachedServiceForTest(client, fakeScopeResolver{scope: scope}, fakeProviderResolver{provider: provider}, nil)
	installTestPrewarmReader(service, reader)
	service.originCache, _ = testOriginCache(t, time.Now, "moving-lease-fallback-token")

	response, err := service.Summary(context.Background(), 1, prewarmReader7dParams())
	if err != nil {
		t.Fatalf("Summary() error = %v", err)
	}
	if response.Summary.RangeActualCost == nil || *response.Summary.RangeActualCost != 9 {
		t.Fatalf("moving-lease fallback range = %#v, want exact 9", response.Summary.RangeActualCost)
	}
	if limiter.calls.Load() != 0 || provider.trendCalls.Load() != 0 {
		t.Fatalf("moving-lease concurrent partial source calls = %d/%d, want zero", limiter.calls.Load(), provider.trendCalls.Load())
	}
	if len(exactProvider.summaryRequestBatches) == 0 || exactProvider.trendCalls == 0 {
		t.Fatalf("moving-lease exact fallback calls = stats %d trend %d, want complete exact origin", len(exactProvider.summaryRequestBatches), exactProvider.trendCalls)
	}
}

func prewarmReader7dParams() OverviewParams {
	return OverviewParams{StartDate: "2026-07-15", EndDate: "2026-07-21", Granularity: "day", Timezone: "UTC"}
}

func mustPrewarmReader(t *testing.T, cache *PrewarmCache, limiter SourceCallLimiter, options PrewarmReaderOptions) *PrewarmReader {
	t.Helper()
	reader, err := NewPrewarmReader(cache, limiter, options)
	if err != nil {
		t.Fatalf("NewPrewarmReader() error = %v", err)
	}
	return reader
}

func installTestPrewarmReader(service *Service, reader *PrewarmReader) {
	slot := NewPrewarmReaderSlot()
	slot.Store(reader)
	service.prewarmReaderSource = slot
}

func seedAuthorizedPrewarmManifest(
	t *testing.T,
	cache *PrewarmCache,
	identity PrewarmCacheIdentity,
	generatedAt time.Time,
	roster []int64,
) PrewarmManifest {
	t.Helper()
	stats := make([]PrewarmCurrentStat, len(roster))
	for index, userID := range roster {
		stats[index] = PrewarmCurrentStat{UserID: userID, TodayActualCost: float64(index + 1), TotalActualCost: float64((index + 1) * 10)}
	}
	current, err := cache.WriteCurrentStats(context.Background(), PrewarmCurrentStatsEnvelope{
		SchemaVersion: prewarmCacheSchemaVersion, ProviderID: identity.ProviderID, ProviderVersion: identity.ProviderVersion,
		GenerationID: strings.Repeat("c", 64), GeneratedAt: generatedAt, RosterCount: len(roster),
		RosterDigest: prewarmRosterDigest(roster), ResponseBytes: 64, Stats: stats,
	})
	if err != nil {
		t.Fatalf("WriteCurrentStats() error = %v", err)
	}
	refs := make(map[PrewarmSegmentClass]PrewarmValueReference, 3)
	for index, class := range []PrewarmSegmentClass{SegmentHistory29d, SegmentHistory6d, SegmentTodayHour} {
		coverage, coverageErr := prewarmSegmentCoverage(class, identity.AnchorDate, identity.Timezone)
		if coverageErr != nil {
			t.Fatalf("prewarmSegmentCoverage(%s) error = %v", class, coverageErr)
		}
		points := []relay.ProviderWideTrendPoint{}
		if len(roster) > 0 && class != SegmentTodayHour {
			points = append(points, relay.ProviderWideTrendPoint{UserID: roster[0], Date: coverage.StartDate, ActualCost: 1, TotalTokens: int64Ptr(10)})
		}
		segment := PrewarmTrendSegment{
			SchemaVersion: prewarmCacheSchemaVersion, ProviderID: identity.ProviderID, ProviderVersion: identity.ProviderVersion,
			TimezoneDigest: prewarmTimezoneDigest(identity.Timezone), GenerationID: strings.Repeat(string(rune('d'+index)), 64),
			GeneratedAt: generatedAt, Timezone: identity.Timezone, AnchorDate: identity.AnchorDate, Class: class, Coverage: coverage,
			Points: points, ResponseBytes: 32, PointCount: len(points), Complete: true,
		}
		if len(points) > 0 {
			segment.UniqueUserCount = 1
		}
		refs[class], err = cache.WriteSegment(context.Background(), segment)
		if err != nil {
			t.Fatalf("WriteSegment(%s) error = %v", class, err)
		}
	}
	manifest := PrewarmManifest{
		SchemaVersion: prewarmCacheSchemaVersion, ProviderID: identity.ProviderID, ProviderVersion: identity.ProviderVersion,
		Timezone: identity.Timezone, TimezoneDigest: prewarmTimezoneDigest(identity.Timezone), AnchorDate: identity.AnchorDate,
		CreatedAt: generatedAt, CurrentStats: current, History29d: refs[SegmentHistory29d],
		History6d: refs[SegmentHistory6d], TodayHour: refs[SegmentTodayHour],
	}
	publishSeedManifest(t, cache, manifest)
	return manifest
}

type readerSourceLimiter struct {
	calls atomic.Int32
}

type prewarmRequestMetric struct {
	timezone string
	outcome  string
	reason   string
}

type prewarmCycleMetric struct {
	class    string
	timezone string
	outcome  string
	duration time.Duration
}

type prewarmSourceMetric struct {
	class    string
	timezone string
	outcome  string
	bytes    int
	points   int
	users    int
}

type prewarmQuantityMetric struct {
	kind     PrewarmQuantityKind
	timezone string
	value    int
}

type prewarmValidationMetric struct {
	check   PrewarmValidationCheck
	outcome PrewarmValidationOutcome
}

type prewarmCacheMetric struct {
	cache   PrewarmCacheKind
	outcome PrewarmCacheOutcome
}

type recordingPrewarmRequestMetrics struct {
	mu          sync.Mutex
	cycleHook   func(class, timezone, outcome string)
	cycles      []prewarmCycleMetric
	requests    []prewarmRequestMetric
	sources     []prewarmSourceMetric
	quantities  []prewarmQuantityMetric
	validations []prewarmValidationMetric
	caches      []prewarmCacheMetric
	generation  []int
}

func (m *recordingPrewarmRequestMetrics) RecordCycle(class, timezone, outcome string, duration time.Duration) {
	m.mu.Lock()
	m.cycles = append(m.cycles, prewarmCycleMetric{class: class, timezone: timezone, outcome: outcome, duration: duration})
	hook := m.cycleHook
	m.mu.Unlock()
	if hook != nil {
		hook(class, timezone, outcome)
	}
}
func (m *recordingPrewarmRequestMetrics) RecordSource(class, timezone, outcome string, _ time.Duration, bytes, points, users int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sources = append(m.sources, prewarmSourceMetric{
		class: class, timezone: timezone, outcome: outcome, bytes: bytes, points: points, users: users,
	})
}
func (*recordingPrewarmRequestMetrics) RecordRedis(string, string, time.Duration, int) {}
func (m *recordingPrewarmRequestMetrics) RecordRequest(timezone, outcome, reason string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.requests = append(m.requests, prewarmRequestMetric{timezone: timezone, outcome: outcome, reason: reason})
}
func (*recordingPrewarmRequestMetrics) SetLastSuccess(string, string, time.Time) {}
func (m *recordingPrewarmRequestMetrics) RecordQuantity(kind PrewarmQuantityKind, timezone string, value int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.quantities = append(m.quantities, prewarmQuantityMetric{kind: kind, timezone: timezone, value: value})
}
func (m *recordingPrewarmRequestMetrics) SetGenerationBytes(value int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.generation = append(m.generation, value)
}
func (m *recordingPrewarmRequestMetrics) RecordValidation(check PrewarmValidationCheck, outcome PrewarmValidationOutcome) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.validations = append(m.validations, prewarmValidationMetric{check: check, outcome: outcome})
}
func (m *recordingPrewarmRequestMetrics) RecordCache(cache PrewarmCacheKind, outcome PrewarmCacheOutcome) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.caches = append(m.caches, prewarmCacheMetric{cache: cache, outcome: outcome})
}

func (l *readerSourceLimiter) Do(ctx context.Context, call func(context.Context) error) error {
	l.calls.Add(1)
	return call(ctx)
}

type prewarmReaderProvider struct {
	*fakeRelayProvider
	trendCalls  atomic.Int32
	trendMu     sync.Mutex
	trendParams []relay.TeamMemberTrendParams
	trendResult relay.ProviderWideTrendResult
	trendErr    error
	started     chan struct{}
	release     <-chan struct{}
	startOnce   sync.Once
}

func (p *prewarmReaderProvider) GetProviderUsageTrend(ctx context.Context, params relay.TeamMemberTrendParams, _ int) (relay.ProviderWideTrendResult, error) {
	p.trendCalls.Add(1)
	p.trendMu.Lock()
	p.trendParams = append(p.trendParams, params)
	p.trendMu.Unlock()
	if p.started != nil {
		p.startOnce.Do(func() { close(p.started) })
	}
	if p.release != nil {
		select {
		case <-p.release:
		case <-ctx.Done():
			return relay.ProviderWideTrendResult{}, ctx.Err()
		}
	}
	if p.trendErr != nil {
		return relay.ProviderWideTrendResult{}, p.trendErr
	}
	return p.trendResult, nil
}

func (p *prewarmReaderProvider) params() []relay.TeamMemberTrendParams {
	p.trendMu.Lock()
	defer p.trendMu.Unlock()
	return append([]relay.TeamMemberTrendParams(nil), p.trendParams...)
}

type sequenceScopeResolver struct {
	mu     sync.Mutex
	scopes []*representativescope.Scope
	calls  int
}

func (r *sequenceScopeResolver) Resolve(context.Context, int) (*representativescope.Scope, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	index := r.calls
	if index >= len(r.scopes) {
		index = len(r.scopes) - 1
	}
	r.calls++
	return r.scopes[index], nil
}
