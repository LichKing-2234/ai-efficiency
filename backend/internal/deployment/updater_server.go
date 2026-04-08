package deployment

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const rollbackImageTagFile = "rollback-image-tag"

type ComposeRunner interface {
	Run(context.Context, ...string) error
}

type UpdaterConfig struct {
	ComposeFile string
	EnvFile     string
	ServiceName string
	StateDir    string
}

type UpdaterServer struct {
	cfg    UpdaterConfig
	runner ComposeRunner
}

func NewUpdaterServer(cfg UpdaterConfig, runner ComposeRunner) *UpdaterServer {
	return &UpdaterServer{
		cfg:    cfg,
		runner: runner,
	}
}

func (s *UpdaterServer) Apply(ctx context.Context, req ApplyRequest) (UpdateStatus, error) {
	targetVersion := strings.TrimSpace(req.TargetVersion)
	if targetVersion == "" {
		return UpdateStatus{}, fmt.Errorf("target_version is required")
	}

	currentTag, err := readEnvVar(s.cfg.EnvFile, "AE_IMAGE_TAG")
	if err != nil {
		return UpdateStatus{}, fmt.Errorf("read current image tag: %w", err)
	}

	if err := os.MkdirAll(s.cfg.StateDir, 0o755); err != nil {
		return UpdateStatus{}, fmt.Errorf("create state dir: %w", err)
	}

	if err := os.WriteFile(filepath.Join(s.cfg.StateDir, rollbackImageTagFile), []byte(currentTag+"\n"), 0o644); err != nil {
		return UpdateStatus{}, fmt.Errorf("write rollback image tag: %w", err)
	}

	if err := writeEnvVar(s.cfg.EnvFile, "AE_IMAGE_TAG", targetVersion); err != nil {
		return UpdateStatus{}, fmt.Errorf("write target image tag: %w", err)
	}

	if err := s.runner.Run(ctx, "pull", s.cfg.ServiceName); err != nil {
		return UpdateStatus{}, fmt.Errorf("docker compose pull %s: %w", s.cfg.ServiceName, err)
	}
	if err := s.runner.Run(ctx, "up", "-d", s.cfg.ServiceName); err != nil {
		return UpdateStatus{}, fmt.Errorf("docker compose up %s: %w", s.cfg.ServiceName, err)
	}

	return UpdateStatus{
		Phase:         "updating",
		TargetVersion: targetVersion,
	}, nil
}

func (s *UpdaterServer) Rollback(ctx context.Context) (UpdateStatus, error) {
	rawTag, err := os.ReadFile(filepath.Join(s.cfg.StateDir, rollbackImageTagFile))
	if err != nil {
		return UpdateStatus{}, fmt.Errorf("read rollback image tag: %w", err)
	}
	rollbackTag := strings.TrimSpace(string(rawTag))
	if rollbackTag == "" {
		return UpdateStatus{}, fmt.Errorf("rollback image tag is empty")
	}

	if err := writeEnvVar(s.cfg.EnvFile, "AE_IMAGE_TAG", rollbackTag); err != nil {
		return UpdateStatus{}, fmt.Errorf("write rollback image tag: %w", err)
	}

	if err := s.runner.Run(ctx, "up", "-d", s.cfg.ServiceName); err != nil {
		return UpdateStatus{}, fmt.Errorf("docker compose up %s: %w", s.cfg.ServiceName, err)
	}

	return UpdateStatus{
		Phase:         "rolling_back",
		TargetVersion: rollbackTag,
	}, nil
}

func readEnvVar(path, key string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open env file: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		if strings.TrimSpace(parts[0]) == key {
			return strings.TrimSpace(parts[1]), nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("scan env file: %w", err)
	}

	return "", fmt.Errorf("key %q not found in env file", key)
}

func writeEnvVar(path, key, value string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read env file: %w", err)
	}

	lines := strings.Split(strings.ReplaceAll(string(content), "\r\n", "\n"), "\n")
	found := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		parts := strings.SplitN(trimmed, "=", 2)
		if len(parts) != 2 {
			continue
		}
		if strings.TrimSpace(parts[0]) == key {
			lines[i] = key + "=" + value
			found = true
		}
	}
	if !found {
		if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
			lines[len(lines)-1] = key + "=" + value
		} else {
			lines = append(lines, key+"="+value)
		}
	}

	output := strings.Join(lines, "\n")
	if !strings.HasSuffix(output, "\n") {
		output += "\n"
	}
	if err := os.WriteFile(path, []byte(output), 0o644); err != nil {
		return fmt.Errorf("write env file: %w", err)
	}

	return nil
}
