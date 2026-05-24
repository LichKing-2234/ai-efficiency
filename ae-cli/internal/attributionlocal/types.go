package attributionlocal

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type UsageUnit string

const (
	UsageUnitToken  UsageUnit = "token"
	UsageUnitCredit UsageUnit = "credit"
)

type LocalToolUsageEvent struct {
	Tool              string
	WorkspaceID       string
	ServerURL         string
	AuthSubject       string
	RepoConfigID      int
	RepoKey           string
	ManagedUpload     bool
	ToolSessionID     string
	ToolEventID       string
	DedupeKey         string
	RequestCount      int
	UsageUnit         UsageUnit
	InputTokens       int64
	OutputTokens      int64
	CachedInputTokens int64
	ReasoningTokens   int64
	CreditUsage       float64
	ContextUsagePct   float64
	ObservedStartAt   time.Time
	ObservedEndAt     time.Time
	RawSourcePath     string
	RawSourceLocator  string
	RawPayload        map[string]any
}

type CodexSQLiteWatermark struct {
	LastLogID int64 `json:"last_log_id"`
}

type CodexSessionIDCache struct {
	ModUnix    int64    `json:"mod_unix"`
	Size       int64    `json:"size"`
	SessionIDs []string `json:"session_ids,omitempty"`
}

func asString(v any) string {
	switch value := v.(type) {
	case string:
		return value
	case fmt.Stringer:
		return value.String()
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", value)
	}
}

func asInt64(v any) int64 {
	switch value := v.(type) {
	case int:
		return int64(value)
	case int64:
		return value
	case float64:
		return int64(value)
	case json.Number:
		out, _ := value.Int64()
		return out
	case string:
		out, _ := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
		return out
	default:
		return 0
	}
}

func parseObservedAt(raw any) time.Time {
	value := strings.TrimSpace(asString(raw))
	if value == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.UTC()
		}
	}
	return time.Time{}
}

func parseUnixMillis(ms int64) time.Time {
	if ms <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(ms).UTC()
}

func sourceFileObservedAt(path string) time.Time {
	path = strings.TrimSpace(path)
	if path == "" {
		return time.Time{}
	}
	info, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	return info.ModTime().UTC()
}

func normalizeObservedWindow(ev LocalToolUsageEvent) LocalToolUsageEvent {
	if !ev.ObservedStartAt.IsZero() && !ev.ObservedEndAt.IsZero() {
		return ev
	}

	observedAt := time.Time{}
	if len(ev.RawPayload) > 0 {
		observedAt = parseObservedAt(ev.RawPayload["timestamp"])
		if observedAt.IsZero() {
			if payload, _ := ev.RawPayload["payload"].(map[string]any); len(payload) > 0 {
				observedAt = parseObservedAt(payload["timestamp"])
			}
		}
		if observedAt.IsZero() {
			if message, _ := ev.RawPayload["message"].(map[string]any); len(message) > 0 {
				observedAt = parseObservedAt(message["timestamp"])
			}
		}
	}
	if observedAt.IsZero() {
		observedAt = sourceFileObservedAt(ev.RawSourcePath)
	}
	if ev.ObservedStartAt.IsZero() {
		ev.ObservedStartAt = observedAt
	}
	if ev.ObservedEndAt.IsZero() {
		ev.ObservedEndAt = observedAt
	}
	return ev
}
