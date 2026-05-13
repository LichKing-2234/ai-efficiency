package attributionlocal

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

func ParseCodexJSONLFallback(path, workspaceRoot string) ([]LocalToolUsageEvent, error) {
	lines, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var sessionID string
	var event *LocalToolUsageEvent
	for _, raw := range strings.Split(string(lines), "\n") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}

		var row map[string]any
		if err := json.Unmarshal([]byte(raw), &row); err != nil {
			continue
		}

		switch strings.TrimSpace(asString(row["type"])) {
		case "session_meta":
			payload, _ := row["payload"].(map[string]any)
			if filepath.Clean(asString(payload["cwd"])) == filepath.Clean(workspaceRoot) {
				sessionID = strings.TrimSpace(asString(payload["id"]))
			}
		case "event_msg":
			if sessionID == "" {
				continue
			}
			payload, _ := row["payload"].(map[string]any)
			if strings.TrimSpace(asString(payload["type"])) != "token_count" {
				continue
			}
			info, _ := payload["info"].(map[string]any)
			lastUsage, _ := info["last_token_usage"].(map[string]any)
			totalUsage, _ := info["total_token_usage"].(map[string]any)
			selected := lastUsage
			if len(selected) == 0 {
				selected = totalUsage
			}
			if len(selected) == 0 {
				continue
			}
			event = &LocalToolUsageEvent{
				Tool:              "codex",
				ToolSessionID:     sessionID,
				ToolEventID:       strings.TrimSpace(asString(payload["response_id"])),
				DedupeKey:         "codex-jsonl:" + sessionID + ":" + filepath.Base(path),
				UsageUnit:         UsageUnitToken,
				RequestCount:      1,
				InputTokens:       asInt64(selected["input_tokens"]),
				OutputTokens:      asInt64(selected["output_tokens"]),
				CachedInputTokens: asInt64(selected["cached_input_tokens"]),
				ReasoningTokens:   asInt64(selected["reasoning_output_tokens"]),
				RawSourcePath:     path,
				RawPayload:        payload,
			}
		}
	}

	if event == nil {
		return nil, nil
	}
	return []LocalToolUsageEvent{*event}, nil
}
