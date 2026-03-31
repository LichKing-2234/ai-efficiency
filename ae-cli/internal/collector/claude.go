package collector

import (
	"bufio"
	"encoding/json"
	"fmt"
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

	sc := bufio.NewScanner(f)
	buf := make([]byte, 0, 1024*1024)
	sc.Buffer(buf, 4*1024*1024)

	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}

		var row claudeLine
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			return nil, fmt.Errorf("decode line: %w", err)
		}
		if row.Type != "assistant" || !samePath(row.CWD, wantCWD) {
			continue
		}

		out.SourceSessionID = strings.TrimSpace(row.SessionID)
		out.InputTokens += row.Message.Usage.InputTokens
		out.OutputTokens += row.Message.Usage.OutputTokens
		out.CachedInputTokens += row.Message.Usage.CacheCreationInputToken + row.Message.Usage.CacheReadInputToken

		var raw map[string]any
		_ = json.Unmarshal([]byte(line), &raw)
		out.RawPayload = raw
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("scan file: %w", err)
	}

	if out.SourceSessionID == "" && out.InputTokens == 0 && out.OutputTokens == 0 && out.CachedInputTokens == 0 {
		return nil, nil
	}
	return out, nil
}
