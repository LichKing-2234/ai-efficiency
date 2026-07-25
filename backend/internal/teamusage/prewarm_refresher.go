package teamusage

import (
	"context"
	"errors"
	"fmt"
	"time"
)

const (
	prewarmRefreshCycleTimeout  = 5 * time.Minute
	prewarmRefreshLeaseTTL      = 6 * time.Minute
	prewarmRefreshSourceTimeout = 80 * time.Second
	prewarmLocalSourceLimit     = 2
)

var errPrewarmRefreshLeaseLost = errors.New("team usage prewarm refresh lease ownership was lost")

type Refresher interface {
	Refresh(context.Context) error
}

type RefresherOptions struct {
	Timezones       []string
	Now             func() time.Time
	NewToken        func() string
	NewGenerationID func() string
	CycleTimeout    time.Duration
	SourceTimeout   time.Duration
	Metrics         PrewarmMetrics
	Reporter        RefreshReporter
}

type refresher struct {
	resolver PrimaryProviderBindingResolver
	cache    *PrewarmCache
	source   *PrewarmSource

	timezones    []string
	now          func() time.Time
	newToken     func() string
	cycleTimeout time.Duration
	reporter     RefreshReporter
}

type refreshCycleResult struct {
	planned      int
	published    int
	sourceCounts map[PrewarmSourceClass]int
}

type refreshLane struct {
	identity PrewarmCacheIdentity
	manifest PrewarmManifest
	failures []error
}

func NewRefresher(
	resolver PrimaryProviderBindingResolver,
	cache *PrewarmCache,
	options RefresherOptions,
) (Refresher, error) {
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
	if options.CycleTimeout <= 0 {
		options.CycleTimeout = prewarmRefreshCycleTimeout
	}
	if options.CycleTimeout >= prewarmRefreshLeaseTTL {
		return nil, fmt.Errorf("team usage prewarm refresh cycle timeout must be shorter than refresh lease TTL")
	}
	if options.SourceTimeout <= 0 {
		options.SourceTimeout = prewarmRefreshSourceTimeout
	}
	if options.SourceTimeout >= options.CycleTimeout {
		return nil, fmt.Errorf("team usage prewarm source timeout must be shorter than refresh cycle timeout")
	}
	options.Metrics = prewarmMetricsOrNoop(options.Metrics)
	options.Reporter = refreshReporterOrNoop(options.Reporter)
	limiter := &localSourceCallLimiter{
		semaphore: make(chan struct{}, prewarmLocalSourceLimit),
		timeout:   options.SourceTimeout,
	}
	source, err := NewPrewarmSource(limiter, PrewarmSourceOptions{
		Now: options.Now, NewGenerationID: options.NewGenerationID, Metrics: options.Metrics,
	})
	if err != nil {
		return nil, err
	}
	return &refresher{
		resolver: resolver, cache: cache, source: source, timezones: timezones,
		now: options.Now, newToken: options.NewToken, cycleTimeout: options.CycleTimeout,
		reporter: options.Reporter,
	}, nil
}

func (r *refresher) Refresh(parent context.Context) (err error) {
	startedAt := time.Now()
	result := refreshCycleResult{sourceCounts: make(map[PrewarmSourceClass]int)}
	owned := false
	defer func() {
		outcome := PrewarmRefreshSuccess
		switch {
		case err != nil && result.published > 0:
			outcome = PrewarmRefreshPartial
		case err != nil:
			outcome = PrewarmRefreshError
		case !owned || result.planned == 0:
			outcome = PrewarmRefreshSkipped
		}
		r.reporter.ReportRefresh(RefreshReport{
			Outcome: outcome, Duration: time.Since(startedAt), PlannedLanes: result.planned,
			PublishedLanes: result.published, SourceCounts: result.sourceCounts,
		})
	}()

	ctx, cancel := context.WithTimeout(parent, r.cycleTimeout)
	defer cancel()

	binding, err := r.resolver.ResolvePrimaryProviderBinding(ctx)
	if err != nil {
		return fmt.Errorf("resolve primary provider binding: %w", err)
	}
	if err := validateProviderBinding(binding); err != nil {
		return err
	}
	token := r.newToken()
	leaseKey := r.cache.RefreshLeaseKey()
	owned, err = r.cache.TryAcquireLease(ctx, leaseKey, token, prewarmRefreshLeaseTTL)
	if err != nil {
		return fmt.Errorf("acquire refresh lease: %w", err)
	}
	if !owned {
		return nil
	}
	defer r.releaseLease(leaseKey, token)

	cycleTime := r.now().UTC()
	result, err = r.refreshOwned(ctx, binding, cycleTime, leaseKey, token)
	return err
}

func (r *refresher) refreshOwned(
	ctx context.Context,
	binding ProviderBinding,
	cycleTime time.Time,
	leaseKey, token string,
) (refreshCycleResult, error) {
	result := refreshCycleResult{sourceCounts: make(map[PrewarmSourceClass]int)}
	lanes, planningFailures := r.planLanes(ctx, binding, cycleTime)
	result.planned = len(lanes)
	if len(lanes) == 0 {
		return result, errors.Join(planningFailures...)
	}

	result.sourceCounts[PrewarmSourceDirectory]++
	result.sourceCounts[PrewarmSourceCurrentStats]++
	current, err := r.source.BuildCurrentStats(ctx, binding)
	if err != nil {
		return result, errors.Join(append(planningFailures, fmt.Errorf("build provider-wide current stats: %w", err))...)
	}
	currentRef, err := r.cache.WriteCurrentStats(ctx, current)
	if err != nil {
		return result, errors.Join(append(planningFailures, fmt.Errorf("write provider-wide current stats: %w", err))...)
	}
	for _, lane := range lanes {
		lane.manifest.CurrentStats = currentRef
	}

	laneClasses := make([][]PrewarmSegmentClass, len(lanes))
	for index, lane := range lanes {
		laneClasses[index] = append(laneClasses[index], SegmentTodayHour)
		if lane.manifest.History6d.Key == "" {
			laneClasses[index] = append(laneClasses[index], SegmentHistory6d)
		}
		if lane.manifest.History29d.Key == "" {
			laneClasses[index] = append(laneClasses[index], SegmentHistory29d)
		}
		for _, class := range laneClasses[index] {
			result.sourceCounts[prewarmSourceClassForSegment(class)]++
		}
	}
	laneResults := make(chan int, len(lanes))
	for index := range lanes {
		index := index
		go func() {
			r.refreshLaneSources(ctx, binding, lanes[index], laneClasses[index])
			laneResults <- index
		}()
	}

	failures := append([]error(nil), planningFailures...)
	publicationFenced := false
	var publicationFenceErr error
	for range lanes {
		lane := lanes[<-laneResults]
		if len(lane.failures) > 0 {
			failures = append(failures, errors.Join(lane.failures...))
			continue
		}
		if !refreshManifestReferencesPresent(lane.manifest) {
			failures = append(failures, fmt.Errorf(
				"timezone %s prewarm manifest is incomplete", lane.identity.Timezone,
			))
			continue
		}
		if publicationFenced {
			failures = append(failures, fmt.Errorf(
				"skip timezone %s prewarm publication after cycle fence: %w",
				lane.identity.Timezone, publicationFenceErr,
			))
			continue
		}
		currentBinding, resolveErr := r.resolver.ResolvePrimaryProviderBinding(ctx)
		if resolveErr != nil {
			failures = append(failures, fmt.Errorf(
				"re-resolve primary provider before timezone %s publication: %w",
				lane.identity.Timezone, resolveErr,
			))
			continue
		}
		if currentBinding.ProviderID != binding.ProviderID || currentBinding.ProviderVersion != binding.ProviderVersion {
			publicationFenced = true
			publicationFenceErr = fmt.Errorf(
				"primary Relay provider version changed before timezone %s prewarm publication",
				lane.identity.Timezone,
			)
			failures = append(failures, publicationFenceErr)
			continue
		}
		if err := ctx.Err(); err != nil {
			failures = append(failures, err)
			continue
		}
		lane.manifest.CreatedAt = r.now().UTC()
		published, publishErr := r.cache.PublishManifest(ctx, leaseKey, token, lane.manifest)
		if publishErr != nil {
			failures = append(failures, fmt.Errorf(
				"publish timezone %s prewarm manifest: %w", lane.identity.Timezone, publishErr,
			))
			continue
		}
		if !published {
			publicationFenced = true
			publicationFenceErr = fmt.Errorf(
				"publish timezone %s prewarm manifest: %w", lane.identity.Timezone, errPrewarmRefreshLeaseLost,
			)
			failures = append(failures, publicationFenceErr)
			continue
		}
		result.published++
	}
	if len(failures) > 0 {
		return result, errors.Join(failures...)
	}
	return result, nil
}

func (r *refresher) refreshLaneSources(
	ctx context.Context,
	binding ProviderBinding,
	lane *refreshLane,
	classes []PrewarmSegmentClass,
) {
	for _, class := range classes {
		segment, err := r.source.FetchSegment(
			ctx, binding, lane.identity.Timezone, lane.identity.AnchorDate, class,
		)
		if err == nil {
			var reference PrewarmValueReference
			reference, err = r.cache.WriteSegment(ctx, segment)
			if err == nil {
				switch class {
				case SegmentHistory29d:
					lane.manifest.History29d = reference
				case SegmentHistory6d:
					lane.manifest.History6d = reference
				case SegmentTodayHour:
					lane.manifest.TodayHour = reference
				}
			}
		}
		if err != nil {
			lane.failures = append(lane.failures, fmt.Errorf(
				"refresh %s for timezone %s: %w", class, lane.identity.Timezone, err,
			))
		}
	}
}

func (r *refresher) planLanes(
	ctx context.Context,
	binding ProviderBinding,
	cycleTime time.Time,
) ([]*refreshLane, []error) {
	lanes := make([]*refreshLane, 0, len(r.timezones))
	failures := make([]error, 0, len(r.timezones))
	for _, timezone := range r.timezones {
		anchorDate, err := refresherLocalAnchorDate(timezone, cycleTime)
		if err != nil {
			failures = append(failures, fmt.Errorf("derive timezone %s anchor: %w", timezone, err))
			continue
		}
		safe, err := SplitSafe(timezone, anchorDate)
		if err != nil {
			failures = append(failures, fmt.Errorf("check timezone %s split safety: %w", timezone, err))
			continue
		}
		if !safe {
			continue
		}
		identity := PrewarmCacheIdentity{
			ProviderID: binding.ProviderID, ProviderVersion: binding.ProviderVersion,
			Timezone: timezone, AnchorDate: anchorDate,
		}
		previous, found, err := r.cache.Read(ctx, identity)
		if err != nil {
			failures = append(failures, fmt.Errorf("read timezone %s prewarm manifest: %w", timezone, err))
			continue
		}
		lane := &refreshLane{
			identity: identity,
			manifest: newRefreshManifestCandidate(identity, cycleTime),
		}
		if found {
			if reusableHistoryReference(cycleTime, r.cache.options.WriteTimeout, previous.History29dStatus, previous.Manifest.History29d) {
				lane.manifest.History29d = previous.Manifest.History29d
			}
			if reusableHistoryReference(cycleTime, r.cache.options.WriteTimeout, previous.History6dStatus, previous.Manifest.History6d) {
				lane.manifest.History6d = previous.Manifest.History6d
			}
		}
		lanes = append(lanes, lane)
	}
	return lanes, failures
}

func reusableHistoryReference(
	cycleTime time.Time,
	commandMargin time.Duration,
	status PrewarmValueStatus,
	reference PrewarmValueReference,
) bool {
	if status != PrewarmValueFresh && status != PrewarmValueStale {
		return false
	}
	return cycleTime.Add(manifestTTL + commandMargin).Before(reference.HardExpiresAt)
}

func prewarmSourceClassForSegment(class PrewarmSegmentClass) PrewarmSourceClass {
	switch class {
	case SegmentHistory29d:
		return PrewarmSourceHistory29d
	case SegmentHistory6d:
		return PrewarmSourceHistory6d
	default:
		return PrewarmSourceTodayHour
	}
}

func newRefreshManifestCandidate(identity PrewarmCacheIdentity, createdAt time.Time) PrewarmManifest {
	return PrewarmManifest{
		SchemaVersion: prewarmCacheSchemaVersion, ProviderID: identity.ProviderID,
		ProviderVersion: identity.ProviderVersion, Timezone: identity.Timezone,
		TimezoneDigest: prewarmTimezoneDigest(identity.Timezone), AnchorDate: identity.AnchorDate,
		CreatedAt: createdAt,
	}
}

func refreshManifestReferencesPresent(manifest PrewarmManifest) bool {
	return manifest.CurrentStats.Key != "" && manifest.History29d.Key != "" &&
		manifest.History6d.Key != "" && manifest.TodayHour.Key != ""
}

func refresherLocalAnchorDate(timezone string, value time.Time) (string, error) {
	location, err := loadPrewarmLocation(timezone)
	if err != nil {
		return "", err
	}
	return value.In(location).Format(time.DateOnly), nil
}

func (r *refresher) releaseLease(key, token string) {
	_, _ = r.cache.ReleaseLease(context.Background(), key, token)
}
