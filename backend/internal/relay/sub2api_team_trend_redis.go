package relay

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ai-efficiency/backend/internal/readcache"
	"github.com/google/uuid"
)

const (
	teamTrendRedisSchemaVersion  = 1
	teamTrendRedisTTL            = time.Minute
	teamTrendRedisCommandTimeout = 100 * time.Millisecond
	teamTrendRedisLeaseTTL       = 15 * time.Second
	teamTrendRedisPollInterval   = 25 * time.Millisecond
)

var teamTrendRedisNamespaceRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,62}$`)

type teamTrendRedisCacheOptions struct {
	Store           readcache.MultiStore
	Namespace       string
	ProviderID      int
	ProviderVersion int64
	Metrics         readcache.Metrics
	Now             func() time.Time
	NewToken        func() string
	Sleep           func(context.Context, time.Duration) error
}

type teamTrendRedisCache struct {
	options teamTrendRedisCacheOptions
}

type teamTrendRedisEnvelope struct {
	SchemaVersion   int               `json:"schema_version"`
	ProviderID      int               `json:"provider_id"`
	ProviderVersion int64             `json:"provider_configuration_version"`
	RelayUserID     int64             `json:"relay_user_id"`
	StartDate       string            `json:"start_date"`
	EndDate         string            `json:"end_date"`
	Granularity     string            `json:"granularity"`
	Timezone        string            `json:"timezone"`
	GeneratedAt     time.Time         `json:"generated_at"`
	Points          []UsageTrendPoint `json:"points"`
}

func newTeamTrendRedisCache(options teamTrendRedisCacheOptions) (*teamTrendRedisCache, error) {
	if options.Store == nil {
		return nil, fmt.Errorf("Relay user trend Redis store is required")
	}
	if !teamTrendRedisNamespaceRE.MatchString(options.Namespace) {
		return nil, fmt.Errorf("invalid Redis namespace %q", options.Namespace)
	}
	if options.ProviderID <= 0 {
		return nil, fmt.Errorf("Relay provider ID must be positive")
	}
	if options.ProviderVersion <= 0 {
		return nil, fmt.Errorf("Relay provider configuration version must be positive")
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
	return &teamTrendRedisCache{options: options}, nil
}

func (c *teamTrendRedisCache) Read(
	ctx context.Context,
	userIDs []int64,
	params TeamMemberTrendParams,
) (map[int64][]UsageTrendPoint, []int64, error) {
	uniqueIDs := uniquePositiveTeamTrendRedisIDs(userIDs, false)
	values := make(map[int64][]UsageTrendPoint, len(uniqueIDs))
	if len(uniqueIDs) == 0 {
		return values, []int64{}, nil
	}

	normalized := normalizeTeamTrendRedisParams(params)
	keys := make([]string, len(uniqueIDs))
	for index, userID := range uniqueIDs {
		keys[index] = c.valueKey(userID, normalized)
	}
	commandCtx, cancel := context.WithTimeout(ctx, teamTrendRedisCommandTimeout)
	rawValues, err := c.options.Store.MGet(commandCtx, keys)
	cancel()
	if err != nil {
		c.record("error")
		misses := append([]int64(nil), uniqueIDs...)
		sort.Slice(misses, func(i, j int) bool { return misses[i] < misses[j] })
		return values, misses, fmt.Errorf("read Relay user trend cache: %w", err)
	}
	if len(rawValues) != len(uniqueIDs) {
		c.record("error")
		misses := append([]int64(nil), uniqueIDs...)
		sort.Slice(misses, func(i, j int) bool { return misses[i] < misses[j] })
		return values, misses, fmt.Errorf("read Relay user trend cache: MGET returned %d values for %d keys", len(rawValues), len(uniqueIDs))
	}

	misses := make([]int64, 0, len(uniqueIDs))
	for index, raw := range rawValues {
		userID := uniqueIDs[index]
		if raw == nil {
			misses = append(misses, userID)
			c.record("miss")
			continue
		}
		points, err := c.decodeEnvelope(raw, userID, normalized)
		if err != nil {
			misses = append(misses, userID)
			c.record("malformed")
			continue
		}
		values[userID] = cloneTeamTrendRedisPoints(points)
		c.record("fresh")
	}
	sort.Slice(misses, func(i, j int) bool { return misses[i] < misses[j] })
	return values, misses, nil
}

func (c *teamTrendRedisCache) Write(
	ctx context.Context,
	values map[int64][]UsageTrendPoint,
	params TeamMemberTrendParams,
	source string,
) error {
	if len(values) == 0 {
		return nil
	}

	normalized := normalizeTeamTrendRedisParams(params)
	userIDs := make([]int64, 0, len(values))
	for userID := range values {
		if userID <= 0 {
			c.record("error")
			return fmt.Errorf("Relay user trend cache user ID must be positive")
		}
		userIDs = append(userIDs, userID)
	}
	sort.Slice(userIDs, func(i, j int) bool { return userIDs[i] < userIDs[j] })

	generatedAt := c.now()
	items := make([]readcache.SetItem, 0, len(userIDs))
	for _, userID := range userIDs {
		points := cloneTeamTrendRedisPoints(values[userID])
		if err := validateTeamTrendRedisPoints(points); err != nil {
			c.record("error")
			return fmt.Errorf("encode Relay user trend cache for user %d: %w", userID, err)
		}
		envelope := teamTrendRedisEnvelope{
			SchemaVersion:   teamTrendRedisSchemaVersion,
			ProviderID:      c.options.ProviderID,
			ProviderVersion: c.options.ProviderVersion,
			RelayUserID:     userID,
			StartDate:       normalized.StartDate,
			EndDate:         normalized.EndDate,
			Granularity:     normalized.Granularity,
			Timezone:        normalized.Timezone,
			GeneratedAt:     generatedAt,
			Points:          points,
		}
		raw, err := json.Marshal(envelope)
		if err != nil {
			c.record("error")
			return fmt.Errorf("encode Relay user trend cache for user %d: %w", userID, err)
		}
		items = append(items, readcache.SetItem{
			Key: c.valueKey(userID, normalized), Value: raw, TTL: teamTrendRedisTTL,
		})
	}

	commandCtx, cancel := context.WithTimeout(ctx, teamTrendRedisCommandTimeout)
	err := c.options.Store.SetMany(commandCtx, items)
	cancel()
	if err != nil {
		c.record("error")
		return fmt.Errorf("write Relay user trend cache: %w", err)
	}
	c.record("write")
	if source != "" {
		c.record(source)
	}
	return nil
}

func (c *teamTrendRedisCache) TryAcquireBatchLease(
	ctx context.Context,
	userIDs []int64,
	params TeamMemberTrendParams,
	limit int,
) (leaseKey, token string, acquired bool, err error) {
	missingIDs := uniquePositiveTeamTrendRedisIDs(userIDs, true)
	if len(missingIDs) == 0 {
		return "", "", false, fmt.Errorf("Relay user trend batch lease requires positive user IDs")
	}
	if limit <= 0 {
		return "", "", false, fmt.Errorf("Relay user trend batch lease limit must be positive")
	}
	leaseKey = c.batchLeaseKey(missingIDs, normalizeTeamTrendRedisParams(params), limit)
	token = c.options.NewToken()
	if strings.TrimSpace(token) == "" {
		return leaseKey, "", false, fmt.Errorf("Relay user trend batch lease token is required")
	}
	commandCtx, cancel := context.WithTimeout(ctx, teamTrendRedisCommandTimeout)
	acquired, err = c.options.Store.TryAcquireLease(commandCtx, leaseKey, token, teamTrendRedisLeaseTTL)
	cancel()
	if err != nil {
		c.record("error")
		return leaseKey, token, false, fmt.Errorf("acquire Relay user trend batch lease: %w", err)
	}
	if acquired {
		c.record("lease_acquired")
	} else {
		c.record("lease_wait")
	}
	return leaseKey, token, acquired, nil
}

func (c *teamTrendRedisCache) LeaseTTL(ctx context.Context, leaseKey string) (time.Duration, error) {
	if strings.TrimSpace(leaseKey) == "" {
		return 0, fmt.Errorf("Relay user trend batch lease key is required")
	}
	commandCtx, cancel := context.WithTimeout(ctx, teamTrendRedisCommandTimeout)
	ttl, err := c.options.Store.LeaseTTL(commandCtx, leaseKey)
	cancel()
	if err != nil {
		if !errors.Is(err, readcache.ErrMiss) {
			c.record("error")
		}
		return 0, fmt.Errorf("read Relay user trend batch lease TTL: %w", err)
	}
	return ttl, nil
}

func (c *teamTrendRedisCache) ReleaseBatchLease(leaseKey, token string) {
	if strings.TrimSpace(leaseKey) == "" || strings.TrimSpace(token) == "" {
		c.record("lease_failed")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), teamTrendRedisCommandTimeout)
	released, err := c.options.Store.ReleaseLease(ctx, leaseKey, token)
	cancel()
	if err != nil {
		c.record("error")
	}
	if err != nil || !released {
		c.record("lease_failed")
	}
}

func (c *teamTrendRedisCache) valueKey(userID int64, params TeamMemberTrendParams) string {
	canonical := strings.Join([]string{
		c.options.Namespace,
		strconv.Itoa(teamTrendRedisSchemaVersion),
		strconv.Itoa(c.options.ProviderID),
		strconv.FormatInt(c.options.ProviderVersion, 10),
		strconv.FormatInt(userID, 10),
		params.StartDate,
		params.EndDate,
		params.Granularity,
		params.Timezone,
	}, "\x00")
	digest := sha256.Sum256([]byte(canonical))
	return fmt.Sprintf("%s:relay-user-trend:v1:%s", c.options.Namespace, hex.EncodeToString(digest[:]))
}

func (c *teamTrendRedisCache) batchLeaseKey(userIDs []int64, params TeamMemberTrendParams, limit int) string {
	idParts := make([]string, len(userIDs))
	for index, userID := range userIDs {
		idParts[index] = strconv.FormatInt(userID, 10)
	}
	idsDigest := sha256.Sum256([]byte(strings.Join(idParts, "\x00")))
	canonical := strings.Join([]string{
		c.options.Namespace,
		strconv.Itoa(teamTrendRedisSchemaVersion),
		strconv.Itoa(c.options.ProviderID),
		strconv.FormatInt(c.options.ProviderVersion, 10),
		params.StartDate,
		params.EndDate,
		params.Granularity,
		params.Timezone,
		strconv.Itoa(limit),
		hex.EncodeToString(idsDigest[:]),
	}, "\x00")
	digest := sha256.Sum256([]byte(canonical))
	return fmt.Sprintf("%s:relay-user-trend-batch-lease:v1:%s", c.options.Namespace, hex.EncodeToString(digest[:]))
}

func (c *teamTrendRedisCache) decodeEnvelope(
	raw []byte,
	userID int64,
	params TeamMemberTrendParams,
) ([]UsageTrendPoint, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var envelope teamTrendRedisEnvelope
	if err := decoder.Decode(&envelope); err != nil {
		return nil, fmt.Errorf("decode Relay user trend cache envelope: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("decode Relay user trend cache envelope: trailing content")
	}
	if envelope.SchemaVersion != teamTrendRedisSchemaVersion ||
		envelope.ProviderID != c.options.ProviderID ||
		envelope.ProviderVersion != c.options.ProviderVersion ||
		envelope.RelayUserID != userID || envelope.RelayUserID <= 0 ||
		envelope.StartDate != params.StartDate ||
		envelope.EndDate != params.EndDate ||
		envelope.Granularity != params.Granularity ||
		envelope.Timezone != params.Timezone {
		return nil, fmt.Errorf("Relay user trend cache envelope identity mismatch")
	}
	now := c.now()
	if envelope.GeneratedAt.IsZero() || envelope.GeneratedAt.After(now) || now.Sub(envelope.GeneratedAt) >= teamTrendRedisTTL {
		return nil, fmt.Errorf("Relay user trend cache envelope is not fresh")
	}
	if envelope.Points == nil {
		return nil, fmt.Errorf("Relay user trend cache points are missing")
	}
	if err := validateTeamTrendRedisPoints(envelope.Points); err != nil {
		return nil, err
	}
	return cloneTeamTrendRedisPoints(envelope.Points), nil
}

func validateTeamTrendRedisPoints(points []UsageTrendPoint) error {
	for index, point := range points {
		if strings.TrimSpace(point.Date) == "" {
			return fmt.Errorf("Relay user trend cache point %d date is required", index)
		}
		if math.IsNaN(point.ActualCost) || math.IsInf(point.ActualCost, 0) {
			return fmt.Errorf("Relay user trend cache point %d cost must be finite", index)
		}
		if point.TotalTokens != nil && *point.TotalTokens < 0 {
			return fmt.Errorf("Relay user trend cache point %d tokens must be nonnegative", index)
		}
	}
	return nil
}

func normalizeTeamTrendRedisParams(params TeamMemberTrendParams) TeamMemberTrendParams {
	return TeamMemberTrendParams{
		StartDate:   strings.TrimSpace(params.StartDate),
		EndDate:     strings.TrimSpace(params.EndDate),
		Granularity: strings.ToLower(strings.TrimSpace(params.Granularity)),
		Timezone:    strings.TrimSpace(params.Timezone),
	}
}

func uniquePositiveTeamTrendRedisIDs(userIDs []int64, sorted bool) []int64 {
	seen := make(map[int64]struct{}, len(userIDs))
	unique := make([]int64, 0, len(userIDs))
	for _, userID := range userIDs {
		if userID <= 0 {
			continue
		}
		if _, exists := seen[userID]; exists {
			continue
		}
		seen[userID] = struct{}{}
		unique = append(unique, userID)
	}
	if sorted {
		sort.Slice(unique, func(i, j int) bool { return unique[i] < unique[j] })
	}
	return unique
}

func cloneTeamTrendRedisPoints(points []UsageTrendPoint) []UsageTrendPoint {
	cloned := make([]UsageTrendPoint, len(points))
	for index, point := range points {
		cloned[index] = point
		if point.TotalTokens != nil {
			tokens := *point.TotalTokens
			cloned[index].TotalTokens = &tokens
		}
	}
	return cloned
}

func (c *teamTrendRedisCache) now() time.Time {
	return c.options.Now().UTC()
}

func (c *teamTrendRedisCache) record(outcome string) {
	if c != nil && c.options.Metrics != nil {
		c.options.Metrics.Record(outcome)
	}
}
