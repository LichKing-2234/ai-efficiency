package deployment

import (
	"context"
	"fmt"
	"os/exec"
)

type SystemdServiceConfig struct {
	ServiceName string
}

type CommandRunner interface {
	Run(ctx context.Context, name string, args ...string) error
}

type ExecCommandRunner struct{}

func NewExecCommandRunner() *ExecCommandRunner {
	return &ExecCommandRunner{}
}

func (r *ExecCommandRunner) Run(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s %v: %w: %s", name, args, err, string(output))
	}
	return nil
}

type SystemdServiceManager struct {
	cfg    SystemdServiceConfig
	runner CommandRunner
}

func NewSystemdServiceManager(cfg SystemdServiceConfig, runner CommandRunner) *SystemdServiceManager {
	if runner == nil {
		runner = NewExecCommandRunner()
	}
	return &SystemdServiceManager{cfg: cfg, runner: runner}
}

func (m *SystemdServiceManager) Restart(ctx context.Context) (SystemdOperationResult, error) {
	if err := m.runner.Run(ctx, "systemctl", "restart", m.cfg.ServiceName); err != nil {
		return SystemdOperationResult{}, fmt.Errorf("restart systemd service: %w", err)
	}
	return SystemdOperationResult{
		Message:     "restart initiated",
		NeedRestart: true,
	}, nil
}
