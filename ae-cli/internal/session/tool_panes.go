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

func toolPaneRegistryPath(sessionID string) string {
	return filepath.Join(runtimeDir(sessionID), "tool-panes.json")
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
	if err := os.MkdirAll(runtimeDir(sessionID), 0o700); err != nil {
		return fmt.Errorf("creating runtime dir: %w", err)
	}
	data, err := json.MarshalIndent(reg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling tool pane registry: %w", err)
	}
	if err := os.WriteFile(toolPaneRegistryPath(sessionID), data, 0o600); err != nil {
		return fmt.Errorf("writing tool pane registry: %w", err)
	}
	return nil
}

func FormatToolPaneLabel(rec ToolPaneRecord) string {
	return fmt.Sprintf("%s#%d", rec.ToolName, rec.InstanceNo)
}

func RegisterToolPane(sessionID, toolName, paneID, source string) (*ToolPaneRecord, error) {
	reg, err := ReadToolPaneRegistry(sessionID)
	if err != nil {
		return nil, err
	}
	toolName = strings.TrimSpace(toolName)
	next := reg.NextInstanceByTool[toolName] + 1
	rec := ToolPaneRecord{
		ToolName:     toolName,
		InstanceNo:   next,
		PaneID:       strings.TrimSpace(paneID),
		LaunchSource: strings.TrimSpace(source),
		CreatedAt:    time.Now().UTC(),
	}
	reg.NextInstanceByTool[toolName] = next
	reg.Instances = append(reg.Instances, rec)
	if err := WriteToolPaneRegistry(sessionID, reg); err != nil {
		return nil, err
	}
	return &rec, nil
}

func FindToolPane(sessionID, toolName string, instanceNo int) (*ToolPaneRecord, error) {
	reg, err := ReadToolPaneRegistry(sessionID)
	if err != nil {
		return nil, err
	}
	for _, rec := range reg.Instances {
		if rec.ToolName == toolName && rec.InstanceNo == instanceNo {
			copy := rec
			return &copy, nil
		}
	}
	return nil, fmt.Errorf("tool instance %s#%d not found", toolName, instanceNo)
}

func ListToolPanes(sessionID string) ([]ToolPaneRecord, error) {
	reg, err := ReadToolPaneRegistry(sessionID)
	if err != nil {
		return nil, err
	}
	return append([]ToolPaneRecord(nil), reg.Instances...), nil
}

func RemoveToolPaneByPaneID(sessionID, paneID string) error {
	reg, err := ReadToolPaneRegistry(sessionID)
	if err != nil {
		return err
	}
	keep := reg.Instances[:0]
	for _, rec := range reg.Instances {
		if rec.PaneID == strings.TrimSpace(paneID) {
			continue
		}
		keep = append(keep, rec)
	}
	reg.Instances = append([]ToolPaneRecord(nil), keep...)
	return WriteToolPaneRegistry(sessionID, reg)
}

func PruneToolPanes(sessionID string, alive func(string) bool) ([]ToolPaneRecord, error) {
	reg, err := ReadToolPaneRegistry(sessionID)
	if err != nil {
		return nil, err
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
		return nil, err
	}
	return append([]ToolPaneRecord(nil), reg.Instances...), nil
}
