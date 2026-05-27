package attributionlocal

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	_ "github.com/glebarez/go-sqlite"
)

var (
	reConversationID = regexp.MustCompile(`conversation\.id=([^\s]+)`)
	reResponseID     = regexp.MustCompile(`response\.id=([^\s]+)`)
	reInputTokens    = regexp.MustCompile(`input_token_count=([0-9]+)`)
	reOutputTokens   = regexp.MustCompile(`output_token_count=([0-9]+)`)
	reCachedTokens   = regexp.MustCompile(`cached_token_count=([0-9]+)`)
	reReasoning      = regexp.MustCompile(`reasoning_token_count=([0-9]+)`)
	reTimestamp      = regexp.MustCompile(`event\.timestamp=([^\s]+)`)
	reThreadID       = regexp.MustCompile(`thread_id=([0-9a-fA-F-]+)`)
)

var codexSQLiteInitialLookbackRows int64 = 5000

type CodexSQLiteParser struct{}

func NewCodexSQLiteParser() *CodexSQLiteParser { return &CodexSQLiteParser{} }

func (p *CodexSQLiteParser) Parse(dbPath string, wm CodexSQLiteWatermark) ([]LocalToolUsageEvent, CodexSQLiteWatermark, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, wm, err
	}
	defer db.Close()

	lowerBound := wm.LastLogID
	if lowerBound <= 0 && codexSQLiteInitialLookbackRows > 0 {
		var maxID int64
		if err := db.QueryRow(`SELECT COALESCE(MAX(id), 0) FROM logs`).Scan(&maxID); err == nil && maxID > codexSQLiteInitialLookbackRows {
			lowerBound = maxID - codexSQLiteInitialLookbackRows
		}
	}

	rows, err := db.Query(`
		SELECT id, feedback_log_body
		FROM logs
		WHERE id > ?
		ORDER BY id ASC
	`, lowerBound)
	if err != nil {
		return nil, wm, err
	}
	defer rows.Close()

	seen := map[string]LocalToolUsageEvent{}
	lastID := wm.LastLogID
	for rows.Next() {
		var id int64
		var body string
		if err := rows.Scan(&id, &body); err != nil {
			return nil, wm, err
		}
		lastID = id
		event := parseCodexCompletedLine(dbPath, id, body)
		if event == nil {
			continue
		}
		seen[event.DedupeKey] = *event
	}
	if err := rows.Err(); err != nil {
		return nil, wm, err
	}

	keys := make([]string, 0, len(seen))
	for k := range seen {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	out := make([]LocalToolUsageEvent, 0, len(keys))
	for _, key := range keys {
		out = append(out, seen[key])
	}
	return out, CodexSQLiteWatermark{LastLogID: lastID}, nil
}

func parseCodexCompletedLine(path string, id int64, body string) *LocalToolUsageEvent {
	if !strings.Contains(body, "response.completed") {
		return nil
	}
	if event := parseCodexCompletedJSONLine(path, id, body); event != nil {
		return event
	}
	convID := regexValue(reConversationID, body)
	respID := regexValue(reResponseID, body)
	if convID == "" || respID == "" {
		return nil
	}

	ts := time.Time{}
	if rawTS := regexValue(reTimestamp, body); rawTS != "" {
		if parsed, err := time.Parse(time.RFC3339, rawTS); err == nil {
			ts = parsed.UTC()
		}
	}

	return &LocalToolUsageEvent{
		Tool:              "codex",
		ToolSessionID:     convID,
		ToolEventID:       respID,
		DedupeKey:         "codex:" + convID + ":" + respID,
		RequestCount:      1,
		UsageUnit:         UsageUnitToken,
		InputTokens:       parseInt64(regexValue(reInputTokens, body)),
		OutputTokens:      parseInt64(regexValue(reOutputTokens, body)),
		CachedInputTokens: parseInt64(regexValue(reCachedTokens, body)),
		ReasoningTokens:   parseInt64(regexValue(reReasoning, body)),
		ObservedStartAt:   ts,
		ObservedEndAt:     ts,
		RawSourcePath:     path,
		RawSourceLocator:  fmt.Sprintf("log_id:%d", id),
		RawPayload: map[string]any{
			"feedback_log_body": body,
		},
	}
}

func parseCodexCompletedJSONLine(path string, id int64, body string) *LocalToolUsageEvent {
	const marker = "websocket event:"
	markerIdx := strings.Index(body, marker)
	if markerIdx < 0 {
		return nil
	}
	jsonStart := strings.Index(body[markerIdx+len(marker):], "{")
	if jsonStart < 0 {
		return nil
	}
	raw := body[markerIdx+len(marker)+jsonStart:]
	var row struct {
		Type     string `json:"type"`
		Response struct {
			ID          string `json:"id"`
			CompletedAt int64  `json:"completed_at"`
			Usage       struct {
				InputTokens        int64 `json:"input_tokens"`
				OutputTokens       int64 `json:"output_tokens"`
				InputTokensDetails struct {
					CachedTokens int64 `json:"cached_tokens"`
				} `json:"input_tokens_details"`
				OutputTokensDetails struct {
					ReasoningTokens int64 `json:"reasoning_tokens"`
				} `json:"output_tokens_details"`
			} `json:"usage"`
		} `json:"response"`
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	if err := decoder.Decode(&row); err != nil {
		return nil
	}
	if strings.TrimSpace(row.Type) != "response.completed" || strings.TrimSpace(row.Response.ID) == "" {
		return nil
	}

	convID := regexValue(reConversationID, body)
	if convID == "" {
		convID = regexValue(reThreadID, body)
	}
	if convID == "" {
		return nil
	}

	ts := time.Time{}
	if rawTS := regexValue(reTimestamp, body); rawTS != "" {
		if parsed, err := time.Parse(time.RFC3339, rawTS); err == nil {
			ts = parsed.UTC()
		}
	}
	if ts.IsZero() && row.Response.CompletedAt > 0 {
		ts = time.Unix(row.Response.CompletedAt, 0).UTC()
	}

	respID := strings.TrimSpace(row.Response.ID)
	return &LocalToolUsageEvent{
		Tool:              "codex",
		ToolSessionID:     convID,
		ToolEventID:       respID,
		DedupeKey:         "codex:" + convID + ":" + respID,
		RequestCount:      1,
		UsageUnit:         UsageUnitToken,
		InputTokens:       row.Response.Usage.InputTokens,
		OutputTokens:      row.Response.Usage.OutputTokens,
		CachedInputTokens: row.Response.Usage.InputTokensDetails.CachedTokens,
		ReasoningTokens:   row.Response.Usage.OutputTokensDetails.ReasoningTokens,
		ObservedStartAt:   ts,
		ObservedEndAt:     ts,
		RawSourcePath:     path,
		RawSourceLocator:  fmt.Sprintf("log_id:%d", id),
		RawPayload: map[string]any{
			"feedback_log_body": body,
		},
	}
}

func regexValue(re *regexp.Regexp, body string) string {
	matches := re.FindStringSubmatch(body)
	if len(matches) != 2 {
		return ""
	}
	return strings.TrimSpace(matches[1])
}

func parseInt64(raw string) int64 {
	var value int64
	fmt.Sscanf(strings.TrimSpace(raw), "%d", &value)
	return value
}
