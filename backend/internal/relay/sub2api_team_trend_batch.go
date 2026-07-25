package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

const teamTrendBatchResponseLimit int64 = 64 << 20

type teamTrendBatchResult struct {
	PointsByUser    map[int64][]UsageTrendPoint
	UniqueUserCount int
	Complete        bool
}

type teamTrendBatchPoint struct {
	Date       string   `json:"date"`
	UserID     int64    `json:"user_id"`
	Tokens     *int64   `json:"tokens"`
	ActualCost *float64 `json:"actual_cost"`
}

type teamTrendBatchData struct {
	Trend *[]teamTrendBatchPoint `json:"trend"`
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
	return 5000
}

func (s *sub2apiRelay) getTeamTrendBatch(
	ctx context.Context,
	requestedUserIDs []int64,
	params TeamMemberTrendParams,
	limit int,
) (teamTrendBatchResult, error) {
	empty := teamTrendBatchResult{PointsByUser: make(map[int64][]UsageTrendPoint)}
	if limit <= 0 || limit > 5000 {
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
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, teamTrendBatchResponseLimit))
	if err != nil {
		return empty, fmt.Errorf("relay: team trend batch: read body: %w", err)
	}
	if int64(len(body)) >= teamTrendBatchResponseLimit {
		return empty, fmt.Errorf("relay: team trend batch: response body reached %d-byte limit", teamTrendBatchResponseLimit)
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return empty, ErrInvalidCredentials
	}
	if resp.StatusCode != http.StatusOK {
		return empty, fmt.Errorf("relay: team trend batch: unexpected status %d%s", resp.StatusCode, relayErrorMessageSuffixFromData(body))
	}

	var envelope teamTrendBatchEnvelope
	decoder := json.NewDecoder(bytes.NewReader(body))
	if err := decoder.Decode(&envelope); err != nil {
		return empty, fmt.Errorf("relay: team trend batch: decode envelope: %w", err)
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return empty, fmt.Errorf("relay: team trend batch: decode envelope: trailing JSON")
		}
		return empty, fmt.Errorf("relay: team trend batch: decode trailing content: %w", err)
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

	return teamTrendBatchResult{
		PointsByUser:    pointsByUser,
		UniqueUserCount: len(uniqueUsers),
		Complete:        len(uniqueUsers) < limit,
	}, nil
}
