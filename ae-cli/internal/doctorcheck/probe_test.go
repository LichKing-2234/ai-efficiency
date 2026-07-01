package doctorcheck

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type fakeRunner struct {
	calls []ProbeCommand
	run   func(context.Context, ProbeCommand) CommandResult
}

func (f *fakeRunner) Run(ctx context.Context, cmd ProbeCommand) CommandResult {
	f.calls = append(f.calls, cmd)
	return f.run(ctx, cmd)
}

func TestProbeToolsRunsConfiguredCommands(t *testing.T) {
	runner := &fakeRunner{run: func(ctx context.Context, cmd ProbeCommand) CommandResult {
		return CommandResult{Stdout: "AE_DOCTOR_OK\n"}
	}}
	results := ProbeTools(context.Background(), ProbeOptions{
		Timeout: 30 * time.Second,
		Runner:  runner,
		Configs: []ConfigResult{
			{Name: "codex", Status: StatusOK, Probeable: true, ExecutablePath: "/bin/codex"},
			{Name: "claude", Status: StatusOK, Probeable: true, ExecutablePath: "/bin/claude"},
			{Name: "gemini", Status: StatusOK, Probeable: true, ExecutablePath: "/bin/gemini", ProbeEnv: map[string]string{"GEMINI_API_KEY": "sk-gemini"}},
		},
	})
	if len(results) != 3 {
		t.Fatalf("result count = %d, want 3", len(results))
	}
	for _, result := range results {
		if result.Status != StatusOK {
			t.Fatalf("%s status = %s message=%s", result.Name, result.Status, result.Message)
		}
	}
	if runner.calls[0].Args[0] != "--ask-for-approval" || runner.calls[0].Args[2] != "exec" {
		t.Fatalf("codex args = %v", runner.calls[0].Args)
	}
	if runner.calls[1].Args[0] != "-p" {
		t.Fatalf("claude args = %v", runner.calls[1].Args)
	}
	if runner.calls[2].Env["GEMINI_API_KEY"] != "sk-gemini" {
		t.Fatalf("gemini env = %+v", runner.calls[2].Env)
	}
}

func TestProbeToolsReportsStartAndResultCallbacksInOrder(t *testing.T) {
	events := []string{}
	runner := &fakeRunner{run: func(ctx context.Context, cmd ProbeCommand) CommandResult {
		events = append(events, "run:"+cmd.Name)
		return CommandResult{Stdout: "AE_DOCTOR_OK\n"}
	}}
	results := ProbeTools(context.Background(), ProbeOptions{
		Timeout: 30 * time.Second,
		Runner:  runner,
		Configs: []ConfigResult{
			{Name: "codex", Status: StatusOK, Probeable: true, ExecutablePath: "/bin/codex"},
			{Name: "claude", Status: StatusOK, Probeable: true, ExecutablePath: "/bin/claude"},
		},
		OnStart: func(cfg ConfigResult, timeout time.Duration) {
			events = append(events, "start:"+cfg.Name+":"+timeout.String())
		},
		OnResult: func(result ProbeResult) {
			events = append(events, "result:"+result.Name+":"+result.Status)
		},
	})
	if len(results) != 2 {
		t.Fatalf("result count = %d, want 2", len(results))
	}
	want := []string{
		"start:codex:30s",
		"run:codex",
		"result:codex:ok",
		"start:claude:30s",
		"run:claude",
		"result:claude:ok",
	}
	if strings.Join(events, "|") != strings.Join(want, "|") {
		t.Fatalf("events = %v, want %v", events, want)
	}
}

func TestRedactSecretsDoesNotCorruptCodexApprovalFlag(t *testing.T) {
	line := RedactSecrets("unexpected argument '--ask-for-approval' found")
	if strings.Contains(line, "--ask-<redacted>for-approval") {
		t.Fatalf("redaction corrupted flag: %s", line)
	}
	if !strings.Contains(line, "--ask-for-approval") {
		t.Fatalf("redaction removed flag: %s", line)
	}
}

func TestProbeToolsReportsTimeoutAndRedactsOutput(t *testing.T) {
	runner := &fakeRunner{run: func(ctx context.Context, cmd ProbeCommand) CommandResult {
		return CommandResult{Err: context.DeadlineExceeded, Stderr: "failed with sk-secret"}
	}}
	results := ProbeTools(context.Background(), ProbeOptions{
		Timeout: time.Millisecond,
		Runner:  runner,
		Configs: []ConfigResult{{Name: "codex", Status: StatusOK, Probeable: true, ExecutablePath: "/bin/codex"}},
	})
	if results[0].Status != StatusFailed {
		t.Fatalf("status = %s, want failed", results[0].Status)
	}
	line := FormatProbeResult(results[0])
	if strings.Contains(line, "sk-secret") {
		t.Fatalf("line leaked secret: %s", line)
	}
	if !strings.Contains(line, "timeout") {
		t.Fatalf("line = %s, want timeout", line)
	}
}

func TestProbeToolsSkipsConfigurationFailures(t *testing.T) {
	runner := &fakeRunner{run: func(ctx context.Context, cmd ProbeCommand) CommandResult {
		t.Fatalf("runner should not be called")
		return CommandResult{}
	}}
	results := ProbeTools(context.Background(), ProbeOptions{
		Timeout: 30 * time.Second,
		Runner:  runner,
		Configs: []ConfigResult{{Name: "claude", Status: StatusFailed, Probeable: false, SkipReason: "configuration failed"}},
	})
	if results[0].Status != StatusSkipped {
		t.Fatalf("status = %s, want skipped", results[0].Status)
	}
}

func TestProbeToolsReportsNonZeroAndEmptyStdout(t *testing.T) {
	for name, commandResult := range map[string]CommandResult{
		"nonzero": {ExitCode: 2, Stderr: "bad"},
		"empty":   {Stdout: ""},
		"error":   {Err: errors.New("spawn failed")},
	} {
		t.Run(name, func(t *testing.T) {
			runner := &fakeRunner{run: func(ctx context.Context, cmd ProbeCommand) CommandResult {
				return commandResult
			}}
			results := ProbeTools(context.Background(), ProbeOptions{
				Timeout: 30 * time.Second,
				Runner:  runner,
				Configs: []ConfigResult{{Name: "gemini", Status: StatusOK, Probeable: true, ExecutablePath: "/bin/gemini"}},
			})
			if results[0].Status != StatusFailed {
				t.Fatalf("status = %s, want failed", results[0].Status)
			}
		})
	}
}
