package teamusage

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/ai-efficiency/backend/internal/representativescope"
	"github.com/ai-efficiency/backend/internal/testdb"
)

func TestPrewarmReaderNeverCallsRelayOrWritesRedis(t *testing.T) {
	baseNow := time.Date(2026, 7, 21, 8, 0, 0, 0, time.UTC)
	tests := []struct {
		name        string
		seed        bool
		authorized  []int64
		mutate      func(*testing.T, *recordingPrewarmStore, PrewarmManifest, *time.Time)
		wantOutcome PrewarmReadOutcome
		wantInvalid bool
		wantOps     []string
	}{
		{name: "full hit", seed: true, authorized: []int64{101}, wantOutcome: PrewarmReadFullHit, wantOps: []string{"GET", "MGET"}},
		{name: "miss", authorized: []int64{101}, wantOutcome: PrewarmReadMiss, wantOps: []string{"GET"}},
		{name: "value corruption", seed: true, authorized: []int64{101}, wantOutcome: PrewarmReadInvalid, wantInvalid: true, wantOps: []string{"GET", "MGET"}, mutate: func(_ *testing.T, store *recordingPrewarmStore, manifest PrewarmManifest, _ *time.Time) {
			store.SetRaw(manifest.CurrentStats.Key, []byte("corrupt-zstd-frame"), movingValueTTL)
		}},
		{name: "manifest corruption", seed: true, authorized: []int64{101}, wantOutcome: PrewarmReadInvalid, wantInvalid: true, wantOps: []string{"GET"}, mutate: func(t *testing.T, store *recordingPrewarmStore, _ PrewarmManifest, _ *time.Time) {
			manifestKey, err := prewarmManifestKeyForIdentity("test", prewarmCacheSchemaVersion, testPrewarmIdentity())
			if err != nil {
				t.Fatalf("prewarmManifestKeyForIdentity() error = %v", err)
			}
			store.SetRaw(manifestKey, []byte("corrupt-json"), manifestTTL)
		}},
		{name: "manifest Redis failure", authorized: []int64{101}, wantOutcome: PrewarmReadFallback, wantOps: []string{"GET"}, mutate: func(_ *testing.T, store *recordingPrewarmStore, _ PrewarmManifest, _ *time.Time) {
			store.getErr = errors.New("synthetic Redis failure")
		}},
		{name: "value Redis failure", seed: true, authorized: []int64{101}, wantOutcome: PrewarmReadFallback, wantOps: []string{"GET", "MGET"}, mutate: func(_ *testing.T, store *recordingPrewarmStore, _ PrewarmManifest, _ *time.Time) {
			store.mgetErr = errors.New("synthetic Redis failure")
		}},
		{name: "expiry", seed: true, authorized: []int64{101}, wantOutcome: PrewarmReadInvalid, wantOps: []string{"GET", "MGET"}, mutate: func(_ *testing.T, _ *recordingPrewarmStore, _ PrewarmManifest, now *time.Time) {
			*now = now.Add(movingHard)
		}},
		{name: "roster absence", seed: true, authorized: []int64{999}, wantOutcome: PrewarmReadFallback, wantOps: []string{"GET", "MGET"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			now := baseNow
			store := newRecordingPrewarmStore()
			cache := mustNewPrewarmCache(t, store, func() time.Time { return now })
			var manifest PrewarmManifest
			if test.seed {
				manifest = seedAuthorizedPrewarmManifest(t, cache, testPrewarmIdentity(), baseNow, []int64{101})
			}
			if test.mutate != nil {
				test.mutate(t, store, manifest, &now)
			}
			store.ResetOperations()
			reader, err := NewPrewarmReader(cache, PrewarmReaderOptions{Now: func() time.Time { return now }})
			if err != nil {
				t.Fatalf("NewPrewarmReader() error = %v", err)
			}

			origin, outcome, readErr := reader.ReadAuthorizedOrigin(context.Background(), PrewarmReadRequest{
				ProviderID: 7, ProviderVersion: 11, Params: prewarmReader7dParams(),
				AuthorizedRelayUserIDs: test.authorized,
			})
			if outcome != test.wantOutcome {
				t.Fatalf("ReadAuthorizedOrigin() outcome = %q, want %q", outcome, test.wantOutcome)
			}
			if test.wantOutcome == PrewarmReadFullHit {
				if origin == nil {
					t.Fatal("ReadAuthorizedOrigin(full hit) origin = nil")
				}
			} else if origin != nil {
				t.Fatalf("ReadAuthorizedOrigin(%s) origin = %#v, want nil", test.name, origin)
			}
			if got := errors.Is(readErr, errPrewarmCacheInvalid); got != test.wantInvalid {
				t.Fatalf("ReadAuthorizedOrigin() invalid cache error = %v, want %v (error %v)", got, test.wantInvalid, readErr)
			}
			if operations := store.Operations(); !reflect.DeepEqual(operations, test.wantOps) {
				t.Fatalf("Redis operations = %#v, want read-only %#v", operations, test.wantOps)
			}
		})
	}
}

func TestPrewarmReaderFullHitFiltersAuthorizedRosterAndUsesSparseZero(t *testing.T) {
	now := time.Date(2026, 7, 21, 8, 0, 0, 0, time.UTC)
	cache, _ := newRedisPrewarmCache(t, func() time.Time { return now })
	identity := testPrewarmIdentity()
	seedAuthorizedPrewarmManifest(t, cache, identity, now, []int64{101, 102})
	metrics := &recordingPrewarmRequestMetrics{}
	reader := mustPrewarmReader(t, cache, PrewarmReaderOptions{Now: func() time.Time { return now }, Metrics: metrics})

	origin, outcome, err := reader.ReadAuthorizedOrigin(context.Background(), PrewarmReadRequest{ProviderID: 7, ProviderVersion: 11, Params: prewarmReader7dParams(), AuthorizedRelayUserIDs: []int64{102, 101, 102}})
	if err != nil || outcome != PrewarmReadFullHit {
		t.Fatalf("ReadAuthorizedOrigin() outcome/error = %q/%v, want full hit", outcome, err)
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
	if !reflect.DeepEqual(metrics.requests, []prewarmRequestMetric{{outcome: PrewarmReadFullHit}}) {
		t.Fatalf("request metrics = %#v", metrics.requests)
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
			reader := mustPrewarmReader(t, cache, PrewarmReaderOptions{Now: func() time.Time { return now }})

			origin, outcome, err := reader.ReadAuthorizedOrigin(context.Background(), PrewarmReadRequest{ProviderID: 7, ProviderVersion: 11, Params: test.params, AuthorizedRelayUserIDs: []int64{101}})
			if err != nil || outcome != PrewarmReadFullHit || origin == nil {
				t.Fatalf("ReadAuthorizedOrigin(%s) = %#v/%q/%v, want full hit", test.name, origin, outcome, err)
			}
			if got := store.LastMGet(); !reflect.DeepEqual(got, test.want(manifest)) {
				t.Fatalf("ReadAuthorizedOrigin(%s) MGET keys = %#v, want %#v", test.name, got, test.want(manifest))
			}
		})
	}
}

func TestPrewarmReaderTamperedRosterDigestRecordsInvalidAndSelectsExactFallback(t *testing.T) {
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
	reader := mustPrewarmReader(t, cache, PrewarmReaderOptions{Now: func() time.Time { return now }, Metrics: metrics})

	origin, outcome, err := reader.ReadAuthorizedOrigin(context.Background(), PrewarmReadRequest{ProviderID: 7, ProviderVersion: 11, Params: prewarmReader7dParams(), AuthorizedRelayUserIDs: []int64{101}})
	if err == nil || origin != nil || outcome != PrewarmReadInvalid {
		t.Fatalf("tampered roster digest = %#v/%q/%v, want invalid cache signal", origin, outcome, err)
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
	leaseKey := cache.RefreshLeaseKey()
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
	reader := mustPrewarmReader(t, cache, PrewarmReaderOptions{Now: func() time.Time { return now }, Metrics: metrics})

	for _, authorized := range [][]int64{{101}, {102}} {
		origin, outcome, readErr := reader.ReadAuthorizedOrigin(context.Background(), PrewarmReadRequest{ProviderID: 7, ProviderVersion: 11, Params: prewarmReader7dParams(), AuthorizedRelayUserIDs: authorized})
		if readErr != nil || outcome != PrewarmReadFullHit || !reflect.DeepEqual(origin.RelayUserIDs, authorized) {
			t.Fatalf("ReadAuthorizedOrigin(%v) = %#v/%q/%v", authorized, origin, outcome, readErr)
		}
	}
	want := []prewarmRequestMetric{
		{outcome: PrewarmReadFullHit},
		{outcome: PrewarmReadFullHit},
	}
	if !reflect.DeepEqual(metrics.requests, want) {
		t.Fatalf("request metrics = %#v, want %#v", metrics.requests, want)
	}
}

func TestPrewarmReaderMetricsUseClosedOutcomes(t *testing.T) {
	now := time.Date(2026, 7, 21, 8, 0, 0, 0, time.UTC)
	cache, _ := newRedisPrewarmCache(t, func() time.Time { return now })
	metrics := &recordingPrewarmRequestMetrics{}
	reader := mustPrewarmReader(t, cache, PrewarmReaderOptions{Now: func() time.Time { return now }, Metrics: metrics})

	requests := []struct {
		request PrewarmReadRequest
		want    prewarmRequestMetric
	}{
		{
			request: PrewarmReadRequest{Params: prewarmReader7dParams()},
			want:    prewarmRequestMetric{outcome: PrewarmReadFallback},
		},
		{
			request: PrewarmReadRequest{
				ProviderID: 7, ProviderVersion: 11,
				Params: OverviewParams{StartDate: "2026-07-01", EndDate: "2026-07-21", Granularity: "day", Timezone: "UTC"},
			},
			want: prewarmRequestMetric{outcome: PrewarmReadIneligible},
		},
		{
			request: PrewarmReadRequest{
				ProviderID: 7, ProviderVersion: 11,
				Params: prewarmReader7dParams(), AuthorizedRelayUserIDs: []int64{101},
			},
			want: prewarmRequestMetric{outcome: PrewarmReadMiss},
		},
	}
	for _, test := range requests {
		_, _, _ = reader.ReadAuthorizedOrigin(context.Background(), test.request)
	}
	if !reflect.DeepEqual(metrics.requests, []prewarmRequestMetric{requests[0].want, requests[1].want, requests[2].want}) {
		t.Fatalf("request metrics = %#v, want closed outcomes %#v", metrics.requests, []prewarmRequestMetric{requests[0].want, requests[1].want, requests[2].want})
	}
}

func TestServiceUsesDirectPrewarmReaderWithoutMutableSlot(t *testing.T) {
	now := time.Date(2026, 7, 21, 8, 0, 0, 0, time.UTC)
	client := testdb.Open(t)
	providerRow := createPrimaryRelayProvider(t, client)
	scopeV1, provider := membersTestData(1)
	scopeV1.Version = "scope-v1"
	scopeV2 := *scopeV1
	scopeV2.Version = "scope-v2"
	resolver := &sequenceScopeResolver{scopes: []*representativescope.Scope{scopeV1, &scopeV2, &scopeV2}}
	rangeCost, rangeTokens := 7.0, int64(70)
	provider.summaryStats[10001] = relay.TeamUserUsageStats{
		UserID: 10001, RangeActualCost: &rangeCost, RangeTotalTokens: &rangeTokens,
	}
	provider.trendPoints[10001] = []relay.UsageTrendPoint{{Date: "2026-07-15", ActualCost: 7, TotalTokens: &rangeTokens}}
	store := newRecordingPrewarmStore()
	cache := mustNewPrewarmCache(t, store, func() time.Time { return now })
	seedAuthorizedPrewarmManifest(t, cache, PrewarmCacheIdentity{
		ProviderID: providerRow.ID, ProviderVersion: providerRow.ConfigurationVersion,
		Timezone: "UTC", AnchorDate: "2026-07-21",
	}, now, []int64{10001})
	store.getAfter = func(string) {
		if provider.listUsersCalls == 0 {
			t.Error("Redis read happened before current Relay mappings were resolved")
		}
	}
	reader, err := NewPrewarmReader(cache, PrewarmReaderOptions{Now: func() time.Time { return now }})
	if err != nil {
		t.Fatalf("NewPrewarmReader() error = %v", err)
	}
	service := newUncachedServiceForTest(client, resolver, fakeProviderResolver{provider: provider}, nil)
	service.prewarmReader = reader
	service.originCache, _ = testOriginCache(t, time.Now, "direct-reader-fallback-token")

	response, err := service.Summary(context.Background(), 1, prewarmReader7dParams())
	if err != nil {
		t.Fatalf("Summary() error = %v", err)
	}
	if response.ScopeVersion != "scope-v2" || response.Summary.RangeActualCost == nil || *response.Summary.RangeActualCost != 7 {
		t.Fatalf("authorization-race response = scope %q range %#v, want scope-v2 exact 7", response.ScopeVersion, response.Summary.RangeActualCost)
	}
	if len(provider.summaryRequestBatches) == 0 || provider.trendCalls == 0 {
		t.Fatalf("authorization-race exact fallback calls = stats %d trend %d", len(provider.summaryRequestBatches), provider.trendCalls)
	}
}

func TestPrewarmReaderAuthorizedRosterAbsenceAndEligibilityFailuresSelectFallback(t *testing.T) {
	now := time.Date(2026, 7, 21, 8, 0, 0, 0, time.UTC)
	store := newRecordingPrewarmStore()
	cache := mustNewPrewarmCache(t, store, func() time.Time { return now })
	seedAuthorizedPrewarmManifest(t, cache, testPrewarmIdentity(), now, []int64{101})
	reader := mustPrewarmReader(t, cache, PrewarmReaderOptions{Now: func() time.Time { return now }})

	origin, outcome, err := reader.ReadAuthorizedOrigin(context.Background(), PrewarmReadRequest{ProviderID: 7, ProviderVersion: 11, Params: prewarmReader7dParams(), AuthorizedRelayUserIDs: []int64{999}})
	if err != nil || origin != nil || outcome != PrewarmReadFallback {
		t.Fatalf("missing authorized roster = %#v/%q/%v, want exact fallback", origin, outcome, err)
	}

	for _, test := range []struct {
		name        string
		params      OverviewParams
		wantOutcome PrewarmReadOutcome
		wantReads   int
	}{
		{name: "custom", params: OverviewParams{StartDate: "2026-07-01", EndDate: "2026-07-21", Granularity: "day", Timezone: "UTC"}, wantOutcome: PrewarmReadIneligible},
		{name: "timezone without manifest", params: OverviewParams{StartDate: "2026-07-21", EndDate: "2026-07-21", Granularity: "hour", Timezone: "Asia/Shanghai"}, wantOutcome: PrewarmReadMiss, wantReads: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			before := store.GetCalls()
			origin, outcome, err := reader.ReadAuthorizedOrigin(context.Background(), PrewarmReadRequest{ProviderID: 7, ProviderVersion: 11, Params: test.params, AuthorizedRelayUserIDs: []int64{101}})
			if err != nil || origin != nil || outcome != test.wantOutcome {
				t.Fatalf("ReadAuthorizedOrigin() = %#v/%q/%v, want %q", origin, outcome, err, test.wantOutcome)
			}
			if reads := store.GetCalls() - before; reads != test.wantReads {
				t.Fatalf("manifest reads = %d, want %d", reads, test.wantReads)
			}
		})
	}
}

func prewarmReader7dParams() OverviewParams {
	return OverviewParams{StartDate: "2026-07-15", EndDate: "2026-07-21", Granularity: "day", Timezone: "UTC"}
}

func mustPrewarmReader(t *testing.T, cache *PrewarmCache, options PrewarmReaderOptions) *PrewarmReader {
	t.Helper()
	reader, err := NewPrewarmReader(cache, options)
	if err != nil {
		t.Fatalf("NewPrewarmReader() error = %v", err)
	}
	return reader
}

func installTestPrewarmReader(service *Service, reader *PrewarmReader) {
	service.prewarmReader = reader
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

type prewarmRequestMetric struct {
	outcome PrewarmReadOutcome
}

type prewarmRefreshMetric struct {
	outcome  PrewarmRefreshOutcome
	duration time.Duration
}

type prewarmSourceMetric struct {
	source   PrewarmSourceClass
	outcome  PrewarmSourceOutcome
	duration time.Duration
}

type prewarmLaneSuccessMetric struct {
	timezone string
	at       time.Time
}

type recordingPrewarmRequestMetrics struct {
	mu            sync.Mutex
	refreshes     []prewarmRefreshMetric
	laneSuccesses []prewarmLaneSuccessMetric
	requests      []prewarmRequestMetric
	sources       []prewarmSourceMetric
}

func (m *recordingPrewarmRequestMetrics) RecordRefresh(outcome PrewarmRefreshOutcome, duration time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.refreshes = append(m.refreshes, prewarmRefreshMetric{outcome: outcome, duration: duration})
}

func (m *recordingPrewarmRequestMetrics) SetLaneLastSuccess(timezone string, at time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.laneSuccesses = append(m.laneSuccesses, prewarmLaneSuccessMetric{timezone: timezone, at: at})
}

func (m *recordingPrewarmRequestMetrics) RecordSource(
	source PrewarmSourceClass,
	outcome PrewarmSourceOutcome,
	duration time.Duration,
) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sources = append(m.sources, prewarmSourceMetric{source: source, outcome: outcome, duration: duration})
}

func (m *recordingPrewarmRequestMetrics) RecordRequest(outcome PrewarmReadOutcome) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.requests = append(m.requests, prewarmRequestMetric{outcome: outcome})
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
