package teamusage

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"hash"
	"sort"
	"strconv"
	"time"

	"github.com/ai-efficiency/backend/internal/relay"
)

const prewarmCurrentStatsChunkSize = 500

type ProviderBinding struct {
	ProviderID      int
	ProviderVersion int64
	Provider        relay.Provider
}

type PrimaryProviderBindingResolver interface {
	ResolvePrimaryProviderBinding(context.Context) (ProviderBinding, error)
}

type SourceCallLimiter interface {
	Do(context.Context, func(context.Context) error) error
}

type PrewarmSourceOptions struct {
	Now             func() time.Time
	NewGenerationID func() string
	Metrics         PrewarmMetrics
}

type PrewarmSource struct {
	limiter SourceCallLimiter
	options PrewarmSourceOptions
}

type prewarmSourceFailureKind uint8

const (
	prewarmSourceFailureRelay prewarmSourceFailureKind = iota + 1
	prewarmSourceFailureValidation
)

type prewarmSourceFailure struct {
	kind prewarmSourceFailureKind
	err  error
}

func (e *prewarmSourceFailure) Error() string { return e.err.Error() }
func (e *prewarmSourceFailure) Unwrap() error { return e.err }

func wrapPrewarmSourceFailure(kind prewarmSourceFailureKind, err error) error {
	if err == nil {
		return nil
	}
	return &prewarmSourceFailure{kind: kind, err: err}
}

func NewPrewarmSource(limiter SourceCallLimiter, options PrewarmSourceOptions) (*PrewarmSource, error) {
	if limiter == nil {
		return nil, fmt.Errorf("team usage prewarm source limiter is required")
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.NewGenerationID == nil {
		options.NewGenerationID = newPrewarmRandomID
	}
	options.Metrics = prewarmMetricsOrNoop(options.Metrics)
	return &PrewarmSource{limiter: limiter, options: options}, nil
}

func (s *PrewarmSource) BuildCurrentStats(ctx context.Context, binding ProviderBinding) (PrewarmCurrentStatsEnvelope, error) {
	if err := validateProviderBinding(binding); err != nil {
		return PrewarmCurrentStatsEnvelope{}, err
	}
	provider, ok := binding.Provider.(relay.ProviderWideTeamUsageProvider)
	if !ok {
		return PrewarmCurrentStatsEnvelope{}, fmt.Errorf("relay provider does not support provider-wide team usage")
	}

	var directory relay.ProviderDirectoryResult
	if err := s.limiter.Do(ctx, func(callCtx context.Context) error {
		var err error
		directory, err = provider.GetProviderUserIDs(callCtx)
		return err
	}); err != nil {
		return PrewarmCurrentStatsEnvelope{}, wrapPrewarmSourceFailure(prewarmSourceFailureRelay, fmt.Errorf("fetch provider-wide directory: %w", err))
	}
	if directory.PageCount <= 0 {
		s.options.Metrics.RecordValidation(PrewarmValidationDirectoryPagination, PrewarmValidationRejected)
		return PrewarmCurrentStatsEnvelope{}, wrapPrewarmSourceFailure(prewarmSourceFailureValidation, fmt.Errorf("provider-wide directory pagination is incomplete"))
	}
	s.options.Metrics.RecordValidation(PrewarmValidationDirectoryPagination, PrewarmValidationAccepted)
	if len(directory.UserIDs) >= PrewarmTrendUserLimit {
		s.options.Metrics.RecordValidation(PrewarmValidationProviderIDBound, PrewarmValidationRejected)
		return PrewarmCurrentStatsEnvelope{}, wrapPrewarmSourceFailure(prewarmSourceFailureValidation, fmt.Errorf("provider-wide directory roster reached limit %d", PrewarmTrendUserLimit))
	}
	userIDs, err := normalizePrewarmRoster(directory.UserIDs)
	if err != nil {
		s.options.Metrics.RecordValidation(PrewarmValidationProviderIDBound, PrewarmValidationRejected)
		return PrewarmCurrentStatsEnvelope{}, wrapPrewarmSourceFailure(prewarmSourceFailureValidation, err)
	}
	s.options.Metrics.RecordValidation(PrewarmValidationProviderIDBound, PrewarmValidationAccepted)

	combined := make(map[int64]relay.TeamUserUsageStats, len(userIDs))
	responseBytes := directory.ResponseBytes
	for offset := 0; offset < len(userIDs); offset += prewarmCurrentStatsChunkSize {
		end := offset + prewarmCurrentStatsChunkSize
		if end > len(userIDs) {
			end = len(userIDs)
		}
		chunk := userIDs[offset:end]
		var result relay.ProviderCurrentStatsResult
		if err := s.limiter.Do(ctx, func(callCtx context.Context) error {
			var callErr error
			result, callErr = provider.GetProviderCurrentUsageStats(callCtx, chunk)
			return callErr
		}); err != nil {
			return PrewarmCurrentStatsEnvelope{}, wrapPrewarmSourceFailure(prewarmSourceFailureRelay, fmt.Errorf("fetch provider-wide current stats chunk %d: %w", offset/prewarmCurrentStatsChunkSize, err))
		}
		if err := mergePrewarmCurrentStatsChunk(combined, chunk, result.Stats); err != nil {
			s.options.Metrics.RecordValidation(PrewarmValidationStatsExactCoverage, PrewarmValidationRejected)
			return PrewarmCurrentStatsEnvelope{}, wrapPrewarmSourceFailure(prewarmSourceFailureValidation, fmt.Errorf("validate provider-wide current stats chunk %d: %w", offset/prewarmCurrentStatsChunkSize, err))
		}
		responseBytes += result.ResponseBytes
	}
	if len(combined) != len(userIDs) {
		s.options.Metrics.RecordValidation(PrewarmValidationStatsExactCoverage, PrewarmValidationRejected)
		return PrewarmCurrentStatsEnvelope{}, wrapPrewarmSourceFailure(prewarmSourceFailureValidation, fmt.Errorf("provider-wide current stats do not cover exact directory roster"))
	}

	stats := make([]PrewarmCurrentStat, 0, len(userIDs))
	for _, userID := range userIDs {
		value, exists := combined[userID]
		if !exists {
			s.options.Metrics.RecordValidation(PrewarmValidationStatsExactCoverage, PrewarmValidationRejected)
			return PrewarmCurrentStatsEnvelope{}, wrapPrewarmSourceFailure(prewarmSourceFailureValidation, fmt.Errorf("provider-wide current stats missing roster ID %d", userID))
		}
		stats = append(stats, PrewarmCurrentStat{
			UserID: userID, TodayActualCost: value.TodayActualCost, TotalActualCost: value.TotalActualCost,
			TotalTokens: clonePrewarmInt64Pointer(value.TotalTokens),
		})
	}
	s.options.Metrics.RecordValidation(PrewarmValidationStatsExactCoverage, PrewarmValidationAccepted)
	value := PrewarmCurrentStatsEnvelope{
		SchemaVersion: prewarmCacheSchemaVersion, ProviderID: binding.ProviderID, ProviderVersion: binding.ProviderVersion,
		GenerationID: s.options.NewGenerationID(), GeneratedAt: s.options.Now(), RosterCount: len(userIDs),
		RosterDigest: prewarmRosterDigest(userIDs), ResponseBytes: responseBytes, Stats: stats,
	}
	if err := validatePrewarmCurrentStatsValue(value); err != nil {
		return PrewarmCurrentStatsEnvelope{}, wrapPrewarmSourceFailure(prewarmSourceFailureValidation, fmt.Errorf("validate provider-wide current stats envelope: %w", err))
	}
	return value, nil
}

func (s *PrewarmSource) FetchSegment(
	ctx context.Context,
	binding ProviderBinding,
	timezone, anchorDate string,
	class PrewarmSegmentClass,
) (PrewarmTrendSegment, error) {
	if err := validateProviderBinding(binding); err != nil {
		return PrewarmTrendSegment{}, err
	}
	provider, ok := binding.Provider.(relay.ProviderWideTeamTrendProvider)
	if !ok {
		return PrewarmTrendSegment{}, fmt.Errorf("relay provider does not support provider-wide team usage trend")
	}
	coverage, err := prewarmSegmentCoverage(class, anchorDate, timezone)
	if err != nil {
		return PrewarmTrendSegment{}, err
	}
	params := relay.TeamMemberTrendParams{
		StartDate: coverage.StartDate, EndDate: coverage.EndDate,
		Granularity: coverage.Granularity, Timezone: coverage.Timezone,
	}
	var result relay.ProviderWideTrendResult
	if err := s.limiter.Do(ctx, func(callCtx context.Context) error {
		var callErr error
		result, callErr = provider.GetProviderUsageTrend(callCtx, params, PrewarmTrendUserLimit)
		return callErr
	}); err != nil {
		return PrewarmTrendSegment{}, wrapPrewarmSourceFailure(prewarmSourceFailureRelay, fmt.Errorf("fetch provider-wide %s trend: %w", class, err))
	}
	if !result.Complete {
		s.options.Metrics.RecordValidation(PrewarmValidationRawTrendCompleteness, PrewarmValidationRejected)
		return PrewarmTrendSegment{}, wrapPrewarmSourceFailure(prewarmSourceFailureValidation, fmt.Errorf("provider-wide %s trend is incomplete", class))
	}
	s.options.Metrics.RecordValidation(PrewarmValidationRawTrendCompleteness, PrewarmValidationAccepted)
	if result.Coverage != params {
		s.options.Metrics.RecordValidation(PrewarmValidationRawTrendCoverage, PrewarmValidationRejected)
		return PrewarmTrendSegment{}, wrapPrewarmSourceFailure(prewarmSourceFailureValidation, fmt.Errorf("provider-wide %s trend coverage does not match request", class))
	}
	s.options.Metrics.RecordValidation(PrewarmValidationRawTrendCoverage, PrewarmValidationAccepted)
	if result.ResponseBytes < 0 || result.ResponseBytes >= PrewarmTrendResponseByteLimit ||
		result.PointCount < 0 || result.PointCount >= PrewarmTrendPointLimit || len(result.Points) >= PrewarmTrendPointLimit ||
		result.UniqueUserCount < 0 || result.UniqueUserCount >= PrewarmTrendUserLimit {
		s.options.Metrics.RecordValidation(PrewarmValidationRawTrendLimit, PrewarmValidationRejected)
		return PrewarmTrendSegment{}, wrapPrewarmSourceFailure(prewarmSourceFailureValidation, fmt.Errorf("provider-wide %s trend reached a strict limit", class))
	}
	s.options.Metrics.RecordValidation(PrewarmValidationRawTrendLimit, PrewarmValidationAccepted)
	points := make([]relay.ProviderWideTrendPoint, len(result.Points))
	for index, point := range result.Points {
		points[index] = point
		points[index].TotalTokens = clonePrewarmInt64Pointer(point.TotalTokens)
	}
	segment := PrewarmTrendSegment{
		SchemaVersion: prewarmCacheSchemaVersion, ProviderID: binding.ProviderID, ProviderVersion: binding.ProviderVersion,
		TimezoneDigest: prewarmTimezoneDigest(timezone), GenerationID: s.options.NewGenerationID(), GeneratedAt: s.options.Now(),
		Timezone: timezone, AnchorDate: anchorDate, Class: class, Coverage: PrewarmCoverage{
			StartDate: result.Coverage.StartDate, EndDate: result.Coverage.EndDate,
			Granularity: result.Coverage.Granularity, Timezone: result.Coverage.Timezone,
		},
		Points: points, ResponseBytes: result.ResponseBytes, PointCount: result.PointCount,
		UniqueUserCount: result.UniqueUserCount, Complete: result.Complete,
	}
	if err := ValidateTrendSegment(segment); err != nil {
		return PrewarmTrendSegment{}, wrapPrewarmSourceFailure(prewarmSourceFailureValidation, fmt.Errorf("validate provider-wide %s trend: %w", class, err))
	}
	return segment, nil
}

func validateProviderBinding(binding ProviderBinding) error {
	if binding.ProviderID <= 0 || binding.ProviderVersion <= 0 || binding.Provider == nil {
		return fmt.Errorf("valid primary Relay provider binding is required")
	}
	return nil
}

func normalizePrewarmRoster(userIDs []int64) ([]int64, error) {
	if len(userIDs) >= PrewarmTrendUserLimit {
		return nil, fmt.Errorf("provider-wide directory roster reached limit %d", PrewarmTrendUserLimit)
	}
	normalized := append([]int64(nil), userIDs...)
	sort.Slice(normalized, func(left, right int) bool { return normalized[left] < normalized[right] })
	for index, userID := range normalized {
		if userID <= 0 {
			return nil, fmt.Errorf("provider-wide directory roster contains invalid ID")
		}
		if index > 0 && userID == normalized[index-1] {
			return nil, fmt.Errorf("provider-wide directory roster contains duplicate ID")
		}
	}
	return normalized, nil
}

func mergePrewarmCurrentStatsChunk(
	combined map[int64]relay.TeamUserUsageStats,
	requested []int64,
	stats map[int64]relay.TeamUserUsageStats,
) error {
	if len(stats) != len(requested) {
		return fmt.Errorf("current stats count %d does not match requested count %d", len(stats), len(requested))
	}
	requestedSet := make(map[int64]struct{}, len(requested))
	for _, userID := range requested {
		requestedSet[userID] = struct{}{}
	}
	for key, value := range stats {
		if _, ok := requestedSet[key]; !ok || value.UserID != key {
			return fmt.Errorf("current stats contain an extra or mismatched user ID")
		}
		if !validPrewarmCost(value.TodayActualCost) || !validPrewarmCost(value.TotalActualCost) ||
			(value.TotalTokens != nil && *value.TotalTokens < 0) {
			return fmt.Errorf("current stats contain invalid usage facts")
		}
		if _, exists := combined[key]; exists {
			return fmt.Errorf("current stats chunks overlap")
		}
		combined[key] = value
	}
	return nil
}

func prewarmRosterDigest(userIDs []int64) string {
	digest := sha256.New()
	for _, userID := range userIDs {
		writePrewarmLengthDelimited(digest, strconv.FormatInt(userID, 10))
	}
	return hex.EncodeToString(digest.Sum(nil))
}

func writePrewarmLengthDelimited(target hash.Hash, value string) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(value)))
	_, _ = target.Write(length[:])
	_, _ = target.Write([]byte(value))
}

func newPrewarmRandomID() string {
	var value [32]byte
	if _, err := rand.Read(value[:]); err != nil {
		panic(fmt.Sprintf("generate team usage prewarm random ID: %v", err))
	}
	return hex.EncodeToString(value[:])
}
