package clistate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

func RootDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".ae-cli")
}

func StateDir() string {
	return filepath.Join(RootDir(), "state")
}

func HooksStateDir() string {
	return filepath.Join(StateDir(), "hooks")
}

func AttributionStateDir() string {
	return filepath.Join(StateDir(), "attribution")
}

func SaveJSON(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal json: %w", err)
	}
	tmp := fmt.Sprintf("%s.%d.%d.tmp", path, os.Getpid(), time.Now().UnixNano())
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write temp json: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename json: %w", err)
	}
	return nil
}

func LoadJSON(path string, dest any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, dest); err != nil {
		return fmt.Errorf("unmarshal json: %w", err)
	}
	return nil
}
