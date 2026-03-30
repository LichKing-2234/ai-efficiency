package dispatcher

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sort"
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
		runtimeEnv = rt.EnvBundle
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
		cmd.Env = append(os.Environ(), envPairs(runtimeEnv)...)
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
