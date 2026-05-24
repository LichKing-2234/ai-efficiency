package attributionlocal

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strconv"
	"strings"
)

var errStopCodexJSONLScan = errors.New("stop codex jsonl scan")

func ParseCodexJSONLFallback(path, workspaceRoot string) ([]LocalToolUsageEvent, error) {
	return ParseCodexJSONLFallbackContext(context.Background(), path, workspaceRoot)
}

func ParseCodexJSONLFallbackContext(ctx context.Context, path, workspaceRoot string) ([]LocalToolUsageEvent, error) {
	var sessionID string
	var events []LocalToolUsageEvent

	err := forEachCodexJSONLLine(ctx, path, func(idx int, raw []byte) error {
		var row struct {
			Type      string `json:"type"`
			Timestamp string `json:"timestamp"`
			Payload   struct {
				ID         string `json:"id"`
				CWD        string `json:"cwd"`
				Type       string `json:"type"`
				ResponseID string `json:"response_id"`
				Info       *struct {
					LastTokenUsage  map[string]any `json:"last_token_usage"`
					TotalTokenUsage map[string]any `json:"total_token_usage"`
				} `json:"info"`
			} `json:"payload"`
		}
		if err := json.Unmarshal(raw, &row); err != nil {
			return nil
		}

		switch strings.TrimSpace(row.Type) {
		case "session_meta":
			if !sameWorkspacePath(row.Payload.CWD, workspaceRoot) {
				return errStopCodexJSONLScan
			}
			sessionID = strings.TrimSpace(row.Payload.ID)
		case "event_msg":
			if sessionID == "" {
				return nil
			}
			if strings.TrimSpace(row.Payload.Type) != "token_count" || row.Payload.Info == nil {
				return nil
			}
			selected := row.Payload.Info.LastTokenUsage
			if len(selected) == 0 {
				selected = row.Payload.Info.TotalTokenUsage
			}
			if len(selected) == 0 {
				return nil
			}
			responseID := strings.TrimSpace(row.Payload.ResponseID)
			if responseID == "" {
				responseID = fallbackCodexJSONLEventID(idx + 1)
			}
			observedAt := parseObservedAt(row.Timestamp)
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
					"timestamp": row.Timestamp,
					"payload": map[string]any{
						"type":        row.Payload.Type,
						"response_id": row.Payload.ResponseID,
						"info": map[string]any{
							"last_token_usage":  row.Payload.Info.LastTokenUsage,
							"total_token_usage": row.Payload.Info.TotalTokenUsage,
						},
					},
				},
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	if len(events) == 0 {
		return nil, nil
	}
	return events, nil
}

func forEachCodexJSONLLine(ctx context.Context, path string, visit func(idx int, raw []byte) error) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	r := bufio.NewReaderSize(f, 64*1024)
	idx := 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		line, err := r.ReadBytes('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		raw := bytes.TrimSpace(line)
		if len(raw) > 0 {
			if visitErr := visit(idx, raw); visitErr != nil {
				if errors.Is(visitErr, errStopCodexJSONLScan) {
					return nil
				}
				return visitErr
			}
			idx++
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
	}
}

func findCodexWorkspaceSessionIDs(path, workspaceRoot string) []string {
	ids, err := findCodexWorkspaceSessionIDsContext(context.Background(), path, workspaceRoot)
	if err != nil {
		return nil
	}
	return ids
}

func findCodexWorkspaceSessionIDsContext(ctx context.Context, path, workspaceRoot string) ([]string, error) {
	var out []string
	err := forEachCodexJSONLLine(ctx, path, func(_ int, raw []byte) error {
		if !bytes.Contains(raw, []byte("session_meta")) {
			return nil
		}

		var row struct {
			Type    string `json:"type"`
			Payload struct {
				ID  string `json:"id"`
				CWD string `json:"cwd"`
			} `json:"payload"`
		}
		if err := json.Unmarshal(raw, &row); err != nil {
			return nil
		}
		if strings.TrimSpace(row.Type) != "session_meta" {
			return nil
		}
		if !sameWorkspacePath(row.Payload.CWD, workspaceRoot) {
			return errStopCodexJSONLScan
		}
		sessionID := strings.TrimSpace(row.Payload.ID)
		if sessionID != "" {
			out = append(out, sessionID)
		}
		return errStopCodexJSONLScan
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func fallbackCodexJSONLEventID(lineNumber int) string {
	if lineNumber <= 0 {
		return "line:unknown"
	}
	return "line:" + strconv.Itoa(lineNumber)
}
