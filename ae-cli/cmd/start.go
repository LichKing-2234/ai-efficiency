package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/ai-efficiency/ae-cli/internal/hooks"
	"github.com/ai-efficiency/ae-cli/internal/session"
	"github.com/ai-efficiency/ae-cli/internal/tmux"
	"github.com/spf13/cobra"
)

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start a new efficiency tracking session",
	RunE: func(cmd *cobra.Command, args []string) error {
		if resolveToken(cfg.Server.Token, "") == "" {
			return fmt.Errorf("not logged in — run 'ae-cli login'")
		}

		mgr := session.NewManager(apiClient, cfg)

		// Check if there's already an active session
		existing, err := mgr.Current()
		if err != nil {
			return fmt.Errorf("checking session: %w", err)
		}
		if existing != nil {
			// Check if tmux session is still alive
			if existing.TmuxSession != "" && tmux.SessionExists(existing.TmuxSession) {
				// If we're already inside this tmux session, don't attach (prevents infinite recursion)
				if tmux.IsInsideSession(existing.TmuxSession) {
					fmt.Printf("Already inside session %s (tmux: %s)\n", existing.ID, existing.TmuxSession)
					return nil
				}
				fmt.Printf("Session already active: %s\n", existing.ID)
				fmt.Printf("Attaching to tmux session %s...\n", existing.TmuxSession)
				tmuxCmd := exec.Command("tmux", "attach-session", "-t", existing.TmuxSession)
				tmuxCmd.Stdin = os.Stdin
				tmuxCmd.Stdout = os.Stdout
				tmuxCmd.Stderr = os.Stderr
				_ = tmuxCmd.Run()
				return nil
			}
			// Tmux session is dead, clean up and start fresh
			fmt.Printf("Previous session %s has no active tmux. Cleaning up...\n", existing.ID)
			if _, err := mgr.Stop(); err != nil {
				return fmt.Errorf("cleaning up previous session: %w", err)
			}
		}

		state, err := mgr.Start()
		if err != nil {
			return fmt.Errorf("starting session: %w", err)
		}

		// Install shared git hooks now that bootstrap artifacts (marker/runtime) are in place.
		// If we can't safely take over hooks, stop the session and fail explicitly.
		selfPath, _ := os.Executable()
		if strings.TrimSpace(selfPath) == "" {
			selfPath = os.Args[0]
		}
		if err := hooks.InstallSharedHooks(state.WorkspaceRoot, selfPath); err != nil {
			_, _ = mgr.Stop()
			return fmt.Errorf("installing shared git hooks: %w", err)
		}

		// Register signal handler for graceful shutdown
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		go func() {
			sig, ok := <-sigCh
			if !ok {
				return // channel closed, normal exit
			}
			_ = sig
			signal.Stop(sigCh)
			if state.TmuxSession != "" && tmux.SessionExists(state.TmuxSession) {
				_ = tmux.KillSession(state.TmuxSession)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			mgr.Shutdown(ctx)
			os.Exit(0)
		}()

		// Create tmux session if tmux is available
		if tmux.HasTmux() {
			suffix := state.ID
			if len(suffix) > 8 {
				suffix = suffix[:8]
			}
			tmuxName := "ae-" + suffix
			// Get the path to the current ae-cli binary
			selfPath, _ := os.Executable()
			// Create tmux session with agent shell as the initial command
			if err := tmux.NewSessionWithCommand(tmuxName, selfPath+" shell"); err != nil {
				fmt.Printf("warning: failed to create tmux session: %v\n", err)
			} else {
				state.TmuxSession = tmuxName
				if err := mgr.SaveState(state); err != nil {
					fmt.Printf("warning: failed to save tmux session name: %v\n", err)
				}
			}
		}

		fmt.Printf("Session started!\n")
		fmt.Printf("  ID:     %s\n", state.ID)
		fmt.Printf("  Repo:   %s\n", state.Repo)
		fmt.Printf("  Branch: %s\n", state.Branch)
		fmt.Printf("  Time:   %s\n", state.StartedAt.Format("2006-01-02 15:04:05"))
		if state.TmuxSession != "" {
			fmt.Printf("  Tmux:   %s\n", state.TmuxSession)

			// Auto-attach to tmux session (skip if already inside to prevent recursion)
			if !tmux.IsInsideSession(state.TmuxSession) {
				tmuxCmd := exec.Command("tmux", "attach-session", "-t", state.TmuxSession)
				tmuxCmd.Stdin = os.Stdin
				tmuxCmd.Stdout = os.Stdout
				tmuxCmd.Stderr = os.Stderr
				_ = tmuxCmd.Run()
			}
		}

		signal.Stop(sigCh)
		close(sigCh)

		return nil
	},
}

func init() {
	rootCmd.AddCommand(startCmd)
}
