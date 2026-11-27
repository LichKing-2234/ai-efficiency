package kirocredits

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/ai-efficiency/backend/internal/readcache"
)

const (
	cacheSchemaVersion = 1
	defaultFreshTTL    = 5 * time.Minute
	defaultStaleTTL    = 30 * time.Minute
	defaultCommandTTL  = 200 * time.Millisecond
	defaultRefreshTTL  = 8 * time.Second
)

// CacheOptions configures the kiro credits read model.
type CacheOptions struct {
	Namespace      string
	FreshTTL       time.Duration
	StaleTTL       time.Duration
	CommandTimeout time.Duration
	RefreshTimeout time.Duration
	Metrics        readcache.Metrics
}

// Cache is the Redis read model for kiro credits. Values are reconstructible:
// every entry can be rebuilt from the kiro2cc admin API, so a Redis failure
// only costs latency, never correctness.
type Cache struct {
	store    readcache.Store
	ns       string
	freshTTL time.Duration
	staleTTL time.Duration
	command  time.Duration
	refresh  time.Duration
	metrics  readcache.Metrics
	now      func() time.Time

	flights readcache.FlightGroup[*cachedCredits]
}

// cachedCredits is the serialized per-user credits value. Every dimension that
// can change the answer is part of the key, so the value itself carries only
// the data plus its own staleness envelope.
type cachedCredits struct {
	SchemaVersion   int    `json:"schema_version"`
	ProviderID      int    `json:"provider_id"`
	ProviderVersion int64  `json:"provider_version"`
	RelayUserID     int64  `json:"relay_user_id"`
	StartDate       string `json:"start_date"`
	EndDate         string `json:"end_date"`
	Granularity     string `json:"granularity"`
	Timezone        string `json:"timezone"`

	KeyFound  bool `json:"key_found"`
	Ambiguous bool `json:"ambiguous"`

	LifetimeCredits  float64 `json:"lifetime_credits"`
	LifetimeRequests int64   `json:"lifetime_requests"`

	Credits        float64        `json:"credits"`
	CreditsSaved   float64        `json:"credits_saved"`
	Requests       int64          `json:"requests"`
	InputTokens    int64          `json:"input_tokens"`
	OutputTokens   int64          `json:"output_tokens"`
	ByModel        []ModelCredits `json:"by_model"`
	ByDay          []DayCredits   `json:"by_day"`
	MissingCredits bool           `json:"missing_credits"`

	AsOf       time.Time `json:"as_of"`
	FreshUntil time.Time `json:"fresh_until"`
	StaleUntil time.Time `json:"stale_until"`
}

func (v *cachedCredits) partial() bool { return v.MissingCredits }

// NewCache validates options and returns a cache backed by store.
func NewCache(store readcache.Store, options CacheOptions) (*Cache, error) {
	if store == nil {
		return nil, errors.New("kiro credits cache requires a store")
	}
	c := &Cache{
		store:    store,
		ns:       options.Namespace,
		freshTTL: options.FreshTTL,
		staleTTL: options.StaleTTL,
		command:  options.CommandTimeout,
		refresh:  options.RefreshTimeout,
		metrics:  options.Metrics,
		now:      time.Now,
	}
	if c.ns == "" {
		c.ns = "ai-efficiency"
	}
	if c.ns == "" {
		return nil, errors.New("kiro credits cache requires a namespace")
	}
	if c.freshTTL <= 0 {
		c.freshTTL = defaultFreshTTL
	}
	if c.staleTTL <= c.freshTTL {
		c.staleTTL = c.freshTTL + defaultStaleTTL
	}
	if c.command <= 0 {
		c.command = defaultCommandTTL
	}
	if c.refresh <= 0 {
		c.refresh = defaultRefreshTTL
	}
	return c, nil
}

func (c *Cache) creditsKey(providerID int, providerVersion int64, relayUserID int64, params Params) string {
	return fmt.Sprintf("ae:%s:kiro-credits:v%d:%d:%d:%s:%s:%s:%s",
		c.ns, cacheSchemaVersion, providerVersion, relayUserID,
		params.StartDate, params.EndDate, params.Granularity, params.Timezone)
}

func (c *Cache) warmLeaseKey() string {
	return fmt.Sprintf("ae:%s:kiro-credits:warm", c.ns)
}

func (c *Cache) keysKey(providerID int, providerVersion int64) string {
	return fmt.Sprintf("ae:%s:kiro-api-keys:v%d:%d:%d", c.ns, cacheSchemaVersion, providerID, providerVersion)
}

func (c *Cache) read(ctx context.Context, key string) (*cachedCredits, bool) {
	if c == nil || c.store == nil {
		return nil, false
	}
	commandCtx, cancel := context.WithTimeout(ctx, c.command)
	defer cancel()
	raw, err := c.store.Get(commandCtx, key)
	if err != nil || len(raw) == 0 {
		return nil, false
	}
	var value cachedCredits
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, false
	}
	if value.SchemaVersion != cacheSchemaVersion {
		return nil, false
	}
	return &value, true
}

func (c *Cache) isFresh(value *cachedCredits, now time.Time) bool {
	return value != nil && now.Before(value.FreshUntil) && now.Before(value.StaleUntil)
}

func (c *Cache) usable(value *cachedCredits, now time.Time) bool {
	return value != nil && now.Before(value.StaleUntil)
}

func (c *Cache) write(ctx context.Context, key string, value cachedCredits) {
	if c == nil || c.store == nil {
		return
	}
	now := c.now()
	value.SchemaVersion = cacheSchemaVersion
	value.AsOf = now
	value.FreshUntil = now.Add(c.freshTTL)
	value.StaleUntil = now.Add(c.staleTTL)
	raw, err := json.Marshal(value)
	if err != nil {
		return
	}
	ttl := c.staleTTL
	if ttl <= 0 {
		return
	}
	commandCtx, cancel := context.WithTimeout(ctx, c.command)
	defer cancel()
	_ = c.store.Set(commandCtx, key, raw, ttl)
}

// GetOrLoad returns the cached value, refreshing it through one shared flight
// when it is missing or past its fresh window. A failed refresh keeps serving
// the stale value so a slow kiro2cc read never blanks the UI.
func (c *Cache) GetOrLoad(ctx context.Context, key string, load func(context.Context) (cachedCredits, error)) (cachedCredits, error) {
	if c == nil {
		return load(ctx)
	}
	now := c.now()
	if current, ok := c.read(ctx, key); ok {
		if c.isFresh(current, now) {
			return *current, nil
		}
	}
	loaded, err := c.flights.Do(ctx, key, c.refresh, func(flightCtx context.Context) (*cachedCredits, error) {
		now := c.now()
		stale, hasStale := c.read(flightCtx, key)
		if hasStale && c.isFresh(stale, now) {
			return stale, nil
		}
		value, err := load(flightCtx)
		if err != nil {
			if hasStale && c.usable(stale, now) {
				return stale, nil
			}
			return nil, err
		}
		c.write(flightCtx, key, value)
		return &value, nil
	})
	if err != nil {
		return cachedCredits{}, err
	}
	if loaded == nil {
		return cachedCredits{}, errors.New("kiro credits cache returned no value")
	}
	return *loaded, nil
}

// Keys returns the cached kiro2cc key list. Keys are shared by every user of a
// provider version, so one Redis entry replaces a per-request list call.
func (c *Cache) Keys(ctx context.Context, providerID int, providerVersion int64, load func(context.Context) ([]adminKey, error)) ([]adminKey, error) {
	if c == nil {
		return load(ctx)
	}
	key := c.keysKey(providerID, providerVersion)
	if current, ok := c.readKeys(ctx, key); ok {
		return current, nil
	}
	keys, err := load(ctx)
	if err != nil {
		return nil, err
	}
	c.writeKeys(ctx, key, keys)
	return keys, nil
}

func (c *Cache) readKeys(ctx context.Context, key string) ([]adminKey, bool) {
	if c == nil || c.store == nil {
		return nil, false
	}
	commandCtx, cancel := context.WithTimeout(ctx, c.command)
	defer cancel()
	raw, err := c.store.Get(commandCtx, key)
	if err != nil || len(raw) == 0 {
		return nil, false
	}
	var payload struct {
		SchemaVersion int        `json:"schema_version"`
		Keys          []adminKey `json:"keys"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, false
	}
	if payload.SchemaVersion != cacheSchemaVersion {
		return nil, false
	}
	return payload.Keys, true
}

func (c *Cache) writeKeys(ctx context.Context, key string, keys []adminKey) {
	if c == nil || c.store == nil {
		return
	}
	raw, err := json.Marshal(struct {
		SchemaVersion int        `json:"schema_version"`
		Keys          []adminKey `json:"keys"`
	}{cacheSchemaVersion, keys})
	if err != nil {
		return
	}
	commandCtx, cancel := context.WithTimeout(ctx, c.command)
	defer cancel()
	_ = c.store.Set(commandCtx, key, raw, c.staleTTL)
}

// keyListSnapshot is a small helper for the warmer: it exposes the relay user
// ids encoded in kiro2cc key names without leaking the matching rules.
func relayUserIDsFromKeys(keys []adminKey) []int64 {
	seen := map[int64]struct{}{}
	ids := make([]int64, 0, len(keys))
	for _, key := range keys {
		name := key.Name
		rest := ""
		// The legacy prefix also starts with the modern one, so test it first.
		if len(name) > len(legacyKeyNameBase) && name[:len(legacyKeyNameBase)] == legacyKeyNameBase {
			rest = name[len(legacyKeyNameBase):]
		} else if len(name) > len(modernKeyNameBase) && name[:len(modernKeyNameBase)] == modernKeyNameBase {
			rest = name[len(modernKeyNameBase):]
		} else {
			continue
		}
		digits := rest
		for i, r := range rest {
			if r < '0' || r > '9' {
				digits = rest[:i]
				break
			}
		}
		id, err := strconv.ParseInt(digits, 10, 64)
		if err != nil || id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids
}

// semaphore guards concurrent kiro2cc reads across one request.
type semaphore chan struct{}

func newSemaphore(size int) semaphore { return make(chan struct{}, size) }

func (s semaphore) acquire(ctx context.Context) error {
	select {
	case s <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s semaphore) release() { <-s }
