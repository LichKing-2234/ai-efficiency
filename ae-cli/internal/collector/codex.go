package collector

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type codexLine struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

type codexSessionMeta struct {
	ID  string `json:"id"`
	CWD string `json:"cwd"`
}

type codexTokenPayload struct {
	Type string `json:"type"`
	Info struct {
		TotalTokenUsage struct {
			InputTokens       int64 `json:"input_tokens"`
			CachedInputTokens int64 `json:"cached_input_tokens"`
			OutputTokens      int64 `json:"output_tokens"`
			ReasoningTokens   int64 `json:"reasoning_output_tokens"`
			TotalTokens       int64 `json:"total_tokens"`
		} `json:"total_token_usage"`
	} `json:"info"`
}

func readCodexSnapshot(path, workspaceRoot string) (*CodexSnapshot, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	wantCWD := cleanPath(workspaceRoot)
	var sourceSessionID string
	var snapshot *CodexSnapshot

	sc := bufio.NewScanner(f)
	buf := make([]byte, 0, 1024*1024)
	sc.Buffer(buf, 4*1024*1024)

	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}

		var row codexLine
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			return nil, fmt.Errorf("decode line: %w", err)
		}

		switch row.Type {
		case "session_meta":
			var meta codexSessionMeta
			if err := json.Unmarshal(row.Payload, &meta); err != nil {
				return nil, fmt.Errorf("decode session_meta payload: %w", err)
			}
			if samePath(meta.CWD, wantCWD) {
				sourceSessionID = strings.TrimSpace(meta.ID)
			}
		case "event_msg":
			if strings.TrimSpace(sourceSessionID) == "" {
				continue
			}
			var payload codexTokenPayload
			if err := json.Unmarshal(row.Payload, &payload); err != nil {
				return nil, fmt.Errorf("decode event_msg payload: %w", err)
			}
			if payload.Type != "token_count" {
				continue
			}
			var raw map[string]any
			_ = json.Unmarshal(row.Payload, &raw)
			snapshot = &CodexSnapshot{
				SourceSessionID:   sourceSessionID,
				InputTokens:       payload.Info.TotalTokenUsage.InputTokens,
				CachedInputTokens: payload.Info.TotalTokenUsage.CachedInputTokens,
				OutputTokens:      payload.Info.TotalTokenUsage.OutputTokens,
				ReasoningTokens:   payload.Info.TotalTokenUsage.ReasoningTokens,
				TotalTokens:       payload.Info.TotalTokenUsage.TotalTokens,
				RawPayload:        raw,
			}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("scan file: %w", err)
	}
	return snapshot, nil
}

func samePath(a, b string) bool {
	return cleanPath(a) == cleanPath(b)
}

func cleanPath(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	return filepath.Clean(v)
}
