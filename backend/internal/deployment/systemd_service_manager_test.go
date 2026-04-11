package deployment

import (
	"context"
	"testing"
	"time"
)

type commandRunnerStub struct {
	args [][]string
	err  error
}

func (r *commandRunnerStub) Run(_ context.Context, name string, args ...string) error {
	r.args = append(r.args, append([]string{name}, args...))
	return r.err
}

func TestSystemdServiceManagerRestart(t *testing.T) {
	runner := &commandRunnerStub{}
	done := make(chan struct{}, 1)
	manager := NewSystemdServiceManager(SystemdServiceConfig{
		ServiceName: "ai-efficiency",
	}, runner)
	manager.afterFunc = func(time.Duration) <-chan time.Time {
		ch := make(chan time.Time, 1)
		ch <- time.Now()
		return ch
	}
	manager.runner = CommandRunner(commandRunnerFunc(func(_ context.Context, name string, args ...string) error {
		runner.args = append(runner.args, append([]string{name}, args...))
		done <- struct{}{}
		return nil
	}))

	result, err := manager.Restart(context.Background())
	if err != nil {
		t.Fatalf("Restart: %v", err)
	}
	if !result.NeedRestart {
		t.Fatalf("NeedRestart = false, want true")
	}
	if result.Message != "restart scheduled" {
		t.Fatalf("unexpected message: %q", result.Message)
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for runner")
	}
	if len(runner.args) != 1 || runner.args[0][0] != "systemctl" || runner.args[0][1] != "restart" || runner.args[0][2] != "ai-efficiency" {
		t.Fatalf("args = %#v", runner.args)
	}
}

type commandRunnerFunc func(context.Context, string, ...string) error

func (f commandRunnerFunc) Run(ctx context.Context, name string, args ...string) error {
	return f(ctx, name, args...)
}

func TestSystemdServiceManagerRestartFallsBackToSelfExit(t *testing.T) {
	done := make(chan int, 1)
	manager := &SystemdServiceManager{
		cfg: SystemdServiceConfig{
			ServiceName:  "ai-efficiency",
			RestartDelay: 0,
		},
		exitFunc: func(code int) {
			done <- code
		},
		afterFunc: func(time.Duration) <-chan time.Time {
			ch := make(chan time.Time, 1)
			ch <- time.Now()
			return ch
		},
	}

	result, err := manager.Restart(context.Background())
	if err != nil {
		t.Fatalf("Restart: %v", err)
	}
	if !result.NeedRestart {
		t.Fatalf("NeedRestart = false, want true")
	}
	if result.Message != "restart scheduled; systemd will restart the service after process exit" {
		t.Fatalf("unexpected message: %q", result.Message)
	}

	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("exit code = %d, want 0", code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for self-exit")
	}
}
