package kirocredits

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/internal/pkg"
	"go.uber.org/zap"
)

// keysFor returns the kiro2cc key list, preferring the shared Redis entry so
// every replica reuses one provider-version list instead of re-listing keys on
// each request.
func (s *Service) keysFor(ctx context.Context, client *adminClient, row *ent.RelayProvider) ([]adminKey, error) {
	if s.cache == nil {
		return s.listKeys(ctx, client, row)
	}
	return s.cache.Keys(ctx, row.ID, row.ConfigurationVersion, func(loadCtx context.Context) ([]adminKey, error) {
		return client.listKeys(loadCtx)
	})
}

// creditsValue performs the expensive kiro2cc read for one user and window. It
// is only called on a cache miss or a stale refresh.
func (s *Service) creditsValue(ctx context.Context, client *adminClient, row *ent.RelayProvider, relayUserID int64, window window, params Params) (cachedCredits, error) {
	value := cachedCredits{
		ProviderID:      row.ID,
		ProviderVersion: row.ConfigurationVersion,
		RelayUserID:     relayUserID,
		StartDate:       params.StartDate,
		EndDate:         params.EndDate,
		Granularity:     params.Granularity,
		Timezone:        params.Timezone,
		ByModel:         []ModelCredits{},
		ByDay:           []DayCredits{},
	}

	keys, err := s.keysFor(ctx, client, row)
	if err != nil {
		return cachedCredits{}, err
	}
	key, found, ambiguous := matchKey(keys, relayUserID)
	if !found {
		// "No key yet" is a valid, cacheable answer: it keeps a brand-new user
		// from paying the key listing on every reload.
		return value, nil
	}
	value.KeyFound = true
	value.Ambiguous = ambiguous

	summary, err := client.usageSummary(ctx, key.ID)
	if err != nil {
		return cachedCredits{}, err
	}
	value.LifetimeCredits = summary.TotalCredits
	value.LifetimeRequests = summary.TotalRequests

	aggregated, err := s.collectAggregate(ctx, client, key.ID, window, params.Granularity)
	if err != nil {
		return cachedCredits{}, err
	}
	value.Credits = aggregated.credits
	value.CreditsSaved = aggregated.creditsSaved
	value.Requests = aggregated.requests
	value.InputTokens = aggregated.inputTokens
	value.OutputTokens = aggregated.outputTokens
	value.ByModel = aggregated.byModel
	value.ByDay = aggregated.byDay
	value.MissingCredits = aggregated.missingCredits
	return value, nil
}

// cachedCreditsFor returns the per-user credits for one dashboard range through
// the Redis read model: fresh hit → no kiro2cc call; stale → one shared
// refresh; refresh failure → the stale value survives.
func (s *Service) cachedCreditsFor(ctx context.Context, client *adminClient, row *ent.RelayProvider, relayUserID int64, params Params) (cachedCredits, error) {
	window, err := parseWindow(params, s.now().UTC())
	if err != nil {
		return cachedCredits{}, err
	}
	key := s.cache.creditsKey(row.ID, row.ConfigurationVersion, relayUserID, params)
	return s.cache.GetOrLoad(ctx, key, func(loadCtx context.Context) (cachedCredits, error) {
		return s.creditsValue(loadCtx, client, row, relayUserID, window, params)
	})
}

func snapshotFromValue(value cachedCredits, now time.Time) Snapshot {
	snapshot := Snapshot{
		Configured: true,
		AsOf:       value.AsOf,
		ByModel:    value.ByModel,
		ByDay:      value.ByDay,
	}
	if snapshot.AsOf.IsZero() {
		snapshot.AsOf = now
	}
	if !value.KeyFound {
		snapshot.Status = StatusNoKey
		snapshot.Message = "尚未在该用户下产生 Kiro CLI 调用"
		return snapshot
	}
	snapshot.KeyFound = true
	snapshot.AmbiguousKey = value.Ambiguous
	snapshot.LifetimeCredits = value.LifetimeCredits
	snapshot.LifetimeRequests = value.LifetimeRequests
	snapshot.Credits = value.Credits
	snapshot.CreditsSaved = value.CreditsSaved
	snapshot.Requests = value.Requests
	snapshot.InputTokens = value.InputTokens
	snapshot.OutputTokens = value.OutputTokens
	snapshot.Partial = value.partial()
	snapshot.Available = true
	snapshot.Status = StatusOK
	return snapshot
}

// StartWarmer keeps the dashboard presets warm for every kiro2cc key, so a
// page load does not pay the record pagination cost. The short windows follow
// warmInterval; the long window costs far more kiro2cc pages, so it is
// refreshed on its own slower interval. It stops with ctx.
func (s *Service) StartWarmer(ctx context.Context) {
	if s == nil || s.cache == nil || s.warmInterval <= 0 {
		return
	}
	location, err := time.LoadLocation(s.warmTimezone)
	if err != nil {
		s.logger.Warn("kiro credits: warmer timezone invalid", zap.String("timezone", s.warmTimezone), zap.Error(err))
		location = time.UTC
	}
	// Warm once before the first tick: a rollout should not leave the
	// dashboards cold for a whole interval.
	s.warmOnce(ctx, location, true)
	lastLong := s.now()
	ticker := time.NewTicker(s.warmInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			includeLong := s.warmLongInterval > 0 && s.now().Sub(lastLong) >= s.warmLongInterval
			if includeLong {
				lastLong = s.now()
			}
			s.warmOnce(ctx, location, includeLong)
		}
	}
}

func (s *Service) warmOnce(ctx context.Context, location *time.Location, includeLong bool) {
	// One warming pass at a time: a heavy key takes minutes of pagination, so a
	// second replica warming in parallel only makes the pages it reads slower.
	leaseKey := s.cache.warmLeaseKey()
	if s.cache.store != nil {
		acquired, err := s.cache.store.TryAcquireLease(ctx, leaseKey, s.warmToken, warmLeaseTTL)
		if err != nil || !acquired {
			return
		}
		defer func() {
			_, _ = s.cache.store.ReleaseLease(context.WithoutCancel(ctx), leaseKey, s.warmToken)
		}()
	}
	row, err := s.source(ctx)
	if err != nil {
		return
	}
	token, err := pkg.Decrypt(row.AdminAPIKey, s.encryptionKey)
	if err != nil || strings.TrimSpace(token) == "" {
		return
	}
	client := newAdminClient(row.BaseURL, token, s.httpTimeout, warmCallTimeout)
	keys, err := s.keysFor(ctx, client, row)
	if err != nil {
		s.logger.Warn("kiro credits: warmer list keys", zap.Int("provider_id", row.ID), zap.Error(err))
		return
	}
	userIDs := relayUserIDsFromKeys(keys)
	if len(userIDs) == 0 {
		return
	}
	presets := warmPresets(s.now(), location, includeLong)
	sem := newSemaphore(warmConcurrency)
	var wg sync.WaitGroup
	for _, userID := range userIDs {
		for _, params := range presets {
			wg.Add(1)
			go func(relayUserID int64, preset Params) {
				defer wg.Done()
				if err := sem.acquire(ctx); err != nil {
					return
				}
				defer sem.release()
				window, err := parseWindow(preset, s.now().UTC())
				if err != nil {
					return
				}
				timeout := warmShortBudget
				if window.endExclusive.Sub(window.start) > longWindowSpan {
					timeout = warmLongBudget
				}
				loadCtx, cancel := context.WithTimeout(ctx, timeout)
				defer cancel()
				value, err := s.creditsValue(loadCtx, client, row, relayUserID, window, preset)
				if err != nil {
					s.logger.Warn("kiro credits: warmer refresh", zap.Int64("relay_user_id", relayUserID), zap.Error(err))
					return
				}
				// Publish even a lower bound: it carries missing_credits, so the
				// dashboard can mark it, and a fast lower bound beats a timeout.
				// (Only a truncated page walk used to be skipped, which left every
				// heavy key uncached and timing out on each visit.)
				s.cache.write(ctx, s.cache.creditsKey(row.ID, row.ConfigurationVersion, relayUserID, preset), value)
			}(userID, params)
		}
	}
	wg.Wait()
}

func warmPresets(now time.Time, location *time.Location, includeLong bool) []Params {
	local := now.In(location)
	today := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, location)
	ranges := []struct {
		start time.Time
		end   time.Time
	}{
		{today, today},
		{today.AddDate(0, 0, -6), today},
	}
	// The 30-day range is the default dashboard window; it also needs dozens of
	// kiro2cc pages per heavy key, so it is refreshed on warmLongInterval only.
	if includeLong {
		ranges = append(ranges, struct {
			start time.Time
			end   time.Time
		}{today.AddDate(0, 0, -29), today})
	}
	presets := make([]Params, 0, len(ranges))
	for _, item := range ranges {
		presets = append(presets, Params{
			StartDate:   item.start.Format(dateLayout),
			EndDate:     item.end.Format(dateLayout),
			Timezone:    location.String(),
			Granularity: "day",
		})
	}
	return presets
}
