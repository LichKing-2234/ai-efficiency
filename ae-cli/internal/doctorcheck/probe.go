package doctorcheck

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

const probePrompt = "Reply exactly: AE_DOCTOR_OK"

type ProbeCommand struct {
	Name string
	Path string
	Args []string
	Env  map[string]string
}

type CommandResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
	Err      error
	Duration time.Duration
}

type CommandRunner interface {
	Run(context.Context, ProbeCommand) CommandResult
}

type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, command ProbeCommand) CommandResult {
	started := time.Now()
	cmd := exec.CommandContext(ctx, command.Path, command.Args...)
	env := os.Environ()
	for key, value := range command.Env {
		env = append(env, key+"="+value)
	}
	cmd.Env = env
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if ctxErr := ctx.Err(); ctxErr != nil {
		err = ctxErr
	}
	result := CommandResult{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		Err:      err,
		Duration: time.Since(started),
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		result.ExitCode = exitErr.ExitCode()
	}
	return result
}

type ProbeOptions struct {
	Timeout time.Duration
	Runner  CommandRunner
	Configs []ConfigResult
}

type ProbeResult struct {
	Name     string
	Status   string
	Duration time.Duration
	Output   string
	Message  string
}

func ProbeTools(ctx context.Context, opts ProbeOptions) []ProbeResult {
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	runner := opts.Runner
	if runner == nil {
		runner = ExecRunner{}
	}
	results := make([]ProbeResult, 0, len(opts.Configs))
	for _, cfg := range opts.Configs {
		if cfg.Status != StatusOK || !cfg.Probeable {
			reason := cfg.SkipReason
			if reason == "" {
				reason = "configuration failed"
			}
			results = append(results, ProbeResult{Name: cfg.Name, Status: StatusSkipped, Message: reason})
			continue
		}
		command := probeCommand(cfg)
		probeCtx, cancel := context.WithTimeout(ctx, timeout)
		result := runner.Run(probeCtx, command)
		cancel()
		results = append(results, classifyProbeResult(cfg.Name, result, timeout))
	}
	return results
}

func probeCommand(cfg ConfigResult) ProbeCommand {
	switch cfg.Name {
	case "codex":
		return ProbeCommand{
			Name: "codex",
			Path: cfg.ExecutablePath,
			Args: []string{"--ask-for-approval", "never", "exec", "--ephemeral", "--sandbox", "read-only", probePrompt},
		}
	case "claude":
		return ProbeCommand{
			Name: "claude",
			Path: cfg.ExecutablePath,
			Args: []string{"-p", probePrompt, "--output-format", "text", "--no-session-persistence", "--tools", ""},
		}
	case "gemini":
		return ProbeCommand{
			Name: "gemini",
			Path: cfg.ExecutablePath,
			Args: []string{"--prompt", probePrompt, "--output-format", "text", "--skip-trust"},
			Env:  cfg.ProbeEnv,
		}
	default:
		return ProbeCommand{Name: cfg.Name, Path: cfg.ExecutablePath}
	}
}

func classifyProbeResult(name string, result CommandResult, timeout time.Duration) ProbeResult {
	out := strings.TrimSpace(result.Stdout)
	errText := strings.TrimSpace(result.Stderr)
	probe := ProbeResult{Name: name, Duration: result.Duration}
	if errors.Is(result.Err, context.DeadlineExceeded) || errors.Is(result.Err, context.Canceled) {
		probe.Status = StatusFailed
		probe.Message = fmt.Sprintf("timeout after %s", timeout)
		if errText != "" {
			probe.Message += ": " + truncate(errText, 240)
		}
		return probe
	}
	if result.Err != nil {
		probe.Status = StatusFailed
		probe.Message = truncate(firstNonEmpty(errText, result.Err.Error()), 240)
		return probe
	}
	if strings.TrimSpace(out) == "" {
		probe.Status = StatusFailed
		probe.Message = "empty output"
		return probe
	}
	probe.Status = StatusOK
	probe.Output = truncate(out, 120)
	return probe
}

func FormatProbeResult(result ProbeResult) string {
	parts := []string{fmt.Sprintf("%s:", result.Name), result.Status}
	if result.Duration > 0 {
		parts = append(parts, "duration="+result.Duration.Round(time.Millisecond).String())
	}
	if result.Output != "" {
		parts = append(parts, "output="+result.Output)
	}
	if result.Message != "" {
		parts = append(parts, result.Message)
	}
	return RedactSecrets(strings.Join(parts, " "))
}

func truncate(value string, limit int) string {
	value = strings.TrimSpace(value)
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "..."
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
