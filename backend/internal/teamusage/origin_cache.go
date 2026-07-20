package teamusage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"time"

	"github.com/ai-efficiency/backend/internal/readcache"
	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/ai-efficiency/backend/internal/representativescope"
	"github.com/google/uuid"
)

const (
	scopeOriginSchemaVersion   = 1
	scopeOriginTTL             = time.Minute
	scopeOriginPayloadMaxBytes = 2 << 20
)

type OriginCacheKey struct {
	ProviderID      int
	ProviderVersion int64
	ScopeVersion    string
	ScopeHash       string
	Params          OverviewParams
}

type OriginCacheOptions struct {
	Namespace      string
	CommandTimeout time.Duration
	RefreshTimeout time.Duration
	LeaseTTL       time.Duration
	PollInterval   time.Duration
	ReleaseTimeout time.Duration
	Now            func() time.Time
	NewToken       func() string
	Sleep          func(context.Context, time.Duration) error
	Metrics        readcache.Metrics
}

type OriginCache struct {
	store   readcache.Store
	options OriginCacheOptions
	flights readcache.FlightGroup[*teamUsageScopeOrigin]
}

type teamUsageScopeOrigin struct {
	RelayUserIDs       []int64                            `json:"relay_user_ids"`
	StatsByRelayUserID map[int64]relay.TeamUserUsageStats `json:"stats_by_relay_user_id"`
	PointsByUser       map[int64][]relay.UsageTrendPoint  `json:"points_by_user"`
	subjects           []representativescope.Subject
	sourceErr          error
}

type originCacheEnvelope struct {
	SchemaVersion   int                   `json:"schema_version"`
	ProviderID      int                   `json:"provider_id"`
	ProviderVersion int64                 `json:"provider_version"`
	ScopeVersion    string                `json:"scope_version"`
	ScopeHash       string                `json:"scope_hash"`
	StartDate       string                `json:"start_date"`
	EndDate         string                `json:"end_date"`
	Granularity     string                `json:"granularity"`
	Timezone        string                `json:"timezone"`
	GeneratedAt     time.Time             `json:"generated_at"`
	Origin          *teamUsageScopeOrigin `json:"origin"`
}

type originCacheKeyDimensions struct {
	Namespace       string `json:"namespace"`
	ProviderID      int    `json:"provider_id"`
	ProviderVersion int64  `json:"provider_version"`
	ScopeVersion    string `json:"scope_version"`
	ScopeHash       string `json:"scope_hash"`
	StartDate       string `json:"start_date"`
	EndDate         string `json:"end_date"`
	Granularity     string `json:"granularity"`
	Timezone        string `json:"timezone"`
}

func NewOriginCache(store readcache.Store, options OriginCacheOptions) (*OriginCache, error) {
	if store == nil {
		return nil, fmt.Errorf("team usage origin cache store is required")
	}
	if !snapshotCacheNamespaceRE.MatchString(options.Namespace) {
		return nil, fmt.Errorf("invalid Redis namespace %q", options.Namespace)
	}
	applyOriginCacheDefaults(&options)
	return &OriginCache{store: store, options: options}, nil
}

func applyOriginCacheDefaults(options *OriginCacheOptions) {
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
	if options.NewToken == nil {
		options.NewToken = uuid.NewString
	}
	if options.Sleep == nil {
		options.Sleep = readcache.Sleep
	}
}

func originCacheKey(namespace string, key OriginCacheKey) (string, error) {
	if err := validateOriginCacheKey(key); err != nil {
		return "", err
	}
	dimensions := originCacheKeyDimensions{
		Namespace: namespace, ProviderID: key.ProviderID, ProviderVersion: key.ProviderVersion,
		ScopeVersion: strings.TrimSpace(key.ScopeVersion), ScopeHash: strings.TrimSpace(key.ScopeHash),
		StartDate: strings.TrimSpace(key.Params.StartDate), EndDate: strings.TrimSpace(key.Params.EndDate),
		Granularity: strings.ToLower(strings.TrimSpace(key.Params.Granularity)), Timezone: strings.TrimSpace(key.Params.Timezone),
	}
	encoded, err := json.Marshal(dimensions)
	if err != nil {
		return "", fmt.Errorf("encode team usage origin cache dimensions: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return fmt.Sprintf("ae:%s:team-usage-origin:v1:%x", namespace, digest), nil
}

func validateOriginCacheKey(key OriginCacheKey) error {
	if key.ProviderID <= 0 || key.ProviderVersion <= 0 || strings.TrimSpace(key.ScopeVersion) == "" || strings.TrimSpace(key.ScopeHash) == "" {
		return fmt.Errorf("invalid team usage origin identity dimensions")
	}
	if strings.TrimSpace(key.Params.StartDate) == "" || strings.TrimSpace(key.Params.EndDate) == "" ||
		strings.TrimSpace(key.Params.Granularity) == "" || strings.TrimSpace(key.Params.Timezone) == "" {
		return fmt.Errorf("invalid team usage origin range dimensions")
	}
	return nil
}

func (c *OriginCache) GetOrLoad(
	ctx context.Context,
	key OriginCacheKey,
	loader func(context.Context) (*teamUsageScopeOrigin, error),
) (*teamUsageScopeOrigin, error) {
	if c == nil || c.store == nil {
		return nil, fmt.Errorf("team usage origin cache is not configured")
	}
	if loader == nil {
		return nil, fmt.Errorf("team usage scope origin loader is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	encodedKey, err := originCacheKey(c.options.Namespace, key)
	if err != nil {
		return nil, err
	}
	return c.flights.Do(ctx, encodedKey, c.options.RefreshTimeout, func(sharedCtx context.Context) (*teamUsageScopeOrigin, error) {
		return c.loadWithLease(sharedCtx, encodedKey, key, loader)
	})
}

func (c *OriginCache) loadWithLease(
	ctx context.Context,
	encodedKey string,
	key OriginCacheKey,
	loader func(context.Context) (*teamUsageScopeOrigin, error),
) (*teamUsageScopeOrigin, error) {
	missRecorded := false
	for {
		origin, found, err := c.read(ctx, encodedKey, key)
		if err != nil {
			c.record("error")
			return c.loadAuthoritative(ctx, encodedKey, key, loader, false)
		}
		if found {
			c.record("fresh")
			return origin, nil
		}
		if !missRecorded {
			c.record("miss")
			missRecorded = true
		}

		leaseKey := encodedKey + ":lease"
		token := c.options.NewToken()
		acquired, err := c.acquireLease(ctx, leaseKey, token)
		if err != nil {
			c.record("error")
			c.record("lease_failed")
			return c.loadAuthoritative(ctx, encodedKey, key, loader, false)
		}
		if acquired {
			c.record("lease_acquired")
			return c.loadAsLeaseHolder(ctx, encodedKey, leaseKey, token, key, loader)
		}
		c.record("lease_wait")

		for {
			ttl, ttlErr := c.leaseTTL(ctx, leaseKey)
			if errors.Is(ttlErr, readcache.ErrMiss) {
				break
			}
			if ttlErr != nil {
				c.record("error")
				c.record("lease_failed")
				return c.loadAuthoritative(ctx, encodedKey, key, loader, false)
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

func (c *OriginCache) loadAsLeaseHolder(
	ctx context.Context,
	encodedKey, leaseKey, token string,
	key OriginCacheKey,
	loader func(context.Context) (*teamUsageScopeOrigin, error),
) (*teamUsageScopeOrigin, error) {
	defer c.releaseLease(leaseKey, token)
	origin, found, err := c.read(ctx, encodedKey, key)
	if err != nil {
		c.record("error")
		return c.loadAuthoritative(ctx, encodedKey, key, loader, false)
	}
	if found {
		c.record("fresh")
		return origin, nil
	}
	return c.loadAuthoritative(ctx, encodedKey, key, loader, true)
}

func (c *OriginCache) loadAuthoritative(
	ctx context.Context,
	encodedKey string,
	key OriginCacheKey,
	loader func(context.Context) (*teamUsageScopeOrigin, error),
	write bool,
) (*teamUsageScopeOrigin, error) {
	c.record("refresh")
	origin, err := loader(ctx)
	if err != nil {
		c.record("error")
		return nil, err
	}
	if !validTeamUsageScopeOrigin(origin) {
		c.record("error")
		return nil, fmt.Errorf("team usage origin loader returned an invalid scope origin")
	}
	result := cloneTeamUsageScopeOrigin(origin)
	if !write || origin.sourceErr != nil {
		if origin.sourceErr != nil {
			c.record("error")
		}
		return result, nil
	}
	envelope := originCacheEnvelope{
		SchemaVersion: scopeOriginSchemaVersion, ProviderID: key.ProviderID, ProviderVersion: key.ProviderVersion,
		ScopeVersion: strings.TrimSpace(key.ScopeVersion), ScopeHash: strings.TrimSpace(key.ScopeHash),
		StartDate: strings.TrimSpace(key.Params.StartDate), EndDate: strings.TrimSpace(key.Params.EndDate),
		Granularity: strings.ToLower(strings.TrimSpace(key.Params.Granularity)), Timezone: strings.TrimSpace(key.Params.Timezone),
		GeneratedAt: c.now(), Origin: cloneTeamUsageScopeOrigin(origin),
	}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		c.record("error")
		return result, nil
	}
	if len(encoded) > scopeOriginPayloadMaxBytes {
		c.record("error")
		return result, nil
	}
	if err := c.set(ctx, encodedKey, encoded, scopeOriginTTL); err != nil {
		c.record("error")
	}
	return result, nil
}

func (c *OriginCache) read(ctx context.Context, encodedKey string, key OriginCacheKey) (*teamUsageScopeOrigin, bool, error) {
	commandCtx, cancel := context.WithTimeout(ctx, c.options.CommandTimeout)
	value, err := c.store.Get(commandCtx, encodedKey)
	cancel()
	if errors.Is(err, readcache.ErrMiss) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if len(value) > scopeOriginPayloadMaxBytes {
		return nil, false, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.DisallowUnknownFields()
	var envelope originCacheEnvelope
	if err := decoder.Decode(&envelope); err != nil {
		return nil, false, nil
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, false, nil
	}
	if !c.validEnvelope(&envelope, key) {
		return nil, false, nil
	}
	return cloneTeamUsageScopeOrigin(envelope.Origin), true, nil
}

func (c *OriginCache) validEnvelope(envelope *originCacheEnvelope, key OriginCacheKey) bool {
	if envelope == nil || envelope.SchemaVersion != scopeOriginSchemaVersion || envelope.GeneratedAt.IsZero() ||
		envelope.ProviderID != key.ProviderID || envelope.ProviderVersion != key.ProviderVersion ||
		envelope.ScopeVersion != strings.TrimSpace(key.ScopeVersion) || envelope.ScopeHash != strings.TrimSpace(key.ScopeHash) ||
		envelope.StartDate != strings.TrimSpace(key.Params.StartDate) || envelope.EndDate != strings.TrimSpace(key.Params.EndDate) ||
		envelope.Granularity != strings.ToLower(strings.TrimSpace(key.Params.Granularity)) || envelope.Timezone != strings.TrimSpace(key.Params.Timezone) {
		return false
	}
	age := c.now().Sub(envelope.GeneratedAt)
	return age >= 0 && age < scopeOriginTTL && validTeamUsageScopeOrigin(envelope.Origin)
}

func validTeamUsageScopeOrigin(origin *teamUsageScopeOrigin) bool {
	if origin == nil || origin.RelayUserIDs == nil || origin.StatsByRelayUserID == nil || origin.PointsByUser == nil {
		return false
	}
	authorized := make(map[int64]struct{}, len(origin.RelayUserIDs))
	var previous int64
	for index, relayUserID := range origin.RelayUserIDs {
		if relayUserID <= 0 || (index > 0 && relayUserID <= previous) {
			return false
		}
		authorized[relayUserID] = struct{}{}
		previous = relayUserID
	}
	for relayUserID, stat := range origin.StatsByRelayUserID {
		if _, ok := authorized[relayUserID]; !ok || stat.UserID != relayUserID || !validOriginStat(stat) {
			return false
		}
	}
	for _, relayUserID := range origin.RelayUserIDs {
		if points, ok := origin.PointsByUser[relayUserID]; !ok || points == nil {
			return false
		}
	}
	for relayUserID, points := range origin.PointsByUser {
		if _, ok := authorized[relayUserID]; !ok || points == nil {
			return false
		}
		previousDate := ""
		for _, point := range points {
			date := strings.TrimSpace(point.Date)
			if date == "" || (previousDate != "" && date <= previousDate) || math.IsNaN(point.ActualCost) || math.IsInf(point.ActualCost, 0) || point.ActualCost < 0 ||
				(point.TotalTokens != nil && *point.TotalTokens < 0) {
				return false
			}
			previousDate = date
		}
	}
	return true
}

func validOriginStat(stat relay.TeamUserUsageStats) bool {
	if math.IsNaN(stat.TodayActualCost) || math.IsInf(stat.TodayActualCost, 0) || stat.TodayActualCost < 0 ||
		math.IsNaN(stat.TotalActualCost) || math.IsInf(stat.TotalActualCost, 0) || stat.TotalActualCost < 0 {
		return false
	}
	for _, value := range []*float64{stat.RangeActualCost} {
		if value != nil && (math.IsNaN(*value) || math.IsInf(*value, 0) || *value < 0) {
			return false
		}
	}
	for _, value := range []*int64{stat.TotalTokens, stat.RangeTotalTokens} {
		if value != nil && *value < 0 {
			return false
		}
	}
	return true
}

func cloneTeamUsageScopeOrigin(origin *teamUsageScopeOrigin) *teamUsageScopeOrigin {
	if origin == nil {
		return nil
	}
	cloned := &teamUsageScopeOrigin{
		RelayUserIDs:       append([]int64(nil), origin.RelayUserIDs...),
		StatsByRelayUserID: make(map[int64]relay.TeamUserUsageStats, len(origin.StatsByRelayUserID)),
		PointsByUser:       make(map[int64][]relay.UsageTrendPoint, len(origin.PointsByUser)),
		subjects:           append([]representativescope.Subject(nil), origin.subjects...),
		sourceErr:          origin.sourceErr,
	}
	for relayUserID, stat := range origin.StatsByRelayUserID {
		stat.TotalTokens = cloneInt64Pointer(stat.TotalTokens)
		stat.RangeActualCost = cloneFloat64Pointer(stat.RangeActualCost)
		stat.RangeTotalTokens = cloneInt64Pointer(stat.RangeTotalTokens)
		cloned.StatsByRelayUserID[relayUserID] = stat
	}
	for relayUserID, points := range origin.PointsByUser {
		clonedPoints := make([]relay.UsageTrendPoint, len(points))
		for index, point := range points {
			clonedPoints[index] = point
			clonedPoints[index].TotalTokens = cloneInt64Pointer(point.TotalTokens)
		}
		cloned.PointsByUser[relayUserID] = clonedPoints
	}
	return cloned
}

func cloneFloat64Pointer(value *float64) *float64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func (c *OriginCache) acquireLease(ctx context.Context, key, token string) (bool, error) {
	commandCtx, cancel := context.WithTimeout(ctx, c.options.CommandTimeout)
	defer cancel()
	return c.store.TryAcquireLease(commandCtx, key, token, c.options.LeaseTTL)
}

func (c *OriginCache) leaseTTL(ctx context.Context, key string) (time.Duration, error) {
	commandCtx, cancel := context.WithTimeout(ctx, c.options.CommandTimeout)
	defer cancel()
	return c.store.LeaseTTL(commandCtx, key)
}

func (c *OriginCache) set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	commandCtx, cancel := context.WithTimeout(ctx, c.options.CommandTimeout)
	defer cancel()
	return c.store.Set(commandCtx, key, value, ttl)
}

func (c *OriginCache) releaseLease(key, token string) {
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

func (c *OriginCache) record(outcome string) {
	if c != nil && c.options.Metrics != nil {
		c.options.Metrics.Record(outcome)
	}
}

func (c *OriginCache) now() time.Time {
	return c.options.Now().UTC()
}
