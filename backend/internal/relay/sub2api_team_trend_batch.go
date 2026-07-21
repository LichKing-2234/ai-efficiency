package relay

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

const (
	teamTrendBatchResponseLimit    int64 = 32 << 20
	teamTrendFallbackResponseLimit int64 = 64 << 20
	teamTrendBatchPointLimit             = 1_000_000
	teamTrendBatchUserLimit              = 5000
)

type teamTrendFallbackResult struct {
	PointsByUser map[int64][]UsageTrendPoint
	Complete     bool
}

type teamTrendBatchPoint struct {
	Date       string   `json:"date"`
	UserID     int64    `json:"user_id"`
	Tokens     *int64   `json:"tokens"`
	ActualCost *float64 `json:"actual_cost"`
}

type teamTrendBatchData struct {
	Trend       *[]teamTrendBatchPoint `json:"trend"`
	StartDate   string                 `json:"start_date"`
	EndDate     string                 `json:"end_date"`
	Granularity string                 `json:"granularity"`
}

type teamTrendBatchEnvelope struct {
	envelopeStatus
	Data *teamTrendBatchData `json:"data"`
}

type teamTrendBatchPointKey struct {
	UserID int64
	Date   string
}

func teamTrendBatchLimit(_ int) int {
	return teamTrendBatchUserLimit
}

// getTeamTrendFallback retains the PR #192 requested-user compatibility
// contract. Provider-wide prewarm reads use GetProviderUsageTrend instead.
func (s *sub2apiRelay) getTeamTrendFallback(
	ctx context.Context,
	requestedUserIDs []int64,
	params TeamMemberTrendParams,
	limit int,
) (teamTrendFallbackResult, error) {
	empty := teamTrendFallbackResult{PointsByUser: make(map[int64][]UsageTrendPoint)}
	if limit <= 0 || limit > teamTrendBatchUserLimit {
		return empty, fmt.Errorf("relay: team trend batch: invalid limit %d", limit)
	}

	query := url.Values{}
	query.Set("start_date", params.StartDate)
	query.Set("end_date", params.EndDate)
	query.Set("granularity", params.Granularity)
	query.Set("timezone", params.Timezone)
	query.Set("limit", strconv.Itoa(limit))
	path := "/api/v1/admin/dashboard/users-trend?" + query.Encode()

	resp, err := s.doAdminRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return empty, fmt.Errorf("relay: team trend batch: fetch: %w", err)
	}
	body, readErr := readBodyStrictlyBelow(resp.Body, teamTrendFallbackResponseLimit)
	resp.Body.Close()
	if readErr != nil {
		return empty, fmt.Errorf("relay: team trend batch: read body: %w", readErr)
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return empty, ErrInvalidCredentials
	}
	if resp.StatusCode != http.StatusOK {
		return empty, fmt.Errorf("relay: team trend batch: unexpected status %d%s", resp.StatusCode, relayErrorMessageSuffixFromData(body))
	}

	var envelope teamTrendBatchEnvelope
	if err := decodeSingleJSON(body, &envelope); err != nil {
		return empty, fmt.Errorf("relay: team trend batch: decode envelope: %w", err)
	}
	if envelope.Code != nil && (*envelope.Code == http.StatusUnauthorized || *envelope.Code == http.StatusForbidden) {
		return empty, ErrInvalidCredentials
	}
	if !envelope.ok() {
		return empty, fmt.Errorf("relay: team trend batch: request failed%s", envelope.envelopeStatus.messageSuffix())
	}
	if envelope.Data == nil || envelope.Data.Trend == nil {
		return empty, fmt.Errorf("relay: team trend batch: missing trend data")
	}

	rows := *envelope.Data.Trend
	uniqueUsers := make(map[int64]struct{}, len(rows))
	seenPoints := make(map[teamTrendBatchPointKey]struct{}, len(rows))
	for index, row := range rows {
		if row.UserID <= 0 {
			return empty, fmt.Errorf("relay: team trend batch: row %d has invalid user ID", index)
		}
		if strings.TrimSpace(row.Date) == "" {
			return empty, fmt.Errorf("relay: team trend batch: row %d has blank date", index)
		}
		if row.Tokens != nil && *row.Tokens < 0 {
			return empty, fmt.Errorf("relay: team trend batch: row %d has negative tokens", index)
		}
		if row.ActualCost == nil {
			return empty, fmt.Errorf("relay: team trend batch: row %d is missing actual cost", index)
		}
		if math.IsNaN(*row.ActualCost) || math.IsInf(*row.ActualCost, 0) {
			return empty, fmt.Errorf("relay: team trend batch: row %d has non-finite actual cost", index)
		}

		uniqueUsers[row.UserID] = struct{}{}
		if len(uniqueUsers) > limit {
			return empty, fmt.Errorf("relay: team trend batch: unique user count exceeds limit %d", limit)
		}
		key := teamTrendBatchPointKey{UserID: row.UserID, Date: row.Date}
		if _, exists := seenPoints[key]; exists {
			return empty, fmt.Errorf("relay: team trend batch: duplicate user/date row")
		}
		seenPoints[key] = struct{}{}
	}

	requested := make(map[int64]struct{}, len(requestedUserIDs))
	for _, userID := range requestedUserIDs {
		requested[userID] = struct{}{}
	}
	pointsByUser := make(map[int64][]UsageTrendPoint, len(requested))
	for _, row := range rows {
		if _, allowed := requested[row.UserID]; !allowed {
			continue
		}
		pointsByUser[row.UserID] = append(pointsByUser[row.UserID], UsageTrendPoint{
			Date: row.Date, ActualCost: *row.ActualCost, TotalTokens: row.Tokens,
		})
	}
	for userID := range pointsByUser {
		sort.Slice(pointsByUser[userID], func(left, right int) bool {
			return pointsByUser[userID][left].Date < pointsByUser[userID][right].Date
		})
	}

	return teamTrendFallbackResult{
		PointsByUser: pointsByUser,
		Complete:     len(uniqueUsers) < limit,
	}, nil
}

func (s *sub2apiRelay) GetProviderUsageTrend(
	ctx context.Context,
	params TeamMemberTrendParams,
	limit int,
) (ProviderWideTrendResult, error) {
	empty := ProviderWideTrendResult{}
	if limit <= 0 || limit > teamTrendBatchUserLimit {
		return empty, fmt.Errorf("relay: provider team trend: invalid limit %d", limit)
	}

	coverage := TeamMemberTrendParams{
		StartDate: strings.TrimSpace(params.StartDate), EndDate: strings.TrimSpace(params.EndDate),
		Granularity: strings.TrimSpace(params.Granularity), Timezone: strings.TrimSpace(params.Timezone),
	}
	query := url.Values{}
	query.Set("start_date", coverage.StartDate)
	query.Set("end_date", coverage.EndDate)
	query.Set("granularity", coverage.Granularity)
	query.Set("timezone", coverage.Timezone)
	query.Set("limit", strconv.Itoa(limit))
	path := "/api/v1/admin/dashboard/users-trend?" + query.Encode()

	resp, err := s.doAdminRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return empty, fmt.Errorf("relay: provider team trend: fetch: %w", err)
	}
	body, readErr := readBodyStrictlyBelow(resp.Body, teamTrendBatchResponseLimit)
	resp.Body.Close()
	if readErr != nil {
		var limitErr *responseBodyLimitError
		if errors.As(readErr, &limitErr) {
			return empty, NewProviderSourceRejection(ProviderSourceRejectionRawTrendLimit, fmt.Errorf("relay: provider team trend: read body: %w", readErr))
		}
		return empty, fmt.Errorf("relay: provider team trend: read body: %w", readErr)
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return empty, ErrInvalidCredentials
	}
	if resp.StatusCode != http.StatusOK {
		return empty, fmt.Errorf("relay: provider team trend: unexpected status %d", resp.StatusCode)
	}

	var envelope teamTrendBatchEnvelope
	if err := decodeSingleJSON(body, &envelope); err != nil {
		return empty, fmt.Errorf("relay: provider team trend: decode envelope: %w", err)
	}
	if envelope.Code != nil && (*envelope.Code == http.StatusUnauthorized || *envelope.Code == http.StatusForbidden) {
		return empty, ErrInvalidCredentials
	}
	if !envelope.ok() {
		return empty, fmt.Errorf("relay: provider team trend: request failed")
	}
	if envelope.Data == nil || envelope.Data.Trend == nil {
		return empty, fmt.Errorf("relay: provider team trend: missing trend data")
	}
	if strings.TrimSpace(envelope.Data.StartDate) != coverage.StartDate ||
		strings.TrimSpace(envelope.Data.EndDate) != coverage.EndDate ||
		strings.TrimSpace(envelope.Data.Granularity) != coverage.Granularity {
		return empty, NewProviderSourceRejection(ProviderSourceRejectionRawTrendCoverage, fmt.Errorf("relay: provider team trend: source coverage does not match request"))
	}

	rows := *envelope.Data.Trend
	if err := validateProviderWideTrendPointCountLimit(rows, s.effectiveProviderWideTrendPointLimit()); err != nil {
		return empty, err
	}
	uniqueUsers := make(map[int64]struct{}, len(rows))
	seenPoints := make(map[teamTrendBatchPointKey]struct{}, len(rows))
	lastLabels := make(map[int64]string, len(rows))
	points := make([]ProviderWideTrendPoint, 0, len(rows))
	for index, row := range rows {
		if err := validateProviderWideTrendPoint(row, index, coverage.Granularity); err != nil {
			return empty, err
		}

		if previous, exists := lastLabels[row.UserID]; exists && row.Date <= previous {
			return empty, fmt.Errorf("relay: provider team trend: row %d is not strictly source ordered", index)
		}
		lastLabels[row.UserID] = row.Date
		key := teamTrendBatchPointKey{UserID: row.UserID, Date: row.Date}
		if _, exists := seenPoints[key]; exists {
			return empty, fmt.Errorf("relay: provider team trend: duplicate user/source-label row")
		}
		seenPoints[key] = struct{}{}

		uniqueUsers[row.UserID] = struct{}{}
		if len(uniqueUsers) > limit {
			return empty, NewProviderSourceRejection(ProviderSourceRejectionRawTrendCompleteness, fmt.Errorf("relay: provider team trend: unique user count exceeds requested limit %d", limit))
		}
		if len(uniqueUsers) >= teamTrendBatchUserLimit {
			return empty, NewProviderSourceRejection(ProviderSourceRejectionRawTrendLimit, fmt.Errorf("relay: provider team trend: unique user count reached truncation limit %d", teamTrendBatchUserLimit))
		}
		points = append(points, ProviderWideTrendPoint{
			UserID: row.UserID, Date: row.Date, ActualCost: *row.ActualCost, TotalTokens: row.Tokens,
		})
	}
	if len(uniqueUsers) >= limit {
		return empty, NewProviderSourceRejection(ProviderSourceRejectionRawTrendCompleteness, fmt.Errorf("relay: provider team trend: source reached requested completeness limit %d", limit))
	}

	return ProviderWideTrendResult{
		Points: points, Coverage: coverage, ResponseBytes: int64(len(body)),
		PointCount: len(points), UniqueUserCount: len(uniqueUsers), Complete: true,
	}, nil
}

func validateProviderWideTrendPointCount(rows []teamTrendBatchPoint) error {
	return validateProviderWideTrendPointCountLimit(rows, teamTrendBatchPointLimit)
}

func validateProviderWideTrendPointCountLimit(rows []teamTrendBatchPoint, limit int) error {
	if len(rows) >= limit {
		return NewProviderSourceRejection(ProviderSourceRejectionRawTrendLimit, fmt.Errorf("relay: provider team trend: point count reached limit %d", limit))
	}
	return nil
}

func (s *sub2apiRelay) effectiveProviderWideTrendPointLimit() int {
	if s.providerWideTrendPointLimit > 0 && s.providerWideTrendPointLimit <= teamTrendBatchPointLimit {
		return s.providerWideTrendPointLimit
	}
	return teamTrendBatchPointLimit
}

func validateProviderWideTrendPoint(row teamTrendBatchPoint, index int, granularity string) error {
	if row.UserID <= 0 {
		return fmt.Errorf("relay: provider team trend: row %d has invalid user ID", index)
	}
	if !validTeamTrendSourceLabel(row.Date, granularity) {
		return fmt.Errorf("relay: provider team trend: row %d has invalid source label", index)
	}
	if row.Tokens != nil && *row.Tokens < 0 {
		return fmt.Errorf("relay: provider team trend: row %d has negative tokens", index)
	}
	if row.ActualCost == nil {
		return fmt.Errorf("relay: provider team trend: row %d is missing actual cost", index)
	}
	if *row.ActualCost < 0 || math.IsNaN(*row.ActualCost) || math.IsInf(*row.ActualCost, 0) {
		return fmt.Errorf("relay: provider team trend: row %d has invalid actual cost", index)
	}
	return nil
}

func validTeamTrendSourceLabel(label, granularity string) bool {
	if strings.TrimSpace(label) != label || label == "" {
		return false
	}
	switch granularity {
	case "":
		return true
	case "day":
		return len(label) == len("2006-01-02") && validASCIIDigits(label, 0, 4) && label[4] == '-' &&
			validASCIIDigits(label, 5, 7) && label[7] == '-' && validASCIIDigits(label, 8, 10)
	case "hour":
		return len(label) == len("2006-01-02 15:04") && validASCIIDigits(label, 0, 4) && label[4] == '-' &&
			validASCIIDigits(label, 5, 7) && label[7] == '-' && validASCIIDigits(label, 8, 10) &&
			label[10] == ' ' && validASCIIDigits(label, 11, 13) && label[13] == ':' && validASCIIDigits(label, 14, 16)
	default:
		return false
	}
}

func validASCIIDigits(value string, start, end int) bool {
	for index := start; index < end; index++ {
		if value[index] < '0' || value[index] > '9' {
			return false
		}
	}
	return true
}
