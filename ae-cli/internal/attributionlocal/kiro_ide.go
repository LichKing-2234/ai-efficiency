package attributionlocal

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type kiroIDEWorkspaceSessionRecord struct {
	SessionID          string `json:"sessionId"`
	WorkspaceDirectory string `json:"workspaceDirectory"`
}

type kiroIDEExecutionDoc struct {
	ExecutionID            string  `json:"executionId"`
	Status                 string  `json:"status"`
	StartTime              int64   `json:"startTime"`
	EndTime                int64   `json:"endTime"`
	ChatSessionID          string  `json:"chatSessionId"`
	ContextUsagePercentage float64 `json:"contextUsagePercentage"`
	UsageSummary           []struct {
		Usage      float64 `json:"usage"`
		Unit       string  `json:"unit"`
		UnitPlural string  `json:"unitPlural"`
	} `json:"usageSummary"`
}

func FindKiroIDESessionIDs(homeDir, workspaceRoot string) map[string]struct{} {
	workspaceRoot = strings.TrimSpace(workspaceRoot)
	if workspaceRoot == "" {
		return nil
	}

	seen := make(map[string]struct{})
	for _, path := range findKiroIDEWorkspaceSessionIndexFiles(homeDir, workspaceRoot) {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		var index []kiroIDEWorkspaceSessionRecord
		if err := json.Unmarshal(data, &index); err != nil {
			continue
		}
		for _, item := range index {
			if !sameWorkspacePath(item.WorkspaceDirectory, workspaceRoot) {
				continue
			}
			sessionID := strings.TrimSpace(item.SessionID)
			if sessionID == "" {
				continue
			}
			seen[sessionID] = struct{}{}
		}
	}

	if len(seen) == 0 {
		return nil
	}
	return seen
}

func FindKiroIDEExecutionFiles(homeDir string) []string {
	root := filepath.Join(strings.TrimSpace(homeDir), "Library", "Application Support", "Kiro", "User", "globalStorage", "kiro.kiroagent")
	if strings.TrimSpace(homeDir) == "" {
		return nil
	}
	if _, err := os.Stat(root); err != nil {
		return nil
	}

	type candidate struct {
		path    string
		modTime time.Time
	}
	var matches []candidate
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d == nil || d.IsDir() {
			return nil
		}
		cleaned := filepath.Clean(path)
		if strings.Contains(cleaned, string(filepath.Separator)+"workspace-sessions"+string(filepath.Separator)) {
			return nil
		}
		if strings.Contains(cleaned, string(filepath.Separator)+"dev_data"+string(filepath.Separator)) {
			return nil
		}
		if filepath.Ext(cleaned) != "" {
			return nil
		}

		info, statErr := d.Info()
		if statErr != nil {
			return nil
		}
		matches = append(matches, candidate{path: cleaned, modTime: info.ModTime()})
		return nil
	})

	sort.Slice(matches, func(i, j int) bool {
		if matches[i].modTime.Equal(matches[j].modTime) {
			return matches[i].path > matches[j].path
		}
		return matches[i].modTime.After(matches[j].modTime)
	})

	out := make([]string, 0, len(matches))
	for _, item := range matches {
		out = append(out, item.path)
	}
	return out
}

func ParseKiroIDEExecution(path string, sessionIDs map[string]struct{}) ([]LocalToolUsageEvent, error) {
	if len(sessionIDs) == 0 {
		return nil, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var doc kiroIDEExecutionDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, nil
	}

	sessionID := strings.TrimSpace(doc.ChatSessionID)
	if sessionID == "" {
		return nil, nil
	}
	if _, ok := sessionIDs[sessionID]; !ok {
		return nil, nil
	}
	if strings.TrimSpace(doc.ExecutionID) == "" {
		return nil, nil
	}
	if !strings.EqualFold(strings.TrimSpace(doc.Status), "succeed") {
		return nil, nil
	}

	var credits float64
	for _, item := range doc.UsageSummary {
		if strings.EqualFold(strings.TrimSpace(item.Unit), "credit") {
			credits += item.Usage
		}
	}
	if credits <= 0 {
		return nil, nil
	}

	var raw map[string]any
	_ = json.Unmarshal(data, &raw)
	return []LocalToolUsageEvent{{
		Tool:             "kiro",
		ToolSessionID:    sessionID,
		ToolEventID:      strings.TrimSpace(doc.ExecutionID),
		DedupeKey:        fmt.Sprintf("kiro-ide:%s:%s", sessionID, doc.ExecutionID),
		RequestCount:     1,
		UsageUnit:        UsageUnitCredit,
		CreditUsage:      credits,
		ContextUsagePct:  doc.ContextUsagePercentage,
		ObservedStartAt:  parseUnixMillis(doc.StartTime),
		ObservedEndAt:    parseUnixMillis(doc.EndTime),
		RawSourcePath:    path,
		RawSourceLocator: "execution:" + strings.TrimSpace(doc.ExecutionID),
		RawPayload:       raw,
	}}, nil
}

func findKiroIDEWorkspaceSessionIndexFiles(homeDir, workspaceRoot string) []string {
	base := filepath.Join(strings.TrimSpace(homeDir), "Library", "Application Support", "Kiro", "User", "globalStorage", "kiro.kiroagent", "workspace-sessions")
	if strings.TrimSpace(homeDir) == "" {
		return nil
	}

	out := make([]string, 0, 3)
	seen := make(map[string]struct{})
	for _, dir := range kiroIDEWorkspaceDirCandidates(workspaceRoot) {
		path := filepath.Join(base, dir, "sessions.json")
		if _, err := os.Stat(path); err != nil {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		out = append(out, path)
	}
	return out
}

func FindKiroIDEWorkspaceSessionIndexFilesForCollector(homeDir, workspaceRoot string) []string {
	return findKiroIDEWorkspaceSessionIndexFiles(homeDir, workspaceRoot)
}

func kiroIDEWorkspaceDirCandidates(workspaceRoot string) []string {
	workspaceRoot = filepath.Clean(strings.TrimSpace(workspaceRoot))
	if workspaceRoot == "" {
		return nil
	}

	add := func(dst *[]string, seen map[string]struct{}, value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		*dst = append(*dst, value)
	}

	seen := make(map[string]struct{})
	out := make([]string, 0, 6)
	std := base64.StdEncoding.EncodeToString([]byte(workspaceRoot))
	url := base64.URLEncoding.EncodeToString([]byte(workspaceRoot))
	for _, value := range []string{
		std,
		strings.TrimRight(std, "="),
		strings.ReplaceAll(std, "=", "_"),
		url,
		strings.TrimRight(url, "="),
		strings.ReplaceAll(url, "=", "_"),
	} {
		add(&out, seen, value)
	}
	return out
}
