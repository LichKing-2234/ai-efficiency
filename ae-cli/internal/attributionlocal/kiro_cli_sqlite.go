package attributionlocal

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	_ "github.com/glebarez/go-sqlite"
)

type kiroCLIConversationDoc struct {
	ConversationID   string               `json:"conversation_id"`
	History          []kiroCLIHistoryItem `json:"history"`
	UserTurnMetadata struct {
		ContinuationID string `json:"continuation_id"`
		Requests       []any  `json:"requests"`
		UsageInfo      []struct {
			Value float64 `json:"value"`
			Unit  string  `json:"unit"`
		} `json:"usage_info"`
	} `json:"user_turn_metadata"`
}

type kiroCLIHistoryItem struct {
	Assistant struct {
		Response struct {
			MessageID string `json:"message_id"`
		} `json:"Response"`
	} `json:"assistant"`
	RequestMetadata struct {
		RequestID              string  `json:"request_id"`
		ContextUsagePercentage float64 `json:"context_usage_percentage"`
		MessageID              string  `json:"message_id"`
		RequestStartTimestamp  int64   `json:"request_start_timestamp_ms"`
		StreamEndTimestamp     int64   `json:"stream_end_timestamp_ms"`
	} `json:"request_metadata"`
}

// kiroCLIHistoryRequestCount counts the model requests a conversations_v2 row
// records.
//
// user_turn_metadata.requests drops the final answer step: a captured Kiro CLI
// turn with three history entries and three usage_info credit records only
// listed two entries in requests. history[] is the complete request backbone and
// stays index-aligned with user_turn_metadata.usage_info, so it is the source
// consistent with the credits this parser already sums per conversation row.
func kiroCLIHistoryRequestCount(history []kiroCLIHistoryItem) int {
	count := 0
	for _, item := range history {
		if strings.TrimSpace(item.RequestMetadata.RequestID) != "" ||
			strings.TrimSpace(item.RequestMetadata.MessageID) != "" ||
			strings.TrimSpace(item.Assistant.Response.MessageID) != "" {
			count++
		}
	}
	if count == 0 {
		// Never report zero for a row that carried history; fall back to the
		// history length rather than inventing a number.
		count = len(history)
	}
	return count
}

func ParseKiroCLISQLite(path, workspaceRoot string) ([]LocalToolUsageEvent, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	rows, err := db.Query(`SELECT conversation_id, updated_at, value FROM conversations_v2 WHERE key = ? ORDER BY updated_at ASC`, filepath.Clean(workspaceRoot))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []LocalToolUsageEvent
	for rows.Next() {
		var conversationID string
		var updatedAtMillis int64
		var rawValue string
		if err := rows.Scan(&conversationID, &updatedAtMillis, &rawValue); err != nil {
			return nil, err
		}

		var doc kiroCLIConversationDoc
		if err := json.Unmarshal([]byte(rawValue), &doc); err != nil {
			return nil, err
		}
		if strings.TrimSpace(doc.ConversationID) == "" {
			doc.ConversationID = strings.TrimSpace(conversationID)
		}
		if strings.TrimSpace(doc.ConversationID) == "" || len(doc.History) == 0 {
			continue
		}

		latest := doc.History[len(doc.History)-1]
		toolEventID := strings.TrimSpace(latest.RequestMetadata.MessageID)
		if toolEventID == "" {
			toolEventID = strings.TrimSpace(latest.Assistant.Response.MessageID)
		}
		if toolEventID == "" {
			toolEventID = strings.TrimSpace(latest.RequestMetadata.RequestID)
		}
		if toolEventID == "" {
			toolEventID = strings.TrimSpace(doc.UserTurnMetadata.ContinuationID)
		}
		if toolEventID == "" {
			continue
		}

		var credits float64
		for _, usage := range doc.UserTurnMetadata.UsageInfo {
			if strings.EqualFold(strings.TrimSpace(usage.Unit), "credit") {
				credits += usage.Value
			}
		}
		requestCount := kiroCLIHistoryRequestCount(doc.History)

		observedStart := parseUnixMillis(latest.RequestMetadata.RequestStartTimestamp)
		observedEnd := parseUnixMillis(latest.RequestMetadata.StreamEndTimestamp)
		if observedEnd.IsZero() {
			observedEnd = parseUnixMillis(updatedAtMillis)
		}
		if observedStart.IsZero() {
			observedStart = observedEnd
		}

		out = append(out, LocalToolUsageEvent{
			Tool:             "kiro",
			ToolSessionID:    doc.ConversationID,
			ToolEventID:      toolEventID,
			DedupeKey:        fmt.Sprintf("kiro-cli:%s:%s", doc.ConversationID, toolEventID),
			RequestCount:     requestCount,
			UsageUnit:        UsageUnitCredit,
			CreditUsage:      credits,
			ContextUsagePct:  latest.RequestMetadata.ContextUsagePercentage,
			ObservedStartAt:  observedStart,
			ObservedEndAt:    observedEnd,
			RawSourcePath:    path,
			RawSourceLocator: "conversation:" + doc.ConversationID,
			RawPayload: map[string]any{
				"conversation_id":    doc.ConversationID,
				"request_metadata":   latest.RequestMetadata,
				"user_turn_metadata": doc.UserTurnMetadata,
			},
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
