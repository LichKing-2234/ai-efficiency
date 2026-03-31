package collector

import (
	"encoding/json"
	"os"
	"strings"
)

type kiroSession struct {
	CWD          string `json:"cwd"`
	SessionState struct {
		RTSModelState struct {
			ConversationID         string  `json:"conversation_id"`
			CreditUsage            float64 `json:"credit_usage"`
			ContextUsagePercentage float64 `json:"context_usage_percentage"`
		} `json:"rts_model_state"`
	} `json:"session_state"`
}

func readKiroSnapshot(path, workspaceRoot string) (*KiroSnapshot, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var row kiroSession
	if err := json.Unmarshal(data, &row); err != nil {
		return nil, err
	}
	if !samePath(row.CWD, workspaceRoot) {
		return nil, nil
	}

	var raw map[string]any
	_ = json.Unmarshal(data, &raw)
	return &KiroSnapshot{
		ConversationID:  strings.TrimSpace(row.SessionState.RTSModelState.ConversationID),
		CreditUsage:     row.SessionState.RTSModelState.CreditUsage,
		ContextUsagePct: row.SessionState.RTSModelState.ContextUsagePercentage,
		RawPayload:      raw,
	}, nil
}
