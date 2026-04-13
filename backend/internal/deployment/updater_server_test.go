package deployment

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

type composeRunnerStub struct {
	calls      [][]string
	failAtCall int
	err        error
	callCount  int
}

func (s *composeRunnerStub) Run(_ context.Context, args ...string) error {
	call := append([]string(nil), args...)
	s.calls = append(s.calls, call)
	s.callCount++
	if s.failAtCall > 0 && s.callCount == s.failAtCall {
		return s.err
	}
	return nil
}

func TestUpdaterServerApplyAndRollbackRewriteEnvFile(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	envFile := filepath.Join(tmpDir, ".env")
	if err := os.WriteFile(envFile, []byte("AE_IMAGE_TAG=v0.4.0\n"), 0o644); err != nil {
		t.Fatalf("write initial env file: %v", err)
	}

	runner := &composeRunnerStub{}
	server := NewUpdaterServer(UpdaterConfig{
		ComposeFile: "deploy/docker-compose.yml",
		EnvFile:     envFile,
		ServiceName: "backend",
		StateDir:    tmpDir,
	}, runner)

	status, err := server.Apply(context.Background(), ApplyRequest{TargetVersion: "v0.5.0"})
	if err != nil {
		t.Fatalf("apply update: %v", err)
	}
	if status.Phase != "updating" {
		t.Fatalf("expected updating phase, got %q", status.Phase)
	}
	if status.TargetVersion != "v0.5.0" {
		t.Fatalf("expected target version v0.5.0, got %q", status.TargetVersion)
	}
	content, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatalf("read env file after apply: %v", err)
	}
	if string(content) != "AE_IMAGE_TAG=v0.5.0\n" {
		t.Fatalf("expected env tag updated, got %q", string(content))
	}
	if !reflect.DeepEqual(runner.calls, [][]string{
		{"pull", "backend"},
		{"up", "-d", "backend"},
	}) {
		t.Fatalf("unexpected compose calls: %#v", runner.calls)
	}

	status, err = server.Rollback(context.Background())
	if err != nil {
		t.Fatalf("rollback update: %v", err)
	}
	if status.Phase != "rolling_back" {
		t.Fatalf("expected rolling_back phase, got %q", status.Phase)
	}
	if status.TargetVersion != "v0.4.0" {
		t.Fatalf("expected rollback target version v0.4.0, got %q", status.TargetVersion)
	}
	content, err = os.ReadFile(envFile)
	if err != nil {
		t.Fatalf("read env file after rollback: %v", err)
	}
	if string(content) != "AE_IMAGE_TAG=v0.4.0\n" {
		t.Fatalf("expected env tag rolled back, got %q", string(content))
	}
	if !reflect.DeepEqual(runner.calls, [][]string{
		{"pull", "backend"},
		{"up", "-d", "backend"},
		{"pull", "backend"},
		{"up", "-d", "backend"},
	}) {
		t.Fatalf("unexpected compose calls after rollback: %#v", runner.calls)
	}
}

func TestUpdaterServerApplyRestoresEnvFileWhenComposeUpFails(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	envFile := filepath.Join(tmpDir, ".env")
	if err := os.WriteFile(envFile, []byte("AE_IMAGE_TAG=v0.4.0\n"), 0o644); err != nil {
		t.Fatalf("write initial env file: %v", err)
	}

	runner := &composeRunnerStub{
		failAtCall: 2,
		err:        errors.New("compose up failed"),
	}
	server := NewUpdaterServer(UpdaterConfig{
		ComposeFile: "deploy/docker-compose.yml",
		EnvFile:     envFile,
		ServiceName: "backend",
		StateDir:    tmpDir,
	}, runner)

	if _, err := server.Apply(context.Background(), ApplyRequest{TargetVersion: "v0.5.0"}); err == nil {
		t.Fatalf("expected apply error")
	}

	content, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatalf("read env file after failed apply: %v", err)
	}
	if string(content) != "AE_IMAGE_TAG=v0.4.0\n" {
		t.Fatalf("expected env tag restored to previous value, got %q", string(content))
	}
}

func TestUpdaterServerApplyAndRollbackWorkWithoutExplicitImageTag(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	envFile := filepath.Join(tmpDir, ".env")
	initialEnv := "AE_RELAY_URL=http://relay.example.com\n"
	if err := os.WriteFile(envFile, []byte(initialEnv), 0o644); err != nil {
		t.Fatalf("write initial env file: %v", err)
	}

	runner := &composeRunnerStub{}
	server := NewUpdaterServer(UpdaterConfig{
		ComposeFile: "deploy/docker-compose.yml",
		EnvFile:     envFile,
		ServiceName: "backend",
		StateDir:    tmpDir,
	}, runner)

	status, err := server.Apply(context.Background(), ApplyRequest{TargetVersion: "v0.5.0"})
	if err != nil {
		t.Fatalf("apply update: %v", err)
	}
	if status.Phase != "updating" {
		t.Fatalf("expected updating phase, got %q", status.Phase)
	}
	if status.TargetVersion != "v0.5.0" {
		t.Fatalf("expected target version v0.5.0, got %q", status.TargetVersion)
	}

	content, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatalf("read env file after apply: %v", err)
	}
	if string(content) != "AE_RELAY_URL=http://relay.example.com\nAE_IMAGE_TAG=v0.5.0\n" {
		t.Fatalf("expected env file to append image tag, got %q", string(content))
	}

	status, err = server.Rollback(context.Background())
	if err != nil {
		t.Fatalf("rollback update: %v", err)
	}
	if status.Phase != "rolling_back" {
		t.Fatalf("expected rolling_back phase, got %q", status.Phase)
	}
	if status.TargetVersion != "latest" {
		t.Fatalf("expected rollback target version latest, got %q", status.TargetVersion)
	}

	content, err = os.ReadFile(envFile)
	if err != nil {
		t.Fatalf("read env file after rollback: %v", err)
	}
	if string(content) != initialEnv {
		t.Fatalf("expected env file restored without explicit image tag, got %q", string(content))
	}
}

func TestUpdaterServerApplyRestoresImplicitLatestWhenComposeUpFails(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	envFile := filepath.Join(tmpDir, ".env")
	initialEnv := "AE_RELAY_URL=http://relay.example.com\n"
	if err := os.WriteFile(envFile, []byte(initialEnv), 0o644); err != nil {
		t.Fatalf("write initial env file: %v", err)
	}

	runner := &composeRunnerStub{
		failAtCall: 2,
		err:        errors.New("compose up failed"),
	}
	server := NewUpdaterServer(UpdaterConfig{
		ComposeFile: "deploy/docker-compose.yml",
		EnvFile:     envFile,
		ServiceName: "backend",
		StateDir:    tmpDir,
	}, runner)

	if _, err := server.Apply(context.Background(), ApplyRequest{TargetVersion: "v0.5.0"}); err == nil {
		t.Fatalf("expected apply error")
	}

	content, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatalf("read env file after failed apply: %v", err)
	}
	if string(content) != initialEnv {
		t.Fatalf("expected env file restored without explicit image tag, got %q", string(content))
	}
}
