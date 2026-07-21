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
	"strings"
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
	prewarmMovingRefreshClass      = "moving"
)

var errPrewarmLeaseLost = errors.New("team usage prewarm lease ownership was lost")
var errPrewarmLeaseBusy = errors.New("team usage prewarm lease is owned by another worker")

type PrewarmerOptions struct {
	Timezones       []string
	Now             func() time.Time
	NewToken        func() string
	NewGenerationID func() string
	Sleep           func(context.Context, time.Duration) error
	Metrics         PrewarmMetrics
	Reporter        PrewarmReporter
	NewOperationID  func() string

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

	timezones        []string
	allowlistDigest  string
	moving           atomic.Bool
	recovery         atomic.Bool
	scheduledWorkers atomic.Int64

	lifecycleMu sync.Mutex
	state       prewarmerLifecycleState
	cancel      context.CancelFunc
	stopDone    chan struct{}
	startupDone chan struct{}
	wg          sync.WaitGroup

	reportingMu       sync.Mutex
	reportingProvider int
	reportingVersion  int64

	publicationMu              sync.Mutex
	publicationProvider        int
	publicationProviderVersion int64
	publicationCurrentID       string
	publicationCurrentBytes    int
	publicationTimezoneBytes   map[string]int
	publicationExpectedAnchors map[string]string
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
	options.Metrics = prewarmMetricsOrNoop(options.Metrics)
	options.Reporter = prewarmReporterOrNoop(options.Reporter)
	if options.NewOperationID == nil {
		options.NewOperationID = newPrewarmRandomID
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
		Now: options.Now, NewGenerationID: options.NewGenerationID, Metrics: options.Metrics,
	})
	if err != nil {
		return nil, err
	}
	return &Prewarmer{
		resolver: resolver, cache: cache, source: source, limiter: limiter, options: options,
		timezones: timezones, allowlistDigest: prewarmStringDigest(timezones...),
		publicationTimezoneBytes: make(map[string]int, len(timezones)),
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
	p.startupDone = make(chan struct{})
	startupDone := p.startupDone
	p.wg.Add(1)
	p.lifecycleMu.Unlock()

	go p.runLifecycle(runCtx, startupDone)
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

func (p *Prewarmer) runLifecycle(ctx context.Context, startupDone chan struct{}) {
	defer p.wg.Done()
	startedAt := time.Now()
	operationID := p.newOperationID()
	startupErr := p.runStartup(ctx)
	p.reportLifecycle(operationID, PrewarmCycleStartup, p.timezones, startedAt, startupErr, true)
	close(startupDone)

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
			p.startRecovery(ctx)
			for _, timezone := range p.timezones {
				p.startHistorical(ctx, timezone, p.options.Now())
			}
		}
	}
}

func (p *Prewarmer) startMoving(ctx context.Context) {
	if !p.moving.CompareAndSwap(false, true) {
		for _, timezone := range p.timezones {
			p.options.Metrics.RecordCycle("moving", timezone, "tick_skipped", 0)
		}
		return
	}
	if !p.beginLifecycleWorker() {
		p.moving.Store(false)
		return
	}
	p.scheduledWorkers.Add(1)
	go func() {
		defer p.wg.Done()
		defer p.scheduledWorkers.Add(-1)
		defer p.moving.Store(false)
		startedAt := time.Now()
		operationID := p.newOperationID()
		err := p.runMoving(ctx)
		p.reportScheduledLifecycle(operationID, prewarmLifecycleTargets(PrewarmCycleMoving, p.timezones), startedAt, err)
	}()
}

func (p *Prewarmer) startRecovery(ctx context.Context) {
	if !p.recovery.CompareAndSwap(false, true) {
		return
	}
	if !p.beginLifecycleWorker() {
		p.recovery.Store(false)
		return
	}
	p.scheduledWorkers.Add(1)
	go func() {
		defer p.wg.Done()
		defer p.scheduledWorkers.Add(-1)
		defer p.recovery.Store(false)
		startedAt := time.Now()
		operationID := p.newOperationID()
		err := p.runRecovery(ctx)
		p.reportScheduledLifecycle(operationID, prewarmLifecycleTargets(PrewarmCycleRecovery, p.timezones), startedAt, err)
	}()
}

func (p *Prewarmer) startHistorical(ctx context.Context, timezone string, anchor time.Time) {
	if !p.beginLifecycleWorker() {
		return
	}
	p.scheduledWorkers.Add(1)
	go func() {
		defer p.wg.Done()
		defer p.scheduledWorkers.Add(-1)
		startedAt := time.Now()
		operationID := p.newOperationID()
		err := p.RunHistorical(ctx, timezone, anchor)
		p.reportScheduledLifecycle(operationID, []prewarmLifecycleTarget{
			{class: PrewarmCycleHistory29d, timezone: timezone},
			{class: PrewarmCycleHistory6d, timezone: timezone},
		}, startedAt, err)
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
	normalLanes, preflightErr := p.preflightMovingLanes(ctx, binding)
	if len(normalLanes) == 0 {
		return preflightErr
	}
	return errors.Join(preflightErr, p.runNormalMoving(ctx, binding, normalLanes))
}

type prewarmTimezoneLane struct {
	timezone   string
	anchorDate string
}

type prewarmLifecycleTarget struct {
	class    PrewarmCycleClass
	timezone string
}

type prewarmLifecycleFailure struct {
	target        prewarmLifecycleTarget
	cycleRecorded bool
	err           error
}

func (e *prewarmLifecycleFailure) Error() string { return e.err.Error() }
func (e *prewarmLifecycleFailure) Unwrap() error { return e.err }

func newPrewarmLifecycleFailure(class PrewarmCycleClass, timezone string, cycleRecorded bool, err error) error {
	if err == nil {
		return nil
	}
	return &prewarmLifecycleFailure{
		target:        prewarmLifecycleTarget{class: class, timezone: timezone},
		cycleRecorded: cycleRecorded,
		err:           err,
	}
}

func newPrewarmLifecycleLaneFailures(
	class PrewarmCycleClass,
	lanes []prewarmTimezoneLane,
	cycleRecorded bool,
	err error,
) error {
	if err == nil {
		return nil
	}
	failures := make([]error, 0, len(lanes))
	for _, lane := range lanes {
		failures = append(failures, newPrewarmLifecycleFailure(class, lane.timezone, cycleRecorded, err))
	}
	return errors.Join(failures...)
}

func (p *Prewarmer) preflightMovingLanes(
	ctx context.Context,
	binding ProviderBinding,
) ([]prewarmTimezoneLane, error) {
	batchTime := p.options.Now()
	normal := make([]prewarmTimezoneLane, 0, len(p.timezones))
	failures := make([]error, 0, len(p.timezones))
	for _, timezone := range p.timezones {
		anchorDate, err := prewarmLocalAnchorDate(timezone, batchTime)
		if err != nil {
			failures = append(failures, newPrewarmLifecycleFailure(
				PrewarmCycleMoving, timezone, false, fmt.Errorf("preflight moving timezone %s anchor: %w", timezone, err),
			))
			continue
		}
		safe, err := SplitSafe(timezone, anchorDate)
		if err != nil {
			failures = append(failures, newPrewarmLifecycleFailure(
				PrewarmCycleMoving, timezone, false, fmt.Errorf("preflight moving timezone %s split safety: %w", timezone, err),
			))
			continue
		}
		if !safe {
			continue
		}
		identity := PrewarmCacheIdentity{
			ProviderID: binding.ProviderID, ProviderVersion: binding.ProviderVersion,
			Timezone: timezone, AnchorDate: anchorDate,
		}
		result, found, err := p.cache.Read(ctx, identity)
		if err != nil {
			failures = append(failures, newPrewarmLifecycleFailure(
				PrewarmCycleMoving, timezone, false, fmt.Errorf("preflight moving timezone %s: %w", timezone, err),
			))
			continue
		}
		lane := prewarmTimezoneLane{timezone: timezone, anchorDate: anchorDate}
		if found {
			if result.History29dStatus != PrewarmValueMissing && result.History29dStatus != PrewarmValueHardExpired &&
				result.History6dStatus != PrewarmValueMissing && result.History6dStatus != PrewarmValueHardExpired {
				normal = append(normal, lane)
			}
		}
	}
	return normal, errors.Join(failures...)
}

func (p *Prewarmer) runNormalMoving(
	ctx context.Context,
	binding ProviderBinding,
	lanes []prewarmTimezoneLane,
) error {
	tick := p.options.Now().Truncate(prewarmMovingInterval).UTC().Format(time.RFC3339)
	tickKey := p.cache.LeaseKey(
		"moving-tick", strconv.Itoa(binding.ProviderID), strconv.FormatInt(binding.ProviderVersion, 10),
		p.allowlistDigest, tick, "moving",
	)
	tickToken, acquired, err := p.acquireLease(ctx, tickKey, prewarmMovingCoordinatorTTL)
	if err != nil || !acquired {
		return newPrewarmLifecycleLaneFailures(PrewarmCycleMoving, lanes, false, err)
	}
	retainTick := false
	defer func() {
		if !retainTick {
			p.releaseLease(tickKey, tickToken)
		}
	}()
	activeKey := p.cache.LeaseKey(
		"moving-active", strconv.Itoa(binding.ProviderID), strconv.FormatInt(binding.ProviderVersion, 10), p.allowlistDigest,
	)
	activeToken, active, err := p.acquireLease(ctx, activeKey, prewarmMovingCoordinatorTTL)
	if err != nil {
		return newPrewarmLifecycleLaneFailures(PrewarmCycleMoving, lanes, false, err)
	}
	if !active {
		retainTick = true
		return nil
	}
	defer p.releaseLease(activeKey, activeToken)
	workerCtx, cancel := context.WithTimeout(ctx, p.options.movingWorkerTimeout)
	defer cancel()
	workerCtx = withPrewarmControllingLeases(workerCtx,
		PrewarmLeaseClaim{Key: tickKey, Token: tickToken},
		PrewarmLeaseClaim{Key: activeKey, Token: activeToken},
	)

	current, err := p.buildCurrentStats(workerCtx, binding, "moving")
	if err != nil {
		return newPrewarmLifecycleLaneFailures(PrewarmCycleMoving, lanes, false, err)
	}
	if err := p.requireCoordinatorOwned(workerCtx); err != nil {
		return newPrewarmLifecycleLaneFailures(PrewarmCycleMoving, lanes, false, err)
	}
	currentRef, err := p.cache.WriteCurrentStats(workerCtx, current)
	if err != nil {
		return newPrewarmLifecycleLaneFailures(
			PrewarmCycleMoving, lanes, false, fmt.Errorf("write moving current stats: %w", err),
		)
	}
	if err := p.requireCoordinatorOwned(workerCtx); err != nil {
		return newPrewarmLifecycleLaneFailures(PrewarmCycleMoving, lanes, false, err)
	}
	p.beginPublicationBatch(binding, currentRef, lanes)

	var wg sync.WaitGroup
	errorsByTimezone := make(chan error, len(lanes))
	for _, lane := range lanes {
		lane := lane
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := p.runMovingLane(workerCtx, binding, currentRef, lane); err != nil {
				errorsByTimezone <- newPrewarmLifecycleFailure(
					PrewarmCycleMoving, lane.timezone, true, fmt.Errorf("moving timezone %s: %w", lane.timezone, err),
				)
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
		return errors.Join(append(failures, newPrewarmLifecycleLaneFailures(PrewarmCycleMoving, lanes, true, err))...)
	}
	if err := p.requireCoordinatorOwned(workerCtx); err != nil {
		return errors.Join(append(failures, newPrewarmLifecycleLaneFailures(PrewarmCycleMoving, lanes, true, err))...)
	}
	retainTick = true
	return errors.Join(failures...)
}

func (p *Prewarmer) runMovingTimezone(
	ctx context.Context,
	binding ProviderBinding,
	currentRef PrewarmValueReference,
	timezone string,
) error {
	anchorDate, err := prewarmLocalAnchorDate(timezone, p.options.Now())
	if err != nil {
		return err
	}
	return p.runMovingLane(ctx, binding, currentRef, prewarmTimezoneLane{timezone: timezone, anchorDate: anchorDate})
}

func (p *Prewarmer) runMovingLane(
	ctx context.Context,
	binding ProviderBinding,
	currentRef PrewarmValueReference,
	lane prewarmTimezoneLane,
) (err error) {
	startedAt := time.Now()
	cycleOutcome := "skipped"
	defer func() {
		if err != nil {
			cycleOutcome = prewarmTelemetryOutcome(err)
		}
		p.options.Metrics.RecordCycle("moving", lane.timezone, cycleOutcome, time.Since(startedAt))
	}()
	if err := p.requireCoordinatorOwned(ctx); err != nil {
		return err
	}
	identity := PrewarmCacheIdentity{
		ProviderID: binding.ProviderID, ProviderVersion: binding.ProviderVersion,
		Timezone: lane.timezone, AnchorDate: lane.anchorDate,
	}
	previous, ok, err := p.cache.Read(ctx, identity)
	if err != nil {
		return fmt.Errorf("read previous moving generation: %w", err)
	}
	if !ok || previous.History29dStatus == PrewarmValueMissing || previous.History29dStatus == PrewarmValueHardExpired ||
		previous.History6dStatus == PrewarmValueMissing || previous.History6dStatus == PrewarmValueHardExpired {
		return nil
	}
	leased, err := p.fetchLeasedSegment(ctx, binding, lane.timezone, lane.anchorDate, SegmentTodayHour, prewarmMovingRefreshClass)
	if errors.Is(err, errPrewarmLeaseBusy) {
		return nil
	}
	if err != nil {
		return err
	}
	defer p.releaseLeasedReference(leased)
	manifest := PrewarmManifest{
		SchemaVersion: prewarmCacheSchemaVersion, ProviderID: binding.ProviderID, ProviderVersion: binding.ProviderVersion,
		Timezone: lane.timezone, TimezoneDigest: prewarmTimezoneDigest(lane.timezone), AnchorDate: lane.anchorDate,
		CreatedAt: p.options.Now(), CurrentStats: currentRef,
		History29d: previous.Manifest.History29d, History6d: previous.Manifest.History6d, TodayHour: leased.reference,
	}
	err = p.publishIfCurrent(leased.ctx, binding, []PrewarmLeaseClaim{{Key: leased.leaseKey, Token: leased.token}}, manifest)
	if err == nil {
		cycleOutcome = "success"
		p.options.Metrics.SetLastSuccess("moving", lane.timezone, p.options.Now())
	}
	return err
}

func (p *Prewarmer) runRecovery(ctx context.Context) error {
	binding, err := p.resolveBinding(ctx)
	if err != nil {
		return err
	}
	tick := p.options.Now().Truncate(prewarmMovingInterval).UTC().Format(time.RFC3339)
	tickKey := p.cache.LeaseKey(
		"recovery-tick", strconv.Itoa(binding.ProviderID), strconv.FormatInt(binding.ProviderVersion, 10),
		p.allowlistDigest, tick, "recovery",
	)
	tickToken, acquired, err := p.acquireLease(ctx, tickKey, prewarmHistoryCoordinatorTTL)
	if err != nil || !acquired {
		return err
	}
	retainTick := false
	defer func() {
		if !retainTick {
			p.releaseLease(tickKey, tickToken)
		}
	}()
	activeKey := p.cache.LeaseKey(
		"recovery-active", strconv.Itoa(binding.ProviderID), strconv.FormatInt(binding.ProviderVersion, 10), p.allowlistDigest,
	)
	activeToken, active, err := p.acquireLease(ctx, activeKey, prewarmHistoryCoordinatorTTL)
	if err != nil {
		return err
	}
	if !active {
		retainTick = true
		return nil
	}
	defer p.releaseLease(activeKey, activeToken)
	workerCtx, cancel := context.WithTimeout(ctx, p.options.historicalWorkerTimeout)
	defer cancel()
	workerCtx = withPrewarmControllingLeases(workerCtx,
		PrewarmLeaseClaim{Key: tickKey, Token: tickToken},
		PrewarmLeaseClaim{Key: activeKey, Token: activeToken},
	)

	lanes, preflightErr := p.preflightRecoveryLanes(workerCtx, binding)
	var failures []error
	if preflightErr != nil {
		failures = append(failures, preflightErr)
	}
	if len(lanes) == 0 {
		workerErr := workerCtx.Err()
		coordinatorErr := p.requireCoordinatorOwned(workerCtx)
		if len(failures) == 0 {
			retainTick = workerErr == nil && coordinatorErr == nil
		}
		return errors.Join(failures...)
	}

	current, err := p.buildCurrentStats(workerCtx, binding, "recovery")
	if err != nil {
		return errors.Join(preflightErr, newPrewarmLifecycleLaneFailures(PrewarmCycleRecovery, lanes, false, err))
	}
	if err := p.requireCoordinatorOwned(workerCtx); err != nil {
		return errors.Join(preflightErr, newPrewarmLifecycleLaneFailures(PrewarmCycleRecovery, lanes, false, err))
	}
	currentRef, err := p.cache.WriteCurrentStats(workerCtx, current)
	if err != nil {
		return errors.Join(preflightErr, newPrewarmLifecycleLaneFailures(
			PrewarmCycleRecovery, lanes, false, fmt.Errorf("write recovery current stats: %w", err),
		))
	}
	p.beginPublicationBatch(binding, currentRef, lanes)

	errorsByTimezone := make(chan error, len(lanes))
	var wg sync.WaitGroup
	for _, lane := range lanes {
		lane := lane
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := p.runRecoveryLane(workerCtx, binding, currentRef, lane); err != nil {
				errorsByTimezone <- newPrewarmLifecycleFailure(
					PrewarmCycleRecovery, lane.timezone, true, fmt.Errorf("recovery timezone %s: %w", lane.timezone, err),
				)
			}
		}()
	}
	wg.Wait()
	close(errorsByTimezone)
	for failure := range errorsByTimezone {
		failures = append(failures, failure)
	}
	if err := workerCtx.Err(); err != nil {
		failures = append(failures, newPrewarmLifecycleLaneFailures(PrewarmCycleRecovery, lanes, true, err))
	}
	if err := p.requireCoordinatorOwned(workerCtx); err != nil {
		failures = append(failures, newPrewarmLifecycleLaneFailures(PrewarmCycleRecovery, lanes, true, err))
	}
	if len(failures) == 0 {
		retainTick = true
	}
	return errors.Join(failures...)
}

func (p *Prewarmer) preflightRecoveryLanes(
	ctx context.Context,
	binding ProviderBinding,
) ([]prewarmTimezoneLane, error) {
	batchTime := p.options.Now()
	lanes := make([]prewarmTimezoneLane, 0, len(p.timezones))
	failures := make([]error, 0, len(p.timezones))
	for _, timezone := range p.timezones {
		anchorDate, err := prewarmLocalAnchorDate(timezone, batchTime)
		if err != nil {
			failures = append(failures, newPrewarmLifecycleFailure(
				PrewarmCycleRecovery, timezone, false, fmt.Errorf("preflight recovery timezone %s anchor: %w", timezone, err),
			))
			continue
		}
		safe, err := SplitSafe(timezone, anchorDate)
		if err != nil {
			failures = append(failures, newPrewarmLifecycleFailure(
				PrewarmCycleRecovery, timezone, false, fmt.Errorf("preflight recovery timezone %s split safety: %w", timezone, err),
			))
			continue
		}
		if !safe {
			continue
		}
		identity := PrewarmCacheIdentity{
			ProviderID: binding.ProviderID, ProviderVersion: binding.ProviderVersion,
			Timezone: timezone, AnchorDate: anchorDate,
		}
		if _, found, err := p.cache.Read(ctx, identity); err != nil {
			failures = append(failures, newPrewarmLifecycleFailure(
				PrewarmCycleRecovery, timezone, false, fmt.Errorf("preflight recovery timezone %s: %w", timezone, err),
			))
			continue
		} else if found {
			continue
		}
		due29, err := p.historicalClassDue(binding, timezone, anchorDate, SegmentHistory29d)
		if err != nil {
			failures = append(failures, newPrewarmLifecycleFailure(
				PrewarmCycleRecovery, timezone, false, fmt.Errorf("preflight recovery timezone %s history_29d jitter: %w", timezone, err),
			))
			continue
		}
		due6, err := p.historicalClassDue(binding, timezone, anchorDate, SegmentHistory6d)
		if err != nil {
			failures = append(failures, newPrewarmLifecycleFailure(
				PrewarmCycleRecovery, timezone, false, fmt.Errorf("preflight recovery timezone %s history_6d jitter: %w", timezone, err),
			))
			continue
		}
		if due29 && due6 {
			lanes = append(lanes, prewarmTimezoneLane{timezone: timezone, anchorDate: anchorDate})
		}
	}
	return lanes, errors.Join(failures...)
}

func (p *Prewarmer) runRecoveryLane(
	ctx context.Context,
	binding ProviderBinding,
	currentRef PrewarmValueReference,
	lane prewarmTimezoneLane,
) (err error) {
	startedAt := time.Now()
	cycleOutcome := "skipped"
	defer func() {
		if err != nil {
			cycleOutcome = prewarmTelemetryOutcome(err)
		}
		p.options.Metrics.RecordCycle("recovery", lane.timezone, cycleOutcome, time.Since(startedAt))
	}()
	if err := p.requireCoordinatorOwned(ctx); err != nil {
		return err
	}
	identity := PrewarmCacheIdentity{
		ProviderID: binding.ProviderID, ProviderVersion: binding.ProviderVersion,
		Timezone: lane.timezone, AnchorDate: lane.anchorDate,
	}
	if _, found, err := p.cache.Read(ctx, identity); err != nil {
		return fmt.Errorf("read recovery generation before source calls: %w", err)
	} else if found {
		return nil
	}
	refs := make(map[PrewarmSegmentClass]PrewarmValueReference, 3)
	classes := []PrewarmSegmentClass{SegmentHistory29d, SegmentHistory6d, SegmentTodayHour}
	var completing leasedPrewarmReference
	for index, class := range classes {
		refreshClass := string(class)
		if class == SegmentTodayHour {
			refreshClass = prewarmMovingRefreshClass
		}
		leased, err := p.fetchLeasedSegment(ctx, binding, lane.timezone, lane.anchorDate, class, refreshClass)
		if err != nil {
			return err
		}
		refs[class] = leased.reference
		if index == len(classes)-1 {
			completing = leased
		} else {
			p.releaseLeasedReference(leased)
		}
	}
	defer p.releaseLeasedReference(completing)
	manifest := PrewarmManifest{
		SchemaVersion: prewarmCacheSchemaVersion, ProviderID: binding.ProviderID, ProviderVersion: binding.ProviderVersion,
		Timezone: lane.timezone, TimezoneDigest: prewarmTimezoneDigest(lane.timezone), AnchorDate: lane.anchorDate,
		CreatedAt: p.options.Now(), CurrentStats: currentRef,
		History29d: refs[SegmentHistory29d], History6d: refs[SegmentHistory6d], TodayHour: refs[SegmentTodayHour],
	}
	err = p.publishIfCurrent(completing.ctx, binding, []PrewarmLeaseClaim{{Key: completing.leaseKey, Token: completing.token}}, manifest)
	if err == nil {
		cycleOutcome = "success"
		p.options.Metrics.SetLastSuccess("recovery", lane.timezone, p.options.Now())
	}
	return err
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
	if _, found, err := p.cache.Read(ctx, identity); err != nil {
		return fmt.Errorf("preflight historical timezone %s: %w", timezone, err)
	} else if !found {
		return nil
	}
	classes := []PrewarmSegmentClass{SegmentHistory29d, SegmentHistory6d}
	var failures []error
	for _, class := range classes {
		due, dueErr := p.historicalClassDue(binding, timezone, anchorDate, class)
		if dueErr != nil {
			failures = append(failures, newPrewarmLifecycleFailure(
				PrewarmCycleClass(class), timezone, false, dueErr,
			))
			continue
		}
		if !due {
			continue
		}
		if err := p.runHistoricalClass(ctx, binding, identity, class); err != nil {
			failures = append(failures, newPrewarmLifecycleFailure(
				PrewarmCycleClass(class), timezone, true, fmt.Errorf("historical %s: %w", class, err),
			))
		}
	}
	return errors.Join(failures...)
}

func (p *Prewarmer) runHistoricalClass(
	ctx context.Context,
	binding ProviderBinding,
	identity PrewarmCacheIdentity,
	class PrewarmSegmentClass,
) (err error) {
	startedAt := time.Now()
	cycleOutcome := "skipped"
	defer func() {
		if err != nil {
			cycleOutcome = prewarmTelemetryOutcome(err)
		}
		p.options.Metrics.RecordCycle(string(class), identity.Timezone, cycleOutcome, time.Since(startedAt))
	}()
	coordinatorKey := p.cache.LeaseKey(
		"historical-coordinator", strconv.Itoa(binding.ProviderID), strconv.FormatInt(binding.ProviderVersion, 10),
		p.allowlistDigest, prewarmTimezoneDigest(identity.Timezone), identity.AnchorDate, string(class),
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
	workerCtx = withPrewarmControllingLeases(workerCtx, PrewarmLeaseClaim{Key: coordinatorKey, Token: coordinatorToken})

	previous, ok, err := p.cache.Read(workerCtx, identity)
	if err != nil {
		return fmt.Errorf("read generation after coordinator acquisition: %w", err)
	}
	if !ok {
		return nil
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
	if err := p.publishIfCurrent(leased.ctx, binding, []PrewarmLeaseClaim{{Key: leased.leaseKey, Token: leased.token}}, manifest); err != nil {
		return err
	}
	if err := p.requireCoordinatorOwned(workerCtx); err != nil {
		return err
	}
	retainCoordinator = true
	cycleOutcome = "success"
	p.options.Metrics.SetLastSuccess(string(class), identity.Timezone, p.options.Now())
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
	workerCtx = withPrewarmControllingLeases(workerCtx, PrewarmLeaseClaim{Key: coordinatorKey, Token: coordinatorToken})

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
				current, buildErr := p.buildCurrentStats(workerCtx, binding, "startup")
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
			if publishErr := p.publishIfCurrent(leased.ctx, binding, []PrewarmLeaseClaim{{Key: leased.leaseKey, Token: leased.token}}, manifest); publishErr != nil {
				failures = append(failures, fmt.Errorf("startup publish %s %s: %w", timezone, class, publishErr))
				switch class {
				case SegmentHistory29d:
					manifest.History29d = previousRef
				case SegmentHistory6d:
					manifest.History6d = previousRef
				case SegmentTodayHour:
					manifest.TodayHour = previousRef
				}
			} else {
				p.options.Metrics.SetLastSuccess("startup", timezone, p.options.Now())
			}
			p.releaseLeasedReference(leased)
		}
		if len(missingClasses) != 0 || !prewarmManifestReferencesPresent(manifest) {
			continue
		}
		if publishErr := p.publishIfCurrent(workerCtx, binding, nil, manifest); publishErr != nil {
			failures = append(failures, fmt.Errorf("startup publish %s: %w", timezone, publishErr))
		} else {
			p.options.Metrics.SetLastSuccess("startup", timezone, p.options.Now())
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
		refreshClass = prewarmMovingRefreshClass
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
	startedAt := time.Now()
	segment, err := p.source.FetchSegment(workerCtx, binding, timezone, anchorDate, class)
	sourceOutcome := "success"
	if err != nil {
		sourceOutcome = prewarmTelemetryOutcome(err)
	}
	p.options.Metrics.RecordSource(
		string(class), timezone, sourceOutcome, time.Since(startedAt),
		int(segment.ResponseBytes), segment.PointCount, segment.UniqueUserCount,
	)
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

func (p *Prewarmer) buildCurrentStats(ctx context.Context, binding ProviderBinding, class string) (PrewarmCurrentStatsEnvelope, error) {
	startedAt := time.Now()
	current, err := p.source.BuildCurrentStats(ctx, binding)
	outcome := "success"
	if err != nil {
		outcome = prewarmTelemetryOutcome(err)
	}
	for _, timezone := range p.timezones {
		p.options.Metrics.RecordSource(
			class, timezone, outcome, time.Since(startedAt),
			int(current.ResponseBytes), 0, current.RosterCount,
		)
	}
	return current, err
}

func prewarmTelemetryOutcome(err error) string {
	var sourceFailure *prewarmSourceFailure
	if errors.As(err, &sourceFailure) && sourceFailure.kind == prewarmSourceFailureValidation {
		return "rejected"
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return "canceled"
	}
	return "error"
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
	extraClaims []PrewarmLeaseClaim,
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
	claims := append(prewarmControllingLeaseClaims(ctx), extraClaims...)
	published, err := p.cache.PublishManifestWithLeases(ctx, claims, manifest)
	if err != nil {
		return err
	}
	if !published {
		return errPrewarmLeaseLost
	}
	p.recordPublishedGeneration(manifest)
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
	p.reportingMu.Lock()
	p.reportingProvider = binding.ProviderID
	p.reportingVersion = binding.ProviderVersion
	p.reportingMu.Unlock()
	return binding, nil
}

func (p *Prewarmer) recordPublishedGeneration(manifest PrewarmManifest) {
	encodedManifest, err := encodePrewarmJSON(manifest, prewarmManifestMaxBytes)
	if err != nil {
		return
	}
	segmentBytes := manifest.History29d.SerializedBytes + manifest.History6d.SerializedBytes + manifest.TodayHour.SerializedBytes
	timezoneBytes := segmentBytes + len(encodedManifest)
	p.options.Metrics.RecordQuantity(PrewarmQuantitySegmentBytes, manifest.Timezone, segmentBytes)
	p.options.Metrics.RecordQuantity(PrewarmQuantityTimezoneBytes, manifest.Timezone, timezoneBytes)

	p.publicationMu.Lock()
	defer p.publicationMu.Unlock()
	if len(p.publicationExpectedAnchors) == 0 ||
		p.publicationProvider != manifest.ProviderID ||
		p.publicationProviderVersion != manifest.ProviderVersion ||
		p.publicationCurrentID != manifest.CurrentStats.GenerationID {
		return
	}
	expectedAnchor, expected := p.publicationExpectedAnchors[manifest.Timezone]
	if !expected || manifest.AnchorDate != expectedAnchor {
		return
	}
	p.publicationTimezoneBytes[manifest.Timezone] = timezoneBytes
	fullGenerationBytes := p.publicationCurrentBytes
	for timezone := range p.publicationExpectedAnchors {
		size, ok := p.publicationTimezoneBytes[timezone]
		if !ok {
			return
		}
		fullGenerationBytes += size
	}
	p.options.Metrics.SetGenerationBytes(fullGenerationBytes)
}

func (p *Prewarmer) beginPublicationBatch(
	binding ProviderBinding,
	current PrewarmValueReference,
	lanes []prewarmTimezoneLane,
) {
	expected := make(map[string]string, len(lanes))
	for _, lane := range lanes {
		expected[lane.timezone] = lane.anchorDate
	}
	p.publicationMu.Lock()
	defer p.publicationMu.Unlock()
	p.publicationProvider = binding.ProviderID
	p.publicationProviderVersion = binding.ProviderVersion
	p.publicationCurrentID = current.GenerationID
	p.publicationCurrentBytes = current.SerializedBytes
	clear(p.publicationTimezoneBytes)
	p.publicationExpectedAnchors = expected
}

func (p *Prewarmer) newOperationID() string {
	operationID := strings.TrimSpace(p.options.NewOperationID())
	if operationID == "" {
		return "unknown"
	}
	if len(operationID) > 64 {
		operationID = operationID[:64]
	}
	for _, char := range operationID {
		if (char < 'a' || char > 'z') && (char < 'A' || char > 'Z') &&
			(char < '0' || char > '9') && char != '-' && char != '_' {
			return "invalid"
		}
	}
	return operationID
}

func (p *Prewarmer) reportLifecycle(
	operationID string,
	class PrewarmCycleClass,
	timezones []string,
	startedAt time.Time,
	err error,
	reportSuccess bool,
) {
	if err == nil && !reportSuccess {
		return
	}
	outcome := PrewarmCycleSuccess
	if err != nil {
		outcome = prewarmCycleOutcomeForError(err)
	}
	duration := time.Since(startedAt)
	p.reportingMu.Lock()
	providerID, providerVersion := p.reportingProvider, p.reportingVersion
	p.reportingMu.Unlock()
	for _, timezone := range timezones {
		p.options.Metrics.RecordCycle(string(class), timezone, string(outcome), duration)
		p.options.Reporter.ReportPrewarmBackground(PrewarmBackgroundEvent{
			OperationID: operationID, ProviderID: providerID, ProviderVersion: providerVersion,
			Timezone: timezone, Class: class, Outcome: outcome, Duration: duration,
		})
	}
}

func prewarmLifecycleTargets(class PrewarmCycleClass, timezones []string) []prewarmLifecycleTarget {
	targets := make([]prewarmLifecycleTarget, 0, len(timezones))
	for _, timezone := range timezones {
		targets = append(targets, prewarmLifecycleTarget{class: class, timezone: timezone})
	}
	return targets
}

func (p *Prewarmer) reportScheduledLifecycle(
	operationID string,
	targets []prewarmLifecycleTarget,
	startedAt time.Time,
	err error,
) {
	if err == nil {
		return
	}
	exact := make(map[prewarmLifecycleTarget]*prewarmLifecycleFailure)
	var globalErrors []error
	partitionPrewarmLifecycleError(err, exact, &globalErrors)
	duration := time.Since(startedAt)
	reported := make(map[prewarmLifecycleTarget]struct{}, len(exact))
	for _, target := range targets {
		if failure, ok := exact[target]; ok {
			p.reportExactLifecycleFailure(operationID, target, failure, duration)
			reported[target] = struct{}{}
			continue
		}
		if len(globalErrors) > 0 {
			p.reportGlobalLifecycleFailure(operationID, target, errors.Join(globalErrors...), duration)
			reported[target] = struct{}{}
		}
	}
	for target, failure := range exact {
		if _, ok := reported[target]; ok {
			continue
		}
		p.reportExactLifecycleFailure(operationID, target, failure, duration)
	}
}

func partitionPrewarmLifecycleError(
	err error,
	exact map[prewarmLifecycleTarget]*prewarmLifecycleFailure,
	globalErrors *[]error,
) {
	if err == nil {
		return
	}
	if failure, ok := err.(*prewarmLifecycleFailure); ok {
		if previous, exists := exact[failure.target]; exists {
			copy := *previous
			copy.cycleRecorded = previous.cycleRecorded || failure.cycleRecorded
			copy.err = errors.Join(previous.err, failure.err)
			exact[failure.target] = &copy
		} else {
			exact[failure.target] = failure
		}
		return
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		for _, child := range joined.Unwrap() {
			partitionPrewarmLifecycleError(child, exact, globalErrors)
		}
		return
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok && containsPrewarmLifecycleFailure(wrapped.Unwrap()) {
		partitionPrewarmLifecycleError(wrapped.Unwrap(), exact, globalErrors)
		return
	}
	*globalErrors = append(*globalErrors, err)
}

func containsPrewarmLifecycleFailure(err error) bool {
	if err == nil {
		return false
	}
	if _, ok := err.(*prewarmLifecycleFailure); ok {
		return true
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		for _, child := range joined.Unwrap() {
			if containsPrewarmLifecycleFailure(child) {
				return true
			}
		}
		return false
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		return containsPrewarmLifecycleFailure(wrapped.Unwrap())
	}
	return false
}

func (p *Prewarmer) reportExactLifecycleFailure(
	operationID string,
	target prewarmLifecycleTarget,
	failure *prewarmLifecycleFailure,
	duration time.Duration,
) {
	outcome := prewarmCycleOutcomeForError(failure.err)
	if !failure.cycleRecorded {
		p.options.Metrics.RecordCycle(string(target.class), target.timezone, string(outcome), duration)
	}
	p.reportBackgroundEvent(operationID, target, outcome, duration)
}

func (p *Prewarmer) reportGlobalLifecycleFailure(
	operationID string,
	target prewarmLifecycleTarget,
	err error,
	duration time.Duration,
) {
	outcome := prewarmCycleOutcomeForError(err)
	p.options.Metrics.RecordCycle(string(target.class), target.timezone, string(outcome), duration)
	p.reportBackgroundEvent(operationID, target, outcome, duration)
}

func prewarmCycleOutcomeForError(err error) PrewarmCycleOutcome {
	switch prewarmTelemetryOutcome(err) {
	case "canceled":
		return PrewarmCycleCanceled
	case "rejected":
		return PrewarmCycleRejected
	default:
		return PrewarmCycleError
	}
}

func (p *Prewarmer) reportBackgroundEvent(
	operationID string,
	target prewarmLifecycleTarget,
	outcome PrewarmCycleOutcome,
	duration time.Duration,
) {
	p.reportingMu.Lock()
	providerID, providerVersion := p.reportingProvider, p.reportingVersion
	p.reportingMu.Unlock()
	p.options.Reporter.ReportPrewarmBackground(PrewarmBackgroundEvent{
		OperationID: operationID, ProviderID: providerID, ProviderVersion: providerVersion,
		Timezone: target.timezone, Class: target.class, Outcome: outcome, Duration: duration,
	})
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

func withPrewarmControllingLeases(ctx context.Context, claims ...PrewarmLeaseClaim) context.Context {
	return context.WithValue(ctx, prewarmCoordinatorLeaseContextKey{}, append([]PrewarmLeaseClaim(nil), claims...))
}

func prewarmControllingLeaseClaims(ctx context.Context) []PrewarmLeaseClaim {
	claims, _ := ctx.Value(prewarmCoordinatorLeaseContextKey{}).([]PrewarmLeaseClaim)
	return append([]PrewarmLeaseClaim(nil), claims...)
}

func (p *Prewarmer) requireCoordinatorOwned(ctx context.Context) error {
	return requirePrewarmCoordinatorOwned(ctx, p.cache)
}

func requirePrewarmCoordinatorOwned(ctx context.Context, cache *PrewarmCache) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	claims := prewarmControllingLeaseClaims(ctx)
	if len(claims) == 0 {
		return nil
	}
	for _, claim := range claims {
		commandCtx, cancel := context.WithTimeout(ctx, cache.options.LeaseTimeout)
		value, err := cache.store.Get(commandCtx, claim.Key)
		cancel()
		if errors.Is(err, readcache.ErrMiss) {
			return errPrewarmLeaseLost
		}
		if err != nil {
			return fmt.Errorf("check team usage prewarm coordinator lease: %w", err)
		}
		if !bytes.Equal(value, []byte(claim.Token)) {
			return errPrewarmLeaseLost
		}
	}
	return nil
}

func (p *Prewarmer) segmentLeaseKey(binding ProviderBinding, timezone, anchorDate, class string) string {
	return prewarmSegmentLeaseKey(p.cache, binding, timezone, anchorDate, class)
}

func prewarmSegmentLeaseKey(cache *PrewarmCache, binding ProviderBinding, timezone, anchorDate, refreshClass string) string {
	return cache.LeaseKey(
		"segment", strconv.Itoa(binding.ProviderID), strconv.FormatInt(binding.ProviderVersion, 10),
		prewarmTimezoneDigest(timezone), anchorDate, refreshClass,
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
