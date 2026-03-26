package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ai-efficiency/ae-cli/internal/session"
	"github.com/ai-efficiency/ae-cli/internal/shell"
	"github.com/ai-efficiency/ae-cli/internal/tmux"
	"github.com/spf13/cobra"
)

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

		// Register signal handler — only SIGTERM, not SIGINT
		// SIGINT (Ctrl+C) is used to cancel current input in interactive shells
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGTERM)
		go func() {
			sig, ok := <-sigCh
			if !ok {
				return // channel closed, normal exit
			}
			_ = sig
			signal.Stop(sigCh)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			mgr.Shutdown(ctx)
			os.Exit(0)
		}()

		s := shell.New(cfg, state)
		err = s.Run()

		// Clean up signal goroutine on normal exit
		signal.Stop(sigCh)
		close(sigCh)

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
