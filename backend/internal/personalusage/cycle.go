package personalusage

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/ai-efficiency/backend/internal/relay"
)

// openaiCycleWindow is the upstream quota window exposed by the OpenAI OAuth
// accounts in a managed pool.
const openaiCycleWindow = 7 * 24 * time.Hour

// OpenAICycleResponse describes the quota cycle of the OpenAI accounts behind
// the caller's OpenAI groups. The cycle ends at the upstream window reset and
// therefore started exactly one window-length earlier.
type OpenAICycleResponse struct {
	Status  string     `json:"status"`
	Message string     `json:"message,omitempty"`
	GroupID string     `json:"group_id,omitempty"`
	ResetAt *time.Time `json:"reset_at,omitempty"`
	StartAt *time.Time `json:"start_at,omitempty"`
}

func unavailableOpenAICycle(message string) *OpenAICycleResponse {
	return &OpenAICycleResponse{Status: "unavailable", Message: message}
}

// OpenAICycle resolves the start of the current OpenAI account quota cycle for
// one user. The upstream accounts report a seven-day window ending at
// NextResetAt, so the cycle started one window-length before that instant.
func (s *Service) OpenAICycle(ctx context.Context, userID int) (*OpenAICycleResponse, error) {
	subject, err := s.resolveSubjectWithOptions(ctx, userID, false)
	if err != nil {
		return nil, err
	}
	if !subject.configured {
		return unavailableOpenAICycle("no usable relay binding"), nil
	}
	reader, ok := subject.provider.(relay.GroupOAuthPoolUsageReader)
	if !ok {
		return unavailableOpenAICycle("relay does not expose account pool resets"), nil
	}
	groups, err := subject.provider.ListAllowedGroupsForUser(ctx, subject.relayUserID)
	if err != nil {
		return unavailableOpenAICycle("load allowed groups failed"), nil
	}
	openaiGroups := make(map[int64]struct{}, len(groups))
	groupIDs := make([]int64, 0, len(groups))
	for _, group := range groups {
		if group.ID <= 0 || !strings.EqualFold(strings.TrimSpace(group.Platform), "openai") {
			continue
		}
		openaiGroups[group.ID] = struct{}{}
		groupIDs = append(groupIDs, group.ID)
	}
	if len(groupIDs) == 0 {
		return unavailableOpenAICycle("no OpenAI group"), nil
	}
	pool, err := reader.ReadGroupOAuthPoolUsage(ctx, groupIDs)
	if err != nil {
		return unavailableOpenAICycle("read OpenAI account pool failed"), nil
	}
	var selected *relay.UserUsageGroupPoolUsageGroupItem
	for index := range pool.Groups {
		item := &pool.Groups[index]
		groupID, parseErr := strconv.ParseInt(strings.TrimSpace(item.GroupID), 10, 64)
		if parseErr != nil {
			continue
		}
		if _, ok := openaiGroups[groupID]; !ok || item.NextResetAt == nil {
			continue
		}
		if selected == nil || item.NextResetAt.Before(*selected.NextResetAt) {
			selected = item
		}
	}
	if selected == nil {
		return unavailableOpenAICycle("OpenAI accounts did not report a reset"), nil
	}
	resetAt := selected.NextResetAt.UTC()
	startAt := resetAt.Add(-openaiCycleWindow)
	return &OpenAICycleResponse{
		Status:  "ok",
		GroupID: selected.GroupID,
		ResetAt: &resetAt,
		StartAt: &startAt,
	}, nil
}
