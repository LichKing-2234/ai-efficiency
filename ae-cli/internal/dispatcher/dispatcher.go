package dispatcher

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/ai-efficiency/ae-cli/config"
	"github.com/ai-efficiency/ae-cli/internal/client"
	"github.com/ai-efficiency/ae-cli/internal/session"
	"github.com/ai-efficiency/ae-cli/internal/tmux"
)

var execCommand = exec.Command

type Dispatcher struct {
	config *config.Config
	client *client.Client
}

func New(cfg *config.Config, c *client.Client) *Dispatcher {
	return &Dispatcher{
		config: cfg,
		client: c,
	}
}

func envPairs(m map[string]string) []string {
	if len(m) == 0 {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, k+"="+m[k])
	}
	return out
}

func proxyClaudeEnvActive(env map[string]string) bool {
	if len(env) == 0 {
		return false
	}
	return strings.TrimSpace(env["ANTHROPIC_AUTH_TOKEN"]) != "" || strings.TrimSpace(env["ANTHROPIC_BASE_URL"]) != ""
}

func sanitizeRuntimeEnv(env map[string]string) map[string]string {
	if len(env) == 0 {
		return nil
	}
	out := make(map[string]string, len(env))
	for k, v := range env {
		out[k] = v
	}
	if proxyClaudeEnvActive(out) {
		delete(out, "ANTHROPIC_API_KEY")
	}
	return out
}

func mergeProcessEnv(base []string, runtimeEnv map[string]string) []string {
	if len(runtimeEnv) == 0 {
		return base
	}
	merged := map[string]string{}
	for _, kv := range base {
		parts := strings.SplitN(kv, "=", 2)
		if len(parts) != 2 || parts[0] == "" {
			continue
		}
		merged[parts[0]] = parts[1]
	}
	if proxyClaudeEnvActive(runtimeEnv) {
		delete(merged, "ANTHROPIC_API_KEY")
	}
	for k, v := range runtimeEnv {
		merged[k] = v
	}
	return envPairs(merged)
}

// Run executes a tool. If tmuxSession is non-empty, it runs inside a tmux pane.
func (d *Dispatcher) Run(sessionID, toolName string, extraArgs []string, tmuxSession string) error {
	toolCfg, ok := d.config.Tools[toolName]
	if !ok {
		return fmt.Errorf("tool %q not found in config", toolName)
	}

	args := make([]string, len(toolCfg.Args))
	copy(args, toolCfg.Args)
	args = append(args, extraArgs...)

	start := time.Now()

	var runtimeEnv map[string]string
	if rt, err := session.ReadRuntimeBundle(sessionID); err == nil && rt != nil {
		runtimeEnv = sanitizeRuntimeEnv(rt.EnvBundle)
	} else if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("loading runtime bundle: %w", err)
	}

	if tmuxSession != "" {
		// Best-effort: make runtime env visible to future panes in this tmux session.
		if len(runtimeEnv) > 0 {
			_ = tmux.SetEnvironment(tmuxSession, runtimeEnv)
		}

		// Always split a new pane — keep the initial pane as idle control pane
		if _, err := tmux.SplitWindow(tmuxSession, toolName, toolCfg.Command, args); err != nil {
			return fmt.Errorf("splitting tmux pane: %w", err)
		}
		fmt.Printf("Tool %q launched in tmux session %q\n", toolName, tmuxSession)
		fmt.Printf("Run 'ae-cli attach' to view all panes.\n")

		// For tmux-launched tools, record start time only (end is unknown)
		inv := client.Invocation{
			Tool:  toolName,
			Start: start,
			End:   start, // same as start — indicates async launch
		}
		if err := d.client.AddInvocation(context.Background(), sessionID, inv); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to record invocation: %v\n", err)
		}
		return nil
	}

	// Direct execution (no tmux)
	cmd := execCommand(toolCfg.Command, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if len(runtimeEnv) > 0 {
		cmd.Env = mergeProcessEnv(os.Environ(), runtimeEnv)
	}

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("running tool %q: %w", toolName, err)
	}

	end := time.Now()

	inv := client.Invocation{
		Tool:  toolName,
		Start: start,
		End:   end,
	}

	if err := d.client.AddInvocation(context.Background(), sessionID, inv); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to record invocation: %v\n", err)
	}

	return nil
}
