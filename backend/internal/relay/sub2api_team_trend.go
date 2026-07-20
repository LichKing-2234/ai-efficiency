package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ai-efficiency/backend/internal/readcache"
	"go.uber.org/zap"
)

const teamTrendBatchOriginTimeout = 12 * time.Second

func (s *sub2apiRelay) GetUsageTrendForUsers(ctx context.Context, relayUserIDs []int64, params TeamMemberTrendParams) (map[int64][]UsageTrendPoint, error) {
	requested := uniqueTeamTrendUserIDs(relayUserIDs)
	hits, misses := s.readTeamTrendCacheFailOpen(ctx, requested, params)
	if len(misses) == 0 {
		return cloneUsageTrendMap(hits), nil
	}
	if len(misses) >= 2 {
		resolved, unresolved, err := s.loadTeamTrendBatchMisses(ctx, requested, misses, params)
		if err != nil {
			return nil, err
		}
		mergeUsageTrendMap(hits, resolved)
		misses = unresolved
	}
	fallback, err := s.loadIndividualTeamTrendMisses(ctx, misses, params)
	if err != nil {
		return nil, err
	}
	mergeUsageTrendMap(hits, fallback)
	return cloneUsageTrendMap(hits), nil
}

func uniqueTeamTrendUserIDs(userIDs []int64) []int64 {
	seen := make(map[int64]struct{}, len(userIDs))
	unique := make([]int64, 0, len(userIDs))
	for _, userID := range userIDs {
		if _, exists := seen[userID]; exists {
			continue
		}
		seen[userID] = struct{}{}
		unique = append(unique, userID)
	}
	return unique
}

func (s *sub2apiRelay) readTeamTrendCacheFailOpen(
	ctx context.Context,
	requested []int64,
	params TeamMemberTrendParams,
) (map[int64][]UsageTrendPoint, []int64) {
	hits := make(map[int64][]UsageTrendPoint, len(requested))
	positive := uniquePositiveTeamTrendRedisIDs(requested, false)
	if s.teamTrends != nil && len(positive) > 0 && ctx.Err() == nil {
		cached, _, err := s.teamTrends.Read(ctx, positive, params)
		if err != nil {
			s.logTeamTrendRedisFailure("read", len(positive), err)
		} else {
			mergeUsageTrendMap(hits, cached)
		}
	}

	misses := make([]int64, 0, len(requested))
	for _, userID := range requested {
		if userID <= 0 {
			misses = append(misses, userID)
			continue
		}
		if _, found := hits[userID]; !found {
			misses = append(misses, userID)
		}
	}
	return hits, misses
}

func (s *sub2apiRelay) loadTeamTrendBatchMisses(
	ctx context.Context,
	requested []int64,
	misses []int64,
	params TeamMemberTrendParams,
) (map[int64][]UsageTrendPoint, []int64, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	positiveMisses := uniquePositiveTeamTrendRedisIDs(misses, false)
	if len(positiveMisses) < 2 {
		return map[int64][]UsageTrendPoint{}, append([]int64(nil), misses...), nil
	}
	limit := teamTrendBatchLimit(len(uniquePositiveTeamTrendRedisIDs(requested, false)))
	if s.teamTrends == nil {
		return s.runTeamTrendBatch(ctx, misses, positiveMisses, params, limit)
	}

	leaseKey, token, acquired, err := s.teamTrends.TryAcquireBatchLease(ctx, positiveMisses, params, limit)
	if err != nil {
		s.teamTrends.record("lease_failed")
		return s.runTeamTrendBatch(ctx, misses, positiveMisses, params, limit)
	}
	if acquired {
		defer s.teamTrends.ReleaseBatchLease(leaseKey, token)
		return s.loadTeamTrendLeaseHolder(ctx, misses, positiveMisses, params, limit)
	}
	return s.waitForTeamTrendLease(ctx, misses, positiveMisses, params, limit, leaseKey)
}

func (s *sub2apiRelay) loadTeamTrendLeaseHolder(
	ctx context.Context,
	misses []int64,
	positiveMisses []int64,
	params TeamMemberTrendParams,
	limit int,
) (map[int64][]UsageTrendPoint, []int64, error) {
	resolved, remaining := s.readTeamTrendCacheFailOpen(ctx, positiveMisses, params)
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	remaining = uniquePositiveTeamTrendRedisIDs(remaining, false)
	if len(remaining) < 2 {
		return resolved, unresolvedTeamTrendIDs(misses, resolved), nil
	}

	batched, _, err := s.runTeamTrendBatch(ctx, misses, remaining, params, limit)
	if err != nil {
		return nil, nil, err
	}
	mergeUsageTrendMap(resolved, batched)
	return resolved, unresolvedTeamTrendIDs(misses, resolved), nil
}

func (s *sub2apiRelay) waitForTeamTrendLease(
	ctx context.Context,
	misses []int64,
	positiveMisses []int64,
	params TeamMemberTrendParams,
	limit int,
	leaseKey string,
) (map[int64][]UsageTrendPoint, []int64, error) {
	resolved := make(map[int64][]UsageTrendPoint, len(positiveMisses))
	pending := append([]int64(nil), positiveMisses...)
	for {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		if err := s.teamTrends.options.Sleep(ctx, teamTrendRedisPollInterval); err != nil {
			return nil, nil, err
		}

		cached, cacheMisses := s.readTeamTrendCacheFailOpen(ctx, pending, params)
		mergeUsageTrendMap(resolved, cached)
		pending = uniquePositiveTeamTrendRedisIDs(cacheMisses, false)
		if len(pending) == 0 {
			return resolved, unresolvedTeamTrendIDs(misses, resolved), nil
		}
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}

		ttl, err := s.teamTrends.LeaseTTL(ctx, leaseKey)
		if err != nil && !errors.Is(err, readcache.ErrMiss) {
			s.teamTrends.record("lease_failed")
			batched, _, batchErr := s.runTeamTrendBatch(ctx, misses, pending, params, limit)
			if batchErr != nil {
				return nil, nil, batchErr
			}
			mergeUsageTrendMap(resolved, batched)
			return resolved, unresolvedTeamTrendIDs(misses, resolved), nil
		}
		if ttl > 0 {
			continue
		}
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}

		newLeaseKey, token, acquired, err := s.teamTrends.TryAcquireBatchLease(ctx, pending, params, limit)
		if err != nil {
			s.teamTrends.record("lease_failed")
			batched, _, batchErr := s.runTeamTrendBatch(ctx, misses, pending, params, limit)
			if batchErr != nil {
				return nil, nil, batchErr
			}
			mergeUsageTrendMap(resolved, batched)
			return resolved, unresolvedTeamTrendIDs(misses, resolved), nil
		}
		leaseKey = newLeaseKey
		if !acquired {
			continue
		}
		defer s.teamTrends.ReleaseBatchLease(newLeaseKey, token)
		holderValues, _, holderErr := s.loadTeamTrendLeaseHolder(ctx, misses, pending, params, limit)
		if holderErr != nil {
			return nil, nil, holderErr
		}
		mergeUsageTrendMap(resolved, holderValues)
		return resolved, unresolvedTeamTrendIDs(misses, resolved), nil
	}
}

func (s *sub2apiRelay) runTeamTrendBatch(
	ctx context.Context,
	misses []int64,
	positiveMisses []int64,
	params TeamMemberTrendParams,
	limit int,
) (map[int64][]UsageTrendPoint, []int64, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	batchCtx, cancel := context.WithTimeout(ctx, teamTrendBatchOriginTimeout)
	s.recordTeamTrendCacheOutcome("batch_origin")
	result, err := s.getTeamTrendBatch(batchCtx, positiveMisses, params, limit)
	cancel()
	if err != nil {
		if callerErr := ctx.Err(); callerErr != nil {
			return nil, nil, callerErr
		}
		return map[int64][]UsageTrendPoint{}, append([]int64(nil), misses...), nil
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if !result.Complete {
		s.recordTeamTrendCacheOutcome("possible_truncation")
	}

	allowed := make(map[int64]struct{}, len(positiveMisses))
	for _, userID := range positiveMisses {
		allowed[userID] = struct{}{}
	}
	resolved := make(map[int64][]UsageTrendPoint, len(positiveMisses))
	for userID, points := range result.PointsByUser {
		if _, ok := allowed[userID]; ok {
			resolved[userID] = cloneUsageTrendPoints(points)
		}
	}
	if result.Complete {
		for _, userID := range positiveMisses {
			if _, found := resolved[userID]; !found {
				resolved[userID] = []UsageTrendPoint{}
			}
		}
	}
	s.writeTeamTrendCacheFailOpen(ctx, resolved, params)
	return resolved, unresolvedTeamTrendIDs(misses, resolved), nil
}

func (s *sub2apiRelay) loadIndividualTeamTrendMisses(
	ctx context.Context,
	misses []int64,
	params TeamMemberTrendParams,
) (map[int64][]UsageTrendPoint, error) {
	out := make(map[int64][]UsageTrendPoint, len(misses))
	if len(misses) == 0 {
		return out, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.recordTeamTrendCacheOutcome("individual_fallback")

	trendCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	workers := maxConcurrentTeamTrendOrigins
	if len(misses) < workers {
		workers = len(misses)
	}
	type trendResult struct {
		relayUserID int64
		points      []UsageTrendPoint
		err         error
	}
	jobs := make(chan int64)
	results := make(chan trendResult, len(misses))
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for relayUserID := range jobs {
				trend, err := s.teamTrendOrigins.Do(trendCtx, func(originCtx context.Context) ([]UsageTrendPoint, error) {
					return s.getTeamMemberTrend(originCtx, relayUserID, params)
				})
				if err == nil && relayUserID > 0 && trendCtx.Err() == nil {
					s.writeTeamTrendCacheFailOpen(trendCtx, map[int64][]UsageTrendPoint{relayUserID: trend}, params)
				}
				results <- trendResult{relayUserID: relayUserID, points: trend, err: err}
			}
		}()
	}
	go func() {
		defer close(jobs)
		for _, relayUserID := range misses {
			select {
			case jobs <- relayUserID:
			case <-trendCtx.Done():
				return
			}
		}
	}()
	go func() {
		wg.Wait()
		close(results)
	}()

	completed := 0
	for result := range results {
		completed++
		if result.err != nil {
			cancel()
			return nil, result.err
		}
		out[result.relayUserID] = cloneUsageTrendPoints(result.points)
	}
	if completed != len(misses) {
		if err := trendCtx.Err(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (s *sub2apiRelay) writeTeamTrendCacheFailOpen(
	ctx context.Context,
	values map[int64][]UsageTrendPoint,
	params TeamMemberTrendParams,
) {
	if s.teamTrends == nil || len(values) == 0 || ctx.Err() != nil {
		return
	}
	positive := make(map[int64][]UsageTrendPoint, len(values))
	for userID, points := range values {
		if userID > 0 {
			positive[userID] = cloneUsageTrendPoints(points)
		}
	}
	if len(positive) == 0 {
		return
	}
	if err := s.teamTrends.Write(ctx, positive, params, ""); err != nil {
		s.logTeamTrendRedisFailure("write", len(positive), err)
	}
}

func (s *sub2apiRelay) recordTeamTrendCacheOutcome(outcome string) {
	if s.teamTrends != nil {
		s.teamTrends.record(outcome)
	}
}

func (s *sub2apiRelay) logTeamTrendRedisFailure(operation string, userCount int, err error) {
	if s.logger == nil || s.teamTrends == nil {
		return
	}
	s.logger.Warn("Relay team trend Redis "+operation+" failed open",
		zap.Int("provider_id", s.teamTrends.options.ProviderID),
		zap.Int("user_count", userCount),
		zap.String("error_class", teamTrendErrorClass(err)),
	)
}

func teamTrendErrorClass(err error) string {
	switch {
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	default:
		return "redis"
	}
}

func unresolvedTeamTrendIDs(requested []int64, resolved map[int64][]UsageTrendPoint) []int64 {
	unresolved := make([]int64, 0, len(requested))
	for _, userID := range requested {
		if _, found := resolved[userID]; !found {
			unresolved = append(unresolved, userID)
		}
	}
	return unresolved
}

func mergeUsageTrendMap(destination, source map[int64][]UsageTrendPoint) {
	for userID, points := range source {
		destination[userID] = cloneUsageTrendPoints(points)
	}
}

func cloneUsageTrendMap(values map[int64][]UsageTrendPoint) map[int64][]UsageTrendPoint {
	cloned := make(map[int64][]UsageTrendPoint, len(values))
	mergeUsageTrendMap(cloned, values)
	return cloned
}

func cloneUsageTrendPoints(points []UsageTrendPoint) []UsageTrendPoint {
	cloned := make([]UsageTrendPoint, len(points))
	for index, point := range points {
		cloned[index] = point
		if point.TotalTokens != nil {
			totalTokens := *point.TotalTokens
			cloned[index].TotalTokens = &totalTokens
		}
	}
	return cloned
}

func (s *sub2apiRelay) getTeamMemberTrend(ctx context.Context, relayUserID int64, params TeamMemberTrendParams) ([]UsageTrendPoint, error) {
	query := url.Values{}
	if v := strings.TrimSpace(params.StartDate); v != "" {
		query.Set("start_date", v)
	}
	if v := strings.TrimSpace(params.EndDate); v != "" {
		query.Set("end_date", v)
	}
	if v := strings.TrimSpace(params.Granularity); v != "" {
		query.Set("granularity", v)
	}
	if v := strings.TrimSpace(params.Timezone); v != "" {
		query.Set("timezone", v)
	}
	query.Set("user_id", strconv.FormatInt(relayUserID, 10))

	var raw json.RawMessage
	if err := s.getAdminEnvelopeJSON(ctx, "/api/v1/admin/dashboard/trend", query, &raw); err != nil {
		return nil, fmt.Errorf("relay: team member trend: %w", err)
	}

	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil, nil
	}

	var points []UsageTrendPoint
	if len(raw) > 0 && raw[0] == '[' {
		if err := json.Unmarshal(raw, &points); err != nil {
			return nil, fmt.Errorf("relay: team member trend: decode points: %w", err)
		}
		return points, nil
	}

	var envelope struct {
		Trend []UsageTrendPoint `json:"trend"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("relay: team member trend: decode envelope: %w", err)
	}
	return envelope.Trend, nil
}
