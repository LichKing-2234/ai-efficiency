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
}

type PrewarmReader struct {
	cache     *PrewarmCache
	source    *PrewarmSource
	timezones []string
	now       func() time.Time
	newToken  func() string
	flights   readcache.FlightGroup[PrewarmTrendSegment]
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
		Now: options.Now, NewGenerationID: options.NewGenerationID,
	})
	if err != nil {
		return nil, err
	}
	return &PrewarmReader{
		cache: cache, source: source, timezones: timezones, now: options.Now, newToken: options.NewToken,
	}, nil
}

func (r *PrewarmReader) ReadAuthorizedOrigin(
	ctx context.Context,
	request PrewarmReadRequest,
) (*teamUsageScopeOrigin, PrewarmReadOutcome, error) {
	if r == nil || r.cache == nil || r.source == nil {
		return nil, PrewarmReadFallback, fmt.Errorf("team usage prewarm reader is not configured")
	}
	if request.ProviderID <= 0 || request.ActorUserID <= 0 || request.ProviderVersion <= 0 ||
		strings.TrimSpace(request.ScopeVersion) == "" || request.Provider == nil {
		return nil, PrewarmReadFallback, fmt.Errorf("valid authorized team usage prewarm request is required")
	}
	if !containsPrewarmTimezone(r.timezones, request.Params.Timezone) {
		return nil, PrewarmReadIneligible, nil
	}
	window, recognized, err := RecognizePrewarmWindow(request.Params, r.now())
	if err != nil {
		return nil, PrewarmReadFallback, err
	}
	if !recognized {
		return nil, PrewarmReadIneligible, nil
	}
	safe, err := SplitSafe(window.Coverage.Timezone, window.AnchorDate)
	if err != nil {
		return nil, PrewarmReadFallback, err
	}
	if !safe {
		return nil, PrewarmReadIneligible, nil
	}
	identity := PrewarmCacheIdentity{
		ProviderID: request.ProviderID, ProviderVersion: request.ProviderVersion,
		Timezone: window.Coverage.Timezone, AnchorDate: window.AnchorDate,
	}
	result, found, err := r.cache.Read(ctx, identity)
	if err != nil {
		return nil, PrewarmReadFallback, err
	}
	if !found || result == nil {
		return nil, PrewarmReadMiss, nil
	}
	if result.Complete {
		origin, eligible, composeErr := ComposePrewarmedOrigin(window, *result.CurrentStats, result.Segments, request.AuthorizedRelayUserIDs)
		if composeErr != nil {
			return nil, PrewarmReadFallback, composeErr
		}
		if !eligible {
			return nil, PrewarmReadFallback, nil
		}
		return origin, PrewarmReadFullHit, nil
	}
	if !prewarmResultCanRecoverToday(result) {
		return nil, PrewarmReadFallback, nil
	}
	rosterEligible, err := prewarmCurrentRosterCoversAuthorized(*result.CurrentStats, request.AuthorizedRelayUserIDs)
	if err != nil {
		return nil, PrewarmReadFallback, err
	}
	if !rosterEligible {
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
		return nil, PrewarmReadFallback, err
	}
	segments := result.Segments
	segments.TodayHour = &today
	origin, eligible, err := ComposePrewarmedOrigin(window, *result.CurrentStats, segments, request.AuthorizedRelayUserIDs)
	if err != nil {
		return nil, PrewarmReadFallback, err
	}
	if !eligible {
		return nil, PrewarmReadFallback, nil
	}
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

func prewarmResultCanRecoverToday(result *PrewarmCacheResult) bool {
	if result == nil || result.CurrentStats == nil || result.Segments.History29d == nil || result.Segments.History6d == nil {
		return false
	}
	available := func(status PrewarmValueStatus) bool {
		return status == PrewarmValueFresh || status == PrewarmValueStale
	}
	if !available(result.CurrentStatsStatus) || !available(result.History29dStatus) || !available(result.History6dStatus) {
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
		return PrewarmTrendSegment{}, fmt.Errorf("acquire request today segment lease: %w", err)
	}
	if !acquired {
		return PrewarmTrendSegment{}, errPrewarmLeaseBusy
	}
	defer func() {
		releaseCtx, cancel := context.WithTimeout(context.Background(), prewarmDefaultCommandTimeout)
		released, releaseErr := r.cache.ReleaseLease(releaseCtx, leaseKey, token)
		cancel()
		if releaseErr != nil {
			err = errors.Join(err, fmt.Errorf("release request today segment lease: %w", releaseErr))
		} else if !released {
			err = errors.Join(err, errPrewarmLeaseLost)
		}
	}()

	today, err = r.source.FetchSegment(ctx, ProviderBinding{
		ProviderID: request.ProviderID, ProviderVersion: request.ProviderVersion, Provider: request.Provider,
	}, identity.Timezone, identity.AnchorDate, SegmentTodayHour)
	if err != nil {
		return PrewarmTrendSegment{}, err
	}
	todayRef, err := r.cache.WriteSegment(ctx, today)
	if err != nil {
		return PrewarmTrendSegment{}, fmt.Errorf("write recovered request today segment: %w", err)
	}
	manifest := newPrewarmManifestCandidate(identity, result, r.now())
	manifest.TodayHour = todayRef
	published, err := r.cache.PublishManifestWithLeases(ctx, []PrewarmLeaseClaim{{Key: leaseKey, Token: token}}, manifest)
	if err != nil {
		return PrewarmTrendSegment{}, fmt.Errorf("publish recovered request today manifest: %w", err)
	}
	if !published {
		return PrewarmTrendSegment{}, errPrewarmLeaseLost
	}
	return today, nil
}
