package collector

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

type claudeLine struct {
	Type      string `json:"type"`
	CWD       string `json:"cwd"`
	SessionID string `json:"sessionId"`
	Message   struct {
		Usage struct {
			InputTokens             int64 `json:"input_tokens"`
			OutputTokens            int64 `json:"output_tokens"`
			CacheCreationInputToken int64 `json:"cache_creation_input_tokens"`
			CacheReadInputToken     int64 `json:"cache_read_input_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

func readClaudeSnapshot(path, workspaceRoot string) (*ClaudeSnapshot, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	wantCWD := cleanPath(workspaceRoot)
	out := &ClaudeSnapshot{}

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

		var row claudeLine
		if err := json.Unmarshal(line, &row); err != nil {
			continue
		}
		if row.Type != "assistant" || !samePath(row.CWD, wantCWD) {
			continue
		}

		out.SourceSessionID = strings.TrimSpace(row.SessionID)
		out.InputTokens += row.Message.Usage.InputTokens
		out.OutputTokens += row.Message.Usage.OutputTokens
		out.CachedInputTokens += row.Message.Usage.CacheCreationInputToken + row.Message.Usage.CacheReadInputToken

		var raw map[string]any
		_ = json.Unmarshal(line, &raw)
		out.RawPayload = raw
		if errors.Is(err, io.EOF) {
			break
		}
	}

	if out.SourceSessionID == "" && out.InputTokens == 0 && out.OutputTokens == 0 && out.CachedInputTokens == 0 {
		return nil, nil
	}
	return out, nil
}
