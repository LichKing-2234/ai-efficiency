package attributionlocal

import (
	"encoding/json"
	"fmt"
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
