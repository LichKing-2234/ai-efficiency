package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type ToolPaneRecord struct {
	ToolName     string    `json:"tool_name"`
	InstanceNo   int       `json:"instance_no"`
	PaneID       string    `json:"pane_id"`
	LaunchSource string    `json:"launch_source,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

type ToolPaneRegistry struct {
	NextInstanceByTool map[string]int  `json:"next_instance_by_tool"`
	Instances          []ToolPaneRecord `json:"instances"`
}

var (
	toolPaneLockName        = "tool-panes.lock"
	toolPaneLockRetryDelay  = 20 * time.Millisecond
	toolPaneLockMaxRetries  = 50
	toolPaneLockStaleWindow = 5 * time.Second
)

const (
	toolPaneLockHeartbeatFile = "heartbeat"
)

func toolPaneRegistryPath(sessionID string) string {
	return filepath.Join(runtimeDir(sessionID), "tool-panes.json")
}

func toolPaneLockPath(sessionID string) string {
	return filepath.Join(runtimeDir(sessionID), toolPaneLockName)
}

func toolPaneHeartbeatPath(sessionID string) string {
	return filepath.Join(toolPaneLockPath(sessionID), toolPaneLockHeartbeatFile)
}

func ReadToolPaneRegistry(sessionID string) (*ToolPaneRegistry, error) {
	if strings.TrimSpace(sessionID) == "" {
		return nil, fmt.Errorf("session_id is required")
	}
	data, err := os.ReadFile(toolPaneRegistryPath(sessionID))
	if err != nil {
		if os.IsNotExist(err) {
			return &ToolPaneRegistry{
				NextInstanceByTool: map[string]int{},
				Instances:          []ToolPaneRecord{},
			}, nil
		}
		return nil, fmt.Errorf("reading tool pane registry: %w", err)
	}
	var reg ToolPaneRegistry
	if err := json.Unmarshal(data, &reg); err != nil {
		return nil, fmt.Errorf("parsing tool pane registry: %w", err)
	}
	if reg.NextInstanceByTool == nil {
		reg.NextInstanceByTool = map[string]int{}
	}
	if reg.Instances == nil {
		reg.Instances = []ToolPaneRecord{}
	}
	return &reg, nil
}

func WriteToolPaneRegistry(sessionID string, reg *ToolPaneRegistry) error {
	if reg == nil {
		return fmt.Errorf("tool pane registry is nil")
	}
	if strings.TrimSpace(sessionID) == "" {
		return fmt.Errorf("session_id is required")
	}
	dir := runtimeDir(sessionID)
	if dir == "" {
		return fmt.Errorf("runtime dir is empty")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating runtime dir: %w", err)
	}
	data, err := json.MarshalIndent(reg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling tool pane registry: %w", err)
	}
	tmpPath := toolPaneRegistryPath(sessionID) + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o600); err != nil {
		return fmt.Errorf("writing tool pane registry temp file: %w", err)
	}
	if err := os.Rename(tmpPath, toolPaneRegistryPath(sessionID)); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("renaming tool pane registry temp file: %w", err)
	}
	return nil
}

func FormatToolPaneLabel(rec ToolPaneRecord) string {
	return fmt.Sprintf("%s#%d", rec.ToolName, rec.InstanceNo)
}

func RegisterToolPane(sessionID, toolName, paneID, source string) (*ToolPaneRecord, error) {
	normalizedTool, err := normalizeToolName(toolName)
	if err != nil {
		return nil, err
	}
	normalizedPane, err := normalizePaneID(paneID)
	if err != nil {
		return nil, err
	}
	normalizedSource := strings.TrimSpace(source)
	var registered *ToolPaneRecord
	if err := withToolPaneLock(sessionID, func() error {
		reg, err := ReadToolPaneRegistry(sessionID)
		if err != nil {
			return err
		}
		next := reg.NextInstanceByTool[normalizedTool] + 1
		rec := ToolPaneRecord{
			ToolName:     normalizedTool,
			InstanceNo:   next,
			PaneID:       normalizedPane,
			LaunchSource: normalizedSource,
			CreatedAt:    time.Now().UTC(),
		}
		reg.NextInstanceByTool[normalizedTool] = next
		reg.Instances = append(reg.Instances, rec)
		if err := WriteToolPaneRegistry(sessionID, reg); err != nil {
			return err
		}
		registered = &rec
		return nil
	}); err != nil {
		return nil, err
	}
	return registered, nil
}

func FindToolPane(sessionID, toolName string, instanceNo int) (*ToolPaneRecord, error) {
	normalizedTool, err := normalizeToolName(toolName)
	if err != nil {
		return nil, err
	}
	reg, err := ReadToolPaneRegistry(sessionID)
	if err != nil {
		return nil, err
	}
	for _, rec := range reg.Instances {
		if rec.ToolName == normalizedTool && rec.InstanceNo == instanceNo {
			copy := rec
			return &copy, nil
		}
	}
	return nil, fmt.Errorf("tool instance %s#%d not found", normalizedTool, instanceNo)
}

func ListToolPanes(sessionID string) ([]ToolPaneRecord, error) {
	reg, err := ReadToolPaneRegistry(sessionID)
	if err != nil {
		return nil, err
	}
	return append([]ToolPaneRecord(nil), reg.Instances...), nil
}

func RemoveToolPaneByPaneID(sessionID, paneID string) error {
	normalizedPane, err := normalizePaneID(paneID)
	if err != nil {
		return err
	}
	return withToolPaneLock(sessionID, func() error {
		reg, err := ReadToolPaneRegistry(sessionID)
		if err != nil {
			return err
		}
		keep := reg.Instances[:0]
		for _, rec := range reg.Instances {
			if rec.PaneID == normalizedPane {
				continue
			}
			keep = append(keep, rec)
		}
		reg.Instances = append([]ToolPaneRecord(nil), keep...)
		return WriteToolPaneRegistry(sessionID, reg)
	})
}

func PruneToolPanes(sessionID string, alive func(string) bool) ([]ToolPaneRecord, error) {
	var result []ToolPaneRecord
	if err := withToolPaneLock(sessionID, func() error {
		reg, err := ReadToolPaneRegistry(sessionID)
		if err != nil {
			return err
		}
		keep := reg.Instances[:0]
		for _, rec := range reg.Instances {
			if alive != nil && !alive(rec.PaneID) {
				continue
			}
			keep = append(keep, rec)
		}
		reg.Instances = append([]ToolPaneRecord(nil), keep...)
		if err := WriteToolPaneRegistry(sessionID, reg); err != nil {
			return err
		}
		result = append([]ToolPaneRecord(nil), reg.Instances...)
		return nil
	}); err != nil {
		return nil, err
	}
	return result, nil
}

func normalizeToolName(toolName string) (string, error) {
	trimmed := strings.TrimSpace(toolName)
	if trimmed == "" {
		return "", fmt.Errorf("tool_name is required")
	}
	return trimmed, nil
}

func normalizePaneID(paneID string) (string, error) {
	trimmed := strings.TrimSpace(paneID)
	if trimmed == "" {
		return "", fmt.Errorf("pane_id is required")
	}
	return trimmed, nil
}

func startLockHeartbeat(lockPath string, interval time.Duration) func() {
	ticker := time.NewTicker(interval)
	stop := make(chan struct{})
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				now := time.Now()
				os.Chtimes(lockPath, now, now)
			case <-stop:
				return
			}
		}
	}()
	return func() { close(stop) }
}

func withToolPaneLock(sessionID string, fn func() error) (retErr error) {
	release, err := acquireToolPaneLock(sessionID)
	if err != nil {
		return err
	}
	defer func() {
		if relErr := release(); relErr != nil && retErr == nil {
			retErr = relErr
		}
	}()
	return fn()
}

func acquireToolPaneLock(sessionID string) (func() error, error) {
	if strings.TrimSpace(sessionID) == "" {
		return nil, fmt.Errorf("session_id is required")
	}
	dir := runtimeDir(sessionID)
	if dir == "" {
		return nil, fmt.Errorf("runtime dir is empty")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("creating runtime dir: %w", err)
	}
	lockPath := toolPaneLockPath(sessionID)
	for attempt := 0; attempt < toolPaneLockMaxRetries; attempt++ {
		if err := os.Mkdir(lockPath, 0o700); err == nil {
			heartbeatPath := filepath.Join(lockPath, toolPaneLockHeartbeatFile)
			if err := os.WriteFile(heartbeatPath, []byte(time.Now().UTC().Format(time.RFC3339Nano)), 0o600); err != nil {
				os.RemoveAll(lockPath)
				return nil, fmt.Errorf("creating lock heartbeat: %w", err)
			}
			stop := startLockHeartbeat(heartbeatPath, toolPaneLockRetryDelay)
			return func() error {
				stop()
				if err := os.RemoveAll(lockPath); err != nil && !os.IsNotExist(err) {
					return fmt.Errorf("releasing tool pane lock: %w", err)
				}
				return nil
			}, nil
		} else if !os.IsExist(err) {
			return nil, fmt.Errorf("acquiring tool pane lock: %w", err)
		}
		if _, statErr := os.Stat(lockPath); statErr == nil {
			heartbeatPath := filepath.Join(lockPath, toolPaneLockHeartbeatFile)
			if hbInfo, hbErr := os.Stat(heartbeatPath); hbErr == nil {
				if time.Since(hbInfo.ModTime()) > toolPaneLockStaleWindow {
					os.RemoveAll(lockPath)
					continue
				}
			} else {
				os.RemoveAll(lockPath)
				continue
			}
		}
		time.Sleep(toolPaneLockRetryDelay)
	}
	return nil, fmt.Errorf("acquiring tool pane lock: timeout")
}
