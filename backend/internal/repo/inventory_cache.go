package repo

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
)

const inventoryCacheSchemaVersion = 1

var inventoryNamespaceRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,62}$`)

type InventoryLoader func(context.Context) ([]InventoryProviderSummary, error)

type InventoryRevisionReader interface {
	Current(context.Context) (string, error)
}

type InventoryCacheOptions struct {
	Namespace      string
	CommandTimeout time.Duration
	RefreshTimeout time.Duration
	LeaseTTL       time.Duration
	PollInterval   time.Duration
	ReleaseTimeout time.Duration
	RandFloat64    func() float64
	NewToken       func() string
	Sleep          func(context.Context, time.Duration) error
	Metrics        readcache.Metrics
}

type InventoryCache struct {
	store     readcache.Store
	revisions InventoryRevisionReader
	options   InventoryCacheOptions
	flights   readcache.FlightGroup[[]InventoryProviderSummary]
}

type inventoryValueEnvelope struct {
	SchemaVersion int                        `json:"schema_version"`
	Inventory     []InventoryProviderSummary `json:"inventory"`
}

func NewInventoryCache(store readcache.Store, revisions InventoryRevisionReader, options InventoryCacheOptions) (*InventoryCache, error) {
	if store == nil {
		return nil, fmt.Errorf("repository inventory store is required")
	}
	if revisions == nil {
		return nil, fmt.Errorf("repository inventory revision reader is required")
	}
	if !inventoryNamespaceRE.MatchString(options.Namespace) {
		return nil, fmt.Errorf("invalid Redis namespace %q", options.Namespace)
	}
	applyInventoryCacheDefaults(&options)
	return &InventoryCache{store: store, revisions: revisions, options: options}, nil
}

func applyInventoryCacheDefaults(options *InventoryCacheOptions) {
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

func (c *InventoryCache) GetOrLoad(ctx context.Context, loader InventoryLoader) ([]InventoryProviderSummary, error) {
	if c == nil || c.store == nil || c.revisions == nil {
		return nil, fmt.Errorf("repository inventory cache is not configured")
	}
	if loader == nil {
		return nil, fmt.Errorf("repository inventory loader is required")
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
		key := inventoryCacheKey(c.options.Namespace, revision)
		inventory, err := c.flights.Do(ctx, key, c.options.RefreshTimeout, func(sharedCtx context.Context) ([]InventoryProviderSummary, error) {
			return c.loadWithLease(sharedCtx, key, revision, loader)
		})
		if err != nil {
			return nil, err
		}
		current, err := c.currentRevision(ctx)
		if err != nil {
			return nil, err
		}
		if current == revision {
			return inventory, nil
		}
	}
}

func (c *InventoryCache) loadWithLease(ctx context.Context, key, revision string, loader InventoryLoader) ([]InventoryProviderSummary, error) {
	if inventory, hit, err := c.read(ctx, key); hit {
		c.record("fresh")
		return inventory, nil
	} else if err != nil {
		c.record("error")
		return c.loadAuthoritative(ctx, loader)
	}
	c.record("miss")

	leaseKey := key + ":lease"
	for {
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
			if inventory, hit, err := c.read(ctx, key); hit {
				c.record("fresh")
				return inventory, nil
			} else if err != nil {
				c.record("error")
				return c.loadAuthoritative(ctx, loader)
			}
			ttl, err := c.leaseTTL(ctx, leaseKey)
			if errors.Is(err, readcache.ErrMiss) {
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

func (c *InventoryCache) loadAsLeaseHolder(ctx context.Context, key, leaseKey, token, revision string, loader InventoryLoader) ([]InventoryProviderSummary, error) {
	defer c.releaseLease(leaseKey, token)

	if inventory, hit, err := c.read(ctx, key); hit {
		c.record("fresh")
		return inventory, nil
	} else if err != nil {
		c.record("error")
		return c.loadAuthoritative(ctx, loader)
	}

	inventory, err := c.loadAuthoritative(ctx, loader)
	if err != nil {
		return nil, err
	}
	current, err := c.currentRevision(ctx)
	if err != nil {
		return nil, err
	}
	if current != revision {
		return inventory, nil
	}
	value, err := json.Marshal(inventoryValueEnvelope{SchemaVersion: inventoryCacheSchemaVersion, Inventory: inventory})
	if err != nil {
		c.record("error")
		return nil, fmt.Errorf("encode repository inventory cache value: %w", err)
	}
	if err := c.set(ctx, key, value, c.valueTTL()); err != nil {
		c.record("error")
	}
	return inventory, nil
}

func (c *InventoryCache) loadAuthoritative(ctx context.Context, loader InventoryLoader) ([]InventoryProviderSummary, error) {
	c.record("refresh")
	inventory, err := loader(ctx)
	if err != nil {
		c.record("error")
		return nil, err
	}
	if inventory == nil {
		c.record("error")
		return nil, fmt.Errorf("repository inventory loader returned nil inventory")
	}
	return inventory, nil
}

func (c *InventoryCache) currentRevision(ctx context.Context) (string, error) {
	revision, err := c.revisions.Current(ctx)
	if err != nil {
		return "", fmt.Errorf("read repository inventory revision: %w", err)
	}
	parsed, err := uuid.Parse(revision)
	if err != nil || parsed.String() != revision {
		return "", fmt.Errorf("read repository inventory revision: invalid UUID")
	}
	return revision, nil
}

func (c *InventoryCache) read(ctx context.Context, key string) ([]InventoryProviderSummary, bool, error) {
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
	var envelope inventoryValueEnvelope
	if err := decoder.Decode(&envelope); err != nil || envelope.SchemaVersion != inventoryCacheSchemaVersion || !validCachedInventory(envelope.Inventory) {
		return nil, false, nil
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, false, nil
	}
	return envelope.Inventory, true, nil
}

func validCachedInventory(inventory []InventoryProviderSummary) bool {
	if inventory == nil {
		return false
	}
	for _, provider := range inventory {
		if provider.ProviderKey == "" || provider.Name == "" || provider.Type == "" || provider.Scopes == nil ||
			provider.TotalRepos < 0 || provider.BoundRepos < 0 || provider.UnboundRepos < 0 ||
			provider.ActiveRepos < 0 || provider.WebhookFailedRepos < 0 ||
			provider.BoundRepos+provider.UnboundRepos != provider.TotalRepos {
			return false
		}
		var total, bound, unbound, active, webhookFailed int
		for _, scope := range provider.Scopes {
			if scope.Scope == "" || scope.TotalRepos < 0 || scope.BoundRepos < 0 || scope.UnboundRepos < 0 ||
				scope.ActiveRepos < 0 || scope.WebhookFailedRepos < 0 ||
				scope.BoundRepos+scope.UnboundRepos != scope.TotalRepos {
				return false
			}
			total += scope.TotalRepos
			bound += scope.BoundRepos
			unbound += scope.UnboundRepos
			active += scope.ActiveRepos
			webhookFailed += scope.WebhookFailedRepos
		}
		if total != provider.TotalRepos || bound != provider.BoundRepos || unbound != provider.UnboundRepos ||
			active != provider.ActiveRepos || webhookFailed != provider.WebhookFailedRepos {
			return false
		}
	}
	return true
}

func (c *InventoryCache) acquireLease(ctx context.Context, key, token string) (bool, error) {
	commandCtx, cancel := context.WithTimeout(ctx, c.options.CommandTimeout)
	defer cancel()
	return c.store.TryAcquireLease(commandCtx, key, token, c.options.LeaseTTL)
}

func (c *InventoryCache) leaseTTL(ctx context.Context, key string) (time.Duration, error) {
	commandCtx, cancel := context.WithTimeout(ctx, c.options.CommandTimeout)
	defer cancel()
	return c.store.LeaseTTL(commandCtx, key)
}

func (c *InventoryCache) set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	commandCtx, cancel := context.WithTimeout(ctx, c.options.CommandTimeout)
	defer cancel()
	return c.store.Set(commandCtx, key, value, ttl)
}

func (c *InventoryCache) releaseLease(key, token string) {
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

func (c *InventoryCache) record(outcome string) {
	if c != nil && c.options.Metrics != nil {
		c.options.Metrics.Record(outcome)
	}
}

func (c *InventoryCache) valueTTL() time.Duration {
	random := c.options.RandFloat64()
	if random < 0 {
		random = 0
	} else if random > 1 {
		random = 1
	}
	return 60*time.Second - time.Duration((0.1+0.1*random)*float64(60*time.Second))
}

func inventoryCacheKey(namespace, revision string) string {
	return fmt.Sprintf("ae:%s:repos:inventory:v1:rev:%s", namespace, revision)
}
