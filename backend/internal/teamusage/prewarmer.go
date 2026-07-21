package teamusage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ai-efficiency/backend/internal/readcache"
)

const (
	prewarmMovingInterval          = 60 * time.Second
	prewarmMovingCoordinatorTTL    = 3 * time.Minute
	prewarmHistoryCoordinatorTTL   = 6 * time.Minute
	prewarmWorkerLeaseTTL          = 90 * time.Second
	prewarmMovingWorkerTimeout     = 175 * time.Second
	prewarmHistoricalWorkerTimeout = 355 * time.Second
	prewarmStartupWorkerTimeout    = 355 * time.Second
	prewarmSegmentWorkerTimeout    = 85 * time.Second
	prewarmSourceCallTimeout       = 80 * time.Second
	prewarmHistoricalJitterMax     = 30 * time.Minute
	prewarmSourceSlotCount         = 2
	prewarmSourceSlotPoll          = 10 * time.Millisecond
)

var errPrewarmLeaseLost = errors.New("team usage prewarm lease ownership was lost")
var errPrewarmLeaseBusy = errors.New("team usage prewarm lease is owned by another worker")

type PrewarmerOptions struct {
	Timezones       []string
	Now             func() time.Time
	NewToken        func() string
	NewGenerationID func() string
	Sleep           func(context.Context, time.Duration) error

	// tick is test-only; runtime uses the fixed 60-second ticker.
	tick                    <-chan time.Time
	sourceCallTimeout       time.Duration
	movingWorkerTimeout     time.Duration
	historicalWorkerTimeout time.Duration
	startupWorkerTimeout    time.Duration
	segmentWorkerTimeout    time.Duration
}

type prewarmerLifecycleState uint8

const (
	prewarmerStopped prewarmerLifecycleState = iota
	prewarmerRunning
	prewarmerStopping
)

type Prewarmer struct {
	resolver PrimaryProviderBindingResolver
	cache    *PrewarmCache
	source   *PrewarmSource
	limiter  SourceCallLimiter
	options  PrewarmerOptions

	timezones       []string
	allowlistDigest string
	moving          atomic.Bool

	lifecycleMu sync.Mutex
	state       prewarmerLifecycleState
	cancel      context.CancelFunc
	stopDone    chan struct{}
	wg          sync.WaitGroup
}

func NewPrewarmer(
	resolver PrimaryProviderBindingResolver,
	cache *PrewarmCache,
	options PrewarmerOptions,
) (*Prewarmer, error) {
	if resolver == nil {
		return nil, fmt.Errorf("team usage primary provider binding resolver is required")
	}
	if cache == nil {
		return nil, fmt.Errorf("team usage prewarm cache is required")
	}
	timezones, err := NormalizePrewarmTimezones(options.Timezones)
	if err != nil {
		return nil, err
	}
	if len(timezones) == 0 {
		return nil, fmt.Errorf("at least one team usage prewarm timezone is required")
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.NewToken == nil {
		options.NewToken = newPrewarmRandomID
	}
	if options.NewGenerationID == nil {
		options.NewGenerationID = newPrewarmRandomID
	}
	if options.Sleep == nil {
		options.Sleep = readcache.Sleep
	}
	if options.sourceCallTimeout <= 0 || options.sourceCallTimeout >= prewarmWorkerLeaseTTL {
		options.sourceCallTimeout = prewarmSourceCallTimeout
	}
	if options.movingWorkerTimeout <= 0 || options.movingWorkerTimeout >= prewarmMovingCoordinatorTTL {
		options.movingWorkerTimeout = prewarmMovingWorkerTimeout
	}
	if options.historicalWorkerTimeout <= 0 || options.historicalWorkerTimeout >= prewarmHistoryCoordinatorTTL {
		options.historicalWorkerTimeout = prewarmHistoricalWorkerTimeout
	}
	if options.startupWorkerTimeout <= 0 || options.startupWorkerTimeout >= prewarmHistoryCoordinatorTTL {
		options.startupWorkerTimeout = prewarmStartupWorkerTimeout
	}
	if options.segmentWorkerTimeout <= 0 || options.segmentWorkerTimeout >= prewarmWorkerLeaseTTL {
		options.segmentWorkerTimeout = prewarmSegmentWorkerTimeout
	}
	limiter := &distributedSourceCallLimiter{
		cache: cache, semaphore: make(chan struct{}, prewarmSourceSlotCount),
		newToken: options.NewToken, sleep: options.Sleep, callTimeout: options.sourceCallTimeout,
	}
	source, err := NewPrewarmSource(limiter, PrewarmSourceOptions{
		Now: options.Now, NewGenerationID: options.NewGenerationID,
	})
	if err != nil {
		return nil, err
	}
	return &Prewarmer{
		resolver: resolver, cache: cache, source: source, limiter: limiter, options: options,
		timezones: timezones, allowlistDigest: prewarmStringDigest(timezones...),
	}, nil
}

func (p *Prewarmer) SourceCallLimiter() SourceCallLimiter {
	if p == nil {
		return nil
	}
	return p.limiter
}

func (p *Prewarmer) Start(ctx context.Context) {
	if p == nil || ctx == nil {
		return
	}
	p.lifecycleMu.Lock()
	if p.state != prewarmerStopped {
		p.lifecycleMu.Unlock()
		return
	}
	runCtx, cancel := context.WithCancel(ctx)
	p.state = prewarmerRunning
	p.cancel = cancel
	p.stopDone = make(chan struct{})
	p.wg.Add(1)
	p.lifecycleMu.Unlock()

	go p.runLifecycle(runCtx)
}

func (p *Prewarmer) Stop() {
	if p == nil {
		return
	}
	p.lifecycleMu.Lock()
	switch p.state {
	case prewarmerStopped:
		p.lifecycleMu.Unlock()
		return
	case prewarmerStopping:
		done := p.stopDone
		p.lifecycleMu.Unlock()
		<-done
		return
	}
	p.state = prewarmerStopping
	cancel := p.cancel
	done := p.stopDone
	p.lifecycleMu.Unlock()
	if cancel != nil {
		cancel()
	}
	p.wg.Wait()
	p.lifecycleMu.Lock()
	p.state = prewarmerStopped
	p.cancel = nil
	close(done)
	p.lifecycleMu.Unlock()
}

func (p *Prewarmer) runLifecycle(ctx context.Context) {
	defer p.wg.Done()
	_ = p.runStartup(ctx)

	if p.options.tick != nil {
		p.runTicks(ctx, p.options.tick)
		return
	}
	ticker := time.NewTicker(prewarmMovingInterval)
	defer ticker.Stop()
	p.runTicks(ctx, ticker.C)
}

func (p *Prewarmer) runTicks(ctx context.Context, ticks <-chan time.Time) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticks:
			p.startMoving(ctx)
			for _, timezone := range p.timezones {
				p.startHistorical(ctx, timezone, p.options.Now())
			}
		}
	}
}

func (p *Prewarmer) startMoving(ctx context.Context) {
	if !p.moving.CompareAndSwap(false, true) {
		return
	}
	if !p.beginLifecycleWorker() {
		p.moving.Store(false)
		return
	}
	go func() {
		defer p.wg.Done()
		defer p.moving.Store(false)
		_ = p.runMoving(ctx)
	}()
}

func (p *Prewarmer) startHistorical(ctx context.Context, timezone string, anchor time.Time) {
	if !p.beginLifecycleWorker() {
		return
	}
	go func() {
		defer p.wg.Done()
		_ = p.RunHistorical(ctx, timezone, anchor)
	}()
}

func (p *Prewarmer) beginLifecycleWorker() bool {
	p.lifecycleMu.Lock()
	defer p.lifecycleMu.Unlock()
	if p.state != prewarmerRunning {
		return false
	}
	p.wg.Add(1)
	return true
}

func (p *Prewarmer) RunMoving(ctx context.Context) error {
	if p == nil {
		return fmt.Errorf("team usage prewarmer is not configured")
	}
	if !p.moving.CompareAndSwap(false, true) {
		return nil
	}
	defer p.moving.Store(false)
	return p.runMoving(ctx)
}

func (p *Prewarmer) runMoving(ctx context.Context) error {
	binding, err := p.resolveBinding(ctx)
	if err != nil {
		return err
	}
	tick := p.options.Now().Truncate(prewarmMovingInterval).UTC().Format(time.RFC3339)
	coordinatorKey := p.cache.LeaseKey(
		"moving-coordinator", strconv.Itoa(binding.ProviderID), strconv.FormatInt(binding.ProviderVersion, 10),
		p.allowlistDigest, tick, "moving",
	)
	coordinatorToken, acquired, err := p.acquireLease(ctx, coordinatorKey, prewarmMovingCoordinatorTTL)
	if err != nil || !acquired {
		return err
	}
	retainCoordinator := false
	defer func() {
		if !retainCoordinator {
			p.releaseLease(coordinatorKey, coordinatorToken)
		}
	}()
	workerCtx, cancel := context.WithTimeout(ctx, p.options.movingWorkerTimeout)
	defer cancel()
	workerCtx = withPrewarmCoordinatorLease(workerCtx, coordinatorKey, coordinatorToken)

	current, err := p.source.BuildCurrentStats(workerCtx, binding)
	if err != nil {
		return err
	}
	if err := p.requireCoordinatorOwned(workerCtx); err != nil {
		return err
	}
	currentRef, err := p.cache.WriteCurrentStats(workerCtx, current)
	if err != nil {
		return fmt.Errorf("write moving current stats: %w", err)
	}
	if err := p.requireCoordinatorOwned(workerCtx); err != nil {
		return err
	}

	var wg sync.WaitGroup
	errorsByTimezone := make(chan error, len(p.timezones))
	for _, timezone := range p.timezones {
		timezone := timezone
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := p.runMovingTimezone(workerCtx, binding, currentRef, timezone); err != nil {
				errorsByTimezone <- fmt.Errorf("moving timezone %s: %w", timezone, err)
			}
		}()
	}
	wg.Wait()
	close(errorsByTimezone)
	var failures []error
	for failure := range errorsByTimezone {
		failures = append(failures, failure)
	}
	if err := workerCtx.Err(); err != nil {
		return errors.Join(append(failures, err)...)
	}
	if err := p.requireCoordinatorOwned(workerCtx); err != nil {
		return errors.Join(append(failures, err)...)
	}
	retainCoordinator = true
	return errors.Join(failures...)
}

func (p *Prewarmer) runMovingTimezone(
	ctx context.Context,
	binding ProviderBinding,
	currentRef PrewarmValueReference,
	timezone string,
) error {
	if err := p.requireCoordinatorOwned(ctx); err != nil {
		return err
	}
	anchorDate, err := prewarmLocalAnchorDate(timezone, p.options.Now())
	if err != nil {
		return err
	}
	safe, err := SplitSafe(timezone, anchorDate)
	if err != nil || !safe {
		return err
	}
	identity := PrewarmCacheIdentity{
		ProviderID: binding.ProviderID, ProviderVersion: binding.ProviderVersion,
		Timezone: timezone, AnchorDate: anchorDate,
	}
	previous, ok, err := p.cache.Read(ctx, identity)
	if err != nil {
		return fmt.Errorf("read previous moving generation: %w", err)
	}
	if !ok || previous.History29dStatus == PrewarmValueMissing || previous.History29dStatus == PrewarmValueHardExpired ||
		previous.History6dStatus == PrewarmValueMissing || previous.History6dStatus == PrewarmValueHardExpired {
		return nil
	}
	leased, err := p.fetchLeasedSegment(ctx, binding, timezone, anchorDate, SegmentTodayHour, "moving")
	if errors.Is(err, errPrewarmLeaseBusy) {
		return nil
	}
	if err != nil {
		return err
	}
	defer p.releaseLeasedReference(leased)
	manifest := PrewarmManifest{
		SchemaVersion: prewarmCacheSchemaVersion, ProviderID: binding.ProviderID, ProviderVersion: binding.ProviderVersion,
		Timezone: timezone, TimezoneDigest: prewarmTimezoneDigest(timezone), AnchorDate: anchorDate,
		CreatedAt: p.options.Now(), CurrentStats: currentRef,
		History29d: previous.Manifest.History29d, History6d: previous.Manifest.History6d, TodayHour: leased.reference,
	}
	return p.publishIfCurrent(leased.ctx, binding, leased.leaseKey, leased.token, manifest)
}

func (p *Prewarmer) RunHistorical(ctx context.Context, timezone string, anchor time.Time) error {
	if p == nil {
		return fmt.Errorf("team usage prewarmer is not configured")
	}
	if !containsPrewarmTimezone(p.timezones, timezone) {
		return nil
	}
	binding, err := p.resolveBinding(ctx)
	if err != nil {
		return err
	}
	anchorDate, err := prewarmLocalAnchorDate(timezone, anchor)
	if err != nil {
		return err
	}
	safe, err := SplitSafe(timezone, anchorDate)
	if err != nil || !safe {
		return err
	}
	identity := PrewarmCacheIdentity{
		ProviderID: binding.ProviderID, ProviderVersion: binding.ProviderVersion,
		Timezone: timezone, AnchorDate: anchorDate,
	}
	classes := []PrewarmSegmentClass{SegmentHistory29d, SegmentHistory6d}
	var failures []error
	for _, class := range classes {
		due, dueErr := p.historicalClassDue(binding, timezone, anchorDate, class)
		if dueErr != nil {
			failures = append(failures, dueErr)
			continue
		}
		if !due {
			continue
		}
		if err := p.runHistoricalClass(ctx, binding, identity, class); err != nil {
			failures = append(failures, fmt.Errorf("historical %s: %w", class, err))
		}
	}
	return errors.Join(failures...)
}

func (p *Prewarmer) runHistoricalClass(
	ctx context.Context,
	binding ProviderBinding,
	identity PrewarmCacheIdentity,
	class PrewarmSegmentClass,
) error {
	coordinatorKey := p.cache.LeaseKey(
		"historical-coordinator", strconv.Itoa(binding.ProviderID), strconv.FormatInt(binding.ProviderVersion, 10),
		prewarmTimezoneDigest(identity.Timezone), identity.AnchorDate, string(class),
	)
	coordinatorToken, acquired, err := p.acquireLease(ctx, coordinatorKey, prewarmHistoryCoordinatorTTL)
	if err != nil {
		return err
	}
	if !acquired {
		return nil
	}
	retainCoordinator := false
	defer func() {
		if !retainCoordinator {
			p.releaseLease(coordinatorKey, coordinatorToken)
		}
	}()
	workerCtx, cancel := context.WithTimeout(ctx, p.options.historicalWorkerTimeout)
	defer cancel()
	workerCtx = withPrewarmCoordinatorLease(workerCtx, coordinatorKey, coordinatorToken)

	previous, ok, err := p.cache.Read(workerCtx, identity)
	if err != nil {
		return fmt.Errorf("read generation after coordinator acquisition: %w", err)
	}
	if ok && prewarmHistoricalStatus(previous, class) == PrewarmValueFresh {
		if err := p.requireCoordinatorOwned(workerCtx); err != nil {
			return err
		}
		retainCoordinator = true
		return nil
	}
	manifest := newPrewarmManifestCandidate(identity, previous, p.options.Now())
	leased, err := p.fetchLeasedSegment(workerCtx, binding, identity.Timezone, identity.AnchorDate, class, string(class))
	if errors.Is(err, errPrewarmLeaseBusy) {
		return nil
	}
	if err != nil {
		return err
	}
	defer p.releaseLeasedReference(leased)
	if class == SegmentHistory29d {
		manifest.History29d = leased.reference
	} else {
		manifest.History6d = leased.reference
	}
	if !prewarmManifestReferencesPresent(manifest) {
		return fmt.Errorf("historical manifest is incomplete")
	}
	if err := p.publishIfCurrent(leased.ctx, binding, leased.leaseKey, leased.token, manifest); err != nil {
		return err
	}
	if err := p.requireCoordinatorOwned(workerCtx); err != nil {
		return err
	}
	retainCoordinator = true
	return nil
}

func prewarmHistoricalStatus(result *PrewarmCacheResult, class PrewarmSegmentClass) PrewarmValueStatus {
	if result == nil {
		return PrewarmValueMissing
	}
	if class == SegmentHistory29d {
		return result.History29dStatus
	}
	return result.History6dStatus
}

func (p *Prewarmer) runStartup(ctx context.Context) error {
	binding, err := p.resolveBinding(ctx)
	if err != nil {
		return err
	}
	coordinatorKey := p.cache.LeaseKey(
		"startup-coordinator", strconv.Itoa(binding.ProviderID), strconv.FormatInt(binding.ProviderVersion, 10), p.allowlistDigest,
	)
	coordinatorToken, acquired, err := p.acquireLease(ctx, coordinatorKey, prewarmHistoryCoordinatorTTL)
	if err != nil || !acquired {
		return err
	}
	defer p.releaseLease(coordinatorKey, coordinatorToken)
	workerCtx, cancel := context.WithTimeout(ctx, p.options.startupWorkerTimeout)
	defer cancel()
	workerCtx = withPrewarmCoordinatorLease(workerCtx, coordinatorKey, coordinatorToken)

	var currentRef *PrewarmValueReference
	var failures []error
	for _, timezone := range p.timezones {
		anchorDate, anchorErr := prewarmLocalAnchorDate(timezone, p.options.Now())
		if anchorErr != nil {
			failures = append(failures, anchorErr)
			continue
		}
		safe, splitErr := SplitSafe(timezone, anchorDate)
		if splitErr != nil {
			failures = append(failures, splitErr)
			continue
		}
		if !safe {
			continue
		}
		identity := PrewarmCacheIdentity{
			ProviderID: binding.ProviderID, ProviderVersion: binding.ProviderVersion,
			Timezone: timezone, AnchorDate: anchorDate,
		}
		if ownershipErr := p.requireCoordinatorOwned(workerCtx); ownershipErr != nil {
			return errors.Join(append(failures, ownershipErr)...)
		}
		previous, ok, readErr := p.cache.Read(workerCtx, identity)
		if readErr != nil {
			failures = append(failures, fmt.Errorf("startup read %s: %w", timezone, readErr))
			continue
		}
		if !startupNeedsRecovery(previous, ok) {
			continue
		}
		manifest := newPrewarmManifestCandidate(identity, previous, p.options.Now())
		if !ok || previous.CurrentStatsStatus == PrewarmValueMissing || previous.CurrentStatsStatus == PrewarmValueHardExpired {
			if currentRef == nil {
				current, buildErr := p.source.BuildCurrentStats(workerCtx, binding)
				if buildErr != nil {
					failures = append(failures, fmt.Errorf("startup current stats: %w", buildErr))
					continue
				}
				written, writeErr := p.cache.WriteCurrentStats(workerCtx, current)
				if writeErr != nil {
					failures = append(failures, fmt.Errorf("startup write current stats: %w", writeErr))
					continue
				}
				currentRef = &written
			}
			manifest.CurrentStats = *currentRef
		}
		missingClasses := startupMissingSegmentClasses(previous, ok)
		for _, class := range missingClasses {
			leased, fetchErr := p.fetchHistoricalOrTodayClass(workerCtx, binding, timezone, anchorDate, class)
			if fetchErr != nil {
				if errors.Is(fetchErr, errPrewarmLeaseBusy) {
					continue
				}
				failures = append(failures, fmt.Errorf("startup %s %s: %w", timezone, class, fetchErr))
				continue
			}
			previousRef := manifest.TodayHour
			switch class {
			case SegmentHistory29d:
				previousRef = manifest.History29d
				manifest.History29d = leased.reference
			case SegmentHistory6d:
				previousRef = manifest.History6d
				manifest.History6d = leased.reference
			case SegmentTodayHour:
				manifest.TodayHour = leased.reference
			}
			if !prewarmManifestReferencesPresent(manifest) {
				p.releaseLeasedReference(leased)
				continue
			}
			if publishErr := p.publishIfCurrent(leased.ctx, binding, leased.leaseKey, leased.token, manifest); publishErr != nil {
				failures = append(failures, fmt.Errorf("startup publish %s %s: %w", timezone, class, publishErr))
				switch class {
				case SegmentHistory29d:
					manifest.History29d = previousRef
				case SegmentHistory6d:
					manifest.History6d = previousRef
				case SegmentTodayHour:
					manifest.TodayHour = previousRef
				}
			}
			p.releaseLeasedReference(leased)
		}
		if len(missingClasses) != 0 || !prewarmManifestReferencesPresent(manifest) {
			continue
		}
		if publishErr := p.publishIfCurrent(workerCtx, binding, coordinatorKey, coordinatorToken, manifest); publishErr != nil {
			failures = append(failures, fmt.Errorf("startup publish %s: %w", timezone, publishErr))
		}
	}
	return errors.Join(failures...)
}

func (p *Prewarmer) fetchHistoricalOrTodayClass(
	ctx context.Context,
	binding ProviderBinding,
	timezone, anchorDate string,
	class PrewarmSegmentClass,
) (leasedPrewarmReference, error) {
	refreshClass := string(class)
	if class == SegmentTodayHour {
		refreshClass = "moving"
	}
	return p.fetchLeasedSegment(ctx, binding, timezone, anchorDate, class, refreshClass)
}

type leasedPrewarmReference struct {
	reference PrewarmValueReference
	leaseKey  string
	token     string
	ctx       context.Context
	cancel    context.CancelFunc
}

func (p *Prewarmer) fetchLeasedSegment(
	ctx context.Context,
	binding ProviderBinding,
	timezone, anchorDate string,
	class PrewarmSegmentClass,
	refreshClass string,
) (leasedPrewarmReference, error) {
	leaseKey := p.segmentLeaseKey(binding, timezone, anchorDate, refreshClass)
	token, acquired, err := p.acquireLease(ctx, leaseKey, prewarmWorkerLeaseTTL)
	if err != nil {
		return leasedPrewarmReference{}, err
	}
	if !acquired {
		return leasedPrewarmReference{}, errPrewarmLeaseBusy
	}
	workerCtx, cancel := context.WithTimeout(ctx, p.options.segmentWorkerTimeout)
	if err := p.requireCoordinatorOwned(workerCtx); err != nil {
		cancel()
		p.releaseLease(leaseKey, token)
		return leasedPrewarmReference{}, err
	}
	segment, err := p.source.FetchSegment(workerCtx, binding, timezone, anchorDate, class)
	if err != nil {
		cancel()
		p.releaseLease(leaseKey, token)
		return leasedPrewarmReference{}, err
	}
	if err := p.requireCoordinatorOwned(workerCtx); err != nil {
		cancel()
		p.releaseLease(leaseKey, token)
		return leasedPrewarmReference{}, err
	}
	ref, err := p.cache.WriteSegment(workerCtx, segment)
	if err != nil {
		cancel()
		p.releaseLease(leaseKey, token)
		return leasedPrewarmReference{}, err
	}
	return leasedPrewarmReference{reference: ref, leaseKey: leaseKey, token: token, ctx: workerCtx, cancel: cancel}, nil
}

func (p *Prewarmer) releaseLeasedReference(leased leasedPrewarmReference) {
	if leased.cancel != nil {
		leased.cancel()
	}
	if leased.leaseKey != "" && leased.token != "" {
		p.releaseLease(leased.leaseKey, leased.token)
	}
}

func (p *Prewarmer) publishIfCurrent(
	ctx context.Context,
	binding ProviderBinding,
	leaseKey, token string,
	manifest PrewarmManifest,
) error {
	if err := p.requireCoordinatorOwned(ctx); err != nil {
		return err
	}
	current, err := p.resolveBinding(ctx)
	if err != nil {
		return fmt.Errorf("re-resolve primary provider before publication: %w", err)
	}
	if current.ProviderID != binding.ProviderID || current.ProviderVersion != binding.ProviderVersion {
		return fmt.Errorf("primary Relay provider version changed before prewarm publication")
	}
	if err := p.requireCoordinatorOwned(ctx); err != nil {
		return err
	}
	manifest.CreatedAt = p.options.Now()
	published, err := p.cache.PublishManifest(ctx, leaseKey, token, manifest)
	if err != nil {
		return err
	}
	if !published {
		return errPrewarmLeaseLost
	}
	return nil
}

func (p *Prewarmer) resolveBinding(ctx context.Context) (ProviderBinding, error) {
	binding, err := p.resolver.ResolvePrimaryProviderBinding(ctx)
	if err != nil {
		return ProviderBinding{}, fmt.Errorf("resolve primary Relay provider binding: %w", err)
	}
	if err := validateProviderBinding(binding); err != nil {
		return ProviderBinding{}, err
	}
	return binding, nil
}

func (p *Prewarmer) acquireLease(ctx context.Context, key string, ttl time.Duration) (string, bool, error) {
	token := p.options.NewToken()
	acquired, err := p.cache.TryAcquireLease(ctx, key, token, ttl)
	if err != nil {
		return "", false, fmt.Errorf("acquire team usage prewarm lease: %w", err)
	}
	return token, acquired, nil
}

func (p *Prewarmer) releaseLease(key, token string) {
	releaseCtx, cancel := context.WithTimeout(context.Background(), prewarmDefaultCommandTimeout)
	defer cancel()
	_, _ = p.cache.ReleaseLease(releaseCtx, key, token)
}

type prewarmCoordinatorLeaseContextKey struct{}

type prewarmCoordinatorLease struct {
	key   string
	token string
}

func withPrewarmCoordinatorLease(ctx context.Context, key, token string) context.Context {
	return context.WithValue(ctx, prewarmCoordinatorLeaseContextKey{}, prewarmCoordinatorLease{key: key, token: token})
}

func (p *Prewarmer) requireCoordinatorOwned(ctx context.Context) error {
	return requirePrewarmCoordinatorOwned(ctx, p.cache)
}

func requirePrewarmCoordinatorOwned(ctx context.Context, cache *PrewarmCache) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	lease, ok := ctx.Value(prewarmCoordinatorLeaseContextKey{}).(prewarmCoordinatorLease)
	if !ok {
		return nil
	}
	commandCtx, cancel := context.WithTimeout(ctx, cache.options.LeaseTimeout)
	value, err := cache.store.Get(commandCtx, lease.key)
	cancel()
	if errors.Is(err, readcache.ErrMiss) {
		return errPrewarmLeaseLost
	}
	if err != nil {
		return fmt.Errorf("check team usage prewarm coordinator lease: %w", err)
	}
	if !bytes.Equal(value, []byte(lease.token)) {
		return errPrewarmLeaseLost
	}
	return nil
}

func (p *Prewarmer) segmentLeaseKey(binding ProviderBinding, timezone, anchorDate, class string) string {
	return p.cache.LeaseKey(
		"segment", strconv.Itoa(binding.ProviderID), strconv.FormatInt(binding.ProviderVersion, 10),
		prewarmTimezoneDigest(timezone), anchorDate, class,
	)
}

func (p *Prewarmer) historicalClassDue(
	binding ProviderBinding,
	timezone, anchorDate string,
	class PrewarmSegmentClass,
) (bool, error) {
	location, err := loadPrewarmLocation(timezone)
	if err != nil {
		return false, err
	}
	midnight, err := time.ParseInLocation(time.DateOnly, anchorDate, location)
	if err != nil {
		return false, err
	}
	dueAt := midnight.Add(prewarmHistoricalJitter(binding, timezone, anchorDate, class))
	return !p.options.Now().Before(dueAt), nil
}

func prewarmHistoricalJitter(
	binding ProviderBinding,
	timezone, anchorDate string,
	class PrewarmSegmentClass,
) time.Duration {
	hash := sha256.New()
	for _, value := range []string{
		strconv.Itoa(binding.ProviderID), strconv.FormatInt(binding.ProviderVersion, 10),
		prewarmTimezoneDigest(timezone), anchorDate, string(class),
	} {
		writePrewarmLengthDelimited(hash, value)
	}
	sum := hash.Sum(nil)
	return time.Duration(binary.BigEndian.Uint64(sum[:8]) % uint64(prewarmHistoricalJitterMax))
}

func prewarmStringDigest(values ...string) string {
	hash := sha256.New()
	for _, value := range values {
		writePrewarmLengthDelimited(hash, value)
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func prewarmLocalAnchorDate(timezone string, value time.Time) (string, error) {
	location, err := loadPrewarmLocation(timezone)
	if err != nil {
		return "", err
	}
	return value.In(location).Format(time.DateOnly), nil
}

func containsPrewarmTimezone(timezones []string, target string) bool {
	for _, timezone := range timezones {
		if timezone == target {
			return true
		}
	}
	return false
}

func newPrewarmManifestCandidate(
	identity PrewarmCacheIdentity,
	previous *PrewarmCacheResult,
	createdAt time.Time,
) PrewarmManifest {
	manifest := PrewarmManifest{
		SchemaVersion: prewarmCacheSchemaVersion, ProviderID: identity.ProviderID, ProviderVersion: identity.ProviderVersion,
		Timezone: identity.Timezone, TimezoneDigest: prewarmTimezoneDigest(identity.Timezone), AnchorDate: identity.AnchorDate,
		CreatedAt: createdAt,
	}
	if previous != nil {
		manifest.CurrentStats = previous.Manifest.CurrentStats
		manifest.History29d = previous.Manifest.History29d
		manifest.History6d = previous.Manifest.History6d
		manifest.TodayHour = previous.Manifest.TodayHour
	}
	return manifest
}

func prewarmManifestReferencesPresent(manifest PrewarmManifest) bool {
	return manifest.CurrentStats.Key != "" && manifest.History29d.Key != "" &&
		manifest.History6d.Key != "" && manifest.TodayHour.Key != ""
}

func startupNeedsRecovery(result *PrewarmCacheResult, ok bool) bool {
	if !ok || result == nil {
		return true
	}
	for _, status := range []PrewarmValueStatus{
		result.CurrentStatsStatus, result.History29dStatus, result.History6dStatus, result.TodayHourStatus,
	} {
		if status == PrewarmValueMissing || status == PrewarmValueHardExpired {
			return true
		}
	}
	return false
}

func startupMissingSegmentClasses(result *PrewarmCacheResult, ok bool) []PrewarmSegmentClass {
	if !ok || result == nil {
		return []PrewarmSegmentClass{SegmentHistory29d, SegmentHistory6d, SegmentTodayHour}
	}
	statuses := []struct {
		class  PrewarmSegmentClass
		status PrewarmValueStatus
	}{
		{SegmentHistory29d, result.History29dStatus},
		{SegmentHistory6d, result.History6dStatus},
		{SegmentTodayHour, result.TodayHourStatus},
	}
	classes := make([]PrewarmSegmentClass, 0, len(statuses))
	for _, item := range statuses {
		if item.status == PrewarmValueMissing || item.status == PrewarmValueHardExpired {
			classes = append(classes, item.class)
		}
	}
	return classes
}

type distributedSourceCallLimiter struct {
	cache       *PrewarmCache
	semaphore   chan struct{}
	newToken    func() string
	sleep       func(context.Context, time.Duration) error
	callTimeout time.Duration
}

func (l *distributedSourceCallLimiter) Do(ctx context.Context, call func(context.Context) error) error {
	if call == nil {
		return fmt.Errorf("team usage prewarm source call is required")
	}
	if err := requirePrewarmCoordinatorOwned(ctx, l.cache); err != nil {
		return err
	}
	select {
	case l.semaphore <- struct{}{}:
		defer func() { <-l.semaphore }()
	case <-ctx.Done():
		return ctx.Err()
	}
	if err := requirePrewarmCoordinatorOwned(ctx, l.cache); err != nil {
		return err
	}

	leaseKey, token, err := l.acquireSlot(ctx)
	if err != nil {
		return fmt.Errorf("acquire team usage prewarm source slot: %w", err)
	}
	releaseSlot := func() (bool, error) {
		releaseCtx, cancel := context.WithTimeout(context.Background(), prewarmDefaultCommandTimeout)
		defer cancel()
		return l.cache.ReleaseLease(releaseCtx, leaseKey, token)
	}
	if err := requirePrewarmCoordinatorOwned(ctx, l.cache); err != nil {
		released, releaseErr := releaseSlot()
		return errors.Join(err, prewarmSlotReleaseError(released, releaseErr))
	}
	if err := ctx.Err(); err != nil {
		released, releaseErr := releaseSlot()
		return errors.Join(err, prewarmSlotReleaseError(released, releaseErr))
	}
	callCtx, cancel := context.WithTimeout(ctx, l.callTimeout)
	callErr := call(callCtx)
	callContextErr := callCtx.Err()
	cancel()
	coordinatorErr := requirePrewarmCoordinatorOwned(ctx, l.cache)
	released, releaseErr := releaseSlot()
	return errors.Join(callErr, callContextErr, coordinatorErr, prewarmSlotReleaseError(released, releaseErr))
}

func prewarmSlotReleaseError(released bool, err error) error {
	if err != nil {
		return fmt.Errorf("release team usage prewarm source slot: %w", err)
	}
	if !released {
		return errPrewarmLeaseLost
	}
	return nil
}

func (l *distributedSourceCallLimiter) acquireSlot(ctx context.Context) (string, string, error) {
	for {
		if err := requirePrewarmCoordinatorOwned(ctx, l.cache); err != nil {
			return "", "", err
		}
		for slot := 0; slot < prewarmSourceSlotCount; slot++ {
			key := l.cache.LeaseKey("source-slot", strconv.Itoa(slot))
			token := l.newToken()
			acquired, err := l.cache.TryAcquireLease(ctx, key, token, prewarmWorkerLeaseTTL)
			if err != nil {
				return "", "", err
			}
			if acquired {
				return key, token, nil
			}
		}
		if err := l.sleep(ctx, prewarmSourceSlotPoll); err != nil {
			return "", "", err
		}
	}
}

var _ SourceCallLimiter = (*distributedSourceCallLimiter)(nil)
