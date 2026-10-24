package attributionlocal

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"
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

	// A session line carrying a large tool result routinely exceeds bufio.Scanner's
	// default 64 KiB token, and Scanner cannot resume past one: it stops and reports
	// bufio.ErrTooLong, which the workspace scan turns into a skipped file. On the
	// machine this was measured against, that silently dropped 13 of the 25 session
	// files belonging to one repository. bufio.Reader grows to whatever a line
	// needs, so an oversized line costs memory rather than the rest of the session.
	reader := bufio.NewReader(f)

	// consider folds one line into best. It is a closure so that an unusable line
	// can return without skipping the end-of-file check the read loop owns.
	consider := func(line string) {
		var row map[string]any
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			return
		}
		if strings.TrimSpace(asString(row["type"])) != "assistant" {
			return
		}
		if !sameWorkspacePath(asString(row["cwd"]), workspaceRoot) {
			return
		}

		msg, _ := row["message"].(map[string]any)
		msgID := strings.TrimSpace(asString(msg["id"]))
		sessionID := strings.TrimSpace(asString(row["sessionId"]))
		if msgID == "" || sessionID == "" {
			return
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

	for {
		raw, readErr := reader.ReadString('\n')
		if line := strings.TrimSpace(raw); line != "" {
			consider(line)
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				break
			}
			return nil, readErr
		}
	}

	out := make([]LocalToolUsageEvent, 0, len(best))
	for _, item := range best {
		out = append(out, normalizeObservedWindow(item.event))
	}
	slices.SortFunc(out, func(a, b LocalToolUsageEvent) int { return strings.Compare(a.DedupeKey, b.DedupeKey) })
	return out, nil
}
