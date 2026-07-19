package relay

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ai-efficiency/backend/internal/readcache"
)

const (
	teamTrendCacheTTL      = time.Minute
	teamTrendCacheCapacity = 4096
	teamTrendFlightTimeout = 20 * time.Second
)

type teamTrendCacheKey struct {
	RelayUserID int64
	StartDate   string
	EndDate     string
	Granularity string
	Timezone    string
}

type teamTrendCacheEntry struct {
	Points    []UsageTrendPoint
	ExpiresAt time.Time
}

type teamTrendCache struct {
	mu         sync.Mutex
	entries    map[teamTrendCacheKey]teamTrendCacheEntry
	generation uint64
	flights    readcache.FlightGroup[[]UsageTrendPoint]
	now        func() time.Time
}

func (c *teamTrendCache) GetOrLoad(
	ctx context.Context,
	relayUserID int64,
	params TeamMemberTrendParams,
	load func(context.Context) ([]UsageTrendPoint, error),
) ([]UsageTrendPoint, error) {
	if load == nil {
		return nil, fmt.Errorf("team trend origin loader is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if relayUserID <= 0 {
		return load(ctx)
	}

	key := normalizedTeamTrendCacheKey(relayUserID, params)
	generation, points, found := c.readCurrent(key)
	if found {
		return points, nil
	}

	points, err := c.flights.Do(ctx, key.flightKey(generation), teamTrendFlightTimeout, func(loadCtx context.Context) ([]UsageTrendPoint, error) {
		if cached, ok := c.readGeneration(key, generation); ok {
			return cached, nil
		}
		loaded, loadErr := load(loadCtx)
		if loadErr != nil {
			return nil, loadErr
		}
		c.store(key, generation, loaded)
		return cloneUsageTrendPoints(loaded), nil
	})
	if err != nil {
		return nil, err
	}
	return cloneUsageTrendPoints(points), nil
}

func (c *teamTrendCache) Invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.generation++
	c.entries = nil
}

func normalizedTeamTrendCacheKey(relayUserID int64, params TeamMemberTrendParams) teamTrendCacheKey {
	return teamTrendCacheKey{
		RelayUserID: relayUserID,
		StartDate:   strings.TrimSpace(params.StartDate),
		EndDate:     strings.TrimSpace(params.EndDate),
		Granularity: strings.TrimSpace(params.Granularity),
		Timezone:    strings.TrimSpace(params.Timezone),
	}
}

func (key teamTrendCacheKey) flightKey(generation uint64) string {
	query := url.Values{}
	query.Set("user_id", strconv.FormatInt(key.RelayUserID, 10))
	query.Set("start_date", key.StartDate)
	query.Set("end_date", key.EndDate)
	query.Set("granularity", key.Granularity)
	query.Set("timezone", key.Timezone)
	return strconv.FormatUint(generation, 10) + ":" + query.Encode()
}

func (c *teamTrendCache) readCurrent(key teamTrendCacheKey) (uint64, []UsageTrendPoint, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	generation := c.generation
	points, found := c.readLocked(key, c.currentTime())
	return generation, points, found
}

func (c *teamTrendCache) readGeneration(key teamTrendCacheKey, generation uint64) ([]UsageTrendPoint, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.generation != generation {
		return nil, false
	}
	return c.readLocked(key, c.currentTime())
}

func (c *teamTrendCache) readLocked(key teamTrendCacheKey, now time.Time) ([]UsageTrendPoint, bool) {
	entry, found := c.entries[key]
	if !found {
		return nil, false
	}
	if !now.Before(entry.ExpiresAt) {
		delete(c.entries, key)
		return nil, false
	}
	return cloneUsageTrendPoints(entry.Points), true
}

func (c *teamTrendCache) store(key teamTrendCacheKey, generation uint64, points []UsageTrendPoint) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.generation != generation {
		return
	}
	now := c.currentTime()
	if c.entries == nil {
		c.entries = make(map[teamTrendCacheKey]teamTrendCacheEntry)
	}
	for cachedKey, entry := range c.entries {
		if !now.Before(entry.ExpiresAt) {
			delete(c.entries, cachedKey)
		}
	}
	if _, exists := c.entries[key]; !exists && len(c.entries) >= teamTrendCacheCapacity {
		c.evictEarliestLocked()
	}
	c.entries[key] = teamTrendCacheEntry{
		Points:    cloneUsageTrendPoints(points),
		ExpiresAt: now.Add(teamTrendCacheTTL),
	}
}

func (c *teamTrendCache) evictEarliestLocked() {
	var earliestKey teamTrendCacheKey
	var earliestExpiry time.Time
	found := false
	for key, entry := range c.entries {
		if !found || entry.ExpiresAt.Before(earliestExpiry) {
			earliestKey = key
			earliestExpiry = entry.ExpiresAt
			found = true
		}
	}
	if found {
		delete(c.entries, earliestKey)
	}
}

func (c *teamTrendCache) currentTime() time.Time {
	if c.now != nil {
		return c.now().UTC()
	}
	return time.Now().UTC()
}

func cloneUsageTrendPoints(points []UsageTrendPoint) []UsageTrendPoint {
	if points == nil {
		return nil
	}
	cloned := make([]UsageTrendPoint, len(points))
	for index, point := range points {
		cloned[index] = point
		if point.TotalTokens != nil {
			totalTokens := *point.TotalTokens
			cloned[index].TotalTokens = &totalTokens
		}
	}
	return cloned
}
