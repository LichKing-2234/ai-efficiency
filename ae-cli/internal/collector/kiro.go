package collector

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/ai-efficiency/ae-cli/internal/attributionlocal"
	_ "github.com/glebarez/go-sqlite"
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

func readKiroSnapshot(paths []string, workspaceRoot string) (*KiroSnapshot, error) {
	ideSessionIDs := collectKiroIDESessionIDs(paths, workspaceRoot)
	for _, path := range orderFilesByModTime(paths) {
		s, err := readSingleKiroSnapshot(path, workspaceRoot, ideSessionIDs)
		if err != nil {
			continue
		}
		if s == nil {
			continue
		}
		return s, nil
	}
	return nil, nil
}

func readSingleKiroSnapshot(path, workspaceRoot string, ideSessionIDs map[string]struct{}) (*KiroSnapshot, error) {
	if strings.EqualFold(filepath.Base(path), "data.sqlite3") {
		return readKiroCLISnapshot(path, workspaceRoot)
	}
	if strings.EqualFold(filepath.Base(path), "sessions.json") {
		return nil, nil
	}

	if len(ideSessionIDs) > 0 {
		events, err := attributionlocal.ParseKiroIDEExecution(path, ideSessionIDs)
		if err != nil {
			return nil, err
		}
		if len(events) > 0 {
			ev := events[0]
			return &KiroSnapshot{
				ConversationID:  strings.TrimSpace(ev.ToolSessionID),
				CreditUsage:     ev.CreditUsage,
				ContextUsagePct: ev.ContextUsagePct,
				RawPayload:      ev.RawPayload,
			}, nil
		}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var row kiroSession
	if err := json.Unmarshal(data, &row); err != nil {
		return nil, nil
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

type kiroCLICollectorConversation struct {
	ConversationID string `json:"conversation_id"`
	History        []struct {
		RequestMetadata struct {
			ContextUsagePercentage float64 `json:"context_usage_percentage"`
		} `json:"request_metadata"`
	} `json:"history"`
	UserTurnMetadata struct {
		UsageInfo []struct {
			Value float64 `json:"value"`
			Unit  string  `json:"unit"`
		} `json:"usage_info"`
	} `json:"user_turn_metadata"`
}

func readKiroCLISnapshot(path, workspaceRoot string) (*KiroSnapshot, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	var rawValue string
	err = db.QueryRow(`SELECT value FROM conversations_v2 WHERE key = ? ORDER BY updated_at DESC LIMIT 1`, filepath.Clean(workspaceRoot)).Scan(&rawValue)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var row kiroCLICollectorConversation
	if err := json.Unmarshal([]byte(rawValue), &row); err != nil {
		return nil, nil
	}
	if strings.TrimSpace(row.ConversationID) == "" {
		return nil, nil
	}

	var credits float64
	for _, usage := range row.UserTurnMetadata.UsageInfo {
		if strings.EqualFold(strings.TrimSpace(usage.Unit), "credit") {
			credits += usage.Value
		}
	}
	contextUsage := 0.0
	if n := len(row.History); n > 0 {
		contextUsage = row.History[n-1].RequestMetadata.ContextUsagePercentage
	}

	var raw map[string]any
	_ = json.Unmarshal([]byte(rawValue), &raw)
	return &KiroSnapshot{
		ConversationID:  strings.TrimSpace(row.ConversationID),
		CreditUsage:     credits,
		ContextUsagePct: contextUsage,
		RawPayload:      raw,
	}, nil
}

func collectKiroIDESessionIDs(paths []string, workspaceRoot string) map[string]struct{} {
	out := make(map[string]struct{})
	for _, path := range paths {
		if !strings.EqualFold(filepath.Base(path), "sessions.json") {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		var index []struct {
			SessionID          string `json:"sessionId"`
			WorkspaceDirectory string `json:"workspaceDirectory"`
		}
		if err := json.Unmarshal(data, &index); err != nil {
			continue
		}
		for _, item := range index {
			if !samePath(item.WorkspaceDirectory, workspaceRoot) {
				continue
			}
			sessionID := strings.TrimSpace(item.SessionID)
			if sessionID == "" {
				continue
			}
			out[sessionID] = struct{}{}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
