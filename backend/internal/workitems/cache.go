package workitems

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"regexp"
	"time"

	"github.com/ai-efficiency/backend/internal/readcache"
	"github.com/google/uuid"
	redis "github.com/redis/go-redis/v9"
)

const countsCacheSchemaVersion = 1

var ErrCountsCacheMiss = readcache.ErrMiss

var cacheNamespaceRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,62}$`)

type CountsLoadResult struct {
	Counts    *CountsResponse
	Cacheable bool
}

type CountsLoader func(context.Context) (CountsLoadResult, error)

type CountsStore = readcache.Store

type RevisionReader interface {
	Current(ctx context.Context) (string, error)
}

type RedisCountsStore = readcache.RedisStore

type CountsCacheMetrics = readcache.Metrics

func NewRedisCountsStore(client redis.UniversalClient) *RedisCountsStore {
	return readcache.NewRedisStore(client)
}

type CountsCacheOptions struct {
	Namespace      string
	CommandTimeout time.Duration
	RefreshTimeout time.Duration
	LeaseTTL       time.Duration
	PollInterval   time.Duration
	ReleaseTimeout time.Duration
	RandFloat64    func() float64
	NewToken       func() string
	Sleep          func(context.Context, time.Duration) error
	Metrics        CountsCacheMetrics
}

type CountsCache struct {
	store     CountsStore
	revisions RevisionReader
	options   CountsCacheOptions
	flights   readcache.FlightGroup[*CountsResponse]
}

type countsValueEnvelope struct {
	SchemaVersion int             `json:"schema_version"`
	Counts        *CountsResponse `json:"counts"`
}

func NewCountsCache(store CountsStore, revisions RevisionReader, options CountsCacheOptions) (*CountsCache, error) {
	if store == nil {
		return nil, fmt.Errorf("work item counts store is required")
	}
	if revisions == nil {
		return nil, fmt.Errorf("work item counts revision reader is required")
	}
	if !cacheNamespaceRE.MatchString(options.Namespace) {
		return nil, fmt.Errorf("invalid Redis namespace %q", options.Namespace)
	}
	applyCountsCacheDefaults(&options)
	return &CountsCache{store: store, revisions: revisions, options: options}, nil
}

func applyCountsCacheDefaults(options *CountsCacheOptions) {
	if options.CommandTimeout <= 0 {
		options.CommandTimeout = 100 * time.Millisecond
	}
	if options.RefreshTimeout <= 0 {
		options.RefreshTimeout = 15 * time.Second
	}
	if options.LeaseTTL <= 0 {
		options.LeaseTTL = 20 * time.Second
	}
	if options.PollInterval <= 0 {
		options.PollInterval = 25 * time.Millisecond
	}
	if options.ReleaseTimeout <= 0 {
		options.ReleaseTimeout = 100 * time.Millisecond
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

func (c *CountsCache) GetOrLoad(ctx context.Context, actorID int, effectiveRole string, loader CountsLoader) (*CountsResponse, error) {
	if c == nil || c.store == nil || c.revisions == nil {
		return nil, fmt.Errorf("work item counts cache is not configured")
	}
	if actorID <= 0 {
		return nil, fmt.Errorf("work item counts actor ID must be positive")
	}
	if effectiveRole != "user" && effectiveRole != "admin" {
		return nil, fmt.Errorf("invalid work item counts role %q", effectiveRole)
	}
	if loader == nil {
		return nil, fmt.Errorf("work item counts loader is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		revision, err := c.currentRevision(ctx)
		if err != nil {
			return nil, err
		}
		key := countsCacheKey(c.options.Namespace, revision, actorID, effectiveRole)
		counts, err := c.flights.Do(ctx, key, c.options.RefreshTimeout, func(sharedCtx context.Context) (*CountsResponse, error) {
			return c.loadWithLease(sharedCtx, key, revision, loader)
		})
		if err != nil {
			return nil, err
		}
		currentRevision, err := c.currentRevision(ctx)
		if err != nil {
			return nil, err
		}
		if currentRevision == revision {
			return counts, nil
		}
	}
}

func (c *CountsCache) loadWithLease(ctx context.Context, key, revision string, loader CountsLoader) (*CountsResponse, error) {
	if counts, hit, err := c.read(ctx, key); hit {
		c.record("fresh")
		return counts, nil
	} else if err != nil {
		c.record("error")
		return c.loadAuthoritative(ctx, loader)
	}
	c.record("miss")

	leaseKey := key + ":lease"
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		token := c.options.NewToken()
		acquired, err := c.acquireLease(ctx, leaseKey, token)
		if err != nil {
			c.record("error")
			c.record("lease_failed")
			return c.loadAuthoritative(ctx, loader)
		}
		if acquired {
			c.record("lease_acquired")
			return c.loadAsLeaseHolder(ctx, key, leaseKey, token, revision, loader)
		}
		c.record("lease_wait")

		for {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if counts, hit, err := c.read(ctx, key); hit {
				return counts, nil
			} else if err != nil {
				c.record("error")
				return c.loadAuthoritative(ctx, loader)
			}
			ttl, err := c.leaseTTL(ctx, leaseKey)
			if errors.Is(err, ErrCountsCacheMiss) {
				break
			}
			if err != nil {
				c.record("error")
				c.record("lease_failed")
				return c.loadAuthoritative(ctx, loader)
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
	}
}

func (c *CountsCache) loadAsLeaseHolder(ctx context.Context, key, leaseKey, token, revision string, loader CountsLoader) (*CountsResponse, error) {
	defer c.releaseLease(leaseKey, token)

	if counts, hit, err := c.read(ctx, key); hit {
		return counts, nil
	} else if err != nil {
		c.record("error")
		return c.loadAuthoritative(ctx, loader)
	}

	result, err := c.runLoader(ctx, loader)
	if err != nil {
		return nil, err
	}
	if !result.Cacheable {
		return result.Counts, nil
	}

	currentRevision, err := c.currentRevision(ctx)
	if err != nil {
		c.record("error")
		return nil, err
	}
	if currentRevision != revision {
		return result.Counts, nil
	}
	value, err := json.Marshal(countsValueEnvelope{SchemaVersion: countsCacheSchemaVersion, Counts: result.Counts})
	if err != nil {
		c.record("error")
		return nil, fmt.Errorf("encode work item counts cache value: %w", err)
	}
	if err := c.set(ctx, key, value, c.valueTTL()); err != nil {
		c.record("error")
	}
	return result.Counts, nil
}

func (c *CountsCache) loadAuthoritative(ctx context.Context, loader CountsLoader) (*CountsResponse, error) {
	result, err := c.runLoader(ctx, loader)
	if err != nil {
		return nil, err
	}
	return result.Counts, nil
}

func (c *CountsCache) runLoader(ctx context.Context, loader CountsLoader) (CountsLoadResult, error) {
	c.record("refresh")
	result, err := loader(ctx)
	if err != nil {
		c.record("error")
		return CountsLoadResult{}, err
	}
	if result.Counts == nil {
		c.record("error")
		return CountsLoadResult{}, fmt.Errorf("work item counts loader returned nil counts")
	}
	return result, nil
}

func (c *CountsCache) currentRevision(ctx context.Context) (string, error) {
	revision, err := c.revisions.Current(ctx)
	if err != nil {
		return "", fmt.Errorf("read work item counts revision: %w", err)
	}
	parsed, err := uuid.Parse(revision)
	if err != nil || parsed.String() != revision {
		return "", fmt.Errorf("read work item counts revision: invalid UUID")
	}
	return revision, nil
}

func (c *CountsCache) read(ctx context.Context, key string) (*CountsResponse, bool, error) {
	commandCtx, cancel := context.WithTimeout(ctx, c.options.CommandTimeout)
	defer cancel()
	value, err := c.store.Get(commandCtx, key)
	if errors.Is(err, ErrCountsCacheMiss) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}

	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.DisallowUnknownFields()
	var envelope countsValueEnvelope
	if err := decoder.Decode(&envelope); err != nil || envelope.SchemaVersion != countsCacheSchemaVersion || !validCounts(envelope.Counts) {
		return nil, false, nil
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, false, nil
	}
	return envelope.Counts, true, nil
}

func validCounts(counts *CountsResponse) bool {
	return counts != nil &&
		counts.QuotaResetApprovalCount >= 0 &&
		counts.QuotaResetAdminCount >= 0 &&
		counts.AIAccessSetupCount >= 0 &&
		counts.OffboardingCount >= 0 &&
		counts.TotalCount >= 0
}

func (c *CountsCache) acquireLease(ctx context.Context, key, token string) (bool, error) {
	commandCtx, cancel := context.WithTimeout(ctx, c.options.CommandTimeout)
	defer cancel()
	return c.store.TryAcquireLease(commandCtx, key, token, c.options.LeaseTTL)
}

func (c *CountsCache) leaseTTL(ctx context.Context, key string) (time.Duration, error) {
	commandCtx, cancel := context.WithTimeout(ctx, c.options.CommandTimeout)
	defer cancel()
	return c.store.LeaseTTL(commandCtx, key)
}

func (c *CountsCache) set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	commandCtx, cancel := context.WithTimeout(ctx, c.options.CommandTimeout)
	defer cancel()
	return c.store.Set(commandCtx, key, value, ttl)
}

func (c *CountsCache) releaseLease(key, token string) {
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

func (c *CountsCache) record(outcome string) {
	if c != nil && c.options.Metrics != nil {
		c.options.Metrics.Record(outcome)
	}
}

func (c *CountsCache) valueTTL() time.Duration {
	random := c.options.RandFloat64()
	if random < 0 {
		random = 0
	} else if random > 1 {
		random = 1
	}
	return 30*time.Second - time.Duration((0.1+0.1*random)*float64(30*time.Second))
}

func countsCacheKey(namespace, revision string, actorID int, effectiveRole string) string {
	return fmt.Sprintf("ae:%s:work-items:counts:v1:rev:%s:actor:%d:role:%s", namespace, revision, actorID, effectiveRole)
}
