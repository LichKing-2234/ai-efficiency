package attributionlocal

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

func ParseClaudeJSONL(path, workspaceRoot string) ([]LocalToolUsageEvent, error) {
	type candidate struct {
		event    LocalToolUsageEvent
		stopDone bool
		score    int64
	}

	best := map[string]candidate{}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var row map[string]any
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			continue
		}
		if strings.TrimSpace(asString(row["type"])) != "assistant" {
			continue
		}
		if filepath.Clean(asString(row["cwd"])) != filepath.Clean(workspaceRoot) {
			continue
		}

		msg, _ := row["message"].(map[string]any)
		msgID := strings.TrimSpace(asString(msg["id"]))
		sessionID := strings.TrimSpace(asString(row["sessionId"]))
		if msgID == "" || sessionID == "" {
			continue
		}

		usage, _ := msg["usage"].(map[string]any)
		score := asInt64(usage["input_tokens"]) + asInt64(usage["output_tokens"]) +
			asInt64(usage["cache_creation_input_tokens"]) + asInt64(usage["cache_read_input_tokens"])

		cur := candidate{
			event: LocalToolUsageEvent{
				Tool:              "claude",
				ToolSessionID:     sessionID,
				ToolEventID:       msgID,
				DedupeKey:         "claude:" + sessionID + ":" + msgID,
				RequestCount:      1,
				UsageUnit:         UsageUnitToken,
				InputTokens:       asInt64(usage["input_tokens"]),
				OutputTokens:      asInt64(usage["output_tokens"]),
				CachedInputTokens: asInt64(usage["cache_creation_input_tokens"]) + asInt64(usage["cache_read_input_tokens"]),
				ObservedStartAt:   parseObservedAt(row["timestamp"]),
				ObservedEndAt:     parseObservedAt(row["timestamp"]),
				RawSourcePath:     path,
				RawPayload:        row,
			},
			stopDone: strings.TrimSpace(asString(row["stop_reason"])) == "end_turn",
			score:    score,
		}

		old, ok := best[cur.event.DedupeKey]
		if !ok || (cur.stopDone && !old.stopDone) || (cur.stopDone == old.stopDone && cur.score >= old.score) {
			best[cur.event.DedupeKey] = cur
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	out := make([]LocalToolUsageEvent, 0, len(best))
	for _, item := range best {
		out = append(out, normalizeObservedWindow(item.event))
	}
	slices.SortFunc(out, func(a, b LocalToolUsageEvent) int { return strings.Compare(a.DedupeKey, b.DedupeKey) })
	return out, nil
}
