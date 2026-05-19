package attributionlocal

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func ParseKiroJSON(path, workspaceRoot string) ([]LocalToolUsageEvent, error) {
	var doc struct {
		SessionID string `json:"session_id"`
		CWD       string `json:"cwd"`
		SessionState struct {
			ConversationMetadata struct {
				UserTurnMetadatas []struct {
					MessageIDs             []string `json:"message_ids"`
					TotalRequestCount      int      `json:"total_request_count"`
					ContextUsagePercentage float64  `json:"context_usage_percentage"`
					MeteringUsage          []struct {
						Value float64 `json:"value"`
						Unit  string  `json:"unit"`
					} `json:"metering_usage"`
					InputTokenCount  int64 `json:"input_token_count"`
					OutputTokenCount int64 `json:"output_token_count"`
				} `json:"user_turn_metadatas"`
			} `json:"conversation_metadata"`
			RTSModelState struct {
				ConversationID string `json:"conversation_id"`
			} `json:"rts_model_state"`
		} `json:"session_state"`
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	if filepath.Clean(doc.CWD) != filepath.Clean(workspaceRoot) {
		return nil, nil
	}

	sessionID := strings.TrimSpace(doc.SessionState.RTSModelState.ConversationID)
	if sessionID == "" {
		sessionID = strings.TrimSpace(doc.SessionID)
	}
	if sessionID == "" {
		return nil, nil
	}
	observedAt := sourceFileObservedAt(path)

	out := make([]LocalToolUsageEvent, 0, len(doc.SessionState.ConversationMetadata.UserTurnMetadatas))
	for idx, turn := range doc.SessionState.ConversationMetadata.UserTurnMetadatas {
		var credits float64
		for _, usage := range turn.MeteringUsage {
			if usage.Unit == "credit" {
				credits += usage.Value
			}
		}
		out = append(out, LocalToolUsageEvent{
			Tool:             "kiro",
			ToolSessionID:    sessionID,
			ToolEventID:      fmt.Sprintf("turn-%d", idx),
			DedupeKey:        fmt.Sprintf("kiro:%s:%d", sessionID, idx),
			RequestCount:     turn.TotalRequestCount,
			UsageUnit:        UsageUnitCredit,
			CreditUsage:      credits,
			ContextUsagePct:  turn.ContextUsagePercentage,
			InputTokens:      turn.InputTokenCount,
			OutputTokens:     turn.OutputTokenCount,
			ObservedStartAt:  observedAt,
			ObservedEndAt:    observedAt,
			RawSourcePath:    path,
			RawSourceLocator: fmt.Sprintf("turn:%d", idx),
			RawPayload: map[string]any{
				"message_ids": turn.MessageIDs,
			},
		})
	}
	return out, nil
}
