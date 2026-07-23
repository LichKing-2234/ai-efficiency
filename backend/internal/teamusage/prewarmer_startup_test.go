package teamusage

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/alicebob/miniredis/v2"
)

var startupTestTimezones = []string{"UTC", "Asia/Shanghai", "America/Los_Angeles", "Europe/Berlin"}

func TestPrewarmStartupFetchesAtTwoAndPublishesAfterBarrier(t *testing.T) {
	prewarmer, provider, cache := newBlockedFourLaneStartup(t)
	result := make(chan error, 1)
	go func() { result <- prewarmer.runStartup(context.Background()) }()
	finished := false
	t.Cleanup(func() {
		if finished {
			return
		}
		provider.releaseAll()
		select {
		case <-result:
		case <-time.After(5 * time.Second):
		}
	})

	provider.waitForActive(t, 2)
	if got := provider.maxActiveCalls(); got != 2 {
		t.Fatalf("startup source concurrency = %d, want 2", got)
	}
	assertNoStartupManifest(t, cache)

	provider.releaseAll()
	if err := waitStartupResult(t, result); err != nil {
		t.Fatalf("runStartup() error = %v", err)
	}
	finished = true
	assertFourCompleteStartupManifests(t, cache)
	assertOneSharedCurrentReference(t, cache)
}

func TestPrewarmStartupPublishesHealthyLanesAfterOneLaneFailure(t *testing.T) {
	prewarmer, provider, cache := newFourLaneStartup(t, false)
	provider.failTimezone = "Asia/Shanghai"
	provider.failClass = SegmentHistory6d

	err := prewarmer.runStartup(context.Background())
	if err == nil || !strings.Contains(err.Error(), "synthetic startup lane source failure") {
		t.Fatalf("runStartup() error = %v, want joined lane source failure", err)
	}
	for _, timezone := range startupTestTimezones {
		result, found, readErr := cache.Read(context.Background(), startupTestIdentity(timezone))
		if readErr != nil {
			t.Fatalf("Read(%s) error = %v", timezone, readErr)
		}
		if timezone == provider.failTimezone {
			if found {
				t.Fatalf("failed startup lane %s published manifest %#v", timezone, result.Manifest)
			}
			continue
		}
		if !found || !result.Complete {
			t.Fatalf("healthy startup lane %s = found:%v complete:%v", timezone, found, found && result.Complete)
		}
	}
	if provider.server.Exists(startupCoordinatorKey(prewarmer)) {
		t.Fatal("failed startup retained six-minute coordinator marker")
	}
}

func TestPrewarmStartupCoordinatorLossPreventsBarrierPublication(t *testing.T) {
	prewarmer, provider, cache := newBlockedFourLaneStartup(t)
	result := make(chan error, 1)
	go func() { result <- prewarmer.runStartup(context.Background()) }()

	provider.waitForActive(t, 2)
	provider.server.Del(startupCoordinatorKey(prewarmer))
	provider.releaseAll()
	if err := waitStartupResult(t, result); err == nil {
		t.Fatal("runStartup() error = nil, want lost coordinator")
	}
	assertNoStartupManifest(t, cache)
}

func TestPrewarmStartupCancellationDrainsTasksAndReleasesLeases(t *testing.T) {
	prewarmer, provider, _ := newBlockedFourLaneStartup(t)
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- prewarmer.runStartup(ctx) }()

	provider.waitForActive(t, 2)
	cancel()
	if err := waitStartupResult(t, result); !errors.Is(err, context.Canceled) {
		t.Fatalf("runStartup() error = %v, want context canceled", err)
	}
	provider.waitForIdle(t)
	assertNoStartupSegmentLeases(t, prewarmer, provider.server)
	if provider.server.Exists(startupCoordinatorKey(prewarmer)) {
		t.Fatal("canceled startup retained coordinator marker")
	}
}

func TestPrewarmStartupBatchRetainsNoCompletedResult(t *testing.T) {
	prewarmer, provider, _ := newFourLaneStartup(t, false)
	if err := prewarmer.runStartup(context.Background()); err != nil {
		t.Fatalf("runStartup() error = %v", err)
	}
	provider.waitForIdle(t)
	if got := provider.completedCalls(); got != len(startupTestTimezones)*3 {
		t.Fatalf("completed startup source calls = %d, want %d", got, len(startupTestTimezones)*3)
	}

	prewarmerType := reflect.TypeOf(*prewarmer)
	for index := 0; index < prewarmerType.NumField(); index++ {
		fieldType := prewarmerType.Field(index).Type.String()
		for _, completedType := range []string{
			"startupLanePlan", "startupSegmentResult", "PrewarmCurrentStatsEnvelope", "PrewarmTrendSegment",
		} {
			if strings.Contains(fieldType, completedType) {
				t.Fatalf("Prewarmer field %s retains completed startup type %s", prewarmerType.Field(index).Name, fieldType)
			}
		}
	}
}

type blockedStartupProvider struct {
	*lifecycleProvider

	server *miniredis.Miniredis

	mu            sync.Mutex
	active        int
	maximumActive int
	calls         int
	completed     int
	entered       chan struct{}
	release       chan struct{}
	releaseOnce   sync.Once
	failTimezone  string
	failClass     PrewarmSegmentClass
}

func (p *blockedStartupProvider) GetProviderUsageTrend(
	ctx context.Context,
	params relay.TeamMemberTrendParams,
	limit int,
) (relay.ProviderWideTrendResult, error) {
	p.mu.Lock()
	p.active++
	p.calls++
	if p.active > p.maximumActive {
		p.maximumActive = p.active
	}
	p.mu.Unlock()
	p.entered <- struct{}{}
	defer func() {
		p.mu.Lock()
		p.active--
		p.completed++
		p.mu.Unlock()
	}()

	select {
	case <-p.release:
	case <-ctx.Done():
		return relay.ProviderWideTrendResult{}, ctx.Err()
	}
	class := classForCoverage(params)
	if params.Timezone == p.failTimezone && class == p.failClass {
		return relay.ProviderWideTrendResult{}, fmt.Errorf("synthetic startup lane source failure")
	}
	return p.lifecycleProvider.GetProviderUsageTrend(ctx, params, limit)
}

func (p *blockedStartupProvider) waitForActive(t *testing.T, want int) {
	t.Helper()
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	for {
		p.mu.Lock()
		active, maximum := p.active, p.maximumActive
		p.mu.Unlock()
		if active >= want {
			return
		}
		select {
		case <-p.entered:
		case <-deadline.C:
			t.Fatalf("startup active source calls = %d, maximum %d, want %d", active, maximum, want)
		}
	}
}

func (p *blockedStartupProvider) waitForIdle(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		p.mu.Lock()
		active := p.active
		p.mu.Unlock()
		if active == 0 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("startup provider workers did not become idle")
}

func (p *blockedStartupProvider) releaseAll() {
	p.releaseOnce.Do(func() { close(p.release) })
}

func (p *blockedStartupProvider) maxActiveCalls() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.maximumActive
}

func (p *blockedStartupProvider) completedCalls() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.completed
}

func newBlockedFourLaneStartup(t *testing.T) (*Prewarmer, *blockedStartupProvider, *PrewarmCache) {
	return newFourLaneStartup(t, true)
}

func newFourLaneStartup(t *testing.T, blocked bool) (*Prewarmer, *blockedStartupProvider, *PrewarmCache) {
	t.Helper()
	now := time.Date(2026, 7, 21, 8, 0, 0, 0, time.UTC)
	cache, server := newRedisPrewarmCache(t, func() time.Time { return now })
	provider := &blockedStartupProvider{
		lifecycleProvider: newLifecycleProvider([]int64{101}),
		server:            server,
		entered:           make(chan struct{}, len(startupTestTimezones)*3),
		release:           make(chan struct{}),
	}
	if !blocked {
		provider.releaseAll()
	}
	t.Cleanup(provider.releaseAll)
	options := lifecycleOptions(startupTestTimezones, func() time.Time { return now })
	prewarmer := mustPrewarmer(t, staticBindingResolver{binding: prewarmBinding(provider)}, cache, options)
	return prewarmer, provider, cache
}

func startupTestIdentity(timezone string) PrewarmCacheIdentity {
	return PrewarmCacheIdentity{
		ProviderID: 7, ProviderVersion: 11, Timezone: timezone, AnchorDate: "2026-07-21",
	}
}

func assertNoStartupManifest(t *testing.T, cache *PrewarmCache) {
	t.Helper()
	for _, timezone := range startupTestTimezones {
		if result, found, err := cache.Read(context.Background(), startupTestIdentity(timezone)); err != nil {
			t.Fatalf("Read(%s) error = %v", timezone, err)
		} else if found {
			t.Fatalf("startup manifest published before barrier for %s: %#v", timezone, result.Manifest)
		}
	}
}

func assertFourCompleteStartupManifests(t *testing.T, cache *PrewarmCache) {
	t.Helper()
	for _, timezone := range startupTestTimezones {
		result, found, err := cache.Read(context.Background(), startupTestIdentity(timezone))
		if err != nil || !found || !result.Complete {
			t.Fatalf("startup manifest %s = found:%v complete:%v error:%v", timezone, found, found && result.Complete, err)
		}
	}
}

func assertOneSharedCurrentReference(t *testing.T, cache *PrewarmCache) {
	t.Helper()
	var key, generationID string
	for _, timezone := range startupTestTimezones {
		result, found, err := cache.Read(context.Background(), startupTestIdentity(timezone))
		if err != nil || !found {
			t.Fatalf("Read(%s) = found:%v error:%v", timezone, found, err)
		}
		if key == "" {
			key, generationID = result.Manifest.CurrentStats.Key, result.Manifest.CurrentStats.GenerationID
			continue
		}
		if result.Manifest.CurrentStats.Key != key || result.Manifest.CurrentStats.GenerationID != generationID {
			t.Fatalf("startup current reference for %s differs from shared %s/%s", timezone, key, generationID)
		}
	}
}

func startupCoordinatorKey(prewarmer *Prewarmer) string {
	return prewarmer.cache.LeaseKey("startup-coordinator", "7", "11", prewarmer.allowlistDigest)
}

func assertNoStartupSegmentLeases(t *testing.T, prewarmer *Prewarmer, server *miniredis.Miniredis) {
	t.Helper()
	binding := ProviderBinding{ProviderID: 7, ProviderVersion: 11}
	for _, timezone := range startupTestTimezones {
		for _, class := range []PrewarmSegmentClass{SegmentHistory29d, SegmentHistory6d, SegmentTodayHour} {
			key := prewarmer.segmentLeaseKey(binding, timezone, "2026-07-21", startupTestRefreshClass(class))
			if server.Exists(key) {
				t.Fatalf("startup segment lease retained for %s/%s", timezone, class)
			}
		}
	}
}

func startupTestRefreshClass(class PrewarmSegmentClass) string {
	if class == SegmentTodayHour {
		return prewarmMovingRefreshClass
	}
	return string(class)
}

func waitStartupResult(t *testing.T, result <-chan error) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("runStartup() did not return")
		return nil
	}
}
