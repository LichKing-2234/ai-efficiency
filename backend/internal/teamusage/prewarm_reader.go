package teamusage

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ai-efficiency/backend/internal/readcache"
	"github.com/ai-efficiency/backend/internal/relay"
)

const prewarmPartialTodayTimeout = 85 * time.Second

type PrewarmReadOutcome string

const (
	PrewarmReadFullHit      PrewarmReadOutcome = "full_hit"
	PrewarmReadPartialToday PrewarmReadOutcome = "partial_today"
	PrewarmReadIneligible   PrewarmReadOutcome = "ineligible"
	PrewarmReadMiss         PrewarmReadOutcome = "miss"
	PrewarmReadFallback     PrewarmReadOutcome = "fallback"
)

type PrewarmReadRequest struct {
	ProviderID, ActorUserID int
	ProviderVersion         int64
	ScopeVersion            string
	Params                  OverviewParams
	AuthorizedRelayUserIDs  []int64
	Provider                relay.Provider
}

type PrewarmReaderOptions struct {
	Timezones       []string
	Now             func() time.Time
	NewToken        func() string
	NewGenerationID func() string
	Metrics         PrewarmMetrics
}

type PrewarmReader struct {
	cache     *PrewarmCache
	source    *PrewarmSource
	timezones []string
	now       func() time.Time
	newToken  func() string
	metrics   PrewarmMetrics
	flights   readcache.FlightGroup[PrewarmTrendSegment]
}

type prewarmRequestFailure struct {
	reason string
	err    error
}

func (e *prewarmRequestFailure) Error() string { return e.err.Error() }
func (e *prewarmRequestFailure) Unwrap() error { return e.err }

func wrapPrewarmRequestFailure(reason string, err error) error {
	if err == nil {
		return nil
	}
	return &prewarmRequestFailure{reason: reason, err: err}
}

func prewarmFallbackReasonForError(err error) string {
	var requestFailure *prewarmRequestFailure
	if errors.As(err, &requestFailure) {
		return requestFailure.reason
	}
	var sourceFailure *prewarmSourceFailure
	if errors.As(err, &sourceFailure) {
		if sourceFailure.kind == prewarmSourceFailureValidation {
			return "generation_invalid"
		}
		return "source_error"
	}
	return "generation_invalid"
}

func NewPrewarmReader(cache *PrewarmCache, limiter SourceCallLimiter, options PrewarmReaderOptions) (*PrewarmReader, error) {
	if cache == nil {
		return nil, fmt.Errorf("team usage prewarm reader cache is required")
	}
	timezones, err := NormalizePrewarmTimezones(options.Timezones)
	if err != nil {
		return nil, err
	}
	if len(timezones) == 0 {
		return nil, fmt.Errorf("at least one team usage prewarm reader timezone is required")
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.NewToken == nil {
		options.NewToken = newPrewarmRandomID
	}
	source, err := NewPrewarmSource(limiter, PrewarmSourceOptions{
		Now: options.Now, NewGenerationID: options.NewGenerationID, Metrics: options.Metrics,
	})
	if err != nil {
		return nil, err
	}
	return &PrewarmReader{
		cache: cache, source: source, timezones: timezones, now: options.Now, newToken: options.NewToken,
		metrics: prewarmMetricsOrNoop(options.Metrics),
	}, nil
}

func (r *PrewarmReader) SourceCallLimiter() SourceCallLimiter {
	if r == nil || r.source == nil {
		return nil
	}
	return r.source.limiter
}

func (r *PrewarmReader) ReadAuthorizedOrigin(
	ctx context.Context,
	request PrewarmReadRequest,
) (origin *teamUsageScopeOrigin, outcome PrewarmReadOutcome, err error) {
	fallbackReason := "none"
	defer func() {
		if r == nil || r.metrics == nil {
			return
		}
		r.metrics.RecordRequest(request.Params.Timezone, string(outcome), fallbackReason)
	}()
	if r == nil || r.cache == nil || r.source == nil {
		fallbackReason = "invalid_request"
		return nil, PrewarmReadFallback, fmt.Errorf("team usage prewarm reader is not configured")
	}
	if request.ProviderID <= 0 || request.ActorUserID <= 0 || request.ProviderVersion <= 0 ||
		strings.TrimSpace(request.ScopeVersion) == "" || request.Provider == nil {
		fallbackReason = "invalid_request"
		return nil, PrewarmReadFallback, fmt.Errorf("valid authorized team usage prewarm request is required")
	}
	if !containsPrewarmTimezone(r.timezones, request.Params.Timezone) {
		fallbackReason = "ineligible"
		return nil, PrewarmReadIneligible, nil
	}
	window, recognized, err := RecognizePrewarmWindow(request.Params, r.now())
	if err != nil {
		fallbackReason = "generation_invalid"
		return nil, PrewarmReadFallback, err
	}
	if !recognized {
		fallbackReason = "ineligible"
		return nil, PrewarmReadIneligible, nil
	}
	safe, err := SplitSafe(window.Coverage.Timezone, window.AnchorDate)
	if err != nil {
		fallbackReason = "generation_invalid"
		return nil, PrewarmReadFallback, err
	}
	if !safe {
		fallbackReason = "ineligible"
		return nil, PrewarmReadIneligible, nil
	}
	identity := PrewarmCacheIdentity{
		ProviderID: request.ProviderID, ProviderVersion: request.ProviderVersion,
		Timezone: window.Coverage.Timezone, AnchorDate: window.AnchorDate,
	}
	result, found, err := r.cache.ReadWindow(ctx, identity, window.Class)
	if err != nil {
		fallbackReason = "redis_error"
		return nil, PrewarmReadFallback, err
	}
	if !found || result == nil {
		fallbackReason = "cache_miss"
		return nil, PrewarmReadMiss, nil
	}
	if result.Complete {
		origin, eligible, unionUsers, composeErr := composePrewarmedOriginWithUnion(window, *result.CurrentStats, result.Segments, request.AuthorizedRelayUserIDs)
		if composeErr != nil {
			fallbackReason = "generation_invalid"
			return nil, PrewarmReadFallback, composeErr
		}
		if !eligible {
			fallbackReason = "roster_incomplete"
			return nil, PrewarmReadFallback, nil
		}
		r.metrics.RecordQuantity(PrewarmQuantityUnionUsers, window.Coverage.Timezone, unionUsers)
		return origin, PrewarmReadFullHit, nil
	}
	if !prewarmResultCanRecoverToday(result, window.Class) {
		fallbackReason = "generation_invalid"
		return nil, PrewarmReadFallback, nil
	}
	rosterEligible, err := prewarmCurrentRosterCoversAuthorized(*result.CurrentStats, request.AuthorizedRelayUserIDs)
	if err != nil {
		fallbackReason = "generation_invalid"
		return nil, PrewarmReadFallback, err
	}
	if !rosterEligible {
		fallbackReason = "roster_incomplete"
		return nil, PrewarmReadFallback, nil
	}

	flightKey := strings.Join([]string{
		strconv.Itoa(identity.ProviderID), strconv.FormatInt(identity.ProviderVersion, 10),
		prewarmTimezoneDigest(identity.Timezone), identity.AnchorDate, string(SegmentTodayHour),
	}, ":")
	today, err := r.flights.Do(ctx, flightKey, prewarmPartialTodayTimeout, func(flightCtx context.Context) (PrewarmTrendSegment, error) {
		return r.recoverToday(flightCtx, request, identity, result)
	})
	if err != nil {
		fallbackReason = prewarmFallbackReasonForError(err)
		return nil, PrewarmReadFallback, err
	}
	segments := result.Segments
	segments.TodayHour = &today
	origin, eligible, unionUsers, err := composePrewarmedOriginWithUnion(window, *result.CurrentStats, segments, request.AuthorizedRelayUserIDs)
	if err != nil {
		fallbackReason = "generation_invalid"
		return nil, PrewarmReadFallback, err
	}
	if !eligible {
		fallbackReason = "roster_incomplete"
		return nil, PrewarmReadFallback, nil
	}
	r.metrics.RecordQuantity(PrewarmQuantityUnionUsers, window.Coverage.Timezone, unionUsers)
	return origin, PrewarmReadPartialToday, nil
}

func prewarmCurrentRosterCoversAuthorized(current PrewarmCurrentStatsEnvelope, authorizedRelayUserIDs []int64) (bool, error) {
	stats, err := validatePrewarmCurrentStats(current)
	if err != nil {
		return false, err
	}
	authorized, err := normalizeAuthorizedRelayUserIDs(authorizedRelayUserIDs)
	if err != nil {
		return false, err
	}
	for _, relayUserID := range authorized {
		if _, ok := stats[relayUserID]; !ok {
			return false, nil
		}
	}
	return true, nil
}

func prewarmResultCanRecoverToday(result *PrewarmCacheResult, class PrewarmWindowClass) bool {
	if result == nil || result.CurrentStats == nil {
		return false
	}
	available := func(status PrewarmValueStatus) bool {
		return status == PrewarmValueFresh || status == PrewarmValueStale
	}
	if !available(result.CurrentStatsStatus) {
		return false
	}
	switch class {
	case PrewarmWindowToday:
	case PrewarmWindow7d:
		if result.Segments.History6d == nil || !available(result.History6dStatus) {
			return false
		}
	case PrewarmWindow30d:
		if result.Segments.History29d == nil || !available(result.History29dStatus) {
			return false
		}
	default:
		return false
	}
	return result.TodayHourStatus == PrewarmValueMissing || result.TodayHourStatus == PrewarmValueInvalid ||
		result.TodayHourStatus == PrewarmValueHardExpired
}

func (r *PrewarmReader) recoverToday(
	ctx context.Context,
	request PrewarmReadRequest,
	identity PrewarmCacheIdentity,
	result *PrewarmCacheResult,
) (today PrewarmTrendSegment, err error) {
	leaseKey := prewarmSegmentLeaseKey(r.cache, ProviderBinding{
		ProviderID: identity.ProviderID, ProviderVersion: identity.ProviderVersion, Provider: request.Provider,
	}, identity.Timezone, identity.AnchorDate, prewarmMovingRefreshClass)
	token := r.newToken()
	acquired, err := r.cache.TryAcquireLease(ctx, leaseKey, token, prewarmWorkerLeaseTTL)
	if err != nil {
		return PrewarmTrendSegment{}, wrapPrewarmRequestFailure("redis_error", fmt.Errorf("acquire request today segment lease: %w", err))
	}
	if !acquired {
		return PrewarmTrendSegment{}, wrapPrewarmRequestFailure("redis_error", errPrewarmLeaseBusy)
	}
	defer func() {
		releaseCtx, cancel := context.WithTimeout(context.Background(), prewarmDefaultCommandTimeout)
		released, releaseErr := r.cache.ReleaseLease(releaseCtx, leaseKey, token)
		cancel()
		if releaseErr != nil {
			err = errors.Join(err, wrapPrewarmRequestFailure("redis_error", fmt.Errorf("release request today segment lease: %w", releaseErr)))
		} else if !released {
			err = errors.Join(err, wrapPrewarmRequestFailure("redis_error", errPrewarmLeaseLost))
		}
	}()

	startedAt := time.Now()
	today, err = r.source.FetchSegment(ctx, ProviderBinding{
		ProviderID: request.ProviderID, ProviderVersion: request.ProviderVersion, Provider: request.Provider,
	}, identity.Timezone, identity.AnchorDate, SegmentTodayHour)
	sourceOutcome := "success"
	if err != nil {
		sourceOutcome = prewarmTelemetryOutcome(err)
	}
	r.metrics.RecordSource(
		string(SegmentTodayHour), identity.Timezone, sourceOutcome, time.Since(startedAt),
		int(today.ResponseBytes), today.PointCount, today.UniqueUserCount,
	)
	if err != nil {
		return PrewarmTrendSegment{}, err
	}
	todayRef, err := r.cache.WriteSegment(ctx, today)
	if err != nil {
		return PrewarmTrendSegment{}, wrapPrewarmRequestFailure("redis_error", fmt.Errorf("write recovered request today segment: %w", err))
	}
	manifest := newPrewarmManifestCandidate(identity, result, r.now())
	manifest.TodayHour = todayRef
	published, err := r.cache.PublishManifestWithLeases(ctx, []PrewarmLeaseClaim{{Key: leaseKey, Token: token}}, manifest)
	if err != nil {
		return PrewarmTrendSegment{}, wrapPrewarmRequestFailure("redis_error", fmt.Errorf("publish recovered request today manifest: %w", err))
	}
	if !published {
		return PrewarmTrendSegment{}, wrapPrewarmRequestFailure("redis_error", errPrewarmLeaseLost)
	}
	return today, nil
}
