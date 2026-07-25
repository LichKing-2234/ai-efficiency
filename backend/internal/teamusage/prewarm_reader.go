package teamusage

import (
	"context"
	"fmt"
	"time"
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
	defer func() {
		if r == nil || r.metrics == nil {
			return
		}
		r.metrics.RecordRequest(outcome)
	}()
	if r == nil || r.cache == nil {
		return nil, PrewarmReadFallback, fmt.Errorf("team usage prewarm reader is not configured")
	}
	if request.ProviderID <= 0 || request.ProviderVersion <= 0 {
		return nil, PrewarmReadFallback, fmt.Errorf("valid authorized team usage prewarm request is required")
	}
	window, recognized, err := RecognizePrewarmWindow(request.Params, r.now())
	if err != nil {
		return nil, PrewarmReadInvalid, err
	}
	if !recognized {
		return nil, PrewarmReadIneligible, nil
	}
	result, found, err := r.cache.ReadWindow(ctx, PrewarmCacheIdentity{
		ProviderID:      request.ProviderID,
		ProviderVersion: request.ProviderVersion,
		Timezone:        window.Coverage.Timezone,
		AnchorDate:      window.AnchorDate,
	}, window.Class)
	if err != nil {
		return nil, PrewarmReadFallback, err
	}
	if !found || result == nil {
		return nil, PrewarmReadMiss, nil
	}
	if !result.Complete {
		return nil, PrewarmReadInvalid, nil
	}
	origin, eligible, _, err := composePrewarmedOriginWithUnion(
		window,
		*result.CurrentStats,
		result.Segments,
		request.AuthorizedRelayUserIDs,
	)
	if err != nil {
		return nil, PrewarmReadFallback, err
	}
	if !eligible {
		return nil, PrewarmReadFallback, nil
	}
	return origin, PrewarmReadFullHit, nil
}
