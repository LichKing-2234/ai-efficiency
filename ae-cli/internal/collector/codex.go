package collector

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

	r := bufio.NewReaderSize(f, 64*1024)
	for {
		line, err := r.ReadBytes('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("read line: %w", err)
		}
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			if errors.Is(err, io.EOF) {
				break
			}
			continue
		}

		var row codexLine
		if err := json.Unmarshal(line, &row); err != nil {
			continue
		}

		switch row.Type {
		case "session_meta":
			var meta codexSessionMeta
			if err := json.Unmarshal(row.Payload, &meta); err != nil {
				continue
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
				continue
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
		if errors.Is(err, io.EOF) {
			break
		}
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
