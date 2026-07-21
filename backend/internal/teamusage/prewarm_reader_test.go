package teamusage

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/ai-efficiency/backend/internal/representativescope"
)

func TestPrewarmReaderFullHitFiltersAuthorizedRosterAndUsesSparseZero(t *testing.T) {
	now := time.Date(2026, 7, 21, 8, 0, 0, 0, time.UTC)
	cache, _ := newRedisPrewarmCache(t, func() time.Time { return now })
	identity := testPrewarmIdentity()
	seedAuthorizedPrewarmManifest(t, cache, identity, now, []int64{101, 102})
	limiter := &readerSourceLimiter{}
	reader := mustPrewarmReader(t, cache, limiter, PrewarmReaderOptions{
		Timezones: []string{"UTC"}, Now: func() time.Time { return now },
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
	reader := mustPrewarmReader(t, cache, limiter, PrewarmReaderOptions{
		Timezones: []string{"UTC"}, Now: func() time.Time { return now },
		NewToken:        func() string { return strings.Repeat("a", 64) },
		NewGenerationID: func() string { return strings.Repeat("b", 64) },
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

func TestPrewarmPartialTodayRelayFailureSelectsCompleteFallback(t *testing.T) {
	now := time.Date(2026, 7, 21, 8, 0, 0, 0, time.UTC)
	cache, server := newRedisPrewarmCache(t, func() time.Time { return now })
	manifest := seedAuthorizedPrewarmManifest(t, cache, testPrewarmIdentity(), now, []int64{101})
	server.Del(manifest.TodayHour.Key)
	reader := mustPrewarmReader(t, cache, &readerSourceLimiter{}, PrewarmReaderOptions{
		Timezones: []string{"UTC"}, Now: func() time.Time { return now },
	})
	provider := &prewarmReaderProvider{fakeRelayProvider: &fakeRelayProvider{}, trendErr: errors.New("synthetic today outage")}

	origin, outcome, err := reader.ReadAuthorizedOrigin(context.Background(), PrewarmReadRequest{
		ProviderID: 7, ActorUserID: 1, ProviderVersion: 11, ScopeVersion: "scope-v1",
		Params: prewarmReader7dParams(), AuthorizedRelayUserIDs: []int64{101}, Provider: provider,
	})
	if err == nil || origin != nil || outcome != PrewarmReadFallback {
		t.Fatalf("partial Relay failure = %#v/%q/%v, want exact fallback signal", origin, outcome, err)
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
