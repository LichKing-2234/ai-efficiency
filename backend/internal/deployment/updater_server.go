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
const defaultImageTag = "latest"
const implicitLatestSentinel = "__implicit_latest__"

type ComposeRunner interface {
	Run(context.Context, ...string) error
}

type UpdaterConfig struct {
	ComposeFile string
	EnvFile     string
	ServiceName string
	StateDir    string
	ProjectName string
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

	currentTag, currentTagExplicit, err := readImageTag(s.cfg.EnvFile)
	if err != nil {
		return UpdateStatus{}, fmt.Errorf("read current image tag: %w", err)
	}

	if err := os.MkdirAll(s.cfg.StateDir, 0o755); err != nil {
		return UpdateStatus{}, fmt.Errorf("create state dir: %w", err)
	}

	rollbackTag := currentTag
	if !currentTagExplicit {
		rollbackTag = implicitLatestSentinel
	}
	if err := os.WriteFile(filepath.Join(s.cfg.StateDir, rollbackImageTagFile), []byte(rollbackTag+"\n"), 0o644); err != nil {
		return UpdateStatus{}, fmt.Errorf("write rollback image tag: %w", err)
	}

	if err := setImageTag(s.cfg.EnvFile, targetVersion); err != nil {
		return UpdateStatus{}, fmt.Errorf("write target image tag: %w", err)
	}

	serviceName := strings.TrimSpace(s.cfg.ServiceName)
	if err := s.runner.Run(ctx, "pull", serviceName); err != nil {
		if restoreErr := restoreImageTag(s.cfg.EnvFile, currentTag, currentTagExplicit); restoreErr != nil {
			return UpdateStatus{}, fmt.Errorf("docker compose pull %s: %w (restore env tag: %v)", serviceName, err, restoreErr)
		}
		return UpdateStatus{}, fmt.Errorf("docker compose pull %s: %w", serviceName, err)
	}
	if err := s.runner.Run(ctx, "up", "-d", serviceName); err != nil {
		if restoreErr := restoreImageTag(s.cfg.EnvFile, currentTag, currentTagExplicit); restoreErr != nil {
			return UpdateStatus{}, fmt.Errorf("docker compose up %s: %w (restore env tag: %v)", serviceName, err, restoreErr)
		}
		return UpdateStatus{}, fmt.Errorf("docker compose up %s: %w", serviceName, err)
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
	currentTag, currentTagExplicit, err := readImageTag(s.cfg.EnvFile)
	if err != nil {
		return UpdateStatus{}, fmt.Errorf("read current image tag: %w", err)
	}

	rollbackTargetVersion := rollbackTag
	if rollbackTargetVersion == implicitLatestSentinel {
		rollbackTargetVersion = defaultImageTag
	}
	if err := setImageTag(s.cfg.EnvFile, rollbackTargetVersion); err != nil {
		return UpdateStatus{}, fmt.Errorf("write rollback image tag: %w", err)
	}

	serviceName := strings.TrimSpace(s.cfg.ServiceName)
	if err := s.runner.Run(ctx, "pull", serviceName); err != nil {
		if restoreErr := restoreImageTag(s.cfg.EnvFile, currentTag, currentTagExplicit); restoreErr != nil {
			return UpdateStatus{}, fmt.Errorf("docker compose pull %s: %w (restore env tag: %v)", serviceName, err, restoreErr)
		}
		return UpdateStatus{}, fmt.Errorf("docker compose pull %s: %w", serviceName, err)
	}
	if err := s.runner.Run(ctx, "up", "-d", serviceName); err != nil {
		if restoreErr := restoreImageTag(s.cfg.EnvFile, currentTag, currentTagExplicit); restoreErr != nil {
			return UpdateStatus{}, fmt.Errorf("docker compose up %s: %w (restore env tag: %v)", serviceName, err, restoreErr)
		}
		return UpdateStatus{}, fmt.Errorf("docker compose up %s: %w", serviceName, err)
	}

	return UpdateStatus{
		Phase:         "rolling_back",
		TargetVersion: rollbackTargetVersion,
	}, nil
}

func readImageTag(path string) (string, bool, error) {
	value, err := readEnvVar(path, "AE_IMAGE_TAG")
	if err == nil {
		return value, true, nil
	}
	if strings.Contains(err.Error(), "key \"AE_IMAGE_TAG\" not found in env file") {
		return defaultImageTag, false, nil
	}
	return "", false, err
}

func setImageTag(path, tag string) error {
	if strings.TrimSpace(tag) == defaultImageTag {
		return deleteEnvVar(path, "AE_IMAGE_TAG")
	}
	return writeEnvVar(path, "AE_IMAGE_TAG", tag)
}

func restoreImageTag(path, tag string, explicit bool) error {
	if !explicit {
		return deleteEnvVar(path, "AE_IMAGE_TAG")
	}
	return writeEnvVar(path, "AE_IMAGE_TAG", tag)
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

func deleteEnvVar(path, key string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read env file: %w", err)
	}

	lines := strings.Split(strings.ReplaceAll(string(content), "\r\n", "\n"), "\n")
	filtered := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			parts := strings.SplitN(trimmed, "=", 2)
			if len(parts) == 2 && strings.TrimSpace(parts[0]) == key {
				continue
			}
		}
		filtered = append(filtered, line)
	}

	output := strings.Join(filtered, "\n")
	if !strings.HasSuffix(output, "\n") {
		output += "\n"
	}
	if err := os.WriteFile(path, []byte(output), 0o644); err != nil {
		return fmt.Errorf("write env file: %w", err)
	}

	return nil
}
