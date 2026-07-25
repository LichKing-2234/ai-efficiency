package teamusage

import (
	"context"
	"fmt"
	"time"
)

type PrewarmReadOutcome string

const (
	PrewarmReadFullHit    PrewarmReadOutcome = "full_hit"
	PrewarmReadMiss       PrewarmReadOutcome = "miss"
	PrewarmReadIneligible PrewarmReadOutcome = "ineligible"
	PrewarmReadInvalid    PrewarmReadOutcome = "invalid"
	PrewarmReadFallback   PrewarmReadOutcome = "fallback"
)

type PrewarmReadRequest struct {
	ProviderID             int
	ProviderVersion        int64
	Params                 OverviewParams
	AuthorizedRelayUserIDs []int64
}

type PrewarmReaderOptions struct {
	Now     func() time.Time
	Metrics PrewarmMetrics
}

type PrewarmReader struct {
	cache   *PrewarmCache
	now     func() time.Time
	metrics PrewarmMetrics
}

func NewPrewarmReader(cache *PrewarmCache, options PrewarmReaderOptions) (*PrewarmReader, error) {
	if cache == nil {
		return nil, fmt.Errorf("team usage prewarm reader cache is required")
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	return &PrewarmReader{
		cache:   cache,
		now:     options.Now,
		metrics: prewarmMetricsOrNoop(options.Metrics),
	}, nil
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
	if r == nil || r.cache == nil {
		fallbackReason = "invalid_request"
		return nil, PrewarmReadFallback, fmt.Errorf("team usage prewarm reader is not configured")
	}
	if request.ProviderID <= 0 || request.ProviderVersion <= 0 {
		fallbackReason = "invalid_request"
		return nil, PrewarmReadFallback, fmt.Errorf("valid authorized team usage prewarm request is required")
	}
	window, recognized, err := RecognizePrewarmWindow(request.Params, r.now())
	if err != nil {
		fallbackReason = "generation_invalid"
		return nil, PrewarmReadInvalid, err
	}
	if !recognized {
		fallbackReason = "ineligible"
		return nil, PrewarmReadIneligible, nil
	}
	result, found, err := r.cache.ReadWindow(ctx, PrewarmCacheIdentity{
		ProviderID:      request.ProviderID,
		ProviderVersion: request.ProviderVersion,
		Timezone:        window.Coverage.Timezone,
		AnchorDate:      window.AnchorDate,
	}, window.Class)
	if err != nil {
		fallbackReason = "redis_error"
		return nil, PrewarmReadFallback, err
	}
	if !found || result == nil {
		fallbackReason = "cache_miss"
		return nil, PrewarmReadMiss, nil
	}
	if !result.Complete {
		fallbackReason = "generation_invalid"
		return nil, PrewarmReadInvalid, nil
	}
	origin, eligible, unionUsers, err := composePrewarmedOriginWithUnion(
		window,
		*result.CurrentStats,
		result.Segments,
		request.AuthorizedRelayUserIDs,
	)
	if err != nil {
		fallbackReason = "generation_invalid"
		return nil, PrewarmReadFallback, err
	}
	if !eligible {
		fallbackReason = "roster_incomplete"
		return nil, PrewarmReadFallback, nil
	}
	r.metrics.RecordQuantity(PrewarmQuantityUnionUsers, window.Coverage.Timezone, unionUsers)
	return origin, PrewarmReadFullHit, nil
}
