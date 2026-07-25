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
	"github.com/alicebob/miniredis/v2"
)

func TestRefresherRefreshPlansFromRedis(t *testing.T) {
	tests := []struct {
		name              string
		seedManifest      bool
		advanceLocalDay   bool
		wantCurrentCalls  int
		wantTodayCalls    int
		wantHistory29Call int
		wantHistory6Call  int
	}{
		{name: "first generation", wantCurrentCalls: 1, wantTodayCalls: 1, wantHistory29Call: 1, wantHistory6Call: 1},
		{name: "same anchor reuses hard-valid history", seedManifest: true, wantCurrentCalls: 1, wantTodayCalls: 1},
		{name: "new local anchor fetches both histories", seedManifest: true, advanceLocalDay: true, wantCurrentCalls: 1, wantTodayCalls: 1, wantHistory29Call: 1, wantHistory6Call: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			base := time.Date(2026, 7, 21, 8, 0, 0, 0, time.UTC)
			now := base
			cache, _ := newRedisPrewarmCache(t, func() time.Time { return now })
			if test.seedManifest {
				seedRefresherManifest(t, cache, PrewarmCacheIdentity{
					ProviderID: 7, ProviderVersion: 11, Timezone: "UTC", AnchorDate: "2026-07-21",
				}, base, "seed")
			}
			if test.advanceLocalDay {
				now = base.Add(24 * time.Hour)
			}
			provider := newRefresherTestProvider([]int64{101, 102})
			refresher := mustRefresher(t, staticRefresherBindingResolver{binding: refresherBinding(provider)}, cache,
				refresherTestOptions([]string{"UTC"}, func() time.Time { return now }, test.name))

			if err := refresher.Refresh(context.Background()); err != nil {
				t.Fatalf("Refresh() error = %v", err)
			}
			if got := provider.currentCount(); got != test.wantCurrentCalls {
				t.Fatalf("current stats calls = %d, want %d", got, test.wantCurrentCalls)
			}
			if got := provider.trendCount(SegmentTodayHour); got != test.wantTodayCalls {
				t.Fatalf("today_hour calls = %d, want %d", got, test.wantTodayCalls)
			}
			if got := provider.trendCount(SegmentHistory29d); got != test.wantHistory29Call {
				t.Fatalf("history_29d calls = %d, want %d", got, test.wantHistory29Call)
			}
			if got := provider.trendCount(SegmentHistory6d); got != test.wantHistory6Call {
				t.Fatalf("history_6d calls = %d, want %d", got, test.wantHistory6Call)
			}
			anchor := "2026-07-21"
			if test.advanceLocalDay {
				anchor = "2026-07-22"
			}
			assertRefresherManifest(t, cache, "UTC", anchor)
		})
	}
	// Each case constructs a new Refresher, proving restart behavior derives
	// planning from Redis rather than process-local state.
}

func TestRefresherCurrentStatsFailurePublishesNoManifest(t *testing.T) {
	now := time.Date(2026, 7, 21, 8, 0, 0, 0, time.UTC)
	cache, _ := newRedisPrewarmCache(t, func() time.Time { return now })
	provider := newRefresherTestProvider([]int64{101})
	provider.currentErr = errors.New("synthetic current stats failure")
	reporter := &recordingRefreshReporter{}
	options := refresherTestOptions([]string{"UTC", "Asia/Shanghai"}, func() time.Time { return now }, "current-failure")
	options.Reporter = reporter
	refresher := mustRefresher(t, staticRefresherBindingResolver{binding: refresherBinding(provider)}, cache, options)

	if err := refresher.Refresh(context.Background()); err == nil {
		t.Fatal("Refresh() error = nil")
	}
	for _, timezone := range []string{"UTC", "Asia/Shanghai"} {
		assertNoRefresherManifest(t, cache, timezone, "2026-07-21")
	}
	if got := reporter.last(); got.Outcome != PrewarmRefreshError || got.PlannedLanes != 2 || got.PublishedLanes != 0 {
		t.Fatalf("refresh report = %#v, want error with 2 planned and 0 published", got)
	}
}

func TestRefresherTimezoneFailureKeepsOldLaneAndPublishesOtherLane(t *testing.T) {
	now := time.Date(2026, 7, 21, 8, 0, 0, 0, time.UTC)
	cache, _ := newRedisPrewarmCache(t, func() time.Time { return now })
	old := seedRefresherManifest(t, cache, PrewarmCacheIdentity{
		ProviderID: 7, ProviderVersion: 11, Timezone: "UTC", AnchorDate: "2026-07-21",
	}, now, "old-utc")
	provider := newRefresherTestProvider([]int64{101})
	provider.failTimezone = "UTC"
	reporter := &recordingRefreshReporter{}
	options := refresherTestOptions([]string{"UTC", "Asia/Shanghai"}, func() time.Time { return now }, "partial")
	options.Reporter = reporter
	refresher := mustRefresher(t, staticRefresherBindingResolver{binding: refresherBinding(provider)}, cache, options)

	if err := refresher.Refresh(context.Background()); err == nil {
		t.Fatal("Refresh() error = nil, want partial lane error")
	}
	utc := assertRefresherManifest(t, cache, "UTC", "2026-07-21")
	if utc.Manifest.CurrentStats.Key != old.CurrentStats.Key || utc.Manifest.TodayHour.Key != old.TodayHour.Key {
		t.Fatal("failed UTC lane replaced its old manifest")
	}
	shanghai := assertRefresherManifest(t, cache, "Asia/Shanghai", "2026-07-21")
	if shanghai.Manifest.CurrentStats.Key == old.CurrentStats.Key {
		t.Fatal("successful lane did not publish the cycle's new current stats")
	}
	if got := reporter.last(); got.Outcome != PrewarmRefreshPartial || got.PlannedLanes != 2 || got.PublishedLanes != 1 {
		t.Fatalf("refresh report = %#v, want partial with 2 planned and 1 published", got)
	}
}

func TestRefresherPublishesCompleteLaneBeforeSlowLaneFinishes(t *testing.T) {
	base := time.Date(2026, 7, 21, 8, 0, 0, 0, time.UTC)
	clock := newRefresherTestClock(base)
	cache, _ := newRedisPrewarmCache(t, clock.Now)
	slowGate := newRefresherTrendGate()
	t.Cleanup(slowGate.Release)
	provider := newRefresherTestProvider([]int64{101})
	provider.setTrendGate("UTC", slowGate)
	oldSlow := seedRefresherManifest(t, cache, PrewarmCacheIdentity{
		ProviderID: 7, ProviderVersion: 11, Timezone: "UTC", AnchorDate: "2026-07-21",
	}, base, "slow-old")
	oldFast := seedRefresherManifest(t, cache, PrewarmCacheIdentity{
		ProviderID: 7, ProviderVersion: 11, Timezone: "Asia/Shanghai", AnchorDate: "2026-07-21",
	}, base, "fast-old")
	reporter := &recordingRefreshReporter{}
	options := refresherTestOptions([]string{"UTC", "Asia/Shanghai"}, clock.Now, "slow-lane")
	options.Reporter = reporter
	refresher := mustRefresher(t, staticRefresherBindingResolver{binding: refresherBinding(provider)}, cache, options)
	errCh := make(chan error, 1)
	go func() { errCh <- refresher.Refresh(context.Background()) }()
	slowGate.WaitEntered(t)

	fast := waitRefresherManifestChange(t, cache, "Asia/Shanghai", "2026-07-21", oldFast.CurrentStats.Key)
	clock.Set(base.Add(5 * time.Minute))
	slowGate.Release()
	if err := <-errCh; err == nil {
		t.Fatal("Refresh() error = nil, want slow lane publication-window failure")
	}
	if fast.Manifest.CurrentStats.Key == oldFast.CurrentStats.Key {
		t.Fatal("complete lane retained its old manifest while another lane was blocked")
	}
	slow := readRefresherManifest(t, cache, "UTC", "2026-07-21")
	if slow.Manifest.CurrentStats.Key != oldSlow.CurrentStats.Key {
		t.Fatal("slow lane published after the shared current reference expired")
	}
	if got := reporter.last(); got.Outcome != PrewarmRefreshPartial || got.PublishedLanes != 1 {
		t.Fatalf("refresh report = %#v, want partial with one early publication", got)
	}
}

func TestRefresherProviderVersionFenceBlocksLaterPublicationAfterReversion(t *testing.T) {
	now := time.Date(2026, 7, 21, 8, 0, 0, 0, time.UTC)
	cache, _ := newRedisPrewarmCache(t, func() time.Time { return now })
	provider := newRefresherTestProvider([]int64{101})
	shanghaiGate := newRefresherTrendGate()
	berlinGate := newRefresherTrendGate()
	t.Cleanup(shanghaiGate.Release)
	t.Cleanup(berlinGate.Release)
	provider.setTrendGate("Asia/Shanghai", shanghaiGate)
	provider.setTrendGate("Europe/Berlin", berlinGate)
	timezones := []string{"UTC", "Asia/Shanghai", "Europe/Berlin"}
	old := seedRefresherLanes(t, cache, timezones, now, "version-fence")
	resolver := &sequenceRefresherBindingResolver{bindings: []ProviderBinding{
		refresherBinding(provider),
		refresherBinding(provider),
		{ProviderID: 7, ProviderVersion: 12, Provider: provider},
		refresherBinding(provider),
	}}
	reporter := &recordingRefreshReporter{}
	options := refresherTestOptions(timezones, func() time.Time { return now }, "version-fence")
	options.Reporter = reporter
	refresher := mustRefresher(t, resolver, cache, options)
	errCh := make(chan error, 1)
	go func() { errCh <- refresher.Refresh(context.Background()) }()
	shanghaiGate.WaitEntered(t)
	berlinGate.WaitEntered(t)

	waitRefresherManifestChange(t, cache, "UTC", "2026-07-21", old["UTC"].CurrentStats.Key)
	shanghaiGate.Release()
	waitRefresherResolverCalls(t, resolver, 3)
	berlinGate.Release()
	if err := <-errCh; err == nil {
		t.Fatal("Refresh() error = nil, want provider-version fence")
	}
	assertRefresherCurrentChanged(t, cache, "UTC", old["UTC"].CurrentStats.Key)
	assertRefresherCurrentUnchanged(t, cache, "Asia/Shanghai", old["Asia/Shanghai"].CurrentStats.Key)
	assertRefresherCurrentUnchanged(t, cache, "Europe/Berlin", old["Europe/Berlin"].CurrentStats.Key)
	if got := resolver.callCount(); got != 3 {
		t.Fatalf("provider resolver calls = %d, want initial + success + irreversible mismatch", got)
	}
	if got := reporter.last(); got.Outcome != PrewarmRefreshPartial || got.PublishedLanes != 1 {
		t.Fatalf("refresh report = %#v, want one publication before provider fence", got)
	}
}

func TestRefresherLeaseLossFenceBlocksLaterPublicationAfterTokenRestoration(t *testing.T) {
	now := time.Date(2026, 7, 21, 8, 0, 0, 0, time.UTC)
	cache, server := newRedisPrewarmCache(t, func() time.Time { return now })
	provider := newRefresherTestProvider([]int64{101})
	shanghaiGate := newRefresherTrendGate()
	berlinGate := newRefresherTrendGate()
	t.Cleanup(shanghaiGate.Release)
	t.Cleanup(berlinGate.Release)
	provider.setTrendGate("Asia/Shanghai", shanghaiGate)
	provider.setTrendGate("Europe/Berlin", berlinGate)
	timezones := []string{"UTC", "Asia/Shanghai", "Europe/Berlin"}
	old := seedRefresherLanes(t, cache, timezones, now, "lease-fence")
	publicationStore := &notifyingRefresherPublicationStore{
		BatchStore: cache.store,
		notOwned:   make(chan struct{}),
	}
	cache.store = publicationStore
	token := strings.Repeat("a", 64)
	reporter := &recordingRefreshReporter{}
	options := refresherTestOptions(timezones, func() time.Time { return now }, "lease-fence")
	options.NewToken = func() string { return token }
	options.Reporter = reporter
	refresher := mustRefresher(t, staticRefresherBindingResolver{binding: refresherBinding(provider)}, cache, options)
	errCh := make(chan error, 1)
	go func() { errCh <- refresher.Refresh(context.Background()) }()
	shanghaiGate.WaitEntered(t)
	berlinGate.WaitEntered(t)

	waitRefresherManifestChange(t, cache, "UTC", "2026-07-21", old["UTC"].CurrentStats.Key)
	server.Set(cache.RefreshLeaseKey(), strings.Repeat("f", 64))
	shanghaiGate.Release()
	publicationStore.WaitNotOwned(t)
	server.Set(cache.RefreshLeaseKey(), token)
	berlinGate.Release()
	if err := <-errCh; err == nil {
		t.Fatal("Refresh() error = nil, want refresh-lease fence")
	}
	assertRefresherCurrentChanged(t, cache, "UTC", old["UTC"].CurrentStats.Key)
	assertRefresherCurrentUnchanged(t, cache, "Asia/Shanghai", old["Asia/Shanghai"].CurrentStats.Key)
	assertRefresherCurrentUnchanged(t, cache, "Europe/Berlin", old["Europe/Berlin"].CurrentStats.Key)
	if got := reporter.last(); got.Outcome != PrewarmRefreshPartial || got.PublishedLanes != 1 {
		t.Fatalf("refresh report = %#v, want one publication before lease fence", got)
	}
}

func TestRefresherLeaseContentionAllowsExactlyOneSourceOwner(t *testing.T) {
	now := time.Date(2026, 7, 21, 8, 0, 0, 0, time.UTC)
	cache, _ := newRedisPrewarmCache(t, func() time.Time { return now })
	provider := newRefresherTestProvider([]int64{101})
	provider.directoryEntered = make(chan struct{}, 2)
	provider.directoryRelease = make(chan struct{})
	resolver := staticRefresherBindingResolver{binding: refresherBinding(provider)}
	first := mustRefresher(t, resolver, cache, refresherTestOptions([]string{"UTC"}, func() time.Time { return now }, "first"))
	second := mustRefresher(t, resolver, cache, refresherTestOptions([]string{"UTC"}, func() time.Time { return now }, "second"))
	start := make(chan struct{})
	errs := make(chan error, 2)
	for _, target := range []Refresher{first, second} {
		target := target
		go func() {
			<-start
			errs <- target.Refresh(context.Background())
		}()
	}
	close(start)
	select {
	case <-provider.directoryEntered:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for lease owner source call")
	}
	select {
	case err := <-errs:
		if err != nil {
			t.Fatalf("lease-busy Refresh() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("lease contender did not return while source owner was held")
	}
	if got := provider.directoryCount(); got != 1 {
		t.Fatalf("directory calls before owner release = %d, want exactly one", got)
	}
	close(provider.directoryRelease)
	if err := <-errs; err != nil {
		t.Fatalf("lease-owner Refresh() error = %v", err)
	}
	if got := provider.directoryCount(); got != 1 {
		t.Fatalf("directory calls = %d, want exactly one source owner", got)
	}
}

func TestRefresherLeaseExpiryTokenReplacementCancellationAndProviderChangeBlockPublication(t *testing.T) {
	tests := []struct {
		name     string
		resolver func(*refresherTestProvider) PrimaryProviderBindingResolver
		mutate   func(*testing.T, context.CancelFunc, *PrewarmCache, *miniredis.Miniredis, *refresherTestProvider)
	}{
		{
			name: "lease expiry",
			resolver: func(provider *refresherTestProvider) PrimaryProviderBindingResolver {
				return staticRefresherBindingResolver{binding: refresherBinding(provider)}
			},
			mutate: func(t *testing.T, _ context.CancelFunc, _ *PrewarmCache, server *miniredis.Miniredis, provider *refresherTestProvider) {
				waitRefresherDirectory(t, provider)
				server.FastForward(6*time.Minute + time.Second)
				close(provider.directoryRelease)
			},
		},
		{
			name: "token replacement",
			resolver: func(provider *refresherTestProvider) PrimaryProviderBindingResolver {
				return staticRefresherBindingResolver{binding: refresherBinding(provider)}
			},
			mutate: func(t *testing.T, _ context.CancelFunc, cache *PrewarmCache, server *miniredis.Miniredis, provider *refresherTestProvider) {
				waitRefresherDirectory(t, provider)
				server.Set(cache.RefreshLeaseKey(), strings.Repeat("f", 64))
				close(provider.directoryRelease)
			},
		},
		{
			name: "cancellation",
			resolver: func(provider *refresherTestProvider) PrimaryProviderBindingResolver {
				return staticRefresherBindingResolver{binding: refresherBinding(provider)}
			},
			mutate: func(t *testing.T, cancel context.CancelFunc, _ *PrewarmCache, _ *miniredis.Miniredis, provider *refresherTestProvider) {
				waitRefresherDirectory(t, provider)
				cancel()
			},
		},
		{
			name: "provider change",
			resolver: func(provider *refresherTestProvider) PrimaryProviderBindingResolver {
				return &sequenceRefresherBindingResolver{bindings: []ProviderBinding{
					refresherBinding(provider),
					{ProviderID: 7, ProviderVersion: 12, Provider: provider},
				}}
			},
			mutate: func(t *testing.T, _ context.CancelFunc, _ *PrewarmCache, _ *miniredis.Miniredis, provider *refresherTestProvider) {
				waitRefresherDirectory(t, provider)
				close(provider.directoryRelease)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			now := time.Date(2026, 7, 21, 8, 0, 0, 0, time.UTC)
			cache, server := newRedisPrewarmCache(t, func() time.Time { return now })
			provider := newRefresherTestProvider([]int64{101})
			provider.directoryEntered = make(chan struct{}, 1)
			provider.directoryRelease = make(chan struct{})
			refresher := mustRefresher(t, test.resolver(provider), cache,
				refresherTestOptions([]string{"UTC"}, func() time.Time { return now }, test.name))
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			errCh := make(chan error, 1)
			go func() { errCh <- refresher.Refresh(ctx) }()
			test.mutate(t, cancel, cache, server, provider)
			if err := <-errCh; err == nil {
				t.Fatal("Refresh() error = nil, want publication blocked")
			}
			assertNoRefresherManifest(t, cache, "UTC", "2026-07-21")
		})
	}
}

func TestRefresherUsesAtMostTwoConcurrentSourceCalls(t *testing.T) {
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	cache, _ := newRedisPrewarmCache(t, func() time.Time { return now })
	provider := newRefresherTestProvider([]int64{101})
	provider.trendRelease = make(chan struct{})
	provider.twoConcurrent = make(chan struct{}, 1)
	refresher := mustRefresher(t, staticRefresherBindingResolver{binding: refresherBinding(provider)}, cache,
		refresherTestOptions(DefaultPrewarmTimezones(), func() time.Time { return now }, "concurrency"))
	errCh := make(chan error, 1)
	go func() { errCh <- refresher.Refresh(context.Background()) }()
	select {
	case <-provider.twoConcurrent:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for two concurrent source calls")
	}
	if got := provider.maxConcurrency(); got != 2 {
		t.Fatalf("maximum source concurrency while blocked = %d, want 2", got)
	}
	close(provider.trendRelease)
	if err := <-errCh; err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if got := provider.maxConcurrency(); got > 2 {
		t.Fatalf("maximum source concurrency = %d, want at most 2", got)
	}
}

func TestRefresherRefreshDoesNotOverlapOrPersistStateBetweenCalls(t *testing.T) {
	now := time.Date(2026, 7, 21, 8, 0, 0, 0, time.UTC)
	cache, _ := newRedisPrewarmCache(t, func() time.Time { return now })
	provider := newRefresherTestProvider([]int64{101})
	provider.directoryEntered = make(chan struct{}, 2)
	provider.directoryRelease = make(chan struct{})
	refresher := mustRefresher(t, staticRefresherBindingResolver{binding: refresherBinding(provider)}, cache,
		refresherTestOptions([]string{"UTC"}, func() time.Time { return now }, "serial"))
	firstErr := make(chan error, 1)
	go func() { firstErr <- refresher.Refresh(context.Background()) }()
	waitRefresherDirectory(t, provider)
	if err := refresher.Refresh(context.Background()); err != nil {
		t.Fatalf("overlapping Refresh() error = %v", err)
	}
	if got := provider.directoryCount(); got != 1 {
		t.Fatalf("overlapping directory calls = %d, want 1", got)
	}
	close(provider.directoryRelease)
	if err := <-firstErr; err != nil {
		t.Fatalf("first Refresh() error = %v", err)
	}
	provider.clearDirectoryBlock()
	if err := refresher.Refresh(context.Background()); err != nil {
		t.Fatalf("later Refresh() error = %v", err)
	}
	if got := provider.directoryCount(); got != 2 {
		t.Fatalf("directory calls after later refresh = %d, want 2", got)
	}
	if got := provider.trendCount(SegmentHistory29d); got != 1 {
		t.Fatalf("history_29d calls = %d, want Redis-derived reuse on later call", got)
	}
	if got := provider.trendCount(SegmentTodayHour); got != 2 {
		t.Fatalf("today_hour calls = %d, want one per owned cycle", got)
	}
}

type recordingRefreshReporter struct {
	mu      sync.Mutex
	reports []RefreshReport
}

func (r *recordingRefreshReporter) ReportRefresh(report RefreshReport) {
	r.mu.Lock()
	defer r.mu.Unlock()
	copyReport := report
	copyReport.SourceCounts = make(map[PrewarmSourceClass]int, len(report.SourceCounts))
	for class, count := range report.SourceCounts {
		copyReport.SourceCounts[class] = count
	}
	r.reports = append(r.reports, copyReport)
}

func (r *recordingRefreshReporter) last() RefreshReport {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.reports) == 0 {
		return RefreshReport{}
	}
	return r.reports[len(r.reports)-1]
}

type staticRefresherBindingResolver struct {
	binding ProviderBinding
	err     error
}

func (r staticRefresherBindingResolver) ResolvePrimaryProviderBinding(context.Context) (ProviderBinding, error) {
	return r.binding, r.err
}

type sequenceRefresherBindingResolver struct {
	mu       sync.Mutex
	bindings []ProviderBinding
	calls    int
}

func (r *sequenceRefresherBindingResolver) ResolvePrimaryProviderBinding(context.Context) (ProviderBinding, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	index := r.calls
	if index >= len(r.bindings) {
		index = len(r.bindings) - 1
	}
	r.calls++
	return r.bindings[index], nil
}

func (r *sequenceRefresherBindingResolver) callCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls
}

type refresherTrendGate struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func newRefresherTrendGate() *refresherTrendGate {
	return &refresherTrendGate{entered: make(chan struct{}, 1), release: make(chan struct{})}
}

func (g *refresherTrendGate) WaitEntered(t *testing.T) {
	t.Helper()
	select {
	case <-g.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for gated trend source call")
	}
}

func (g *refresherTrendGate) Release() {
	g.once.Do(func() { close(g.release) })
}

type notifyingRefresherPublicationStore struct {
	readcache.BatchStore
	notOwned chan struct{}
}

func (s *notifyingRefresherPublicationStore) SetIfLeasesOwned(
	ctx context.Context,
	leaseKeys, tokens []string,
	key string,
	value []byte,
	ttl time.Duration,
) (bool, error) {
	published, err := s.BatchStore.SetIfLeasesOwned(ctx, leaseKeys, tokens, key, value, ttl)
	if err == nil && !published {
		s.notOwned <- struct{}{}
	}
	return published, err
}

func (s *notifyingRefresherPublicationStore) WaitNotOwned(t *testing.T) {
	t.Helper()
	select {
	case <-s.notOwned:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for lost-lease publication rejection")
	}
}

type refresherTestClock struct {
	mu  sync.Mutex
	now time.Time
}

func newRefresherTestClock(now time.Time) *refresherTestClock {
	return &refresherTestClock{now: now}
}

func (c *refresherTestClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *refresherTestClock) Set(now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = now
}

type refresherTestProvider struct {
	relay.Provider

	mu               sync.Mutex
	ids              []int64
	directoryCalls   int
	currentCalls     int
	trendCalls       map[PrewarmSegmentClass]int
	currentErr       error
	failTimezone     string
	directoryEntered chan struct{}
	directoryRelease chan struct{}
	trendRelease     chan struct{}
	twoConcurrent    chan struct{}
	trendGates       map[string]*refresherTrendGate
	activeCalls      int
	maxActiveCalls   int
}

func newRefresherTestProvider(ids []int64) *refresherTestProvider {
	return &refresherTestProvider{
		ids: append([]int64(nil), ids...), trendCalls: make(map[PrewarmSegmentClass]int),
		trendGates: make(map[string]*refresherTrendGate),
	}
}

func (p *refresherTestProvider) GetProviderUserIDs(ctx context.Context) (relay.ProviderDirectoryResult, error) {
	p.beginCall()
	defer p.endCall()
	p.mu.Lock()
	p.directoryCalls++
	ids := append([]int64(nil), p.ids...)
	entered, release := p.directoryEntered, p.directoryRelease
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
	return relay.ProviderDirectoryResult{UserIDs: ids, ResponseBytes: 32, PageCount: 1}, nil
}

func (p *refresherTestProvider) GetProviderCurrentUsageStats(_ context.Context, ids []int64) (relay.ProviderCurrentStatsResult, error) {
	p.beginCall()
	defer p.endCall()
	p.mu.Lock()
	p.currentCalls++
	err := p.currentErr
	p.mu.Unlock()
	if err != nil {
		return relay.ProviderCurrentStatsResult{}, err
	}
	stats := make(map[int64]relay.TeamUserUsageStats, len(ids))
	for _, id := range ids {
		tokens := id * 10
		stats[id] = relay.TeamUserUsageStats{
			UserID: id, TodayActualCost: 1, TotalActualCost: 2, TotalTokens: &tokens,
		}
	}
	return relay.ProviderCurrentStatsResult{Stats: stats, ResponseBytes: 64}, nil
}

func (p *refresherTestProvider) GetProviderUsageTrend(ctx context.Context, params relay.TeamMemberTrendParams, _ int) (relay.ProviderWideTrendResult, error) {
	p.beginCall()
	defer p.endCall()
	class := refresherClassForCoverage(params)
	p.mu.Lock()
	p.trendCalls[class]++
	fail := p.failTimezone == params.Timezone
	release := p.trendRelease
	gate := p.trendGates[params.Timezone]
	p.mu.Unlock()
	if gate != nil {
		select {
		case gate.entered <- struct{}{}:
		default:
		}
		select {
		case <-gate.release:
		case <-ctx.Done():
			return relay.ProviderWideTrendResult{}, ctx.Err()
		}
	}
	if release != nil {
		select {
		case <-release:
		case <-ctx.Done():
			return relay.ProviderWideTrendResult{}, ctx.Err()
		}
	}
	if fail {
		return relay.ProviderWideTrendResult{}, fmt.Errorf("synthetic %s source failure", class)
	}
	label := params.StartDate
	if params.Granularity == "hour" {
		label += " 00:00"
	}
	tokens := int64(10)
	return relay.ProviderWideTrendResult{
		Points:   []relay.ProviderWideTrendPoint{{UserID: 101, Date: label, ActualCost: 1, TotalTokens: &tokens}},
		Coverage: params, ResponseBytes: 64, PointCount: 1, UniqueUserCount: 1, Complete: true,
	}, nil
}

func (p *refresherTestProvider) setTrendGate(timezone string, gate *refresherTrendGate) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.trendGates[timezone] = gate
}

func (p *refresherTestProvider) beginCall() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.activeCalls++
	if p.activeCalls > p.maxActiveCalls {
		p.maxActiveCalls = p.activeCalls
		if p.maxActiveCalls == 2 && p.twoConcurrent != nil {
			select {
			case p.twoConcurrent <- struct{}{}:
			default:
			}
		}
	}
}

func (p *refresherTestProvider) endCall() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.activeCalls--
}

func (p *refresherTestProvider) directoryCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.directoryCalls
}

func (p *refresherTestProvider) currentCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.currentCalls
}

func (p *refresherTestProvider) trendCount(class PrewarmSegmentClass) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.trendCalls[class]
}

func (p *refresherTestProvider) maxConcurrency() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.maxActiveCalls
}

func (p *refresherTestProvider) clearDirectoryBlock() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.directoryEntered = nil
	p.directoryRelease = nil
}

func refresherClassForCoverage(params relay.TeamMemberTrendParams) PrewarmSegmentClass {
	if params.Granularity == "hour" {
		return SegmentTodayHour
	}
	start, _ := time.Parse(time.DateOnly, params.StartDate)
	end, _ := time.Parse(time.DateOnly, params.EndDate)
	if end.Sub(start) >= 28*24*time.Hour {
		return SegmentHistory29d
	}
	return SegmentHistory6d
}

func refresherBinding(provider relay.Provider) ProviderBinding {
	return ProviderBinding{ProviderID: 7, ProviderVersion: 11, Provider: provider}
}

func refresherTestOptions(timezones []string, now func() time.Time, seed string) RefresherOptions {
	return RefresherOptions{
		Timezones: timezones, Now: now,
		NewToken:        newRefresherIDFactory(seed + "-token"),
		NewGenerationID: newRefresherIDFactory(seed + "-generation"),
		CycleTimeout:    5 * time.Second,
		SourceTimeout:   2 * time.Second,
	}
}

func newRefresherIDFactory(seed string) func() string {
	var sequence atomic.Uint64
	return func() string {
		value := sha256.Sum256([]byte(fmt.Sprintf("%s-%d", seed, sequence.Add(1))))
		return fmt.Sprintf("%x", value[:])
	}
}

func mustRefresher(
	t *testing.T,
	resolver PrimaryProviderBindingResolver,
	cache *PrewarmCache,
	options RefresherOptions,
) Refresher {
	t.Helper()
	refresher, err := NewRefresher(resolver, cache, options)
	if err != nil {
		t.Fatalf("NewRefresher() error = %v", err)
	}
	return refresher
}

func seedRefresherManifest(
	t *testing.T,
	cache *PrewarmCache,
	identity PrewarmCacheIdentity,
	generatedAt time.Time,
	seed string,
) PrewarmManifest {
	t.Helper()
	currentValue := testPrewarmCurrentStats(generatedAt, refresherHexRune(seed, 0))
	currentValue.ProviderID = identity.ProviderID
	currentValue.ProviderVersion = identity.ProviderVersion
	current, err := cache.WriteCurrentStats(context.Background(), currentValue)
	if err != nil {
		t.Fatalf("WriteCurrentStats(seed) error = %v", err)
	}
	refs := make(map[PrewarmSegmentClass]PrewarmValueReference, 3)
	for index, class := range []PrewarmSegmentClass{SegmentHistory29d, SegmentHistory6d, SegmentTodayHour} {
		segment := testPrewarmSegment(t, identity, generatedAt, class, refresherHexRune(seed, index+1))
		refs[class], err = cache.WriteSegment(context.Background(), segment)
		if err != nil {
			t.Fatalf("WriteSegment(seed %s) error = %v", class, err)
		}
	}
	manifest := PrewarmManifest{
		SchemaVersion: prewarmCacheSchemaVersion, ProviderID: identity.ProviderID, ProviderVersion: identity.ProviderVersion,
		Timezone: identity.Timezone, TimezoneDigest: prewarmTimezoneDigest(identity.Timezone), AnchorDate: identity.AnchorDate,
		CreatedAt: generatedAt, CurrentStats: current, History29d: refs[SegmentHistory29d],
		History6d: refs[SegmentHistory6d], TodayHour: refs[SegmentTodayHour],
	}
	token := newRefresherIDFactory(seed + "-publish")()
	leaseKey := cache.RefreshLeaseKey()
	owned, err := cache.TryAcquireLease(context.Background(), leaseKey, token, 6*time.Minute)
	if err != nil || !owned {
		t.Fatalf("TryAcquireLease(seed) = %v, %v", owned, err)
	}
	published, err := cache.PublishManifest(context.Background(), leaseKey, token, manifest)
	if err != nil || !published {
		t.Fatalf("PublishManifest(seed) = %v, %v", published, err)
	}
	if _, err := cache.ReleaseLease(context.Background(), leaseKey, token); err != nil {
		t.Fatalf("ReleaseLease(seed) error = %v", err)
	}
	return manifest
}

func seedRefresherLanes(
	t *testing.T,
	cache *PrewarmCache,
	timezones []string,
	generatedAt time.Time,
	seed string,
) map[string]PrewarmManifest {
	t.Helper()
	manifests := make(map[string]PrewarmManifest, len(timezones))
	for index, timezone := range timezones {
		anchorDate, err := refresherLocalAnchorDate(timezone, generatedAt)
		if err != nil {
			t.Fatalf("refresherLocalAnchorDate(%s) error = %v", timezone, err)
		}
		manifests[timezone] = seedRefresherManifest(t, cache, PrewarmCacheIdentity{
			ProviderID: 7, ProviderVersion: 11, Timezone: timezone, AnchorDate: anchorDate,
		}, generatedAt, fmt.Sprintf("%s-%d", seed, index))
	}
	return manifests
}

func refresherHexRune(seed string, offset int) string {
	value := sha256.Sum256([]byte(fmt.Sprintf("%s-%d", seed, offset)))
	return fmt.Sprintf("%x", value[:1])[:1]
}

func assertRefresherManifest(t *testing.T, cache *PrewarmCache, timezone, anchorDate string) *PrewarmCacheResult {
	t.Helper()
	result, found, err := cache.Read(context.Background(), PrewarmCacheIdentity{
		ProviderID: 7, ProviderVersion: 11, Timezone: timezone, AnchorDate: anchorDate,
	})
	if err != nil || !found || result == nil || !result.Complete {
		t.Fatalf("Read(%s/%s) = %#v, %v, %v, want complete manifest", timezone, anchorDate, result, found, err)
	}
	return result
}

func readRefresherManifest(t *testing.T, cache *PrewarmCache, timezone, anchorDate string) *PrewarmCacheResult {
	t.Helper()
	result, found, err := cache.Read(context.Background(), PrewarmCacheIdentity{
		ProviderID: 7, ProviderVersion: 11, Timezone: timezone, AnchorDate: anchorDate,
	})
	if err != nil || !found || result == nil {
		t.Fatalf("Read(%s/%s) = %#v, %v, %v, want manifest", timezone, anchorDate, result, found, err)
	}
	return result
}

func waitRefresherManifestChange(
	t *testing.T,
	cache *PrewarmCache,
	timezone, anchorDate, oldCurrentKey string,
) *PrewarmCacheResult {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		result, found, err := cache.Read(context.Background(), PrewarmCacheIdentity{
			ProviderID: 7, ProviderVersion: 11, Timezone: timezone, AnchorDate: anchorDate,
		})
		if err != nil {
			t.Fatalf("Read(%s/%s) error = %v", timezone, anchorDate, err)
		}
		if found && result != nil && result.Manifest.CurrentStats.Key != oldCurrentKey {
			return result
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timezone %s did not publish a new manifest while another lane was blocked", timezone)
	return nil
}

func assertRefresherCurrentChanged(
	t *testing.T,
	cache *PrewarmCache,
	timezone, oldCurrentKey string,
) {
	t.Helper()
	result := readRefresherManifest(t, cache, timezone, "2026-07-21")
	if result.Manifest.CurrentStats.Key == oldCurrentKey {
		t.Fatalf("timezone %s retained its old current reference", timezone)
	}
}

func assertRefresherCurrentUnchanged(
	t *testing.T,
	cache *PrewarmCache,
	timezone, oldCurrentKey string,
) {
	t.Helper()
	result := readRefresherManifest(t, cache, timezone, "2026-07-21")
	if result.Manifest.CurrentStats.Key != oldCurrentKey {
		t.Fatalf("timezone %s published after an irreversible cycle fence", timezone)
	}
}

func waitRefresherResolverCalls(t *testing.T, resolver *sequenceRefresherBindingResolver, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if resolver.callCount() >= want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("provider resolver calls = %d, want at least %d", resolver.callCount(), want)
}

func assertNoRefresherManifest(t *testing.T, cache *PrewarmCache, timezone, anchorDate string) {
	t.Helper()
	result, found, err := cache.Read(context.Background(), PrewarmCacheIdentity{
		ProviderID: 7, ProviderVersion: 11, Timezone: timezone, AnchorDate: anchorDate,
	})
	if err != nil {
		t.Fatalf("Read(%s/%s) error = %v", timezone, anchorDate, err)
	}
	if found || result != nil {
		t.Fatalf("Read(%s/%s) = %#v, %v, want no manifest", timezone, anchorDate, result, found)
	}
}

func waitRefresherDirectory(t *testing.T, provider *refresherTestProvider) {
	t.Helper()
	select {
	case <-provider.directoryEntered:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for directory source call")
	}
}
