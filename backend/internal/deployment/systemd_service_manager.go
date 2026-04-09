package deployment

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"
)

type SystemdServiceConfig struct {
	ServiceName  string
	RestartDelay time.Duration
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
	exitFunc  func(int)
	afterFunc func(time.Duration) <-chan time.Time
}

func NewSystemdServiceManager(cfg SystemdServiceConfig, runner CommandRunner) *SystemdServiceManager {
	return &SystemdServiceManager{
		cfg:       cfg,
		runner:    runner,
		exitFunc:  os.Exit,
		afterFunc: time.After,
	}
}

func (m *SystemdServiceManager) Restart(ctx context.Context) (SystemdOperationResult, error) {
	if m.runner != nil {
		if err := m.runner.Run(ctx, "systemctl", "restart", m.cfg.ServiceName); err != nil {
			return SystemdOperationResult{}, fmt.Errorf("restart systemd service: %w", err)
		}
		return SystemdOperationResult{
			Message:     "restart initiated",
			NeedRestart: true,
		}, nil
	}

	delay := m.cfg.RestartDelay
	if delay <= 0 {
		delay = 500 * time.Millisecond
	}

	go func() {
		<-m.afterFunc(delay)
		m.exitFunc(0)
	}()

	return SystemdOperationResult{
		Message:     "restart scheduled; systemd will restart the service after process exit",
		NeedRestart: true,
	}, nil
}
