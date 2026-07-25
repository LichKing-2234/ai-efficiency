package personalusage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"regexp"
	"time"

	"github.com/ai-efficiency/backend/internal/readcache"
	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/google/uuid"
)

const usageCacheSchemaVersion = 1

var usageCacheNamespaceRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,62}$`)

type Cache struct {
	store   readcache.Store
	options CacheOptions
	flights readcache.FlightGroup[*CacheResult]
}

type usagePayload struct {
	Range  relay.UserUsageDashboardRange  `json:"range"`
	Stats  *relay.UserUsageDashboardStats `json:"stats"`
	Trend  []relay.UserUsageTrendPoint    `json:"trend"`
	Models []relay.UserUsageModelStat     `json:"models"`
}

type usageValueEnvelope struct {
	SchemaVersion int          `json:"schema_version"`
	GeneratedAt   time.Time    `json:"generated_at"`
	FreshUntil    time.Time    `json:"fresh_until"`
	StaleUntil    time.Time    `json:"stale_until"`
	Usage         usagePayload `json:"usage"`
}

type cacheKeyDimensions struct {
	Namespace       string `json:"namespace"`
	ProviderID      int    `json:"provider_id"`
	ProviderVersion int64  `json:"provider_version"`
	ActorID         int    `json:"actor_id"`
	RelayUserID     int64  `json:"relay_user_id"`
	BindingVersion  int64  `json:"binding_version"`
	StartDate       string `json:"start_date"`
	EndDate         string `json:"end_date"`
	Granularity     string `json:"granularity"`
	Timezone        string `json:"timezone"`
}

func NewCache(store readcache.Store, options CacheOptions) (*Cache, error) {
	if store == nil {
		return nil, fmt.Errorf("personal usage cache store is required")
	}
	if !usageCacheNamespaceRE.MatchString(options.Namespace) {
		return nil, fmt.Errorf("invalid Redis namespace %q", options.Namespace)
	}
	applyCacheDefaults(&options)
	return &Cache{store: store, options: options}, nil
}

func applyCacheDefaults(options *CacheOptions) {
	if options.CommandTimeout <= 0 {
		options.CommandTimeout = 100 * time.Millisecond
	}
	if options.RefreshTimeout <= 0 {
		options.RefreshTimeout = 12 * time.Second
	}
	if options.LeaseTTL <= 0 {
		options.LeaseTTL = 15 * time.Second
	}
	if options.PollInterval <= 0 {
		options.PollInterval = 25 * time.Millisecond
	}
	if options.ReleaseTimeout <= 0 {
		options.ReleaseTimeout = 100 * time.Millisecond
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.RandFloat64 == nil {
		options.RandFloat64 = rand.Float64
	}
	if options.NewToken == nil {
		options.NewToken = uuid.NewString
	}
	if options.Sleep == nil {
		options.Sleep = readcache.Sleep
	}
}

func cacheKey(namespace string, key CacheKey) (string, error) {
	dimensions := cacheKeyDimensions{
		Namespace:       namespace,
		ProviderID:      key.ProviderID,
		ProviderVersion: key.ProviderVersion,
		ActorID:         key.ActorID,
		RelayUserID:     key.RelayUserID,
		BindingVersion:  key.BindingVersion,
		StartDate:       key.Params.StartDate,
		EndDate:         key.Params.EndDate,
		Granularity:     key.Params.Granularity,
		Timezone:        key.Params.Timezone,
	}
	encoded, err := json.Marshal(dimensions)
	if err != nil {
		return "", fmt.Errorf("encode personal usage cache dimensions: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return fmt.Sprintf("ae:%s:personal-usage:v1:%x", namespace, digest), nil
}

func (c *Cache) GetOrLoad(ctx context.Context, key CacheKey, includeQuota bool, loader OriginLoader) (*CacheResult, error) {
	if c == nil || c.store == nil {
		return nil, fmt.Errorf("personal usage cache is not configured")
	}
	if loader == nil {
		return nil, fmt.Errorf("personal usage origin loader is required")
	}
	if err := validateCacheKey(key); err != nil {
		return nil, err
	}
	encodedKey, err := cacheKey(c.options.Namespace, key)
	if err != nil {
		return nil, err
	}
	return c.flights.Do(ctx, encodedKey, c.options.RefreshTimeout, func(sharedCtx context.Context) (*CacheResult, error) {
		return c.loadWithLease(sharedCtx, encodedKey, includeQuota, loader)
	})
}

func validateCacheKey(key CacheKey) error {
	if key.ProviderID <= 0 || key.ProviderVersion <= 0 || key.ActorID <= 0 || key.RelayUserID <= 0 || key.BindingVersion <= 0 {
		return fmt.Errorf("invalid personal usage cache identity dimensions")
	}
	if key.Params.StartDate == "" || key.Params.EndDate == "" || key.Params.Granularity == "" {
		return fmt.Errorf("invalid personal usage cache range dimensions")
	}
	return nil
}

func (c *Cache) loadWithLease(ctx context.Context, key string, includeQuota bool, loader OriginLoader) (*CacheResult, error) {
	var stale *usageValueEnvelope
	missRecorded := false
	for {
		envelope, found, err := c.read(ctx, key)
		if err != nil {
			c.record("error")
			return c.loadAuthoritative(ctx, key, nil, includeQuota, loader, false)
		}
		if found {
			now := c.now()
			if !now.After(envelope.FreshUntil) {
				c.record("fresh")
				return cacheResultFromEnvelope(envelope, "fresh", "ok"), nil
			}
			if !now.After(envelope.StaleUntil) {
				stale = envelope
			}
		}
		if !missRecorded {
			c.record("miss")
			missRecorded = true
		}

		leaseKey := key + ":lease"
		token := c.options.NewToken()
		acquired, err := c.acquireLease(ctx, leaseKey, token)
		if err != nil {
			c.record("error")
			c.record("lease_failed")
			return c.loadAuthoritative(ctx, key, stale, includeQuota, loader, false)
		}
		if acquired {
			c.record("lease_acquired")
			return c.loadAsLeaseHolder(ctx, key, leaseKey, token, stale, includeQuota, loader)
		}
		c.record("lease_wait")

		for {
			envelope, found, err = c.read(ctx, key)
			if err != nil {
				c.record("error")
				return c.loadAuthoritative(ctx, key, stale, includeQuota, loader, false)
			}
			if found {
				now := c.now()
				if !now.After(envelope.FreshUntil) {
					c.record("fresh")
					return cacheResultFromEnvelope(envelope, "fresh", "ok"), nil
				}
				if !now.After(envelope.StaleUntil) {
					stale = envelope
				}
			}
			ttl, ttlErr := c.leaseTTL(ctx, leaseKey)
			if errors.Is(ttlErr, readcache.ErrMiss) {
				break
			}
			if ttlErr != nil {
				c.record("error")
				c.record("lease_failed")
				return c.loadAuthoritative(ctx, key, stale, includeQuota, loader, false)
			}
			if ttl <= 0 {
				break
			}
			wait := c.options.PollInterval
			if ttl < wait {
				wait = ttl
			}
			if err := c.options.Sleep(ctx, wait); err != nil {
				return nil, err
			}
		}
		if stale != nil && !c.now().After(stale.StaleUntil) {
			c.record("stale")
			return cacheResultFromEnvelope(stale, "stale", "error"), nil
		}
	}
}

func (c *Cache) loadAsLeaseHolder(ctx context.Context, key, leaseKey, token string, stale *usageValueEnvelope, includeQuota bool, loader OriginLoader) (*CacheResult, error) {
	defer c.releaseLease(leaseKey, token)

	envelope, found, err := c.read(ctx, key)
	if err != nil {
		c.record("error")
		return c.loadAuthoritative(ctx, key, stale, includeQuota, loader, false)
	}
	if found {
		now := c.now()
		if !now.After(envelope.FreshUntil) {
			c.record("fresh")
			return cacheResultFromEnvelope(envelope, "fresh", "ok"), nil
		}
		if !now.After(envelope.StaleUntil) {
			stale = envelope
		}
	}
	return c.loadAuthoritative(ctx, key, stale, includeQuota, loader, true)
}

func (c *Cache) loadAuthoritative(ctx context.Context, key string, stale *usageValueEnvelope, includeQuota bool, loader OriginLoader, write bool) (*CacheResult, error) {
	c.record("refresh")
	loaded, err := loader(ctx, includeQuota)
	if err != nil {
		c.record("error")
		return nil, err
	}
	if loaded.UsageErr != nil {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if stale == nil || c.now().After(stale.StaleUntil) {
			c.record("error")
			return nil, loaded.UsageErr
		}
		c.record("error")
		c.record("stale")
		result := cacheResultFromEnvelope(stale, "stale", "error")
		applyLoadedQuota(result, loaded)
		return result, nil
	}
	if loaded.Usage == nil {
		c.record("error")
		return nil, fmt.Errorf("personal usage origin returned no usage generation")
	}

	envelope, err := c.newEnvelope(loaded.Usage)
	if err != nil {
		c.record("error")
		return nil, err
	}
	if write {
		encoded, encodeErr := json.Marshal(envelope)
		if encodeErr != nil {
			c.record("error")
			return nil, fmt.Errorf("encode personal usage cache value: %w", encodeErr)
		}
		ttl := envelope.StaleUntil.Sub(c.now())
		if ttl > 0 {
			if err := c.set(ctx, key, encoded, ttl); err != nil {
				c.record("error")
			}
		}
	}

	result := cacheResultFromEnvelope(envelope, "miss", "ok")
	applyLoadedQuota(result, loaded)
	return result, nil
}

func applyLoadedQuota(result *CacheResult, loaded OriginLoadResult) {
	if result == nil || !loaded.QuotaLoaded {
		return
	}
	result.Quota = loaded.Quota
	result.QuotaFreshness = loaded.QuotaFreshness
	result.QuotaLoaded = true
}

func (c *Cache) newEnvelope(usage *relay.UserUsageDashboardResponse) (*usageValueEnvelope, error) {
	if !validOriginUsage(usage) {
		return nil, fmt.Errorf("personal usage origin returned an invalid generation")
	}
	generatedAt := c.now()
	random := c.options.RandFloat64()
	if random < 0 {
		random = 0
	} else if random > 1 {
		random = 1
	}
	jitter := 0.1 + 0.1*random
	freshWindow := 30*time.Second - time.Duration(jitter*float64(30*time.Second))
	staleWindow := 2*time.Minute - time.Duration(jitter*float64(2*time.Minute))
	return &usageValueEnvelope{
		SchemaVersion: usageCacheSchemaVersion,
		GeneratedAt:   generatedAt,
		FreshUntil:    generatedAt.Add(freshWindow),
		StaleUntil:    generatedAt.Add(staleWindow),
		Usage: usagePayload{
			Range: usage.Range, Stats: usage.Stats,
			Trend:  append([]relay.UserUsageTrendPoint{}, usage.Trend...),
			Models: append([]relay.UserUsageModelStat{}, usage.Models...),
		},
	}, nil
}

func validOriginUsage(usage *relay.UserUsageDashboardResponse) bool {
	return usage != nil && usage.Configured && usage.Stats != nil && usage.Trend != nil && usage.Models != nil
}

func (c *Cache) read(ctx context.Context, key string) (*usageValueEnvelope, bool, error) {
	commandCtx, cancel := context.WithTimeout(ctx, c.options.CommandTimeout)
	defer cancel()
	value, err := c.store.Get(commandCtx, key)
	if errors.Is(err, readcache.ErrMiss) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}

	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.DisallowUnknownFields()
	var envelope usageValueEnvelope
	if err := decoder.Decode(&envelope); err != nil || !validEnvelope(&envelope) {
		return nil, false, nil
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, false, nil
	}
	return &envelope, true, nil
}

func validEnvelope(envelope *usageValueEnvelope) bool {
	if envelope == nil || envelope.SchemaVersion != usageCacheSchemaVersion || envelope.GeneratedAt.IsZero() {
		return false
	}
	freshWindow := envelope.FreshUntil.Sub(envelope.GeneratedAt)
	staleWindow := envelope.StaleUntil.Sub(envelope.GeneratedAt)
	if freshWindow < 15*time.Second || freshWindow > 30*time.Second || staleWindow <= freshWindow || staleWindow > 2*time.Minute {
		return false
	}
	return envelope.Usage.Stats != nil && envelope.Usage.Trend != nil && envelope.Usage.Models != nil
}

func cacheResultFromEnvelope(envelope *usageValueEnvelope, cacheStatus, sourceStatus string) *CacheResult {
	return &CacheResult{
		Usage: &relay.UserUsageDashboardResponse{
			Configured: true,
			Range:      envelope.Usage.Range,
			Stats:      envelope.Usage.Stats,
			Trend:      append([]relay.UserUsageTrendPoint{}, envelope.Usage.Trend...),
			Models:     append([]relay.UserUsageModelStat{}, envelope.Usage.Models...),
		},
		UsageFreshness: UsageFreshness{
			AsOf: envelope.GeneratedAt, FreshUntil: envelope.FreshUntil, StaleUntil: envelope.StaleUntil,
			CacheStatus: cacheStatus, SourceStatus: sourceStatus,
		},
	}
}

func (c *Cache) acquireLease(ctx context.Context, key, token string) (bool, error) {
	commandCtx, cancel := context.WithTimeout(ctx, c.options.CommandTimeout)
	defer cancel()
	return c.store.TryAcquireLease(commandCtx, key, token, c.options.LeaseTTL)
}

func (c *Cache) leaseTTL(ctx context.Context, key string) (time.Duration, error) {
	commandCtx, cancel := context.WithTimeout(ctx, c.options.CommandTimeout)
	defer cancel()
	return c.store.LeaseTTL(commandCtx, key)
}

func (c *Cache) set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	commandCtx, cancel := context.WithTimeout(ctx, c.options.CommandTimeout)
	defer cancel()
	return c.store.Set(commandCtx, key, value, ttl)
}

func (c *Cache) releaseLease(key, token string) {
	ctx, cancel := context.WithTimeout(context.Background(), c.options.ReleaseTimeout)
	defer cancel()
	released, err := c.store.ReleaseLease(ctx, key, token)
	if err != nil {
		c.record("error")
	}
	if err != nil || !released {
		c.record("lease_failed")
	}
}

func (c *Cache) record(outcome string) {
	if c != nil && c.options.Metrics != nil {
		c.options.Metrics.Record(outcome)
	}
}

func (c *Cache) now() time.Time {
	return c.options.Now().UTC()
}
