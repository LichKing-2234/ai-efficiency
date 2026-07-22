package teamusage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	"github.com/ai-efficiency/backend/internal/readcache"
)

const (
	prewarmCacheSchemaVersion = 2

	movingFresh    = 75 * time.Second
	movingHard     = 4 * time.Minute
	movingValueTTL = 6 * time.Minute

	historyFresh    = 25 * time.Hour
	historyHard     = 49 * time.Hour
	historyValueTTL = 50 * time.Hour
	manifestTTL     = 3 * time.Minute

	prewarmKeyReferenceMaxBytes        = 512
	prewarmCurrentStatsMaxBytes        = 2 << 20
	prewarmSegmentMaxBytes             = 8 << 20
	prewarmManifestMaxBytes            = 64 << 10
	prewarmTimezoneGenerationMaxBytes  = 16 << 20
	prewarmDefaultReadCommandBudget    = 250 * time.Millisecond
	prewarmDefaultWriteCommandBudget   = 250 * time.Millisecond
	prewarmDefaultLeaseCommandBudget   = 250 * time.Millisecond
	prewarmDefaultReleaseCommandBudget = 250 * time.Millisecond
	prewarmDefaultCommandTimeout       = 2 * time.Second
	prewarmImmutableClaimTTL           = 90 * time.Second
	prewarmPublicationLeaseLimit       = 5
)

type PrewarmCacheIdentity struct {
	ProviderID      int
	ProviderVersion int64
	Timezone        string
	AnchorDate      string
}

type PrewarmLeaseClaim struct {
	Key   string
	Token string
}

type PrewarmValueReference struct {
	Key             string              `json:"key"`
	SchemaVersion   int                 `json:"schema_version"`
	ProviderID      int                 `json:"provider_id"`
	ProviderVersion int64               `json:"provider_version"`
	TimezoneDigest  string              `json:"timezone_digest,omitempty"`
	AnchorDate      string              `json:"anchor_date,omitempty"`
	Class           PrewarmSegmentClass `json:"class,omitempty"`
	GenerationID    string              `json:"generation_id"`
	GeneratedAt     time.Time           `json:"generated_at"`
	FreshUntil      time.Time           `json:"fresh_until"`
	HardExpiresAt   time.Time           `json:"hard_expires_at"`
	SerializedBytes int                 `json:"serialized_bytes"`
	ResponseBytes   int64               `json:"response_bytes"`
	RosterCount     int                 `json:"roster_count,omitempty"`
	RosterDigest    string              `json:"roster_digest,omitempty"`
	Coverage        PrewarmCoverage     `json:"coverage,omitempty"`
	PointCount      int                 `json:"point_count,omitempty"`
	UniqueUserCount int                 `json:"unique_user_count,omitempty"`
}

type PrewarmManifest struct {
	SchemaVersion   int                   `json:"schema_version"`
	ProviderID      int                   `json:"provider_id"`
	ProviderVersion int64                 `json:"provider_version"`
	Timezone        string                `json:"timezone"`
	TimezoneDigest  string                `json:"timezone_digest"`
	AnchorDate      string                `json:"anchor_date"`
	CreatedAt       time.Time             `json:"created_at"`
	CurrentStats    PrewarmValueReference `json:"current_stats"`
	History29d      PrewarmValueReference `json:"history_29d"`
	History6d       PrewarmValueReference `json:"history_6d"`
	TodayHour       PrewarmValueReference `json:"today_hour"`
}

type PrewarmValueStatus string

const (
	PrewarmValueMissing     PrewarmValueStatus = "missing"
	PrewarmValueFresh       PrewarmValueStatus = "fresh"
	PrewarmValueStale       PrewarmValueStatus = "stale"
	PrewarmValueInvalid     PrewarmValueStatus = "invalid"
	PrewarmValueHardExpired PrewarmValueStatus = "hard_expired"
)

type PrewarmCacheResult struct {
	Manifest           PrewarmManifest
	CurrentStats       *PrewarmCurrentStatsEnvelope
	Segments           PrewarmSegmentSet
	Complete           bool
	CurrentStatsStatus PrewarmValueStatus
	History29dStatus   PrewarmValueStatus
	History6dStatus    PrewarmValueStatus
	TodayHourStatus    PrewarmValueStatus
}

type prewarmReadSelection struct {
	currentStats bool
	history29d   bool
	history6d    bool
	todayHour    bool
}

type prewarmReferencedValueError struct {
	indexes []int
	err     error
}

func (e *prewarmReferencedValueError) Error() string { return e.err.Error() }
func (e *prewarmReferencedValueError) Unwrap() error { return e.err }

func prewarmSelectionForWindow(class PrewarmWindowClass) (prewarmReadSelection, error) {
	selection := prewarmReadSelection{currentStats: true, todayHour: true}
	switch class {
	case PrewarmWindowToday:
	case PrewarmWindow7d:
		selection.history6d = true
	case PrewarmWindow30d:
		selection.history29d = true
	default:
		return prewarmReadSelection{}, fmt.Errorf("invalid prewarm window class %q", class)
	}
	return selection, nil
}

func prewarmAllReferencesSelection() prewarmReadSelection {
	return prewarmReadSelection{currentStats: true, history29d: true, history6d: true, todayHour: true}
}

func (s prewarmReadSelection) includes(index int) bool {
	switch index {
	case 0:
		return s.currentStats
	case 1:
		return s.history29d
	case 2:
		return s.history6d
	case 3:
		return s.todayHour
	default:
		return false
	}
}

type PrewarmCacheOptions struct {
	Namespace      string
	ReadTimeout    time.Duration
	WriteTimeout   time.Duration
	LeaseTimeout   time.Duration
	ReleaseTimeout time.Duration
	Now            func() time.Time
	Metrics        PrewarmMetrics
}

type PrewarmCache struct {
	store   readcache.BatchStore
	options PrewarmCacheOptions
}

func (c *PrewarmCache) recordRedisError(operation string, class PrewarmRedisErrorClass) {
	c.options.Metrics.RecordRedisError(operation, class)
}

func (c *PrewarmCache) recordRedisCommandError(
	operation string,
	parentCtx, commandCtx context.Context,
	err error,
) {
	if err == nil || errors.Is(err, readcache.ErrMiss) {
		return
	}
	c.recordRedisError(operation, classifyPrewarmRedisCommandError(parentCtx, commandCtx, err))
}

func classifyPrewarmRedisCommandError(
	parentCtx, commandCtx context.Context,
	err error,
) PrewarmRedisErrorClass {
	if parentCtx != nil && parentCtx.Err() != nil {
		return PrewarmRedisErrorCallerCanceled
	}
	if commandCtx != nil {
		if errors.Is(commandCtx.Err(), context.DeadlineExceeded) {
			return PrewarmRedisErrorCommandDeadline
		}
		if errors.Is(commandCtx.Err(), context.Canceled) {
			return PrewarmRedisErrorCallerCanceled
		}
	}
	if errors.Is(err, context.Canceled) {
		return PrewarmRedisErrorCallerCanceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return PrewarmRedisErrorCommandDeadline
	}
	var networkError net.Error
	if errors.As(err, &networkError) {
		if networkError.Timeout() {
			return PrewarmRedisErrorNetworkTimeout
		}
		return PrewarmRedisErrorNetwork
	}
	return PrewarmRedisErrorCommand
}

func NewPrewarmCache(store readcache.BatchStore, options PrewarmCacheOptions) (*PrewarmCache, error) {
	if store == nil {
		return nil, fmt.Errorf("team usage prewarm cache store is required")
	}
	if !snapshotCacheNamespaceRE.MatchString(options.Namespace) {
		return nil, fmt.Errorf("invalid Redis namespace %q", options.Namespace)
	}
	if options.ReadTimeout <= 0 {
		options.ReadTimeout = prewarmDefaultReadCommandBudget
	}
	if options.WriteTimeout <= 0 {
		options.WriteTimeout = prewarmDefaultWriteCommandBudget
	}
	if options.LeaseTimeout <= 0 {
		options.LeaseTimeout = prewarmDefaultLeaseCommandBudget
	}
	if options.ReleaseTimeout <= 0 {
		options.ReleaseTimeout = prewarmDefaultReleaseCommandBudget
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	options.Metrics = prewarmMetricsOrNoop(options.Metrics)
	for name, timeout := range map[string]time.Duration{
		"read": options.ReadTimeout, "write": options.WriteTimeout,
		"lease": options.LeaseTimeout, "release": options.ReleaseTimeout,
	} {
		if timeout > prewarmDefaultCommandTimeout {
			return nil, fmt.Errorf("team usage prewarm %s timeout exceeds two-second cap", name)
		}
	}
	return &PrewarmCache{store: store, options: options}, nil
}

func (c *PrewarmCache) Read(
	ctx context.Context,
	identity PrewarmCacheIdentity,
) (result *PrewarmCacheResult, found bool, err error) {
	return c.read(ctx, identity, prewarmAllReferencesSelection())
}

func (c *PrewarmCache) ReadWindow(
	ctx context.Context,
	identity PrewarmCacheIdentity,
	class PrewarmWindowClass,
) (result *PrewarmCacheResult, found bool, err error) {
	selection, err := prewarmSelectionForWindow(class)
	if err != nil {
		return nil, false, err
	}
	return c.read(ctx, identity, selection)
}

func (c *PrewarmCache) read(
	ctx context.Context,
	identity PrewarmCacheIdentity,
	selection prewarmReadSelection,
) (result *PrewarmCacheResult, found bool, err error) {
	startedAt := time.Now()
	readBytes := 0
	manifestOutcome := PrewarmCacheError
	defer func() {
		outcome := "hit"
		if err != nil {
			outcome = "error"
		} else if !found {
			outcome = "miss"
		}
		c.options.Metrics.RecordRedis("manifest_read", outcome, time.Since(startedAt), readBytes)
		c.options.Metrics.RecordCache(PrewarmCacheManifest, manifestOutcome)
	}()
	manifestKey, err := prewarmManifestKeyForIdentity(c.options.Namespace, prewarmCacheSchemaVersion, identity)
	if err != nil {
		c.recordRedisError("manifest_read", PrewarmRedisErrorValidation)
		return nil, false, err
	}
	commandCtx, cancel := context.WithTimeout(ctx, c.options.ReadTimeout)
	encodedManifest, err := c.store.Get(commandCtx, manifestKey)
	c.recordRedisCommandError("manifest_read", ctx, commandCtx, err)
	cancel()
	if errors.Is(err, readcache.ErrMiss) {
		manifestOutcome = PrewarmCacheMiss
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("read team usage prewarm manifest: %w", err)
	}
	readBytes = len(encodedManifest)

	var manifest PrewarmManifest
	if err := decodePrewarmJSON(encodedManifest, prewarmManifestMaxBytes, &manifest); err != nil {
		manifestOutcome = PrewarmCacheInvalid
		c.recordRedisError("manifest_read", PrewarmRedisErrorDecodeOrReference)
		return nil, false, fmt.Errorf("decode team usage prewarm manifest: %w", err)
	}
	now := c.now()
	if err := validatePrewarmManifest(c.options.Namespace, identity, manifest, now, false); err != nil {
		manifestOutcome = PrewarmCacheInvalid
		c.recordRedisError("manifest_read", PrewarmRedisErrorDecodeOrReference)
		return nil, false, fmt.Errorf("validate team usage prewarm manifest: %w", err)
	}

	current, segments, statuses, complete, err := c.readReferencedValues(ctx, manifest, now, selection, true)
	if err != nil {
		return nil, false, err
	}
	manifestOutcome = PrewarmCacheFresh
	return &PrewarmCacheResult{
		Manifest:           manifest,
		CurrentStats:       current,
		Segments:           segments,
		Complete:           complete,
		CurrentStatsStatus: statuses[0],
		History29dStatus:   statuses[1],
		History6dStatus:    statuses[2],
		TodayHourStatus:    statuses[3],
	}, true, nil
}

func prewarmValueStatus(now time.Time, ref PrewarmValueReference, available bool) PrewarmValueStatus {
	if !now.Before(ref.HardExpiresAt) {
		return PrewarmValueHardExpired
	}
	if !available {
		return PrewarmValueMissing
	}
	if now.Before(ref.FreshUntil) {
		return PrewarmValueFresh
	}
	return PrewarmValueStale
}

func (c *PrewarmCache) WriteCurrentStats(
	ctx context.Context,
	value PrewarmCurrentStatsEnvelope,
) (PrewarmValueReference, error) {
	if err := validatePrewarmCurrentStatsValue(value); err != nil {
		return PrewarmValueReference{}, err
	}
	encoded, err := encodePrewarmStoredJSON(value, prewarmCurrentStatsMaxBytes, prewarmCurrentStatsMaxBytes)
	if err != nil {
		return PrewarmValueReference{}, fmt.Errorf("encode team usage prewarm current stats: %w", err)
	}
	key, err := prewarmCurrentStatsKey(
		c.options.Namespace,
		prewarmCacheSchemaVersion,
		value.ProviderID,
		value.ProviderVersion,
		value.GenerationID,
	)
	if err != nil {
		return PrewarmValueReference{}, err
	}
	if err := c.writeImmutable(ctx, key, encoded, movingValueTTL); err != nil {
		return PrewarmValueReference{}, fmt.Errorf("write team usage prewarm current stats: %w", err)
	}
	return PrewarmValueReference{
		Key: key, SchemaVersion: value.SchemaVersion, ProviderID: value.ProviderID,
		ProviderVersion: value.ProviderVersion, GenerationID: value.GenerationID,
		GeneratedAt: value.GeneratedAt, FreshUntil: value.GeneratedAt.Add(movingFresh),
		HardExpiresAt: value.GeneratedAt.Add(movingHard), SerializedBytes: len(encoded),
		ResponseBytes: value.ResponseBytes, RosterCount: value.RosterCount, RosterDigest: value.RosterDigest,
	}, nil
}

func (c *PrewarmCache) WriteSegment(
	ctx context.Context,
	value PrewarmTrendSegment,
) (PrewarmValueReference, error) {
	if err := validatePrewarmSegmentValue(value); err != nil {
		return PrewarmValueReference{}, err
	}
	encoded, err := encodePrewarmStoredJSON(value, prewarmSegmentMaxBytes, prewarmSegmentMaxBytes)
	if err != nil {
		return PrewarmValueReference{}, fmt.Errorf("encode team usage prewarm segment: %w", err)
	}
	key, err := prewarmSegmentKey(
		c.options.Namespace,
		prewarmCacheSchemaVersion,
		value.ProviderID,
		value.ProviderVersion,
		value.TimezoneDigest,
		value.AnchorDate,
		value.Class,
		value.GenerationID,
	)
	if err != nil {
		return PrewarmValueReference{}, err
	}
	freshFor, hardFor, ttl, err := prewarmClassTTLs(value.Class)
	if err != nil {
		return PrewarmValueReference{}, err
	}
	if err := c.writeImmutable(ctx, key, encoded, ttl); err != nil {
		return PrewarmValueReference{}, fmt.Errorf("write team usage prewarm segment: %w", err)
	}
	return PrewarmValueReference{
		Key: key, SchemaVersion: value.SchemaVersion, ProviderID: value.ProviderID,
		ProviderVersion: value.ProviderVersion, TimezoneDigest: value.TimezoneDigest,
		AnchorDate: value.AnchorDate, Class: value.Class, GenerationID: value.GenerationID,
		GeneratedAt: value.GeneratedAt, FreshUntil: value.GeneratedAt.Add(freshFor),
		HardExpiresAt: value.GeneratedAt.Add(hardFor), SerializedBytes: len(encoded),
		ResponseBytes: value.ResponseBytes, Coverage: value.Coverage,
		PointCount: value.PointCount, UniqueUserCount: value.UniqueUserCount,
	}, nil
}

func (c *PrewarmCache) PublishManifest(
	ctx context.Context,
	leaseKey, token string,
	manifest PrewarmManifest,
) (bool, error) {
	return c.PublishManifestWithLeases(ctx, []PrewarmLeaseClaim{{Key: leaseKey, Token: token}}, manifest)
}

func (c *PrewarmCache) PublishManifestWithLeases(
	ctx context.Context,
	claims []PrewarmLeaseClaim,
	manifest PrewarmManifest,
) (published bool, err error) {
	published, _, err = c.publishManifestWithLeases(ctx, claims, manifest, nil)
	return published, err
}

func (c *PrewarmCache) publishRecoveredManifestWithLeases(
	ctx context.Context,
	claims []PrewarmLeaseClaim,
	manifest PrewarmManifest,
	class PrewarmWindowClass,
) (published bool, skipped bool, err error) {
	selection, err := prewarmSelectionForWindow(class)
	if err != nil {
		return false, false, err
	}
	return c.publishManifestWithLeases(ctx, claims, manifest, &selection)
}

func (c *PrewarmCache) publishManifestWithLeases(
	ctx context.Context,
	claims []PrewarmLeaseClaim,
	manifest PrewarmManifest,
	requestSelection *prewarmReadSelection,
) (published bool, skipped bool, err error) {
	startedAt := time.Now()
	writtenBytes := 0
	defer func() {
		outcome := "success"
		if err != nil {
			outcome = "error"
		} else if skipped {
			outcome = "skipped"
		} else if !published {
			outcome = "not_owned"
		}
		c.options.Metrics.RecordRedis("manifest_write", outcome, time.Since(startedAt), writtenBytes)
	}()
	if len(claims) == 0 || len(claims) > prewarmPublicationLeaseLimit {
		c.recordRedisError("manifest_write", PrewarmRedisErrorValidation)
		return false, false, fmt.Errorf("team usage prewarm publication requires between one and %d lease claims", prewarmPublicationLeaseLimit)
	}
	leaseKeys := make([]string, len(claims))
	tokens := make([]string, len(claims))
	seen := make(map[string]struct{}, len(claims))
	for index, claim := range claims {
		if err := validatePrewarmLeaseInput(claim.Key, claim.Token, manifestTTL); err != nil {
			c.recordRedisError("manifest_write", PrewarmRedisErrorValidation)
			return false, false, err
		}
		if _, exists := seen[claim.Key]; exists {
			c.recordRedisError("manifest_write", PrewarmRedisErrorValidation)
			return false, false, fmt.Errorf("team usage prewarm publication contains duplicate lease claim")
		}
		seen[claim.Key] = struct{}{}
		leaseKeys[index] = claim.Key
		tokens[index] = claim.Token
	}
	identity := PrewarmCacheIdentity{
		ProviderID: manifest.ProviderID, ProviderVersion: manifest.ProviderVersion,
		Timezone: manifest.Timezone, AnchorDate: manifest.AnchorDate,
	}
	now := c.now()
	if err := validatePrewarmManifest(c.options.Namespace, identity, manifest, now, true); err != nil {
		c.recordRedisError("manifest_write", PrewarmRedisErrorValidation)
		return false, false, fmt.Errorf("validate team usage prewarm manifest: %w", err)
	}
	if err := validatePrewarmPublicationWindow(now, c.options.WriteTimeout, manifest); err != nil {
		c.recordRedisError("manifest_write", PrewarmRedisErrorValidation)
		return false, false, err
	}
	encoded, err := encodePrewarmJSON(manifest, prewarmManifestMaxBytes)
	if err != nil {
		c.recordRedisError("manifest_write", PrewarmRedisErrorValidation)
		return false, false, fmt.Errorf("encode team usage prewarm manifest: %w", err)
	}
	writtenBytes = len(encoded)
	_, _, statuses, complete, err := c.readReferencedValues(ctx, manifest, now, prewarmAllReferencesSelection(), false)
	if err != nil {
		if requestSelection != nil && prewarmOnlyUnselectedHistoryFailed(err, *requestSelection) {
			return false, true, nil
		}
		return false, false, fmt.Errorf("validate team usage prewarm values before publication: %w", err)
	} else if !complete {
		missing := prewarmUnavailableReferenceIndexes(statuses, prewarmAllReferencesSelection())
		missingErr := &prewarmReferencedValueError{indexes: missing, err: readcache.ErrMiss}
		if requestSelection != nil && prewarmOnlyUnselectedHistoryFailed(missingErr, *requestSelection) {
			return false, true, nil
		}
		c.recordRedisError("manifest_write", PrewarmRedisErrorDecodeOrReference)
		return false, false, fmt.Errorf("validate team usage prewarm values before publication: %w", missingErr)
	}
	key, err := prewarmManifestKeyForIdentity(c.options.Namespace, prewarmCacheSchemaVersion, identity)
	if err != nil {
		c.recordRedisError("manifest_write", PrewarmRedisErrorValidation)
		return false, false, err
	}
	if err := validatePrewarmPublicationWindow(c.now(), c.options.WriteTimeout, manifest); err != nil {
		c.recordRedisError("manifest_write", PrewarmRedisErrorValidation)
		return false, false, err
	}
	commandCtx, cancel := context.WithTimeout(ctx, c.options.WriteTimeout)
	published, err = c.store.SetIfLeasesOwned(commandCtx, leaseKeys, tokens, key, encoded, manifestTTL)
	c.recordRedisCommandError("manifest_write", ctx, commandCtx, err)
	cancel()
	if err != nil {
		return false, false, fmt.Errorf("publish team usage prewarm manifest: %w", err)
	}
	return published, false, nil
}

func prewarmUnavailableReferenceIndexes(statuses [4]PrewarmValueStatus, selection prewarmReadSelection) []int {
	indexes := make([]int, 0, len(statuses))
	for index, status := range statuses {
		if selection.includes(index) && status != PrewarmValueFresh && status != PrewarmValueStale {
			indexes = append(indexes, index)
		}
	}
	return indexes
}

func prewarmOnlyUnselectedHistoryFailed(err error, selection prewarmReadSelection) bool {
	var referencedValueErr *prewarmReferencedValueError
	if !errors.As(err, &referencedValueErr) || len(referencedValueErr.indexes) == 0 {
		return false
	}
	for _, index := range referencedValueErr.indexes {
		switch index {
		case 1:
			if selection.history29d {
				return false
			}
		case 2:
			if selection.history6d {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func (c *PrewarmCache) LeaseKey(kind string, dimensions ...string) string {
	encoded, _ := json.Marshal(struct {
		SchemaVersion int      `json:"schema_version"`
		Kind          string   `json:"kind"`
		Dimensions    []string `json:"dimensions"`
	}{SchemaVersion: prewarmCacheSchemaVersion, Kind: kind, Dimensions: dimensions})
	digest := sha256.Sum256(encoded)
	return fmt.Sprintf("ae:%s:team-usage-prewarm:v%d:lease:%x", c.options.Namespace, prewarmCacheSchemaVersion, digest)
}

func (c *PrewarmCache) TryAcquireLease(
	ctx context.Context,
	key, token string,
	ttl time.Duration,
) (acquired bool, err error) {
	startedAt := time.Now()
	defer func() {
		outcome := "acquired"
		if err != nil {
			outcome = "error"
		} else if !acquired {
			outcome = "busy"
		}
		c.options.Metrics.RecordRedis("lease_acquire", outcome, time.Since(startedAt), 0)
	}()
	if err := validatePrewarmLeaseInput(key, token, ttl); err != nil {
		c.recordRedisError("lease_acquire", PrewarmRedisErrorValidation)
		return false, err
	}
	commandCtx, cancel := context.WithTimeout(ctx, c.options.LeaseTimeout)
	acquired, err = c.store.TryAcquireLease(commandCtx, key, token, ttl)
	c.recordRedisCommandError("lease_acquire", ctx, commandCtx, err)
	cancel()
	return acquired, err
}

func (c *PrewarmCache) LeaseTTL(ctx context.Context, key string) (time.Duration, error) {
	startedAt := time.Now()
	if len(key) == 0 || len(key) > prewarmKeyReferenceMaxBytes {
		c.options.Metrics.RecordRedis("lease_ttl", "error", time.Since(startedAt), 0)
		c.recordRedisError("lease_ttl", PrewarmRedisErrorValidation)
		return 0, fmt.Errorf("invalid team usage prewarm lease key")
	}
	commandCtx, cancel := context.WithTimeout(ctx, c.options.LeaseTimeout)
	ttl, err := c.store.LeaseTTL(commandCtx, key)
	c.recordRedisCommandError("lease_ttl", ctx, commandCtx, err)
	cancel()
	outcome := "hit"
	if err != nil {
		outcome = "error"
	}
	c.options.Metrics.RecordRedis("lease_ttl", outcome, time.Since(startedAt), 0)
	return ttl, err
}

func (c *PrewarmCache) ReleaseLease(ctx context.Context, key, token string) (released bool, err error) {
	startedAt := time.Now()
	defer func() {
		outcome := "released"
		if err != nil {
			outcome = "error"
		} else if !released {
			outcome = "not_owned"
		}
		c.options.Metrics.RecordRedis("lease_release", outcome, time.Since(startedAt), 0)
	}()
	if err := validatePrewarmLeaseInput(key, token, time.Nanosecond); err != nil {
		c.recordRedisError("lease_release", PrewarmRedisErrorValidation)
		return false, err
	}
	commandCtx, cancel := context.WithTimeout(ctx, c.options.ReleaseTimeout)
	released, err = c.store.ReleaseLease(commandCtx, key, token)
	c.recordRedisCommandError("lease_release", ctx, commandCtx, err)
	cancel()
	return released, err
}

func (c *PrewarmCache) writeImmutable(ctx context.Context, key string, value []byte, ttl time.Duration) (err error) {
	startedAt := time.Now()
	defer func() {
		outcome := "success"
		if err != nil {
			outcome = "error"
		}
		c.options.Metrics.RecordRedis("immutable_write", outcome, time.Since(startedAt), len(value))
	}()
	digest := sha256.Sum256(value)
	claimToken := "immutable:" + hex.EncodeToString(digest[:])
	commandCtx, cancel := context.WithTimeout(ctx, c.options.WriteTimeout)
	acquired, err := c.store.TryAcquireLease(commandCtx, key, claimToken, prewarmImmutableClaimTTL)
	c.recordRedisCommandError("immutable_write", ctx, commandCtx, err)
	cancel()
	if err != nil {
		return err
	}
	if !acquired {
		return c.waitForImmutableValue(ctx, key, claimToken, value)
	}
	commandCtx, cancel = context.WithTimeout(ctx, c.options.WriteTimeout)
	written, err := c.store.SetIfLeaseOwned(commandCtx, key, claimToken, key, value, ttl)
	c.recordRedisCommandError("immutable_write", ctx, commandCtx, err)
	cancel()
	if err != nil {
		return err
	}
	if !written {
		c.recordRedisError("immutable_write", PrewarmRedisErrorDecodeOrReference)
		return fmt.Errorf("immutable prewarm generation claim was lost")
	}
	return nil
}

func (c *PrewarmCache) waitForImmutableValue(ctx context.Context, key, claimToken string, value []byte) error {
	waitCtx, cancel := context.WithTimeout(ctx, c.options.WriteTimeout)
	defer cancel()
	for {
		existing, err := c.store.Get(waitCtx, key)
		if err != nil {
			if errors.Is(err, readcache.ErrMiss) {
				c.recordRedisError("immutable_write", PrewarmRedisErrorDecodeOrReference)
			} else {
				c.recordRedisCommandError("immutable_write", ctx, waitCtx, err)
			}
			return err
		}
		if bytes.Equal(existing, value) {
			return nil
		}
		if string(existing) != claimToken {
			c.recordRedisError("immutable_write", PrewarmRedisErrorDecodeOrReference)
			return fmt.Errorf("immutable prewarm generation key already contains different bytes")
		}
		if err := readcache.Sleep(waitCtx, 5*time.Millisecond); err != nil {
			c.recordRedisCommandError("immutable_write", ctx, waitCtx, err)
			return fmt.Errorf("wait for identical immutable prewarm generation: %w", err)
		}
	}
}

func (c *PrewarmCache) readReferencedValues(
	ctx context.Context,
	manifest PrewarmManifest,
	now time.Time,
	selection prewarmReadSelection,
	allowInvalidToday bool,
) (*PrewarmCurrentStatsEnvelope, PrewarmSegmentSet, [4]PrewarmValueStatus, bool, error) {
	refs := [...]PrewarmValueReference{manifest.CurrentStats, manifest.History29d, manifest.History6d, manifest.TodayHour}
	var statuses [4]PrewarmValueStatus
	cacheOutcomes := [4]PrewarmCacheOutcome{
		PrewarmCacheError, PrewarmCacheError, PrewarmCacheError, PrewarmCacheError,
	}
	defer func() {
		for index, outcome := range cacheOutcomes {
			if !selection.includes(index) {
				continue
			}
			kind := PrewarmCacheSegment
			if index == 0 {
				kind = PrewarmCacheCurrentStats
			}
			c.options.Metrics.RecordCache(kind, outcome)
		}
	}()
	selectedIndexes := make([]int, 0, len(refs))
	keys := make([]string, 0, len(refs))
	for index := range refs {
		if selection.includes(index) {
			selectedIndexes = append(selectedIndexes, index)
			keys = append(keys, refs[index].Key)
		}
	}
	commandCtx, cancel := context.WithTimeout(ctx, c.options.ReadTimeout)
	startedAt := time.Now()
	values, err := c.store.MGet(commandCtx, keys...)
	c.recordRedisCommandError("generation_read", ctx, commandCtx, err)
	cancel()
	if err != nil {
		c.options.Metrics.RecordRedis("generation_read", "error", time.Since(startedAt), 0)
		return nil, PrewarmSegmentSet{}, statuses, false, fmt.Errorf("read team usage prewarm values: %w", err)
	}
	readBytes := 0
	readOutcome := "hit"
	for _, value := range values {
		readBytes += len(value)
		if value == nil {
			readOutcome = "miss"
		}
	}
	c.options.Metrics.RecordRedis("generation_read", readOutcome, time.Since(startedAt), readBytes)
	if len(values) != len(selectedIndexes) {
		c.recordRedisError("generation_read", PrewarmRedisErrorDecodeOrReference)
		return nil, PrewarmSegmentSet{}, statuses, false, fmt.Errorf("read team usage prewarm values: got %d results, want %d", len(values), len(selectedIndexes))
	}
	var valuesByReference [4][]byte
	for valueIndex, referenceIndex := range selectedIndexes {
		value := values[valueIndex]
		valuesByReference[referenceIndex] = value
		statuses[referenceIndex] = prewarmValueStatus(now, refs[referenceIndex], value != nil)
		cacheOutcomes[referenceIndex] = prewarmCacheOutcomeForStatus(statuses[referenceIndex])
	}

	var current *PrewarmCurrentStatsEnvelope
	var referenceErrors []error
	referenceFailure := false
	if selection.currentStats && statuses[0] != PrewarmValueHardExpired && valuesByReference[0] != nil {
		var decoded PrewarmCurrentStatsEnvelope
		if err := decodePrewarmStoredJSON(valuesByReference[0], prewarmCurrentStatsMaxBytes, &decoded); err != nil {
			referenceFailure = true
			statuses[0] = PrewarmValueInvalid
			cacheOutcomes[0] = PrewarmCacheInvalid
			referenceErrors = append(referenceErrors, fmt.Errorf("decode team usage prewarm current stats: %w", err))
		} else if err := validatePrewarmCurrentStatsReference(c.options.Namespace, manifest.CurrentStats, decoded, len(valuesByReference[0]), now); err != nil {
			referenceFailure = true
			statuses[0] = PrewarmValueInvalid
			cacheOutcomes[0] = PrewarmCacheInvalid
			referenceErrors = append(referenceErrors, fmt.Errorf("validate team usage prewarm current stats: %w", err))
		} else {
			current = &decoded
		}
	}

	segments := make([]*PrewarmTrendSegment, 3)
	for index, ref := range refs[1:] {
		statusIndex := index + 1
		if !selection.includes(statusIndex) || statuses[statusIndex] == PrewarmValueHardExpired || valuesByReference[statusIndex] == nil {
			continue
		}
		var segment PrewarmTrendSegment
		if err := decodePrewarmStoredJSON(valuesByReference[statusIndex], prewarmSegmentMaxBytes, &segment); err != nil {
			referenceFailure = true
			statuses[statusIndex] = PrewarmValueInvalid
			cacheOutcomes[statusIndex] = PrewarmCacheInvalid
			if allowInvalidToday && ref.Class == SegmentTodayHour {
				continue
			}
			referenceErrors = append(referenceErrors, fmt.Errorf("decode team usage prewarm %s: %w", ref.Class, err))
			continue
		} else if err := validatePrewarmSegmentReference(c.options.Namespace, ref, segment, len(valuesByReference[statusIndex]), now); err != nil {
			referenceFailure = true
			statuses[statusIndex] = PrewarmValueInvalid
			cacheOutcomes[statusIndex] = PrewarmCacheInvalid
			if allowInvalidToday && ref.Class == SegmentTodayHour {
				continue
			}
			referenceErrors = append(referenceErrors, fmt.Errorf("validate team usage prewarm %s: %w", ref.Class, err))
			continue
		}
		segments[index] = &segment
	}
	if referenceFailure {
		c.recordRedisError("generation_read", PrewarmRedisErrorDecodeOrReference)
	}
	if len(referenceErrors) > 0 {
		return nil, PrewarmSegmentSet{}, statuses, false, &prewarmReferencedValueError{
			indexes: prewarmUnavailableReferenceIndexes(statuses, selection),
			err:     errors.Join(referenceErrors...),
		}
	}
	result := PrewarmSegmentSet{History29d: segments[0], History6d: segments[1], TodayHour: segments[2]}
	complete := (!selection.currentStats || current != nil) &&
		(!selection.history29d || result.History29d != nil) &&
		(!selection.history6d || result.History6d != nil) &&
		(!selection.todayHour || result.TodayHour != nil)
	return current, result, statuses, complete, nil
}

func prewarmCacheOutcomeForStatus(status PrewarmValueStatus) PrewarmCacheOutcome {
	switch status {
	case PrewarmValueFresh:
		return PrewarmCacheFresh
	case PrewarmValueStale:
		return PrewarmCacheStale
	case PrewarmValueInvalid:
		return PrewarmCacheInvalid
	case PrewarmValueHardExpired:
		return PrewarmCacheHardExpired
	default:
		return PrewarmCacheMiss
	}
}

func validatePrewarmManifest(
	namespace string,
	identity PrewarmCacheIdentity,
	manifest PrewarmManifest,
	now time.Time,
	requireHardValid bool,
) error {
	if err := validatePrewarmCacheIdentity(identity); err != nil {
		return err
	}
	digest := prewarmTimezoneDigest(identity.Timezone)
	if manifest.SchemaVersion != prewarmCacheSchemaVersion ||
		manifest.ProviderID != identity.ProviderID ||
		manifest.ProviderVersion != identity.ProviderVersion ||
		manifest.Timezone != identity.Timezone ||
		manifest.TimezoneDigest != digest ||
		manifest.AnchorDate != identity.AnchorDate ||
		manifest.CreatedAt.IsZero() {
		return fmt.Errorf("prewarm manifest identity does not match requested generation")
	}
	if err := validatePrewarmCurrentReference(namespace, identity, manifest.CurrentStats, now, requireHardValid); err != nil {
		return fmt.Errorf("current stats reference: %w", err)
	}
	segmentRefs := [...]struct {
		class PrewarmSegmentClass
		ref   PrewarmValueReference
	}{
		{class: SegmentHistory29d, ref: manifest.History29d},
		{class: SegmentHistory6d, ref: manifest.History6d},
		{class: SegmentTodayHour, ref: manifest.TodayHour},
	}
	totalBytes := 0
	for _, item := range segmentRefs {
		if err := validatePrewarmSegmentReferenceMetadata(namespace, identity, item.class, item.ref, now, requireHardValid); err != nil {
			return fmt.Errorf("%s reference: %w", item.class, err)
		}
		totalBytes += item.ref.SerializedBytes
	}
	if totalBytes >= prewarmTimezoneGenerationMaxBytes {
		return fmt.Errorf("prewarm timezone generation reached %d-byte limit", prewarmTimezoneGenerationMaxBytes)
	}
	for _, ref := range [...]PrewarmValueReference{manifest.CurrentStats, manifest.History29d, manifest.History6d, manifest.TodayHour} {
		if ref.GeneratedAt.After(manifest.CreatedAt) {
			return fmt.Errorf("prewarm reference was generated after manifest creation")
		}
	}
	return nil
}

func validatePrewarmPublicationWindow(now time.Time, commandMargin time.Duration, manifest PrewarmManifest) error {
	earliestHardExpiry := manifest.CurrentStats.HardExpiresAt
	for _, ref := range [...]PrewarmValueReference{manifest.History29d, manifest.History6d, manifest.TodayHour} {
		if ref.HardExpiresAt.Before(earliestHardExpiry) {
			earliestHardExpiry = ref.HardExpiresAt
		}
	}
	if !now.Add(manifestTTL + commandMargin).Before(earliestHardExpiry) {
		return fmt.Errorf("team usage prewarm manifest TTL plus command margin would reach earliest hard expiry")
	}
	return nil
}

func validatePrewarmCurrentReference(
	namespace string,
	identity PrewarmCacheIdentity,
	ref PrewarmValueReference,
	now time.Time,
	requireHardValid bool,
) error {
	if err := validatePrewarmReferenceCommon(ref, movingFresh, movingHard, prewarmCurrentStatsMaxBytes, now, requireHardValid); err != nil {
		return err
	}
	if ref.SchemaVersion != prewarmCacheSchemaVersion || ref.ProviderID != identity.ProviderID ||
		ref.ProviderVersion != identity.ProviderVersion || ref.TimezoneDigest != "" || ref.AnchorDate != "" || ref.Class != "" ||
		ref.RosterCount < 0 || !validPrewarmDigest(ref.RosterDigest) {
		return fmt.Errorf("invalid current stats reference metadata")
	}
	expectedKey, err := prewarmCurrentStatsKey(namespace, prewarmCacheSchemaVersion, ref.ProviderID, ref.ProviderVersion, ref.GenerationID)
	if err != nil {
		return err
	}
	if ref.Key != expectedKey {
		return fmt.Errorf("current stats reference key does not match metadata")
	}
	return nil
}

func validatePrewarmSegmentReferenceMetadata(
	namespace string,
	identity PrewarmCacheIdentity,
	class PrewarmSegmentClass,
	ref PrewarmValueReference,
	now time.Time,
	requireHardValid bool,
) error {
	freshFor, hardFor, _, err := prewarmClassTTLs(class)
	if err != nil {
		return err
	}
	if err := validatePrewarmReferenceCommon(ref, freshFor, hardFor, prewarmSegmentMaxBytes, now, requireHardValid); err != nil {
		return err
	}
	if ref.SchemaVersion != prewarmCacheSchemaVersion || ref.ProviderID != identity.ProviderID ||
		ref.ProviderVersion != identity.ProviderVersion || ref.TimezoneDigest != prewarmTimezoneDigest(identity.Timezone) ||
		ref.AnchorDate != identity.AnchorDate || ref.Class != class || ref.Coverage.Timezone != identity.Timezone {
		return fmt.Errorf("invalid segment reference metadata")
	}
	expectedCoverage, err := prewarmSegmentCoverage(class, identity.AnchorDate, identity.Timezone)
	if err != nil {
		return err
	}
	if ref.Coverage != expectedCoverage {
		return fmt.Errorf("segment reference coverage does not match class")
	}
	expectedKey, err := prewarmSegmentKey(
		namespace, prewarmCacheSchemaVersion, ref.ProviderID, ref.ProviderVersion,
		ref.TimezoneDigest, ref.AnchorDate, ref.Class, ref.GenerationID,
	)
	if err != nil {
		return err
	}
	if ref.Key != expectedKey {
		return fmt.Errorf("segment reference key does not match metadata")
	}
	return nil
}

func validatePrewarmReferenceCommon(
	ref PrewarmValueReference,
	freshFor, hardFor time.Duration,
	sizeLimit int,
	now time.Time,
	requireHardValid bool,
) error {
	if len(ref.Key) == 0 || len(ref.Key) > prewarmKeyReferenceMaxBytes {
		return fmt.Errorf("prewarm Redis key reference exceeds %d bytes", prewarmKeyReferenceMaxBytes)
	}
	if !validPrewarmGenerationID(ref.GenerationID) || ref.GeneratedAt.IsZero() {
		return fmt.Errorf("invalid prewarm generation metadata")
	}
	if !ref.FreshUntil.Equal(ref.GeneratedAt.Add(freshFor)) || !ref.HardExpiresAt.Equal(ref.GeneratedAt.Add(hardFor)) {
		return fmt.Errorf("prewarm freshness timestamps do not match class contract")
	}
	if requireHardValid && !now.Before(ref.HardExpiresAt) {
		return fmt.Errorf("prewarm value is hard expired")
	}
	if ref.SerializedBytes <= 0 || ref.SerializedBytes >= sizeLimit || ref.ResponseBytes < 0 {
		return fmt.Errorf("invalid prewarm bounded size metadata")
	}
	return nil
}

func validatePrewarmCurrentStatsReference(
	namespace string,
	ref PrewarmValueReference,
	value PrewarmCurrentStatsEnvelope,
	serializedBytes int,
	now time.Time,
) error {
	identity := PrewarmCacheIdentity{ProviderID: value.ProviderID, ProviderVersion: value.ProviderVersion, Timezone: "UTC", AnchorDate: "2000-01-01"}
	if err := validatePrewarmCurrentReference(namespace, identity, ref, now, false); err != nil {
		return err
	}
	if err := validatePrewarmCurrentStatsValue(value); err != nil {
		return err
	}
	if ref.SchemaVersion != value.SchemaVersion || ref.ProviderID != value.ProviderID ||
		ref.ProviderVersion != value.ProviderVersion || ref.GenerationID != value.GenerationID ||
		!ref.GeneratedAt.Equal(value.GeneratedAt) || ref.SerializedBytes != serializedBytes ||
		ref.ResponseBytes != value.ResponseBytes || ref.RosterCount != value.RosterCount || ref.RosterDigest != value.RosterDigest {
		return fmt.Errorf("current stats value does not match manifest reference")
	}
	return nil
}

func validatePrewarmSegmentReference(
	namespace string,
	ref PrewarmValueReference,
	value PrewarmTrendSegment,
	serializedBytes int,
	now time.Time,
) error {
	identity := PrewarmCacheIdentity{
		ProviderID: value.ProviderID, ProviderVersion: value.ProviderVersion,
		Timezone: value.Timezone, AnchorDate: value.AnchorDate,
	}
	if err := validatePrewarmSegmentReferenceMetadata(namespace, identity, value.Class, ref, now, false); err != nil {
		return err
	}
	if err := validatePrewarmSegmentValue(value); err != nil {
		return err
	}
	if ref.SchemaVersion != value.SchemaVersion || ref.ProviderID != value.ProviderID ||
		ref.ProviderVersion != value.ProviderVersion || ref.TimezoneDigest != value.TimezoneDigest ||
		ref.AnchorDate != value.AnchorDate || ref.Class != value.Class || ref.GenerationID != value.GenerationID ||
		!ref.GeneratedAt.Equal(value.GeneratedAt) || ref.SerializedBytes != serializedBytes ||
		ref.ResponseBytes != value.ResponseBytes || ref.Coverage != value.Coverage ||
		ref.PointCount != value.PointCount || ref.UniqueUserCount != value.UniqueUserCount {
		return fmt.Errorf("segment value does not match manifest reference")
	}
	return nil
}

func validatePrewarmCurrentStatsValue(value PrewarmCurrentStatsEnvelope) error {
	if value.SchemaVersion != prewarmCacheSchemaVersion || value.ProviderID <= 0 || value.ProviderVersion <= 0 ||
		!validPrewarmGenerationID(value.GenerationID) || value.GeneratedAt.IsZero() || !validPrewarmDigest(value.RosterDigest) || value.ResponseBytes < 0 {
		return fmt.Errorf("invalid team usage prewarm current stats metadata")
	}
	if _, err := validatePrewarmCurrentStats(value); err != nil {
		return err
	}
	return nil
}

func validatePrewarmSegmentValue(value PrewarmTrendSegment) error {
	if value.SchemaVersion != prewarmCacheSchemaVersion || value.ProviderID <= 0 || value.ProviderVersion <= 0 ||
		!validPrewarmGenerationID(value.GenerationID) || value.GeneratedAt.IsZero() ||
		value.TimezoneDigest != prewarmTimezoneDigest(value.Timezone) {
		return fmt.Errorf("invalid team usage prewarm segment metadata")
	}
	return ValidateTrendSegment(value)
}

func validatePrewarmCacheIdentity(identity PrewarmCacheIdentity) error {
	if identity.ProviderID <= 0 || identity.ProviderVersion <= 0 ||
		strings.TrimSpace(identity.Timezone) != identity.Timezone || identity.Timezone == "" ||
		len(identity.Timezone) > prewarmTimezoneNameMaxBytes || !validPrewarmDayLabel(identity.AnchorDate) {
		return fmt.Errorf("invalid team usage prewarm cache identity")
	}
	if _, err := loadPrewarmLocation(identity.Timezone); err != nil {
		return fmt.Errorf("invalid team usage prewarm timezone: %w", err)
	}
	return nil
}

func prewarmTimezoneDigest(timezone string) string {
	return prewarmStringDigest(timezone)
}

func prewarmManifestKeyForIdentity(namespace string, schemaVersion int, identity PrewarmCacheIdentity) (string, error) {
	if err := validatePrewarmCacheIdentity(identity); err != nil {
		return "", err
	}
	return prewarmManifestKey(namespace, schemaVersion, identity.ProviderID, identity.ProviderVersion, prewarmTimezoneDigest(identity.Timezone), identity.AnchorDate)
}

func prewarmManifestKey(
	namespace string,
	schemaVersion int,
	providerID int,
	providerVersion int64,
	timezoneDigest, anchorDate string,
) (string, error) {
	if err := validatePrewarmKeyDimensions(namespace, schemaVersion, providerID, providerVersion); err != nil {
		return "", err
	}
	if !validPrewarmDigest(timezoneDigest) || !validPrewarmDayLabel(anchorDate) {
		return "", fmt.Errorf("invalid team usage prewarm manifest dimensions")
	}
	return boundedPrewarmKey(fmt.Sprintf(
		"ae:%s:team-usage-prewarm:v%d:manifest:%d:%d:%s:%s",
		namespace, schemaVersion, providerID, providerVersion, timezoneDigest, anchorDate,
	))
}

func prewarmCurrentStatsKey(
	namespace string,
	schemaVersion int,
	providerID int,
	providerVersion int64,
	generationID string,
) (string, error) {
	if err := validatePrewarmKeyDimensions(namespace, schemaVersion, providerID, providerVersion); err != nil {
		return "", err
	}
	if !validPrewarmGenerationID(generationID) {
		return "", fmt.Errorf("invalid team usage prewarm current generation")
	}
	return boundedPrewarmKey(fmt.Sprintf(
		"ae:%s:team-usage-prewarm:v%d:current:%d:%d:%s",
		namespace, schemaVersion, providerID, providerVersion, generationID,
	))
}

func prewarmSegmentKey(
	namespace string,
	schemaVersion int,
	providerID int,
	providerVersion int64,
	timezoneDigest, anchorDate string,
	class PrewarmSegmentClass,
	generationID string,
) (string, error) {
	if err := validatePrewarmKeyDimensions(namespace, schemaVersion, providerID, providerVersion); err != nil {
		return "", err
	}
	if !validPrewarmDigest(timezoneDigest) || !validPrewarmGenerationID(generationID) || !validPrewarmDayLabel(anchorDate) {
		return "", fmt.Errorf("invalid team usage prewarm segment dimensions")
	}
	if _, _, _, err := prewarmClassTTLs(class); err != nil {
		return "", err
	}
	return boundedPrewarmKey(fmt.Sprintf(
		"ae:%s:team-usage-prewarm:v%d:segment:%d:%d:%s:%s:%s:%s",
		namespace, schemaVersion, providerID, providerVersion, timezoneDigest, anchorDate, class, generationID,
	))
}

func validatePrewarmKeyDimensions(namespace string, schemaVersion, providerID int, providerVersion int64) error {
	if namespace != "" && !snapshotCacheNamespaceRE.MatchString(namespace) {
		return fmt.Errorf("invalid Redis namespace %q", namespace)
	}
	if schemaVersion <= 0 || providerID <= 0 || providerVersion <= 0 {
		return fmt.Errorf("invalid team usage prewarm key dimensions")
	}
	return nil
}

func boundedPrewarmKey(key string) (string, error) {
	if len(key) > prewarmKeyReferenceMaxBytes {
		return "", fmt.Errorf("prewarm Redis key reference exceeds %d bytes", prewarmKeyReferenceMaxBytes)
	}
	return key, nil
}

func prewarmClassTTLs(class PrewarmSegmentClass) (time.Duration, time.Duration, time.Duration, error) {
	switch class {
	case SegmentTodayHour:
		return movingFresh, movingHard, movingValueTTL, nil
	case SegmentHistory29d, SegmentHistory6d:
		return historyFresh, historyHard, historyValueTTL, nil
	default:
		return 0, 0, 0, fmt.Errorf("invalid team usage prewarm segment class %q", class)
	}
}

func validatePrewarmLeaseInput(key, token string, ttl time.Duration) error {
	if key == "" || len(key) > prewarmKeyReferenceMaxBytes || token == "" || len(token) > prewarmKeyReferenceMaxBytes || ttl <= 0 {
		return fmt.Errorf("invalid team usage prewarm lease input")
	}
	return nil
}

func validPrewarmDigest(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size && value == strings.ToLower(value)
}

func validPrewarmGenerationID(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	for _, character := range value {
		if (character < 'a' || character > 'z') && (character < 'A' || character > 'Z') &&
			(character < '0' || character > '9') && character != '-' && character != '_' {
			return false
		}
	}
	return true
}

func encodePrewarmJSON(value any, limit int) ([]byte, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	if len(encoded) >= limit {
		return nil, fmt.Errorf("serialized value reached strict %d-byte limit", limit)
	}
	return encoded, nil
}

func decodePrewarmJSON(encoded []byte, limit int, destination any) error {
	if len(encoded) == 0 || len(encoded) >= limit {
		return fmt.Errorf("serialized value is empty or reached strict %d-byte limit", limit)
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return fmt.Errorf("serialized value contains trailing data")
	}
	return nil
}

func (c *PrewarmCache) now() time.Time {
	return c.options.Now().UTC()
}
