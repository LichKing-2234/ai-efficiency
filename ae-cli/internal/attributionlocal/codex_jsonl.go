package attributionlocal

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func ParseCodexJSONLFallback(path, workspaceRoot string) ([]LocalToolUsageEvent, error) {
	lines, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var sessionID string
	var events []LocalToolUsageEvent
	for idx, raw := range strings.Split(string(lines), "\n") {
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
			responseID := strings.TrimSpace(asString(payload["response_id"]))
			if responseID == "" {
				responseID = fallbackCodexJSONLEventID(idx + 1)
			}
			observedAt := parseObservedAt(row["timestamp"])
			events = append(events, LocalToolUsageEvent{
				Tool:              "codex",
				ToolSessionID:     sessionID,
				ToolEventID:       responseID,
				DedupeKey:         "codex-jsonl:" + sessionID + ":" + responseID,
				UsageUnit:         UsageUnitToken,
				RequestCount:      1,
				InputTokens:       asInt64(selected["input_tokens"]),
				OutputTokens:      asInt64(selected["output_tokens"]),
				CachedInputTokens: asInt64(selected["cached_input_tokens"]),
				ReasoningTokens:   asInt64(selected["reasoning_output_tokens"]),
				ObservedStartAt:   observedAt,
				ObservedEndAt:     observedAt,
				RawSourcePath:     path,
				RawSourceLocator:  "line:" + strconv.Itoa(idx+1),
				RawPayload: map[string]any{
					"timestamp": row["timestamp"],
					"payload":   payload,
				},
			})
		}
	}

	if len(events) == 0 {
		return nil, nil
	}
	return events, nil
}

func findCodexWorkspaceSessionIDs(path, workspaceRoot string) []string {
	lines, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	seen := map[string]struct{}{}
	for _, raw := range strings.Split(string(lines), "\n") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}

		var row map[string]any
		if err := json.Unmarshal([]byte(raw), &row); err != nil {
			continue
		}
		if strings.TrimSpace(asString(row["type"])) != "session_meta" {
			continue
		}

		payload, _ := row["payload"].(map[string]any)
		if filepath.Clean(asString(payload["cwd"])) != filepath.Clean(workspaceRoot) {
			continue
		}

		sessionID := strings.TrimSpace(asString(payload["id"]))
		if sessionID == "" {
			continue
		}
		seen[sessionID] = struct{}{}
	}

	out := make([]string, 0, len(seen))
	for sessionID := range seen {
		out = append(out, sessionID)
	}
	return out
}

func fallbackCodexJSONLEventID(lineNumber int) string {
	if lineNumber <= 0 {
		return "line:unknown"
	}
	return "line:" + strconv.Itoa(lineNumber)
}
