package deployment

import (
	"context"
	"testing"
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
	manager := NewSystemdServiceManager(SystemdServiceConfig{
		ServiceName: "ai-efficiency",
	}, runner)

	result, err := manager.Restart(context.Background())
	if err != nil {
		t.Fatalf("Restart: %v", err)
	}
	if !result.NeedRestart {
		t.Fatalf("NeedRestart = false, want true")
	}
	if len(runner.args) != 1 || runner.args[0][0] != "systemctl" || runner.args[0][1] != "restart" || runner.args[0][2] != "ai-efficiency" {
		t.Fatalf("args = %#v", runner.args)
	}
}
