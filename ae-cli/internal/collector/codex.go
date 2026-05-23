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
	Type    string `json:"type"`
	Payload struct {
		ID   string `json:"id"`
		CWD  string `json:"cwd"`
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
	} `json:"payload"`
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
			if !samePath(row.Payload.CWD, wantCWD) {
				return nil, nil
			}
			sourceSessionID = strings.TrimSpace(row.Payload.ID)
		case "event_msg":
			if strings.TrimSpace(sourceSessionID) == "" {
				continue
			}
			if row.Payload.Type != "token_count" {
				continue
			}
			usage := row.Payload.Info.TotalTokenUsage
			snapshot = &CodexSnapshot{
				SourceSessionID:   sourceSessionID,
				InputTokens:       usage.InputTokens,
				CachedInputTokens: usage.CachedInputTokens,
				OutputTokens:      usage.OutputTokens,
				ReasoningTokens:   usage.ReasoningTokens,
				TotalTokens:       usage.TotalTokens,
				RawPayload: map[string]any{
					"type": "token_count",
					"info": map[string]any{
						"total_token_usage": map[string]any{
							"input_tokens":            usage.InputTokens,
							"cached_input_tokens":     usage.CachedInputTokens,
							"output_tokens":           usage.OutputTokens,
							"reasoning_output_tokens": usage.ReasoningTokens,
							"total_tokens":            usage.TotalTokens,
						},
					},
				},
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
