package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/ai-efficiency/ae-cli/config"
	"github.com/ai-efficiency/ae-cli/internal/session"
	"github.com/ai-efficiency/ae-cli/internal/shell"
	"github.com/ai-efficiency/ae-cli/internal/tmux"
	"github.com/ai-efficiency/ae-cli/internal/toolconfig"
	"github.com/spf13/cobra"
)

type shellRunner interface {
	Run() error
	ShouldKillTmux() bool
}

type ticker interface {
	Chan() <-chan time.Time
	Stop()
}

type realTicker struct{ *time.Ticker }

func (t realTicker) Chan() <-chan time.Time { return t.C }

// newShellRunner is an injection point for cmd tests. Production uses the real
// interactive shell implementation.
var newShellRunner = func(cfg *config.Config, state *session.State) shellRunner {
	return shell.New(cfg, state)
}

var (
	shellSignalSet         = []os.Signal{syscall.SIGTERM, syscall.SIGHUP}
	newHeartbeatTicker     = func(d time.Duration) ticker { return realTicker{time.NewTicker(d)} }
	shellHeartbeatInterval = 30 * time.Second
)

func shellToolNames(cfg *config.Config) string {
	if cfg == nil {
		return ""
	}
	var names []string
	for name := range cfg.Tools {
		names = append(names, name)
	}
	return strings.Join(names, ", ")
}

func applyRuntimeEnvironment(tmuxSession string, rt *session.RuntimeBundle) {
	if rt == nil {
		return
	}

	env := map[string]string{}
	for k, v := range rt.EnvBundle {
		env[k] = v
	}

	if rt.Proxy != nil {
		env = toolconfig.ApplyClaudeProxyEnv(env, toolconfig.ClaudeEnv{
			BaseURL: "http://" + rt.Proxy.ListenAddr + "/anthropic",
			Token:   rt.Proxy.AuthToken,
		})
		_ = os.Unsetenv("ANTHROPIC_API_KEY")
	}

	for k, v := range env {
		_ = os.Setenv(k, v)
	}
	if tmuxSession != "" {
		_ = tmux.SetEnvironment(tmuxSession, env)
		if rt.Proxy != nil {
			_ = tmux.UnsetEnvironment(tmuxSession, []string{"ANTHROPIC_API_KEY"})
		}
	}
}

func startHeartbeatLoop(mgr *session.Manager, state *session.State) func() {
	if mgr == nil || state == nil || strings.TrimSpace(state.ID) == "" {
		return func() {}
	}
	t := newHeartbeatTicker(shellHeartbeatInterval)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.Chan():
				hbCtx, hbCancel := context.WithTimeout(context.Background(), 5*time.Second)
				_ = mgr.Heartbeat(hbCtx, state.ID)
				hbCancel()
			}
		}
	}()
	return func() {
		cancel()
		t.Stop()
		<-done
	}
}

var shellCmd = &cobra.Command{
	Use:    "shell",
	Short:  "Start the interactive agent shell (used internally by tmux)",
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		mgr := session.NewManager(apiClient, cfg)
		state, err := mgr.Current()
		if err != nil {
			return fmt.Errorf("checking session: %w", err)
		}
		if state == nil {
			return fmt.Errorf("no active session")
		}

		// Load runtime env bundle so tools (and router) see session-scoped variables.
		if bound, err := session.ResolveBoundState(""); err != nil {
			return fmt.Errorf("resolving session binding: %w", err)
		} else if bound != nil && bound.Runtime != nil {
			applyRuntimeEnvironment(state.TmuxSession, bound.Runtime)
		} else if rt, err := session.ReadRuntimeBundle(state.ID); err == nil {
			applyRuntimeEnvironment(state.TmuxSession, rt)
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("loading runtime bundle: %w", err)
		}

		// Register signal handler — only SIGTERM, not SIGINT
		// SIGINT (Ctrl+C) is used to cancel current input in interactive shells
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, shellSignalSet...)
		stopHeartbeat := startHeartbeatLoop(mgr, state)
		go func() {
			sig, ok := <-sigCh
			if !ok {
				return // channel closed, normal exit
			}
			_ = sig
			signal.Stop(sigCh)
			stopHeartbeat()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			mgr.Shutdown(ctx)
			os.Exit(0)
		}()

		s := newShellRunner(cfg, state)
		for _, line := range shell.BannerLines(shellToolNames(cfg)) {
			fmt.Println(line)
		}
		err = s.Run()

		// Clean up signal goroutine on normal exit
		signal.Stop(sigCh)
		close(sigCh)
		stopHeartbeat()

		// Graceful shutdown: mark session completed on backend
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		mgr.Shutdown(ctx)

		// Kill tmux session if shell decided to (e.g. user confirmed exit with active panes)
		if s.ShouldKillTmux() && state.TmuxSession != "" {
			_ = tmux.KillSession(state.TmuxSession)
		}

		return err
	},
}

func init() {
	rootCmd.AddCommand(shellCmd)
}
