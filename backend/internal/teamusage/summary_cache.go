package teamusage

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
	"strings"
	"time"

	"github.com/ai-efficiency/backend/internal/readcache"
	"github.com/ai-efficiency/backend/internal/representativescope"
	"github.com/google/uuid"
)

const snapshotCacheSchemaVersion = 2

var snapshotCacheNamespaceRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,62}$`)

type SnapshotCache struct {
	store   readcache.Store
	options SnapshotCacheOptions
	flights readcache.FlightGroup[*SnapshotCacheResult]
}

type snapshotValueEnvelope struct {
	SchemaVersion int               `json:"schema_version"`
	GeneratedAt   time.Time         `json:"generated_at"`
	FreshUntil    time.Time         `json:"fresh_until"`
	StaleUntil    time.Time         `json:"stale_until"`
	Snapshot      *OverviewResponse `json:"snapshot"`
}

type snapshotCacheKeyDimensions struct {
	Namespace       string `json:"namespace"`
	ProviderID      int    `json:"provider_id"`
	ProviderVersion int64  `json:"provider_version"`
	ActorID         int    `json:"actor_id"`
	ScopeVersion    string `json:"scope_version"`
	ScopeHash       string `json:"scope_hash"`
	StartDate       string `json:"start_date"`
	EndDate         string `json:"end_date"`
	Granularity     string `json:"granularity"`
	Timezone        string `json:"timezone"`
}

func NewSnapshotCache(store readcache.Store, options SnapshotCacheOptions) (*SnapshotCache, error) {
	if store == nil {
		return nil, fmt.Errorf("team usage snapshot cache store is required")
	}
	if !snapshotCacheNamespaceRE.MatchString(options.Namespace) {
		return nil, fmt.Errorf("invalid Redis namespace %q", options.Namespace)
	}
	applySnapshotCacheDefaults(&options)
	return &SnapshotCache{store: store, options: options}, nil
}

func applySnapshotCacheDefaults(options *SnapshotCacheOptions) {
	if options.CommandTimeout <= 0 {
		options.CommandTimeout = 100 * time.Millisecond
	}
	if options.RefreshTimeout <= 0 {
		options.RefreshTimeout = 25 * time.Second
	}
	if options.LeaseTTL <= 0 {
		options.LeaseTTL = 30 * time.Second
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

func snapshotCacheKey(namespace string, key SnapshotCacheKey) (string, error) {
	dimensions := snapshotCacheKeyDimensions{
		Namespace: namespace, ProviderID: key.ProviderID, ProviderVersion: key.ProviderVersion,
		ActorID: key.ActorID, ScopeVersion: key.ScopeVersion, ScopeHash: key.ScopeHash,
		StartDate: key.Params.StartDate, EndDate: key.Params.EndDate,
		Granularity: key.Params.Granularity, Timezone: key.Params.Timezone,
	}
	encoded, err := json.Marshal(dimensions)
	if err != nil {
		return "", fmt.Errorf("encode team usage snapshot cache dimensions: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return fmt.Sprintf("ae:%s:team-usage-snapshot:v1:%x", namespace, digest), nil
}

func effectiveScopeHash(scope *representativescope.Scope) (string, error) {
	if scope == nil || strings.TrimSpace(scope.Version) == "" || scope.ActorUserID <= 0 {
		return "", fmt.Errorf("team usage effective scope is incomplete")
	}
	encoded, err := json.Marshal(scope)
	if err != nil {
		return "", fmt.Errorf("encode team usage effective scope: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return fmt.Sprintf("%x", digest), nil
}

func (c *SnapshotCache) GetOrLoad(ctx context.Context, key SnapshotCacheKey, loader SnapshotOriginLoader) (*SnapshotCacheResult, error) {
	if c == nil || c.store == nil {
		return nil, fmt.Errorf("team usage snapshot cache is not configured")
	}
	if loader == nil {
		return nil, fmt.Errorf("team usage snapshot origin loader is required")
	}
	if err := validateSnapshotCacheKey(key); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	encodedKey, err := snapshotCacheKey(c.options.Namespace, key)
	if err != nil {
		return nil, err
	}
	return c.flights.Do(ctx, encodedKey, c.options.RefreshTimeout, func(sharedCtx context.Context) (*SnapshotCacheResult, error) {
		return c.loadWithLease(sharedCtx, encodedKey, loader)
	})
}

func validateSnapshotCacheKey(key SnapshotCacheKey) error {
	if key.ProviderID <= 0 || key.ProviderVersion <= 0 || key.ActorID <= 0 ||
		strings.TrimSpace(key.ScopeVersion) == "" || strings.TrimSpace(key.ScopeHash) == "" {
		return fmt.Errorf("invalid team usage snapshot identity dimensions")
	}
	if strings.TrimSpace(key.Params.StartDate) == "" || strings.TrimSpace(key.Params.EndDate) == "" ||
		strings.TrimSpace(key.Params.Granularity) == "" || strings.TrimSpace(key.Params.Timezone) == "" {
		return fmt.Errorf("invalid team usage snapshot range dimensions")
	}
	return nil
}

func (c *SnapshotCache) loadWithLease(ctx context.Context, key string, loader SnapshotOriginLoader) (*SnapshotCacheResult, error) {
	var stale *snapshotValueEnvelope
	for {
		envelope, found, err := c.read(ctx, key)
		if err != nil {
			return c.loadAuthoritative(ctx, key, nil, loader, false)
		}
		if found {
			now := c.now()
			if !now.After(envelope.FreshUntil) {
				return snapshotResultFromEnvelope(envelope, "fresh", "ok"), nil
			}
			if !now.After(envelope.StaleUntil) {
				stale = envelope
			}
		}

		leaseKey := key + ":lease"
		token := c.options.NewToken()
		acquired, err := c.acquireLease(ctx, leaseKey, token)
		if err != nil {
			return c.loadAuthoritative(ctx, key, stale, loader, false)
		}
		if acquired {
			return c.loadAsLeaseHolder(ctx, key, leaseKey, token, stale, loader)
		}

		for {
			envelope, found, err = c.read(ctx, key)
			if err != nil {
				return c.loadAuthoritative(ctx, key, stale, loader, false)
			}
			if found {
				now := c.now()
				if !now.After(envelope.FreshUntil) {
					return snapshotResultFromEnvelope(envelope, "fresh", "ok"), nil
				}
				if !now.After(envelope.StaleUntil) {
					stale = envelope
				}
			}
			ttl, ttlErr := c.leaseTTL(ctx, leaseKey)
			if errors.Is(ttlErr, readcache.ErrMiss) || ttl <= 0 {
				break
			}
			if ttlErr != nil {
				return c.loadAuthoritative(ctx, key, stale, loader, false)
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
			return snapshotResultFromEnvelope(stale, "stale", "error"), nil
		}
	}
}

func (c *SnapshotCache) loadAsLeaseHolder(ctx context.Context, key, leaseKey, token string, stale *snapshotValueEnvelope, loader SnapshotOriginLoader) (*SnapshotCacheResult, error) {
	defer c.releaseLease(leaseKey, token)

	envelope, found, err := c.read(ctx, key)
	if err != nil {
		return c.loadAuthoritative(ctx, key, stale, loader, false)
	}
	if found {
		now := c.now()
		if !now.After(envelope.FreshUntil) {
			return snapshotResultFromEnvelope(envelope, "fresh", "ok"), nil
		}
		if !now.After(envelope.StaleUntil) {
			stale = envelope
		}
	}
	return c.loadAuthoritative(ctx, key, stale, loader, true)
}

func (c *SnapshotCache) loadAuthoritative(ctx context.Context, key string, stale *snapshotValueEnvelope, loader SnapshotOriginLoader, write bool) (*SnapshotCacheResult, error) {
	loaded, err := loader(ctx)
	if err != nil {
		return nil, err
	}
	if loaded.SnapshotErr != nil {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if stale == nil || c.now().After(stale.StaleUntil) {
			return nil, loaded.SnapshotErr
		}
		return snapshotResultFromEnvelope(stale, "stale", "error"), nil
	}
	if !validOverviewSnapshot(loaded.Snapshot) {
		return nil, fmt.Errorf("team usage origin returned an invalid snapshot")
	}

	envelope := c.newEnvelope(loaded.Snapshot)
	if write {
		encoded, encodeErr := json.Marshal(envelope)
		if encodeErr != nil {
			return nil, fmt.Errorf("encode team usage snapshot cache value: %w", encodeErr)
		}
		ttl := envelope.StaleUntil.Sub(c.now())
		if ttl > 0 {
			_ = c.set(ctx, key, encoded, ttl)
		}
	}
	return snapshotResultFromEnvelope(envelope, "miss", "ok"), nil
}

func (c *SnapshotCache) newEnvelope(snapshot *OverviewResponse) *snapshotValueEnvelope {
	generatedAt := c.now().UTC()
	random := c.options.RandFloat64()
	if random < 0 {
		random = 0
	} else if random > 1 {
		random = 1
	}
	jitter := 0.1 + 0.1*random
	freshWindow := time.Minute - time.Duration(jitter*float64(time.Minute))
	staleWindow := 5*time.Minute - time.Duration(jitter*float64(5*time.Minute))
	return &snapshotValueEnvelope{
		SchemaVersion: snapshotCacheSchemaVersion,
		GeneratedAt:   generatedAt, FreshUntil: generatedAt.Add(freshWindow), StaleUntil: generatedAt.Add(staleWindow),
		Snapshot: snapshot,
	}
}

func (c *SnapshotCache) read(ctx context.Context, key string) (*snapshotValueEnvelope, bool, error) {
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
	var envelope snapshotValueEnvelope
	if err := decoder.Decode(&envelope); err != nil || !validSnapshotEnvelope(&envelope) {
		return nil, false, nil
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, false, nil
	}
	return &envelope, true, nil
}

func validSnapshotEnvelope(envelope *snapshotValueEnvelope) bool {
	if envelope == nil || envelope.SchemaVersion != snapshotCacheSchemaVersion || envelope.GeneratedAt.IsZero() {
		return false
	}
	freshWindow := envelope.FreshUntil.Sub(envelope.GeneratedAt)
	staleWindow := envelope.StaleUntil.Sub(envelope.GeneratedAt)
	if freshWindow < 48*time.Second || freshWindow > 54*time.Second ||
		staleWindow < 4*time.Minute || staleWindow > 4*time.Minute+30*time.Second || staleWindow <= freshWindow {
		return false
	}
	return validOverviewSnapshot(envelope.Snapshot)
}

func validOverviewSnapshot(snapshot *OverviewResponse) bool {
	if snapshot == nil || !snapshot.Configured || !snapshot.IsRepresentative || strings.TrimSpace(snapshot.Summary.UnitLabel) == "" {
		return false
	}
	if strings.TrimSpace(snapshot.Window.StartDate) == "" || strings.TrimSpace(snapshot.Window.EndDate) == "" ||
		strings.TrimSpace(snapshot.Window.Granularity) == "" || strings.TrimSpace(snapshot.Window.Timezone) == "" {
		return false
	}
	return snapshot.TopMembers != nil && snapshot.TopMemberTrend.Series != nil && snapshot.DepartmentTrend.Series != nil &&
		snapshot.Members != nil && snapshot.MemberTree != nil
}

func snapshotResultFromEnvelope(envelope *snapshotValueEnvelope, cacheStatus, sourceStatus string) *SnapshotCacheResult {
	return &SnapshotCacheResult{
		Snapshot: envelope.Snapshot,
		Freshness: SnapshotFreshness{
			AsOf: envelope.GeneratedAt, FreshUntil: envelope.FreshUntil, StaleUntil: envelope.StaleUntil,
			CacheStatus: cacheStatus, SourceStatus: sourceStatus,
		},
	}
}

func (c *SnapshotCache) acquireLease(ctx context.Context, key, token string) (bool, error) {
	commandCtx, cancel := context.WithTimeout(ctx, c.options.CommandTimeout)
	defer cancel()
	return c.store.TryAcquireLease(commandCtx, key, token, c.options.LeaseTTL)
}

func (c *SnapshotCache) leaseTTL(ctx context.Context, key string) (time.Duration, error) {
	commandCtx, cancel := context.WithTimeout(ctx, c.options.CommandTimeout)
	defer cancel()
	return c.store.LeaseTTL(commandCtx, key)
}

func (c *SnapshotCache) set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	commandCtx, cancel := context.WithTimeout(ctx, c.options.CommandTimeout)
	defer cancel()
	return c.store.Set(commandCtx, key, value, ttl)
}

func (c *SnapshotCache) releaseLease(key, token string) {
	ctx, cancel := context.WithTimeout(context.Background(), c.options.ReleaseTimeout)
	defer cancel()
	_, _ = c.store.ReleaseLease(ctx, key, token)
}

func (c *SnapshotCache) now() time.Time {
	return c.options.Now().UTC()
}
