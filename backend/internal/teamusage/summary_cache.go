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

const (
	snapshotCacheSchemaVersion = 2
	summaryCacheSchemaVersion  = 1
)

var snapshotCacheNamespaceRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,62}$`)

type SnapshotCache struct {
	overview *readModelCache[*OverviewResponse]
	summary  *readModelCache[*SummarySnapshot]
}

type readModelCache[T any] struct {
	store         readcache.Store
	options       SnapshotCacheOptions
	keyPrefix     string
	schemaVersion int
	validate      func(T) bool
	flights       readcache.FlightGroup[*readModelCacheResult[T]]
}

type readModelCacheResult[T any] struct {
	Snapshot  T
	Freshness SnapshotFreshness
}

type readModelOriginLoadResult[T any] struct {
	Snapshot    T
	SnapshotErr error
}

type readModelOriginLoader[T any] func(context.Context) (readModelOriginLoadResult[T], error)

type readModelValueEnvelope[T any] struct {
	SchemaVersion int       `json:"schema_version"`
	GeneratedAt   time.Time `json:"generated_at"`
	FreshUntil    time.Time `json:"fresh_until"`
	StaleUntil    time.Time `json:"stale_until"`
	Snapshot      T         `json:"snapshot"`
}

type snapshotValueEnvelope = readModelValueEnvelope[*OverviewResponse]

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
	return &SnapshotCache{
		overview: &readModelCache[*OverviewResponse]{
			store: store, options: options, keyPrefix: "team-usage-snapshot",
			schemaVersion: snapshotCacheSchemaVersion, validate: validOverviewSnapshot,
		},
		summary: &readModelCache[*SummarySnapshot]{
			store: store, options: options, keyPrefix: "team-usage-summary",
			schemaVersion: summaryCacheSchemaVersion, validate: validSummarySnapshot,
		},
	}, nil
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
	return readModelCacheKey(namespace, "team-usage-snapshot", key)
}

func summaryCacheKey(namespace string, key SnapshotCacheKey) (string, error) {
	return readModelCacheKey(namespace, "team-usage-summary", key)
}

func readModelCacheKey(namespace, keyPrefix string, key SnapshotCacheKey) (string, error) {
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
	return fmt.Sprintf("ae:%s:%s:v1:%x", namespace, keyPrefix, digest), nil
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
	if c == nil || c.overview == nil {
		return nil, fmt.Errorf("team usage snapshot cache is not configured")
	}
	if loader == nil {
		return nil, fmt.Errorf("team usage snapshot origin loader is required")
	}
	result, err := c.overview.getOrLoad(ctx, key, func(ctx context.Context) (readModelOriginLoadResult[*OverviewResponse], error) {
		loaded, err := loader(ctx)
		return readModelOriginLoadResult[*OverviewResponse]{Snapshot: loaded.Snapshot, SnapshotErr: loaded.SnapshotErr}, err
	})
	if err != nil {
		return nil, err
	}
	return &SnapshotCacheResult{Snapshot: result.Snapshot, Freshness: result.Freshness}, nil
}

func (c *SnapshotCache) GetSummaryOrLoad(ctx context.Context, key SnapshotCacheKey, loader SummaryOriginLoader) (*SummaryCacheResult, error) {
	if c == nil || c.summary == nil {
		return nil, fmt.Errorf("team usage summary cache is not configured")
	}
	if loader == nil {
		return nil, fmt.Errorf("team usage summary origin loader is required")
	}
	result, err := c.summary.getOrLoad(ctx, key, func(ctx context.Context) (readModelOriginLoadResult[*SummarySnapshot], error) {
		loaded, err := loader(ctx)
		return readModelOriginLoadResult[*SummarySnapshot]{Snapshot: loaded.Snapshot, SnapshotErr: loaded.SnapshotErr}, err
	})
	if err != nil {
		return nil, err
	}
	return &SummaryCacheResult{Snapshot: result.Snapshot, Freshness: result.Freshness}, nil
}

func (c *readModelCache[T]) getOrLoad(ctx context.Context, key SnapshotCacheKey, loader readModelOriginLoader[T]) (*readModelCacheResult[T], error) {
	if c == nil || c.store == nil {
		return nil, fmt.Errorf("team usage read model cache is not configured")
	}
	if err := validateSnapshotCacheKey(key); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	encodedKey, err := readModelCacheKey(c.options.Namespace, c.keyPrefix, key)
	if err != nil {
		return nil, err
	}
	return c.flights.Do(ctx, encodedKey, c.options.RefreshTimeout, func(sharedCtx context.Context) (*readModelCacheResult[T], error) {
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

func (c *readModelCache[T]) loadWithLease(ctx context.Context, key string, loader readModelOriginLoader[T]) (*readModelCacheResult[T], error) {
	var stale *readModelValueEnvelope[T]
	for {
		envelope, found, err := c.read(ctx, key)
		if err != nil {
			return c.loadAuthoritative(ctx, key, nil, loader, false)
		}
		if found {
			now := c.now()
			if !now.After(envelope.FreshUntil) {
				return readModelResultFromEnvelope(envelope, "fresh", "ok"), nil
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
					return readModelResultFromEnvelope(envelope, "fresh", "ok"), nil
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
			return readModelResultFromEnvelope(stale, "stale", "error"), nil
		}
	}
}

func (c *readModelCache[T]) loadAsLeaseHolder(ctx context.Context, key, leaseKey, token string, stale *readModelValueEnvelope[T], loader readModelOriginLoader[T]) (*readModelCacheResult[T], error) {
	defer c.releaseLease(leaseKey, token)

	envelope, found, err := c.read(ctx, key)
	if err != nil {
		return c.loadAuthoritative(ctx, key, stale, loader, false)
	}
	if found {
		now := c.now()
		if !now.After(envelope.FreshUntil) {
			return readModelResultFromEnvelope(envelope, "fresh", "ok"), nil
		}
		if !now.After(envelope.StaleUntil) {
			stale = envelope
		}
	}
	return c.loadAuthoritative(ctx, key, stale, loader, true)
}

func (c *readModelCache[T]) loadAuthoritative(ctx context.Context, key string, stale *readModelValueEnvelope[T], loader readModelOriginLoader[T], write bool) (*readModelCacheResult[T], error) {
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
		return readModelResultFromEnvelope(stale, "stale", "error"), nil
	}
	if c.validate == nil || !c.validate(loaded.Snapshot) {
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
	return readModelResultFromEnvelope(envelope, "miss", "ok"), nil
}

func (c *readModelCache[T]) newEnvelope(snapshot T) *readModelValueEnvelope[T] {
	generatedAt := c.now()
	random := c.options.RandFloat64()
	if random < 0 {
		random = 0
	} else if random > 1 {
		random = 1
	}
	jitter := 0.1 + 0.1*random
	freshWindow := time.Minute - time.Duration(jitter*float64(time.Minute))
	staleWindow := 5*time.Minute - time.Duration(jitter*float64(5*time.Minute))
	return &readModelValueEnvelope[T]{
		SchemaVersion: c.schemaVersion,
		GeneratedAt:   generatedAt,
		FreshUntil:    generatedAt.Add(freshWindow),
		StaleUntil:    generatedAt.Add(staleWindow),
		Snapshot:      snapshot,
	}
}

func (c *readModelCache[T]) read(ctx context.Context, key string) (*readModelValueEnvelope[T], bool, error) {
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
	var envelope readModelValueEnvelope[T]
	if err := decoder.Decode(&envelope); err != nil || !c.validEnvelope(&envelope) {
		return nil, false, nil
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, false, nil
	}
	return &envelope, true, nil
}

func (c *readModelCache[T]) validEnvelope(envelope *readModelValueEnvelope[T]) bool {
	if envelope == nil || envelope.SchemaVersion != c.schemaVersion || envelope.GeneratedAt.IsZero() {
		return false
	}
	freshWindow := envelope.FreshUntil.Sub(envelope.GeneratedAt)
	staleWindow := envelope.StaleUntil.Sub(envelope.GeneratedAt)
	if freshWindow < 48*time.Second || freshWindow > 54*time.Second ||
		staleWindow < 4*time.Minute || staleWindow > 4*time.Minute+30*time.Second || staleWindow <= freshWindow {
		return false
	}
	return c.validate != nil && c.validate(envelope.Snapshot)
}

func validOverviewSnapshot(snapshot *OverviewResponse) bool {
	if snapshot == nil || !snapshot.Configured || !snapshot.IsRepresentative || strings.TrimSpace(snapshot.Summary.UnitLabel) == "" {
		return false
	}
	if !validOverviewWindow(snapshot.Window) {
		return false
	}
	return snapshot.TopMembers != nil && snapshot.TopMemberTrend.Series != nil && snapshot.DepartmentTrend.Series != nil &&
		snapshot.Members != nil && snapshot.MemberTree != nil
}

func validSummarySnapshot(snapshot *SummarySnapshot) bool {
	return snapshot != nil && validOverviewWindow(snapshot.Window) && strings.TrimSpace(snapshot.Summary.UnitLabel) != ""
}

func validOverviewWindow(window OverviewWindow) bool {
	return strings.TrimSpace(window.StartDate) != "" && strings.TrimSpace(window.EndDate) != "" &&
		strings.TrimSpace(window.Granularity) != "" && strings.TrimSpace(window.Timezone) != ""
}

func readModelResultFromEnvelope[T any](envelope *readModelValueEnvelope[T], cacheStatus, sourceStatus string) *readModelCacheResult[T] {
	return &readModelCacheResult[T]{
		Snapshot: envelope.Snapshot,
		Freshness: SnapshotFreshness{
			AsOf: envelope.GeneratedAt, FreshUntil: envelope.FreshUntil, StaleUntil: envelope.StaleUntil,
			CacheStatus: cacheStatus, SourceStatus: sourceStatus,
		},
	}
}

func (c *readModelCache[T]) acquireLease(ctx context.Context, key, token string) (bool, error) {
	commandCtx, cancel := context.WithTimeout(ctx, c.options.CommandTimeout)
	defer cancel()
	return c.store.TryAcquireLease(commandCtx, key, token, c.options.LeaseTTL)
}

func (c *readModelCache[T]) leaseTTL(ctx context.Context, key string) (time.Duration, error) {
	commandCtx, cancel := context.WithTimeout(ctx, c.options.CommandTimeout)
	defer cancel()
	return c.store.LeaseTTL(commandCtx, key)
}

func (c *readModelCache[T]) set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	commandCtx, cancel := context.WithTimeout(ctx, c.options.CommandTimeout)
	defer cancel()
	return c.store.Set(commandCtx, key, value, ttl)
}

func (c *readModelCache[T]) releaseLease(key, token string) {
	ctx, cancel := context.WithTimeout(context.Background(), c.options.ReleaseTimeout)
	defer cancel()
	_, _ = c.store.ReleaseLease(ctx, key, token)
}

func (c *readModelCache[T]) now() time.Time {
	return c.options.Now().UTC()
}
